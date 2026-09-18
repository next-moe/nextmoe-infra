package handler

import (
	"context"
	"net/http"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/vocab"

	"github.com/danielgtaylor/huma/v2"
)

type listObject[T any] struct {
	_      struct{} `json:"-" additionalProperties:"true"`
	Object string   `json:"object" enum:"list" doc:"Type discriminant. Always list."`
	Items  []T      `json:"items" doc:"The members of this list. Empty array, never null."`
}

type problemType struct {
	_           struct{} `json:"-" additionalProperties:"true"`
	Object      string   `json:"object" enum:"problem_type" doc:"Type discriminant. Always problem_type."`
	Code        string   `json:"code" pattern:"^[A-Z][A-Z0-9_]*[A-Z0-9]$" maxLength:"63" doc:"Top-level error code. UPPER_SNAKE."`
	Domain      string   `json:"domain" enum:"platform,catalog,me,moderation,news,store" doc:"Type URI domain segment."`
	Status      int      `json:"status" minimum:"400" maximum:"599" doc:"HTTP status this code is bound to. One status per code."`
	Type        string   `json:"type" format:"uri" maxLength:"256" doc:"Problem type URI. The last path segment is the kebab-case form of code."`
	Title       string   `json:"title" maxLength:"128" pattern:"^[ -~]+$" doc:"Stable English phrase for this type. Does not vary per request."`
	Description string   `json:"description" maxLength:"512" doc:"English prose. Must not be used as a discriminant."`
}

type problemReason struct {
	_           struct{} `json:"-" additionalProperties:"true"`
	Object      string   `json:"object" enum:"problem_reason" doc:"Type discriminant. Always problem_reason."`
	Reason      string   `json:"reason" pattern:"^[A-Z][A-Z0-9_]*[A-Z0-9]$" maxLength:"63" doc:"Field-level reason. UPPER_SNAKE. Disjoint from top-level codes."`
	Title       string   `json:"title" maxLength:"128" pattern:"^[ -~]+$" doc:"Stable English phrase for this reason."`
	Description string   `json:"description" maxLength:"512" doc:"English prose. Must not be used as a discriminant."`
	ParamNames  []string `json:"param_names" enum:"max_length,min_length,minimum,maximum,max_items,min_items,allowed" maxItems:"7" doc:"Keys this reason can carry in a field error's params. Empty array when it carries none."`
}

type getProblemInput struct {
	Code string `path:"code" maxLength:"63" pattern:"^[A-Z][A-Z0-9_]*[A-Z0-9]$" doc:"Top-level error code from the registry (UPPER_SNAKE)."`
}
type getProblemOutput struct {
	Body problemType
}
type listReasonsOutput struct {
	Body listObject[problemReason]
}
type getVocabInput struct {
	Name string `path:"name" maxLength:"63" pattern:"^[a-z][a-z0-9_]*$" doc:"Vocabulary name. The path segment of /v2/vocabularies/{name}."`
}
type getVocabOutput struct {
	Body vocab.Vocabulary
}

func registerMeta(api huma.API) {
	tags := []string{"meta"}
	huma.Register(api, huma.Operation{
		OperationID: "listProblemReasons",
		Method:      http.MethodGet,
		Path:        "/v2/problems/reasons",
		Summary:     "List every field-level error reason",
		Description: "The closed registry of field-level reasons. Unauthenticated. Values in this list never appear as top-level codes.",
		Tags:        tags,
		Errors:      []int{http.StatusTooManyRequests, http.StatusInternalServerError},
	}, listProblemReasons)
	huma.Register(api, huma.Operation{
		OperationID: "getProblemType",
		Method:      http.MethodGet,
		Path:        "/v2/problems/{code}",
		Summary:     "Get one top-level error code",
		Description: "Returns the registry entry for one code. Unknown codes are 404 NOT_FOUND, not 422 — the path parameter is a lookup key, not a closed enum.",
		Tags:        tags,
		Errors:      []int{http.StatusNotFound, http.StatusTooManyRequests, http.StatusInternalServerError},
	}, getProblemType)
	huma.Register(api, huma.Operation{
		OperationID: "getVocabulary",
		Method:      http.MethodGet,
		Path:        "/v2/vocabularies/{name}",
		Summary:     "Get one vocabulary",
		Description: "Returns every published value of one vocabulary. Unknown names are 404 NOT_FOUND.",
		Tags:        tags,
		Errors:      []int{http.StatusNotFound, http.StatusTooManyRequests, http.StatusInternalServerError},
	}, getVocabulary)
}

func listProblemReasons(ctx context.Context, _ *struct{}) (*listReasonsOutput, error) {
	items := make([]problemReason, 0, len(problem.Reasons))
	for _, d := range problem.Reasons {
		params := d.Params
		if params == nil {
			params = []string{}
		}
		items = append(items, problemReason{
			Object: "problem_reason", Reason: d.Reason, Title: d.Title, Description: d.Description, ParamNames: params,
		})
	}
	return &listReasonsOutput{Body: listObject[problemReason]{Object: "list", Items: items}}, nil
}

func getProblemType(ctx context.Context, in *getProblemInput) (*getProblemOutput, error) {
	d, ok := problem.Lookup(in.Code)
	if !ok {
		id, inst := ident(ctx)
		return nil, problem.New(problem.CodeNotFound, id, inst, "No problem type named "+in.Code+".")
	}
	return &getProblemOutput{Body: problemTypeFrom(d)}, nil
}

func getVocabulary(ctx context.Context, in *getVocabInput) (*getVocabOutput, error) {
	v, ok := vocab.Lookup(in.Name)
	if !ok {
		id, inst := ident(ctx)
		return nil, problem.New(problem.CodeNotFound, id, inst, "No vocabulary named "+in.Name+".")
	}
	return &getVocabOutput{Body: v}, nil
}

func problemTypeFrom(d problem.Def) problemType {
	return problemType{
		Object:      "problem_type",
		Code:        d.Code,
		Domain:      string(d.Domain),
		Status:      d.Status,
		Type:        d.TypeURI(),
		Title:       d.Title,
		Description: d.Description,
	}
}

func ident(ctx context.Context) (string, string) {
	id, _ := ctx.Value("request_id").(string)
	inst, _ := ctx.Value("instance").(string)
	return id, inst
}
