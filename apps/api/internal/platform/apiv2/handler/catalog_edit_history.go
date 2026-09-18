package handler

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/editspec"
	catsvc "api/internal/platform/catalog/service"
	"api/internal/platform/editing"

	"gorm.io/datatypes"
)

func schemaObject(entityType string) string {
	switch entityType {
	case editspec.TypeWork:
		return "work"
	case editspec.TypeLabel:
		return "company"
	case editspec.TypeCharacter:
		return "character"
	case editspec.TypeRelease:
		return "release"
	case editspec.TypeTag:
		return "tag"
	case editspec.TypeEngine:
		return "engine"
	case editspec.TypeSeries:
		return "series"
	default:
		return ""
	}
}

func rfc3339(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }

func decodeFieldKeys(raw datatypes.JSON) []string {
	var out []string
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	if out == nil {
		out = []string{}
	}
	return out
}

func decodeJSONObject(raw datatypes.JSON) map[string]any {
	out := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func idPtr(v *int64) *string {
	if v == nil || *v <= 0 {
		return nil
	}
	s := repr.ID(*v)
	return &s
}

func revisionFrom(r *editing.Revision, siteWorkID *string) repr.Revision {
	return repr.Revision{
		Object: "revision", ID: repr.ID(r.ID),
		TargetObject: schemaObject(r.EntityType), EntityID: repr.ID(r.EntityID),
		SiteWorkID: siteWorkID, Seq: r.Seq, Action: editing.ActionName[r.Action],
		ChangedFields: decodeFieldKeys(r.ChangedFields),
		ActorUID:      repr.ID(r.ActorUID), AmenderUID: idPtr(r.AmenderUID),
		ProposalID: idPtr(r.ProposalID), Site: r.Site, CreatedAt: rfc3339(r.CreatedAt),
	}
}

// v1's feed only attaches product_work_id when the claiming site matches the
// revision's own site; dropping that guard hands a tenant the other tenant's id
// for the same catalog row.
func (c *Catalog) siteWorkIDs(ctx context.Context, revs []editing.Revision) (map[int64]catsvc.ClaimIdentity, error) {
	if c.Claims == nil {
		return nil, nil
	}
	seen := map[int64]struct{}{}
	ids := make([]int64, 0, len(revs))
	for i := range revs {
		if revs[i].EntityType != editspec.TypeWork {
			continue
		}
		if _, dup := seen[revs[i].EntityID]; dup {
			continue
		}
		seen[revs[i].EntityID] = struct{}{}
		ids = append(ids, revs[i].EntityID)
	}
	return c.Claims.ClaimIdentities(ctx, ids)
}

func siteWorkIDFor(claims map[int64]catsvc.ClaimIdentity, r *editing.Revision) *string {
	if r.EntityType != editspec.TypeWork {
		return nil
	}
	id, ok := claims[r.EntityID]
	if !ok || id.Site != r.Site {
		return nil
	}
	s := repr.ID(id.ProductWorkID)
	return &s
}

type revisionFilter struct {
	Object   string
	EntityID string
	Site     string
	ActorUID string
}

func (c *Catalog) ListRevisions(ctx context.Context, q collect.Query, f revisionFilter) (repr.List[repr.Revision], error) {
	if c == nil || c.EditHistory == nil {
		return repr.List[repr.Revision]{}, problem.New(problem.CodeServiceUnavailable, "", "", "edit history is not bound.")
	}
	page := catsvc.RevisionPage{
		Site: f.Site, Ascending: q.Sort == "recorded_asc", WithTotal: q.IncludeTotal,
	}
	if f.Object != "" {
		entityType := schemaEntityType(f.Object)
		if entityType == "" {
			p := problem.New(problem.CodeUnknownEnumValue, "", "", "object= is not an editable family.")
			p.Errors = []problem.FieldError{{Parameter: "object", Reason: problem.ReasonUnknownValue,
				Detail: "allowed values: work, company, character, release, tag, engine, series",
				Params: &problem.FieldParams{Allowed: &[]string{"work", "company", "character", "release", "tag", "engine", "series"}}}}
			return repr.List[repr.Revision]{}, p
		}
		page.EntityType = entityType
	}
	if f.EntityID != "" {
		id, ok := repr.ParseID(f.EntityID)
		if !ok {
			return repr.List[repr.Revision]{}, badFilterID("entity_id", f.EntityID)
		}
		if page.EntityType == "" {
			p := problem.New(problem.CodeValidationFailed, "", "", "entity_id= needs object= to be unambiguous.")
			p.Errors = []problem.FieldError{{Parameter: "entity_id", Reason: problem.ReasonInconsistentWith,
				Detail: "entity ids are only unique within one family; pass object= as well"}}
			return repr.List[repr.Revision]{}, p
		}
		page.EntityID = id
	}
	if f.ActorUID != "" {
		id, ok := repr.ParseID(f.ActorUID)
		if !ok {
			return repr.List[repr.Revision]{}, badFilterID("actor_uid", f.ActorUID)
		}
		page.ActorUID = id
	}

	var missing []string
	if q.Batch {
		ids, err := parseIDList(q.IDs)
		if err != nil {
			return repr.List[repr.Revision]{}, err
		}
		page.IDs = ids
	} else {
		cur, cerr := claimEventCursor(q.Cursor)
		if cerr != nil {
			return repr.List[repr.Revision]{}, cerr
		}
		page.Cursor = cur
		page.Limit = q.Limit
		if page.Limit <= 0 {
			page.Limit = collect.DefaultLimit
		}
		page.Limit++
	}

	rows, total, err := c.EditHistory.Revisions(ctx, page)
	if err != nil {
		return repr.List[repr.Revision]{}, err
	}
	var next *string
	if !q.Batch {
		limit := page.Limit - 1
		if len(rows) > limit {
			rows = rows[:limit]
			s := strconv.FormatInt(rows[len(rows)-1].ID, 10)
			next = &s
		}
	}
	claims, cerr := c.siteWorkIDs(ctx, rows)
	if cerr != nil {
		return repr.List[repr.Revision]{}, cerr
	}
	items := make([]repr.Revision, 0, len(rows))
	found := map[string]bool{}
	for i := range rows {
		items = append(items, revisionFrom(&rows[i], siteWorkIDFor(claims, &rows[i])))
		found[repr.ID(rows[i].ID)] = true
	}
	if q.Batch {
		for _, want := range q.IDs {
			if !found[want] {
				missing = append(missing, want)
			}
		}
	}
	return finishList(items, next, total, q, missing), nil
}

func (c *Catalog) GetRevision(ctx context.Context, id int64, q collect.Query, diffBase string) (repr.Revision, error) {
	if c == nil || c.EditHistory == nil {
		return repr.Revision{}, problem.New(problem.CodeServiceUnavailable, "", "", "edit history is not bound.")
	}
	rev, err := c.EditHistory.RevisionByID(ctx, id)
	if err != nil {
		return repr.Revision{}, err
	}
	if rev == nil || schemaObject(rev.EntityType) == "" {
		return repr.Revision{}, problem.New(problem.CodeNotFound, "", "", "No revision with this id.")
	}
	claims, cerr := c.siteWorkIDs(ctx, []editing.Revision{*rev})
	if cerr != nil {
		return repr.Revision{}, cerr
	}
	out := revisionFrom(rev, siteWorkIDFor(claims, rev))
	if !hasToken(q.Include, "diff") {
		if diffBase != "" {
			p := problem.New(problem.CodeValidationFailed, "", "", "diff_base= only means something with include=diff.")
			p.Errors = []problem.FieldError{{Parameter: "diff_base", Reason: problem.ReasonInconsistentWith,
				Detail: "pass include=diff to request the change set"}}
			return repr.Revision{}, p
		}
		return out, nil
	}
	base, berr := c.diffBaseRevision(ctx, rev, diffBase)
	if berr != nil {
		return repr.Revision{}, berr
	}
	diff, derr := c.fieldDiff(ctx, rev, base)
	if derr != nil {
		return repr.Revision{}, derr
	}
	out.Diff = &diff
	out.DiffBase = optionalRevisionID(base)
	return out, nil
}

func optionalRevisionID(base *editing.Revision) *string {
	if base == nil {
		return nil
	}
	s := repr.ID(base.ID)
	return &s
}

func (c *Catalog) diffBaseRevision(ctx context.Context, rev *editing.Revision, raw string) (*editing.Revision, error) {
	if raw == "" {
		prior, err := c.EditHistory.PreviousRevisionID(ctx, rev)
		if err != nil {
			return nil, err
		}
		if prior == 0 {
			return nil, nil
		}
		return c.EditHistory.RevisionByID(ctx, prior)
	}
	id, ok := repr.ParseID(raw)
	if !ok {
		return nil, badFilterID("diff_base", raw)
	}
	base, err := c.EditHistory.RevisionByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if base == nil || base.EntityType != rev.EntityType || base.EntityID != rev.EntityID {
		p := problem.New(problem.CodeValidationFailed, "", "", "diff_base must be a revision of the same entity.")
		p.Errors = []problem.FieldError{{Parameter: "diff_base", Reason: problem.ReasonUnknownReference,
			Detail: "no visible revision with this id on this entity"}}
		return nil, p
	}
	return base, nil
}

func (c *Catalog) fieldDiff(ctx context.Context, rev, base *editing.Revision) ([]repr.FieldDiff, error) {
	out := []repr.FieldDiff{}
	if base == nil {
		snap := decodeJSONObject(rev.Snapshot)
		for _, key := range decodeFieldKeys(rev.ChangedFields) {
			out = append(out, repr.FieldDiff{Key: key, From: nil, To: snap[key]})
		}
		return out, nil
	}
	if c.Engine == nil {
		return out, problem.New(problem.CodeServiceUnavailable, "", "", "the editing engine is not bound.")
	}
	diffs, err := c.Engine.Diff(ctx, rev.EntityType, rev.EntityID, base.Seq, rev.Seq)
	if err != nil {
		return nil, proposalErr(err)
	}
	for _, d := range diffs {
		out = append(out, repr.FieldDiff{Key: d.Key, From: d.From, To: d.To})
	}
	return out, nil
}

func hasToken(tokens []string, want string) bool {
	for _, t := range tokens {
		if t == want {
			return true
		}
	}
	return false
}

func parseIDList(raw []string) ([]int64, error) {
	ids := make([]int64, 0, len(raw))
	for _, s := range raw {
		n, ok := repr.ParseID(s)
		if !ok {
			p := problem.New(problem.CodeInvalidParameter, "", "", "ids= values must be decimal ids.")
			p.Errors = []problem.FieldError{{Parameter: "ids", Reason: problem.ReasonInvalidFormat, Detail: s}}
			return nil, p
		}
		ids = append(ids, n)
	}
	return ids, nil
}

func badFilterID(name, raw string) *problem.Problem {
	p := problem.New(problem.CodeInvalidParameter, "", "", name+" must be a positive decimal id.")
	p.Errors = []problem.FieldError{{Parameter: name, Reason: problem.ReasonInvalidFormat, Detail: raw}}
	return p
}
