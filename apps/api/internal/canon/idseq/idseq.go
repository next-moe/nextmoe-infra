// Package idseq moves ownership of the public id sequences from the legacy
// catalog tables to canon, so both keep drawing from one sequence per entity
// family and a legacy DROP TABLE can never take the sequence with it.
package idseq

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type Family struct {
	Legacy   string
	Sequence string
}

// Families is every public entity family; Legacy is the table whose ids it
// continues, empty for a family new in canon. A legacy family missing here
// would get a sequence of its own and collide with legacy ids.
var Families = []Family{
	{"catalog_work", "work"},
	{"catalog_release", "release"},
	{"catalog_person", "person"},
	{"catalog_credit_name", "person_alias"},
	{"catalog_credit", "credit"},
	{"catalog_label", "org"},
	{"catalog_character", "character"},
	{"catalog_character_trait", "trait"},
	{"catalog_tag", "tag"},
	{"catalog_series", "franchise"},
	{"catalog_engine", "engine"},
	{"", "work_part"},
}

const Schema = "canon_ids"

func QualifiedSequence(f Family) string { return Schema + "." + f.Sequence }

func Adopt(ctx context.Context, db *sql.DB, families []Family) error {
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS `+Schema); err != nil {
		return err
	}
	for _, f := range families {
		if err := adoptWithin(ctx, db, f); err != nil {
			return fmt.Errorf("%s: %w", f.Legacy, err)
		}
	}
	return nil
}

// lockWait bounds how long an adoption queues for a legacy table. A waiting
// lock blocks every later writer, so behind one long import transaction an
// unbounded wait would be an outage of that table.
var (
	lockWait     = 2 * time.Second
	lockPause    = 3 * time.Second
	lockAttempts = 20
)

func adoptWithin(ctx context.Context, db *sql.DB, f Family) error {
	done, err := adopted(ctx, db, f)
	if err != nil || done {
		return err
	}
	for attempt := 1; ; attempt++ {
		err := adopt(ctx, db, f)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "55P03" && pg.Code != "40P01" || attempt >= lockAttempts {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(lockPause):
		}
	}
}

// adopted is checked without a lock: the migration runs on every deploy, and
// the first version locked all eleven legacy tables to learn nothing was due.
func adopted(ctx context.Context, db *sql.DB, f Family) (bool, error) {
	var sequence, table bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL, $2 <> '' AND to_regclass('public.' || $2) IS NOT NULL`,
		QualifiedSequence(f), f.Legacy).Scan(&sequence, &table); err != nil || !sequence {
		return false, err
	}
	if !table {
		return true, nil
	}
	return DrawsFrom(ctx, db, f)
}

func adopt(ctx context.Context, db *sql.DB, f Family) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	table := "public." + f.Legacy
	seq := QualifiedSequence(f)
	present := false
	if f.Legacy != "" {
		if err := tx.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&present); err != nil {
			return err
		}
	}
	if !present {
		if _, err := tx.ExecContext(ctx, `CREATE SEQUENCE IF NOT EXISTS `+seq+` AS bigint`); err != nil {
			return err
		}
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`SET LOCAL lock_timeout = '%dms'`, lockWait.Milliseconds())); err != nil {
		return err
	}
	// The strongest lock at once: SHARE ROW EXCLUSIVE here and the ALTER's
	// upgrade below deadlocked with a transaction that had read the table and
	// then wrote it, on the first statement of the 2026-11-24 rehearsal.
	if _, err := tx.ExecContext(ctx, `LOCK TABLE `+table+` IN ACCESS EXCLUSIVE MODE`); err != nil {
		return err
	}
	var identity string
	var def sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT is_identity, column_default FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1 AND column_name = 'id'`, f.Legacy).Scan(&identity, &def); err != nil {
		return err
	}
	if identity == "NO" {
		ok, err := DrawsFrom(ctx, tx, f)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("id is neither an identity nor drawn from %s (default %q)", seq, def.String)
		}
		return tx.Commit()
	}

	var owned string
	if err := tx.QueryRowContext(ctx, `SELECT pg_get_serial_sequence($1, 'id')`, table).Scan(&owned); err != nil {
		return err
	}
	var next int64
	if err := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT GREATEST(
		(SELECT CASE WHEN is_called THEN last_value ELSE last_value - 1 END FROM %s),
		(SELECT COALESCE(max(id), 0) FROM %s))`, owned, table)).Scan(&next); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE SEQUENCE IF NOT EXISTS `+seq+` AS bigint`); err != nil {
		return err
	}
	var drawn int64
	if err := tx.QueryRowContext(ctx, `SELECT CASE WHEN is_called THEN last_value ELSE 0 END FROM `+seq).Scan(&drawn); err != nil {
		return err
	}
	// CI recreated a legacy table with its identity while the canon sequence
	// already existed; the sequence must never move back below ids it handed out.
	next = max(next, drawn)
	for _, stmt := range []string{
		fmt.Sprintf(`SELECT setval('%s', %d, %t)`, seq, max(next, 1), next >= 1),
		`ALTER TABLE ` + table + ` ALTER COLUMN id DROP IDENTITY`,
		`ALTER TABLE ` + table + ` ALTER COLUMN id SET DEFAULT nextval('` + seq + `')`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%s: %w", stmt, err)
		}
	}
	return tx.Commit()
}

type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// DrawsFrom compares through regclass: Postgres quotes a keyword sequence name
// in the default (canon_ids."character"), so a text match failed on it.
func DrawsFrom(ctx context.Context, q querier, f Family) (bool, error) {
	var ok bool
	err := q.QueryRowContext(ctx, `SELECT coalesce(column_default = format('nextval(%L::regclass)', $2::regclass::text), false)
		FROM information_schema.columns WHERE table_schema = 'public' AND table_name = $1 AND column_name = 'id'`,
		f.Legacy, QualifiedSequence(f)).Scan(&ok)
	return ok, err
}
