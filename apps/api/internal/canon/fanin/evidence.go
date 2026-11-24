package fanin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"api/internal/canon/commit"
	"api/internal/canon/contract"
	"api/internal/canon/ent"
	"api/internal/canon/registry"
	"api/internal/canon/verify"
)

type refusal struct{ code, detail string }

func (r *refusal) Error() string { return r.code + ": " + r.detail }

func refuse(code, format string, args ...any) *refusal {
	return &refusal{code: code, detail: fmt.Sprintf(format, args...)}
}

// checked is one candidate after fanin re-derived everything about it.
type checked struct {
	value commit.Value
	snaps []contract.Snapshot
	recs  []contract.Record
}

type run struct {
	program registry.Program
	client  *ent.Client
	pool    Pool
	snaps   map[string]contract.Snapshot
	recs    map[string]contract.Record
	// row is the changeset being checked; its own entities are named by their
	// properties, not by canon.
	row *contract.Changeset
}

func (r *run) snapshot(ctx context.Context, hash string) (contract.Snapshot, *refusal) {
	if s, ok := r.snaps[hash]; ok {
		return s, nil
	}
	s, err := r.pool.Snapshot(ctx, hash)
	if err != nil {
		return s, refuse("SNAPSHOT_MISSING", "snapshot %s: %v", hash, err)
	}
	sum := sha256.Sum256([]byte(strings.Join(s.Lines, "\n")))
	if hex.EncodeToString(sum[:]) != hash || s.Hash != hash {
		return s, refuse("SNAPSHOT_MISSING", "snapshot %s does not hash to its name", hash)
	}
	r.snaps[hash] = s
	return s, nil
}

func (r *run) record(ctx context.Context, id string) (contract.Record, *refusal) {
	if rec, ok := r.recs[id]; ok {
		return rec, nil
	}
	rec, err := r.pool.Record(ctx, id)
	if err != nil {
		return rec, refuse("SNAPSHOT_MISSING", "record %s: %v", id, err)
	}
	r.recs[id] = rec
	return rec, nil
}

// check verifies a candidate from scratch: every cited item must resolve, at
// least one must entail the value, and the tier is the best entailing source's.
func (r *run) check(ctx context.Context, p registry.Property, c contract.Candidate) (checked, *refusal) {
	out := checked{}
	val, verr := verify.Value(p, c.Value)
	if verr != nil {
		return out, refuse(verr.Code, "%s", verr.Detail)
	}
	if p.Shape == registry.ShapeRef && !p.Named {
		out.value = commit.Value{Raw: c.Value}
		return out, nil
	}
	var names []string
	if p.Named {
		var err error
		if names, err = r.names(ctx, p.Ref, val); err != nil {
			return out, refuse("SNAPSHOT_MISSING", "%v", err)
		}
	}
	if len(c.Evidence) == 0 {
		return out, refuse("EXCERPT_NOT_ON_PAGE", "no evidence cited")
	}
	var best registry.Tier
	for _, e := range c.Evidence {
		ex, src, snap, rec, rf := r.resolve(ctx, e)
		if rf != nil {
			return out, rf
		}
		entailed := false
		switch {
		case p.Named && p.Ref == "work":
			entailed = titled(names, ex)
		case p.Named:
			entailed = verify.Names(names, ex, rec != nil)
		case rec != nil:
			entailed = verify.Matches(p, val, ex.Lines[0])
		default:
			entailed = verify.Entails(p, val, ex)
		}
		if !entailed {
			continue
		}
		if best == "" || registry.Rank(src.Tier) < registry.Rank(best) {
			best, out.value.Source = src.Tier, src.Key
		}
		if snap != nil {
			out.snaps = append(out.snaps, *snap)
		}
		if rec != nil {
			out.recs = append(out.recs, *rec)
		}
		out.value.Evidence = append(out.value.Evidence, commit.EvidenceRef{Snapshot: e.SnapshotID, LineFrom: e.LineFrom, LineTo: e.LineTo, Record: e.RecordID, Path: e.Path})
	}
	if best == "" {
		return out, refuse("VALUE_NOT_ENTAILED", "no cited excerpt supports %s", string(c.Value))
	}
	out.value.Raw, out.value.Tier = c.Value, best
	return out, nil
}

func (r *run) resolve(ctx context.Context, e contract.Evidence) (verify.Excerpt, registry.Source, *contract.Snapshot, *contract.Record, *refusal) {
	switch {
	case e.SnapshotID != "":
		s, rf := r.snapshot(ctx, e.SnapshotID)
		if rf != nil {
			return verify.Excerpt{}, registry.Source{}, nil, nil, rf
		}
		if e.LineFrom < 1 || e.LineTo < e.LineFrom || e.LineTo > len(s.Lines) {
			return verify.Excerpt{}, registry.Source{}, nil, nil, refuse("EXCERPT_NOT_ON_PAGE", "lines %d-%d of %d", e.LineFrom, e.LineTo, len(s.Lines))
		}
		u, err := url.Parse(s.FinalURL)
		if err != nil {
			return verify.Excerpt{}, registry.Source{}, nil, nil, refuse("HOST_EXCLUDED", "final_url %q", s.FinalURL)
		}
		src, ok := registry.SourceForHost(strings.ToLower(u.Hostname()))
		if !ok || src.Excluded {
			return verify.Excerpt{}, registry.Source{}, nil, nil, refuse("HOST_EXCLUDED", "%s is not a registered source", u.Hostname())
		}
		return verify.Excerpt{Lines: s.Lines[e.LineFrom-1 : e.LineTo], URL: s.FinalURL}, src, &s, nil, nil
	case e.RecordID != "":
		rec, rf := r.record(ctx, e.RecordID)
		if rf != nil {
			return verify.Excerpt{}, registry.Source{}, nil, nil, rf
		}
		src, ok := registry.SourceForRecord(rec.Source)
		if !ok || src.Excluded {
			return verify.Excerpt{}, registry.Source{}, nil, nil, refuse("HOST_EXCLUDED", "record source %s is not registered", rec.Source)
		}
		v, ok := lookup(rec.Fields, e.Path)
		if !ok {
			return verify.Excerpt{}, registry.Source{}, nil, nil, refuse("EXCERPT_NOT_ON_PAGE", "record %s has no %s", rec.ID, e.Path)
		}
		return verify.Excerpt{Lines: []string{v}}, src, nil, &rec, nil
	}
	return verify.Excerpt{}, registry.Source{}, nil, nil, refuse("EXCERPT_NOT_ON_PAGE", "evidence names neither a snapshot nor a record")
}

// lookup reads a dotted path ("creators.1.name") out of a record's fields.
func lookup(fields json.RawMessage, path string) (string, bool) {
	var cur any
	if json.Unmarshal(fields, &cur) != nil {
		return "", false
	}
	for _, part := range strings.Split(path, ".") {
		switch node := cur.(type) {
		case map[string]any:
			cur = node[part]
		case []any:
			var i int
			if _, err := fmt.Sscan(part, &i); err != nil || i < 0 || i >= len(node) {
				return "", false
			}
			cur = node[i]
		default:
			return "", false
		}
	}
	switch v := cur.(type) {
	case nil:
		return "", false
	case string:
		return v, true
	default:
		b, _ := json.Marshal(v)
		return string(b), true
	}
}

func archive(ctx context.Context, client *ent.Client, c checked) error {
	for _, s := range c.snaps {
		u, _ := url.Parse(s.FinalURL)
		at, _ := time.Parse(time.RFC3339, s.FetchedAt)
		err := client.EvidenceSnapshot.Create().SetID(s.Hash).SetRequestedURL(s.RequestedURL).SetFinalURL(s.FinalURL).
			SetHost(u.Hostname()).SetFetchedAt(at).SetLines(s.Lines).OnConflictColumns("id").DoNothing().Exec(ctx)
		if err != nil && !errors.Is(err, errNoRows) {
			return err
		}
	}
	for _, rec := range c.recs {
		at, _ := time.Parse(time.RFC3339, rec.FetchedAt)
		var fields map[string]any
		_ = json.Unmarshal(rec.Fields, &fields)
		err := client.EvidenceRecord.Create().SetID(rec.ID).SetSource(rec.Source).SetKey(rec.Key).SetHash(rec.Hash).
			SetFetchedAt(at).SetFields(fields).OnConflictColumns("id").DoNothing().Exec(ctx)
		if err != nil && !errors.Is(err, errNoRows) {
			return err
		}
	}
	return nil
}
