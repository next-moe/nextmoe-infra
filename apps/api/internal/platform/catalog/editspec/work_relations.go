package editspec

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"api/internal/platform/editing"

	"gorm.io/gorm"
)

const (
	FieldWorkRelations      = "catalog.work.relations"
	FieldWorkRelationsSuppr = FieldWorkRelations + editing.SuppressedFieldSuffix
)

// A work touches at most 323 relation rows in production (census 2026-10-20,
// both directions counted).
const (
	maxWorkRelationElements  = 400
	maxWorkRelationSuppress  = 700
	maxWorkRelationNoteRunes = 300
)

// workRelationTokens is the closed work_relation vocabulary: the domain-0 rows
// of catalog_relation_type, which are seeded and never renumbered.
var workRelationTokens = []string{
	"adaptation_of", "sequel_of", "side_story_of", "fandisc_of", "collects", "remake_of",
	"same_series", "same_setting", "crossover_with", "shares_character",
	"alternative_setting", "alternative_version",
}

var workRelationTokenSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(workRelationTokens))
	for _, t := range workRelationTokens {
		m[t] = struct{}{}
	}
	return m
}()

func init() {
	upstreamLists[TypeWork] = append(upstreamLists[TypeWork], upstreamList{
		field: FieldWorkRelations, maxSuppressed: maxWorkRelationSuppress,
		identity: editing.IdentitySpec{
			Segments: 4, KeyCheck: workRelationKeyCheck,
			Refs: []editing.IdentityRef{{Segment: 4, EntityTag: TagWork}},
		},
		guard: refuseCuratedRelationSuppression,
	})
}

type workRelation struct {
	WorkID   int64
	Relation string
	Reverse  bool
	Note     string
}

func workRelationsFieldSpec() editing.FieldSpec {
	return editing.FieldSpec{
		Key: FieldWorkRelations, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
		MaxElements: maxWorkRelationElements,
		Value: &editing.ValueSpec{Element: &editing.ElementSpec{
			Type: "object",
			Members: []editing.ElementMember{
				{Key: "work_id", Type: "ref"},
				{Key: "relation", Type: "enum", Vocabulary: "work_relation"},
				{Key: "reverse", Type: "bool", Nullable: true},
				{Key: "note", Type: "text", Nullable: true},
			},
		}},
		Validate: validateWorkRelations,
		Apply:    applyWorkRelations,
	}
}

func parseWorkRelations(v any) ([]workRelation, error) {
	arr, err := asArrayN(v, "relation objects", maxWorkRelationElements)
	if err != nil {
		return nil, err
	}
	out := make([]workRelation, 0, len(arr))
	seen := make(map[workRelation]struct{}, len(arr))
	for i, el := range arr {
		obj, err := asObject(el, i, "work_id", "relation", "reverse", "note")
		if err != nil {
			return nil, err
		}
		workID, err := objInt(obj, "work_id", i, true)
		if err != nil {
			return nil, err
		}
		if workID <= 0 {
			return nil, fmt.Errorf("element %d: work_id must be a positive id", i)
		}
		relation, err := objString(obj, "relation", i, true, 64)
		if err != nil {
			return nil, err
		}
		if _, ok := workRelationTokenSet[relation]; !ok {
			return nil, fmt.Errorf("element %d: %q is not a work relation", i, relation)
		}
		var reverse bool
		if raw, present := obj["reverse"]; present && raw != nil {
			if reverse, err = objBool(obj, "reverse", i); err != nil {
				return nil, err
			}
		}
		note, err := objString(obj, "note", i, false, maxWorkRelationNoteRunes)
		if err != nil {
			return nil, err
		}
		key := workRelation{WorkID: workID, Relation: relation, Reverse: reverse}
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("element %d: duplicate (work_id, relation, reverse)", i)
		}
		seen[key] = struct{}{}
		key.Note = note
		out = append(out, key)
	}
	return out, nil
}

func validateWorkRelations(v any) error { _, err := parseWorkRelations(v); return err }

type relationType struct {
	ID          int64  `gorm:"column:id"`
	Key         string `gorm:"column:key"`
	IsSymmetric bool   `gorm:"column:is_symmetric"`
}

func loadWorkRelationTypes(ctx context.Context, db *gorm.DB) (map[string]relationType, error) {
	var rows []relationType
	if err := db.WithContext(ctx).Raw(`SELECT id, key, is_symmetric FROM catalog_relation_type
		WHERE domain = 0 AND NOT is_deprecated`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]relationType, len(rows))
	for _, r := range rows {
		out[r.Key] = r
	}
	return out, nil
}

type relationEdge struct {
	A, B, Type int64
	Note       string
}

// edgeOf stores a relation the way the importers do: a symmetric edge once,
// smaller id first; a directed one from the work that "is" the relation.
func edgeOf(workID int64, r workRelation, t relationType) relationEdge {
	a, b := workID, r.WorkID
	switch {
	case t.IsSymmetric:
		if a > b {
			a, b = b, a
		}
	case r.Reverse:
		a, b = b, a
	}
	return relationEdge{A: a, B: b, Type: t.ID, Note: r.Note}
}

func applyWorkRelations(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	rels, err := parseWorkRelations(value)
	if err != nil {
		return fmt.Errorf("editspec: relations: %w", err)
	}
	if err := assertWorkExists(ctx, tx, entityID); err != nil {
		return err
	}
	types, err := loadWorkRelationTypes(ctx, tx)
	if err != nil {
		return err
	}
	targets := make([]int64, 0, len(rels))
	for _, r := range rels {
		if r.WorkID == entityID {
			return &editing.ValidationError{Key: FieldWorkRelations, Reason: "a work cannot be related to itself"}
		}
		if _, ok := types[r.Relation]; !ok {
			return &editing.ValidationError{Key: FieldWorkRelations,
				Reason: fmt.Sprintf("relation %q is not an active work relation", r.Relation)}
		}
		targets = append(targets, r.WorkID)
	}
	if len(targets) > 0 {
		var live []int64
		if err := tx.WithContext(ctx).Raw(`SELECT id FROM catalog_work WHERE id IN ? AND deleted_at IS NULL`,
			uniqueIDs(targets)).Scan(&live).Error; err != nil {
			return err
		}
		if missing := missingIDs(uniqueIDs(targets), live); len(missing) > 0 {
			return &editing.ValidationError{Key: FieldWorkRelations,
				Reason: fmt.Sprintf("work %s does not exist", idList(missing))}
		}
	}
	edges := make([]relationEdge, 0, len(rels))
	seen := make(map[[3]int64]struct{}, len(rels))
	for _, r := range rels {
		e := edgeOf(entityID, r, types[r.Relation])
		k := [3]int64{e.A, e.B, e.Type}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		edges = append(edges, e)
	}
	for _, e := range edges {
		var upstream int64
		if err := tx.WithContext(ctx).Raw(`SELECT count(*) FROM catalog_work_relation r
			WHERE r.relation_type_id = ? AND `+NotCuratedLaneSQL("r.source_id")+`
			  AND ((r.a_work_id = ? AND r.b_work_id = ?) OR (r.a_work_id = ? AND r.b_work_id = ?))`,
			e.Type, e.A, e.B, e.B, e.A).Scan(&upstream).Error; err != nil {
			return err
		}
		if upstream > 0 {
			return &editing.ValidationError{Key: FieldWorkRelations,
				Reason: fmt.Sprintf("relation %d between works %d and %d is already provided upstream; "+
					"to hide it use %s instead of re-adding it", e.Type, e.A, e.B, FieldWorkRelationsSuppr)}
		}
		var hidden int64
		if err := tx.WithContext(ctx).Raw(`SELECT count(*) FROM (SELECT ?::bigint AS a_work_id, ?::bigint AS b_work_id,
			?::bigint AS relation_type_id) r WHERE NOT `+NotSuppressedWorkRelationSQL("r"),
			e.A, e.B, e.Type).Scan(&hidden).Error; err != nil {
			return err
		}
		if hidden > 0 {
			return &editing.ValidationError{Key: FieldWorkRelations,
				Reason: fmt.Sprintf("relation %d between works %d and %d is suppressed on one of the two works; "+
					"lift the suppression in an edit of its own first", e.Type, e.A, e.B)}
		}
	}
	if err := tx.WithContext(ctx).Exec(`DELETE FROM catalog_work_relation
		WHERE `+CuratedLaneSQL("source_id")+` AND (a_work_id = ? OR b_work_id = ?)`,
		entityID, entityID).Error; err != nil {
		return err
	}
	source := curatedSourceID
	for _, e := range edges {
		if err := tx.WithContext(ctx).Exec(`INSERT INTO catalog_work_relation
			  (a_work_id, b_work_id, relation_type_id, note, source_id, created_at)
			VALUES (?, ?, ?, ?, ?, now()) ON CONFLICT DO NOTHING`,
			e.A, e.B, e.Type, e.Note, source).Error; err != nil {
			return err
		}
	}
	return nil
}

// loadWorkRelations is the curated lane seen from this work: an edge added on
// the other work's page shows here too, reversed, so a save from either side
// echoes it back instead of deleting it.
func loadWorkRelations(ctx context.Context, db *gorm.DB, workID int64) ([]any, error) {
	var rows []struct {
		A           int64  `gorm:"column:a_work_id"`
		B           int64  `gorm:"column:b_work_id"`
		Key         string `gorm:"column:key"`
		IsSymmetric bool   `gorm:"column:is_symmetric"`
		Note        string `gorm:"column:note"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT r.a_work_id, r.b_work_id, rt.key, rt.is_symmetric, r.note
		FROM catalog_work_relation r JOIN catalog_relation_type rt ON rt.id = r.relation_type_id
		WHERE `+CuratedLaneSQL("r.source_id")+` AND (r.a_work_id = ? OR r.b_work_id = ?)`,
		workID, workID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	type el struct {
		other   int64
		key     string
		reverse bool
		note    string
	}
	els := make([]el, 0, len(rows))
	for _, r := range rows {
		e := el{other: r.B, key: r.Key, note: r.Note}
		if r.B == workID {
			e.other = r.A
			e.reverse = !r.IsSymmetric
		}
		els = append(els, e)
	}
	sort.Slice(els, func(i, j int) bool {
		if els[i].key != els[j].key {
			return els[i].key < els[j].key
		}
		if els[i].other != els[j].other {
			return els[i].other < els[j].other
		}
		return !els[i].reverse && els[j].reverse
	})
	out := make([]any, 0, len(els))
	for _, e := range els {
		m := map[string]any{"work_id": e.other, "relation": e.key}
		if e.reverse {
			m["reverse"] = true
		}
		if e.note != "" {
			m["note"] = e.note
		}
		out = append(out, m)
	}
	return out, nil
}

// WorkRelationIdentity keys an edge from one end: f when this work is the a
// side, r when it is the b side.
func WorkRelationIdentity(relationTypeID int64, reverse bool, otherWorkID int64) string {
	dir := "f"
	if reverse {
		dir = "r"
	}
	return fmt.Sprintf("relation:%d:%s:%d", relationTypeID, dir, otherWorkID)
}

func workRelationKeyCheck(key string) error {
	parts, err := identityParts(key, "relation", 4)
	if err != nil {
		return err
	}
	if _, err := strconv.ParseInt(parts[1], 10, 64); err != nil {
		return fmt.Errorf("%q is not an identity key of this field: %q is not a relation type id", key, parts[1])
	}
	if parts[2] != "f" && parts[2] != "r" {
		return fmt.Errorf("%q is not an identity key of this field: direction must be f or r", key)
	}
	if _, err := strconv.ParseInt(parts[3], 10, 64); err != nil {
		return fmt.Errorf("%q is not an identity key of this field: %q is not a work id", key, parts[3])
	}
	return nil
}

// WorkRelationIdentitySQL keys a row of a derived table that lists every edge
// from both ends: owner_id, other_id, relation_type_id and dir ('f' or 'r').
func WorkRelationIdentitySQL(alias string) string {
	return fmt.Sprintf(`('relation:' || %[1]s.relation_type_id || ':' || %[1]s.dir || ':' || %[1]s.other_id)`, alias)
}

// NotSuppressedWorkRelationSQL is correlated on a catalog_work_relation alias.
// An edge hidden from either end is hidden from both: both pages render the
// same row.
func NotSuppressedWorkRelationSQL(alias string) string {
	return fmt.Sprintf(`NOT EXISTS (SELECT 1 FROM edit_suppressed_row esr
		WHERE esr.entity_type = '%[2]s' AND esr.field_key = '%[3]s'
		  AND ((esr.entity_id = %[1]s.a_work_id
		        AND esr.identity_key = 'relation:' || %[1]s.relation_type_id || ':f:' || %[1]s.b_work_id)
		    OR (esr.entity_id = %[1]s.b_work_id
		        AND esr.identity_key = 'relation:' || %[1]s.relation_type_id || ':r:' || %[1]s.a_work_id)))`,
		alias, TypeWork, FieldWorkRelations)
}

func refuseCuratedRelationSuppression(ctx context.Context, tx *gorm.DB, entityID int64, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	var curated []string
	if err := tx.WithContext(ctx).Raw(`SELECT k FROM (
		  SELECT 'relation:' || r.relation_type_id || ':f:' || r.b_work_id AS k FROM catalog_work_relation r
		  WHERE r.a_work_id = ? AND `+CuratedLaneSQL("r.source_id")+`
		  UNION ALL
		  SELECT 'relation:' || r.relation_type_id || ':r:' || r.a_work_id FROM catalog_work_relation r
		  WHERE r.b_work_id = ? AND `+CuratedLaneSQL("r.source_id")+`
		) c WHERE k IN ?`, entityID, entityID, keys).Scan(&curated).Error; err != nil {
		return err
	}
	if len(curated) == 0 {
		return nil
	}
	return &editing.ValidationError{Key: FieldWorkRelationsSuppr,
		Reason: fmt.Sprintf("%s is a curated relation; remove it from %s instead of suppressing it",
			curated[0], FieldWorkRelations)}
}
