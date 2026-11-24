package fanin

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"api/internal/canon/contract"
	"api/internal/canon/identity"
	"api/internal/canon/verify"
)

// nameOf maps a referenced entity to the child row that carries its names.
var nameOf = map[string]struct{ entity, parent, property, query string }{
	"person_alias": {"person_alias", "", "name", `SELECT name FROM canon.person_alias WHERE id = $1`},
	"org":          {"org_name", "org_id", "text", `SELECT text FROM canon.org_name WHERE org_id = $1`},
	"work":         {"work_title", "work_id", "text", `SELECT text FROM canon.work_title WHERE work_id = $1`},
}

// names is what a referenced entity is called: in canon for an id, in the
// changeset itself for a ref it creates.
func (r *run) names(ctx context.Context, entity string, val any) ([]string, error) {
	n, ok := nameOf[entity]
	if !ok {
		return nil, fmt.Errorf("%s has no names to cite", entity)
	}
	if ref, isRef := val.(string); isRef {
		return r.rowNames(n.entity, n.parent, n.property, strings.TrimPrefix(ref, "ref:")), nil
	}
	rows, err := r.client.QueryContext(ctx, n.query, val)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *run) rowNames(entity, parent, property, ref string) []string {
	if r.row == nil {
		return nil
	}
	var out []string
	for _, ne := range r.row.Entities {
		if ne.Entity != entity {
			continue
		}
		if parent == "" && ne.Ref != ref {
			continue
		}
		if parent != "" && unquoted(ne.Properties[parent]) != "ref:"+ref {
			continue
		}
		if s := unquoted(ne.Properties[property]); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// titled compares title keys, not strings: an edition's page prints the
// series title with the volume and the subtitle run into it.
func titled(titles []string, ex verify.Excerpt) bool {
	var want []string
	for _, t := range titles {
		want = append(want, identity.TitleKeys(t)...)
	}
	for _, l := range ex.Lines {
		if slices.ContainsFunc(identity.TitleKeys(l), func(k string) bool { return slices.Contains(want, k) }) {
			return true
		}
	}
	return false
}

func unquoted(c contract.Candidate) string {
	var s string
	_ = json.Unmarshal(c.Value, &s)
	return s
}
