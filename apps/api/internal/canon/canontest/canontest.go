// Package canontest opens a migrated canon client on the track's test
// database, for tests in any canon package.
package canontest

import (
	"context"
	"database/sql"
	"testing"

	"api/internal/canon/ent"
	"api/internal/canon/migrations"
	"api/internal/testsupport/dbtest"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func Client(t *testing.T) (*ent.Client, *sql.DB) {
	t.Helper()
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
	}
	db, err := sql.Open("pgx", dsn+" search_path=canon")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := migrations.Up(context.Background(), db); err != nil {
		t.Fatalf("migrate canon: %v", err)
	}
	return ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db))), db
}
