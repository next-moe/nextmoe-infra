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

	WebSearch func() bool
	Vision    func() bool
	SiteTools *service.SiteTools
}

func (s *Server) webSearch() bool { return s.WebSearch != nil && s.WebSearch() }

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
	Name               string            `json:"name"`
	Avatar             string            `json:"avatar"`
	Greetings          []string          `json:"greetings"`
	Suggestions        []SuggestionGroup `json:"suggestions" doc:"Starter prompts for an empty conversation, grouped like the mode chips of a composer."`
	Uploads            UploadsBody       `json:"uploads" doc:"What the upload menu may offer on this site, from the settings center."`
	WebSearchAvailable bool              `json:"web_search_available" doc:"Whether web_search can do anything; hide the toggle when false."`
	WebSearchAuto      bool              `json:"web_search_auto" doc:"The assistant searches the web by itself when a question needs it, and the surcharge is charged only when it did. The toggle then asks it to search first; without auto, only toggled questions can search."`
}

type ConversationBody struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Mode        string    `json:"mode" enum:"assistant,search"`
	Starred     bool      `json:"starred"`
	ActiveRunID *string   `json:"active_run_id" doc:"A reply still streaming in this conversation."`
	UpdatedAt   time.Time `json:"updated_at"`
	CreatedAt   time.Time `json:"created_at"`
}

func conversationBody(c model.Conversation, activeRun string) ConversationBody {
	b := ConversationBody{ID: strconv.FormatInt(c.ID, 10), Title: c.Title, Mode: c.Mode, Starred: c.Starred, UpdatedAt: c.UpdatedAt, CreatedAt: c.CreatedAt}
	if activeRun != "" {
		b.ActiveRunID = &activeRun
	}
	return b
}

type MessageBody struct {
	ID         string          `json:"id"`
	ParentID   *string         `json:"parent_id"`
	Role       string          `json:"role" enum:"user,assistant"`
	Parts      json.RawMessage `json:"parts"`
	Status     string          `json:"status" enum:"complete,streaming,failed,canceled"`
	RunID      string          `json:"run_id"`
	Siblings   int             `json:"siblings" doc:"len(sibling_ids)."`
	SiblingIDs []string        `json:"sibling_ids" doc:"Every message under this message's parent, itself included, oldest first. Its position is the n of an n/m switcher; fetch ?head=<sibling> to show another one."`
	Feedback   *string         `json:"feedback" enum:"up,down" doc:"The user's rating of an assistant reply."`
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
			return &struct{ Body PersonaBody }{PersonaBody{Name: p.Name, Avatar: p.Avatar, Greetings: greetings,
				Suggestions: s.suggestions(p), WebSearchAvailable: s.webSearch(), Uploads: s.uploadsBody(c.SiteID),
				WebSearchAuto: s.webSearch() && keys.AgentWebAuto.ForSite(uint(c.SiteID))}}, nil
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
			Cursor  string `query:"cursor" maxLength:"64"`
			Limit   int    `query:"limit" minimum:"1" maximum:"100" default:"30"`
			Starred string `query:"starred" enum:"true,false" doc:"Only starred (true) or only unstarred (false) conversations; absent lists both."`
			Q       string `query:"q" maxLength:"100" doc:"Search: conversations whose title or any message text contains this, case-insensitively. Same order and cursor as the plain list."`
		}) (*listOut, error) {
			c, err := requireUser(ctx)
			if err != nil {
				return nil, err
			}
			var starred *bool
			if in.Starred != "" {
				v := in.Starred == "true"
				starred = &v
			}
			before, beforeID := decodeCursor(in.Cursor)
			rows, err := s.Store.ListConversations(ctx, c.UserID, c.SiteID, starred, in.Q, before, beforeID, in.Limit+1)
			if err != nil {
				return nil, err
			}
			ids := make([]int64, len(rows))
			for i, r := range rows {
				ids[i] = r.ID
			}
			active, err := s.Store.ActiveRuns(ctx, ids)
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
				out.Body.Items = append(out.Body.Items, conversationBody(r, active[r.ID]))
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
			return &struct{ Body ConversationBody }{conversationBody(*conv, "")}, nil
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
			siblings, err := s.Store.SiblingIDs(ctx, conv.ID)
			if err != nil {
				return nil, err
			}
			out := &convOut{}
			out.Body.Messages = []MessageBody{}
			for _, m := range branch {
				mb := MessageBody{ID: strconv.FormatInt(m.ID, 10), Role: m.Role, Parts: json.RawMessage(m.Parts), Status: m.Status, RunID: m.RunID}
				parent := int64(0)
				if m.ParentID != nil {
					parent = *m.ParentID
					p := strconv.FormatInt(parent, 10)
					mb.ParentID = &p
				}
				for _, id := range siblings[parent] {
					mb.SiblingIDs = append(mb.SiblingIDs, strconv.FormatInt(id, 10))
				}
				mb.Siblings = len(mb.SiblingIDs)
				if m.Feedback != "" {
					f := m.Feedback
					mb.Feedback = &f
				}
				out.Body.Messages = append(out.Body.Messages, mb)
				if m.Status == model.MessageStreaming && s.Broker.Active(m.RunID) {
					id := m.RunID
					out.Body.ActiveRunID = &id
				}
			}
			active := ""
			if out.Body.ActiveRunID != nil {
				active = *out.Body.ActiveRunID
			}
			out.Body.Conversation = conversationBody(*conv, active)
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "renameAgentConversation", Method: http.MethodPatch, Path: "/v2/agent/conversations/{id}",
		Summary: "Rename, star or switch the branch of a conversation", Tags: []string{"agent"},
		Description: "Only the fields present change. head_message_id makes the branch through that message current, down to the reply last chosen (or else last created) under each message, so a reload and a return to an earlier question both show what the user last picked."},
		func(ctx context.Context, in *struct {
			ID   string `path:"id"`
			Body struct {
				Title         *string `json:"title,omitempty" minLength:"1" maxLength:"100"`
				Starred       *bool   `json:"starred,omitempty"`
				HeadMessageID *string `json:"head_message_id,omitempty"`
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
			if t := in.Body.Title; t != nil {
				title := strings.TrimSpace(*t)
				if title == "" {
					return nil, huma.Error422UnprocessableEntity("title must not be blank")
				}
				if err := s.Store.RenameConversation(ctx, conv, title); err != nil {
					return nil, err
				}
				conv.Title = title
			}
			if in.Body.Starred != nil {
				if err := s.Store.StarConversation(ctx, conv, *in.Body.Starred); err != nil {
					return nil, err
				}
				conv.Starred = *in.Body.Starred
			}
			if h := in.Body.HeadMessageID; h != nil {
				id, err := strconv.ParseInt(*h, 10, 64)
				if err != nil {
					return nil, huma.Error404NotFound("no such message in this conversation")
				}
				leaf, err := s.Store.SetHead(ctx, conv, id)
				if errors.Is(err, service.ErrNotFound) {
					return nil, huma.Error404NotFound("no such message in this conversation")
				}
				if err != nil {
					return nil, err
				}
				conv.HeadMessageID = &leaf
			}
			return &struct{ Body ConversationBody }{conversationBody(*conv, "")}, nil
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

	s.registerBranching(api)
	s.registerUploads(api)
	s.registerAdmin(api)
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
	parts, perr := s.validateParts(ctx, c, conv, in.Body.Parts)
	if perr != nil {
		return nil, perr
	}
	var parent *int64
	if in.Body.ParentID != nil {
		v, err := strconv.ParseInt(*in.Body.ParentID, 10, 64)
		if err != nil {
			return nil, huma.Error400BadRequest("parent_id must be a message id")
		}
		parent = &v
	}
	return s.begin(ctx, c, service.StartRequest{Conversation: conv, ParentID: parent, Parts: parts, Mode: model.ModeAssistant,
		WebSearch: in.Body.WebSearch, IdemKey: strings.Clone(in.IdempotencyKey)})
}

func sent(run *model.Run) *sendOutput {
	out := &sendOutput{Status: http.StatusAccepted}
	out.Body.RunID = run.ID
	out.Body.UserMessageID = strconv.FormatInt(run.UserMessageID, 10)
	out.Body.AssistantMessageID = strconv.FormatInt(run.AssistantMessageID, 10)
	return out
}

func (s *Server) startTurn(ctx context.Context, c *caller, run *model.Run, res service.Reservation, web bool) error {
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
	var ids []string
	for _, m := range branch {
		var parts []service.Part
		_ = json.Unmarshal(m.Parts, &parts)
		ids = append(ids, service.UploadIDs(parts)...)
	}
	uploads, err := s.Store.UploadsByID(ctx, ids)
	if err != nil {
		return err
	}
	s.Runner.Start(service.Turn{Run: run, Conv: conv, Persona: persona, History: branch, Reservation: res, SiteName: strings.Clone(c.SiteName), Uploads: uploads, UserToken: c.Token, Web: web})
	return nil
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
