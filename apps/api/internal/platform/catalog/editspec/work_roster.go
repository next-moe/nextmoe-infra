package editspec

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"gorm.io/gorm"
)

const (
	// A work carries at most 354 roster rows in production (7 above 200, none
	// above 500, 2026-08 over 218,846 rows). The element list and the
	// suppression set share the number for the same reason credits do.
	maxRosterElements = 500
	maxRosterSuppress = maxRosterElements
)

type rosterEdge struct {
	CharacterID int64
	Kind        int16
	Spoiler     int16
}

func parseRoster(v any) ([]rosterEdge, error) {
	arr, err := asArrayN(v, "roster objects", maxRosterElements)
	if err != nil {
		return nil, err
	}
	out := make([]rosterEdge, 0, len(arr))
	seen := make(map[int64]struct{}, len(arr))
	for i, el := range arr {
		obj, err := asObject(el, i, "character_id", "kind", "spoiler")
		if err != nil {
			return nil, err
		}
		characterID, err := objInt(obj, "character_id", i, true)
		if err != nil {
			return nil, err
		}
		if characterID <= 0 {
			return nil, fmt.Errorf("element %d: character_id must be a positive id", i)
		}
		if _, dup := seen[characterID]; dup {
			return nil, fmt.Errorf("element %d: character_id %d appears twice; "+
				"each character has at most one roster row, so two elements would be two different values for it",
				i, characterID)
		}
		seen[characterID] = struct{}{}
		kind, err := objInt(obj, "kind", i, true)
		if err != nil {
			return nil, err
		}
		switch int16(kind) {
		case catmodel.WorkCharacterKindUnknown, catmodel.WorkCharacterKindMain,
			catmodel.WorkCharacterKindSecondary, catmodel.WorkCharacterKindAppears:
		default:
			return nil, fmt.Errorf("element %d: kind must be 0 (unknown), 1 (main), 2 (secondary) or 3 (appears)", i)
		}
		spoiler, err := objInt(obj, "spoiler", i, true)
		if err != nil {
			return nil, err
		}
		switch int16(spoiler) {
		case catmodel.SpoilerNone, catmodel.SpoilerMild, catmodel.SpoilerSevere:
		default:
			return nil, fmt.Errorf("element %d: spoiler must be 0 (none), 1 (minor) or 2 (major)", i)
		}
		out = append(out, rosterEdge{CharacterID: characterID, Kind: int16(kind), Spoiler: int16(spoiler)})
	}
	return out, nil
}

func validateRoster(v any) error {
	_, err := parseRoster(v)
	return err
}

// applyRoster patches the named rows of this work's roster in place. It holds
// no INSERT and no DELETE by construction: a roster edge is the input of the
// vndb attach channel, which has minted 35,714 permanent identity anchors from
// "this name is already on this work's roster", so creating one from the content
// write face would mint anchors sideways. Removing an edge is what
// catalog.work.roster.suppressed is for.
func applyRoster(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	edges, err := parseRoster(value)
	if err != nil {
		return fmt.Errorf("editspec: roster: %w", err)
	}
	var work catmodel.CatalogWork
	if err := tx.WithContext(ctx).Select("id").First(&work, entityID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return editing.ErrEntityNotFound
		}
		return err
	}
	if len(edges) == 0 {
		return nil
	}
	if err := rejectUnknownRosterReferences(ctx, tx, entityID, edges); err != nil {
		return err
	}
	type valuePair struct{ Kind, Spoiler int16 }
	groups := make(map[valuePair][]int64, len(edges))
	for _, e := range edges {
		p := valuePair{Kind: e.Kind, Spoiler: e.Spoiler}
		groups[p] = append(groups[p], e.CharacterID)
	}
	pairs := make([]valuePair, 0, len(groups))
	for p := range groups {
		pairs = append(pairs, p)
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Kind != pairs[j].Kind {
			return pairs[i].Kind < pairs[j].Kind
		}
		return pairs[i].Spoiler < pairs[j].Spoiler
	})
	for _, p := range pairs {
		ids := groups[p]
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		if err := tx.WithContext(ctx).Exec(
			`UPDATE catalog_work_character SET kind = ?, spoiler = ?, updated_at = now()
			 WHERE work_id = ? AND character_id IN ?
			   AND (kind, spoiler) IS DISTINCT FROM (?, ?)`,
			p.Kind, p.Spoiler, entityID, ids, p.Kind, p.Spoiler).Error; err != nil {
			return err
		}
	}
	return nil
}

// rejectUnknownRosterReferences turns a dangling or off-roster character into a
// 422 the proposer sees. character_id is a real foreign key and the update is
// scoped to this work, so without this an id from another work silently patches
// nothing while a soft-deleted id would look the same — and the 08-wave lesson
// was that an unvalidated reference reaches the reviewer as a 500 carrying a
// Postgres constraint name, with the proposal stuck in the queue forever.
//
// catalog_character carries gorm.DeletedAt, so a character merged away since the
// proposal was written is simply not on the roster any more.
func rejectUnknownRosterReferences(ctx context.Context, tx *gorm.DB, workID int64, edges []rosterEdge) error {
	ids := make([]int64, 0, len(edges))
	for _, e := range edges {
		ids = append(ids, e.CharacterID)
	}
	var known []int64
	if err := tx.WithContext(ctx).Raw(
		`SELECT wc.character_id FROM catalog_work_character wc
		 JOIN catalog_character ch ON ch.id = wc.character_id AND ch.deleted_at IS NULL
		 WHERE wc.work_id = ? AND wc.character_id IN ?`, workID, ids).Scan(&known).Error; err != nil {
		return err
	}
	missing := missingIDs(ids, known)
	if len(missing) == 0 {
		return nil
	}
	return &editing.ValidationError{
		Key: FieldWorkRoster,
		Reason: fmt.Sprintf(
			"character %s is not on this work's roster: this field edits the appearance of characters upstream already lists, "+
				"it cannot add one; to hide a row use %s",
			idList(missing), FieldWorkRosterSuppr),
	}
}

// rosterRowsWhoseValueChanges resolves the submitted value to the ids of the
// rows whose stored `column` actually differs from it. Only those get the human
// stamp: the edit face echoes the whole roster back (up to 354 rows), so
// stamping every submitted row would mark a work's entire cast as
// human-asserted and the merge gate would then be protecting machine values.
func rosterRowsWhoseValueChanges(column string, of func(rosterEdge) int16) func(context.Context, *gorm.DB, int64, any) ([]int64, error) {
	return func(ctx context.Context, tx *gorm.DB, entityID int64, value any) ([]int64, error) {
		edges, err := parseRoster(value)
		if err != nil {
			return nil, err
		}
		if len(edges) == 0 {
			return nil, nil
		}
		want := make(map[int64]int16, len(edges))
		ids := make([]int64, 0, len(edges))
		for _, e := range edges {
			want[e.CharacterID] = of(e)
			ids = append(ids, e.CharacterID)
		}
		var rows []struct {
			ID          int64 `gorm:"column:id"`
			CharacterID int64 `gorm:"column:character_id"`
			Value       int16 `gorm:"column:value"`
		}
		if err := tx.WithContext(ctx).Raw(
			`SELECT id, character_id, `+column+` AS value FROM catalog_work_character
			 WHERE work_id = ? AND character_id IN ? ORDER BY id`, entityID, ids).Scan(&rows).Error; err != nil {
			return nil, err
		}
		var out []int64
		for _, r := range rows {
			if r.Value != want[r.CharacterID] {
				out = append(out, r.ID)
			}
		}
		return out, nil
	}
}

// RosterIdentity is the frozen row identity of catalog.work.roster (doc 21 D10:
// registered means never redefined). It is exactly uq_catalog_work_character
// minus work_id, so its uniqueness is held by an index rather than by a
// convention. character_id is a real foreign key with no 0 sentinel, so the key
// has no absent form.
func RosterIdentity(characterID int64) string {
	return fmt.Sprintf("roster:%d", characterID)
}

// RosterIdentitySQL restates RosterIdentity for a catalog_work_character alias —
// the one place the two definitions could drift apart, which is why
// TestRosterIdentityGoAndSQLAgree recomputes both over real rows.
func RosterIdentitySQL(alias string) string {
	return fmt.Sprintf(`('roster:' || %s.character_id)`, alias)
}

// NotSuppressedRosterSQL is the read-path predicate, correlated on a
// catalog_work_character alias.
func NotSuppressedRosterSQL(alias string) string {
	return fmt.Sprintf(
		`NOT EXISTS (SELECT 1 FROM edit_suppressed_row esr
			WHERE esr.entity_type = '%s' AND esr.field_key = '%s'
			  AND esr.entity_id = %s.work_id AND esr.identity_key = %s)`,
		TypeWork, FieldWorkRoster, alias, RosterIdentitySQL(alias))
}

func LiveWorkSQL(workCol string) string {
	return fmt.Sprintf(`EXISTS (SELECT 1 FROM catalog_work lw WHERE lw.id = %s AND lw.deleted_at IS NULL AND lw.status = %d)`,
		workCol, catmodel.WorkStatusLive)
}

func rosterKeyCheck(key string) error {
	parts := strings.Split(key, ":")
	if len(parts) != 2 || parts[0] != "roster" {
		return fmt.Errorf("%q is not an identity key of this field: expected roster:<character_id>", key)
	}
	n, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || strconv.FormatInt(n, 10) != parts[1] {
		return fmt.Errorf("%q is not an identity key of this field: %q is not a canonical id", key, parts[1])
	}
	if n <= 0 {
		return fmt.Errorf("%q is not an identity key of this field: character_id must be positive", key)
	}
	return nil
}

func loadSuppressedRoster(ctx context.Context, db *gorm.DB, workID int64) ([]any, error) {
	return editing.LoadSuppressedValue(ctx, db, TypeWork, workID, FieldWorkRoster)
}

func loadRoster(ctx context.Context, db *gorm.DB, workID int64) ([]any, error) {
	var rows []struct {
		CharacterID int64 `gorm:"column:character_id"`
		Kind        int16 `gorm:"column:kind"`
		Spoiler     int16 `gorm:"column:spoiler"`
	}
	if err := db.WithContext(ctx).Raw(
		`SELECT character_id, kind, spoiler FROM catalog_work_character
		 WHERE work_id = ? ORDER BY character_id`, workID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]any{
			"character_id": r.CharacterID, "kind": r.Kind, "spoiler": r.Spoiler,
		})
	}
	return out, nil
}
