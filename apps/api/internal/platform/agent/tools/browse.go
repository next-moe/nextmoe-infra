package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"api/internal/platform/llm"
)

type tagRef struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"display_name"`
	Localized   localized `json:"localized"`
	WorkCount   int       `json:"work_count"`
	IsSexual    *bool     `json:"is_sexual"`
	TraitPath   *struct {
		Group          string    `json:"group"`
		GroupLocalized localized `json:"group_localized"`
	} `json:"trait_path"`
}

func (t tagRef) card() Card {
	name := t.Localized.name(t.DisplayName)
	e := EntityCard{Kind: "tag", ID: t.ID, Title: name, Localized: t.Localized, ContentLimit: "sfw"}
	if t.WorkCount > 0 {
		e.Subtitle = fmt.Sprintf("标签 · %d 部作品", t.WorkCount)
		e.Facts = []Fact{{"作品数", fmt.Sprint(t.WorkCount)}}
	}
	return e.card()
}

var traitParts = []struct{ suffix, group string }{
	{"头发", "Hair"}, {"发", "Hair"}, {"髪", "Hair"}, {"毛", "Hair"},
	{"眼睛", "Eyes"}, {"眼", "Eyes"}, {"瞳", "Eyes"}, {"眸", "Eyes"},
}

var traitColours = map[string]string{"银": "灰色", "灰": "灰色", "白": "白色", "黑": "黑色", "金": "金色", "红": "红色", "赤": "红色",
	"粉": "粉色", "紫": "紫色", "蓝": "蓝色", "青": "青色", "绿": "绿色", "茶": "棕色", "棕": "棕色", "褐": "棕色", "橙": "橙色", "黄": "黄色"}

// Hair and eye colours are traits named by the colour alone under the Hair or
// Eyes group (VNDB's "Silver" reads 灰色), so 银发 and 白发 matched nothing and
// the model gave up on a plain silver-hair question.
func traitVariants(kw string) (queries []string, group string) {
	queries = []string{kw}
	for _, p := range traitParts {
		base, ok := strings.CutSuffix(kw, p.suffix)
		if !ok || base == "" {
			continue
		}
		base = strings.TrimSuffix(base, "色")
		if c, ok := traitColours[base]; ok {
			queries = append(queries, c)
		} else {
			queries = append(queries, base+"色", base)
		}
		return queries, p.group
	}
	return queries, ""
}

type SearchTags struct{ Client *CatalogClient }

func (t *SearchTags) Spec() llm.Tool {
	return llm.Tool{
		Name: "search_tags",
		Description: "Turn what the user describes into catalog tags, the first step of finding works or characters by theme, mood or trait. " +
			"Pass several short keywords at once (each one tag-sized, e.g. 催泪, 虐心, 猫耳, 妹妹, 校园) and try synonyms when a keyword finds nothing. " +
			"for=works returns work tags with how many works carry each; for=characters returns character traits grouped as hair, eyes, body, clothes, personality, role… (a colour such as 银发 or 红瞳 resolves to the colour trait of that group). " +
			"Then call browse_works or browse_characters with the ids.",
		Parameters: schema(map[string]any{
			"keywords": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1, "maxItems": 6},
			"for":      map[string]any{"type": "string", "enum": []string{"works", "characters"}, "description": "Default works."},
		}, "keywords"),
	}
}

func (t *SearchTags) Run(ctx context.Context, args json.RawMessage) (Output, error) {
	var in struct {
		Keywords []string `json:"keywords"`
		For      string   `json:"for"`
	}
	if err := json.Unmarshal(args, &in); err != nil || len(in.Keywords) == 0 {
		return Output{Text: `{"error":"keywords is required"}`}, nil
	}
	if len(in.Keywords) > 6 {
		in.Keywords = in.Keywords[:6]
	}
	object := "tag"
	if in.For == "characters" {
		object = "trait"
	}
	hits := make([][]tagRef, len(in.Keywords))
	errs := make([]error, len(in.Keywords))
	var wg sync.WaitGroup
	for i, kw := range in.Keywords {
		wg.Add(1)
		go func() {
			defer wg.Done()
			queries, group := []string{strings.TrimSpace(kw)}, ""
			if object == "trait" {
				queries, group = traitVariants(strings.TrimSpace(kw))
			}
			for _, q := range queries {
				var res struct{ Items []tagRef }
				limit := "3"
				if group != "" {
					limit = "10"
				}
				if errs[i] = t.Client.get(ctx, "/v2/catalog/search", url.Values{"object": {object}, "q": {q}, "limit": {limit}}, &res); errs[i] != nil {
					return
				}
				for _, h := range res.Items {
					if group == "" || (h.TraitPath != nil && h.TraitPath.Group == group) {
						hits[i] = append(hits[i], h)
					}
				}
				if len(hits[i]) > 0 {
					hits[i] = hits[i][:min(len(hits[i]), 3)]
					return
				}
			}
		}()
	}
	wg.Wait()
	counts := map[string]tagRef{}
	if object == "tag" {
		var ids []string
		for _, hs := range hits {
			for _, h := range hs {
				ids = append(ids, h.ID)
			}
		}
		if len(ids) > 0 {
			var full struct{ Items []tagRef }
			if err := t.Client.get(ctx, "/v2/catalog/tags", url.Values{"ids": {strings.Join(ids, ",")}}, &full); err == nil {
				for _, f := range full.Items {
					counts[f.ID] = f
				}
			}
		}
	}
	out := Output{}
	seen := map[string]bool{}
	type match struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		WorkCount int    `json:"work_count,omitempty"`
		Group     string `json:"group,omitempty"`
	}
	type result struct {
		Keyword string  `json:"keyword"`
		Matches []match `json:"matches"`
		Error   string  `json:"error,omitempty"`
	}
	results := make([]result, len(in.Keywords))
	for i, kw := range in.Keywords {
		results[i] = result{Keyword: kw, Matches: []match{}}
		if errs[i] != nil {
			results[i].Error = errs[i].Error()
			continue
		}
		for _, h := range hits[i] {
			if f, ok := counts[h.ID]; ok {
				h.WorkCount, h.IsSexual = f.WorkCount, f.IsSexual
			}
			if h.IsSexual != nil && *h.IsSexual && !t.Client.NSFW {
				continue
			}
			m := match{ID: h.ID, Name: h.Localized.name(h.DisplayName), WorkCount: h.WorkCount}
			if h.TraitPath != nil {
				m.Group = h.TraitPath.GroupLocalized.name(h.TraitPath.Group)
			}
			results[i].Matches = append(results[i].Matches, m)
			if object == "tag" && !seen[h.ID] {
				seen[h.ID] = true
				out.Cards = append(out.Cards, h.card())
			}
		}
	}
	raw, _ := json.Marshal(map[string]any{"for": object + "s", "results": results})
	out.Text = string(raw)
	return out, nil
}

var workSorts = map[string]string{"popular": "popularity", "classic": "rating_vndb", "newest": "released_desc"}

type BrowseWorks struct{ Client *CatalogClient }

func (t *BrowseWorks) Spec() llm.Tool {
	return llm.Tool{
		Name: "browse_works",
		Description: "List catalog works carrying ALL the given tag ids (from search_tags), up to 8, with tags, companies, ratings and synopsis, and the total that match. " +
			"sort=popular for what people play most, classic for the best rated, newest for recent releases. " +
			"Call it more than once (popular and classic) to recommend both; drop a tag when too few match.",
		Parameters: schema(map[string]any{
			"tag_ids":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1, "maxItems": 5},
			"sort":            map[string]any{"type": "string", "enum": []string{"popular", "classic", "newest"}},
			"exclude_tag_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 10},
			"released_after":  map[string]any{"type": "string", "description": "YYYY-MM-DD, only works released on or after it."},
		}, "tag_ids", "sort"),
	}
}

func idList(ids []string) string {
	var out []string
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	return strings.Join(out, ",")
}

func (t *BrowseWorks) Run(ctx context.Context, args json.RawMessage) (Output, error) {
	var in struct {
		TagIDs        []string `json:"tag_ids"`
		Sort          string   `json:"sort"`
		ExcludeTagIDs []string `json:"exclude_tag_ids"`
		ReleasedAfter string   `json:"released_after"`
	}
	if err := json.Unmarshal(args, &in); err != nil || idList(in.TagIDs) == "" || workSorts[in.Sort] == "" {
		return Output{Text: `{"error":"tag_ids and sort (popular, classic or newest) are required"}`}, nil
	}
	q := url.Values{"tag_id": {idList(in.TagIDs)}, "sort": {workSorts[in.Sort]}, "limit": {"8"}, "include_total": {"true"},
		"include": {"intros,tags,companies,ratings"}}
	if ex := idList(in.ExcludeTagIDs); ex != "" {
		q.Set("exclude_tag_id", ex)
	}
	if in.ReleasedAfter != "" {
		q.Set("released_after", in.ReleasedAfter)
	}
	var list struct {
		Items []work `json:"items"`
		Total int    `json:"total"`
	}
	if err := t.Client.get(ctx, "/v2/catalog/works", q, &list); err != nil {
		return Output{}, err
	}
	out := Output{Total: list.Total}
	briefs := make([]map[string]any, 0, len(list.Items))
	for _, w := range list.Items {
		b := w.searchBrief(t.Client.NSFW)
		if r := w.ratingText(); r != "" {
			b["rating"] = r
		}
		briefs = append(briefs, b)
		out.Cards = append(out.Cards, w.card(t.Client.NSFW))
	}
	raw, _ := json.Marshal(map[string]any{"sort": in.Sort, "total": list.Total, "works": briefs})
	out.Text = string(raw)
	return out, nil
}

type BrowseCharacters struct{ Client *CatalogClient }

func (t *BrowseCharacters) Spec() llm.Tool {
	return llm.Tool{
		Name:        "browse_characters",
		Description: "List catalog characters having the given trait ids (from search_tags with for=characters), up to 8, most popular first unless sort=newest, with the total that match.",
		Parameters: schema(map[string]any{
			"trait_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1, "maxItems": 5},
			"match":     map[string]any{"type": "string", "enum": []string{"all", "any"}, "description": "Default all."},
			"gender":    map[string]any{"type": "string", "enum": []string{"female", "male", "other"}},
			"sort":      map[string]any{"type": "string", "enum": []string{"popular", "newest"}},
		}, "trait_ids"),
	}
}

func (t *BrowseCharacters) Run(ctx context.Context, args json.RawMessage) (Output, error) {
	var in struct {
		TraitIDs []string `json:"trait_ids"`
		Match    string   `json:"match"`
		Gender   string   `json:"gender"`
		Sort     string   `json:"sort"`
	}
	if err := json.Unmarshal(args, &in); err != nil || idList(in.TraitIDs) == "" {
		return Output{Text: `{"error":"trait_ids is required"}`}, nil
	}
	sort := "popularity"
	if in.Sort == "newest" {
		sort = "newest"
	}
	q := url.Values{"trait_id": {idList(in.TraitIDs)}, "sort": {sort}, "limit": {"8"}, "include_total": {"true"},
		"include": {"gender,image,intros,work_count,traits"}}
	if in.Match == "any" {
		q.Set("trait_match", "any")
	}
	if in.Gender != "" {
		q.Set("gender", in.Gender)
	}
	var list struct {
		Items []character `json:"items"`
		Total int         `json:"total"`
	}
	if err := t.Client.get(ctx, "/v2/catalog/characters", q, &list); err != nil {
		return Output{}, err
	}
	out := Output{Total: list.Total}
	briefs := make([]map[string]any, 0, len(list.Items))
	for _, c := range list.Items {
		e := c.entity(t.Client.NSFW, nil)
		out.Cards = append(out.Cards, e.card())
		b := map[string]any{"id": c.ID, "name": e.Title, "traits": c.traits(t.Client.NSFW, 8)}
		if c.WorkCount != nil {
			b["work_count"] = *c.WorkCount
		}
		briefs = append(briefs, b)
	}
	raw, _ := json.Marshal(map[string]any{"total": list.Total, "characters": briefs})
	out.Text = string(raw)
	return out, nil
}
