package handler

import (
	"context"
	"strconv"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/editing"
)

func amendmentFrom(a *editing.ProposalAmendment) repr.Amendment {
	return repr.Amendment{
		Object: "amendment", ID: repr.ID(a.ID), Seq: a.Seq,
		AmenderUID: repr.ID(a.AmenderUID), Note: a.Note, CreatedAt: rfc3339(a.CreatedAt),
	}
}

func amendmentsFrom(items []editing.ProposalAmendment) []repr.Amendment {
	out := make([]repr.Amendment, 0, len(items))
	for i := range items {
		out = append(out, amendmentFrom(&items[i]))
	}
	return out
}

type proposalFilter struct {
	Object      string
	EntityType  string
	EntityID    string
	Site        string
	ProposerUID string
	State       string
	// The me lane's rows are already fenced to one proposer, so entity_id=
	// alone narrows only what the caller owns and cannot leak a neighbouring
	// family's proposal to anyone. On the queue lanes the same parameter is the
	// authority discriminant — naming one entity is what admits an owner who
	// holds no review permission — so an ambiguous id there is a hole, not a
	// loose filter, and stays 422.
	ProposerScoped bool
}

func (c *Catalog) proposalQuery(f proposalFilter, limit int, cursor int64) (editing.ProposalFilter, error) {
	out := editing.ProposalFilter{Site: f.Site, Status: -1, Limit: limit, BeforeID: cursor}
	if f.Object != "" {
		entityType := schemaEntityType(f.Object)
		if entityType == "" {
			p := problem.New(problem.CodeUnknownEnumValue, "", "", "object= is not an editable family.")
			p.Errors = []problem.FieldError{{Parameter: "object", Reason: problem.ReasonUnknownValue,
				Detail: "allowed values: work, company, character, release, tag, engine, series",
				Params: &problem.FieldParams{Allowed: &[]string{"work", "company", "character", "release", "tag", "engine", "series"}}}}
			return out, p
		}
		out.EntityType = entityType
	}
	// entity_type= is the same editing-engine spelling POST /v2/me/proposals
	// takes in its body and every proposal record publishes; object= is the
	// family spelling. Both name one filter, so disagreeing is a contradiction
	// rather than an intersection.
	if f.EntityType != "" {
		if schemaObject(f.EntityType) == "" {
			p := problem.New(problem.CodeUnknownEnumValue, "", "", "entity_type= is not an editable type.")
			p.Errors = []problem.FieldError{{Parameter: "entity_type", Reason: problem.ReasonUnknownValue,
				Detail: "allowed values: catalog.work, catalog.company, catalog.character, catalog.release, catalog.tag, catalog.engine, catalog.series",
				Params: &problem.FieldParams{Allowed: &[]string{"catalog.work", "catalog.company", "catalog.character", "catalog.release", "catalog.tag", "catalog.engine", "catalog.series"}}}}
			return out, p
		}
		if out.EntityType != "" && out.EntityType != f.EntityType {
			p := problem.New(problem.CodeMutuallyExclusiveParameters, "", "", "object= and entity_type= name different families.")
			p.Errors = []problem.FieldError{{Parameter: "entity_type", Reason: problem.ReasonInconsistentWith,
				Detail: "object=" + f.Object + " is " + schemaEntityType(f.Object)}}
			return out, p
		}
		out.EntityType = f.EntityType
	}
	if f.EntityID != "" {
		id, ok := repr.ParseID(f.EntityID)
		if !ok {
			return out, badFilterID("entity_id", f.EntityID)
		}
		if out.EntityType == "" && !f.ProposerScoped {
			p := problem.New(problem.CodeValidationFailed, "", "", "entity_id= needs object= to be unambiguous.")
			p.Errors = []problem.FieldError{{Parameter: "entity_id", Reason: problem.ReasonInconsistentWith,
				Detail: "entity ids are only unique within one family; pass object= as well"}}
			return out, p
		}
		out.EntityID = id
	}
	if f.ProposerUID != "" {
		id, ok := repr.ParseID(f.ProposerUID)
		if !ok {
			return out, badFilterID("proposer_uid", f.ProposerUID)
		}
		out.ProposerUID = id
	}
	if f.State != "" {
		st, ok := proposalStateValue(f.State)
		if !ok {
			p := problem.New(problem.CodeUnknownEnumValue, "", "", "state= is not a proposal state.")
			p.Errors = []problem.FieldError{{Parameter: "state", Reason: problem.ReasonUnknownValue,
				Detail: "allowed values: open, pending, merged, declined, withdrawn",
				Params: &problem.FieldParams{Allowed: &[]string{"open", "pending", "merged", "declined", "withdrawn"}}}}
			return out, p
		}
		out.Status = st
	}
	return out, nil
}

func (c *Catalog) ListPublicProposals(ctx context.Context, q collect.Query, f proposalFilter) (repr.List[repr.ProposalRecord], error) {
	if c == nil || c.Engine == nil {
		return repr.List[repr.ProposalRecord]{}, problem.New(problem.CodeServiceUnavailable, "", "", "proposals are not bound.")
	}
	if q.Batch {
		return c.batchPublicProposals(ctx, q)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = collect.DefaultLimit
	}
	cursor, cerr := claimEventCursor(q.Cursor)
	if cerr != nil {
		return repr.List[repr.ProposalRecord]{}, cerr
	}
	filter, ferr := c.proposalQuery(f, limit+1, cursor)
	if ferr != nil {
		return repr.List[repr.ProposalRecord]{}, ferr
	}
	rows, total, lerr := c.Engine.ListProposalsWithTotal(ctx, filter)
	if lerr != nil {
		return repr.List[repr.ProposalRecord]{}, lerr
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		s := strconv.FormatInt(rows[len(rows)-1].ID, 10)
		next = &s
	}
	items, ierr := c.proposalPage(ctx, rows, q.Include)
	if ierr != nil {
		return repr.List[repr.ProposalRecord]{}, ierr
	}
	return finishList(items, next, total, q, nil), nil
}

func (c *Catalog) batchPublicProposals(ctx context.Context, q collect.Query) (repr.List[repr.ProposalRecord], error) {
	if c.EditHistory == nil {
		return repr.List[repr.ProposalRecord]{}, problem.New(problem.CodeServiceUnavailable, "", "", "edit history is not bound.")
	}
	ids, perr := parseIDList(q.IDs)
	if perr != nil {
		return repr.List[repr.ProposalRecord]{}, perr
	}
	rows, err := c.EditHistory.ProposalsByID(ctx, ids)
	if err != nil {
		return repr.List[repr.ProposalRecord]{}, err
	}
	items, ierr := c.proposalPage(ctx, rows, q.Include)
	if ierr != nil {
		return repr.List[repr.ProposalRecord]{}, ierr
	}
	found := map[string]bool{}
	for _, it := range items {
		found[it.ID] = true
	}
	var missing []string
	for _, want := range q.IDs {
		if !found[want] {
			missing = append(missing, want)
		}
	}
	return finishList(items, nil, int64(len(items)), q, missing), nil
}

func (c *Catalog) proposalPage(ctx context.Context, rows []editing.Proposal, include []string) ([]repr.ProposalRecord, error) {
	items := make([]repr.ProposalRecord, 0, len(rows))
	var amendments map[int64][]editing.ProposalAmendment
	if hasToken(include, "amendments") && c.EditHistory != nil {
		ids := make([]int64, 0, len(rows))
		for i := range rows {
			ids = append(ids, rows[i].ID)
		}
		got, err := c.EditHistory.AmendmentsFor(ctx, ids)
		if err != nil {
			return nil, err
		}
		amendments = got
	}
	for i := range rows {
		rec := proposalFrom(&rows[i])
		if amendments != nil {
			list := amendmentsFrom(amendments[rows[i].ID])
			rec.Amendments = &list
		}
		items = append(items, rec)
	}
	return items, nil
}

func (c *Catalog) GetPublicProposal(ctx context.Context, id int64, q collect.Query) (repr.ProposalRecord, error) {
	if c == nil || c.EditHistory == nil {
		return repr.ProposalRecord{}, problem.New(problem.CodeServiceUnavailable, "", "", "edit history is not bound.")
	}
	prop, err := c.EditHistory.ProposalByID(ctx, id)
	if err != nil {
		return repr.ProposalRecord{}, err
	}
	if prop == nil || schemaObject(prop.EntityType) == "" {
		return repr.ProposalRecord{}, problem.New(problem.CodeNotFound, "", "", "No proposal with this id.")
	}
	items, ierr := c.proposalPage(ctx, []editing.Proposal{*prop}, q.Include)
	if ierr != nil {
		return repr.ProposalRecord{}, ierr
	}
	return items[0], nil
}
