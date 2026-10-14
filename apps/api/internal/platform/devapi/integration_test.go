package devapi

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	authModel "api/internal/platform/auth/model"
	siteModel "api/internal/platform/site/model"
	storeModel "api/internal/platform/store/model"
	"api/internal/testsupport/dbtest"

	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

const suiteLockKey = 0x64766170

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("devapi")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("devapi", "cannot connect to test database: %v", err)
	}
	sqlDB, _ := db.DB()
	release := acquireSuiteLock(sqlDB)

	if err := provision(db); err != nil {
		release()
		dbtest.SkipMainf("devapi", "devapi migration failed: %v", err)
	}
	if err := provision(db); err != nil {
		release()
		fmt.Fprintf(os.Stderr, "devapi migration is NOT idempotent: %v\n", err)
		os.Exit(1)
	}

	testDB = db
	code := m.Run()
	release()
	os.Exit(code)
}

func provision(db *gorm.DB) error {
	if err := AddOAuthClientDevColumns(db); err != nil {
		return err
	}
	if err := AddUsagePathColumn(db); err != nil {
		return err
	}
	// sessions / authorization_codes / the two store link tables are not devapi
	// models, but AdminService.DeleteApp counts rows in them before it removes
	// an oauth_clients row. Without them here the guard could only ever be
	// tested against tables that do not exist, which is the shape of a passing
	// test that proves nothing.
	models := []any{
		&siteModel.Site{}, &siteModel.OAuthClient{},
		&authModel.Session{}, &authModel.AuthorizationCode{},
		&storeModel.PurchaseLink{}, &storeModel.CouponLink{},
	}
	models = append(models, Models()...)
	return db.AutoMigrate(models...)
}

func TestFreshDatabaseMigrationOrder(t *testing.T) {
	tx := testDB.Begin()
	if tx.Error != nil {
		t.Fatalf("begin fresh-schema transaction: %v", tx.Error)
	}
	defer tx.Rollback()

	if err := tx.Exec(`CREATE SCHEMA devapi_fresh_migration_test`).Error; err != nil {
		t.Fatalf("create fresh schema: %v", err)
	}
	if err := tx.Exec(`SET LOCAL search_path TO devapi_fresh_migration_test`).Error; err != nil {
		t.Fatalf("select fresh schema: %v", err)
	}

	if tx.Migrator().HasTable("oauth_clients") {
		t.Fatal("fresh schema unexpectedly contains oauth_clients")
	}
	if err := AddOAuthClientDevColumns(tx); err != nil {
		t.Fatalf("pre-migrate fresh schema: %v", err)
	}
	if tx.Migrator().HasTable("oauth_clients") {
		t.Fatal("pre-migration must leave fresh table creation to AutoMigrate")
	}

	if err := tx.AutoMigrate(&siteModel.Site{}, &siteModel.OAuthClient{}); err != nil {
		t.Fatalf("auto-migrate fresh schema: %v", err)
	}
	if !tx.Migrator().HasColumn(&siteModel.OAuthClient{}, "dev_enabled") {
		t.Fatal("fresh oauth_clients table is missing dev_enabled")
	}
	if err := AddOAuthClientDevColumns(tx); err != nil {
		t.Fatalf("re-run pre-migration on created table: %v", err)
	}
}

func acquireSuiteLock(db *sql.DB) func() {
	if db == nil {
		return func() {}
	}
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return func() {}
	}
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", suiteLockKey); err != nil {
		_ = conn.Close()
		return func() {}
	}
	return func() {
		_, _ = conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", suiteLockKey)
		_ = conn.Close()
	}
}

func cleanup(t *testing.T) {
	t.Helper()
	if err := testDB.Exec(`TRUNCATE developer_api_keys, developer_api_usage RESTART IDENTITY`).Error; err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if err := testDB.Exec(`DELETE FROM oauth_clients WHERE id LIKE 'devapitest_%'`).Error; err != nil {
		t.Fatalf("clean clients: %v", err)
	}
}

func makeApp(t *testing.T, clientID string, devEnabled bool) {
	t.Helper()
	app := &siteModel.OAuthClient{
		ID:            clientID,
		Name:          "devapi test app",
		Secret:        "x",
		RedirectURIs:  datatypes.JSON([]byte("[]")),
		Grants:        datatypes.JSON([]byte(`["authorization_code"]`)),
		AllowedScopes: datatypes.JSON([]byte(`["catalog:read","galgame:read"]`)),
		DevEnabled:    devEnabled,
		DevTier:       TierFree,
	}
	if err := testDB.Create(app).Error; err != nil {
		t.Fatalf("create app: %v", err)
	}
}

func newService(t *testing.T) (*AdminService, *Repository) {
	t.Helper()
	repo := NewRepository(testDB)
	return NewAdminService(repo, newMemStore()), repo
}

func columnDefault(t *testing.T, table, col string) *string {
	t.Helper()
	var def *string
	if err := testDB.Raw(
		`SELECT column_default FROM information_schema.columns
		   WHERE table_schema='public' AND table_name=? AND column_name=?`, table, col,
	).Scan(&def).Error; err != nil {
		t.Fatalf("read default %s.%s: %v", table, col, err)
	}
	return def
}

func TestDevColumnDiscipline(t *testing.T) {
	for _, col := range []string{"dev_enabled", "dev_tier", "dev_rate_per_min", "dev_quota_daily"} {
		if def := columnDefault(t, "oauth_clients", col); def != nil {
			t.Errorf("oauth_clients.%s must have NO default (intent column), got %q", col, *def)
		}
	}
}

// The retired NSFW capability left two NOT NULL columns behind that no Go
// struct writes any more, so the migration keeps a DEFAULT on them instead of
// dropping it. Both halves are rebuilt in their pre-retirement shape here
// rather than read off the live schema: a fresh database has neither column at
// all (AutoMigrate builds both tables from models that no longer declare them),
// so an unguarded read passes vacuously.
func TestRetiredNSFWColumnsKeepTheirDefault(t *testing.T) {
	cleanup(t)
	tx := testDB.Begin()
	if tx.Error != nil {
		t.Fatalf("begin: %v", tx.Error)
	}
	defer tx.Rollback()

	for _, s := range []string{
		`ALTER TABLE oauth_clients DROP COLUMN IF EXISTS dev_nsfw_allowed`,
		`ALTER TABLE oauth_clients ADD COLUMN dev_nsfw_allowed boolean NOT NULL DEFAULT false`,
		`ALTER TABLE oauth_clients ALTER COLUMN dev_nsfw_allowed DROP DEFAULT`,
		`ALTER TABLE developer_api_keys DROP COLUMN IF EXISTS nsfw_allowed`,
		`ALTER TABLE developer_api_keys ADD COLUMN nsfw_allowed boolean NOT NULL DEFAULT false`,
		`ALTER TABLE developer_api_keys ALTER COLUMN nsfw_allowed DROP DEFAULT`,
	} {
		if err := tx.Exec(s).Error; err != nil {
			t.Fatalf("rebuild the pre-retirement shape %q: %v", s, err)
		}
	}

	// Negative control: without the migration the writers that no longer know
	// the column are refused outright, which is what makes the default
	// load-bearing. The failing statement aborts the transaction, so it runs
	// inside a savepoint — without one every later query is 25P02 instead.
	if err := tx.SavePoint("before_nsfw_default").Error; err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	if err := tx.Exec(`INSERT INTO oauth_clients (id, name, secret, redirect_uris, grants, dev_enabled, dev_tier, dev_rate_per_min, dev_quota_daily, dev_review_status)
		VALUES ('devapitest_nsfwdefault', 'x', 'x', '[]'::jsonb, '[]'::jsonb, false, 'free', 0, 0, 'approved')`).Error; err == nil {
		t.Fatal("insert without dev_nsfw_allowed succeeded before the migration ran")
	}
	if err := tx.RollbackTo("before_nsfw_default").Error; err != nil {
		t.Fatalf("rollback to savepoint: %v", err)
	}

	if err := AddOAuthClientDevColumns(tx); err != nil {
		t.Fatalf("add columns: %v", err)
	}
	if err := RestoreKeyNSFWDefault(tx); err != nil {
		t.Fatalf("restore key default: %v", err)
	}
	if err := RestoreKeyNSFWDefault(tx); err != nil {
		t.Fatalf("restore key default is not idempotent: %v", err)
	}

	for _, tc := range []struct{ table, col string }{
		{"oauth_clients", "dev_nsfw_allowed"},
		{"developer_api_keys", "nsfw_allowed"},
	} {
		var def *string
		if err := tx.Raw(
			`SELECT column_default FROM information_schema.columns
			   WHERE table_schema=current_schema() AND table_name=? AND column_name=?`, tc.table, tc.col,
		).Scan(&def).Error; err != nil {
			t.Fatalf("read default %s.%s: %v", tc.table, tc.col, err)
		}
		if def == nil {
			t.Errorf("%s.%s has no writer left; it must keep a default", tc.table, tc.col)
			continue
		}
		if !strings.HasPrefix(*def, "false") {
			t.Errorf("%s.%s default = %q, want false", tc.table, tc.col, *def)
		}
	}

	app := &siteModel.OAuthClient{
		ID: "devapitest_nsfwdefault", Name: "x", Secret: "x",
		RedirectURIs: datatypes.JSON([]byte("[]")),
		Grants:       datatypes.JSON([]byte("[]")),
		DevTier:      TierFree,
	}
	if err := tx.Create(app).Error; err != nil {
		t.Fatalf("create an application the model can no longer describe fully: %v", err)
	}
	key := &DeveloperAPIKey{
		ClientID: app.ID, Name: "k", KeyHash: "h", KeyPrefix: "nmk_test_", Last4: "abcd",
		Scopes: datatypes.JSON([]byte(`["catalog:read"]`)), CreatedByUserID: 1,
	}
	if err := tx.Create(key).Error; err != nil {
		t.Fatalf("mint a key against the retired column: %v", err)
	}
}

func TestUsageUniqueIndex(t *testing.T) {
	var def string
	if err := testDB.Raw(
		`SELECT indexdef FROM pg_indexes WHERE schemaname='public' AND indexname='idx_usage_day'`,
	).Scan(&def).Error; err != nil {
		t.Fatalf("read indexdef: %v", err)
	}
	if def == "" {
		t.Fatal("idx_usage_day does not exist")
	}
	if !strings.Contains(def, "UNIQUE") {
		t.Errorf("idx_usage_day is not unique: %s", def)
	}
	// The upsert's conflict target in UpsertUsage must be exactly this tuple.
	// A narrower target is not a compile error and not a wrong-looking query —
	// it is a 23505 that rolls back the whole batched INSERT at runtime.
	if !strings.Contains(def, "(client_id, key_id, face, day, path)") {
		t.Errorf("idx_usage_day columns/order wrong: %s", def)
	}
}

func TestKeyLifecycle(t *testing.T) {
	cleanup(t)
	const clientID = "devapitest_lifecycle"
	makeApp(t, clientID, true)
	svc, repo := newService(t)
	ctx := context.Background()
	now := time.Now()

	key, plaintext, err := svc.MintKey(ctx, clientID, MintKeyInput{Name: "k1"}, 42)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if plaintext == "" || !HasKeyPrefix(plaintext) {
		t.Fatalf("mint returned bad plaintext %q", plaintext)
	}
	cred, err := repo.ResolveByHash(ctx, HashKey(plaintext), now)
	if err != nil || cred == nil {
		t.Fatalf("resolve after mint = (%+v, %v), want active", cred, err)
	}
	if cred.KeyID != key.ID || cred.ClientID != clientID || cred.Tier != TierFree {
		t.Errorf("resolved cred mismatch: %+v", cred)
	}
	if !cred.HasScope(ScopeCatalogRead) || cred.HasScope(ScopeGalgameRead) {
		t.Errorf("resolved scopes = %v, want catalog:read alone (galgame:read retired at wave 146)", cred.Scopes)
	}

	newKey, newPlain, err := svc.RotateKey(ctx, key.ID, 42)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if newPlain == plaintext {
		t.Fatalf("rotate returned the same plaintext")
	}
	if c, _ := repo.ResolveByHash(ctx, HashKey(plaintext), now); c == nil {
		t.Errorf("old key must still resolve during grace window")
	}
	if c, _ := repo.ResolveByHash(ctx, HashKey(newPlain), now); c == nil {
		t.Errorf("new key must resolve after rotate")
	}
	old, _ := repo.GetKey(ctx, key.ID)
	if old.ExpiresAt == nil {
		t.Fatalf("rotated-out key should have an expiry")
	}
	if c, _ := repo.ResolveByHash(ctx, HashKey(plaintext), old.ExpiresAt.Add(time.Second)); c != nil {
		t.Errorf("old key must not resolve after its expiry")
	}

	if err := svc.RevokeKey(ctx, newKey.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if c, _ := repo.ResolveByHash(ctx, HashKey(newPlain), now); c != nil {
		t.Errorf("revoked key must not resolve")
	}
}

func TestEveryTierMintsV2Keys(t *testing.T) {
	cleanup(t)
	svc, repo := newService(t)
	ctx := context.Background()

	for _, tier := range []string{TierFree, TierTrusted, TierInternal} {
		clientID := "devapitest_ga_" + tier
		makeApp(t, clientID, true)
		if err := testDB.Model(&siteModel.OAuthClient{}).Where("id = ?", clientID).
			Update("dev_tier", tier).Error; err != nil {
			t.Fatalf("set tier %s: %v", tier, err)
		}
		_, live, err := svc.MintKey(ctx, clientID, MintKeyInput{Name: "live"}, 1)
		if err != nil {
			t.Fatalf("mint %s: %v", tier, err)
		}
		if !strings.HasPrefix(live, V2LivePrefix) || !ValidV2Key(live) {
			t.Errorf("tier %s minted %q, want a valid %s key", tier, live[:min(len(live), 9)], V2LivePrefix)
		}
		_, test, err := svc.MintKey(ctx, clientID, MintKeyInput{Name: "test", Test: true}, 1)
		if err != nil {
			t.Fatalf("mint test %s: %v", tier, err)
		}
		if !strings.HasPrefix(test, V2TestPrefix) || !ValidV2Key(test) {
			t.Errorf("tier %s minted %q, want a valid %s key", tier, test[:min(len(test), 9)], V2TestPrefix)
		}
	}

	const legacyClient = "devapitest_ga_legacy"
	makeApp(t, legacyClient, true)
	raw, err := GenerateKey(LivePrefix)
	if err != nil {
		t.Fatalf("generate v1 key: %v", err)
	}
	kp, last4 := KeyMetadata(raw)
	legacy := &DeveloperAPIKey{
		ClientID: legacyClient, Name: "legacy", KeyHash: HashKey(raw),
		KeyPrefix: kp, Last4: last4,
		Scopes: datatypes.JSON([]byte(`["catalog:read"]`)), CreatedByUserID: 1,
	}
	if err := repo.CreateKey(ctx, legacy); err != nil {
		t.Fatalf("create legacy key: %v", err)
	}
	_, rotated, err := svc.RotateKey(ctx, legacy.ID, 1)
	if err != nil {
		t.Fatalf("rotate legacy: %v", err)
	}
	if !IsV2KeyPrefix(rotated) || !ValidV2Key(rotated) {
		t.Errorf("rotating a v1 key returned %q, want a valid nmk_ key", rotated[:min(len(rotated), 9)])
	}
}

func TestRevokeBustsCache(t *testing.T) {
	cleanup(t)
	const clientID = "devapitest_bust"
	makeApp(t, clientID, true)
	store := newMemStore()
	svc := NewAdminService(NewRepository(testDB), store)
	ctx := context.Background()

	key, plaintext, err := svc.MintKey(ctx, clientID, MintKeyInput{Name: "k"}, 1)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	cacheKey := "devkey:" + hashHex(plaintext)
	store.kv[cacheKey] = []byte("cached")

	if err := svc.RevokeKey(ctx, key.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, present := store.kv[cacheKey]; present {
		t.Errorf("revoke must actively delete the resolve-cache entry %q", cacheKey)
	}
}

func TestDevEnabledGate(t *testing.T) {
	cleanup(t)
	const clientID = "devapitest_disabled"
	makeApp(t, clientID, false)
	svc, repo := newService(t)
	ctx := context.Background()

	_, plaintext, err := svc.MintKey(ctx, clientID, MintKeyInput{Name: "k"}, 1)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if c, _ := repo.ResolveByHash(ctx, HashKey(plaintext), time.Now()); c != nil {
		t.Errorf("key on a dev_enabled=false app must not resolve, got %+v", c)
	}

	if _, err := svc.UpdateAppConfig(ctx, clientID, AppConfig{DevEnabled: boolPtr(true)}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if c, _ := repo.ResolveByHash(ctx, HashKey(plaintext), time.Now()); c == nil {
		t.Errorf("key must resolve once the app is enabled")
	}
}

func TestUsageFlushIdempotent(t *testing.T) {
	cleanup(t)
	repo := NewRepository(testDB)
	rec := NewUsageRecorder(repo, newMemStore())
	ctx := context.Background()
	cred := &Credential{KeyID: 100, ClientID: "devapitest_usage"}

	const path = "/v1/catalog/works/:id"
	rec.Record(cred, "catalog", path, 200)
	rec.Record(cred, "catalog", path, 200)
	rec.Record(cred, "catalog", path, 404)
	if err := rec.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	assertUsage(t, cred.ClientID, cred.KeyID, "catalog", path, 3, 1, 0)

	if err := rec.Flush(ctx); err != nil {
		t.Fatalf("re-flush: %v", err)
	}
	assertUsage(t, cred.ClientID, cred.KeyID, "catalog", path, 3, 1, 0)

	rec.Record(cred, "catalog", path, 500)
	rec.Record(cred, "catalog", path, 200)
	if err := rec.Flush(ctx); err != nil {
		t.Fatalf("flush 2: %v", err)
	}
	assertUsage(t, cred.ClientID, cred.KeyID, "catalog", path, 5, 1, 1)
}

// Two route patterns of the same face, key and day are two rows, and the batch
// that writes them both must survive: a conflict target narrower than
// idx_usage_day would take the second row for a duplicate and abort the INSERT.
func TestUsageSplitsByRoutePattern(t *testing.T) {
	cleanup(t)
	repo := NewRepository(testDB)
	rec := NewUsageRecorder(repo, newMemStore())
	ctx := context.Background()
	cred := &Credential{KeyID: 101, ClientID: "devapitest_usage_path"}

	rec.Record(cred, "catalog", "/v1/catalog/works/:id", 200)
	rec.Record(cred, "catalog", "/v1/catalog/works/:id", 200)
	rec.Record(cred, "catalog", "/v1/catalog/search", 200)
	rec.Record(cred, "catalog", "/v1/catalog/search", 404)
	if err := rec.Flush(ctx); err != nil {
		t.Fatalf("flush two patterns in one batch: %v", err)
	}
	assertUsage(t, cred.ClientID, cred.KeyID, "catalog", "/v1/catalog/works/:id", 2, 0, 0)
	assertUsage(t, cred.ClientID, cred.KeyID, "catalog", "/v1/catalog/search", 2, 1, 0)

	// A second batch on both patterns must accumulate, not collide.
	rec.Record(cred, "catalog", "/v1/catalog/works/:id", 500)
	rec.Record(cred, "catalog", "/v1/catalog/search", 200)
	if err := rec.Flush(ctx); err != nil {
		t.Fatalf("second flush: %v", err)
	}
	assertUsage(t, cred.ClientID, cred.KeyID, "catalog", "/v1/catalog/works/:id", 3, 0, 1)
	assertUsage(t, cred.ClientID, cred.KeyID, "catalog", "/v1/catalog/search", 3, 1, 0)

	// The face-level rollup the developer portal reads must still see one
	// catalog total, summed across the patterns.
	faces, err := repo.SumUsageByFace(ctx, []string{cred.ClientID}, "1970-01-01")
	if err != nil {
		t.Fatalf("SumUsageByFace: %v", err)
	}
	if len(faces) != 1 || faces[0].Face != "catalog" || faces[0].Count != 6 || faces[0].Status4xx != 1 || faces[0].Status5xx != 1 {
		t.Fatalf("by-face rollup = %+v (want one catalog row, count 6, 4xx 1, 5xx 1)", faces)
	}
}

func assertUsage(t *testing.T, clientID string, keyID uint, face, path string, count, s4xx, s5xx int64) {
	t.Helper()
	var row DeveloperAPIUsage
	err := testDB.Where("client_id = ? AND key_id = ? AND face = ? AND path = ?", clientID, keyID, face, path).
		Take(&row).Error
	if err != nil {
		t.Fatalf("read usage: %v", err)
	}
	if row.Count != count || row.Status4xx != s4xx || row.Status5xx != s5xx {
		t.Errorf("usage[%s] = (count %d, 4xx %d, 5xx %d), want (%d, %d, %d)",
			path, row.Count, row.Status4xx, row.Status5xx, count, s4xx, s5xx)
	}
}

func TestAddDevColumnsBackfillsExisting(t *testing.T) {
	// cleanupSelf, not cleanup: this test drops owner_user_id, and any
	// owner-scoped row still present comes back with a NULL owner that no
	// later cleanup can find — which is how a self-service row from an earlier
	// test file turned up in the admin app list two tests later.
	cleanupSelf(t)
	flushPool := func() {
		sqlDB, err := testDB.DB()
		if err != nil {
			t.Fatalf("flush pool: %v", err)
		}
		sqlDB.SetMaxIdleConns(0)
		sqlDB.SetMaxIdleConns(2)
	}
	defer flushPool()
	for _, col := range []string{"dev_enabled", "dev_tier", "dev_nsfw_allowed", "dev_rate_per_min", "dev_quota_daily", "owner_user_id", "dev_review_status", "dev_review_note"} {
		if err := testDB.Exec("ALTER TABLE oauth_clients DROP COLUMN IF EXISTS " + col).Error; err != nil {
			t.Fatalf("drop %s: %v", col, err)
		}
	}
	if err := testDB.Exec(`INSERT INTO oauth_clients (id, name, secret, redirect_uris, grants)
		VALUES ('devapitest_legacy', 'legacy', 'x', '[]'::jsonb, '[]'::jsonb)`).Error; err != nil {
		t.Fatalf("insert legacy: %v", err)
	}

	if err := AddOAuthClientDevColumns(testDB); err != nil {
		t.Fatalf("add columns: %v", err)
	}
	if err := AddOAuthClientDevColumns(testDB); err != nil {
		t.Fatalf("add columns not idempotent: %v", err)
	}
	flushPool()

	var app siteModel.OAuthClient
	if err := testDB.Where("id = ?", "devapitest_legacy").Take(&app).Error; err != nil {
		t.Fatalf("read legacy: %v", err)
	}
	if app.DevEnabled != false || app.DevTier != "free" || app.DevRatePerMin != 0 || app.DevQuotaDaily != 0 {
		t.Errorf("legacy backfill wrong: enabled=%v tier=%q rate=%d quota=%d",
			app.DevEnabled, app.DevTier, app.DevRatePerMin, app.DevQuotaDaily)
	}
	// Every row that predates the approval flow was created when creation was
	// unconditionally self-service; backfilling anything but approved would
	// retroactively suspend live applications.
	if app.DevReviewStatus != AppReviewApproved || app.DevReviewNote != "" {
		t.Errorf("legacy review backfill wrong: status=%q note=%q", app.DevReviewStatus, app.DevReviewNote)
	}
	if appAwaitsReview(app.DevReviewStatus) {
		t.Error("a backfilled row must not be treated as awaiting review")
	}

	var def *string
	if err := testDB.Raw(
		`SELECT column_default FROM information_schema.columns
		   WHERE table_schema='public' AND table_name='oauth_clients' AND column_name='dev_enabled'`,
	).Scan(&def).Error; err != nil {
		t.Fatalf("read default: %v", err)
	}
	if def != nil {
		t.Errorf("dev_enabled default must be dropped, got %q", *def)
	}
}

// Rebuilds the pre-path shape (no path column, 4-column idx_usage_day) with a
// row in it, then runs the migration the way cmd/migrate does — raw SQL first,
// AutoMigrate second — and checks the three things that can go wrong: the
// populated table takes a NOT NULL column, the existing row backfills to the
// empty string, and the unique index really widened (otherwise the two patterns
// of one face collide on 23505 and take the whole batch down).
func TestAddUsagePathColumnWidensExisting(t *testing.T) {
	cleanup(t)
	if err := testDB.Exec(`DROP INDEX IF EXISTS idx_usage_day`).Error; err != nil {
		t.Fatalf("drop index: %v", err)
	}
	if err := testDB.Exec(`ALTER TABLE developer_api_usage DROP COLUMN IF EXISTS path`).Error; err != nil {
		t.Fatalf("drop path: %v", err)
	}
	if err := testDB.Exec(`CREATE UNIQUE INDEX idx_usage_day
		ON developer_api_usage (client_id, key_id, face, day)`).Error; err != nil {
		t.Fatalf("recreate the pre-path index: %v", err)
	}
	if err := testDB.Exec(`INSERT INTO developer_api_usage
		(client_id, key_id, face, day, count, status_4xx, status_5xx, updated_at)
		VALUES ('devapitest_prepath', 9, 'catalog', '2026-08-01', 7, 1, 0, now())`).Error; err != nil {
		t.Fatalf("insert pre-path row: %v", err)
	}

	if err := provision(testDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := provision(testDB); err != nil {
		t.Fatalf("migration is not idempotent: %v", err)
	}

	var row DeveloperAPIUsage
	if err := testDB.Where("client_id = ?", "devapitest_prepath").Take(&row).Error; err != nil {
		t.Fatalf("read backfilled row: %v", err)
	}
	if row.Path != "" || row.Count != 7 {
		t.Errorf("pre-path row = (path %q, count %d), want ('', 7)", row.Path, row.Count)
	}

	var def *string
	if err := testDB.Raw(`SELECT column_default FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'developer_api_usage' AND column_name = 'path'`).
		Scan(&def).Error; err != nil {
		t.Fatalf("read default: %v", err)
	}
	if def != nil {
		t.Errorf("path default must be dropped, got %q", *def)
	}

	var indexdef string
	if err := testDB.Raw(`SELECT indexdef FROM pg_indexes
		WHERE schemaname = current_schema() AND tablename = 'developer_api_usage' AND indexname = 'idx_usage_day'`).
		Scan(&indexdef).Error; err != nil {
		t.Fatalf("read indexdef: %v", err)
	}
	if !strings.Contains(indexdef, "path") {
		t.Fatalf("idx_usage_day = %q, want the path column in it", indexdef)
	}

	// The proof the widened index is what the DB enforces, not just what the
	// tag says: same client/key/face/day, two patterns, both must land.
	for _, p := range []string{"/v1/catalog/works/:id", "/v1/catalog/search"} {
		if err := testDB.Exec(`INSERT INTO developer_api_usage
			(client_id, key_id, face, day, path, count, status_4xx, status_5xx, updated_at)
			VALUES ('devapitest_prepath', 9, 'catalog', '2026-08-02', ?, 1, 0, 0, now())`, p).Error; err != nil {
			t.Fatalf("insert %s: %v", p, err)
		}
	}
}

func boolPtr(b bool) *bool { return &b }

// Disabling an app is a revocation of every key on it: ResolveByHash refuses
// on !dev_enabled. The cached credential carries dev_enabled, so without a
// bust the app's keys went on working for the 60s positive-cache TTL.
func TestDisablingAnAppBustsItsCache(t *testing.T) {
	cleanup(t)
	const clientID = "devapitest_appbust"
	makeApp(t, clientID, true)
	store := newMemStore()
	svc := NewAdminService(NewRepository(testDB), store)
	mw := NewMiddleware(NewRepository(testDB), store)
	ctx := context.Background()

	_, plaintext, err := svc.MintKey(ctx, clientID, MintKeyInput{Name: "k"}, 1)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	cred, err := mw.Lookup(ctx, plaintext)
	if err != nil || cred == nil {
		t.Fatalf("positive control: a live key must resolve, got %+v %v", cred, err)
	}
	if _, cached := store.kv[credCacheKey(hashHex(plaintext))]; !cached {
		t.Fatal("positive control: the resolve must have cached something to bust")
	}

	if _, err := svc.UpdateAppConfig(ctx, clientID, AppConfig{DevEnabled: boolPtr(false)}); err != nil {
		t.Fatalf("disable app: %v", err)
	}
	cred, err = mw.Lookup(ctx, plaintext)
	if err != nil {
		t.Fatalf("lookup after disable: %v", err)
	}
	if cred != nil {
		t.Errorf("a key on a disabled app must stop resolving now, not in 60s: %+v", cred)
	}
}

// A tier change is a limit change, and the tier rides in the same cached blob.
func TestTighteningATierBustsItsCache(t *testing.T) {
	cleanup(t)
	const clientID = "devapitest_tierbust"
	makeApp(t, clientID, true)
	store := newMemStore()
	svc := NewAdminService(NewRepository(testDB), store)
	mw := NewMiddleware(NewRepository(testDB), store)
	ctx := context.Background()

	_, plaintext, err := svc.MintKey(ctx, clientID, MintKeyInput{Name: "k"}, 1)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if _, err := mw.Lookup(ctx, plaintext); err != nil {
		t.Fatalf("warm the cache: %v", err)
	}
	trusted := TierTrusted
	if _, err := svc.UpdateAppConfig(ctx, clientID, AppConfig{DevTier: &trusted}); err != nil {
		t.Fatalf("retier: %v", err)
	}
	cred, err := mw.Lookup(ctx, plaintext)
	if err != nil || cred == nil {
		t.Fatalf("lookup after retier: %+v %v", cred, err)
	}
	if cred.Tier != TierTrusted {
		t.Errorf("the cached tier must not outlive the change: got %q", cred.Tier)
	}
}
