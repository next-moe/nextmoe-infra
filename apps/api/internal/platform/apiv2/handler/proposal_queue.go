package handler

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/apiv2/parse"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/editing"
)

type proposalQueueParams struct {
	Site         string
	OriginSite   string
	Field        string
	HasConflict  string
	UpdatedSince string
}

var proposalFieldKey = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)+$`)

func (p proposalQueueParams) apply(f *editing.ProposalFilter) error {
	origin, err := originSiteParam(p.OriginSite)
	if err != nil {
		return err
	}
	f.OriginSite = origin
	if p.Field != "" {
		if !proposalFieldKey.MatchString(p.Field) {
			pr := problem.New(problem.CodeInvalidParameter, "", "", "field= is not a field key.")
			pr.Errors = []problem.FieldError{{Parameter: "field", Reason: problem.ReasonInvalidFormat,
				Detail: "a dotted registry key such as catalog.work.titles"}}
			return pr
		}
		f.FieldKey = p.Field
	}
	if p.HasConflict != "" {
		v, err := parse.Bool(p.HasConflict, "has_conflict")
		if err != nil {
			return err
		}
		f.HasConflict = &v
	}
	if p.UpdatedSince != "" {
		t, err := parse.DateTimeUTC(p.UpdatedSince, "updated_since")
		if err != nil {
			return err
		}
		f.UpdatedSince = &t
	}
	return nil
}

func proposalOrder(sort string) string {
	switch sort {
	case "filed_asc":
		return editing.ProposalOrderFiledAsc
	case "updated_desc":
		return editing.ProposalOrderUpdatedDesc
	case "updated_asc":
		return editing.ProposalOrderUpdatedAsc
	default:
		return editing.ProposalOrderFiledDesc
	}
}

// filed_desc keeps the bare id payload every existing cursor carries; the other
// orders tag theirs, so a cursor replayed under a different sort is refused
// instead of silently paging from the wrong place.
var proposalCursorTags = map[string]string{
	editing.ProposalOrderFiledAsc:    "fa",
	editing.ProposalOrderUpdatedDesc: "ud",
	editing.ProposalOrderUpdatedAsc:  "ua",
}

func proposalCursorFor(order string, last *editing.Proposal) string {
	id := strconv.FormatInt(last.ID, 10)
	switch order {
	case editing.ProposalOrderFiledDesc:
		return id
	case editing.ProposalOrderFiledAsc:
		return proposalCursorTags[order] + ":" + id
	default:
		return proposalCursorTags[order] + ":" + strconv.FormatInt(last.UpdatedAt.UnixMicro(), 10) + ":" + id
	}
}

func applyProposalCursor(order, raw string, f *editing.ProposalFilter) error {
	f.Order = order
	if raw == "" {
		return nil
	}
	if order == editing.ProposalOrderFiledDesc {
		id, err := claimEventCursor(raw)
		if err != nil {
			return err
		}
		f.BeforeID = id
		return nil
	}
	parts := strings.Split(raw, ":")
	if len(parts) < 2 || parts[0] != proposalCursorTags[order] {
		return collectInvalidCursor()
	}
	id, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	if err != nil || id <= 0 {
		return collectInvalidCursor()
	}
	f.Cursor.ID = id
	if order == editing.ProposalOrderFiledAsc {
		if len(parts) != 2 {
			return collectInvalidCursor()
		}
		return nil
	}
	if len(parts) != 3 {
		return collectInvalidCursor()
	}
	micro, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return collectInvalidCursor()
	}
	f.Cursor.UpdatedAt = time.UnixMicro(micro).UTC()
	return nil
}

func (c *Catalog) proposalRecords(ctx context.Context, rows []editing.Proposal, withPatch bool) ([]repr.ProposalRecord, error) {
	var effective map[int64]map[string]any
	var conflicts map[int64][]string
	if withPatch {
		var err error
		if effective, err = c.Engine.EffectivePatches(ctx, rows); err != nil {
			return nil, err
		}
		if conflicts, err = c.Engine.ConflictFields(ctx, rows, effective); err != nil {
			return nil, err
		}
	}
	items := make([]repr.ProposalRecord, 0, len(rows))
	for i := range rows {
		rec := proposalFrom(&rows[i])
		if withPatch {
			patch := decodeJSONObject(rows[i].Patch)
			eff := effective[rows[i].ID]
			if eff == nil {
				eff = map[string]any{}
			}
			attachPatch(&rec, &rows[i], patch, eff, conflicts[rows[i].ID])
		}
		items = append(items, rec)
	}
	return items, nil
}

func sortedMapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func attachPatch(rec *repr.ProposalRecord, p *editing.Proposal, patch, effective map[string]any, conflicts []string) {
	changed := sortedMapKeys(effective)
	rec.Patch, rec.EffectivePatch, rec.ChangedFields = &patch, &effective, &changed
	if p.Status == editing.StatusOpen {
		if conflicts == nil {
			conflicts = []string{}
		}
		rec.ConflictFields = &conflicts
	}
}
