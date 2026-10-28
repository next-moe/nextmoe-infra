package handler

import (
	"context"
	stderrors "errors"
	"log/slog"
	"net/http"

	aiPerm "api/internal/platform/ai/perm"
	"api/internal/platform/llm"
	"api/pkg/errors"

	"github.com/danielgtaylor/huma/v2"
)

type llmAdmin struct{ a *llm.Admin }

func requireLLMManage(ctx context.Context) *houseError {
	if !aiPerm.Resolver.Can(adminRoles(ctx), aiPerm.LLMManage) {
		return apiErrMsg(http.StatusForbidden, errors.ErrForbidden, "LLM gateway configuration requires ai.llm_manage (ren)")
	}
	return nil
}

func mapLLMErr(op string, err error) *houseError {
	switch {
	case stderrors.Is(err, llm.ErrNotFound):
		return apiErr(http.StatusNotFound, errors.ErrNotFound)
	case stderrors.Is(err, llm.ErrInUse):
		return apiErrMsg(http.StatusConflict, errors.ErrValidationFailed, err.Error())
	case stderrors.Is(err, llm.ErrCatalogUnsupported):
		return apiErrMsg(http.StatusBadRequest, errors.ErrValidationFailed, "该供应商不在 /models 提供模型列表（例如 Cloudflare Workers AI 的 OpenAI 兼容地址），请在「路由」里手动添加")
	case stderrors.Is(err, llm.ErrInvalid), stderrors.Is(err, llm.ErrNoKEK):
		return apiErrMsg(http.StatusBadRequest, errors.ErrValidationFailed, err.Error())
	case stderrors.As(err, new(*llm.Error)):
		return apiErrMsg(http.StatusBadGateway, errors.ErrValidationFailed, err.Error())
	default:
		slog.Error("ai llm admin "+op, "err", err)
		return apiErr(http.StatusInternalServerError, errors.ErrInternalServer)
	}
}

// Exported because huma binds nothing from an unexported embedded input
// struct: as idPath/keyPath every {id} and {key} arrived empty.
type IDPath struct {
	ID int64 `path:"id" minimum:"1"`
}

type KeyPath struct {
	Key string `path:"key" maxLength:"64"`
}

type out[T any] struct {
	Body Envelope[T]
}

type empty struct{}

func ok[T any](v T) *out[T] { return &out[T]{Body: okEnvelope(v)} }

func guard[I, T any](op string, fn func(context.Context, *I) (T, error)) func(context.Context, *I) (*out[T], error) {
	return func(ctx context.Context, in *I) (*out[T], error) {
		if he := requireLLMManage(ctx); he != nil {
			return nil, he
		}
		v, err := fn(ctx, in)
		if err != nil {
			return nil, mapLLMErr(op, err)
		}
		return ok(v), nil
	}
}

type llmStatus struct {
	KEKConfigured bool `json:"kek_configured"`
}

type createdAccessKey struct {
	Key    llm.AccessKeyView `json:"key"`
	Secret string            `json:"secret" doc:"Shown once; only its hash is stored."`
}

func (s *llmAdmin) register(api huma.API) {
	tags := []string{"ai-admin-llm"}
	const base = "/api/v1/admin/ai/llm"
	const note = "Requires ai.llm_manage (ren) for reading and writing."
	op := func(id, method, path, summary string) huma.Operation {
		return huma.Operation{OperationID: id, Method: method, Path: base + path, Summary: summary, Description: note, Tags: tags}
	}

	huma.Register(api, op("getLLMStatus", http.MethodGet, "/status", "Whether the server can store provider API keys"),
		guard("status", func(context.Context, *struct{}) (llmStatus, error) {
			return llmStatus{KEKConfigured: s.a.KEKConfigured()}, nil
		}))

	huma.Register(api, op("listLLMProviders", http.MethodGet, "/providers", "List providers (keys are never returned)"),
		guard("list providers", func(ctx context.Context, _ *struct{}) ([]llm.ProviderView, error) { return s.a.Providers(ctx) }))
	huma.Register(api, op("createLLMProvider", http.MethodPost, "/providers", "Create a provider"),
		guard("create provider", func(ctx context.Context, in *struct{ Body llm.ProviderInput }) (llm.ProviderView, error) {
			return s.a.CreateProvider(ctx, in.Body)
		}))
	huma.Register(api, op("updateLLMProvider", http.MethodPatch, "/providers/{id}", "Update a provider; a non-empty api_key replaces the stored key"),
		guard("update provider", func(ctx context.Context, in *struct {
			IDPath
			Body llm.ProviderInput
		}) (llm.ProviderView, error) {
			return s.a.UpdateProvider(ctx, in.ID, in.Body)
		}))
	huma.Register(api, op("deleteLLMProvider", http.MethodDelete, "/providers/{id}", "Delete a provider no route uses"),
		guard("delete provider", func(ctx context.Context, in *IDPath) (*empty, error) { return nil, s.a.DeleteProvider(ctx, in.ID) }))

	huma.Register(api, op("listLLMProviderModels", http.MethodGet, "/providers/{id}/models", "The provider's model catalog from its last sync"),
		guard("list provider models", func(ctx context.Context, in *IDPath) ([]llm.ProviderModelView, error) {
			return s.a.ProviderModels(ctx, in.ID)
		}))
	huma.Register(api, op("syncLLMProviderModels", http.MethodPost, "/providers/{id}/models/sync", "Read the provider's /models now and update its catalog"),
		guard("sync provider models", func(ctx context.Context, in *IDPath) (llm.SyncResult, error) {
			return s.a.SyncProviderModels(ctx, in.ID)
		}))
	huma.Register(api, op("updateLLMProviderModel", http.MethodPatch, "/provider-models/{id}", "Set a catalog entry's kind by hand"),
		guard("update provider model", func(ctx context.Context, in *struct {
			IDPath
			Body struct {
				Kind string `json:"kind" enum:"auto,chat,embedding,moderation,search,image,video,speech,transcription,rerank" doc:"auto goes back to the kind the metadata or name suggests."`
			}
		}) (llm.ProviderModelView, error) {
			return s.a.SetProviderModelKind(ctx, in.ID, in.Body.Kind)
		}))
	huma.Register(api, op("probeLLMProviderModel", http.MethodPost, "/provider-models/{id}/probe", "Call the upstream model once as the given kind; a success records the kind"),
		guard("probe provider model", func(ctx context.Context, in *struct {
			IDPath
			Body *struct {
				Kind string `json:"kind,omitempty" enum:"chat,embedding,moderation,search,image" doc:"Defaults to the entry's kind. image generates one picture and is billed."`
			} `required:"false"`
		}) (llm.ProviderModelView, error) {
			kind := ""
			if in.Body != nil {
				kind = in.Body.Kind
			}
			return s.a.ProbeProviderModel(ctx, in.ID, kind)
		}))
	huma.Register(api, op("adoptLLMProviderModel", http.MethodPost, "/provider-models/{id}/adopt", "Route a catalog entry: create the gateway model if needed, then the route"),
		guard("adopt provider model", func(ctx context.Context, in *struct {
			IDPath
			Body llm.AdoptInput
		}) (llm.AdoptResult, error) {
			return s.a.AdoptProviderModel(ctx, in.ID, in.Body)
		}))

	huma.Register(api, op("getLLMHealth", http.MethodGet, "/health", "Every route's breaker, load, limits and last-hour numbers"),
		guard("health", func(ctx context.Context, _ *struct{}) ([]llm.RouteHealthView, error) { return s.a.Health(ctx) }))
	huma.Register(api, op("resetLLMRouteCooldown", http.MethodPost, "/routes/{id}/reset-cooldown", "End a route's cooldown in every gateway process"),
		guard("reset cooldown", func(ctx context.Context, in *IDPath) (*empty, error) { return nil, s.a.ResetCooldown(ctx, in.ID) }))
	huma.Register(api, op("planLLMScene", http.MethodGet, "/scenes/{key}/plan", "The order a call to this scene would try routes in, right now"),
		guard("plan scene", func(ctx context.Context, in *struct {
			KeyPath
			Affinity string `query:"affinity" maxLength:"128" doc:"A conversation id, to see where the weighted strategy pins it."`
		}) (llm.ScenePlan, error) {
			return s.a.PlanScene(ctx, in.Key, in.Affinity)
		}))

	huma.Register(api, op("listLLMModels", http.MethodGet, "/models", "List models"),
		guard("list models", func(ctx context.Context, _ *struct{}) ([]llm.ModelView, error) { return s.a.Models(ctx) }))
	huma.Register(api, op("createLLMModel", http.MethodPost, "/models", "Create a model"),
		guard("create model", func(ctx context.Context, in *struct{ Body llm.ModelInput }) (llm.ModelView, error) {
			return s.a.CreateModel(ctx, in.Body)
		}))
	huma.Register(api, op("updateLLMModel", http.MethodPatch, "/models/{id}", "Update a model"),
		guard("update model", func(ctx context.Context, in *struct {
			IDPath
			Body llm.ModelInput
		}) (llm.ModelView, error) {
			return s.a.UpdateModel(ctx, in.ID, in.Body)
		}))
	huma.Register(api, op("deleteLLMModel", http.MethodDelete, "/models/{id}", "Delete a model no route or scene uses"),
		guard("delete model", func(ctx context.Context, in *IDPath) (*empty, error) { return nil, s.a.DeleteModel(ctx, in.ID) }))

	huma.Register(api, op("listLLMRoutes", http.MethodGet, "/routes", "List routes with their price and last-24h stats"),
		guard("list routes", func(ctx context.Context, _ *struct{}) ([]llm.RouteView, error) { return s.a.Routes(ctx) }))
	huma.Register(api, op("createLLMRoute", http.MethodPost, "/routes", "Create a route (model × provider)"),
		guard("create route", func(ctx context.Context, in *struct{ Body llm.RouteInput }) (llm.RouteView, error) {
			return s.a.CreateRoute(ctx, in.Body)
		}))
	huma.Register(api, op("updateLLMRoute", http.MethodPatch, "/routes/{id}", "Update a route; status active also lifts a suspension; price adds a revision"),
		guard("update route", func(ctx context.Context, in *struct {
			IDPath
			Body llm.RouteInput
		}) (llm.RouteView, error) {
			return s.a.UpdateRoute(ctx, in.ID, in.Body)
		}))
	huma.Register(api, op("deleteLLMRoute", http.MethodDelete, "/routes/{id}", "Delete a route"),
		guard("delete route", func(ctx context.Context, in *IDPath) (*empty, error) { return nil, s.a.DeleteRoute(ctx, in.ID) }))
	huma.Register(api, op("testLLMRoute", http.MethodPost, "/routes/{id}/test", "Send one tiny real request through exactly this route"),
		guard("test route", func(ctx context.Context, in *IDPath) (llm.ProbeResult, error) { return s.a.Probe(ctx, in.ID) }))

	huma.Register(api, op("listLLMScenes", http.MethodGet, "/scenes", "List scenes"),
		guard("list scenes", func(ctx context.Context, _ *struct{}) ([]llm.SceneView, error) { return s.a.Scenes(ctx) }))
	huma.Register(api, op("putLLMScene", http.MethodPut, "/scenes/{key}", "Create or replace a scene"),
		guard("put scene", func(ctx context.Context, in *struct {
			KeyPath
			Body llm.SceneInput
		}) (llm.SceneView, error) {
			return s.a.PutScene(ctx, in.Key, in.Body)
		}))
	huma.Register(api, op("deleteLLMScene", http.MethodDelete, "/scenes/{key}", "Delete a scene"),
		guard("delete scene", func(ctx context.Context, in *KeyPath) (*empty, error) { return nil, s.a.DeleteScene(ctx, in.Key) }))

	huma.Register(api, op("listLLMAccessKeys", http.MethodGet, "/access-keys", "List gateway access keys"),
		guard("list access keys", func(ctx context.Context, _ *struct{}) ([]llm.AccessKeyView, error) { return s.a.AccessKeys(ctx) }))
	huma.Register(api, op("createLLMAccessKey", http.MethodPost, "/access-keys", "Create an access key; the secret is returned once"),
		guard("create access key", func(ctx context.Context, in *struct{ Body llm.AccessKeyInput }) (createdAccessKey, error) {
			k, secret, err := s.a.CreateAccessKey(ctx, in.Body)
			return createdAccessKey{Key: k, Secret: secret}, err
		}))
	huma.Register(api, op("updateLLMAccessKey", http.MethodPatch, "/access-keys/{id}", "Rename, re-scope, enable or disable an access key"),
		guard("update access key", func(ctx context.Context, in *struct {
			IDPath
			Body llm.AccessKeyInput
		}) (llm.AccessKeyView, error) {
			return s.a.UpdateAccessKey(ctx, in.ID, in.Body)
		}))
	huma.Register(api, op("deleteLLMAccessKey", http.MethodDelete, "/access-keys/{id}", "Revoke an access key"),
		guard("delete access key", func(ctx context.Context, in *IDPath) (*empty, error) { return nil, s.a.DeleteAccessKey(ctx, in.ID) }))

	huma.Register(api, op("listLLMEvents", http.MethodGet, "/events", "Recent gateway events, newest first"),
		guard("list events", func(ctx context.Context, in *struct {
			Limit int `query:"limit" default:"100" minimum:"1" maximum:"500"`
		}) ([]llm.EventView, error) {
			return s.a.Events(ctx, in.Limit)
		}))
}
