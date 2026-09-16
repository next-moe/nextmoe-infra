package getchuchars

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"api/internal/infrastructure/database"
	"api/internal/jobs/charattrs"
	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

const (
	entityTypeRelease = int16(6)
	linkKindExact     = int16(0)
	langJa            = "ja"
	provenanceSource  = "getchu"
)

type Opts struct {
	DSN       string
	GetchuDSN string
	Apply     bool
	Limit     int
}

type Stats struct {
	Match MatchStats

	IntroWritten int
	IntroExists  int
	IntroNoText  int
	AttrsWritten int
	AttrFields   int
	AttrSkipped  int
	AttrAdopted  int
	Conflict     int
	Errors       int
}

func (s Stats) String() string {
	return fmt.Sprintf(
		"input=%d matched=%d (name=%d alias=%d reading=%d) no_work=%d bundle=%d no_name=%d ambiguous=%d collided=%d | "+
			"intro_written=%d intro_exists=%d intro_no_text=%d | attr_rows=%d attr_fields=%d attr_adopted=%d attr_skipped=%d | conflict=%d errors=%d",
		s.Match.Input, s.Match.Matched, s.Match.ByName, s.Match.ByAlias, s.Match.ByReading,
		s.Match.NoWork, s.Match.Bundle, s.Match.NoNameInWork, s.Match.Ambiguous, s.Match.Collided,
		s.IntroWritten, s.IntroExists, s.IntroNoText,
		s.AttrsWritten, s.AttrFields, s.AttrAdopted, s.AttrSkipped, s.Conflict, s.Errors)
}

func Run(ctx context.Context, opts Opts) (*Stats, error) {
	if opts.DSN == "" || opts.GetchuDSN == "" {
		return nil, fmt.Errorf("--dsn and --getchu-dsn are both REQUIRED; refusing to guess either")
	}
	db, err := open(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog: %w", err)
	}
	defer closeDB(db)
	gdb, err := open(opts.GetchuDSN)
	if err != nil {
		return nil, fmt.Errorf("connect getchu staging: %w", err)
	}
	defer closeDB(gdb)

	getchuSource, err := SourceID(ctx, db)
	if err != nil {
		return nil, err
	}

	cands, ms, err := Resolve(ctx, db, gdb, getchuSource)
	if err != nil {
		return nil, err
	}
	if opts.Limit > 0 && opts.Limit < len(cands) {
		cands = cands[:opts.Limit]
	}
	st := &Stats{Match: ms}
	slog.Info("getchu-chars matched", "result", ms, "candidates", len(cands), "apply", opts.Apply)

	for _, c := range cands {
		writeIntro(ctx, db, getchuSource, c, opts.Apply, st)
		writeAttrs(ctx, db, provenanceSource, c, opts.Apply, st)
	}
	slog.Info("getchu-chars done", "apply", opts.Apply, "result", st.String())
	return st, nil
}

func writeIntro(ctx context.Context, db *gorm.DB, source int16, c Candidate, apply bool, st *Stats) {
	text := strings.TrimSpace(c.Profile)
	if text == "" {
		st.IntroNoText++
		return
	}
	var n int64
	if err := db.WithContext(ctx).Raw(
		`SELECT count(*) FROM catalog_character_intro WHERE character_id = ? AND lang = ?`,
		c.CharacterID, langJa).Scan(&n).Error; err != nil {
		st.Errors++
		return
	}
	if n > 0 {
		st.IntroExists++
		return
	}
	if !apply {
		st.IntroWritten++
		return
	}
	res := db.WithContext(ctx).Exec(`
		INSERT INTO catalog_character_intro (character_id, lang, intro, source_id, provenance)
		VALUES (?,?,?,?,0) ON CONFLICT DO NOTHING`, c.CharacterID, langJa, text, source)
	switch {
	case res.Error != nil:
		st.Errors++
		slog.Warn("write character intro", "character", c.CharacterID, "err", res.Error)
	case res.RowsAffected == 0:
		st.Conflict++
	default:
		st.IntroWritten++
	}
}

func writeAttrs(ctx context.Context, db *gorm.DB, source string, c Candidate, apply bool, st *Stats) {
	if len(c.Attrs) == 0 {
		return
	}
	var raw map[string]string
	if err := json.Unmarshal(c.Attrs, &raw); err != nil {
		st.Errors++
		return
	}

	proposed := map[string]any{}
	if v, _ := charattrs.ParseHeightCM(raw["身長"]); v != nil {
		proposed["height_cm"] = *v
	}
	if v, _ := charattrs.ParseWeightKG(raw["体重"]); v != nil {
		proposed["weight_kg"] = *v
	}
	if v := charattrs.ParseBloodType(raw["血液型"]); v != nil {
		proposed["blood_type"] = *v
	}
	if m, d := charattrs.ParseBirthdayMD(raw["誕生日"]); m != nil || d != nil {
		if m != nil {
			proposed["birthday_month"] = *m
		}
		if d != nil {
			proposed["birthday_day"] = *d
		}
	}
	if b, w, h, cup := charattrs.ParseBWH(raw["スリーサイズ"]); b != nil || w != nil || h != nil || cup != nil {
		if b != nil {
			proposed["bust_cm"] = *b
		}
		if w != nil {
			proposed["waist_cm"] = *w
		}
		if h != nil {
			proposed["hip_cm"] = *h
		}
		if cup != nil {
			proposed["cup"] = *cup
		}
	}
	if len(proposed) == 0 {
		return
	}

	cur, prov, err := currentAttrs(ctx, db, c.CharacterID)
	if err != nil {
		st.Errors++
		return
	}
	fill := map[string]any{}
	var adopt []string
	for col, v := range proposed {
		switch {
		case cur[col] == nil:
			fill[col] = v
		case sameValue(cur[col], v):
			if _, has := prov[col]; !has {
				adopt = append(adopt, col)
			}
		}
	}
	if len(fill) == 0 && len(adopt) == 0 {
		st.AttrSkipped++
		return
	}
	st.AttrAdopted += len(adopt)
	if !apply {
		if len(fill) > 0 {
			st.AttrsWritten++
			st.AttrFields += len(fill)
		}
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, col := range adopt {
		prov[col] = mustMarshal([]json.RawMessage{
			json.RawMessage(fmt.Sprintf(`{"at":%q,"source":%q}`, now, source))})
	}
	for col := range fill {
		entry := json.RawMessage(fmt.Sprintf(`{"at":%q,"source":%q}`, now, source))
		var arr []json.RawMessage
		if b, ok := prov[col]; ok {
			_ = json.Unmarshal(b, &arr)
		}
		prov[col] = mustMarshal(append([]json.RawMessage{entry}, arr...))
	}
	fill["field_provenance"] = mustMarshal(prov)
	fill["updated_at"] = time.Now()

	res := db.WithContext(ctx).Table("catalog_character").Where("id = ?", c.CharacterID).Updates(fill)
	if res.Error != nil {
		st.Errors++
		slog.Warn("write character attrs", "character", c.CharacterID, "err", res.Error)
		return
	}
	if n := len(fill) - 2; n > 0 {
		st.AttrsWritten++
		st.AttrFields += n
	}
}

func sameValue(cur any, proposed any) bool {
	switch c := cur.(type) {
	case int16:
		p, ok := proposed.(int16)
		return ok && c == p
	case string:
		p, ok := proposed.(string)
		return ok && c == p
	default:
		return false
	}
}

func currentAttrs(ctx context.Context, db *gorm.DB, id int64) (map[string]any, map[string]json.RawMessage, error) {
	var row struct {
		HeightCM      *int16          `gorm:"column:height_cm"`
		WeightKG      *int16          `gorm:"column:weight_kg"`
		BloodType     *int16          `gorm:"column:blood_type"`
		BirthdayMonth *int16          `gorm:"column:birthday_month"`
		BirthdayDay   *int16          `gorm:"column:birthday_day"`
		BustCM        *int16          `gorm:"column:bust_cm"`
		WaistCM       *int16          `gorm:"column:waist_cm"`
		HipCM         *int16          `gorm:"column:hip_cm"`
		Cup           *string         `gorm:"column:cup"`
		Prov          json.RawMessage `gorm:"column:field_provenance"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT height_cm, weight_kg, blood_type, birthday_month, birthday_day,
		       bust_cm, waist_cm, hip_cm, cup, field_provenance
		FROM catalog_character WHERE id = ?`, id).Scan(&row).Error; err != nil {
		return nil, nil, err
	}
	cur := map[string]any{
		"height_cm": nilIf(row.HeightCM), "weight_kg": nilIf(row.WeightKG),
		"blood_type":     nilIf(row.BloodType),
		"birthday_month": nilIf(row.BirthdayMonth), "birthday_day": nilIf(row.BirthdayDay),
		"bust_cm": nilIf(row.BustCM), "waist_cm": nilIf(row.WaistCM), "hip_cm": nilIf(row.HipCM),
		"cup": nilIfStr(row.Cup),
	}
	prov := map[string]json.RawMessage{}
	if len(row.Prov) > 0 {
		_ = json.Unmarshal(row.Prov, &prov)
	}
	return cur, prov, nil
}

func nilIf(v *int16) any {
	if v == nil {
		return nil
	}
	return *v
}

func nilIfStr(v *string) any {
	if v == nil || *v == "" {
		return nil
	}
	return *v
}

func mustMarshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func open(dsn string) (*gorm.DB, error) {
	return database.OpenJob(dsn)
}

func closeDB(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

var _ = model.CatalogCharacterIntro{}
