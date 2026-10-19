package editing_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	"api/internal/platform/editing"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	testDB  *gorm.DB
	testCtx = context.Background()
)

const editSuiteLockKey int64 = 0x65647473

func acquireEditSuiteLock(db *sql.DB) func() {
	if db == nil {
		return func() {}
	}
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return func() {}
	}
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", editSuiteLockKey); err != nil {
		_ = conn.Close()
		return func() {}
	}
	return func() {
		_, _ = conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", editSuiteLockKey)
		_ = conn.Close()
	}
}

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("editing")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		dbtest.SkipMainf("editing", "cannot connect to test database: %v", err)
	}
	sqlDB, _ := db.DB()
	release := acquireEditSuiteLock(sqlDB)
	if err := editing.AutoMigrate(db); err != nil {
		dbtest.SkipMainf("editing", "editing migration failed: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS edit_test_widget (
			id           bigint PRIMARY KEY,
			name         text NOT NULL DEFAULT '',
			olang        text NOT NULL DEFAULT 'ja',
			rating       smallint NOT NULL DEFAULT 0,
			open_note    text NOT NULL DEFAULT '',
			trusted_note text NOT NULL DEFAULT '',
			locked_code  text NOT NULL DEFAULT ''
		)`).Error; err != nil {
		dbtest.SkipMainf("editing", "widget table creation failed: %v", err)
	}
	testDB = db
	code := m.Run()
	release()
	os.Exit(code)
}

func cleanTables(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"edit_proposal_amendment", "edit_proposal", "edit_revision", "edit_suppressed_row", "edit_test_widget",
		"edit_policy_override", "edit_policy_override_audit",
	} {
		if err := testDB.Exec("TRUNCATE " + table + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
}

const (
	fName    = "test.widget.name"
	fOLang   = "test.widget.olang"
	fRating  = "test.widget.rating"
	fOpen    = "test.widget.open_note"
	fTrusted = "test.widget.trusted_note"
	fLocked  = "test.widget.locked_code"
	fLegacy  = "test.widget.legacy"

	permPropose = "edit.test.widget"
	permReview  = "edit.test.widget.review"
)

func strp(s string) *string { return &s }

func boolp(b bool) *bool { return &b }

func lockOpenOpenName() []editing.PolicyOverride {
	return []editing.PolicyOverride{
		{EntityType: "test.widget", FieldKey: fOpen, Propose: strp(editing.ProposeLocked), Automerge: strp(editing.AutomergeNever)},
		{EntityType: "test.widget", FieldKey: fName, Propose: strp(editing.ProposeOpen), Automerge: strp(editing.AutomergeAlways)},
	}
}

func ownerReviewsName() []editing.PolicyOverride {
	return []editing.PolicyOverride{
		{EntityType: "test.widget", FieldKey: fName, Propose: strp(editing.ProposeOpen), OwnerReview: boolp(true)},
	}
}

func applyOverrides(t *testing.T, e *editing.Engine, rows []editing.PolicyOverride) {
	t.Helper()
	if skipped := e.Registry().SetPolicyOverrides(rows); len(skipped) > 0 {
		t.Fatalf("overrides rejected: %v", skipped)
	}
}

type widgetRow struct {
	ID          int64  `gorm:"column:id"`
	Name        string `gorm:"column:name"`
	OLang       string `gorm:"column:olang"`
	Rating      int16  `gorm:"column:rating"`
	OpenNote    string `gorm:"column:open_note"`
	TrustedNote string `gorm:"column:trusted_note"`
	LockedCode  string `gorm:"column:locked_code"`
}

func (widgetRow) TableName() string { return "edit_test_widget" }

func stringValidator(v any) error {
	if _, ok := v.(string); !ok {
		return fmt.Errorf("must be a string")
	}
	return nil
}

func ratingValidator(v any) error {
	f, ok := v.(float64)
	if !ok || f != float64(int64(f)) || f < 0 || f > 10 {
		return fmt.Errorf("must be an integer 0..10")
	}
	return nil
}

func widgetApply(column string) editing.ApplyFunc {
	return func(ctx context.Context, tx *gorm.DB, id int64, value any) error {
		if f, ok := value.(float64); ok {
			value = int64(f)
		}
		res := tx.Exec("UPDATE edit_test_widget SET "+column+" = ? WHERE id = ?", value, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return editing.ErrEntityNotFound
		}
		return nil
	}
}

func widgetSpec(db *gorm.DB) editing.EntityTypeSpec {
	deny := func(any) error { return fmt.Errorf("legacy field") }
	denyApply := func(context.Context, *gorm.DB, int64, any) error { return fmt.Errorf("legacy field") }
	return editing.EntityTypeSpec{
		Family: "test",
		Type:   "test.widget",
		LoadSnapshot: func(ctx context.Context, id int64) (map[string]any, error) {
			var w widgetRow
			if err := db.WithContext(ctx).First(&w, id).Error; err != nil {
				if err == gorm.ErrRecordNotFound {
					return nil, editing.ErrEntityNotFound
				}
				return nil, err
			}
			return map[string]any{
				fName: w.Name, fOLang: w.OLang, fRating: int64(w.Rating),
				fOpen: w.OpenNote, fTrusted: w.TrustedNote, fLocked: w.LockedCode,
			}, nil
		},
		DefaultPolicy: editing.Policy{
			Propose:   editing.ProposePerm(permPropose),
			Review:    editing.ReviewPerm(permReview),
			Automerge: editing.AutomergeNever,
		},
		Fields: []editing.FieldSpec{
			{Key: fName, Kind: editing.KindText, DiffHint: editing.DiffHintInline,
				Validate: stringValidator, Apply: widgetApply("name")},
			{Key: fOLang, Kind: editing.KindEnum, DiffHint: editing.DiffHintInline,
				Validate: stringValidator, Apply: widgetApply("olang")},
			{Key: fRating, Kind: editing.KindInt, DiffHint: editing.DiffHintInline,
				Validate: ratingValidator, Apply: widgetApply("rating")},
			{Key: fOpen, Kind: editing.KindText, DiffHint: editing.DiffHintLines,
				Policy:   &editing.Policy{Propose: editing.ProposeOpen, Review: editing.ReviewPerm(permReview), Automerge: editing.AutomergeAlways},
				Validate: stringValidator, Apply: widgetApply("open_note")},
			{Key: fTrusted, Kind: editing.KindText, DiffHint: editing.DiffHintLines,
				Policy:   &editing.Policy{Propose: editing.ProposeOpen, Review: editing.ReviewPerm(permReview), Automerge: editing.AutomergeTrusted},
				Validate: stringValidator, Apply: widgetApply("trusted_note")},
			{Key: fLocked, Kind: editing.KindText, DiffHint: editing.DiffHintInline,
				Policy:   &editing.Policy{Propose: editing.ProposeLocked, Review: editing.ReviewPerm(permReview), Automerge: editing.AutomergeNever},
				Validate: stringValidator, Apply: widgetApply("locked_code")},
			{Key: fLegacy, Kind: editing.KindText, DiffHint: editing.DiffHintInline,
				Deprecated: true, Validate: deny, Apply: denyApply},
		},
	}
}

func newEngine(t *testing.T) *editing.Engine {
	t.Helper()
	cleanTables(t)
	reg := editing.NewRegistry()
	if err := reg.Register(widgetSpec(testDB)); err != nil {
		t.Fatalf("register widget spec: %v", err)
	}
	return editing.NewEngine(testDB, reg)
}

func createWidget(t *testing.T, id int64) {
	t.Helper()
	if err := testDB.Exec("INSERT INTO edit_test_widget (id) VALUES (?)", id).Error; err != nil {
		t.Fatalf("create widget: %v", err)
	}
}

func loadWidget(t *testing.T, id int64) widgetRow {
	t.Helper()
	var w widgetRow
	if err := testDB.First(&w, id).Error; err != nil {
		t.Fatalf("load widget: %v", err)
	}
	return w
}

func actorWith(uid int64, tier int16, perms ...string) editing.PolicyContext {
	set := make(map[string]bool, len(perms))
	for _, p := range perms {
		set[p] = true
	}
	return editing.PolicyContext{
		UserID: uid, Site: "test-site", TrustTier: tier,
		HasPerm: func(key string) bool { return set[key] },
	}
}

func anonActor(uid int64) editing.PolicyContext    { return actorWith(uid, 0) }
func trustedActor(uid int64) editing.PolicyContext { return actorWith(uid, editing.TrustedTier) }
func editorActor(uid int64) editing.PolicyContext  { return actorWith(uid, 0, permPropose) }
func reviewerActor(uid int64) editing.PolicyContext {
	return actorWith(uid, 0, permPropose, permReview)
}
