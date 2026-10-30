package handler

import (
	"context"
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

const CodeMemoryFull = "MEMORY_FULL"

type PreferencesBody struct {
	Memory           bool `json:"memory" doc:"The user lets the assistant remember things about them on this site and use what it remembered. Off keeps the memories but neither reads nor writes them."`
	History          bool `json:"history" doc:"The user lets the assistant see their other conversations on this site: their titles with every question, and searching and reading them when asked about earlier chats."`
	MemoryAvailable  bool `json:"memory_available" doc:"The site offers memory at all; hide the memory page and switch when false."`
	HistoryAvailable bool `json:"history_available" doc:"The site lets the assistant search past conversations."`
}

type MemoryBody struct {
	ID             string    `json:"id"`
	Content        string    `json:"content"`
	Origin         string    `json:"origin" enum:"explicit,auto" doc:"explicit: the user asked for it or wrote it; auto: extracted from a conversation after it went quiet."`
	ConversationID *string   `json:"conversation_id" doc:"The conversation it came from, while that conversation exists."`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func memoryBody(m model.Memory) MemoryBody {
	b := MemoryBody{ID: strconv.FormatInt(m.ID, 10), Content: m.Content, Origin: m.Origin, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
	if m.ConversationID != nil {
		id := strconv.FormatInt(*m.ConversationID, 10)
		b.ConversationID = &id
	}
	return b
}

func preferencesBody(p service.Prefs, siteID int64) PreferencesBody {
	site := uint(siteID)
	return PreferencesBody{Memory: p.Memory, History: p.History,
		MemoryAvailable: keys.AgentMemoryEnabled.ForSite(site), HistoryAvailable: keys.AgentHistorySearch.ForSite(site)}
}

func memoryID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, huma.Error404NotFound("no such memory")
	}
	return id, nil
}

func memoryErr(err error) error {
	switch {
	case errors.Is(err, service.ErrNotFound):
		return huma.Error404NotFound("no such memory")
	case errors.Is(err, service.ErrMemoryFull):
		return fail(http.StatusConflict, CodeMemoryFull, "memory is full; delete some memories first")
	}
	return err
}

func (s *Server) registerMemory(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "getAgentPreferences", Method: http.MethodGet, Path: "/v2/agent/preferences",
		Summary: "The caller's memory and chat history switches on this site", Tags: []string{"agent"}},
		func(ctx context.Context, _ *struct{}) (*struct{ Body PreferencesBody }, error) {
			c, err := requireUser(ctx)
			if err != nil {
				return nil, err
			}
			p, err := s.Store.Preferences(ctx, c.UserID, c.SiteID)
			if err != nil {
				return nil, err
			}
			return &struct{ Body PreferencesBody }{preferencesBody(p, c.SiteID)}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "updateAgentPreferences", Method: http.MethodPatch, Path: "/v2/agent/preferences",
		Summary: "Turn memory or chat history on or off", Tags: []string{"agent"}, Description: "Only the fields present change. Both are on until the user turns them off."},
		func(ctx context.Context, in *struct {
			Body struct {
				Memory  *bool `json:"memory,omitempty"`
				History *bool `json:"history,omitempty"`
			}
		}) (*struct{ Body PreferencesBody }, error) {
			c, err := requireUser(ctx)
			if err != nil {
				return nil, err
			}
			p, err := s.Store.SetPreferences(ctx, c.UserID, c.SiteID, in.Body.Memory, in.Body.History)
			if err != nil {
				return nil, err
			}
			return &struct{ Body PreferencesBody }{preferencesBody(p, c.SiteID)}, nil
		})

	type listOut struct {
		Body struct {
			Items []MemoryBody `json:"items" doc:"Newest change first."`
			Max   int          `json:"max" doc:"How many memories a user can keep on this site; past it the oldest auto memory makes room."`
		}
	}
	huma.Register(api, huma.Operation{OperationID: "listAgentMemories", Method: http.MethodGet, Path: "/v2/agent/memories",
		Summary: "What the assistant remembers about the caller on this site", Tags: []string{"agent"}},
		func(ctx context.Context, _ *struct{}) (*listOut, error) {
			c, err := requireUser(ctx)
			if err != nil {
				return nil, err
			}
			rows, err := s.Store.Memories(ctx, c.UserID, c.SiteID)
			if err != nil {
				return nil, err
			}
			out := &listOut{}
			out.Body.Items = make([]MemoryBody, len(rows))
			for i, m := range rows {
				out.Body.Items[i] = memoryBody(m)
			}
			out.Body.Max = int(keys.AgentMemoryMaxItems.Get())
			return out, nil
		})

	type contentIn struct {
		Content string `json:"content" minLength:"1" maxLength:"200"`
	}
	huma.Register(api, huma.Operation{OperationID: "createAgentMemory", Method: http.MethodPost, Path: "/v2/agent/memories",
		Summary: "Add a memory by hand", Tags: []string{"agent"}, DefaultStatus: http.StatusCreated,
		Description: "Stored as explicit. Adding a memory that already exists returns that one."},
		func(ctx context.Context, in *struct{ Body contentIn }) (*struct{ Body MemoryBody }, error) {
			c, err := requireUser(ctx)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.Body.Content) == "" {
				return nil, huma.Error422UnprocessableEntity("content must not be blank")
			}
			m, err := s.Store.AddMemory(ctx, c.UserID, c.SiteID, in.Body.Content, model.MemoryExplicit, nil, int(keys.AgentMemoryMaxItems.Get()))
			if err != nil {
				return nil, memoryErr(err)
			}
			return &struct{ Body MemoryBody }{memoryBody(*m)}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "updateAgentMemory", Method: http.MethodPatch, Path: "/v2/agent/memories/{id}",
		Summary: "Correct a memory", Tags: []string{"agent"}, Description: "An edited memory becomes explicit, so automatic extraction no longer changes or evicts it."},
		func(ctx context.Context, in *struct {
			ID   string `path:"id"`
			Body contentIn
		}) (*struct{ Body MemoryBody }, error) {
			c, err := requireUser(ctx)
			if err != nil {
				return nil, err
			}
			id, err := memoryID(in.ID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.Body.Content) == "" {
				return nil, huma.Error422UnprocessableEntity("content must not be blank")
			}
			m, err := s.Store.UpdateMemory(ctx, c.UserID, c.SiteID, id, in.Body.Content, model.MemoryExplicit)
			if err != nil {
				return nil, memoryErr(err)
			}
			return &struct{ Body MemoryBody }{memoryBody(*m)}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "deleteAgentMemory", Method: http.MethodDelete, Path: "/v2/agent/memories/{id}",
		Summary: "Forget one memory", Tags: []string{"agent"}, DefaultStatus: http.StatusNoContent},
		func(ctx context.Context, in *struct {
			ID string `path:"id"`
		}) (*struct{}, error) {
			c, err := requireUser(ctx)
			if err != nil {
				return nil, err
			}
			id, err := memoryID(in.ID)
			if err != nil {
				return nil, err
			}
			_, err = s.Store.DeleteMemory(ctx, c.UserID, c.SiteID, id)
			return nil, memoryErr(err)
		})

	huma.Register(api, huma.Operation{OperationID: "clearAgentMemories", Method: http.MethodDelete, Path: "/v2/agent/memories",
		Summary: "Forget everything remembered about the caller on this site", Tags: []string{"agent"}, DefaultStatus: http.StatusNoContent},
		func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			c, err := requireUser(ctx)
			if err != nil {
				return nil, err
			}
			_, err = s.Store.ClearMemories(ctx, c.UserID, c.SiteID)
			return nil, err
		})
}
