package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/agent/model"
	"api/internal/platform/agent/service"
	"api/internal/platform/settings/keys"

	"github.com/danielgtaylor/huma/v2"
)

type SuggestionGroup struct {
	Label     string   `json:"label"`
	Icon      string   `json:"icon" doc:"A lucide icon name."`
	WebSearch bool     `json:"web_search" doc:"Send this group's prompts with web_search on."`
	Prompts   []string `json:"prompts" doc:"More than a screenful; show a few and rotate."`
}

var defaultSuggestions = []SuggestionGroup{
	{Label: "找作品", Icon: "search", Prompts: []string{
		"推荐几部猫耳妹妹的催泪作",
		"有没有类似《Summer Pockets》的夏日青春作品",
		"最近两年评价高的纯爱作品有哪些",
		"推荐几部一个晚上就能通关的短篇",
		"有哪些适合新人入坑的经典作品",
		"推荐几部有官方中文的悬疑推理作品",
	}},
	{Label: "问资料", Icon: "book-open", Prompts: []string{
		"《千恋＊万花》有哪些角色线",
		"《白色相簿2》讲了什么故事，不要剧透结局",
		"Key 社有哪些代表作",
		"《星空列车与白的旅行》是什么类型的作品",
		"ALcot 的作品有什么特点",
		"《樱之诗》和《樱之刻》是什么关系",
	}},
	{Label: "联网查", Icon: "globe", WebSearch: true, Prompts: []string{
		"最近有哪些 galgame 新作发售",
		"今年有哪些 galgame 改编的动画",
		"《魔法使之夜》动画化有什么新消息",
	}},
}

func (s *Server) suggestions(p *model.Persona) []SuggestionGroup {
	groups := defaultSuggestions
	var custom []SuggestionGroup
	if json.Unmarshal(p.Suggestions, &custom) == nil && len(custom) > 0 {
		groups = custom
	}
	out := make([]SuggestionGroup, 0, len(groups))
	for _, g := range groups {
		if g.WebSearch && !s.webSearch() {
			continue
		}
		out = append(out, g)
	}
	return out
}

type regenerateInput struct {
	ID             string `path:"id"`
	IdempotencyKey string `header:"Idempotency-Key" required:"true" maxLength:"128"`
	Body           struct {
		MessageID string `json:"message_id" doc:"The user message to answer again. From an assistant message, pass its parent_id."`
		WebSearch bool   `json:"web_search,omitempty"`
	}
}

func (s *Server) registerBranching(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "regenerateAgentReply", Method: http.MethodPost, Path: "/v2/agent/conversations/{id}/regenerate",
		Summary: "Answer a user message again", Tags: []string{"agent"}, DefaultStatus: http.StatusAccepted,
		Description: "Adds another assistant reply under the same user message, so the replies become siblings the UI can switch between; no user message is added. Priced and streamed like sending a message."},
		func(ctx context.Context, in *regenerateInput) (*sendOutput, error) {
			c, err := requireUser(ctx)
			if err != nil {
				return nil, err
			}
			conv, err := s.conversation(ctx, c, in.ID)
			if err != nil {
				return nil, err
			}
			id, err := strconv.ParseInt(in.Body.MessageID, 10, 64)
			if err != nil {
				return nil, fail(http.StatusBadRequest, CodeNotRegenerable, "message_id must be a user message of this conversation")
			}
			return s.begin(ctx, c, service.StartRequest{Conversation: conv, Regenerate: &id, Mode: model.ModeAssistant,
				WebSearch: in.Body.WebSearch, IdemKey: strings.Clone(in.IdempotencyKey)})
		})

	huma.Register(api, huma.Operation{OperationID: "setAgentFeedback", Method: http.MethodPut, Path: "/v2/agent/messages/{id}/feedback",
		Summary: "Rate an assistant reply", Tags: []string{"agent"}, DefaultStatus: http.StatusNoContent},
		func(ctx context.Context, in *struct {
			ID   string `path:"id"`
			Body struct {
				Rating string `json:"rating" enum:"up,down,none"`
			}
		}) (*struct{}, error) {
			c, err := requireUser(ctx)
			if err != nil {
				return nil, err
			}
			id, err := strconv.ParseInt(in.ID, 10, 64)
			if err != nil {
				return nil, huma.Error404NotFound("no such reply")
			}
			rating := in.Body.Rating
			if rating == "none" {
				rating = ""
			}
			err = s.Store.SetFeedback(ctx, c.UserID, c.SiteID, id, rating)
			if errors.Is(err, service.ErrNotFound) {
				return nil, huma.Error404NotFound("no such reply")
			}
			return nil, err
		})
}

func (s *Server) begin(ctx context.Context, c *caller, req service.StartRequest) (*sendOutput, error) {
	if !s.Limits.Allow(ctx, "user:"+strconv.FormatInt(c.UserID, 10), keys.AgentUserRatePerMinute.Get(), time.Minute) {
		return nil, fail(http.StatusTooManyRequests, CodeRateLimited, "too many messages; wait a minute")
	}
	base, surcharge := int(keys.AgentPriceAssistant.Get()), int(keys.AgentPriceWebSurcharge.Get())
	web := s.webSearch() && (req.WebSearch || keys.AgentWebAuto.ForSite(uint(c.SiteID)))
	req.Price = base
	if web {
		req.Price += surcharge
	}
	if existing, err := s.Store.RunByIdem(ctx, c.UserID, req.IdemKey); err == nil {
		if existing.IdemHash != service.RequestHash(req) {
			return nil, fail(http.StatusUnprocessableEntity, CodeIdempotencyKeyReused, "Idempotency-Key was used with a different request")
		}
		return sent(existing), nil
	}
	res, err := s.Quota.Reserve(ctx, c.UserID, req.Price)
	if web && !req.WebSearch && surcharge > 0 && (errors.Is(err, service.ErrQuotaExhausted) || errors.Is(err, service.ErrInsufficientPoints)) {
		web, req.Price = false, base
		res, err = s.Quota.Reserve(ctx, c.UserID, req.Price)
	}
	switch {
	case errors.Is(err, service.ErrQuotaExhausted):
		return nil, fail(http.StatusPaymentRequired, CodeFreeUnitsExhausted, "today's free units are used up")
	case errors.Is(err, service.ErrInsufficientPoints):
		return nil, fail(http.StatusPaymentRequired, CodeInsufficientPoints, "not enough moemoepoints for this question")
	case err != nil:
		return nil, err
	}
	req.FreeHeld = res.Free
	run, created, err := s.Store.StartRun(ctx, req)
	if err != nil || !created {
		s.Quota.Release(context.WithoutCancel(ctx), c.UserID, res)
	}
	switch {
	case errors.Is(err, service.ErrRunActive):
		return nil, fail(http.StatusConflict, CodeRunActive, "a reply is still streaming in this conversation")
	case errors.Is(err, service.ErrIdempotencyUsed):
		return nil, fail(http.StatusUnprocessableEntity, CodeIdempotencyKeyReused, "Idempotency-Key was used with a different request")
	case errors.Is(err, service.ErrBadParent):
		return nil, fail(http.StatusBadRequest, CodeBadParent, "parent_id is not a message of this conversation")
	case errors.Is(err, service.ErrNotRegenerable):
		return nil, fail(http.StatusBadRequest, CodeNotRegenerable, "message_id must be a user message of this conversation")
	case err != nil:
		return nil, err
	}
	if created {
		if err := s.startTurn(ctx, c, run, res, web); err != nil {
			return nil, err
		}
	}
	return sent(run), nil
}
