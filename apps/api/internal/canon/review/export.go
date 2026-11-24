package review

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"api/internal/canon/commit"
	"api/internal/canon/ent"
	enttodo "api/internal/canon/ent/todo"
	"api/internal/canon/fanin"
	"api/internal/canon/identity"
)

// Sheet is a todo as a reviewer who cannot reach the database needs it: every
// value with the words it was read from, and what is known about each candidate.
type Sheet struct {
	TodoID    int              `json:"todo_id"`
	Kind      string           `json:"kind"`
	Program   string           `json:"program"`
	Detail    string           `json:"detail"`
	Changes   []SheetChange    `json:"changes"`
	Questions []SheetQuestion  `json:"questions"`
	Notes     []map[string]any `json:"notes,omitempty"`
}

type SheetChange struct {
	Ref    string       `json:"ref"`
	Entity string       `json:"entity"`
	ID     int64        `json:"id,omitempty"`
	Values []SheetValue `json:"values"`
}

type SheetValue struct {
	Property string          `json:"property"`
	Value    json.RawMessage `json:"value"`
	// Is is what a cited id is called.
	Is       string   `json:"is,omitempty"`
	Tier     string   `json:"tier,omitempty"`
	Evidence []string `json:"evidence,omitempty"`
}

type SheetQuestion struct {
	Ref        string           `json:"ref"`
	Entity     string           `json:"entity"`
	Name       string           `json:"name"`
	Why        string           `json:"why"`
	Candidates []SheetCandidate `json:"candidates"`
}

type SheetCandidate struct {
	Token string   `json:"token"`
	ID    int64    `json:"id"`
	Label string   `json:"label"`
	Facts []string `json:"facts"`
}

// Sheets lists the oldest open todos of the kinds a review agent may settle.
func Sheets(ctx context.Context, client *ent.Client, kinds []string, limit int) ([]Sheet, error) {
	ts, err := client.Todo.Query().Where(enttodo.State("open"), enttodo.KindIn(kinds...)).Order(ent.Asc(enttodo.FieldID)).Limit(limit).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Sheet, 0, len(ts))
	for _, t := range ts {
		s, err := sheet(ctx, client, t)
		if err != nil {
			return nil, fmt.Errorf("todo %d: %w", t.ID, err)
		}
		out = append(out, s)
	}
	return out, nil
}

func sheet(ctx context.Context, client *ent.Client, t *ent.Todo) (Sheet, error) {
	out := Sheet{TodoID: t.ID, Kind: t.Kind, Program: t.Program, Detail: t.Detail, Notes: t.Notes, Questions: []SheetQuestion{}}
	var cs commit.Changeset
	var qs []fanin.Question
	if err := convert(t.Changeset, &cs); err != nil {
		return out, err
	}
	if err := convert(t.Questions, &qs); err != nil {
		return out, err
	}
	for _, ch := range cs.Changes {
		sc := SheetChange{Ref: ch.Ref, Entity: ch.Entity, ID: ch.ID}
		for _, prop := range slices.Sorted(maps.Keys(ch.Values)) {
			v := ch.Values[prop]
			sv := SheetValue{Property: prop, Value: v.Raw, Tier: string(v.Tier)}
			for _, e := range v.Evidence {
				words, err := excerpt(ctx, client, e)
				if err != nil {
					return out, err
				}
				sv.Evidence = append(sv.Evidence, words)
			}
			var id int64
			if json.Unmarshal(v.Raw, &id) == nil && strings.HasSuffix(prop, "_id") {
				is, err := called(ctx, client, strings.TrimSuffix(prop, "_id"), id)
				if err != nil {
					return out, err
				}
				sv.Is = is
			}
			sc.Values = append(sc.Values, sv)
		}
		out.Changes = append(out.Changes, sc)
	}
	for _, q := range qs {
		sq := SheetQuestion{Ref: q.Ref, Entity: q.Entity, Name: q.Name, Why: q.Why, Candidates: []SheetCandidate{}}
		for _, c := range q.Candidates {
			facts, err := facts(ctx, client, q.Entity, c)
			if err != nil {
				return out, err
			}
			sq.Candidates = append(sq.Candidates, SheetCandidate{Token: c.Token(), ID: c.ID, Label: c.Label, Facts: facts})
		}
		out.Questions = append(out.Questions, sq)
	}
	return out, nil
}

func excerpt(ctx context.Context, client *ent.Client, e commit.EvidenceRef) (string, error) {
	if e.Snapshot != "" {
		s, err := client.EvidenceSnapshot.Get(ctx, e.Snapshot)
		if err != nil {
			return "", err
		}
		return s.FinalURL + ": " + strings.Join(s.Lines[e.LineFrom-1:e.LineTo], " / "), nil
	}
	r, err := client.EvidenceRecord.Get(ctx, e.Record)
	if err != nil {
		return "", err
	}
	var v any = r.Fields
	for _, part := range strings.Split(e.Path, ".") {
		switch node := v.(type) {
		case map[string]any:
			v = node[part]
		case []any:
			var i int
			if _, err := fmt.Sscan(part, &i); err != nil || i < 0 || i >= len(node) {
				return "", fmt.Errorf("record %s has no %s", e.Record, e.Path)
			}
			v = node[i]
		}
	}
	return fmt.Sprintf("%s record %s, %s: %v", r.Source, r.Key, e.Path, v), nil
}

var calledBy = map[string]string{
	"alias":     `SELECT name FROM canon.person_alias WHERE id = $1`,
	"label":     `SELECT string_agg(text, ' / ') FROM canon.org_name WHERE org_id = $1`,
	"publisher": `SELECT string_agg(text, ' / ') FROM canon.org_name WHERE org_id = $1`,
	"org":       `SELECT string_agg(text, ' / ') FROM canon.org_name WHERE org_id = $1`,
	"work":      `SELECT string_agg(text, ' / ') FROM canon.work_title WHERE work_id = $1`,
}

func called(ctx context.Context, client *ent.Client, what string, id int64) (string, error) {
	q, ok := calledBy[what]
	if !ok {
		return "", nil
	}
	lines, err := lines(ctx, client, q, id)
	return strings.Join(lines, " / "), err
}

// facts is what tells two same-named candidates apart: what they are credited
// on, in canon or in the legacy catalog, and the anchors a search can follow.
func facts(ctx context.Context, client *ent.Client, entity string, c identity.Candidate) ([]string, error) {
	if c.Origin == identity.OriginCanon {
		return lines(ctx, client, canonFacts[entity], c.ID)
	}
	ok, err := lines(ctx, client, `SELECT 'x' WHERE to_regclass($1) IS NOT NULL`, identity.LegacySchema+".catalog_external_ref")
	if err != nil || len(ok) == 0 {
		return []string{}, err
	}
	q, has := legacyFacts(entity)
	if !has {
		return []string{}, nil
	}
	return lines(ctx, client, q, c.ID)
}

var canonFacts = map[string]string{
	"work": `SELECT 'part ' || COALESCE(NULLIF(p.label, ''), '(unlabelled)') || COALESCE(' released ' || p.released, '') FROM canon.work_part p WHERE p.work_id = $1
		UNION ALL SELECT c.role || ' ' || a.name FROM canon.credit c JOIN canon.person_alias a ON a.id = c.alias_id WHERE c.work_id = $1 LIMIT 40`,
	"person_alias": `SELECT c.role || ' of ' || (SELECT string_agg(text, ' / ') FROM canon.work_title t WHERE t.work_id = c.work_id)
		FROM canon.credit c WHERE c.alias_id = $1 AND c.work_id IS NOT NULL LIMIT 20`,
	"credit": `SELECT c.role || ' of ' || (SELECT string_agg(text, ' / ') FROM canon.work_title t WHERE t.work_id = c.work_id)
		FROM canon.credit c WHERE c.alias_id = $1 AND c.work_id IS NOT NULL LIMIT 20`,
	"org": `SELECT kind FROM canon.org WHERE id = $1`,
}

func legacyFacts(entity string) (string, bool) {
	s := identity.LegacySchema
	refs := func(entityType int) string {
		return fmt.Sprintf(` UNION ALL SELECT s.key || ':' || e.external_id FROM %[1]s.catalog_external_ref e JOIN %[1]s.catalog_source s ON s.id = e.source_id
			WHERE e.entity_type = %[2]d AND e.entity_id = $1 LIMIT 30`, s, entityType)
	}
	switch entity {
	case "work":
		return fmt.Sprintf(`SELECT 'title ' || title FROM %[1]s.catalog_work_title WHERE work_id = $1 AND mt_model IS NULL`, s) + refs(5), true
	case "person_alias":
		return fmt.Sprintf(`SELECT x FROM (SELECT COALESCE(r.key, '?') || ' of ' || w.display_name || ' (' || m.key || ')' AS x FROM %[1]s.catalog_credit c JOIN %[1]s.catalog_work w ON w.id = c.work_id
			JOIN %[1]s.catalog_medium m ON m.id = w.medium_id LEFT JOIN %[1]s.catalog_role r ON r.id = c.role_id WHERE c.credit_name_id = $1 ORDER BY w.id LIMIT 15) t`, s) + refs(1), true
	case "org":
		return fmt.Sprintf(`SELECT x FROM (SELECT 'name ' || name AS x FROM %[1]s.catalog_label_alias WHERE label_id = $1 LIMIT 10) t`, s) + refs(3), true
	}
	return "", false
}

func lines(ctx context.Context, client *ent.Client, query string, args ...any) ([]string, error) {
	out := []string{}
	if query == "" {
		return out, nil
	}
	rows, err := client.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s sql.NullString
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		if s.String != "" {
			out = append(out, s.String)
		}
	}
	return out, rows.Err()
}

// Verdict is one line a review agent writes back.
type Verdict struct {
	TodoID  int    `json:"todo_id"`
	Verdict string `json:"verdict"`
	Decision
}

// Settle applies one verdict as a review agent.
func Settle(ctx context.Context, client *ent.Client, by string, v Verdict) error {
	r := Reviewer{Key: by}
	switch v.Verdict {
	case "accept":
		_, err := Accept(ctx, client, v.TodoID, r, v.Decision)
		return err
	case "reject":
		return Reject(ctx, client, v.TodoID, r, v.Note)
	case "unsure":
		return Unsure(ctx, client, v.TodoID, r, v.Note)
	}
	return Invalid("verdict " + v.Verdict + " is not accept, reject or unsure")
}
