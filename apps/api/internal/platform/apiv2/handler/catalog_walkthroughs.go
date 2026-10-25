package handler

import (
	"context"
	"net/http"
	"strings"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/dto"
	catmodel "api/internal/platform/catalog/model"
	catsvc "api/internal/platform/catalog/service"
	"api/internal/platform/catalog/walkthrough"

	"github.com/danielgtaylor/huma/v2"
)

type listWalkthroughsInput struct {
	CollectionInput
	WorkID       string `query:"work_id" maxLength:"20" doc:"Catalog work id. Only walkthroughs of this work."`
	CharacterID  string `query:"character_id" maxLength:"20" doc:"Catalog character id. Only walkthroughs whose chart routes to this character."`
	ContentLimit string `query:"content_limit" maxLength:"32" example:"sfw" doc:"Comma-separated closed editorial axis: sfw, nsfw, applied to each walkthrough's work exactly as on /v2/catalog/works. Absent applies no gate."`
}

type listWalkthroughsOutput struct {
	Body repr.List[repr.Walkthrough]
}

type getWalkthroughOutput struct {
	Body repr.Walkthrough
}

func registerCatalogWalkthroughs(api huma.API, cat *Catalog) {
	catalog := []string{"catalog"}
	huma.Register(api, huma.Operation{
		OperationID:        "listCatalogWalkthroughs",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/walkthroughs",
		Summary:            "List walkthroughs",
		Description:        "Keyset-paginated walkthroughs, sorted by id. work_id= keeps one work's walkthroughs; character_id= keeps those whose chart routes to that character. A walkthrough is listed exactly when its work is visible under the same nsfw gate as GET /v2/catalog/works/{id}. nsfw= gates only on the age axis content_rating; content_limit= gates on the display axis, the work's content_limit, which every item carries as work_content_limit. The two are independent and most r18 games are sfw, so a site that hides adult content from a reader passes nsfw=true&content_limit=sfw, not nsfw=false. Items carry no chart; GET /v2/catalog/walkthroughs/{id} does. Anonymous calls are allowed and limited per IP address; an application key or a user access token with catalog:read gets its own, higher limit. ids= is a batch lane and does not paginate.",
		Tags:               catalog,
		Errors:             collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable),
		SkipValidateParams: true,
	}, listCatalogWalkthroughs(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "listMyWalkthroughBacklog",
		Method:             http.MethodGet,
		Path:               "/v2/me/walkthrough-backlog",
		Summary:            "Walkthroughs waiting to be completed",
		Description:        "Walkthroughs the import kept hidden because their chart was incomplete, sorted by id, each with import_review saying what is missing. A walkthrough leaves the backlog when an edit unhides it. work_id= keeps one work's; ids= is a batch lane and does not paginate. No age gate applies. Requires a user access token bound to a catalog site. The token must carry the catalog:edit scope.",
		Tags:               []string{"me"},
		Errors:             collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable),
		SkipValidateParams: true,
	}, listMyWalkthroughBacklog(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "getCatalogWalkthrough",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/walkthroughs/{id}",
		Summary:            "Get one walkthrough",
		Description:        "A walkthrough's choice graph: entries, saves and endings as nodes, segments of choices as edges, and the characters each route and ending belongs to. Text fields are in the walkthrough's lang with machine translations in localized. Derived from the page at source_url; its prose is not reproduced. 404 when the work is not visible: r18 needs nsfw=true. work_content_limit carries the work's display axis, so a site can gate the page for a reader who hides adult content instead of answering 404. Anonymous calls are allowed and limited per IP address; an application key or a user access token with catalog:read gets its own, higher limit.",
		Tags:               catalog,
		Errors:             collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable),
		SkipValidateParams: true,
	}, getCatalogWalkthrough(cat))
}

func listCatalogWalkthroughs(cat *Catalog) func(context.Context, *listWalkthroughsInput) (*listWalkthroughsOutput, error) {
	return func(ctx context.Context, in *listWalkthroughsInput) (*listWalkthroughsOutput, error) {
		if in == nil {
			in = &listWalkthroughsInput{}
		}
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.WalkthroughSpec())
		if err != nil {
			return nil, err
		}
		limits, perr := closedCSV(in.ContentLimit, "content_limit", []string{
			catmodel.DisplayLimitKeySFW, catmodel.DisplayLimitKeyNSFW,
		})
		if perr != nil {
			return nil, withIdent(ctx, perr)
		}
		f := catsvc.WalkthroughsListFilter{NSFW: q.NSFW, DisplayLimits: limits}
		if id, perr := optionalID(in.WorkID, "work_id"); perr != nil {
			return nil, withIdent(ctx, perr)
		} else if id > 0 {
			f.WorkID = &id
		}
		if id, perr := optionalID(in.CharacterID, "character_id"); perr != nil {
			return nil, withIdent(ctx, perr)
		} else if id > 0 {
			f.CharacterID = &id
		}
		page, lerr := cat.ListWalkthroughs(ctx, q, f)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listWalkthroughsOutput{Body: page}, nil
	}
}

type listBacklogInput struct {
	CollectionInput
	WorkID string `query:"work_id" maxLength:"20" doc:"Catalog work id. Only this work's walkthroughs."`
}

func listMyWalkthroughBacklog(cat *Catalog) func(context.Context, *listBacklogInput) (*listWalkthroughsOutput, error) {
	return func(ctx context.Context, in *listBacklogInput) (*listWalkthroughsOutput, error) {
		if in == nil {
			in = &listBacklogInput{}
		}
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.WalkthroughSpec())
		if err != nil {
			return nil, err
		}
		f := catsvc.WalkthroughsListFilter{Backlog: true}
		if id, perr := optionalID(in.WorkID, "work_id"); perr != nil {
			return nil, withIdent(ctx, perr)
		} else if id > 0 {
			f.WorkID = &id
		}
		page, lerr := cat.ListWalkthroughs(ctx, q, f)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listWalkthroughsOutput{Body: page}, nil
	}
}

func getCatalogWalkthrough(cat *Catalog) func(context.Context, *ResourceIDInput) (*getWalkthroughOutput, error) {
	return func(ctx context.Context, in *ResourceIDInput) (*getWalkthroughOutput, error) {
		id, q, err := parseResource(ctx, in, collect.WalkthroughSpec())
		if err != nil {
			return nil, err
		}
		rec, gerr := cat.GetWalkthrough(ctx, id, q.NSFW)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &getWalkthroughOutput{Body: rec}, nil
	}
}

func (c *Catalog) ListWalkthroughs(ctx context.Context, q collect.Query, f catsvc.WalkthroughsListFilter) (repr.List[repr.Walkthrough], error) {
	if c == nil || c.Public == nil {
		return repr.List[repr.Walkthrough]{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	ids, missing, err := c.batchEntityIDs(ctx, q, entityTypeNone)
	if err != nil {
		return repr.List[repr.Walkthrough]{}, err
	}
	if q.Batch && len(ids) == 0 {
		return finishList([]repr.Walkthrough{}, nil, 0, q, missing), nil
	}
	f.IDs, f.WithTotal = ids, q.IncludeTotal
	limit := q.Limit
	if q.Batch {
		limit = 100
		q.Cursor = ""
	}
	data, lerr := c.Public.WalkthroughsList(ctx, f, q.Cursor, limit)
	if lerr != nil {
		return repr.List[repr.Walkthrough]{}, listCursorErr(lerr)
	}
	items := make([]repr.Walkthrough, 0, len(data.Items))
	seen := map[int64]bool{}
	for _, it := range data.Items {
		items = append(items, walkthroughFrom(it))
		seen[it.ID] = true
	}
	missing = appendUnseen(missing, ids, seen)
	return finishList(items, data.NextCursor, data.Total, q, missing), nil
}

func (c *Catalog) GetWalkthrough(ctx context.Context, id int64, nsfw bool) (repr.Walkthrough, error) {
	if c == nil || c.Public == nil {
		return repr.Walkthrough{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	rec, found, err := c.Public.WalkthroughDetail(ctx, id, nsfw)
	if err != nil {
		return repr.Walkthrough{}, err
	}
	if !found {
		return repr.Walkthrough{}, problem.New(problem.CodeNotFound, "", "", "walkthrough not found.")
	}
	return walkthroughFrom(rec), nil
}

func walkthroughFrom(w dto.PublicWalkthrough) repr.Walkthrough {
	out := repr.Walkthrough{
		Object: "walkthrough", ID: repr.ID(w.ID), WorkID: repr.ID(w.WorkID), WorkContentLimit: w.WorkContentLimit, Source: w.Source,
		SourceAuthor: optString(w.SourceAuthor), Lang: w.Lang,
		Characters:  make([]repr.WalkthroughCharacter, 0, len(w.Characters)),
		EndingCount: w.EndingCount, SaveCount: w.SaveCount, SegmentCount: w.SegmentCount,
		UpdatedAt: rfc3339(w.UpdatedAt),
	}
	if !strings.HasPrefix(w.SourceURL, catmodel.WalkthroughNativeURLPrefix) {
		out.SourceURL = &w.SourceURL
	}
	if w.SourceUpdatedOn != nil {
		d := w.SourceUpdatedOn.Format("2006-01-02")
		out.SourceUpdatedOn = &d
	}
	for _, ch := range w.Characters {
		rc := repr.WalkthroughCharacter{Key: ch.Key, Name: ch.Name, Localized: localizedFrom(ch.Localized)}
		if ch.CharacterID != nil {
			id := repr.ID(*ch.CharacterID)
			rc.CharacterID, rc.DisplayName = &id, &ch.DisplayName
		}
		out.Characters = append(out.Characters, rc)
	}
	if w.Chart != nil {
		out.Chart = walkthroughChartFrom(*w.Chart, walkthroughLocalizer(w))
	}
	if r := w.ImportReview; r != nil {
		out.ImportReview = &repr.WalkthroughImportReview{
			Missed: orEmptyStrings(r.Missed), EndMarkers: r.EndMarkers, EndingsReached: r.EndingsReached, Coverage: r.Coverage,
		}
	}
	return out
}

// walkthroughLocalizer reads the chart's *_zh strings. An imported chart's
// are machine translations; a chart written in the editor has people's, and a
// chart written in Chinese has none to offer.
func walkthroughLocalizer(w dto.PublicWalkthrough) func(zh *string) map[string]repr.LocalizedText {
	machine := !strings.HasPrefix(w.SourceURL, catmodel.WalkthroughNativeURLPrefix)
	return func(zh *string) map[string]repr.LocalizedText {
		out := map[string]repr.LocalizedText{}
		if zh != nil && *zh != "" && w.Lang != catmodel.WalkthroughLangZhHans {
			out["zh-Hans"] = repr.LocalizedText{Value: *zh, IsMachine: machine}
		}
		return out
	}
}

func walkthroughNode(n walkthrough.Node) repr.WalkthroughNode {
	return repr.WalkthroughNode{NodeType: n.NodeType, Key: n.Key}
}

func walkthroughChartFrom(c walkthrough.Chart, walkthroughLocalized func(*string) map[string]repr.LocalizedText) *repr.WalkthroughChart {
	out := &repr.WalkthroughChart{
		Sections: make([]repr.WalkthroughSection, 0, len(c.Sections)),
		Entries:  make([]repr.WalkthroughEntry, 0, len(c.Entries)),
		Saves:    make([]repr.WalkthroughSave, 0, len(c.Saves)),
		Endings:  make([]repr.WalkthroughEnding, 0, len(c.Endings)),
		Segments: make([]repr.WalkthroughSegment, 0, len(c.Segments)),
	}
	for _, s := range c.Sections {
		out.Sections = append(out.Sections, repr.WalkthroughSection{
			Key: s.Key, Title: s.Title, Localized: walkthroughLocalized(s.TitleZh), CharacterKeys: orEmptyStrings(s.Characters),
		})
	}
	for _, e := range c.Entries {
		out.Entries = append(out.Entries, repr.WalkthroughEntry{Key: e.Key, Label: e.Label, UnlockedByEndingKey: e.UnlockedBy})
	}
	for _, s := range c.Saves {
		out.Saves = append(out.Saves, repr.WalkthroughSave{Key: s.Key, Label: s.Label})
	}
	for _, e := range c.Endings {
		out.Endings = append(out.Endings, repr.WalkthroughEnding{
			Key: e.Key, Name: e.Name, Localized: walkthroughLocalized(e.NameZh), EndingType: e.EndingType, CharacterKeys: orEmptyStrings(e.Characters),
		})
	}
	for _, s := range c.Segments {
		seg := repr.WalkthroughSegment{
			Key: s.Key, SectionKey: s.Section, FromNode: walkthroughNode(s.From), ToNode: walkthroughNode(s.To),
			Condition: s.Condition, ConditionLocalized: walkthroughLocalized(s.ConditionZh),
			Steps: make([]repr.WalkthroughStep, 0, len(s.Steps)),
		}
		for _, st := range s.Steps {
			step := repr.WalkthroughStep{
				StepType: st.StepType, Text: st.Text, Localized: walkthroughLocalized(st.TextZh),
				Achievement: st.Achievement, SaveKey: st.Save,
				Alternatives: make([]repr.WalkthroughAlternative, 0, len(st.Alt)),
			}
			for i, a := range st.Alt {
				var zh *string
				if i < len(st.AltZh) {
					zh = st.AltZh[i]
				}
				step.Alternatives = append(step.Alternatives, repr.WalkthroughAlternative{Text: a, Localized: walkthroughLocalized(zh)})
			}
			seg.Steps = append(seg.Steps, step)
		}
		out.Segments = append(out.Segments, seg)
	}
	return out
}

func orEmptyStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
