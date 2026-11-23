package perm

import (
	"context"
	"database/sql"
	"testing"

	"api/internal/platform/authz"
	"api/internal/testsupport/dbtest"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestRefreshReadsTheConsoleRows(t *testing.T) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn+" search_path=canon_perm_test")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{
		`DROP SCHEMA IF EXISTS canon_perm_test CASCADE`,
		`CREATE SCHEMA canon_perm_test`,
		`CREATE TABLE role_permission_overrides (id serial PRIMARY KEY, role varchar(32) NOT NULL, permission varchar(128) NOT NULL,
			effect varchar(8) NOT NULL, granted_by_user_id bigint NOT NULL, created_at timestamptz)`,
		`INSERT INTO role_permission_overrides (role, permission, effect, granted_by_user_id) VALUES
			('editor', 'canon.todo.value', 'grant', 1), ('moderator', 'canon.takedown', 'deny', 1), ('editor', 'catalog.review', 'grant', 1)`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		Holder.Swap(authz.NewResolver(Bundles))
		_, _ = db.ExecContext(ctx, `DROP SCHEMA canon_perm_test CASCADE`)
	})
	if err := Refresh(ctx, db); err != nil {
		t.Fatal(err)
	}
	if !Holder.Can([]string{"editor"}, TodoValue) || Holder.Can([]string{"moderator"}, Takedown) || !Holder.Can([]string{"moderator"}, Merge) {
		t.Errorf("holder after refresh: editor value %v, moderator takedown %v",
			Holder.Can([]string{"editor"}, TodoValue), Holder.Can([]string{"moderator"}, Takedown))
	}
}
