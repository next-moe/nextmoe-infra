//go:build ignore

// CANON_DEV_DSN='host=localhost user=postgres dbname=<empty db> sslmode=disable' go run -mod=mod migrategen.go <name>
package main

import (
	"context"
	"database/sql"
	"log"
	"net/url"
	"os"
	"strings"

	"api/internal/canon/ent/migrate"
	"api/internal/canon/idseq"

	"ariga.io/atlas/sql/sqltool"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql/schema"
	"github.com/jackc/pgx/v5/stdlib"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: migrategen <name>")
	}
	dsn := os.Getenv("CANON_DEV_DSN")
	if dsn == "" {
		log.Fatal("CANON_DEV_DSN must name an empty dev database")
	}
	ctx := context.Background()
	sql.Register("postgres", stdlib.GetDefaultDriver())
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal(err)
	}
	stmts := []string{"CREATE SCHEMA IF NOT EXISTS canon", "CREATE SCHEMA IF NOT EXISTS " + idseq.Schema}
	for _, f := range idseq.Families {
		stmts = append(stmts, "CREATE SEQUENCE IF NOT EXISTS "+idseq.QualifiedSequence(f)+" AS bigint")
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			log.Fatal(err)
		}
	}
	db.Close()

	dir, err := sqltool.NewGooseDir("../migrations")
	if err != nil {
		log.Fatal(err)
	}
	if err := migrate.NamedDiff(ctx, devURL(dsn), os.Args[1],
		schema.WithDir(dir),
		schema.WithMigrationMode(schema.ModeReplay),
		schema.WithDialect(dialect.Postgres),
		schema.WithFormatter(sqltool.GooseFormatter),
	); err != nil {
		log.Fatal(err)
	}
}

func devURL(dsn string) string {
	kv := map[string]string{}
	for _, f := range strings.Fields(dsn) {
		k, v, _ := strings.Cut(f, "=")
		kv[k] = v
	}
	u := url.URL{Scheme: "postgres", User: url.User(kv["user"]), Host: kv["host"] + ":" + orDefault(kv["port"], "5432"), Path: "/" + kv["dbname"]}
	q := url.Values{"search_path": {"canon"}, "sslmode": {orDefault(kv["sslmode"], "disable")}}
	u.RawQuery = q.Encode()
	return u.String()
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
