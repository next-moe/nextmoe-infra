package identity

import (
	"context"
	"fmt"
	"slices"
)

// LegacySchema is where the legacy catalog lives until P5 (D1); a variable so
// that tests stand up their own instead of the shared public tables. Everything
// here only reads it, and answers "nothing" once it is gone.
var LegacySchema = "public"

var legacyTables = map[string]struct {
	table      string
	entityType int
	// alive is false for a row the legacy catalog no longer serves.
	alive string
}{
	"work":         {"catalog_work", 5, "t.deleted_at IS NULL"},
	"person":       {"catalog_person", 0, "t.deleted_at IS NULL"},
	"person_alias": {"catalog_credit_name", 1, "TRUE"},
	"org":          {"catalog_label", 3, "t.deleted_at IS NULL"},
	"release":      {"catalog_release", 6, "t.deleted_at IS NULL"},
}

var legacyMedia = map[string]int{"game": 1, "manga": 2, "novel": 3, "anime": 4, "audio": 5}

func live(alive string, entityType int) string {
	return alive + fmt.Sprintf(` AND NOT EXISTS (SELECT 1 FROM %s.catalog_redirect r WHERE r.entity_type = %d AND r.old_id = t.id AND r.revoked_at IS NULL)`, LegacySchema, entityType)
}

func legacyPresent(ctx context.Context, q Querier, table string) (bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT 1 WHERE to_regclass($1) IS NOT NULL AND to_regclass($2) IS NOT NULL`, LegacySchema+"."+table, LegacySchema+".catalog_redirect")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	ok := rows.Next()
	return ok, rows.Err()
}

func findLegacy(ctx context.Context, q Querier, entity, medium string, keys []string) ([]Candidate, error) {
	lt, ok := legacyTables[entity]
	if !ok {
		return nil, nil
	}
	if ok, err := legacyPresent(ctx, q, lt.table); err != nil || !ok {
		return nil, err
	}
	alive := live(lt.alive, lt.entityType)
	switch entity {
	case "work":
		return legacyWorks(ctx, q, medium, keys, alive)
	case "person_alias":
		// A credit name nothing credits is not migrated (D19).
		return legacyNames(ctx, q, `SELECT DISTINCT t.id, t.name FROM `+LegacySchema+`.catalog_credit_name t
			LEFT JOIN `+LegacySchema+`.catalog_name_alias a ON a.credit_name_id = t.id
			WHERE (replace(t.name_norm, ' ', '') = ANY($1) OR replace(a.name_norm, ' ', '') = ANY($1))
			AND EXISTS (SELECT 1 FROM `+LegacySchema+`.catalog_credit c WHERE c.credit_name_id = t.id) AND `+alive+` ORDER BY 1`, keys)
	case "org":
		return legacyNames(ctx, q, `SELECT DISTINCT t.id, t.display_name FROM `+LegacySchema+`.catalog_label t
			LEFT JOIN `+LegacySchema+`.catalog_label_alias a ON a.label_id = t.id
			WHERE (replace(t.display_name_norm, ' ', '') = ANY($1) OR replace(a.name_norm, ' ', '') = ANY($1)) AND `+alive+` ORDER BY 1`, keys)
	}
	return nil, nil
}

// legacyWorks compares keys in Go: the key drops punctuation Postgres classes
// depend on the collation for, and a book medium holds about a thousand works.
func legacyWorks(ctx context.Context, q Querier, medium string, keys []string, alive string) ([]Candidate, error) {
	m, ok := legacyMedia[medium]
	if !ok {
		return nil, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT t.id, t.display_name, x.title FROM `+LegacySchema+`.catalog_work t
		LEFT JOIN `+LegacySchema+`.catalog_work_title x ON x.work_id = t.id AND x.mt_model IS NULL
		WHERE t.medium_id = $1 AND `+alive+` ORDER BY t.id`, m)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		var id int64
		var label string
		var title *string
		if err := rows.Scan(&id, &label, &title); err != nil {
			return nil, err
		}
		if len(out) > 0 && out[len(out)-1].ID == id {
			continue
		}
		hit := func(s string) bool {
			return slices.ContainsFunc(TitleKeys(s), func(k string) bool { return slices.Contains(keys, k) })
		}
		if hit(label) || (title != nil && hit(*title)) {
			out = append(out, Candidate{OriginLegacy, id, label})
		}
	}
	return out, rows.Err()
}

func legacyNames(ctx context.Context, q Querier, query string, keys []string) ([]Candidate, error) {
	rows, err := q.QueryContext(ctx, query, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		c := Candidate{Origin: OriginLegacy}
		if err := rows.Scan(&c.ID, &c.Label); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Adoptable refuses a legacy id a new canon entity may not take over: one the
// legacy catalog does not serve, one merged away, or a work of another medium.
func Adoptable(ctx context.Context, q Querier, entity, medium string, id int64) (string, error) {
	lt, ok := legacyTables[entity]
	if !ok {
		return entity + " has no legacy family", nil
	}
	if ok, err := legacyPresent(ctx, q, lt.table); err != nil {
		return "", err
	} else if !ok {
		return "the legacy catalog is not in this database", nil
	}
	cond := live(lt.alive, lt.entityType)
	args := []any{id}
	if entity == "work" {
		m, ok := legacyMedia[medium]
		if !ok {
			return "medium " + medium + " has no legacy counterpart", nil
		}
		cond += " AND t.medium_id = $2"
		args = append(args, m)
	}
	rows, err := q.QueryContext(ctx, `SELECT 1 FROM `+LegacySchema+`.`+lt.table+` t WHERE t.id = $1 AND `+cond, args...)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if !rows.Next() {
		return fmt.Sprintf("legacy %s %d is not live, was merged, or is of another medium", entity, id), rows.Err()
	}
	return "", nil
}

// LegacyPerson is the person a legacy credit name hangs under; 0 when it
// stands alone and gets a person of its own (D19).
func LegacyPerson(ctx context.Context, q Querier, creditName int64) (int64, error) {
	rows, err := q.QueryContext(ctx, `SELECT COALESCE(p.id, 0) FROM `+LegacySchema+`.catalog_credit_name t
		LEFT JOIN `+LegacySchema+`.catalog_person p ON p.id = t.person_id AND p.deleted_at IS NULL WHERE t.id = $1`, creditName)
	if err != nil {
		return 0, err
	}
	ids, err := scanIDs(rows)
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	return ids[0], nil
}
