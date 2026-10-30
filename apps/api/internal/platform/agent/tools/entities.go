package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"api/internal/platform/llm"
)

type Fact struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type EntityCard struct {
	Kind         string    `json:"kind"`
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Localized    localized `json:"localized,omitempty"`
	Subtitle     string    `json:"subtitle,omitempty"`
	Cover        string    `json:"cover,omitempty"`
	Summary      string    `json:"summary,omitempty"`
	Facts        []Fact    `json:"facts,omitempty"`
	ContentLimit string    `json:"content_limit"`
}

func (e EntityCard) card() Card {
	if e.Facts == nil {
		e.Facts = []Fact{}
	}
	raw, _ := json.Marshal(e)
	return Card{ID: e.Kind + "-" + e.ID, Data: raw}
}

type localized map[string]struct {
	Value string `json:"value"`
}

func (l localized) name(fallback string) string {
	for _, lang := range []string{"zh-Hans", "zh", "zh-Hant"} {
		if v := l[lang].Value; v != "" {
			return v
		}
	}
	return fallback
}

func (l localized) others(main string) []string {
	var out []string
	for _, t := range l {
		if t.Value != "" && t.Value != main && !contains(out, t.Value) {
			out = append(out, t.Value)
		}
	}
	return out
}

type image struct {
	URL    string  `json:"url"`
	Sexual *string `json:"sexual"`
}

func (i *image) safeURL(nsfw bool) (string, string) {
	if i == nil || !strings.HasPrefix(i.URL, "https://") {
		return "", "sfw"
	}
	if i.Sexual != nil && *i.Sexual != "safe" {
		if !nsfw {
			return "", "nsfw"
		}
		return i.URL, "nsfw"
	}
	return i.URL, "sfw"
}

type intro struct {
	Lang  string `json:"lang"`
	Value string `json:"value"`
}

func pickIntro(intros []intro, n int) string {
	for _, want := range []string{"zh", "ja", ""} {
		for _, in := range intros {
			if strings.HasPrefix(in.Lang, want) && strings.TrimSpace(in.Value) != "" {
				return clipRunes(strings.TrimSpace(in.Value), n)
			}
		}
	}
	return ""
}

type alias struct {
	Value string `json:"value"`
}

type character struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"display_name"`
	Localized   localized `json:"localized"`
	Gender      *string   `json:"gender"`
	Birthday    *string   `json:"birthday"`
	HeightCm    *int      `json:"height_cm"`
	BloodType   *string   `json:"blood_type"`
	Image       *image    `json:"image"`
	Aliases     []alias   `json:"aliases"`
	Intros      []intro   `json:"intros"`
	WorkCount   *int      `json:"work_count"`
	Traits      []struct {
		DisplayName string    `json:"display_name"`
		Localized   localized `json:"localized"`
		Spoiler     string    `json:"spoiler"`
		IsSexual    bool      `json:"is_sexual"`
	} `json:"traits"`
}

type appearance struct {
	Work struct {
		ID          string    `json:"id"`
		DisplayName string    `json:"display_name"`
		Localized   localized `json:"localized"`
	} `json:"work"`
	RosterRole string `json:"roster_role"`
	Spoiler    string `json:"spoiler"`
	Voices     []struct {
		ID          string    `json:"id"`
		DisplayName string    `json:"display_name"`
		Localized   localized `json:"localized"`
	} `json:"voices"`
}

var genderZH = map[string]string{"female": "女", "male": "男", "nonbinary": "非二元", "ambiguous": "不明"}

var rosterZH = map[string]string{"main": "主角", "secondary": "主要角色", "appears": "登场"}

func (c character) traits(nsfw bool, n int) []string {
	var out []string
	for _, t := range c.Traits {
		if len(out) == n {
			break
		}
		if t.Spoiler != "none" || (t.IsSexual && !nsfw) {
			continue
		}
		out = append(out, t.Localized.name(t.DisplayName))
	}
	return out
}

func (c character) entity(nsfw bool, apps []appearance) EntityCard {
	e := EntityCard{Kind: "character", ID: c.ID, Title: c.Localized.name(c.DisplayName), Localized: c.Localized, Summary: pickIntro(c.Intros, 160)}
	e.Cover, e.ContentLimit = c.Image.safeURL(nsfw)
	var sub []string
	if c.Gender != nil && genderZH[*c.Gender] != "" {
		sub = append(sub, genderZH[*c.Gender])
		e.Facts = append(e.Facts, Fact{"性别", genderZH[*c.Gender]})
	}
	if c.Birthday != nil {
		var m, d int
		if _, err := fmt.Sscanf(*c.Birthday, "%d-%d", &m, &d); err == nil {
			e.Facts = append(e.Facts, Fact{"生日", fmt.Sprintf("%d月%d日", m, d)})
		}
	}
	if c.HeightCm != nil && *c.HeightCm > 0 {
		e.Facts = append(e.Facts, Fact{"身高", fmt.Sprintf("%d cm", *c.HeightCm)})
	}
	if c.BloodType != nil {
		e.Facts = append(e.Facts, Fact{"血型", strings.ToUpper(*c.BloodType) + " 型"})
	}
	var works, voices []string
	for _, a := range apps {
		if a.Spoiler != "" && a.Spoiler != "none" {
			continue
		}
		title := "《" + a.Work.Localized.name(a.Work.DisplayName) + "》"
		if len(works) == 0 {
			if r := rosterZH[a.RosterRole]; r != "" {
				sub = append(sub, title+r)
			} else {
				sub = append(sub, title)
			}
		}
		if len(works) < 4 && !contains(works, title) {
			works = append(works, title)
		}
		for _, v := range a.Voices {
			if name := v.Localized.name(v.DisplayName); len(voices) < 2 && !contains(voices, name) {
				voices = append(voices, name)
			}
		}
	}
	if len(voices) > 0 {
		e.Facts = append(e.Facts, Fact{"声优", strings.Join(voices, "、")})
	}
	if len(works) > 0 {
		e.Facts = append(e.Facts, Fact{"登场作品", strings.Join(works, "")})
	}
	if t := c.traits(nsfw, 6); len(t) > 0 {
		e.Facts = append(e.Facts, Fact{"特征", strings.Join(t, "、")})
	}
	e.Subtitle = strings.Join(sub, " · ")
	return e
}

type creditName struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"display_name"`
	Localized   localized `json:"localized"`
	Photo       *image    `json:"photo"`
	Aliases     []alias   `json:"aliases"`
	Intros      []intro   `json:"intros"`
}

type credit struct {
	Work struct {
		ID           string    `json:"id"`
		DisplayName  string    `json:"display_name"`
		Localized    localized `json:"localized"`
		ReleaseDate  *string   `json:"release_date"`
		ContentLimit string    `json:"content_limit"`
	} `json:"work"`
	Roles []struct {
		RoleName      string  `json:"role_name"`
		CharacterName *string `json:"character_name"`
	} `json:"roles"`
}

func (p creditName) entity(nsfw bool, credits []credit) EntityCard {
	e := EntityCard{Kind: "person", ID: p.ID, Title: p.Localized.name(p.DisplayName), Localized: p.Localized, Summary: pickIntro(p.Intros, 160)}
	e.Cover, e.ContentLimit = p.Photo.safeURL(nsfw)
	var roles, works []string
	for _, c := range credits {
		for _, r := range c.Roles {
			if r.RoleName != "" && !contains(roles, r.RoleName) {
				roles = append(roles, r.RoleName)
			}
		}
		if title := "《" + c.Work.Localized.name(c.Work.DisplayName) + "》"; len(works) < 5 && !contains(works, title) {
			works = append(works, title)
		}
	}
	e.Subtitle = strings.Join(roles, " · ")
	if len(roles) > 0 {
		e.Facts = append(e.Facts, Fact{"职能", strings.Join(roles, "、")})
	}
	if len(works) > 0 {
		e.Facts = append(e.Facts, Fact{"参与作品", strings.Join(works, "")})
	}
	var names []string
	for _, a := range p.Aliases {
		if len(names) < 3 && a.Value != e.Title && !contains(names, a.Value) {
			names = append(names, a.Value)
		}
	}
	if len(names) > 0 {
		e.Facts = append(e.Facts, Fact{"别名", strings.Join(names, "、")})
	}
	return e
}

type company struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"display_name"`
	Localized   localized `json:"localized"`
	CompanyKind string    `json:"company_kind"`
	WorkCount   *int      `json:"work_count"`
	Logo        *image    `json:"logo"`
	Aliases     []alias   `json:"aliases"`
	Intros      []intro   `json:"intros"`
}

var companyKindZH = map[string]string{"game_brand": "游戏品牌", "bunko": "文库", "publisher": "发行商", "anime_studio": "动画公司", "doujin_circle": "同人社团", "group": "企业集团"}

type workRef struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"display_name"`
	Localized   localized `json:"localized"`
	ReleaseDate *string   `json:"release_date"`
}

func titles(works []workRef, n int) string {
	var out []string
	for _, w := range works {
		if len(out) == n {
			break
		}
		out = append(out, "《"+w.Localized.name(w.DisplayName)+"》")
	}
	return strings.Join(out, "")
}

func (c company) entity(nsfw bool, works []workRef) EntityCard {
	e := EntityCard{Kind: "company", ID: c.ID, Title: c.Localized.name(c.DisplayName), Localized: c.Localized, Summary: pickIntro(c.Intros, 160)}
	e.Cover, e.ContentLimit = c.Logo.safeURL(nsfw)
	var sub []string
	if k := companyKindZH[c.CompanyKind]; k != "" {
		sub = append(sub, k)
		e.Facts = append(e.Facts, Fact{"类型", k})
	}
	if c.WorkCount != nil && *c.WorkCount > 0 {
		sub = append(sub, fmt.Sprintf("%d 部作品", *c.WorkCount))
		e.Facts = append(e.Facts, Fact{"作品数", fmt.Sprint(*c.WorkCount)})
	}
	if t := titles(works, 4); t != "" {
		e.Facts = append(e.Facts, Fact{"代表作", t})
	}
	e.Subtitle = strings.Join(sub, " · ")
	return e
}

type series struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"display_name"`
	Localized   localized `json:"localized"`
	WorkCount   int       `json:"work_count"`
	HasNSFW     bool      `json:"has_nsfw"`
	Intros      []intro   `json:"intros"`
}

func (s series) entity(works []workRef) EntityCard {
	e := EntityCard{Kind: "series", ID: s.ID, Title: s.Localized.name(s.DisplayName), Localized: s.Localized, Summary: pickIntro(s.Intros, 160), ContentLimit: "sfw"}
	if s.HasNSFW {
		e.ContentLimit = "nsfw"
	}
	if s.WorkCount > 0 {
		e.Subtitle = fmt.Sprintf("系列 · %d 部作品", s.WorkCount)
		e.Facts = append(e.Facts, Fact{"作品数", fmt.Sprint(s.WorkCount)})
	}
	if t := titles(works, 6); t != "" {
		e.Facts = append(e.Facts, Fact{"作品", t})
	}
	return e
}

var entityObjects = map[string]string{"character": "character", "person": "credit_name", "company": "company", "series": "series"}

type SearchEntities struct{ Client *CatalogClient }

func (t *SearchEntities) Spec() llm.Tool {
	return llm.Tool{
		Name:        "search_entities",
		Description: "Find characters, people (voice actors, artists, writers, composers…), companies (brands, circles, publishers) or series in the NextMoe catalog by name in any language. Returns up to 6 with ids for get_entity. Works are searched with search_catalog instead.",
		Parameters: schema(map[string]any{
			"kind":  map[string]any{"type": "string", "enum": []string{"character", "person", "company", "series"}},
			"query": map[string]any{"type": "string", "description": "A name or alias."},
		}, "kind", "query"),
	}
}

func (t *SearchEntities) Run(ctx context.Context, args json.RawMessage) (Output, error) {
	var in struct {
		Kind  string `json:"kind"`
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.Query) == "" || entityObjects[in.Kind] == "" {
		return Output{Text: `{"error":"kind (character, person, company or series) and query are required"}`}, nil
	}
	var hits struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := t.Client.get(ctx, "/v2/catalog/search", url.Values{"object": {entityObjects[in.Kind]}, "q": {in.Query}, "limit": {"6"}}, &hits); err != nil {
		return Output{}, err
	}
	if len(hits.Items) == 0 {
		return Output{Text: `{"results":[]}`}, nil
	}
	ids := make([]string, len(hits.Items))
	for i, h := range hits.Items {
		ids[i] = h.ID
	}
	nsfw := t.Client.NSFW
	var cards []EntityCard
	var briefs []map[string]any
	q := url.Values{"ids": {strings.Join(ids, ",")}}
	switch in.Kind {
	case "character":
		var list struct{ Items []character }
		q.Set("include", "gender,image,intros,work_count,traits")
		if err := t.Client.get(ctx, "/v2/catalog/characters", q, &list); err != nil {
			return Output{}, err
		}
		for _, c := range list.Items {
			e := c.entity(nsfw, nil)
			cards = append(cards, e)
			b := map[string]any{"id": c.ID, "name": e.Title, "other_names": c.Localized.others(e.Title), "traits": c.traits(nsfw, 6)}
			if c.WorkCount != nil {
				b["work_count"] = *c.WorkCount
			}
			briefs = append(briefs, b)
		}
	case "person":
		var list struct{ Items []creditName }
		q.Set("include", "photo,aliases")
		if err := t.Client.get(ctx, "/v2/catalog/credit-names", q, &list); err != nil {
			return Output{}, err
		}
		for _, p := range list.Items {
			e := p.entity(nsfw, nil)
			cards = append(cards, e)
			briefs = append(briefs, map[string]any{"id": p.ID, "name": e.Title, "other_names": p.Localized.others(e.Title)})
		}
	case "company":
		var list struct{ Items []company }
		q.Set("include", "logo,aliases")
		if err := t.Client.get(ctx, "/v2/catalog/companies", q, &list); err != nil {
			return Output{}, err
		}
		for _, c := range list.Items {
			e := c.entity(nsfw, nil)
			cards = append(cards, e)
			briefs = append(briefs, map[string]any{"id": c.ID, "name": e.Title, "other_names": c.Localized.others(e.Title), "kind": companyKindZH[c.CompanyKind], "work_count": c.WorkCount})
		}
	case "series":
		var list struct{ Items []series }
		if err := t.Client.get(ctx, "/v2/catalog/series", q, &list); err != nil {
			return Output{}, err
		}
		for _, s := range list.Items {
			e := s.entity(nil)
			cards = append(cards, e)
			briefs = append(briefs, map[string]any{"id": s.ID, "name": e.Title, "work_count": s.WorkCount})
		}
	}
	out := Output{}
	for _, e := range cards {
		out.Cards = append(out.Cards, e.card())
	}
	raw, _ := json.Marshal(map[string]any{"kind": in.Kind, "results": briefs})
	out.Text = string(raw)
	return out, nil
}

type GetEntity struct{ Client *CatalogClient }

func (t *GetEntity) Spec() llm.Tool {
	return llm.Tool{
		Name:        "get_entity",
		Description: "Read one character, person, company or series by the id search_entities returned: a character's profile, traits, works and voice actors; a person's roles and works; a company's or series' works.",
		Parameters: schema(map[string]any{
			"kind": map[string]any{"type": "string", "enum": []string{"character", "person", "company", "series"}},
			"id":   map[string]any{"type": "string"},
		}, "kind", "id"),
	}
}

func (t *GetEntity) Run(ctx context.Context, args json.RawMessage) (Output, error) {
	var in struct {
		Kind string      `json:"kind"`
		ID   json.Number `json:"id"`
	}
	if err := json.Unmarshal(args, &in); err != nil || in.ID == "" || entityObjects[in.Kind] == "" {
		return Output{Text: `{"error":"kind (character, person, company or series) and id are required"}`}, nil
	}
	id := url.PathEscape(in.ID.String())
	nsfw := t.Client.NSFW
	var e EntityCard
	detail := map[string]any{}
	switch in.Kind {
	case "character":
		var c character
		if err := t.Client.get(ctx, "/v2/catalog/characters/"+id, url.Values{"include": {"gender,birthday,height_cm,blood_type,image,traits,aliases,intros,work_count"}}, &c); err != nil {
			return Output{}, err
		}
		var apps struct{ Items []appearance }
		if err := t.Client.get(ctx, "/v2/catalog/characters/"+id+"/appearances", url.Values{"limit": {"8"}}, &apps); err != nil {
			return Output{}, err
		}
		e = c.entity(nsfw, apps.Items)
		var works []map[string]any
		for _, a := range apps.Items {
			if a.Spoiler != "" && a.Spoiler != "none" {
				continue
			}
			w := map[string]any{"work_id": a.Work.ID, "title": a.Work.Localized.name(a.Work.DisplayName), "role": rosterZH[a.RosterRole]}
			var voices []string
			for _, v := range a.Voices {
				voices = append(voices, v.Localized.name(v.DisplayName))
			}
			if len(voices) > 0 {
				w["voices"] = voices
			}
			works = append(works, w)
		}
		detail["works"] = works
		detail["traits"] = c.traits(nsfw, 20)
		detail["intro"] = pickIntro(c.Intros, 800)
	case "person":
		var p creditName
		if err := t.Client.get(ctx, "/v2/catalog/credit-names/"+id, url.Values{"include": {"photo,aliases,intros"}}, &p); err != nil {
			return Output{}, err
		}
		var credits struct{ Items []credit }
		if err := t.Client.get(ctx, "/v2/catalog/credit-names/"+id+"/credits", url.Values{"limit": {"20"}}, &credits); err != nil {
			return Output{}, err
		}
		e = p.entity(nsfw, credits.Items)
		var works []map[string]any
		for _, c := range credits.Items {
			if c.Work.ContentLimit == "nsfw" && !nsfw {
				continue
			}
			w := map[string]any{"work_id": c.Work.ID, "title": c.Work.Localized.name(c.Work.DisplayName), "release_date": c.Work.ReleaseDate}
			var roles []string
			for _, r := range c.Roles {
				if r.CharacterName != nil {
					roles = append(roles, r.RoleName+"："+*r.CharacterName)
				} else {
					roles = append(roles, r.RoleName)
				}
			}
			w["roles"] = roles
			works = append(works, w)
		}
		detail["works"] = works
		detail["intro"] = pickIntro(p.Intros, 800)
	case "company", "series":
		var works struct{ Items []workRef }
		q := url.Values{"limit": {"10"}, "sort": {"popularity"}}
		if in.Kind == "company" {
			var c company
			if err := t.Client.get(ctx, "/v2/catalog/companies/"+id, url.Values{"include": {"logo,aliases,intros"}}, &c); err != nil {
				return Output{}, err
			}
			q.Set("company_id", in.ID.String())
			if err := t.Client.get(ctx, "/v2/catalog/works", q, &works); err != nil {
				return Output{}, err
			}
			e = c.entity(nsfw, works.Items)
			detail["intro"] = pickIntro(c.Intros, 800)
		} else {
			var s series
			if err := t.Client.get(ctx, "/v2/catalog/series/"+id, url.Values{"include": {"intros"}}, &s); err != nil {
				return Output{}, err
			}
			q.Set("series_id", in.ID.String())
			if err := t.Client.get(ctx, "/v2/catalog/works", q, &works); err != nil {
				return Output{}, err
			}
			e = s.entity(works.Items)
			detail["intro"] = pickIntro(s.Intros, 800)
		}
		var list []map[string]any
		for _, w := range works.Items {
			list = append(list, map[string]any{"work_id": w.ID, "title": w.Localized.name(w.DisplayName), "release_date": w.ReleaseDate})
		}
		detail["works"] = list
	}
	detail["kind"], detail["id"], detail["name"] = e.Kind, e.ID, e.Title
	detail["other_names"] = e.Localized.others(e.Title)
	facts := map[string]string{}
	for _, f := range e.Facts {
		facts[f.Label] = f.Value
	}
	detail["facts"] = facts
	raw, _ := json.Marshal(detail)
	return Output{Text: string(raw), Cards: []Card{e.card()}}, nil
}
