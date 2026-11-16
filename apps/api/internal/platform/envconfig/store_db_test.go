package envconfig

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"api/internal/testsupport/dbtest"
	"api/pkg/config"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	if dsn, ok := dbtest.DSN(); ok {
		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: cannot open the assigned test database: %v\n", err)
			os.Exit(1)
		}
		if err := db.AutoMigrate(&ConfigSecret{}, &ConfigSecretAuditLog{}, &BootSnapshot{}); err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: migrate: %v\n", err)
			os.Exit(1)
		}
		if err := db.Exec(`CREATE TABLE IF NOT EXISTS users (
			id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL DEFAULT '',
			email TEXT NOT NULL DEFAULT '')`).Error; err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: create users stub: %v\n", err)
			os.Exit(1)
		}
		testDB = db
	}
	os.Exit(m.Run())
}

func reset(t *testing.T) *Store {
	t.Helper()
	if testDB == nil {
		dbtest.Skip(t)
	}
	if err := testDB.Exec(`TRUNCATE config_secrets, config_secret_audit_logs, config_boot_snapshots`).Error; err != nil {
		t.Fatal(err)
	}
	return NewStore(testDB)
}

func TestPutVersionsAndAudits(t *testing.T) {
	s := reset(t)
	ctx := context.Background()
	put := func(key, value string, version *int64) error {
		_, err := s.Put(ctx, testKEK, PutInput{ActorID: 1, Action: ActionSet, Key: key, Value: value, ExpectVersion: version})
		return err
	}
	if err := put("JWT_SECRET", "one", nil); err != nil {
		t.Fatal(err)
	}
	stale := int64(0)
	if err := put("JWT_SECRET", "two", &stale); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale version = %v, want conflict", err)
	}
	current := int64(1)
	if err := put("JWT_SECRET", "two", &current); err != nil {
		t.Fatal(err)
	}
	if err := put("KUN_IMAGE_S3_BUCKET", "b", nil); err != nil {
		t.Fatal(err)
	}
	if err := put("KUN_PG_PASSWORD", "x", nil); !errors.Is(err, ErrNotStorable) {
		t.Fatalf("bootstrap key = %v, want ErrNotStorable", err)
	}

	rows, err := s.Secrets(ctx)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows = %v, %v", rows, err)
	}
	for _, r := range rows {
		if r.Key == "JWT_SECRET" && (r.Version != 2 || r.Hint == "" || strings.Contains(string(r.Cipher), "two")) {
			t.Errorf("JWT_SECRET row = %+v", r.ConfigSecret)
		}
	}
	audit, err := s.Audit(ctx, 10)
	if err != nil || len(audit) != 3 {
		t.Fatalf("audit = %v, %v", audit, err)
	}
	for _, a := range audit {
		if a.Key == "JWT_SECRET" && (a.OldValue != "" || a.NewValue != "") {
			t.Errorf("a secret's value reached the audit log: %+v", a.ConfigSecretAuditLog)
		}
		if a.Key == "KUN_IMAGE_S3_BUCKET" && a.NewValue != "b" {
			t.Errorf("a plain value's audit lacks it: %+v", a.ConfigSecretAuditLog)
		}
	}

	if err := s.Delete(ctx, testKEK, 1, "", "JWT_SECRET", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, testKEK, 1, "", "JWT_SECRET", ""); !errors.Is(err, ErrNoRow) {
		t.Fatalf("second delete = %v, want ErrNoRow", err)
	}
}

// A service reads the store at startup through the same database as the console
// writes it, and reports what it resolved; the console then reads that report.
func TestServiceStartReadsTheStoreAndReportsIt(t *testing.T) {
	s := reset(t)
	ctx := context.Background()
	dsn, _ := dbtest.DSN()
	for _, part := range strings.Fields(dsn) {
		k, v, _ := strings.Cut(part, "=")
		env := map[string]string{"host": "KUN_PG_HOST", "port": "KUN_PG_PORT", "user": "KUN_PG_USER", "dbname": "KUN_PG_DATABASE", "sslmode": "KUN_PG_SSLMODE", "password": "KUN_PG_PASSWORD"}[k]
		if env != "" {
			t.Setenv(env, v)
		}
	}
	t.Setenv(config.KEKEnv, base64.StdEncoding.EncodeToString(testKEK))
	t.Setenv("JWT_SECRET", "")
	t.Setenv("KUN_TRUST_CLIENT_SECRET", "")

	for _, in := range []PutInput{
		{Scope: "", Key: "JWT_SECRET", Value: "shared"},
		{Scope: "", Key: "KUN_TRUST_CLIENT_SECRET", Value: "forwarder"},
		{Scope: "chat", Key: "KUN_TRUST_CLIENT_SECRET", Value: "chat-own"},
	} {
		in.Action = ActionImport
		if _, err := s.Put(ctx, testKEK, in); err != nil {
			t.Fatal(err)
		}
	}

	// A DSN without a password (local ~/.pgpass) makes validate() reject the
	// load after the store was read; CI's DSN carries one and the load succeeds.
	if _, err := config.LoadService("chat"); err != nil && !strings.Contains(err.Error(), "KUN_PG_PASSWORD") {
		t.Fatalf("LoadService = %v", err)
	}
	jwt, trust := config.Env("JWT_SECRET", ""), config.Env("KUN_TRUST_CLIENT_SECRET", "")
	if jwt != "shared" || trust != "chat-own" {
		t.Fatalf("chat resolved jwt=%q trust=%q", jwt, trust)
	}

	Report(ctx, testDB, "chat")
	ov, err := NewService(s).Overview(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if !ov.Writable || len(ov.Services) != 1 || ov.Services[0].Name != "chat" || !ov.Services[0].StoreLoaded {
		t.Fatalf("overview header = %+v / %+v", ov, ov.Services)
	}
	for _, v := range ov.Vars {
		if v.Key != "KUN_TRUST_CLIENT_SECRET" {
			continue
		}
		if len(v.Services) != 1 || v.Services[0].Source != config.SourceStore || v.Services[0].Issue != "" {
			t.Errorf("trust secret as chat sees it = %+v", v.Services)
		}
		if len(v.Stored) != 2 {
			t.Errorf("stored rows = %+v", v.Stored)
		}
		return
	}
	t.Fatal("KUN_TRUST_CLIENT_SECRET missing from the overview")
}
