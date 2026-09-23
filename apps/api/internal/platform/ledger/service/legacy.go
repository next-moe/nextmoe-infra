package service

import (
	"context"
	"database/sql"
	stderrors "errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// Advisory-lock keys share one keyspace across the instance ("ldgr" in ASCII);
// keep it distinct from every suite and job key.
const importLockKey = 0x6c646772

type Imported struct {
	Legacy   int64
	Openings int64
}

// ImportLegacy carries every moemoepoint_log row the ledger has not seen into
// it, and opens an account for every user whose balance the log does not
// explain. It is idempotent and incremental, and it is correct only while
// nothing is writing the ledger for the users it touches — which is why
// cmd/oauth runs it before it serves, after the binary that still wrote
// moemoepoint_log has stopped: rows that binary wrote after cmd/migrate's pass
// would otherwise never reach the ledger, and the first transfer to their user
// would overwrite users.moemoepoint with a balance missing them.
func ImportLegacy(ctx context.Context, db *gorm.DB) (Imported, error) {
	var out Imported
	err := db.WithContext(ctx).Connection(func(conn *gorm.DB) error {
		// A session lock taken before the transaction, not an xact lock inside
		// it: the repeatable-read snapshot starts at the first statement, so an
		// importer that waited for the lock inside its transaction would import
		// from a snapshot that cannot see the rows the previous holder wrote.
		if err := conn.Exec(`SELECT pg_advisory_lock(?)`, importLockKey).Error; err != nil {
			return err
		}
		defer conn.WithContext(context.Background()).Exec(`SELECT pg_advisory_unlock(?)`, importLockKey)
		for attempt := 1; ; attempt++ {
			var err error
			out, err = importOnce(conn)
			if err == nil || !isSerializationFailure(err) || attempt == 5 {
				return err
			}
		}
	})
	return out, err
}

func isSerializationFailure(err error) bool {
	var pgErr *pgconn.PgError
	return stderrors.As(err, &pgErr) && (pgErr.Code == "40001" || pgErr.Code == "40P01")
}

func importOnce(conn *gorm.DB) (Imported, error) {
	var out Imported
	err := conn.Transaction(func(tx *gorm.DB) error {
		for _, stmt := range importStage {
			if err := tx.Exec(stmt).Error; err != nil {
				return fmt.Errorf("ledger import: %w", err)
			}
		}
		if err := tx.Raw(`
			SELECT COUNT(*) FILTER (WHERE legacy_id IS NOT NULL) AS legacy,
			       COUNT(*) FILTER (WHERE legacy_id IS NULL) AS openings
			FROM ledger_import_moves`).Scan(&out).Error; err != nil {
			return err
		}
		if out.Legacy+out.Openings == 0 {
			return nil
		}
		for _, stmt := range importApply {
			if err := tx.Exec(stmt).Error; err != nil {
				return fmt.Errorf("ledger import: %w", err)
			}
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	return out, err
}

// A legacy row whose key a ledger transfer already holds is not pending: the
// only way that happens is a retry answered once by each binary, and the
// ledger's answer is the one that stands.
var importStage = []string{
	`CREATE TEMP TABLE ledger_import_moves (
		n bigint, legacy_id bigint, user_id bigint NOT NULL, delta bigint NOT NULL,
		reason text NOT NULL, source_app text NOT NULL, ref text NOT NULL,
		actor_user_id bigint NOT NULL, idempotency_key text NOT NULL, note text NOT NULL,
		created_at timestamptz NOT NULL, counter_kind text NOT NULL, counter_code text NOT NULL,
		transfer_id bigint, user_account_id bigint, counter_account_id bigint
	) ON COMMIT DROP`,

	`INSERT INTO ledger_import_moves (legacy_id, user_id, delta, reason, source_app, ref,
		actor_user_id, idempotency_key, note, created_at, counter_kind, counter_code)
	SELECT l.id, l.user_id, l.delta, l.reason, l.source_app, COALESCE(l.ref, ''),
		l.actor_user_id, l.idempotency_key, COALESCE(l.note, ''), l.created_at,
		CASE WHEN l.reason = 'name_change' THEN 'sink' ELSE 'issuer' END, l.source_app
	FROM moemoepoint_log l
	WHERE NOT EXISTS (SELECT 1 FROM ledger_transfers t WHERE t.legacy_log_id = l.id)
	  AND NOT EXISTS (SELECT 1 FROM ledger_transfers t
	                  WHERE t.source_app = l.source_app AND t.idempotency_key = l.idempotency_key)`,

	`INSERT INTO ledger_import_moves (legacy_id, user_id, delta, reason, source_app, ref,
		actor_user_id, idempotency_key, note, created_at, counter_kind, counter_code)
	SELECT NULL, u.id, u.moemoepoint - COALESCE(p.pending, 0), 'opening_balance', 'oauth', '',
		0, 'oauth:opening_balance:' || u.id, '', u.created_at, 'issuer', 'oauth'
	FROM users u
	LEFT JOIN (SELECT user_id, SUM(delta) AS pending FROM ledger_import_moves GROUP BY user_id) p
	  ON p.user_id = u.id
	WHERE u.moemoepoint - COALESCE(p.pending, 0) <> 0
	  AND NOT EXISTS (SELECT 1 FROM ledger_accounts a
	                  WHERE a.asset = 'moemoepoint' AND a.kind = 'user'
	                    AND a.user_id = u.id AND a.code = '')`,
}

var importApply = []string{
	`UPDATE ledger_import_moves m SET n = s.n
	FROM (SELECT ctid, row_number() OVER (ORDER BY legacy_id IS NOT NULL, legacy_id, user_id) AS n
	      FROM ledger_import_moves) s
	WHERE m.ctid = s.ctid`,

	`INSERT INTO ledger_accounts (asset, kind, user_id, code, balance, created_at, updated_at)
	SELECT DISTINCT 'moemoepoint', counter_kind, 0, counter_code, 0, now(), now()
	FROM ledger_import_moves
	ON CONFLICT (asset, kind, user_id, code) DO NOTHING`,

	`INSERT INTO ledger_accounts (asset, kind, user_id, code, balance, created_at, updated_at)
	SELECT DISTINCT 'moemoepoint', 'user', user_id, '', 0, now(), now()
	FROM ledger_import_moves
	ON CONFLICT (asset, kind, user_id, code) DO NOTHING`,

	`UPDATE ledger_import_moves m SET user_account_id = a.id
	FROM ledger_accounts a
	WHERE a.asset = 'moemoepoint' AND a.kind = 'user' AND a.user_id = m.user_id AND a.code = ''`,

	`UPDATE ledger_import_moves m SET counter_account_id = a.id
	FROM ledger_accounts a
	WHERE a.asset = 'moemoepoint' AND a.kind = m.counter_kind AND a.user_id = 0 AND a.code = m.counter_code`,

	`INSERT INTO ledger_transfers (asset, reason, source_app, idempotency_key, ref,
		actor_user_id, note, legacy_log_id, created_at)
	SELECT 'moemoepoint', reason, source_app, idempotency_key, ref, actor_user_id, note, legacy_id, created_at
	FROM ledger_import_moves ORDER BY n`,

	`UPDATE ledger_import_moves m SET transfer_id = t.id
	FROM ledger_transfers t
	WHERE t.source_app = m.source_app AND t.idempotency_key = m.idempotency_key`,

	`CREATE TEMP TABLE ledger_import_legs ON COMMIT DROP AS
	SELECT n, 0 AS leg, transfer_id, user_account_id AS account_id, delta AS amount, created_at
	FROM ledger_import_moves
	UNION ALL
	SELECT n, 1, transfer_id, counter_account_id, -delta, created_at
	FROM ledger_import_moves`,

	`INSERT INTO ledger_entries (transfer_id, account_id, amount, balance_after, created_at)
	SELECT g.transfer_id, g.account_id, g.amount,
		a.balance + SUM(g.amount) OVER (PARTITION BY g.account_id ORDER BY g.n, g.leg
		                                ROWS UNBOUNDED PRECEDING),
		g.created_at
	FROM ledger_import_legs g JOIN ledger_accounts a ON a.id = g.account_id
	ORDER BY g.n, g.leg`,

	`UPDATE ledger_accounts a SET balance = a.balance + d.total, updated_at = now()
	FROM (SELECT account_id, SUM(amount) AS total FROM ledger_import_legs GROUP BY account_id) d
	WHERE a.id = d.account_id`,

	`UPDATE users u SET moemoepoint = a.balance
	FROM ledger_accounts a
	WHERE a.asset = 'moemoepoint' AND a.kind = 'user' AND a.code = '' AND a.user_id = u.id
	  AND u.id IN (SELECT user_id FROM ledger_import_moves)
	  AND u.moemoepoint IS DISTINCT FROM a.balance`,
}
