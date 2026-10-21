package editspec

import (
	"context"
	"fmt"
	"strings"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"gorm.io/gorm"
)

const FieldWorkCreditsUpstream = "catalog.work.credits_upstream"

var creditStampColumns = []string{"note", "spoiler", "release_id", "label_id"}

func creditsUpstreamFieldSpec() editing.FieldSpec {
	table := catmodel.CatalogCredit{}.TableName()
	targets := make([]editing.ProvenanceTarget, 0, len(creditStampColumns))
	for _, column := range creditStampColumns {
		targets = append(targets, editing.ProvenanceTarget{
			Table: table, Column: column, Rows: upstreamCreditRowsChanging(column),
		})
	}
	return editing.FieldSpec{
		Key: FieldWorkCreditsUpstream, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
		MaxElements: maxCreditElements,
		Value: &editing.ValueSpec{Element: &editing.ElementSpec{
			Type: "object",
			Members: []editing.ElementMember{
				{Key: "role_id", Type: "ref"},
				{Key: "credit_name_id", Type: "ref"},
				{Key: "character_id", Type: "ref", Nullable: true},
				{Key: "note", Type: "text", Nullable: true},
				{Key: "spoiler", Type: "int", Vocabulary: "spoiler"},
				{Key: "release_id", Type: "ref", Nullable: true},
				{Key: "label_id", Type: "ref", Nullable: true},
			},
		}},
		Validate:   validateUpstreamCredits,
		Apply:      applyUpstreamCredits,
		Provenance: targets,
	}
}

// Unlike catalog.work.credits, an element here states the whole row: an
// absent nullable member means empty, not "keep what the row has". Editors drop
// blank nullable members before submitting, so a cleared note or release would
// otherwise never reach the row.
func parseUpstreamCredits(v any) ([]workCredit, error) {
	arr, err := asArrayN(v, "credit objects", maxCreditElements)
	if err != nil {
		return nil, err
	}
	out := make([]workCredit, 0, len(arr))
	seen := make(map[creditKey]struct{}, len(arr))
	for i, el := range arr {
		obj, err := asObject(el, i, "role_id", "credit_name_id", "character_id", "note", "spoiler", "release_id", "label_id")
		if err != nil {
			return nil, err
		}
		if err := requireMembers(obj, i, "role_id", "credit_name_id", "spoiler"); err != nil {
			return nil, err
		}
		row := workCredit{HasSpoiler: true, HasRelease: true, HasLabel: true}
		if row.RoleID, err = objInt(obj, "role_id", i, true); err != nil {
			return nil, err
		}
		if row.CreditNameID, err = objInt(obj, "credit_name_id", i, true); err != nil {
			return nil, err
		}
		if row.RoleID <= 0 || row.CreditNameID <= 0 {
			return nil, fmt.Errorf("element %d: role_id and credit_name_id must be positive ids", i)
		}
		if raw, present := obj["character_id"]; present && raw != nil {
			if row.CharacterID, err = objInt(obj, "character_id", i, true); err != nil {
				return nil, err
			}
			if row.CharacterID <= 0 {
				return nil, fmt.Errorf("element %d: character_id must be a positive id; omit it when the credit names no character", i)
			}
		}
		if _, dup := seen[row.key()]; dup {
			return nil, fmt.Errorf("element %d: duplicate (role_id, credit_name_id, character_id)", i)
		}
		seen[row.key()] = struct{}{}
		if raw, present := obj["note"]; present && raw != nil {
			if row.Note, err = objString(obj, "note", i, false, maxCreditNoteRunes); err != nil {
				return nil, err
			}
		}
		sp, err := objInt(obj, "spoiler", i, true)
		if err != nil {
			return nil, err
		}
		switch int16(sp) {
		case catmodel.SpoilerNone, catmodel.SpoilerMild, catmodel.SpoilerSevere:
		default:
			return nil, fmt.Errorf("element %d: spoiler must be 0 (none), 1 (minor) or 2 (major)", i)
		}
		spoiler := int16(sp)
		row.Spoiler = &spoiler
		if _, row.ReleaseID, err = optionalRef(obj, "release_id", i); err != nil {
			return nil, err
		}
		if _, row.LabelID, err = optionalRef(obj, "label_id", i); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

func validateUpstreamCredits(v any) error {
	_, err := parseUpstreamCredits(v)
	return err
}

type upstreamCreditRow struct {
	ID           int64  `gorm:"column:id"`
	RoleID       int64  `gorm:"column:role_id"`
	CreditNameID int64  `gorm:"column:credit_name_id"`
	CharacterID  int64  `gorm:"column:character_id"`
	Note         string `gorm:"column:note"`
	Spoiler      int16  `gorm:"column:spoiler"`
	ReleaseID    *int64 `gorm:"column:release_id"`
	LabelID      *int64 `gorm:"column:label_id"`
}

func (r upstreamCreditRow) key() creditKey { return creditKey{r.RoleID, r.CreditNameID, r.CharacterID} }

func loadUpstreamCreditRows(ctx context.Context, tx *gorm.DB, workID int64, credits []workCredit) ([]upstreamCreditRow, error) {
	if len(credits) == 0 {
		return nil, nil
	}
	tuples := make([][]any, 0, len(credits))
	for _, c := range credits {
		tuples = append(tuples, []any{c.RoleID, c.CreditNameID, c.CharacterID})
	}
	var rows []upstreamCreditRow
	err := tx.WithContext(ctx).Raw(`SELECT id, role_id, credit_name_id, COALESCE(character_id, 0) AS character_id,
		note, spoiler, release_id, label_id
		FROM catalog_credit
		WHERE work_id = ? AND `+NotCuratedLaneSQL("source_id")+`
		  AND (role_id, credit_name_id, COALESCE(character_id, 0)) IN ?
		ORDER BY id`, workID, tuples).Scan(&rows).Error
	return rows, err
}

func sameRef(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func creditColumnDiffers(column string, have upstreamCreditRow, want workCredit) bool {
	switch column {
	case "note":
		return have.Note != want.Note
	case "spoiler":
		return have.Spoiler != *want.Spoiler
	case "release_id":
		return !sameRef(have.ReleaseID, want.ReleaseID)
	case "label_id":
		return !sameRef(have.LabelID, want.LabelID)
	}
	return false
}

func applyUpstreamCredits(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	credits, err := parseUpstreamCredits(value)
	if err != nil {
		return fmt.Errorf("editspec: credits_upstream: %w", err)
	}
	if err := assertWorkExists(ctx, tx, entityID); err != nil {
		return err
	}
	if err := rejectUnknownCreditScopes(ctx, tx, entityID, credits, FieldWorkCreditsUpstream); err != nil {
		return err
	}
	rows, err := loadUpstreamCreditRows(ctx, tx, entityID, credits)
	if err != nil {
		return err
	}
	byKey := make(map[creditKey]upstreamCreditRow, len(rows))
	for _, r := range rows {
		byKey[r.key()] = r
	}
	for _, c := range credits {
		have, ok := byKey[c.key()]
		if !ok {
			return &editing.ValidationError{Key: FieldWorkCreditsUpstream, Reason: fmt.Sprintf(
				"%s is not an upstream credit of this work", CreditIdentity(c.RoleID, c.CreditNameID, c.CharacterID))}
		}
		changed := false
		for _, column := range creditStampColumns {
			changed = changed || creditColumnDiffers(column, have, c)
		}
		if !changed {
			continue
		}
		if err := tx.WithContext(ctx).Exec(`UPDATE catalog_credit
			SET note = ?, spoiler = ?, release_id = ?, label_id = ?, updated_at = now()
			WHERE id = ?`, c.Note, *c.Spoiler, c.ReleaseID, c.LabelID, have.ID).Error; err != nil {
			return err
		}
	}
	return nil
}

func upstreamCreditRowsChanging(column string) func(context.Context, *gorm.DB, int64, any) ([]int64, error) {
	return func(ctx context.Context, tx *gorm.DB, entityID int64, value any) ([]int64, error) {
		credits, err := parseUpstreamCredits(value)
		if err != nil || len(credits) == 0 {
			return nil, err
		}
		rows, err := loadUpstreamCreditRows(ctx, tx, entityID, credits)
		if err != nil {
			return nil, err
		}
		want := make(map[creditKey]workCredit, len(credits))
		for _, c := range credits {
			want[c.key()] = c
		}
		var out []int64
		for _, r := range rows {
			if c, ok := want[r.key()]; ok && creditColumnDiffers(column, r, c) {
				out = append(out, r.ID)
			}
		}
		return out, nil
	}
}

func loadUpstreamCredits(ctx context.Context, db *gorm.DB, workID int64) ([]any, error) {
	var rows []catmodel.CatalogCredit
	if err := db.WithContext(ctx).
		Where("work_id = ? AND "+NotCuratedLaneSQL("source_id"), workID).
		Order("source_id, role_id, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows))
	for _, r := range rows {
		el := map[string]any{
			"role_id": r.RoleID, "credit_name_id": r.CreditNameID, "spoiler": int64(r.Spoiler),
		}
		if r.CharacterID != nil {
			el["character_id"] = *r.CharacterID
		}
		if r.Note != "" {
			el["note"] = r.Note
		}
		if r.ReleaseID != nil {
			el["release_id"] = *r.ReleaseID
		}
		if r.LabelID != nil {
			el["label_id"] = *r.LabelID
		}
		out = append(out, el)
	}
	return out, nil
}

func CreditHumanStampedSQL(alias string) string {
	parts := make([]string, 0, len(creditStampColumns))
	for _, column := range creditStampColumns {
		parts = append(parts, HumanFieldProvenanceSQL(alias+".field_provenance", column))
	}
	return "COALESCE(" + strings.Join(parts, " OR ") + ", false)"
}

// CreditStampCarrySet is the SET clause of an UPDATE of catalog_credit `target`
// FROM catalog_credit `source`, for a machine step that keeps target and throws
// source away on a key collision (a merge, a role repair): every column a
// person corrected on source and nobody corrected on target moves over with
// its stamp.
func CreditStampCarrySet(target, source string) string {
	var sets []string
	prov := target + ".field_provenance"
	for _, column := range creditStampColumns {
		carry := fmt.Sprintf("COALESCE(%s, false) AND NOT COALESCE(%s, false)",
			HumanFieldProvenanceSQL(source+".field_provenance", column),
			HumanFieldProvenanceSQL(target+".field_provenance", column))
		sets = append(sets, fmt.Sprintf("%[1]s = CASE WHEN %[2]s THEN %[3]s.%[1]s ELSE %[4]s.%[1]s END",
			column, carry, source, target))
		prov += fmt.Sprintf(" || CASE WHEN %s THEN jsonb_build_object('%s', %s.field_provenance -> '%s') ELSE '{}'::jsonb END",
			carry, column, source, column)
	}
	sets = append(sets, "field_provenance = "+prov, "updated_at = now()")
	return "SET " + strings.Join(sets, ",\n\t\t    ")
}
