package handler

import (
	"context"
	"net/http"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
)

type createNewsInput struct {
	Body struct {
		Source      string   `json:"source,omitempty" maxLength:"64" pattern:"^[a-z][a-z0-9_]*$" doc:"News source key. Omit it for community, which any signed-in user may submit to. Any other source must be bound to the bearer as its publisher and active."`
		Lane        string   `json:"lane,omitempty" enum:"news,column" doc:"Section of the source. Defaults to news."`
		Title       string   `json:"title" minLength:"1" maxLength:"512" doc:"Must not be used as a discriminant."`
		Summary     string   `json:"summary" minLength:"1" maxLength:"2000" doc:"The lede. At most 200 runes; longer is 422 VALIDATION_FAILED with reason TOO_LONG. Must not be used as a discriminant."`
		Body        string   `json:"body,omitempty" maxLength:"80000" doc:"The item's own text, CommonMark Markdown, at most 20000 runes (longer is 422 TOO_LONG). Only a community submission may carry one. Must not be used as a discriminant."`
		SourceURL   string   `json:"source_url,omitempty" maxLength:"1024" doc:"Canonical link to the original item, an absolute http(s) URL. Required for a partner source, whose attribution always carries a link; optional for an original community submission. Must not be used as a discriminant."`
		PublishedAt string   `json:"published_at,omitempty" format:"date-time" maxLength:"32" doc:"RFC 3339. Defaults to the moment of submission."`
		BannerHash  string   `json:"banner_hash,omitempty" maxLength:"64" pattern:"^([0-9a-f]{64})?$" doc:"Hash of the banner, as returned by POST /v2/me/news-images. A hash that face did not store is 422 VALIDATION_FAILED with reason UNKNOWN_REFERENCE."`
		WorkIDs     []string `json:"work_ids,omitempty" maxItems:"100" doc:"Catalog work ids to link. Stored with manual confidence."`
	}
}

type getNewsSubmissionInput struct {
	ID string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"News item id."`
}

type patchNewsSubmissionInput struct {
	ID      string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"News item id."`
	IfMatch string `header:"If-Match" doc:"Current ETag. Required to withdraw."`
	Body    struct {
		Status     *string   `json:"status,omitempty" enum:"withdrawn" doc:"Only withdrawn. Publishing and rejection happen in the moderation queue, never here."`
		Title      *string   `json:"title,omitempty" minLength:"1" maxLength:"512" doc:"Must not be used as a discriminant."`
		Summary    *string   `json:"summary,omitempty" minLength:"1" maxLength:"2000" doc:"At most 200 runes; longer is 422 VALIDATION_FAILED with reason TOO_LONG. Must not be used as a discriminant."`
		Body       *string   `json:"body,omitempty" maxLength:"80000" doc:"At most 20000 runes. Only a community submission may carry one; the empty string clears it. Must not be used as a discriminant."`
		SourceURL  *string   `json:"source_url,omitempty" maxLength:"1024" doc:"Canonical link to the original item, an absolute http(s) URL. The empty string clears it, which only a community submission may do. Must not be used as a discriminant."`
		BannerHash *string   `json:"banner_hash,omitempty" maxLength:"64" pattern:"^([0-9a-f]{64})?$" doc:"Hash of the banner, as returned by POST /v2/me/news-images; a hash that face did not store is 422 VALIDATION_FAILED with reason UNKNOWN_REFERENCE. The empty string clears the banner."`
		WorkIDs    *[]string `json:"work_ids,omitempty" maxItems:"100" doc:"Replaces the whole linked-work set."`
	}
}

type listNewsSubmissionsOutput struct {
	Body repr.List[repr.NewsSubmission]
}

type newsSubmissionOutput struct {
	ETag string `header:"ETag"`
	Body repr.NewsSubmission
}

type createNewsSubmissionOutput struct {
	Location string `header:"Location"`
	ETag     string `header:"ETag"`
	Body     repr.NewsSubmission
}

func registerMeNews(api huma.API, cat *Catalog) {
	me := []string{"me"}
	errs := collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable)
	writeErrs := append(errs, http.StatusUnprocessableEntity, http.StatusConflict, http.StatusPreconditionRequired, http.StatusPreconditionFailed)

	huma.Register(api, huma.Operation{
		OperationID: "listMyNews", Method: http.MethodGet, Path: "/v2/me/news",
		Summary: "List my news items", Description: "Items the bearer submitted, plus every item under a partner source bound to the bearer, pending included. Keyset-paginated. Requires a user access token.",
		Tags: me, Errors: errs, SkipValidateParams: true,
	}, listMyNews(cat))
	huma.Register(api, huma.Operation{
		OperationID: "createMyNews", Method: http.MethodPost, Path: "/v2/me/news",
		Summary: "Submit a news item", Description: "Any signed-in user may submit to source community (the default): the item lands on pending and goes out once a moderator publishes it, and an account that has used up catalog.news_submissions_per_day inside the sliding window is refused 429 QUOTA_EXCEEDED. A partner source must be bound to the bearer and active; its item lands on pending, or straight on published when the source is trusted. Requires a user access token.",
		Tags: me, Errors: append(writeErrs, http.StatusTooManyRequests), DefaultStatus: http.StatusCreated, SkipValidateParams: true,
	}, createMyNews(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getMyNewsItem", Method: http.MethodGet, Path: "/v2/me/news/{id}",
		Summary: "Get one of my news items", Description: "Carries an ETag for If-Match. Requires a user access token.",
		Tags: me, Errors: errs, SkipValidateParams: true,
	}, getMyNewsItem(cat))
	huma.Register(api, huma.Operation{
		OperationID: "patchMyNewsItem", Method: http.MethodPatch, Path: "/v2/me/news/{id}",
		Summary: "Edit or withdraw one of my news items", Description: `A pending or published item may be edited (title/summary/body/source_url/banner_hash/work_ids). A pending edit is scored again; a published edit takes the item off the feed and back to pending until a moderator publishes it again, unless its source is trusted. A published item may be withdrawn with {"status":"withdrawn"} and If-Match. rejected and withdrawn are terminal.`,
		Tags: me, Errors: writeErrs, SkipValidateParams: true,
	}, patchMyNewsItem(cat))
	huma.Register(api, huma.Operation{
		OperationID: "uploadMyNewsImage", Method: http.MethodPost, Path: "/v2/me/news-images",
		Summary:     "Upload a banner for a news item",
		Description: "multipart/form-data with file: a JPEG, PNG or WebP. Returns the hash a news item carries in banner_hash. A banner has to come from here: POST and PATCH /v2/me/news refuse a hash this face did not store, because bytes held under another site's image client are not kept alive for the news feed. An account that has used up catalog.news_image_uploads_per_day for the UTC day is refused 429 QUOTA_EXCEEDED. Requires a user access token.",
		Tags:        me, Errors: collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity, http.StatusServiceUnavailable),
		DefaultStatus: http.StatusCreated, SkipValidateParams: true,
	}, uploadMyNewsImage(cat))
}

func listMyNews(cat *Catalog) func(context.Context, *CollectionInput) (*listNewsSubmissionsOutput, error) {
	return func(ctx context.Context, in *CollectionInput) (*listNewsSubmissionsOutput, error) {
		q, err := parseCatalogList(ctx, in, collect.NewsSubmissionSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListMyNews(ctx, q)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listNewsSubmissionsOutput{Body: page}, nil
	}
}

func createMyNews(cat *Catalog) func(context.Context, *createNewsInput) (*createNewsSubmissionOutput, error) {
	return func(ctx context.Context, in *createNewsInput) (*createNewsSubmissionOutput, error) {
		if in == nil {
			in = &createNewsInput{}
		}
		rec, etag, err := cat.CreateMyNews(ctx, newsSubmissionBody{
			Source: in.Body.Source, Lane: in.Body.Lane, Title: in.Body.Title,
			Summary: in.Body.Summary, Body: in.Body.Body, SourceURL: in.Body.SourceURL,
			PublishedAt: in.Body.PublishedAt, BannerHash: in.Body.BannerHash,
			WorkIDs: in.Body.WorkIDs,
		})
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &createNewsSubmissionOutput{Location: "/v2/me/news/" + rec.ID, ETag: etag, Body: rec}, nil
	}
}

func getMyNewsItem(cat *Catalog) func(context.Context, *getNewsSubmissionInput) (*newsSubmissionOutput, error) {
	return func(ctx context.Context, in *getNewsSubmissionInput) (*newsSubmissionOutput, error) {
		if in == nil {
			in = &getNewsSubmissionInput{}
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		rec, etag, err := cat.GetMyNews(ctx, id)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &newsSubmissionOutput{ETag: etag, Body: rec}, nil
	}
}

func patchMyNewsItem(cat *Catalog) func(context.Context, *patchNewsSubmissionInput) (*newsSubmissionOutput, error) {
	return func(ctx context.Context, in *patchNewsSubmissionInput) (*newsSubmissionOutput, error) {
		if in == nil {
			in = &patchNewsSubmissionInput{}
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		rec, etag, err := cat.PatchMyNews(ctx, id, newsPatchBody{
			Status: in.Body.Status, Title: in.Body.Title, Summary: in.Body.Summary, Body: in.Body.Body,
			SourceURL: in.Body.SourceURL, BannerHash: in.Body.BannerHash, WorkIDs: in.Body.WorkIDs,
		}, in.IfMatch)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &newsSubmissionOutput{ETag: etag, Body: rec}, nil
	}
}

type newsImageForm struct {
	File huma.FormFile `form:"file" required:"true" contentType:"application/octet-stream" doc:"The image bytes: JPEG, PNG or WebP. The ceiling is the service's own body limit, not a field constraint."`
}

type uploadNewsImageInput struct {
	RawBody huma.MultipartFormFiles[newsImageForm]
}

type uploadNewsImageOutput struct {
	Location string `header:"Location" doc:"Absolute URL of the stored image."`
	Body     repr.NewsImage
}

func uploadMyNewsImage(cat *Catalog) func(context.Context, *uploadNewsImageInput) (*uploadNewsImageOutput, error) {
	return func(ctx context.Context, in *uploadNewsImageInput) (*uploadNewsImageOutput, error) {
		if in == nil {
			in = &uploadNewsImageInput{}
		}
		form := in.RawBody.Data()
		if form == nil || !form.File.IsSet {
			p := problem.New(problem.CodeValidationFailed, "", "", "a multipart file part named file is required.")
			p.Errors = []problem.FieldError{{Pointer: "/file", Reason: problem.ReasonRequired,
				Detail: "send multipart/form-data with a file part"}}
			return nil, catalogErr(ctx, p)
		}
		defer form.File.Close()
		rec, err := cat.UploadNewsImage(ctx, form.File.Filename, form.File)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &uploadNewsImageOutput{Location: rec.URL, Body: rec}, nil
	}
}
