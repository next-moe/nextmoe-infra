package service

import (
	"context"
	"os"
	"testing"

	suitelock "api/internal/platform/trust/dbtest"
	"api/internal/platform/trust/migrate"
	"api/internal/platform/trust/model"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("trust/service")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("trust/service", "cannot connect to test database: %v", err)
	}
	sqlDB, _ := db.DB()
	release := suitelock.AcquireSuiteLock(sqlDB)

	if err := migrate.Run(db); err != nil {
		release()
		dbtest.SkipMainf("trust/service", "trust migration failed: %v", err)
	}
	testDB = db
	code := m.Run()
	release()
	os.Exit(code)
}

func cleanTables(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"trust_audit_log", "trust_disposition", "trust_report",
		"trust_review_item", "trust_subject_kind", "trust_scan_result",
		"trust_term", "trust_site_policy",
	} {
		if err := testDB.Exec("TRUNCATE " + table + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
}

type fakeWeigher struct {
	weights map[int64]ReporterWeight
	def     ReporterWeight
}

func newWeigher() *fakeWeigher {
	return &fakeWeigher{weights: map[int64]ReporterWeight{}, def: ReporterWeight{Weight: 1.0}}
}

func (f *fakeWeigher) Weigh(_ context.Context, reporterID int64) (ReporterWeight, error) {
	if w, ok := f.weights[reporterID]; ok {
		return w, nil
	}
	return f.def, nil
}

func (f *fakeWeigher) set(reporterID int64, w ReporterWeight) { f.weights[reporterID] = w }

func registerKind(t *testing.T, site, key string, callbackURL, secret *string) {
	t.Helper()
	kind := model.TrustSubjectKind{Site: site, Key: key, CallbackURL: callbackURL, CallbackSecret: secret, IsDeprecated: false}
	if err := testDB.Create(&kind).Error; err != nil {
		t.Fatalf("register kind %s/%s: %v", site, key, err)
	}
}

func countReports(t *testing.T, site, kind, subject string) int64 {
	t.Helper()
	var n int64
	if err := testDB.Model(&model.TrustReport{}).
		Where("site = ? AND subject_kind = ? AND subject_id = ?", site, kind, subject).
		Count(&n).Error; err != nil {
		t.Fatalf("count reports: %v", err)
	}
	return n
}

func countOpenItems(t *testing.T, site, kind, subject string) int64 {
	t.Helper()
	var n int64
	if err := testDB.Model(&model.TrustReviewItem{}).
		Where("site = ? AND subject_kind = ? AND subject_id = ? AND status IN ?",
			site, kind, subject, []int16{model.ReviewStatusPending, model.ReviewStatusClaimed}).
		Count(&n).Error; err != nil {
		t.Fatalf("count open items: %v", err)
	}
	return n
}

func getItem(t *testing.T, id int64) *model.TrustReviewItem {
	t.Helper()
	var it model.TrustReviewItem
	if err := testDB.Take(&it, id).Error; err != nil {
		t.Fatalf("reload item %d: %v", id, err)
	}
	return &it
}
