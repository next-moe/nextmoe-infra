package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
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
		os.Exit(m.Run())
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("cmd/trust-term-judge", "cannot connect to test database: %v", err)
	}
	sqlDB, _ := db.DB()
	release := suitelock.AcquireSuiteLock(sqlDB)
	if err := migrate.Run(db); err != nil {
		release()
		dbtest.SkipMainf("cmd/trust-term-judge", "trust migration failed: %v", err)
	}
	testDB = db
	code := m.Run()
	release()
	os.Exit(code)
}

func seedTerm(t *testing.T, term string, kind, purpose int16, deprecated bool) int64 {
	t.Helper()
	row := model.TrustTerm{TermNorm: term, Kind: kind, Purpose: purpose}
	if err := testDB.Create(&row).Error; err != nil {
		t.Fatalf("seed %q: %v", term, err)
	}
	if deprecated {
		if err := testDB.Model(&row).Update("is_deprecated", true).Error; err != nil {
			t.Fatalf("deprecate %q: %v", term, err)
		}
	}
	return row.ID
}

func TestApply_RetiresOnlyActiveAbuseSuspectTermsUnderTheJudgedSpelling(t *testing.T) {
	if testDB == nil {
		dbtest.Skip(t)
	}
	if err := testDB.Exec("TRUNCATE trust_term, trust_audit_log RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatalf("truncate: %v", err)
	}
	ordinary := seedTerm(t, "第一次", model.TermKindSuspect, model.TermPurposeAbuse, false)
	kept := seedTerm(t, "外送茶", model.TermKindSuspect, model.TermPurposeAbuse, false)
	banned := seedTerm(t, "楼凤", model.TermKindBanned, model.TermPurposeAbuse, false)
	compliance := seedTerm(t, "起爆器", model.TermKindSuspect, model.TermPurposeCompliance, false)
	already := seedTerm(t, "www", model.TermKindSuspect, model.TermPurposeAbuse, true)
	respelled := seedTerm(t, "服务器", model.TermKindSuspect, model.TermPurposeAbuse, false)

	dir := t.TempDir()
	verdicts, backup := filepath.Join(dir, "verdicts.jsonl"), filepath.Join(dir, "backup.json")
	lines := ""
	for _, v := range []verdict{
		{ID: ordinary, Term: "第一次", Verdict: verdictRetire, Category: categoryOrdinary},
		{ID: kept, Term: "外送茶", Verdict: verdictKeep, Category: "solicitation"},
		{ID: banned, Term: "楼凤", Verdict: verdictRetire, Category: categoryOrdinary},
		{ID: compliance, Term: "起爆器", Verdict: verdictRetire, Category: categoryOrdinary},
		{ID: already, Term: "www", Verdict: verdictRetire, Category: categoryOrdinary},
		{ID: respelled, Term: "伺服器", Verdict: verdictRetire, Category: categoryOrdinary},
		{ID: 999999, Term: "ghost", Verdict: verdictRetire, Category: categoryOrdinary},
	} {
		lines += `{"id":` + itoa(v.ID) + `,"term":"` + v.Term + `","verdict":"` + v.Verdict + `","category":"` + v.Category + `"}` + "\n"
	}
	if err := os.WriteFile(verdicts, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := runApply(testDB, verdicts, backup, "test-model", false); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if deprecated(t, ordinary) {
		t.Fatal("a dry run wrote to the lexicon")
	}

	if err := runApply(testDB, verdicts, backup, "test-model", true); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !deprecated(t, ordinary) {
		t.Fatal("the retired term is still active")
	}
	for name, id := range map[string]int64{"kept": kept, "banned": banned, "compliance": compliance, "respelled": respelled} {
		if deprecated(t, id) {
			t.Errorf("the %s term was retired", name)
		}
	}
	saved, err := readBackup(backup)
	if err != nil || len(saved) != 1 || saved[0].ID != ordinary || saved[0].Term != "第一次" {
		t.Fatalf("backup = %+v (err %v), want only the retired term", saved, err)
	}
	var audits []model.TrustAuditLog
	if err := testDB.Find(&audits).Error; err != nil || len(audits) != 1 || audits[0].Action != "terms_pruned_by_llm" {
		t.Fatalf("audit rows = %+v (err %v), want one terms_pruned_by_llm", audits, err)
	}
}

func deprecated(t *testing.T, id int64) bool {
	t.Helper()
	var row model.TrustTerm
	if err := testDB.Take(&row, id).Error; err != nil {
		t.Fatalf("load term %d: %v", id, err)
	}
	return row.IsDeprecated
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func readBackup(path string) ([]retired, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rows []retired
	return rows, json.Unmarshal(b, &rows)
}
