package migrations

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"api/internal/canon/ent/migrate"
	"api/internal/canon/idseq"
	"api/internal/testsupport/dbtest"

	"ariga.io/atlas/sql/sqltool"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql/schema"
	"github.com/jackc/pgx/v5/stdlib"
)

func TestMigrationsMatchTheEntSchema(t *testing.T) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmts := []string{"DROP SCHEMA IF EXISTS canon_drift CASCADE", "CREATE SCHEMA canon_drift", "CREATE SCHEMA IF NOT EXISTS " + idseq.Schema}
	for _, f := range idseq.Families {
		stmts = append(stmts, "CREATE SEQUENCE IF NOT EXISTS "+idseq.QualifiedSequence(f)+" AS bigint")
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	defer db.ExecContext(ctx, "DROP SCHEMA IF EXISTS canon_drift CASCADE")

	tmp := t.TempDir()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") || e.Name() == "atlas.sum" {
			b, err := os.ReadFile(e.Name())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(tmp, e.Name()), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	dir, err := sqltool.NewGooseDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	registerPostgres()
	if err := migrate.NamedDiff(ctx, devURL(t, dsn), "drift",
		schema.WithDir(dir), schema.WithMigrationMode(schema.ModeReplay),
		schema.WithDialect(dialect.Postgres), schema.WithFormatter(sqltool.GooseFormatter),
	); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range after {
		if strings.HasSuffix(e.Name(), "_drift.sql") {
			b, _ := os.ReadFile(filepath.Join(tmp, e.Name()))
			t.Fatalf("the ent schema has changes no migration carries; run internal/canon/ent/migrategen.go:\n%s", b)
		}
	}
}

func registerPostgres() {
	for _, d := range sql.Drivers() {
		if d == "postgres" {
			return
		}
	}
	sql.Register("postgres", stdlib.GetDefaultDriver())
}

func devURL(t *testing.T, dsn string) string {
	kv := map[string]string{}
	for _, f := range strings.Fields(dsn) {
		k, v, _ := strings.Cut(f, "=")
		kv[k] = v
	}
	port := kv["port"]
	if port == "" {
		port = "5432"
	}
	u := url.URL{Scheme: "postgres", Host: kv["host"] + ":" + port, Path: "/" + kv["dbname"]}
	if kv["password"] != "" {
		u.User = url.UserPassword(kv["user"], kv["password"])
	} else {
		u.User = url.User(kv["user"])
	}
	q := url.Values{"search_path": {"canon_drift"}, "sslmode": {"disable"}}
	if kv["sslmode"] != "" {
		q.Set("sslmode", kv["sslmode"])
	}
	u.RawQuery = q.Encode()
	return u.String()
}
