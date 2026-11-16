package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"api/pkg/secretbox"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ScopeTools is the scope of every binary that calls Load instead of
// LoadService: the one-off cmd tools.
const ScopeTools = "tools"

type storeState struct {
	scope  string
	key    []byte
	values map[string]string
}

type StoredSecret struct {
	Scope  string
	Key    string
	Cipher []byte
}

func loadStore(scope string) error {
	key, err := secretbox.ParseKey(KEKEnv, lookup(KEKEnv, ""))
	if err != nil {
		return err
	}
	next := storeState{scope: scope, key: key}
	if key != nil {
		rows, err := fetchStore(mainDatabase())
		if err != nil {
			return fmt.Errorf("config store: %w", err)
		}
		next.values, err = Resolve(key, scope, rows)
		if err != nil {
			return err
		}
		slog.Info("config store loaded", "scope", scope, "values", len(next.values))
	}
	mu.Lock()
	store = next
	mu.Unlock()
	return nil
}

// Resolve opens the rows that apply to scope; a row for the scope itself wins
// over the row for every service.
func Resolve(key []byte, scope string, rows []StoredSecret) (map[string]string, error) {
	out := map[string]string{}
	apply := func(want string) error {
		for _, r := range rows {
			if r.Scope != want {
				continue
			}
			plain, err := secretbox.Open(key, r.Cipher)
			if err != nil {
				return fmt.Errorf("config store: %s (scope %q) does not open with %s: %w", r.Key, r.Scope, KEKEnv, err)
			}
			out[r.Key] = plain
		}
		return nil
	}
	if err := apply(""); err != nil {
		return nil, err
	}
	if scope != "" {
		if err := apply(scope); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func fetchStore(db DatabaseConfig) ([]StoredSecret, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, db.DSN())
	if err != nil {
		return nil, err
	}
	defer conn.Close(context.Background())

	rows, err := conn.Query(ctx, `SELECT scope, key, cipher FROM config_secrets`)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
			return nil, nil
		}
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (StoredSecret, error) {
		var s StoredSecret
		err := r.Scan(&s.Scope, &s.Key, &s.Cipher)
		return s, err
	})
}
