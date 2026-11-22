// Package idseq moves ownership of the public id sequences from the legacy
// catalog tables to canon, so both keep drawing from one sequence per entity
// family and a legacy DROP TABLE can never take the sequence with it.
package idseq

import (
	"context"
	"database/sql"
	"fmt"
)

type Family struct {
	Legacy   string
	Sequence string
}

// Families is every entity family whose ids canon continues. A family missing
// here would get a canon sequence of its own and collide with legacy ids.
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
}

const Schema = "canon_ids"

func QualifiedSequence(f Family) string { return Schema + "." + f.Sequence }

func Adopt(ctx context.Context, db *sql.DB, families []Family) error {
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS `+Schema); err != nil {
		return err
	}
	for _, f := range families {
		if err := adopt(ctx, db, f); err != nil {
			return fmt.Errorf("%s: %w", f.Legacy, err)
		}
	}
	return nil
}

func adopt(ctx context.Context, db *sql.DB, f Family) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	table := "public." + f.Legacy
	seq := QualifiedSequence(f)
	var present bool
	if err := tx.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&present); err != nil {
		return err
	}
	if !present {
		if _, err := tx.ExecContext(ctx, `CREATE SEQUENCE IF NOT EXISTS `+seq+` AS bigint`); err != nil {
			return err
		}
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `LOCK TABLE `+table+` IN SHARE ROW EXCLUSIVE MODE`); err != nil {
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
	for _, stmt := range []string{
		`CREATE SEQUENCE ` + seq + ` AS bigint`,
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
