package commit

import (
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Citing answers "that entity is this existing one": its create goes away,
// everything pointing at it points at id, and what only described it goes too.
func (cs Changeset) Citing(ref string, id int64) Changeset {
	out := cs
	out.Changes = nil
	out.Known = maps.Clone(cs.Known)
	if out.Known == nil {
		out.Known = map[string]int64{}
	}
	out.Known[ref] = id
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
	return out.pruned(cs)
}

// Omitting leaves out refs, and single properties as "<ref>.<property>". What
// points at an omitted ref goes with it, and so does a name nothing credits
// any more. An entry that names nothing here is returned.
func (cs Changeset) Omitting(entries ...string) (Changeset, []string) {
	gone := map[string]bool{}
	props := map[string]map[string]bool{}
	var unknown []string
	for _, e := range entries {
		ref, prop, ok := cs.locate(e)
		switch {
		case !ok:
			unknown = append(unknown, e)
		case prop == "":
			gone[ref] = true
		default:
			if props[ref] == nil {
				props[ref] = map[string]bool{}
			}
			props[ref][prop] = true
		}
	}
	for again := true; again; {
		again = false
		for _, ch := range cs.Changes {
			if ch.Ref == "" || gone[ch.Ref] {
				continue
			}
			for _, v := range ch.Values {
				if gone[refIn(v)] {
					gone[ch.Ref], again = true, true
				}
			}
		}
	}
	out := cs
	out.Changes = nil
	for _, ch := range cs.Changes {
		if gone[ch.Ref] {
			continue
		}
		drop := false
		values := make(map[string]Value, len(ch.Values))
		for k, v := range ch.Values {
			drop = drop || gone[refIn(v)]
			if !props[ch.Ref][k] {
				values[k] = v
			}
		}
		if drop {
			continue
		}
		ch.Values = values
		out.Changes = append(out.Changes, ch)
	}
	return out.pruned(cs), unknown
}

// locate reads an omit entry; refs carry dots themselves, so the longest ref wins.
func (cs Changeset) locate(entry string) (ref, prop string, ok bool) {
	for _, ch := range cs.Changes {
		if ch.Ref == entry {
			return entry, "", true
		}
		rest, has := strings.CutPrefix(entry, ch.Ref+".")
		if _, known := ch.Values[rest]; ch.Ref != "" && has && known && len(ch.Ref) > len(ref) {
			ref, prop, ok = ch.Ref, rest, true
		}
	}
	return ref, prop, ok
}

func (cs Changeset) used() map[string]bool {
	out := map[string]bool{}
	for _, ch := range cs.Changes {
		for _, v := range ch.Values {
			out[refIn(v)] = true
		}
	}
	return out
}

// descriptive lists, per entity, the rows that only restate what an existing
// one already says when a changeset cites it instead of creating it.
var descriptive = map[string]map[string]string{
	"work": {"work_title": "work_id", "book_work": "work_id"},
	"org":  {"org_name": "org_id"},
}

func describes(entity string, ch Change, ref string) bool {
	field, ok := descriptive[entity][ch.Entity]
	return ok && refIn(ch.Values[field]) == ref
}

// pruned drops the names was credited and cs no longer does, then the persons
// nothing names any more.
func (cs Changeset) pruned(was Changeset) Changeset {
	credited := was.used()
	for _, entity := range []string{"person_alias", "person"} {
		used := cs.used()
		cs.Changes = slices.DeleteFunc(slices.Clone(cs.Changes), func(ch Change) bool {
			return ch.Entity == entity && ch.ID == 0 && ch.Ref != "" && credited[ch.Ref] && !used[ch.Ref]
		})
	}
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
