package main

import (
	"fmt"
	"io"
	"strings"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

type healCase struct {
	LabelID   int64
	Expect    string
	Canonical string
}

var healCases = []healCase{
	{4086, "POISON / POISON MOTION / POISON EXTASY", "POISON"},
	{5960, "インターハート / Candy Soft / ぐみそふと / はちみつそふと / REAL / DarknessPot / 娘。 / しばそふと / DESSERT Soft / カカオ / ういろうそふと / ましゅまろそふと", "インターハート"},
	{8791, "CHAOS-R / CHAOS-L / CHAOS-R EXTREME /CHAOS-R Re:Master / CHAOS-R feat. Freak Strike / FREAK STRIKE / CHAOS-R LIS-UP / MAYHEM", "CHAOS-R"},
	{9133, "ココロリウム / ア・ラ・フィリア", "ココロリウム"},
	{9522, "Omega Program / 正経同人", "Omega Program"},
	{10145, "ぱちぱちそふと / ぱちぱちそふと黒", "ぱちぱちそふと"},
	{11646, "モニスタラッシュ / a Matures", "ア・マチュアズ"},
	{11786, "ピンポイント / キングピン / ピンポイントクイック", "ピンポイント"},
}

type aliasRow struct {
	Name string
	Kind int16
}

type caseResult struct {
	skipped bool
	reason  string
	aliases []aliasRow
}

func planAliases(original, canonical string) []aliasRow {
	rows := []aliasRow{{Name: original, Kind: model.AliasKindSearchHint}}
	seen := map[string]bool{original: true, canonical: true}
	for _, seg := range strings.Split(original, "/") {
		seg = strings.TrimSpace(seg)
		if seg == "" || seen[seg] {
			continue
		}
		seen[seg] = true
		rows = append(rows, aliasRow{Name: seg, Kind: model.AliasKindSpellingVariant})
	}
	return rows
}

const updateSQL = `
UPDATE catalog_label
   SET display_name = ?,
       field_provenance = jsonb_set(field_provenance, '{display_name}',
           jsonb_build_array(jsonb_build_object(
               'source', 'curated',
               'at', to_char(now() AT TIME ZONE 'utc', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')))
           || COALESCE(field_provenance->'display_name', '[]'::jsonb)),
       updated_at = now()
 WHERE id = ? AND display_name = ? AND deleted_at IS NULL`

const insertAliasSQL = `
INSERT INTO catalog_label_alias (label_id, name, lang, kind, is_primary_for_locale, provenance)
VALUES (?, ?, '', ?, false, 0)
ON CONFLICT (label_id, name, lang) DO NOTHING`

func applyCase(db *gorm.DB, c healCase, apply bool, w io.Writer) (caseResult, error) {
	var current []string
	if err := db.Raw(`SELECT display_name FROM catalog_label WHERE id = ? AND deleted_at IS NULL`,
		c.LabelID).Scan(&current).Error; err != nil {
		return caseResult{}, err
	}
	if len(current) == 0 {
		return skip(w, c, "label not found or soft-deleted"), nil
	}
	got := current[0]
	if !strings.Contains(got, "/") {
		return skip(w, c, fmt.Sprintf("display_name carries no slash any more (%q) — already healed, or healed by someone else", got)), nil
	}
	if got != c.Expect {
		return skip(w, c, fmt.Sprintf("display_name drifted since adjudication\n      adjudicated: %q\n      live:        %q", c.Expect, got)), nil
	}

	aliases := planAliases(got, c.Canonical)
	fmt.Fprintf(w, "[heal] label %d\n  %q\n  -> %q\n", c.LabelID, got, c.Canonical)
	for _, a := range aliases {
		fmt.Fprintf(w, "  + alias kind=%d %q\n", a.Kind, a.Name)
	}
	if !apply {
		return caseResult{aliases: aliases}, nil
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(updateSQL, c.Canonical, c.LabelID, c.Expect)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return fmt.Errorf("rename matched %d rows, want 1 (the row changed under us)", res.RowsAffected)
		}
		for _, a := range aliases {
			if err := tx.Exec(insertAliasSQL, c.LabelID, a.Name, a.Kind).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return caseResult{}, err
	}
	return caseResult{aliases: aliases}, nil
}

func skip(w io.Writer, c healCase, reason string) caseResult {
	fmt.Fprintf(w, "[SKIP] label %d: %s\n", c.LabelID, reason)
	return caseResult{skipped: true, reason: reason}
}
