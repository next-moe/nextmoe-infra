package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"api/internal/platform/agent/model"
	"api/internal/platform/agent/service"
	"api/internal/platform/agent/tools"

	"github.com/danielgtaylor/huma/v2"
	"gorm.io/datatypes"
)

type SiteToolView struct {
	ID         int64     `json:"id"`
	SiteID     int64     `json:"site_id"`
	Name       string    `json:"name"`
	URL        string    `json:"url"`
	Auth       string    `json:"auth" enum:"user_token,none"`
	CardPrefix string    `json:"card_prefix"`
	Allowed    []string  `json:"allowed" doc:"Tool names the assistant may call; empty allows every tool the server lists."`
	TimeoutMs  int       `json:"timeout_ms"`
	Enabled    bool      `json:"enabled"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func siteToolView(t model.SiteTool) SiteToolView {
	v := SiteToolView{ID: t.ID, SiteID: t.SiteID, Name: t.Name, URL: t.URL, Auth: t.Auth, CardPrefix: t.CardPrefix,
		TimeoutMs: t.TimeoutMs, Enabled: t.Enabled, UpdatedAt: t.UpdatedAt, Allowed: []string{}}
	_ = json.Unmarshal(t.Allowed, &v.Allowed)
	if v.Allowed == nil {
		v.Allowed = []string{}
	}
	return v
}

type SiteToolInput struct {
	SiteID     *int64    `json:"site_id,omitempty" minimum:"1"`
	Name       *string   `json:"name,omitempty" pattern:"^[a-z][a-z0-9_]{0,31}$" doc:"Prefixes the tool names the model sees, as <name>__<tool>."`
	URL        *string   `json:"url,omitempty" maxLength:"512" doc:"The site's MCP endpoint (streamable HTTP), reachable from the agent."`
	Auth       *string   `json:"auth,omitempty" enum:"user_token,none" doc:"user_token forwards the asking user's access token, so the site applies its own visibility rules; such tools are skipped for signed-out searches."`
	CardPrefix *string   `json:"card_prefix,omitempty" pattern:"^[a-z][a-z0-9_]{0,31}$" doc:"Cards from this server must use kinds <card_prefix>:<kind>; others are dropped."`
	Allowed    *[]string `json:"allowed,omitempty" maxItems:"50"`
	TimeoutMs  *int      `json:"timeout_ms,omitempty" minimum:"1000" maximum:"120000"`
	Enabled    *bool     `json:"enabled,omitempty"`
}

type SiteToolProbe struct {
	OK           bool              `json:"ok"`
	Error        string            `json:"error"`
	Instructions string            `json:"instructions" doc:"What the server tells the assistant about its tools; added to the system prompt."`
	Tools        []SiteToolListing `json:"tools"`
}

type SiteToolListing struct {
	Name        string `json:"name" doc:"As the server names it."`
	ModelName   string `json:"model_name" doc:"As the model sees it."`
	Description string `json:"description"`
	Allowed     bool   `json:"allowed"`
}

var errBadSiteTool = errors.New("bad site tool")

func applySiteTool(row *model.SiteTool, in SiteToolInput) error {
	if in.SiteID != nil {
		row.SiteID = *in.SiteID
	}
	if in.Name != nil {
		row.Name = *in.Name
	}
	if in.URL != nil {
		u, err := url.Parse(strings.TrimSpace(*in.URL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errBadSiteTool
		}
		row.URL = u.String()
	}
	if in.Auth != nil {
		row.Auth = *in.Auth
	}
	if in.CardPrefix != nil {
		row.CardPrefix = *in.CardPrefix
	}
	if in.Allowed != nil {
		allowed := *in.Allowed
		if allowed == nil {
			allowed = []string{}
		}
		raw, _ := json.Marshal(allowed)
		row.Allowed = datatypes.JSON(raw)
	}
	if in.TimeoutMs != nil {
		row.TimeoutMs = *in.TimeoutMs
	}
	if in.Enabled != nil {
		row.Enabled = *in.Enabled
	}
	if row.SiteID == 0 || row.Name == "" || row.URL == "" || row.CardPrefix == "" {
		return errBadSiteTool
	}
	if row.Auth == "" {
		row.Auth = model.SiteToolAuthUser
	}
	if row.TimeoutMs == 0 {
		row.TimeoutMs = 20000
	}
	if len(row.Allowed) == 0 {
		row.Allowed = datatypes.JSON("[]")
	}
	return nil
}

var siteToolUnique = regexp.MustCompile(`uq_agent_site_tool`)

func (s *Server) registerAdmin(api huma.API) {
	tags := []string{"agent-admin"}
	const base = "/api/v1/admin/agent"
	const note = "Admin console only; requires ai.agent_manage."
	op := func(id, method, path, summary string) huma.Operation {
		return huma.Operation{OperationID: id, Method: method, Path: base + path, Summary: summary, Description: note, Tags: tags}
	}
	s.registerPersonaAdmin(api, op)
	type viewOut struct{ Body SiteToolView }
	save := func(ctx context.Context, row *model.SiteTool, in SiteToolInput) (*viewOut, error) {
		if err := applySiteTool(row, in); err != nil {
			return nil, huma.Error422UnprocessableEntity("site_id, name, an http(s) url and card_prefix are required")
		}
		if err := s.SiteTools.Save(ctx, row); err != nil {
			if siteToolUnique.MatchString(err.Error()) {
				return nil, huma.Error409Conflict("this site already has a tool server with that name")
			}
			return nil, err
		}
		return &viewOut{siteToolView(*row)}, nil
	}

	huma.Register(api, op("listAgentSiteTools", http.MethodGet, "/site-tools", "Site tool servers, optionally for one site"),
		func(ctx context.Context, in *struct {
			SiteID int64 `query:"site_id" minimum:"0"`
		}) (*struct{ Body []SiteToolView }, error) {
			rows, err := s.SiteTools.List(ctx, in.SiteID)
			if err != nil {
				return nil, err
			}
			out := make([]SiteToolView, len(rows))
			for i, r := range rows {
				out[i] = siteToolView(r)
			}
			return &struct{ Body []SiteToolView }{out}, nil
		})
	huma.Register(api, op("createAgentSiteTool", http.MethodPost, "/site-tools", "Register a site's MCP server"),
		func(ctx context.Context, in *struct{ Body SiteToolInput }) (*viewOut, error) {
			return save(ctx, &model.SiteTool{Enabled: true}, in.Body)
		})
	huma.Register(api, op("updateAgentSiteTool", http.MethodPatch, "/site-tools/{id}", "Change a site tool server"),
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body SiteToolInput
		}) (*viewOut, error) {
			row, err := s.SiteTools.Get(ctx, in.ID)
			if errors.Is(err, service.ErrNotFound) {
				return nil, huma.Error404NotFound("no such site tool")
			}
			if err != nil {
				return nil, err
			}
			return save(ctx, row, in.Body)
		})
	huma.Register(api, op("deleteAgentSiteTool", http.MethodDelete, "/site-tools/{id}", "Remove a site tool server"),
		func(ctx context.Context, in *struct {
			ID int64 `path:"id"`
		}) (*struct{}, error) {
			err := s.SiteTools.Delete(ctx, in.ID)
			if errors.Is(err, service.ErrNotFound) {
				return nil, huma.Error404NotFound("no such site tool")
			}
			return nil, err
		})
	huma.Register(api, op("testAgentSiteTool", http.MethodPost, "/site-tools/{id}/test", "Connect to the server and list its tools, with your own token"),
		func(ctx context.Context, in *struct {
			ID            int64  `path:"id"`
			Authorization string `header:"Authorization"`
		}) (*struct{ Body SiteToolProbe }, error) {
			row, err := s.SiteTools.Get(ctx, in.ID)
			if errors.Is(err, service.ErrNotFound) {
				return nil, huma.Error404NotFound("no such site tool")
			}
			if err != nil {
				return nil, err
			}
			out := SiteToolProbe{Tools: []SiteToolListing{}}
			listed, err := s.SiteTools.Probe(ctx, *row, strings.TrimPrefix(in.Authorization, "Bearer "))
			if err != nil {
				out.Error = err.Error()
				return &struct{ Body SiteToolProbe }{out}, nil
			}
			var allowed []string
			_ = json.Unmarshal(row.Allowed, &allowed)
			out.OK, out.Instructions = true, listed.Instructions
			for _, t := range listed.Tools {
				ok := len(allowed) == 0
				for _, a := range allowed {
					ok = ok || a == t.Name
				}
				out.Tools = append(out.Tools, SiteToolListing{Name: t.Name, ModelName: tools.SiteToolName(row.Name, t.Name), Description: t.Description, Allowed: ok})
			}
			return &struct{ Body SiteToolProbe }{out}, nil
		})
}
