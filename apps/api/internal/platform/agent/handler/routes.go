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
	"api/internal/platform/agent/stream"
	"api/internal/platform/settings/keys"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/redis/go-redis/v9"
)

type Server struct {
	Store   *service.Store
	Runner  *service.Runner
	Broker  *stream.Broker
	Quota   *service.Quota
	Limits  *Limiter
	Search  *Searcher
	Verify  Verifier
	Redis   redis.UniversalClient
	Clock   func() time.Time
	MaxBody int
}

func who(ctx huma.Context) *caller { return callerOf(humafiber.Unwrap(ctx)) }

type ctxKey struct{}

func withCaller(api huma.API) {
	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		next(huma.WithValue(ctx, ctxKey{}, who(ctx)))
	})
}

func from(ctx context.Context) *caller {
	c, _ := ctx.Value(ctxKey{}).(*caller)
	return c
}

func siteEnabled(c *caller) error {
	if c == nil || !keys.AgentEnabled.ForSite(uint(c.SiteID)) {
		return fail(http.StatusForbidden, CodeAssistantDisabled, "the assistant is not enabled for this site")
	}
	return nil
}

func requireUser(ctx context.Context) (*caller, error) {
	c := from(ctx)
	if err := siteEnabled(c); err != nil {
		return nil, err
	}
	if c.UserID == 0 {
		return nil, fail(http.StatusUnauthorized, CodeLoginRequired, "a user access token is required")
	}
	return c, nil
}

type PersonaBody struct {
	Name      string   `json:"name"`
	Avatar    string   `json:"avatar"`
	Greetings []string `json:"greetings"`
}

type ConversationBody struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Mode      string    `json:"mode" enum:"assistant,search"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

func conversationBody(c model.Conversation) ConversationBody {
	return ConversationBody{ID: strconv.FormatInt(c.ID, 10), Title: c.Title, Mode: c.Mode, UpdatedAt: c.UpdatedAt, CreatedAt: c.CreatedAt}
}

type MessageBody struct {
	ID       string          `json:"id"`
	ParentID *string         `json:"parent_id"`
	Role     string          `json:"role" enum:"user,assistant"`
	Parts    json.RawMessage `json:"parts"`
	Status   string          `json:"status" enum:"complete,streaming,failed,canceled"`
	RunID    string          `json:"run_id"`
	Siblings int             `json:"siblings" doc:"How many messages share this message's parent, itself included. Above 1 the UI offers to switch branches."`
}

type QuotaBody struct {
	FreeLeft       int    `json:"free_left"`
	FreePerDay     int    `json:"free_per_day"`
	PriceAssistant int    `json:"price_assistant"`
	PriceWeb       int    `json:"price_web_surcharge"`
	PriceSearch    int    `json:"price_search"`
	PaidEnabled    bool   `json:"paid_enabled"`
	Balance        *int64 `json:"moemoepoint_balance" doc:"The user's NextMoe moemoepoint balance; null when it could not be read."`
}

type sendInput struct {
	ID             string `path:"id"`
	IdempotencyKey string `header:"Idempotency-Key" required:"true" maxLength:"128"`
	Body           struct {
		Parts     []service.Part `json:"parts" minItems:"1" maxItems:"6"`
		ParentID  *string        `json:"parent_id,omitempty" doc:"Message to answer. Absent continues from the conversation head; \"0\" starts a new root branch; a user message's parent regenerates or edits."`
		WebSearch bool           `json:"web_search,omitempty"`
	}
}

type sendOutput struct {
	Status int
	Body   struct {
		RunID              string `json:"run_id"`
		UserMessageID      string `json:"user_message_id"`
		AssistantMessageID string `json:"assistant_message_id"`
	}
}

func (s *Server) Register(api huma.API) {
	withCaller(api)

	huma.Register(api, huma.Operation{OperationID: "getAgentPersona", Method: http.MethodGet, Path: "/v2/agent/persona",
		Summary: "The site's mascot", Tags: []string{"agent"}},
		func(ctx context.Context, _ *struct{}) (*struct{ Body PersonaBody }, error) {
			c := from(ctx)
			if err := siteEnabled(c); err != nil {
				return nil, err
			}
			p, err := s.Store.Persona(ctx, c.SiteID)
			if err != nil {
				return nil, err
			}
			var greetings []string
			_ = json.Unmarshal(p.Greetings, &greetings)
			return &struct{ Body PersonaBody }{PersonaBody{Name: p.Name, Avatar: p.Avatar, Greetings: greetings}}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "getAgentQuota", Method: http.MethodGet, Path: "/v2/agent/quota",
		Summary: "The caller's free units and prices", Tags: []string{"agent"}},
		func(ctx context.Context, _ *struct{}) (*struct{ Body QuotaBody }, error) {
			c, err := requireUser(ctx)
			if err != nil {
				return nil, err
			}
			out := QuotaBody{
				FreeLeft: s.Quota.FreeLeft(ctx, c.UserID), FreePerDay: int(keys.AgentFreeUnitsPerDay.Get()),
				PriceAssistant: int(keys.AgentPriceAssistant.Get()), PriceWeb: int(keys.AgentPriceWebSurcharge.Get()),
				PriceSearch: int(keys.AgentPriceSearch.Get()), PaidEnabled: s.Quota.PaidAvailable(),
			}
			if b, ok := s.Quota.Balance(ctx, c.UserID); ok {
				out.Balance = &b
			}
			return &struct{ Body QuotaBody }{out}, nil
		})

	type listOut struct {
		Body struct {
			Items      []ConversationBody `json:"items"`
			NextCursor *string            `json:"next_cursor"`
		}
	}
	huma.Register(api, huma.Operation{OperationID: "listAgentConversations", Method: http.MethodGet, Path: "/v2/agent/conversations",
		Summary: "The caller's conversations on this site, newest first", Tags: []string{"agent"}},
		func(ctx context.Context, in *struct {
			Cursor string `query:"cursor" maxLength:"64"`
			Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"30"`
		}) (*listOut, error) {
			c, err := requireUser(ctx)
			if err != nil {
				return nil, err
			}
			before, beforeID := decodeCursor(in.Cursor)
			rows, err := s.Store.ListConversations(ctx, c.UserID, c.SiteID, before, beforeID, in.Limit+1)
			if err != nil {
				return nil, err
			}
			out := &listOut{}
			out.Body.Items = []ConversationBody{}
			for i, r := range rows {
				if i == in.Limit {
					cur := encodeCursor(rows[i-1].UpdatedAt, rows[i-1].ID)
					out.Body.NextCursor = &cur
					break
				}
				out.Body.Items = append(out.Body.Items, conversationBody(r))
			}
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "createAgentConversation", Method: http.MethodPost, Path: "/v2/agent/conversations",
		Summary: "Start a conversation", Tags: []string{"agent"}, DefaultStatus: http.StatusCreated},
		func(ctx context.Context, in *struct {
			Body struct {
				Mode string `json:"mode,omitempty" enum:"assistant"`
			}
		}) (*struct{ Body ConversationBody }, error) {
			c, err := requireUser(ctx)
			if err != nil {
				return nil, err
			}
			conv, err := s.Store.CreateConversation(ctx, c.UserID, c.SiteID, model.ModeAssistant, nil)
			if err != nil {
				return nil, err
			}
			return &struct{ Body ConversationBody }{conversationBody(*conv)}, nil
		})

	type convOut struct {
		Body struct {
			Conversation ConversationBody `json:"conversation"`
			Messages     []MessageBody    `json:"messages" doc:"The current branch, oldest first."`
			ActiveRunID  *string          `json:"active_run_id" doc:"A run still streaming into this conversation; resume it on /v2/agent/runs/{id}/stream."`
		}
	}
	huma.Register(api, huma.Operation{OperationID: "getAgentConversation", Method: http.MethodGet, Path: "/v2/agent/conversations/{id}",
		Summary: "A conversation and its current branch", Tags: []string{"agent"}},
		func(ctx context.Context, in *struct {
			ID   string `path:"id"`
			Head string `query:"head" doc:"Show the branch ending at this message instead of the conversation head."`
		}) (*convOut, error) {
			c, err := requireUser(ctx)
			if err != nil {
				return nil, err
			}
			conv, err := s.conversation(ctx, c, in.ID)
			if err != nil {
				return nil, err
			}
			head := conv.HeadMessageID
			if in.Head != "" {
				id, err := strconv.ParseInt(in.Head, 10, 64)
				if err != nil {
					return nil, huma.Error400BadRequest("head must be a message id")
				}
				leaf, err := s.Store.Leaf(ctx, conv.ID, id)
				if err != nil {
					return nil, huma.Error404NotFound("no such message in this conversation")
				}
				head = &leaf
			}
			branch, err := s.Store.Branch(ctx, head)
			if err != nil {
				return nil, err
			}
			siblings, err := s.Store.SiblingCounts(ctx, conv.ID)
			if err != nil {
				return nil, err
			}
			out := &convOut{}
			out.Body.Conversation = conversationBody(*conv)
			out.Body.Messages = []MessageBody{}
			for _, m := range branch {
				mb := MessageBody{ID: strconv.FormatInt(m.ID, 10), Role: m.Role, Parts: json.RawMessage(m.Parts), Status: m.Status, RunID: m.RunID}
				parent := int64(0)
				if m.ParentID != nil {
					parent = *m.ParentID
					p := strconv.FormatInt(parent, 10)
					mb.ParentID = &p
				}
				mb.Siblings = siblings[parent]
				out.Body.Messages = append(out.Body.Messages, mb)
				if m.Status == model.MessageStreaming && s.Broker.Active(m.RunID) {
					id := m.RunID
					out.Body.ActiveRunID = &id
				}
			}
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "renameAgentConversation", Method: http.MethodPatch, Path: "/v2/agent/conversations/{id}",
		Summary: "Rename a conversation", Tags: []string{"agent"}},
		func(ctx context.Context, in *struct {
			ID   string `path:"id"`
			Body struct {
				Title string `json:"title" minLength:"1" maxLength:"100"`
			}
		}) (*struct{ Body ConversationBody }, error) {
			c, err := requireUser(ctx)
			if err != nil {
				return nil, err
			}
			conv, err := s.conversation(ctx, c, in.ID)
			if err != nil {
				return nil, err
			}
			if err := s.Store.RenameConversation(ctx, conv, strings.TrimSpace(in.Body.Title)); err != nil {
				return nil, err
			}
			conv.Title = strings.TrimSpace(in.Body.Title)
			return &struct{ Body ConversationBody }{conversationBody(*conv)}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "deleteAgentConversation", Method: http.MethodDelete, Path: "/v2/agent/conversations/{id}",
		Summary: "Delete a conversation and its messages", Tags: []string{"agent"}, DefaultStatus: http.StatusNoContent},
		func(ctx context.Context, in *struct {
			ID string `path:"id"`
		}) (*struct{}, error) {
			c, err := requireUser(ctx)
			if err != nil {
				return nil, err
			}
			conv, err := s.conversation(ctx, c, in.ID)
			if err != nil {
				return nil, err
			}
			return nil, s.Store.DeleteConversation(ctx, conv)
		})

	huma.Register(api, huma.Operation{OperationID: "sendAgentMessage", Method: http.MethodPost, Path: "/v2/agent/conversations/{id}/messages",
		Summary: "Send a message and start the reply", Tags: []string{"agent"}, DefaultStatus: http.StatusAccepted,
		Description: "Stores the user message, reserves its price and starts a run. Stream the reply from /v2/agent/runs/{run_id}/stream. Replaying the same Idempotency-Key with the same body returns the same run."},
		s.send)

	huma.Register(api, huma.Operation{OperationID: "cancelAgentRun", Method: http.MethodPost, Path: "/v2/agent/runs/{id}/cancel",
		Summary: "Stop a streaming reply", Tags: []string{"agent"}, DefaultStatus: http.StatusNoContent},
		func(ctx context.Context, in *struct {
			ID string `path:"id"`
		}) (*struct{}, error) {
			c, err := requireUser(ctx)
			if err != nil {
				return nil, err
			}
			if _, err := s.Store.Run(ctx, c.UserID, in.ID); err != nil {
				return nil, huma.Error404NotFound("no such run")
			}
			s.Runner.Cancel(in.ID)
			return nil, nil
		})

	s.registerSearch(api)
}

func (s *Server) conversation(ctx context.Context, c *caller, raw string) (*model.Conversation, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil, huma.Error404NotFound("no such conversation")
	}
	conv, err := s.Store.Conversation(ctx, c.UserID, c.SiteID, id)
	if errors.Is(err, service.ErrNotFound) {
		return nil, huma.Error404NotFound("no such conversation")
	}
	return conv, err
}

func (s *Server) send(ctx context.Context, in *sendInput) (*sendOutput, error) {
	c, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	conv, err := s.conversation(ctx, c, in.ID)
	if err != nil {
		return nil, err
	}
	parts, perr := validateParts(in.Body.Parts)
	if perr != nil {
		return nil, perr
	}
	if !s.Limits.Allow(ctx, "user:"+strconv.FormatInt(c.UserID, 10), keys.AgentUserRatePerMinute.Get(), time.Minute) {
		return nil, fail(http.StatusTooManyRequests, CodeRateLimited, "too many messages; wait a minute")
	}
	var parent *int64
	if in.Body.ParentID != nil {
		v, err := strconv.ParseInt(*in.Body.ParentID, 10, 64)
		if err != nil {
			return nil, huma.Error400BadRequest("parent_id must be a message id")
		}
		parent = &v
	}
	price := int(keys.AgentPriceAssistant.Get())
	if in.Body.WebSearch {
		price += int(keys.AgentPriceWebSurcharge.Get())
	}
	req := service.StartRequest{Conversation: conv, ParentID: parent, Parts: parts, Mode: model.ModeAssistant,
		WebSearch: in.Body.WebSearch, IdemKey: strings.Clone(in.IdempotencyKey), Price: price}
	if existing, err := s.Store.RunByIdem(ctx, c.UserID, req.IdemKey); err == nil {
		if existing.IdemHash != service.RequestHash(req) {
			return nil, fail(http.StatusUnprocessableEntity, CodeIdempotencyKeyReused, "Idempotency-Key was used with a different request")
		}
		return sent(existing), nil
	}
	res, err := s.Quota.Reserve(ctx, c.UserID, price)
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
	case err != nil:
		return nil, err
	}
	if created {
		if err := s.startTurn(ctx, c, run, res); err != nil {
			return nil, err
		}
	}
	return sent(run), nil
}

func sent(run *model.Run) *sendOutput {
	out := &sendOutput{Status: http.StatusAccepted}
	out.Body.RunID = run.ID
	out.Body.UserMessageID = strconv.FormatInt(run.UserMessageID, 10)
	out.Body.AssistantMessageID = strconv.FormatInt(run.AssistantMessageID, 10)
	return out
}

func (s *Server) startTurn(ctx context.Context, c *caller, run *model.Run, res service.Reservation) error {
	ctx = context.WithoutCancel(ctx)
	conv, err := s.Store.Conversation(ctx, c.UserID, c.SiteID, run.ConversationID)
	if err != nil {
		return err
	}
	persona, err := s.Store.Persona(ctx, c.SiteID)
	if err != nil {
		return err
	}
	branch, err := s.Store.Branch(ctx, &run.UserMessageID)
	if err != nil {
		return err
	}
	s.Runner.Start(service.Turn{Run: run, Conv: conv, Persona: persona, History: branch, Reservation: res, SiteName: strings.Clone(c.SiteName)})
	return nil
}

func validateParts(in []service.Part) ([]service.Part, error) {
	total := 0
	out := make([]service.Part, 0, len(in))
	for _, p := range in {
		switch p.Type {
		case "text":
		case "file":
			if !strings.HasPrefix(p.MediaType, "text/") && p.MediaType != "application/json" && p.MediaType != "application/x-yaml" {
				return nil, fail(http.StatusUnsupportedMediaType, CodeUnsupportedAttachment, "only text files can be attached for now")
			}
		default:
			return nil, huma.Error400BadRequest("unsupported part type " + p.Type)
		}
		total += len([]rune(p.Text))
		out = append(out, service.Part{Type: p.Type, Text: strings.Clone(p.Text), MediaType: strings.Clone(p.MediaType), Filename: strings.Clone(p.Filename)})
	}
	if total == 0 {
		return nil, fail(http.StatusBadRequest, CodeEmptyMessage, "the message is empty")
	}
	if total > int(keys.AgentTurnMaxInputChars.Get()) {
		return nil, fail(http.StatusRequestEntityTooLarge, CodeInputTooLong, "the message and its attachments are too long")
	}
	return out, nil
}

func encodeCursor(t time.Time, id int64) string {
	return strconv.FormatInt(t.UnixMicro(), 36) + "." + strconv.FormatInt(id, 36)
}

func decodeCursor(s string) (time.Time, int64) {
	a, b, ok := strings.Cut(s, ".")
	if !ok {
		return time.Time{}, 0
	}
	us, err1 := strconv.ParseInt(a, 36, 64)
	id, err2 := strconv.ParseInt(b, 36, 64)
	if err1 != nil || err2 != nil {
		return time.Time{}, 0
	}
	return time.UnixMicro(us), id
}
