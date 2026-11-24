package identity

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
)

const (
	OriginCanon  = "canon"
	OriginLegacy = "legacy"
)

// Candidate is an existing entity a new one might be. A legacy candidate is
// not in canon yet; a new entity judged to be it takes over its id (D30).
type Candidate struct {
	Origin string `json:"origin"`
	ID     int64  `json:"id"`
	Label  string `json:"label"`
}

func (c Candidate) Token() string { return fmt.Sprintf("%s:%d", c.Origin, c.ID) }

// Querier is what ent's client and transaction share.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Keys is what one name of an entity is found by; nil for an entity that has none.
func Keys(entity, text string) (kind string, keys []string) {
	switch entity {
	case "work":
		return KindTitle, TitleKeys(text)
	case "person_alias", "org":
		if k := NameKey(text); k != "" {
			return KindName, []string{k}
		}
	}
	return KindName, nil
}

// Find lists what entity named text might already be, canon first, leaving
// out self. Identity is judged within one medium (06 §7), which only works have.
func Find(ctx context.Context, q Querier, entity, medium, text string, self int64) ([]Candidate, error) {
	_, keys := Keys(entity, text)
	if len(keys) == 0 {
		return nil, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT entity_id FROM canon.identity_key
		WHERE entity = $1 AND key = ANY($2) AND medium = $3 AND entity_id <> $4 AND kind <> 'authority' ORDER BY 1`,
		entity, keys, mediumOf(entity, medium), self)
	if err != nil {
		return nil, err
	}
	ids, err := scanIDs(rows)
	if err != nil {
		return nil, err
	}
	var out []Candidate
	for _, id := range ids {
		label, err := canonLabel(ctx, q, entity, id)
		if err != nil {
			return nil, err
		}
		out = append(out, Candidate{OriginCanon, id, label})
	}
	legacy, err := findLegacy(ctx, q, entity, medium, keys)
	if err != nil {
		return nil, err
	}
	for _, c := range legacy {
		if c.ID != self && !slices.Contains(ids, c.ID) {
			out = append(out, c)
		}
	}
	return out, nil
}

// ByAuthority finds the alias a registry authority (an NDL authority number)
// was recorded for; 0 when none was.
func ByAuthority(ctx context.Context, q Querier, authority string) (int64, error) {
	rows, err := q.QueryContext(ctx, `SELECT entity_id FROM canon.identity_key WHERE entity = 'person_alias' AND kind = 'authority' AND key = $1 ORDER BY 1 LIMIT 1`, authority)
	if err != nil {
		return 0, err
	}
	ids, err := scanIDs(rows)
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	return ids[0], nil
}

// Lock serializes judgements on the same keys (06 §6 I4): two commits that
// each saw no candidate must not both create one.
func Lock(ctx context.Context, q Querier, entity, medium string, keys []string) error {
	sorted := slices.Clone(keys)
	slices.Sort(sorted)
	for _, k := range sorted {
		if _, err := q.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, entity+"|"+mediumOf(entity, medium)+"|"+k); err != nil {
			return err
		}
	}
	return nil
}

var nameSources = map[string]string{
	"work":         `SELECT t.text, w.medium FROM canon.work_title t JOIN canon.work w ON w.id = t.work_id WHERE t.work_id = $1`,
	"person_alias": `SELECT name, '' FROM canon.person_alias WHERE id = $1`,
	"org":          `SELECT text, '' FROM canon.org_name WHERE org_id = $1`,
}

// Rekey rebuilds an entity's title or name keys from its rows; authority keys
// are evidence, not derived, and stay.
func Rekey(ctx context.Context, q Querier, entity string, id int64) error {
	src, ok := nameSources[entity]
	if !ok {
		return nil
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM canon.identity_key WHERE entity = $1 AND entity_id = $2 AND kind <> 'authority'`, entity, id); err != nil {
		return err
	}
	rows, err := q.QueryContext(ctx, src, id)
	if err != nil {
		return err
	}
	type name struct{ text, medium string }
	var names []name
	for rows.Next() {
		var n name
		if err := rows.Scan(&n.text, &n.medium); err != nil {
			rows.Close()
			return err
		}
		names = append(names, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, n := range names {
		kind, keys := Keys(entity, n.text)
		for _, k := range keys {
			if err := addKey(ctx, q, entity, id, kind, k, n.medium); err != nil {
				return err
			}
		}
	}
	return nil
}

func AddAuthority(ctx context.Context, q Querier, entity string, id int64, authority string) error {
	return addKey(ctx, q, entity, id, KindAuthority, authority, "")
}

// Forget drops every key of an entity that no longer exists.
func Forget(ctx context.Context, q Querier, entity string, id int64) error {
	_, err := q.ExecContext(ctx, `DELETE FROM canon.identity_key WHERE entity = $1 AND entity_id = $2`, entity, id)
	return err
}

func addKey(ctx context.Context, q Querier, entity string, id int64, kind, key, medium string) error {
	_, err := q.ExecContext(ctx, `INSERT INTO canon.identity_key (entity, entity_id, kind, key, medium) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (entity, entity_id, kind, key) DO NOTHING`, entity, id, kind, key, medium)
	return err
}

var canonLabels = map[string]string{
	"work":         `SELECT string_agg(text, ' / ' ORDER BY position, id) FROM canon.work_title WHERE work_id = $1`,
	"person_alias": `SELECT name FROM canon.person_alias WHERE id = $1`,
	"org":          `SELECT string_agg(text, ' / ' ORDER BY position, id) FROM canon.org_name WHERE org_id = $1`,
}

func canonLabel(ctx context.Context, q Querier, entity string, id int64) (string, error) {
	return text(ctx, q, canonLabels[entity], id)
}

func text(ctx context.Context, q Querier, query string, args ...any) (string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var s sql.NullString
	if rows.Next() {
		if err := rows.Scan(&s); err != nil {
			return "", err
		}
	}
	return s.String, rows.Err()
}

func mediumOf(entity, medium string) string {
	if entity == "work" {
		return medium
	}
	return ""
}

func scanIDs(rows *sql.Rows) ([]int64, error) {
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
