package handler

import (
	"context"
	"maps"
	"net/http"
	"slices"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/model"
	catsvc "api/internal/platform/catalog/service"

	"github.com/danielgtaylor/huma/v2"
)

type mergePreviewInput struct {
	Object   string `query:"object" required:"true" enum:"work,company,character,person,credit_name" doc:"Family of the two entries."`
	SourceID string `query:"source_id" required:"true" pattern:"^[0-9]+$" maxLength:"20" doc:"The entry to fold away."`
	TargetID string `query:"target_id" required:"true" pattern:"^[0-9]+$" maxLength:"20" doc:"The entry that survives."`
}

type mergePreviewOutput struct {
	Body repr.MergePreview
}

type mergeCandidatesInput struct {
	Object string `query:"object" required:"true" enum:"work,company,character,person,credit_name" doc:"Family of the entry."`
	ID     string `query:"id" required:"true" pattern:"^[0-9]+$" maxLength:"20" doc:"The entry to find duplicates of."`
}

type mergeCandidatesOutput struct {
	Body repr.List[repr.MergeCandidate]
}

type amendMergeRequestInput struct {
	ID   string `path:"id" pattern:"^[0-9]+$" maxLength:"20" doc:"Merge request id."`
	Body mergeChoicesBody
}

func registerMergeWorkbench(api huma.API, cat *Catalog, errs []int) {
	me, mod := []string{"me"}, []string{"moderation"}
	huma.Register(api, huma.Operation{
		OperationID: "previewMyMerge", Method: http.MethodGet, Path: "/v2/me/merge-preview",
		Summary: "Preview a merge",
		Description: "Computes what merging source_id into target_id would do, without writing: both sides' values for the resolvable fields, the fate of every external id, the releases of both works with source duplicates marked, title and intro outcomes, row counts, the site claim, and blockers and warnings. " +
			"An entry already merged away or deleted comes back with a not_live blocker rather than an error. " +
			"Requires a user access token bound to a catalog site. The token must carry the catalog:edit scope.",
		Tags: me, Errors: errs, SkipValidateParams: true,
	}, previewMyMerge(cat))
	huma.Register(api, huma.Operation{
		OperationID: "listMyMergeCandidates", Method: http.MethodGet, Path: "/v2/me/merge-candidates",
		Summary:     "Suggest merge partners",
		Description: "Up to ten entries that may duplicate id: undecided pairs from the reconciliation queue first, then live entries with exactly the same name. A single page; there is no cursor. Requires a user access token bound to a catalog site. The token must carry the catalog:edit scope.",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, listMyMergeCandidates(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getMyMergeRequest", Method: http.MethodGet, Path: "/v2/me/merge-requests/{id}",
		Summary:     "Get one merge request",
		Description: "Any editor-filed merge request by id, for the requester and everyone following it. Requires a user access token bound to a catalog site. The token must carry the catalog:edit scope.",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, getMyMergeRequest(cat))
	huma.Register(api, huma.Operation{
		OperationID: "amendModerationMergeRequest", Method: http.MethodPatch, Path: "/v2/moderation/merge-requests/{id}",
		Summary:     "Change a merge request's choices",
		Description: "Replaces field_resolution and hide_release_ids on an open or approved request; amended_by and amended_at record who. Requires the catalog.merge.review permission.",
		Tags:        mod, Errors: errs, SkipValidateParams: true,
	}, amendModerationMergeRequest(cat))
}

func previewMyMerge(cat *Catalog) func(context.Context, *mergePreviewInput) (*mergePreviewOutput, error) {
	return func(ctx context.Context, in *mergePreviewInput) (*mergePreviewOutput, error) {
		if err := cat.mergesBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if _, err := requireFirstPartyUser(ctx); err != nil {
			return nil, catalogErr(ctx, err)
		}
		et, ok := mergeEntityTypeOf(in.Object)
		if !ok {
			return nil, catalogErr(ctx, problem.New(problem.CodeNotFound, "", "", "entries of family "+in.Object+" cannot be merged."))
		}
		src, ok1 := repr.ParseID(in.SourceID)
		dst, ok2 := repr.ParseID(in.TargetID)
		if !ok1 || !ok2 {
			return nil, catalogErr(ctx, problem.New(problem.CodeValidationFailed, "", "", "source_id and target_id must be catalog ids."))
		}
		p, err := cat.Merges.PreviewMerge(ctx, et, src, dst)
		if err != nil {
			return nil, catalogErr(ctx, mergeRequestErr(err))
		}
		return &mergePreviewOutput{Body: previewFrom(in.Object, p)}, nil
	}
}

func listMyMergeCandidates(cat *Catalog) func(context.Context, *mergeCandidatesInput) (*mergeCandidatesOutput, error) {
	return func(ctx context.Context, in *mergeCandidatesInput) (*mergeCandidatesOutput, error) {
		if err := cat.mergesBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if _, err := requireFirstPartyUser(ctx); err != nil {
			return nil, catalogErr(ctx, err)
		}
		et, ok := mergeEntityTypeOf(in.Object)
		if !ok {
			return nil, catalogErr(ctx, problem.New(problem.CodeNotFound, "", "", "entries of family "+in.Object+" cannot be merged."))
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problem.New(problem.CodeValidationFailed, "", "", "id must be a catalog id."))
		}
		rows, err := cat.Merges.MergeCandidates(ctx, et, id, 10)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		out := repr.List[repr.MergeCandidate]{Object: "list", Items: make([]repr.MergeCandidate, 0, len(rows))}
		for _, r := range rows {
			out.Items = append(out.Items, repr.MergeCandidate{Object: "merge_candidate", ID: repr.ID(r.ID), Name: r.Name, Reason: r.Reason})
		}
		return &mergeCandidatesOutput{Body: out}, nil
	}
}

func getMyMergeRequest(cat *Catalog) func(context.Context, *mergeRequestIDInput) (*mergeRequestOutput, error) {
	return func(ctx context.Context, in *mergeRequestIDInput) (*mergeRequestOutput, error) {
		if err := cat.mergesBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if _, err := requireFirstPartyUser(ctx); err != nil {
			return nil, catalogErr(ctx, err)
		}
		id, _ := repr.ParseID(in.ID)
		p, err := cat.Merges.GetMergeRequest(ctx, id)
		if err != nil {
			return nil, catalogErr(ctx, mergeRequestErr(err))
		}
		if p.Origin != catsvc.OriginEditor {
			return nil, catalogErr(ctx, problem.New(problem.CodeNotFound, "", "", "no merge request with that id was filed through the editor."))
		}
		return &mergeRequestOutput{Body: cat.mergeRequestRecord(ctx, p)}, nil
	}
}

func amendModerationMergeRequest(cat *Catalog) func(context.Context, *amendMergeRequestInput) (*mergeRequestOutput, error) {
	return func(ctx context.Context, in *amendMergeRequestInput) (*mergeRequestOutput, error) {
		if err := cat.mergesBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if err := requireMergeReviewer(ctx); err != nil {
			return nil, catalogErr(ctx, err)
		}
		id, _ := repr.ParseID(in.ID)
		p, err := cat.Merges.GetMergeRequest(ctx, id)
		if err != nil {
			return nil, catalogErr(ctx, mergeRequestErr(err))
		}
		if p.Origin != catsvc.OriginEditor {
			return nil, catalogErr(ctx, problem.New(problem.CodeNotFound, "", "", "no merge request with that id was filed through the editor."))
		}
		choices, err := in.Body.choices()
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		if err := cat.Merges.AmendMergeRequest(ctx, id, userIDFrom(ctx), choices); err != nil {
			return nil, catalogErr(ctx, mergeRequestErr(err))
		}
		return cat.mergeRequestAfterWrite(ctx, id)
	}
}

func movedCounts[N int | int64](m map[string]N) []repr.MergeMovedCount {
	out := make([]repr.MergeMovedCount, 0, len(m))
	for _, key := range slices.Sorted(maps.Keys(m)) {
		if m[key] > 0 {
			out = append(out, repr.MergeMovedCount{Key: key, Count: int64(m[key])})
		}
	}
	slices.SortStableFunc(out, func(a, b repr.MergeMovedCount) int { return int(b.Count - a.Count) })
	return out
}

func undoReportFrom(r catsvc.UndoReport) *repr.MergeUndoReport {
	return &repr.MergeUndoReport{
		Restored: movedCounts(r.Restored), NotRestored: movedCounts(r.Missing),
		RevertedFields: nonNil(r.RevertedFields), KeptFields: nonNil(r.KeptFields),
		RemovedInserted: int64(r.RemovedInserted), UnhiddenRelease: int64(r.UnhiddenRelease),
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func linkKindToken(k int16) string {
	switch k {
	case model.LinkKindExact:
		return "identity"
	case model.LinkKindRelated:
		return "related"
	}
	return "probable"
}

func titleKindToken(k int16) string {
	switch k {
	case model.WorkTitleKindAlias:
		return "alias"
	case model.WorkTitleKindAbbreviation:
		return "abbreviation"
	}
	return "official"
}

func previewFrom(object string, p *catsvc.MergePreview) repr.MergePreview {
	issues := func(in []catsvc.PreviewIssue) []repr.MergePreviewIssue {
		out := make([]repr.MergePreviewIssue, 0, len(in))
		for _, i := range in {
			out = append(out, repr.MergePreviewIssue{Code: i.Code, Message: i.Message})
		}
		return out
	}
	side := func(s catsvc.PreviewSide) repr.MergePreviewSide {
		return repr.MergePreviewSide{ID: repr.ID(s.ID), Name: s.Name, Live: s.Live, Site: optString(s.Site)}
	}
	out := repr.MergePreview{
		Object: "merge_preview", TargetObject: object,
		Source: side(p.Source), Target: side(p.Target),
		Blockers: issues(p.Blockers), Warnings: issues(p.Warnings), Facts: nonNil(p.Facts),
		Fields: []repr.MergePreviewField{}, Anchors: []repr.MergePreviewAnchor{},
		Releases: []repr.MergePreviewRelease{}, Titles: []repr.MergePreviewTitle{},
		Intros: []repr.MergePreviewIntro{}, Counts: []repr.MergePreviewCount{},
	}
	if p.OpenID > 0 {
		id := repr.ID(p.OpenID)
		out.OpenRequestID = &id
	}
	for _, f := range p.Fields {
		out.Fields = append(out.Fields, repr.MergePreviewField{Key: f.Key, SourceValue: f.SourceValue,
			TargetValue: f.TargetValue, Default: f.Default, HumanProtected: f.HumanProtected})
	}
	for _, a := range p.Anchors {
		out.Anchors = append(out.Anchors, repr.MergePreviewAnchor{Side: a.Side, SourceKey: a.SourceKey,
			ExternalID: a.ExternalID, KindBefore: linkKindToken(a.KindBefore), KindAfter: linkKindToken(a.KindAfter),
			Fate: a.Fate, Reason: a.Reason})
	}
	for _, r := range p.Releases {
		rel := repr.MergePreviewRelease{Side: r.Side, ID: repr.ID(r.ID), Title: r.Title, ReleaseKind: r.KindKey,
			Platform: r.Platform, Lang: r.Lang, VNDBRid: optString(r.VNDBRid), DuplicateBy: optString(r.DuplicateBy)}
		rel.ReleaseDate, rel.ReleaseDatePrecision = catsvc.CalendarDate(r.Y, r.M, r.D)
		if r.DuplicateOf > 0 {
			id := repr.ID(r.DuplicateOf)
			rel.DuplicateOf = &id
		}
		out.Releases = append(out.Releases, rel)
	}
	for _, t := range p.Titles {
		out.Titles = append(out.Titles, repr.MergePreviewTitle{Side: t.Side, Title: t.Title, Lang: t.Lang,
			TitleKind: titleKindToken(t.Kind), Fate: t.Fate})
	}
	for _, i := range p.Intros {
		out.Intros = append(out.Intros, repr.MergePreviewIntro{Lang: i.Lang, SourceHas: i.SourceHas, TargetHas: i.TargetHas,
			SourceText: i.SourceText, TargetText: i.TargetText, Fate: i.Fate})
	}
	for _, c := range p.Counts {
		out.Counts = append(out.Counts, repr.MergePreviewCount{Key: c.Key, OnSource: c.Source, OnTarget: c.Target, AlreadyOnTarget: c.Duplicate})
	}
	if p.EntityType == model.EntityTypeWork {
		out.Claim = &repr.MergePreviewClaim{SourceSite: optString(p.Claim.SourceSite),
			TargetSite: optString(p.Claim.TargetSite), Fate: p.Claim.Fate}
	}
	return out
}
