package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"api/internal/platform/agent/model"
	"api/internal/platform/agent/service"

	"github.com/danielgtaylor/huma/v2"
)

type PersonaView struct {
	ID          int64             `json:"id"`
	SiteID      int64             `json:"site_id" doc:"0 is the default for every site without an active persona of its own."`
	Name        string            `json:"name"`
	Avatar      string            `json:"avatar"`
	Prompt      string            `json:"prompt"`
	Greetings   []string          `json:"greetings" nullable:"false"`
	Suggestions []SuggestionGroup `json:"suggestions" nullable:"false" doc:"Empty uses the built-in groups."`
	Active      bool              `json:"active"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type PersonaList struct {
	Personas           []PersonaView     `json:"personas" nullable:"false"`
	BuiltinSuggestions []SuggestionGroup `json:"builtin_suggestions" nullable:"false"`
}

type PersonaInput struct {
	SiteID      *int64             `json:"site_id,omitempty" minimum:"0"`
	Name        *string            `json:"name,omitempty" minLength:"1" maxLength:"64"`
	Avatar      *string            `json:"avatar,omitempty" maxLength:"128" doc:"An https image URL, or empty for the site's own default."`
	Prompt      *string            `json:"prompt,omitempty" minLength:"1" maxLength:"8000" doc:"Who the assistant is and how it talks. The tool and safety rules are added after it and cannot be overridden here."`
	Greetings   *[]string          `json:"greetings,omitempty" maxItems:"10"`
	Suggestions *[]SuggestionGroup `json:"suggestions,omitempty" maxItems:"6"`
	Active      *bool              `json:"active,omitempty" doc:"Activating a persona deactivates the site's other one."`
}

func personaView(p model.Persona) PersonaView {
	v := PersonaView{ID: p.ID, SiteID: p.SiteID, Name: p.Name, Avatar: p.Avatar, Prompt: p.Prompt, Active: p.Active, UpdatedAt: p.UpdatedAt,
		Greetings: []string{}, Suggestions: []SuggestionGroup{}}
	_ = json.Unmarshal(p.Greetings, &v.Greetings)
	_ = json.Unmarshal(p.Suggestions, &v.Suggestions)
	if v.Greetings == nil {
		v.Greetings = []string{}
	}
	if v.Suggestions == nil {
		v.Suggestions = []SuggestionGroup{}
	}
	return v
}

func applyPersona(p *model.Persona, in PersonaInput) error {
	if in.SiteID != nil {
		p.SiteID = *in.SiteID
	}
	if in.Name != nil {
		p.Name = strings.TrimSpace(*in.Name)
	}
	if in.Avatar != nil {
		a := strings.TrimSpace(*in.Avatar)
		if a != "" && !strings.HasPrefix(a, "https://") {
			return errors.New("avatar must be an https URL")
		}
		p.Avatar = a
	}
	if in.Prompt != nil {
		p.Prompt = strings.TrimSpace(*in.Prompt)
	}
	if in.Greetings != nil {
		var gs []string
		for _, g := range *in.Greetings {
			if g = strings.TrimSpace(g); g != "" {
				if len([]rune(g)) > 200 {
					return errors.New("a greeting is at most 200 characters")
				}
				gs = append(gs, g)
			}
		}
		if gs == nil {
			gs = []string{}
		}
		p.Greetings, _ = json.Marshal(gs)
	}
	if in.Suggestions != nil {
		groups := []SuggestionGroup{}
		for _, g := range *in.Suggestions {
			g.Label = strings.TrimSpace(g.Label)
			var prompts []string
			for _, q := range g.Prompts {
				if q = strings.TrimSpace(q); q != "" {
					if len([]rune(q)) > 100 {
						return errors.New("a suggested prompt is at most 100 characters")
					}
					prompts = append(prompts, q)
				}
			}
			if g.Label == "" || len(prompts) == 0 || len(prompts) > 12 {
				return errors.New("each suggestion group needs a label and 1 to 12 prompts")
			}
			g.Prompts = prompts
			groups = append(groups, g)
		}
		p.Suggestions, _ = json.Marshal(groups)
	}
	if in.Active != nil {
		p.Active = *in.Active
	}
	if p.Name == "" || p.Prompt == "" {
		return errors.New("name and prompt are required")
	}
	if len(p.Greetings) == 0 {
		p.Greetings = []byte("[]")
	}
	if len(p.Suggestions) == 0 {
		p.Suggestions = []byte("[]")
	}
	p.Kind = model.PersonaMascot
	return nil
}

func (s *Server) registerPersonaAdmin(api huma.API, op func(id, method, path, summary string) huma.Operation) {
	type viewOut struct{ Body PersonaView }
	save := func(ctx context.Context, p *model.Persona, in PersonaInput) (*viewOut, error) {
		wasDefault := p.ID != 0 && p.SiteID == 0 && p.Active
		if err := applyPersona(p, in); err != nil {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		if wasDefault && (p.SiteID != 0 || !p.Active) {
			zero := int64(0)
			rows, err := s.Store.Personas(ctx, &zero)
			if err != nil {
				return nil, err
			}
			other := false
			for _, r := range rows {
				other = other || (r.ID != p.ID && r.Active)
			}
			if !other {
				return nil, huma.Error409Conflict("the active default persona answers every site without its own; activate another default first")
			}
		}
		if err := s.Store.SavePersona(ctx, p); err != nil {
			return nil, err
		}
		return &viewOut{personaView(*p)}, nil
	}
	notFound := func(err error) error {
		if errors.Is(err, service.ErrNotFound) {
			return huma.Error404NotFound("no such persona")
		}
		return err
	}

	huma.Register(api, op("listAgentPersonas", http.MethodGet, "/personas", "Personas, optionally for one site"),
		func(ctx context.Context, in *struct {
			SiteID int64 `query:"site_id" minimum:"-1" default:"-1" doc:"-1 lists every site; 0 the defaults."`
		}) (*struct{ Body PersonaList }, error) {
			var site *int64
			if in.SiteID >= 0 {
				site = &in.SiteID
			}
			rows, err := s.Store.Personas(ctx, site)
			if err != nil {
				return nil, err
			}
			out := PersonaList{Personas: make([]PersonaView, len(rows)), BuiltinSuggestions: defaultSuggestions}
			for i, r := range rows {
				out.Personas[i] = personaView(r)
			}
			return &struct{ Body PersonaList }{out}, nil
		})
	huma.Register(api, op("createAgentPersona", http.MethodPost, "/personas", "Add a persona"),
		func(ctx context.Context, in *struct{ Body PersonaInput }) (*viewOut, error) {
			return save(ctx, &model.Persona{Active: true}, in.Body)
		})
	huma.Register(api, op("updateAgentPersona", http.MethodPatch, "/personas/{id}", "Change a persona"),
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body PersonaInput
		}) (*viewOut, error) {
			p, err := s.Store.PersonaByID(ctx, in.ID)
			if err != nil {
				return nil, notFound(err)
			}
			return save(ctx, p, in.Body)
		})
	huma.Register(api, op("deleteAgentPersona", http.MethodDelete, "/personas/{id}", "Remove a persona"),
		func(ctx context.Context, in *struct {
			ID int64 `path:"id"`
		}) (*struct{}, error) {
			p, err := s.Store.PersonaByID(ctx, in.ID)
			if err != nil {
				return nil, notFound(err)
			}
			if p.SiteID == 0 && p.Active {
				return nil, huma.Error409Conflict("the active default persona answers every site without its own; activate another default first")
			}
			return nil, notFound(s.Store.DeletePersona(ctx, in.ID))
		})
}
