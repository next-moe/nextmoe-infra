package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	imgmodel "api/internal/platform/image/model"

	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func imagesTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// Same switch as jobs/imagegradesync: CI's integration job has no images
	// database, so this suite runs only where one is handed over.
	dsn := os.Getenv("TEST_IMAGES_DSN")
	if dsn == "" {
		t.Skip("TEST_IMAGES_DSN is unset — regrade database test not run")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := db.AutoMigrate(&imgmodel.Image{}); err != nil {
		t.Fatalf("migrate images: %v", err)
	}
	if err := db.Exec("TRUNCATE images RESTART IDENTITY").Error; err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return db
}

func seedImage(t *testing.T, db *gorm.DB, name, labels string) string {
	t.Helper()
	hash := strings.Repeat("0", 64-len(name)) + name
	img := imgmodel.Image{Hash: hash, StorageKey: name, MIME: "image/webp", Ext: "webp", Width: 1, Height: 1, SizeBytes: 1}
	if labels != "" {
		img.ReviewLabels = datatypes.JSON(labels)
	}
	if err := db.Create(&img).Error; err != nil {
		t.Fatalf("seed %s: %v", name, err)
	}
	return hash
}

func labelsOf(t *testing.T, db *gorm.DB, hash string) map[string]gradeLabels {
	t.Helper()
	var raw *string
	if err := db.Raw(`SELECT review_labels::text FROM images WHERE hash = ?`, hash).Scan(&raw).Error; err != nil {
		t.Fatalf("read %s: %v", hash, err)
	}
	out := map[string]gradeLabels{}
	if raw == nil {
		return out
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal([]byte(*raw), &all); err != nil {
		t.Fatalf("labels of %s: %v", hash, err)
	}
	for _, key := range []string{"grade", "grade_machine"} {
		if v, ok := all[key]; ok {
			var g gradeLabels
			if err := json.Unmarshal(v, &g); err != nil {
				t.Fatalf("%s of %s: %v", key, hash, err)
			}
			out[key] = g
		}
	}
	return out
}

const machineSafe = `{"grade":{"provider":"cloudflare-workers-ai","model":"moondream","level":0,"answers":{"act":false,"nude":false,"underwear":false}},"manual_reason":"kept"}`

func TestRegrade_HumanGradeReplacesTheMachineOneAndKeepsItBeside(t *testing.T) {
	db := imagesTestDB(t)
	ctx := context.Background()
	masked := seedImage(t, db, "masked", machineSafe)
	ungraded := seedImage(t, db, "ungraded", "")
	untouched := seedImage(t, db, "untouched", machineSafe)

	rows := []regradeRow{{Hash: masked, Level: 3}, {Hash: ungraded, Level: 1}}
	st, err := regrade(ctx, db, rows, false)
	if err != nil || st.changed != 2 {
		t.Fatalf("dry run: %+v, %v", st, err)
	}
	if g := labelsOf(t, db, masked)["grade"]; g.Provider == providerHuman {
		t.Fatal("a dry run wrote a grade")
	}

	if st, err = regrade(ctx, db, rows, true); err != nil || st.changed != 2 || st.unchanged != 0 {
		t.Fatalf("apply: %+v, %v", st, err)
	}
	got := labelsOf(t, db, masked)
	if g := got["grade"]; g.Provider != providerHuman || g.Level != 3 || !g.Answers["act"] || g.Answers["nude"] {
		t.Fatalf("human grade = %+v", g)
	}
	if m := got["grade_machine"]; m.Provider != "cloudflare-workers-ai" || m.Level != 0 {
		t.Fatalf("the machine's answer was not kept: %+v", m)
	}
	var reason string
	if err := db.Raw(`SELECT review_labels->>'manual_reason' FROM images WHERE hash = ?`, masked).Scan(&reason).Error; err != nil || reason != "kept" {
		t.Fatalf("another label key was lost: %q, %v", reason, err)
	}
	got = labelsOf(t, db, ungraded)
	if g := got["grade"]; g.Provider != providerHuman || g.Level != 1 || !g.Answers["underwear"] {
		t.Fatalf("grade on a never-graded image = %+v", g)
	}
	if _, has := got["grade_machine"]; has {
		t.Fatal("an image the machine never graded got a grade_machine")
	}
	if g := labelsOf(t, db, untouched)["grade"]; g.Provider == providerHuman {
		t.Fatal("an image that was not listed was regraded")
	}

	if st, err = regrade(ctx, db, rows, true); err != nil || st.changed != 0 || st.unchanged != 2 {
		t.Fatalf("a repeat must change nothing: %+v, %v", st, err)
	}
	if st, err = regrade(ctx, db, []regradeRow{{Hash: masked, Level: 2}}, true); err != nil || st.changed != 1 {
		t.Fatalf("second correction: %+v, %v", st, err)
	}
	got = labelsOf(t, db, masked)
	if got["grade"].Level != 2 || got["grade_machine"].Provider != "cloudflare-workers-ai" {
		t.Fatalf("a second correction replaced the machine's answer with the first human one: %+v", got)
	}

	st, err = regrade(ctx, db, []regradeRow{{Hash: strings.Repeat("f", 64), Level: 2}}, true)
	if err != nil || st.missing != 1 || st.changed != 0 {
		t.Fatalf("unknown hash: %+v, %v", st, err)
	}
}

func TestReadRegradeInput_RefusesWhatItCannotApply(t *testing.T) {
	dir := t.TempDir()
	hash := strings.Repeat("a", 64)
	for name, body := range map[string]string{
		"no level":     `{"hash":"` + hash + `"}`,
		"level 4":      `{"hash":"` + hash + `","level":4}`,
		"short hash":   `{"hash":"abcd","level":2}`,
		"listed twice": `{"hash":"` + hash + `","level":2}` + "\n" + `{"hash":"` + hash + `","level":3}`,
	} {
		p := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".jsonl")
		if err := os.WriteFile(p, []byte(body+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if rows, err := readRegradeInput(p); err == nil {
			t.Errorf("%s: accepted %+v", name, rows)
		}
	}
	p := filepath.Join(dir, "ok.jsonl")
	if err := os.WriteFile(p, []byte(`{"hash":"`+hash+`","level":0}`+"\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if rows, err := readRegradeInput(p); err != nil || len(rows) != 1 || rows[0].Level != 0 {
		t.Fatalf("a level-0 correction must be accepted: %+v, %v", rows, err)
	}
}
