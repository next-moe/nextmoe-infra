package editspec

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"gorm.io/gorm"
)

const (
	// A work carries at most 340 credit rows in production (34 works above 200,
	// none above 500, 2026-08 over 1,020,770 rows), so 500 is headroom over the
	// observed maximum rather than a round number. The curated list and the
	// suppression set share it: how many credit rows one work's human lane can
	// carry should have exactly one answer.
	maxCreditElements  = 500
	maxCreditSuppress  = maxCreditElements
	maxCreditNoteRunes = 500
)

type workCredit struct {
	RoleID       int64
	CreditNameID int64
	CharacterID  int64
	Note         string
	// Spoiler, ReleaseID and LabelID are optional members: an element that
	// leaves one out keeps the value the same credit already carries, so a
	// client that predates them cannot wipe what another one set.
	Spoiler    *int16
	ReleaseID  *int64
	LabelID    *int64
	HasSpoiler bool
	HasRelease bool
	HasLabel   bool
}

type creditKey struct{ RoleID, CreditNameID, CharacterID int64 }

func (c workCredit) key() creditKey { return creditKey{c.RoleID, c.CreditNameID, c.CharacterID} }

func parseCredits(v any) ([]workCredit, error) {
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
		roleID, err := objInt(obj, "role_id", i, true)
		if err != nil {
			return nil, err
		}
		if roleID <= 0 {
			return nil, fmt.Errorf("element %d: role_id must be a positive id", i)
		}
		creditNameID, err := objInt(obj, "credit_name_id", i, true)
		if err != nil {
			return nil, err
		}
		if creditNameID <= 0 {
			return nil, fmt.Errorf("element %d: credit_name_id must be a positive id", i)
		}
		var characterID int64
		if _, present := obj["character_id"]; present {
			characterID, err = objInt(obj, "character_id", i, true)
			if err != nil {
				return nil, err
			}
			if characterID <= 0 {
				return nil, fmt.Errorf("element %d: character_id must be a positive id; omit it when the credit names no character", i)
			}
		}
		note, err := objString(obj, "note", i, false, maxCreditNoteRunes)
		if err != nil {
			return nil, err
		}
		row := workCredit{RoleID: roleID, CreditNameID: creditNameID, CharacterID: characterID}
		if _, dup := seen[row.key()]; dup {
			return nil, fmt.Errorf("element %d: duplicate (role_id, credit_name_id, character_id)", i)
		}
		seen[row.key()] = struct{}{}
		row.Note = note
		if raw, present := obj["spoiler"]; present {
			row.HasSpoiler = true
			if raw != nil {
				sp, err := objInt(obj, "spoiler", i, true)
				if err != nil {
					return nil, err
				}
				switch int16(sp) {
				case catmodel.SpoilerNone, catmodel.SpoilerMild, catmodel.SpoilerSevere:
				default:
					return nil, fmt.Errorf("element %d: spoiler must be 0 (none), 1 (minor) or 2 (major)", i)
				}
				v := int16(sp)
				row.Spoiler = &v
			}
		}
		if row.HasRelease, row.ReleaseID, err = optionalRef(obj, "release_id", i); err != nil {
			return nil, err
		}
		if row.HasLabel, row.LabelID, err = optionalRef(obj, "label_id", i); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

func validateCredits(v any) error {
	_, err := parseCredits(v)
	return err
}

func applyCredits(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	credits, err := parseCredits(value)
	if err != nil {
		return fmt.Errorf("editspec: credits: %w", err)
	}
	var work catmodel.CatalogWork
	if err := tx.WithContext(ctx).Select("id").First(&work, entityID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return editing.ErrEntityNotFound
		}
		return err
	}
	if err := rejectUnknownCreditReferences(ctx, tx, credits); err != nil {
		return err
	}
	if err := rejectUpstreamCreditCollisions(ctx, tx, entityID, credits); err != nil {
		return err
	}
	if err := rejectUnknownCreditScopes(ctx, tx, entityID, credits, FieldWorkCredits); err != nil {
		return err
	}
	var existing []catmodel.CatalogCredit
	if err := tx.WithContext(ctx).
		Where("work_id = ? AND source_id = ?", entityID, curatedSourceID).
		Find(&existing).Error; err != nil {
		return err
	}
	prior := make(map[creditKey]catmodel.CatalogCredit, len(existing))
	for _, e := range existing {
		k := creditKey{RoleID: e.RoleID, CreditNameID: e.CreditNameID}
		if e.CharacterID != nil {
			k.CharacterID = *e.CharacterID
		}
		prior[k] = e
	}
	if err := tx.WithContext(ctx).
		Where("work_id = ? AND source_id = ?", entityID, curatedSourceID).
		Delete(&catmodel.CatalogCredit{}).Error; err != nil {
		return err
	}
	if len(credits) == 0 {
		return nil
	}
	source := curatedSourceID
	rows := make([]catmodel.CatalogCredit, 0, len(credits))
	for _, c := range credits {
		was := prior[c.key()]
		row := catmodel.CatalogCredit{
			WorkID: entityID, CreditNameID: c.CreditNameID, RoleID: c.RoleID,
			Note: c.Note, Spoiler: was.Spoiler, ReleaseID: was.ReleaseID, LabelID: was.LabelID,
			SourceID: &source,
		}
		if c.HasSpoiler {
			row.Spoiler = catmodel.SpoilerNone
			if c.Spoiler != nil {
				row.Spoiler = *c.Spoiler
			}
		}
		if c.HasRelease {
			row.ReleaseID = c.ReleaseID
		}
		if c.HasLabel {
			row.LabelID = c.LabelID
		}
		if c.CharacterID != 0 {
			characterID := c.CharacterID
			row.CharacterID = &characterID
		}
		rows = append(rows, row)
	}
	return tx.WithContext(ctx).Create(&rows).Error
}

// rejectUnknownCreditReferences turns a dangling reference into a 422 the
// proposer sees. All three columns are real foreign keys, so without this the
// insert raises 23503 out of the MERGE transaction: the 500 carrying a Postgres
// constraint name goes to the reviewer applying somebody else's proposal, and
// the proposal itself stays in the queue permanently unappliable.
//
// A soft-deleted character needs no clause of its own — CatalogCharacter
// carries gorm.DeletedAt, so a character merged away since the proposal was
// written simply is not found.
func rejectUnknownCreditReferences(ctx context.Context, tx *gorm.DB, credits []workCredit) error {
	if len(credits) == 0 {
		return nil
	}
	roleIDs, nameIDs, characterIDs := distinctCreditRefs(credits)

	var problems []string
	var roles []catmodel.CatalogRole
	if err := tx.WithContext(ctx).Select("id", "is_deprecated").
		Where("id IN ?", roleIDs).Find(&roles).Error; err != nil {
		return err
	}
	known := make([]int64, 0, len(roles))
	var deprecated []int64
	for _, r := range roles {
		known = append(known, r.ID)
		if r.IsDeprecated {
			deprecated = append(deprecated, r.ID)
		}
	}
	if missing := missingIDs(roleIDs, known); len(missing) > 0 {
		problems = append(problems, fmt.Sprintf("no role has id %s", idList(missing)))
	}
	if len(deprecated) > 0 {
		problems = append(problems, fmt.Sprintf(
			"role %s is deprecated: the vocabulary retired it, so it cannot be newly assigned",
			idList(deprecated)))
	}

	var knownNames []int64
	if err := tx.WithContext(ctx).Model(&catmodel.CatalogCreditName{}).
		Where("id IN ?", nameIDs).Pluck("id", &knownNames).Error; err != nil {
		return err
	}
	if missing := missingIDs(nameIDs, knownNames); len(missing) > 0 {
		problems = append(problems, fmt.Sprintf("no credited name has id %s", idList(missing)))
	}

	if len(characterIDs) > 0 {
		var knownCharacters []int64
		if err := tx.WithContext(ctx).Model(&catmodel.CatalogCharacter{}).
			Where("id IN ?", characterIDs).Pluck("id", &knownCharacters).Error; err != nil {
			return err
		}
		if missing := missingIDs(characterIDs, knownCharacters); len(missing) > 0 {
			problems = append(problems, fmt.Sprintf("no character has id %s", idList(missing)))
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return &editing.ValidationError{Key: FieldWorkCredits, Reason: strings.Join(problems, "; ")}
}

func distinctCreditRefs(credits []workCredit) (roles, names, characters []int64) {
	seen := make(map[[2]int64]struct{}, len(credits)*3)
	add := func(kind, id int64, into *[]int64) {
		if id == 0 {
			return
		}
		if _, dup := seen[[2]int64{kind, id}]; dup {
			return
		}
		seen[[2]int64{kind, id}] = struct{}{}
		*into = append(*into, id)
	}
	for _, c := range credits {
		add(0, c.RoleID, &roles)
		add(1, c.CreditNameID, &names)
		add(2, c.CharacterID, &characters)
	}
	return roles, names, characters
}

func missingIDs(want, have []int64) []int64 {
	found := make(map[int64]struct{}, len(have))
	for _, id := range have {
		found[id] = struct{}{}
	}
	var out []int64
	for _, id := range want {
		if _, ok := found[id]; !ok {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

func idList(ids []int64) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	return strings.Join(parts, ", ")
}

// rejectUpstreamCreditCollisions makes an unwinnable insert an explicit 422.
// uq_catalog_credit does not carry source_id, so the curated lane and every
// importer share one key. Widening the index was rejected: two rows for one
// credit means the read faces need a fold, and R2a had just shown that a fold
// gets missed in half its call sites. The alternatives here are a raw 23505 out
// of the merge or the importers' own ON CONFLICT DO NOTHING, which saves the
// proposal and drops the row.
func rejectUpstreamCreditCollisions(ctx context.Context, tx *gorm.DB, entityID int64, credits []workCredit) error {
	if len(credits) == 0 {
		return nil
	}
	tuples := make([][]any, 0, len(credits))
	for _, c := range credits {
		tuples = append(tuples, []any{c.RoleID, c.CreditNameID, c.CharacterID})
	}
	var clash []struct {
		Name      string `gorm:"column:name"`
		RoleName  string `gorm:"column:role_name"`
		Character string `gorm:"column:character_nm"`
	}
	if err := tx.WithContext(ctx).Raw(`SELECT cn.name, COALESCE(NULLIF(ro.name_cn, ''), ro.key) AS role_name,
		COALESCE(ch.display_name, '') AS character_nm
		FROM catalog_credit c
		JOIN catalog_credit_name cn ON cn.id = c.credit_name_id
		JOIN catalog_role ro ON ro.id = c.role_id
		LEFT JOIN catalog_character ch ON ch.id = c.character_id
		WHERE c.work_id = ? AND c.source_id IS DISTINCT FROM ?
		  AND (c.role_id, c.credit_name_id, COALESCE(c.character_id, 0)) IN ?
		ORDER BY c.role_id, c.credit_name_id, COALESCE(c.character_id, 0)`,
		entityID, curatedSourceID, tuples).Scan(&clash).Error; err != nil {
		return err
	}
	if len(clash) == 0 {
		return nil
	}
	who := clash[0].Name
	if clash[0].Character != "" {
		who += " / " + clash[0].Character
	}
	return &editing.ValidationError{
		Key: FieldWorkCredits,
		Reason: fmt.Sprintf("%q (%s) is already provided upstream for this work; "+
			"to hide it use %s instead of re-adding it",
			who, clash[0].RoleName, FieldWorkCreditsSuppr),
	}
}

// CreditIdentity is the frozen row identity of catalog.work.credits (doc 21
// D10: registered means never redefined). It is exactly uq_catalog_credit minus
// work_id, so its uniqueness is held by an index rather than by a convention.
//
// role_id is in the key and cannot be dropped: 91,549 (work, credit_name) pairs
// span more than one role in production, so a key without it would turn "this
// credit is wrong" into "this person had nothing to do with this work" in every
// one of those places, invisibly. role_id is used rather than role.key because
// the vocabulary carries renameable non-ASCII keys while the id is pinned by a
// million foreign keys.
func CreditIdentity(roleID, creditNameID, characterID int64) string {
	return fmt.Sprintf("credit:%d:%d:%d", roleID, creditNameID, characterID)
}

// CreditIdentitySQL restates CreditIdentity for a catalog_credit alias — the one
// place the two definitions could drift apart, which is why
// TestCreditIdentitySQLMatchesGo recomputes both over real rows.
func CreditIdentitySQL(alias string) string {
	return fmt.Sprintf(`('credit:' || %[1]s.role_id || ':' || %[1]s.credit_name_id || ':' || COALESCE(%[1]s.character_id, 0))`, alias)
}

// NotSuppressedCreditSQL is the read-path predicate, correlated on a
// catalog_credit alias.
func NotSuppressedCreditSQL(alias string) string {
	return fmt.Sprintf(
		`NOT EXISTS (SELECT 1 FROM edit_suppressed_row esr
			WHERE esr.entity_type = '%s' AND esr.field_key = '%s'
			  AND esr.entity_id = %s.work_id AND esr.identity_key = %s)`,
		TypeWork, FieldWorkCredits, alias, CreditIdentitySQL(alias))
}

func creditKeyCheck(key string) error {
	parts := strings.Split(key, ":")
	if len(parts) != 4 || parts[0] != "credit" {
		return fmt.Errorf("%q is not an identity key of this field: expected credit:<role_id>:<credit_name_id>:<character_id>", key)
	}
	ids := make([]int64, 3)
	for i, p := range parts[1:] {
		n, err := strconv.ParseInt(p, 10, 64)
		if err != nil || n < 0 || strconv.FormatInt(n, 10) != p {
			return fmt.Errorf("%q is not an identity key of this field: %q is not a canonical id", key, p)
		}
		ids[i] = n
	}
	if ids[0] <= 0 || ids[1] <= 0 {
		return fmt.Errorf("%q is not an identity key of this field: role_id and credit_name_id must be positive", key)
	}
	return nil
}

func loadSuppressedCredits(ctx context.Context, db *gorm.DB, workID int64) ([]any, error) {
	return editing.LoadSuppressedValue(ctx, db, TypeWork, workID, FieldWorkCredits)
}

func loadCredits(ctx context.Context, db *gorm.DB, workID int64) ([]any, error) {
	var rows []catmodel.CatalogCredit
	if err := db.WithContext(ctx).
		Where("work_id = ? AND source_id = ?", workID, curatedSourceID).
		Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows))
	for _, r := range rows {
		el := map[string]any{"role_id": r.RoleID, "credit_name_id": r.CreditNameID}
		if r.CharacterID != nil {
			el["character_id"] = *r.CharacterID
		}
		if r.Note != "" {
			el["note"] = r.Note
		}
		if r.Spoiler != catmodel.SpoilerNone {
			el["spoiler"] = int64(r.Spoiler)
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

func optionalRef(obj map[string]any, key string, index int) (bool, *int64, error) {
	raw, present := obj[key]
	if !present {
		return false, nil, nil
	}
	if raw == nil {
		return true, nil, nil
	}
	id, err := objInt(obj, key, index, true)
	if err != nil {
		return true, nil, err
	}
	if id <= 0 {
		return true, nil, fmt.Errorf("element %d: %s must be a positive id; send null to clear it", index, key)
	}
	return true, &id, nil
}

// rejectUnknownCreditScopes refuses a release of another work and a company
// that does not exist, as a 422 the proposer sees instead of a foreign-key 500.
func rejectUnknownCreditScopes(ctx context.Context, tx *gorm.DB, workID int64, credits []workCredit, field string) error {
	var releases, labels []int64
	for _, c := range credits {
		if c.ReleaseID != nil {
			releases = append(releases, *c.ReleaseID)
		}
		if c.LabelID != nil {
			labels = append(labels, *c.LabelID)
		}
	}
	if len(releases) > 0 {
		releases = uniqueIDs(releases)
		var own []int64
		if err := tx.WithContext(ctx).Unscoped().Model(&catmodel.CatalogRelease{}).
			Where("id IN ? AND work_id = ?", releases, workID).Pluck("id", &own).Error; err != nil {
			return err
		}
		if missing := missingIDs(releases, own); len(missing) > 0 {
			return &editing.ValidationError{Key: field,
				Reason: fmt.Sprintf("release %s is not a release of this work", idList(missing))}
		}
	}
	return assertEntitiesExist(ctx, tx, &catmodel.CatalogLabel{}, uniqueIDs(labels), field)
}
