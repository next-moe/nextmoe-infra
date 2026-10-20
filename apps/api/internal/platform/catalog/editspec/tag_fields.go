package editspec

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"api/internal/platform/authz"
	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/catalog/perm"
	"api/internal/platform/editing"

	"gorm.io/gorm"
)

const (
	FieldTagName         = "catalog.tag.name"
	FieldTagKind         = "catalog.tag.kind"
	FieldTagTier         = "catalog.tag.tier"
	FieldTagSexual       = "catalog.tag.sexual"
	FieldTagNote         = "catalog.tag.note"
	FieldTagParents      = "catalog.tag.parents"
	FieldTagParentsSuppr = FieldTagParents + editing.SuppressedFieldSuffix

	TagTag = "tag"

	// p100 is 4 parents on one tag (2026-10).
	maxTagParents = 20
)

var tagTable = columnTable{table: catmodel.CatalogTag{}.TableName(), newModel: func() any { return &catmodel.CatalogTag{} }}

func reviewerOnlyPolicy(review authz.Permission) *editing.Policy {
	return &editing.Policy{
		Propose:   editing.ProposePerm(string(review)),
		Review:    editing.ReviewPerm(string(review)),
		Automerge: editing.AutomergeNever,
	}
}

func ParentIdentity(parentID int64) string {
	return "parent:" + strconv.FormatInt(parentID, 10)
}

func parentKeyCheck(key string) error {
	parts := strings.Split(key, ":")
	if len(parts) != 2 || parts[0] != "parent" {
		return fmt.Errorf("%q is not an identity key of this field: expected parent:<id>", key)
	}
	n, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || strconv.FormatInt(n, 10) != parts[1] || n <= 0 {
		return fmt.Errorf("%q is not an identity key of this field: %q is not a canonical id", key, parts[1])
	}
	return nil
}

// TagParentSuppressedSQL is true when the (tag, parent) edge named by the two
// expressions is suppressed. Suppression is enforced on write: the edit
// deletes the importer's edge and tagparents.Sync must skip these pairs.
func TagParentSuppressedSQL(tagIDExpr, parentIDExpr string) string {
	return fmt.Sprintf(`EXISTS (SELECT 1 FROM edit_suppressed_row esr
		WHERE esr.entity_type = '%s' AND esr.field_key = '%s'
		  AND esr.entity_id = %s AND esr.identity_key = ('parent:' || %s))`,
		TypeTag, FieldTagParents, tagIDExpr, parentIDExpr)
}

func tagScalarFields() []editing.FieldSpec {
	name := tagTable.emptyableText(FieldTagName, "name", maxNameRunes, editing.DiffHintInline)
	name.Validate = validateName
	name.Apply = applyTagName

	tier := tagTable.codedEnum(FieldTagTier, "tier", "tier", 0, false,
		catmodel.TagTierCore, catmodel.TagTierLongtail, catmodel.TagTierHidden)
	tier.Policy = reviewerOnlyPolicy(perm.EditTaxonomyReview)

	sexual := editing.FieldSpec{
		Key: FieldTagSexual, Kind: editing.KindEnum, DiffHint: editing.DiffHintInline,
		Validate:   validateBool,
		Apply:      tagTable.apply("sexual", asBool),
		Provenance: tagTable.provenance("sexual"),
		Policy:     reviewerOnlyPolicy(perm.EditTaxonomyReview),
	}

	return []editing.FieldSpec{
		name,
		tagTable.codedEnum(FieldTagKind, "kind", "tag_kind", 0, false, catmodel.TagKindContent, catmodel.TagKindMeta),
		tier,
		sexual,
		tagTable.emptyableText(FieldTagNote, "note", maxNoteRunes, editing.DiffHintLines),
	}
}

// name is the canonical key tagcanon resolves a new source spelling against,
// so a rename onto another tag's name would silently fuse the two vocabularies.
func applyTagName(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	s, err := asString(value)
	if err != nil {
		return fmt.Errorf("editspec: tag name: %w", err)
	}
	name := strings.TrimSpace(s.(string))
	var clash []int64
	if err := tx.WithContext(ctx).Raw(`SELECT id FROM catalog_tag WHERE name = ? AND id <> ?`, name, entityID).
		Scan(&clash).Error; err != nil {
		return err
	}
	if len(clash) > 0 {
		return &editing.ValidationError{Key: FieldTagName,
			Reason: fmt.Sprintf("tag %d is already named %q; merge the two tags instead of renaming one onto the other", clash[0], name)}
	}
	return tagTable.apply("name", func(any) (any, error) { return name, nil })(ctx, tx, entityID, value)
}

func tagParentsFieldSpec() editing.FieldSpec {
	return editing.FieldSpec{
		Key: FieldTagParents, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
		MaxElements:   maxTagParents,
		MaxSuppressed: maxTagParents,
		Identity: &editing.IdentitySpec{Segments: 2, KeyCheck: parentKeyCheck,
			Refs: []editing.IdentityRef{{Segment: 2, EntityTag: TagTag}}},
		Value: &editing.ValueSpec{Element: &editing.ElementSpec{Type: "ref"}},
		Validate: func(v any) error {
			_, err := parseParentIDs(v)
			return err
		},
		Apply: applyTagParents,
	}
}

func parseParentIDs(v any) ([]int64, error) {
	ids, err := parseIDList(v, "parent ids")
	if err != nil {
		return nil, err
	}
	if len(ids) > maxTagParents {
		return nil, fmt.Errorf("at most %d parents", maxTagParents)
	}
	return ids, nil
}

// applyTagParents owns the curated edges (origin 1). An element naming an edge
// the VNDB sync already wrote endorses it in place, so the sync can no longer
// prune it; dropping a curated element hands the edge back to the next sync if
// VNDB still has it. Hiding a VNDB edge is the .suppressed companion's job.
func applyTagParents(ctx context.Context, tx *gorm.DB, tagID int64, value any) error {
	ids, err := parseParentIDs(value)
	if err != nil {
		return fmt.Errorf("editspec: tag parents: %w", err)
	}
	if err := firstEntity(ctx, tx, &catmodel.CatalogTag{}, tagID, "id"); err != nil {
		return err
	}
	if err := assertExistingIDs(ctx, tx, "catalog_tag", FieldTagParents, "tag", tagID, ids); err != nil {
		return err
	}
	del := tx.WithContext(ctx).Where("tag_id = ? AND origin = ?", tagID, catmodel.TagParentOriginCurated)
	if len(ids) > 0 {
		del = del.Where("parent_id NOT IN ?", ids)
	}
	if err := del.Delete(&catmodel.CatalogTagParent{}).Error; err != nil {
		return err
	}
	for _, p := range ids {
		if err := tx.WithContext(ctx).Exec(`
			INSERT INTO catalog_tag_parent (tag_id, parent_id, origin, note, created_at)
			VALUES (?, ?, ?, '', now())
			ON CONFLICT (tag_id, parent_id) DO UPDATE SET origin = EXCLUDED.origin`,
			tagID, p, catmodel.TagParentOriginCurated).Error; err != nil {
			return err
		}
	}
	if err := rejectParentCycle(ctx, tx, "catalog_tag_parent", "tag_id", FieldTagParents, tagID); err != nil {
		return err
	}
	return rejectSuppressedCuratedParents(ctx, tx, tagParentEdges, tagID, FieldTagParents)
}

func tagParentSuppressionSpec(parents editing.FieldSpec) editing.FieldSpec {
	spec := editing.SuppressedFieldSpec(TypeTag, parents)
	reconcile := spec.Apply
	spec.Apply = func(ctx context.Context, tx *gorm.DB, tagID int64, value any) error {
		if err := reconcile(ctx, tx, tagID, value); err != nil {
			return err
		}
		if err := rejectSuppressedCuratedParents(ctx, tx, tagParentEdges, tagID, FieldTagParentsSuppr); err != nil {
			return err
		}
		return tx.WithContext(ctx).Exec(`DELETE FROM catalog_tag_parent p
			WHERE p.tag_id = ? AND p.origin = ? AND `+TagParentSuppressedSQL("p.tag_id", "p.parent_id"),
			tagID, catmodel.TagParentOriginVNDB).Error
	}
	return spec
}

type parentEdges struct {
	table, childColumn, entityLabel, field string
	curatedOrigin                          int16
	suppressedSQL                          func(childExpr, parentExpr string) string
}

var tagParentEdges = parentEdges{
	table: "catalog_tag_parent", childColumn: "tag_id", entityLabel: "tag", field: FieldTagParents,
	curatedOrigin: catmodel.TagParentOriginCurated, suppressedSQL: TagParentSuppressedSQL,
}

// rejectSuppressedCuratedParents makes a curated edge and a suppression of the
// same edge an explicit 422 from whichever field created the contradiction.
func rejectSuppressedCuratedParents(ctx context.Context, tx *gorm.DB, e parentEdges, childID int64, field string) error {
	var clash []int64
	if err := tx.WithContext(ctx).Raw(`
		SELECT p.parent_id FROM `+e.table+` p
		WHERE p.`+e.childColumn+` = ? AND p.origin = ? AND `+e.suppressedSQL("p."+e.childColumn, "p.parent_id")+`
		ORDER BY p.parent_id`, childID, e.curatedOrigin).Scan(&clash).Error; err != nil {
		return err
	}
	if len(clash) == 0 {
		return nil
	}
	reason := fmt.Sprintf("parent %d is suppressed on this %s; lift it from %s in an edit of its own before adding it",
		clash[0], e.entityLabel, e.field+editing.SuppressedFieldSuffix)
	if field != e.field {
		reason = fmt.Sprintf("parent %d is one of this %s's curated parents in %s; remove it there instead of suppressing it",
			clash[0], e.entityLabel, e.field)
	}
	return &editing.ValidationError{Key: field, Reason: reason}
}

func rejectParentCycle(ctx context.Context, tx *gorm.DB, table, childColumn, field string, childID int64) error {
	var looped bool
	if err := tx.WithContext(ctx).Raw(`
		WITH RECURSIVE up(id) AS (
			SELECT parent_id FROM `+table+` WHERE `+childColumn+` = ?
			UNION
			SELECT p.parent_id FROM `+table+` p JOIN up ON p.`+childColumn+` = up.id
		)
		SELECT EXISTS (SELECT 1 FROM up WHERE id = ?)`, childID, childID).Scan(&looped).Error; err != nil {
		return err
	}
	if looped {
		return &editing.ValidationError{Key: field, Reason: "these parents would make the entry its own ancestor"}
	}
	return nil
}

func assertExistingIDs(ctx context.Context, tx *gorm.DB, table, field, label string, self int64, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	var found []int64
	if err := tx.WithContext(ctx).Raw(`SELECT id FROM `+table+` WHERE id IN ?`, ids).Scan(&found).Error; err != nil {
		return err
	}
	have := make(map[int64]struct{}, len(found))
	for _, id := range found {
		have[id] = struct{}{}
	}
	for _, id := range ids {
		if id == self {
			return &editing.ValidationError{Key: field, Reason: fmt.Sprintf("a %s cannot be its own parent", label)}
		}
		if _, ok := have[id]; !ok {
			return &editing.ValidationError{Key: field, Reason: fmt.Sprintf("%s %d does not exist", label, id)}
		}
	}
	return nil
}

func loadCuratedTagParents(ctx context.Context, db *gorm.DB, tagID int64) ([]any, error) {
	var ids []int64
	if err := db.WithContext(ctx).Raw(`SELECT parent_id FROM catalog_tag_parent
		WHERE tag_id = ? AND origin = ? ORDER BY parent_id`, tagID, catmodel.TagParentOriginCurated).Scan(&ids).Error; err != nil {
		return nil, err
	}
	out := make([]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, id)
	}
	return out, nil
}

func loadSuppressedTagParents(ctx context.Context, db *gorm.DB, tagID int64) ([]any, error) {
	return editing.LoadSuppressedValue(ctx, db, TypeTag, tagID, FieldTagParents)
}
