package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"api/internal/platform/llm"
)

type CatalogClient struct {
	Base string
	NSFW bool
	HTTP *http.Client
}

func (c *CatalogClient) get(ctx context.Context, path string, q url.Values, out any) error {
	if c.NSFW {
		q.Set("nsfw", "true")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.Base, "/")+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("catalog %s: http %d", path, resp.StatusCode)
	}
	return json.Unmarshal(body, out)
}

type work struct {
	ID            string          `json:"id"`
	DisplayName   string          `json:"display_name"`
	ReleaseDate   *string         `json:"release_date"`
	ContentRating string          `json:"content_rating"`
	Cover         json.RawMessage `json:"cover"`
	Localized     map[string]struct {
		Value string `json:"value"`
	} `json:"localized"`
	Intros []struct {
		Lang  string `json:"lang"`
		Value string `json:"value"`
	} `json:"intros"`
	Tags []struct {
		DisplayName string `json:"display_name"`
	} `json:"tags"`
	Companies []struct {
		DisplayName string `json:"display_name"`
	} `json:"companies"`
}

func (w work) card() Card {
	raw, _ := json.Marshal(map[string]any{
		"kind": "work", "id": w.ID, "title": w.DisplayName, "localized": w.Localized,
		"release_date": w.ReleaseDate, "content_rating": w.ContentRating, "cover": w.Cover,
	})
	return Card{ID: "work-" + w.ID, Data: raw}
}

func (w work) brief() map[string]any {
	m := map[string]any{"id": w.ID, "title": w.DisplayName, "release_date": w.ReleaseDate}
	var names []string
	for lang, t := range w.Localized {
		if t.Value != "" && t.Value != w.DisplayName {
			names = append(names, lang+":"+t.Value)
		}
	}
	if len(names) > 0 {
		m["other_titles"] = names
	}
	return m
}

type SearchCatalog struct{ Client *CatalogClient }

func (t *SearchCatalog) Spec() llm.Tool {
	return llm.Tool{
		Name:        "search_catalog",
		Description: "Search the NextMoe catalog of visual novels / galgames / doujin games by title or by a free-text description of the plot, setting, characters or mood (any language). Returns up to 8 works with ids. Use it before naming any work.",
		Parameters: schema(map[string]any{
			"query": map[string]any{"type": "string", "description": "A title, an alias, or a description such as 'cat-eared little sister, tearjerker'."},
		}, "query"),
	}
}

func (t *SearchCatalog) Run(ctx context.Context, args json.RawMessage) (Output, error) {
	var in struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.Query) == "" {
		return Output{Text: `{"error":"query is required"}`}, nil
	}
	var list struct {
		Items      []work  `json:"items"`
		SearchMode *string `json:"search_mode"`
	}
	q := url.Values{"q": {in.Query}, "search_mode": {"hybrid"}, "limit": {"8"}, "include": {"titles"}}
	if err := t.Client.get(ctx, "/v2/catalog/works", q, &list); err != nil {
		return Output{}, err
	}
	out := Output{}
	briefs := make([]map[string]any, 0, len(list.Items))
	for _, w := range list.Items {
		briefs = append(briefs, w.brief())
		out.Cards = append(out.Cards, w.card())
	}
	raw, _ := json.Marshal(map[string]any{"works": briefs})
	out.Text = string(raw)
	return out, nil
}

type GetWork struct{ Client *CatalogClient }

func (t *GetWork) Spec() llm.Tool {
	return llm.Tool{
		Name:        "get_work",
		Description: "Read one catalog work by id: titles, release date, companies, tags and synopsis. Use the id from search_catalog.",
		Parameters:  schema(map[string]any{"id": map[string]any{"type": "string", "description": "Catalog work id"}}, "id"),
	}
}

func (t *GetWork) Run(ctx context.Context, args json.RawMessage) (Output, error) {
	var in struct {
		ID json.Number `json:"id"`
	}
	if err := json.Unmarshal(args, &in); err != nil || in.ID == "" {
		return Output{Text: `{"error":"id is required"}`}, nil
	}
	var w work
	q := url.Values{"include": {"intros,tags,companies"}}
	if err := t.Client.get(ctx, "/v2/catalog/works/"+url.PathEscape(in.ID.String()), q, &w); err != nil {
		return Output{}, err
	}
	m := w.brief()
	m["content_rating"] = w.ContentRating
	var tags []string
	for i, tg := range w.Tags {
		if i >= 30 {
			break
		}
		tags = append(tags, tg.DisplayName)
	}
	m["tags"] = tags
	var companies []string
	for _, c := range w.Companies {
		companies = append(companies, c.DisplayName)
	}
	m["companies"] = companies
	for _, in := range w.Intros {
		m["intro_"+in.Lang] = clip(in.Value, 1500)
	}
	raw, _ := json.Marshal(m)
	return Output{Text: string(raw), Cards: []Card{w.card()}}, nil
}
