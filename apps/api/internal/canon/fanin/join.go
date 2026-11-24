package fanin

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"api/internal/canon/commit"
	"api/internal/canon/ent"
	"api/internal/canon/identity"
)

type credited struct {
	alias int64
	name  string
	role  string
}

// JoinWork makes a changeset that would create a work add to an existing one:
// its volumes and editions go there, a credit the work has already is dropped,
// and a name the work credits is that same person (R7.5), so only people the
// work does not know yet come along.
func JoinWork(ctx context.Context, client *ent.Client, cs commit.Changeset, ref string, work int64) (commit.Changeset, error) {
	known, err := creditsOf(ctx, client, work)
	if err != nil {
		return cs, err
	}
	var duplicate []string
	cite := map[string]int64{}
	for _, ch := range cs.Changes {
		if ch.Entity != "credit" || refTo(ch.Values["work_id"]) != ref {
			continue
		}
		alias := refTo(ch.Values["alias_id"])
		name, err := creditedName(ctx, client, cs, ch)
		if err != nil {
			return cs, err
		}
		for _, k := range known {
			if identity.NameKey(k.name) != identity.NameKey(name) {
				continue
			}
			if alias != "" {
				cite[alias] = k.alias
			}
			if k.role == str(ch.Values["role"]) {
				duplicate = append(duplicate, ch.Ref)
				break
			}
		}
	}
	if len(duplicate) > 0 {
		cs, _ = cs.Omitting(duplicate...)
	}
	for alias, id := range cite {
		cs = cs.Citing(alias, id)
	}
	return cs.Citing(ref, work), nil
}

func creditsOf(ctx context.Context, client *ent.Client, work int64) ([]credited, error) {
	rows, err := client.QueryContext(ctx, `SELECT a.id, a.name, c.role FROM canon.credit c JOIN canon.person_alias a ON a.id = c.alias_id
		WHERE c.work_id = $1 ORDER BY c.id`, work)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []credited
	for rows.Next() {
		var c credited
		if err := rows.Scan(&c.alias, &c.name, &c.role); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func creditedName(ctx context.Context, client *ent.Client, cs commit.Changeset, credit commit.Change) (string, error) {
	if ref := refTo(credit.Values["alias_id"]); ref != "" {
		for _, ch := range cs.Changes {
			if ch.Ref == ref {
				return str(ch.Values["name"]), nil
			}
		}
		return "", nil
	}
	a, err := client.PersonAlias.Get(ctx, num(credit.Values["alias_id"]))
	if ent.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return a.Name, nil
}

// SameWork applies the ladder's fourth rung (06 §4): the one canon work of
// exactly this title, crediting the same author and published under the same
// label, is this work. A title that only resembles another, a legacy
// candidate, or two candidates stay a question.
func SameWork(ctx context.Context, client *ent.Client, cs commit.Changeset, q Question) (int64, error) {
	if q.Entity != "work" || len(q.Candidates) != 1 || q.Candidates[0].Origin != identity.OriginCanon {
		return 0, nil
	}
	work := q.Candidates[0].ID
	titles, err := client.QueryContext(ctx, `SELECT text FROM canon.work_title WHERE work_id = $1`, work)
	if err != nil {
		return 0, err
	}
	exact := false
	for titles.Next() {
		var t string
		if err := titles.Scan(&t); err != nil {
			titles.Close()
			return 0, err
		}
		exact = exact || identity.TitleKey(t) == identity.TitleKey(q.Name)
	}
	titles.Close()
	if err := titles.Err(); err != nil || !exact {
		return 0, err
	}
	known, err := creditsOf(ctx, client, work)
	if err != nil {
		return 0, err
	}
	author := false
	for _, ch := range cs.Changes {
		if ch.Entity != "credit" || str(ch.Values["role"]) != "author" {
			continue
		}
		name, err := creditedName(ctx, client, cs, ch)
		if err != nil {
			return 0, err
		}
		for _, k := range known {
			author = author || k.role == "author" && name != "" && identity.NameKey(k.name) == identity.NameKey(name)
		}
	}
	if !author {
		return 0, nil
	}
	labels := map[int64]bool{}
	rows, err := client.QueryContext(ctx, `SELECT DISTINCT b.label_id FROM canon.book_release b
		JOIN canon.release_content c ON c.release_id = b.release_id WHERE c.work_id = $1 AND b.label_id IS NOT NULL`, work)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var l int64
		if err := rows.Scan(&l); err != nil {
			rows.Close()
			return 0, err
		}
		labels[l] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, ch := range cs.Changes {
		if ch.Entity == "book_release" && !labels[num(ch.Values["label_id"])] {
			return 0, nil
		}
	}
	return work, nil
}

func refTo(v commit.Value) string {
	var s string
	if json.Unmarshal(v.Raw, &s) != nil {
		return ""
	}
	if r, ok := strings.CutPrefix(s, "ref:"); ok {
		return r
	}
	return ""
}

func num(v commit.Value) int64 {
	n, _ := strconv.ParseInt(string(v.Raw), 10, 64)
	return n
}
