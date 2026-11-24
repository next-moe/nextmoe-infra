package commit

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
)

// Citing answers "that entity is this existing one": its create goes away,
// everything pointing at it points at id, and what only described it goes too.
func (cs Changeset) Citing(ref string, id int64) Changeset {
	out := cs
	out.Changes = nil
	entity := ""
	for _, ch := range cs.Changes {
		if ch.Ref == ref {
			entity = ch.Entity
		}
	}
	for _, ch := range cs.Changes {
		if ch.Ref == ref || describes(entity, ch, ref) {
			continue
		}
		ch.Values = repoint(ch.Values, ref, id)
		out.Changes = append(out.Changes, ch)
	}
	return out.pruned()
}

// Omitting leaves refs out, and every change that points at one of them.
func (cs Changeset) Omitting(refs ...string) Changeset {
	out := cs
	gone := map[string]bool{}
	for _, r := range refs {
		gone[r] = true
	}
	for again := true; again; {
		again = false
		for _, ch := range cs.Changes {
			if gone[ch.Ref] || ch.Ref == "" {
				continue
			}
			for _, v := range ch.Values {
				if gone[refIn(v)] {
					gone[ch.Ref], again = true, true
				}
			}
		}
	}
	out.Changes = slices.DeleteFunc(slices.Clone(cs.Changes), func(ch Change) bool {
		if gone[ch.Ref] {
			return true
		}
		for _, v := range ch.Values {
			if gone[refIn(v)] {
				return true
			}
		}
		return false
	})
	return out.pruned()
}

// descriptive lists, per entity, the rows that only restate what an existing
// one already says when a changeset cites it instead of creating it.
var descriptive = map[string]map[string]string{
	"work": {"work_title": "work_id", "book_work": "work_id", "credit": "work_id"},
	"org":  {"org_name": "org_id"},
}

func describes(entity string, ch Change, ref string) bool {
	field, ok := descriptive[entity][ch.Entity]
	return ok && refIn(ch.Values[field]) == ref
}

// pruned drops a person nothing in the changeset names any more: citing an
// alias would otherwise leave a person with no name behind.
func (cs Changeset) pruned() Changeset {
	used := map[string]bool{}
	for _, ch := range cs.Changes {
		for _, v := range ch.Values {
			used[refIn(v)] = true
		}
	}
	cs.Changes = slices.DeleteFunc(slices.Clone(cs.Changes), func(ch Change) bool {
		return ch.Entity == "person" && ch.ID == 0 && ch.Ref != "" && !used[ch.Ref]
	})
	return cs
}

func repoint(values map[string]Value, ref string, id int64) map[string]Value {
	out := make(map[string]Value, len(values))
	for k, v := range values {
		if refIn(v) == ref {
			v.Raw = json.RawMessage(strconv.FormatInt(id, 10))
		}
		out[k] = v
	}
	return out
}

// refIn is the ref a value points at, or "" for a plain value.
func refIn(v Value) string {
	var s string
	if json.Unmarshal(v.Raw, &s) != nil {
		return ""
	}
	if r, ok := strings.CutPrefix(s, "ref:"); ok {
		return r
	}
	return ""
}
