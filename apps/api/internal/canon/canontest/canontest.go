// Package canontest opens a migrated canon client on the track's test
// database, for tests in any canon package.
package canontest

import (
	"context"
	"database/sql"
	"testing"

	"api/internal/canon/ent"
	"api/internal/canon/identity"
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

// Legacy stands up an empty copy of the legacy tables identity reads, in a
// schema of its own: the shared public ones belong to the catalog suites.
func Legacy(t *testing.T, db *sql.DB) {
	t.Helper()
	const schema = "canon_legacy_test"
	ddl := `DROP SCHEMA IF EXISTS ` + schema + ` CASCADE; CREATE SCHEMA ` + schema + `;
		SET LOCAL search_path TO ` + schema + `;
		CREATE TABLE catalog_redirect (entity_type smallint, old_id bigint, current_id bigint, revoked_at timestamptz);
		CREATE TABLE catalog_work (id bigint PRIMARY KEY, medium_id smallint, display_name text, deleted_at timestamptz);
		CREATE TABLE catalog_work_title (work_id bigint, title text, mt_model text);
		CREATE TABLE catalog_person (id bigint PRIMARY KEY, deleted_at timestamptz);
		CREATE TABLE catalog_credit_name (id bigint PRIMARY KEY, person_id bigint, name text,
			name_norm text GENERATED ALWAYS AS (lower(normalize(name, NFKC))) STORED);
		CREATE TABLE catalog_name_alias (credit_name_id bigint, name_norm text);
		CREATE TABLE catalog_credit (credit_name_id bigint, work_id bigint);
		CREATE TABLE catalog_label (id bigint PRIMARY KEY, display_name text, deleted_at timestamptz,
			display_name_norm text GENERATED ALWAYS AS (lower(normalize(display_name, NFKC))) STORED);
		CREATE TABLE catalog_label_alias (label_id bigint, name_norm text);
		CREATE TABLE catalog_release (id bigint PRIMARY KEY, deleted_at timestamptz);`
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ddl); err != nil {
		t.Fatalf("legacy stand-in: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	was := identity.LegacySchema
	identity.LegacySchema = schema
	t.Cleanup(func() {
		identity.LegacySchema = was
		_, _ = db.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE`)
	})
}
