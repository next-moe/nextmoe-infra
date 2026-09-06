package handler

import (
	"context"
	"net/http"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
)

// These two structs repeat the collection parameters instead of embedding
// CollectionInput because their doc strings are deliberately face-specific
// (include=diff here, include=amendments there, a per-face sort vocabulary).
// The earlier reason recorded here — "huma does not walk anonymous embedded
// structs" — was wrong: huma walks them fine, it skipped the old unexported
// collectionInput because an embed of an unexported type is an unexported
// field. listWorksInput repeats them for the face-specific-doc reason too.
//
// Neither face carries an nsfw= axis: they emit ids and field keys, never
// titles, covers or prose, and the two mirror crons read them ascending by id.
// A visibility filter would silently drop rows a watermark has already passed.
type listRevisionsInput struct {
	Cursor       string `query:"cursor" maxLength:"512" doc:"Opaque keyset cursor from a prior next_cursor. Must start with cur_."`
	Limit        string `query:"limit" maxLength:"8" doc:"Page size 1-100, default 20. Values above 100 are 400 LIMIT_TOO_LARGE, not clamped."`
	View         string `query:"view" maxLength:"16" doc:"basic (default) or full. Closed vocabulary."`
	Include      string `query:"include" maxLength:"1024" doc:"Comma-separated blocks. diff is the only token. Unknown token is 400 UNKNOWN_INCLUDE."`
	Fields       string `query:"fields" maxLength:"1024" doc:"Comma-separated top-level keys after view/include. Unknown token is 400 UNKNOWN_FIELD. object and id are always kept."`
	IDs          string `query:"ids" maxLength:"4096" doc:"Comma-separated revision ids, max 100. Batch lane: no pagination."`
	IncludeTotal string `query:"include_total" maxLength:"8" doc:"true to include total. Only true or false."`
	Sort         string `query:"sort" maxLength:"32" doc:"recorded_desc (default) or recorded_asc. Closed."`
	Object       string `query:"object" maxLength:"32" doc:"Closed family filter: work, company, character, release, tag, engine, series."`
	EntityID     string `query:"entity_id" maxLength:"20" doc:"Catalog id of one entity. Requires object=."`
	Site         string `query:"site" maxLength:"64" doc:"Tenant key. Open vocabulary; unknown values match nothing."`
	ActorUID     string `query:"actor_uid" maxLength:"20" doc:"The claiming site's own user id of the actor. Not a catalog id."`
}

type listPublicProposalsInput struct {
	Cursor       string `query:"cursor" maxLength:"512" doc:"Opaque keyset cursor from a prior next_cursor. Must start with cur_."`
	Limit        string `query:"limit" maxLength:"8" doc:"Page size 1-100, default 20. Values above 100 are 400 LIMIT_TOO_LARGE, not clamped."`
	View         string `query:"view" maxLength:"16" doc:"basic (default) or full. Closed vocabulary."`
	Include      string `query:"include" maxLength:"1024" doc:"Comma-separated blocks. amendments is the only token on this face. Unknown token is 400 UNKNOWN_INCLUDE."`
	Fields       string `query:"fields" maxLength:"1024" doc:"Comma-separated top-level keys after view/include. Unknown token is 400 UNKNOWN_FIELD. object and id are always kept."`
	IDs          string `query:"ids" maxLength:"4096" doc:"Comma-separated proposal ids, max 100. Batch lane: no pagination."`
	IncludeTotal string `query:"include_total" maxLength:"8" doc:"true to include total. Only true or false."`
	Sort         string `query:"sort" maxLength:"32" doc:"filed_desc is the only key. Closed."`
	Object       string `query:"object" maxLength:"32" doc:"Closed family filter: work, company, character, release, tag, engine, series."`
	EntityID     string `query:"entity_id" maxLength:"20" doc:"Catalog id of one entity. Requires object=."`
	Site         string `query:"site" maxLength:"64" doc:"Tenant key. Open vocabulary; unknown values match nothing."`
	ProposerUID  string `query:"proposer_uid" maxLength:"20" doc:"The claiming site's own user id of the proposer. Not a catalog id."`
	State        string `query:"state" maxLength:"16" doc:"Closed: open, merged, declined, withdrawn."`
}

type getRevisionInput struct {
	ID       string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Revision id."`
	Include  string `query:"include" maxLength:"1024" doc:"Comma-separated blocks. diff is the only token. Unknown token is 400 UNKNOWN_INCLUDE."`
	View     string `query:"view" maxLength:"16" doc:"basic (default) or full. full adds diff."`
	Fields   string `query:"fields" maxLength:"1024" doc:"Comma-separated top-level keys. Unknown token is 400 UNKNOWN_FIELD."`
	DiffBase string `query:"diff_base" maxLength:"20" doc:"Revision id to diff against. Requires include=diff. Absent means the preceding revision of the same entity."`
}

type getPublicProposalInput struct {
	ID      string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Proposal id."`
	Include string `query:"include" maxLength:"1024" doc:"Comma-separated blocks. amendments is the only token on this face. Unknown token is 400 UNKNOWN_INCLUDE."`
	View    string `query:"view" maxLength:"16" doc:"basic (default) or full. full adds amendments."`
	Fields  string `query:"fields" maxLength:"1024" doc:"Comma-separated top-level keys. Unknown token is 400 UNKNOWN_FIELD."`
}

type listRevisionsOutput struct {
	Body repr.List[repr.Revision]
}
type getRevisionOutput struct {
	Body repr.Revision
}
type getProposalRecordOutput struct {
	Body repr.ProposalRecord
}

func registerCatalogEditHistory(api huma.API, cat *Catalog) {
	catalog := []string{"catalog"}
	errs := collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable)
	detailErrs := append(errs, http.StatusNotFound, http.StatusUnprocessableEntity)

	huma.Register(api, huma.Operation{
		OperationID: "listCatalogRevisions", Method: http.MethodGet, Path: "/v2/catalog/revisions",
		Summary:     "Entity revision history",
		Description: "Every merged edit, newest first by default. sort=recorded_asc walks the same collection oldest-first by id, which is the shape a mirror or a contributor tally reads with a watermark. object=+entity_id= narrows to one entity's history. Requires an application key or a user access token with catalog:read.",
		Tags:        catalog, Errors: errs, SkipValidateParams: true,
	}, listCatalogRevisions(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getCatalogRevision", Method: http.MethodGet, Path: "/v2/catalog/revisions/{id}",
		Summary:     "One revision",
		Description: "include=diff adds the field-level change set against diff_base, or against the preceding revision when diff_base is absent. This id is what POST /v2/moderation/reverts takes. Requires an application key or a user access token with catalog:read.",
		Tags:        catalog, Errors: detailErrs, SkipValidateParams: true,
	}, getCatalogRevision(cat))
	huma.Register(api, huma.Operation{
		OperationID: "listCatalogProposals", Method: http.MethodGet, Path: "/v2/catalog/proposals",
		Summary:     "Edit proposal history",
		Description: "Filed proposals, newest first. proposer_uid=+state=merged with include_total=true is the per-contributor tally. This face publishes no patch and no decision note. Requires an application key or a user access token with catalog:read.",
		Tags:        catalog, Errors: errs, SkipValidateParams: true,
	}, listCatalogProposals(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getCatalogProposal", Method: http.MethodGet, Path: "/v2/catalog/proposals/{id}",
		Summary:     "One proposal",
		Description: "Public transparency view: proposer, state, target entity and timestamps. include=amendments adds the amendment chain. Requires an application key or a user access token with catalog:read.",
		Tags:        catalog, Errors: detailErrs, SkipValidateParams: true,
	}, getCatalogProposal(cat))
}

func listCatalogRevisions(cat *Catalog) func(context.Context, *listRevisionsInput) (*listRevisionsOutput, error) {
	return func(ctx context.Context, in *listRevisionsInput) (*listRevisionsOutput, error) {
		if in == nil {
			in = &listRevisionsInput{}
		}
		q, perr := collect.Parse(collect.Raw{
			Cursor: in.Cursor, Limit: in.Limit, View: in.View, Include: in.Include,
			Fields: in.Fields, IDs: in.IDs, IncludeTotal: in.IncludeTotal, Sort: in.Sort,
		}, collect.RevisionSpec())
		if perr != nil {
			return nil, withIdent(ctx, perr)
		}
		page, lerr := cat.ListRevisions(ctx, q, revisionFilter{
			Object: in.Object, EntityID: in.EntityID, Site: in.Site, ActorUID: in.ActorUID,
		})
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listRevisionsOutput{Body: page}, nil
	}
}

func getCatalogRevision(cat *Catalog) func(context.Context, *getRevisionInput) (*getRevisionOutput, error) {
	return func(ctx context.Context, in *getRevisionInput) (*getRevisionOutput, error) {
		if in == nil {
			in = &getRevisionInput{}
		}
		q, perr := collect.Parse(collect.Raw{
			View: in.View, Include: in.Include, Fields: in.Fields,
		}, collect.RevisionSpec())
		if perr != nil {
			return nil, withIdent(ctx, perr)
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		rec, err := cat.GetRevision(ctx, id, q, in.DiffBase)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getRevisionOutput{Body: rec}, nil
	}
}

func listCatalogProposals(cat *Catalog) func(context.Context, *listPublicProposalsInput) (*listProposalsOutput, error) {
	return func(ctx context.Context, in *listPublicProposalsInput) (*listProposalsOutput, error) {
		if in == nil {
			in = &listPublicProposalsInput{}
		}
		q, perr := collect.Parse(collect.Raw{
			Cursor: in.Cursor, Limit: in.Limit, View: in.View, Include: in.Include,
			Fields: in.Fields, IDs: in.IDs, IncludeTotal: in.IncludeTotal, Sort: in.Sort,
		}, collect.PublicProposalSpec())
		if perr != nil {
			return nil, withIdent(ctx, perr)
		}
		page, lerr := cat.ListPublicProposals(ctx, q, proposalFilter{
			Object: in.Object, EntityID: in.EntityID, Site: in.Site,
			ProposerUID: in.ProposerUID, State: in.State,
		})
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listProposalsOutput{Body: page}, nil
	}
}

func getCatalogProposal(cat *Catalog) func(context.Context, *getPublicProposalInput) (*getProposalRecordOutput, error) {
	return func(ctx context.Context, in *getPublicProposalInput) (*getProposalRecordOutput, error) {
		if in == nil {
			in = &getPublicProposalInput{}
		}
		q, perr := collect.Parse(collect.Raw{
			View: in.View, Include: in.Include, Fields: in.Fields,
		}, collect.PublicProposalSpec())
		if perr != nil {
			return nil, withIdent(ctx, perr)
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		rec, err := cat.GetPublicProposal(ctx, id, q)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getProposalRecordOutput{Body: rec}, nil
	}
}
