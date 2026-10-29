package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func fakeBrowseCatalog(t *testing.T) (*CatalogClient, *[]url.URL) {
	t.Helper()
	var mu sync.Mutex
	var seen []url.URL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, *r.URL)
		mu.Unlock()
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/v2/catalog/search" && q.Get("object") == "tag" && q.Get("q") == "催泪":
			_, _ = w.Write([]byte(`{"items":[{"id":"6296","display_name":"催泪"}]}`))
		case r.URL.Path == "/v2/catalog/search" && q.Get("object") == "tag" && q.Get("q") == "触手":
			_, _ = w.Write([]byte(`{"items":[{"id":"77","display_name":"触手"}]}`))
		case r.URL.Path == "/v2/catalog/search" && q.Get("object") == "trait":
			_, _ = w.Write([]byte(`{"items":[{"id":"534","display_name":"Kemonomimi","localized":{"zh-Hans":{"value":"兽耳"}},"trait_path":{"group":"Hair","group_localized":{"zh-Hans":{"value":"毛发"}}}}]}`))
		case r.URL.Path == "/v2/catalog/search":
			_, _ = w.Write([]byte(`{"items":[]}`))
		case r.URL.Path == "/v2/catalog/tags":
			_, _ = w.Write([]byte(`{"items":[{"id":"6296","display_name":"催泪","work_count":22,"is_sexual":false},{"id":"77","display_name":"触手","work_count":9,"is_sexual":true}]}`))
		case r.URL.Path == "/v2/catalog/works":
			_, _ = w.Write([]byte(`{"total":123,"items":[{"id":"86","display_name":"クラナド","release_date":"2004-04-28","content_limit":"sfw",
				"companies":[{"display_name":"Key"}],"tags":[{"display_name":"催泪"},{"display_name":"性描写","is_sexual":true}],
				"ratings":[{"source":"vndb","score":8.73,"vote_count":11510},{"source":"bangumi","score":8.9,"vote_count":6522},{"source":"dlsite","score":4.8,"vote_count":10}]}]}`))
		case r.URL.Path == "/v2/catalog/characters":
			_, _ = w.Write([]byte(`{"total":1589,"items":[{"id":"96652","display_name":"椎名 まゆり","gender":"female","work_count":12}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &CatalogClient{Base: srv.URL, HTTP: srv.Client()}, &seen
}

func TestSearchTagsCountsWorksAndHidesSexualTags(t *testing.T) {
	c, _ := fakeBrowseCatalog(t)
	out, err := (&SearchTags{Client: c}).Run(context.Background(), json.RawMessage(`{"keywords":["催泪","悲剧","触手"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Results []struct {
			Keyword string
			Matches []struct {
				ID        string
				WorkCount int `json:"work_count"`
			}
		}
	}
	_ = json.Unmarshal([]byte(out.Text), &body)
	if len(body.Results) != 3 || len(body.Results[0].Matches) != 1 || body.Results[0].Matches[0].WorkCount != 22 {
		t.Fatalf("each keyword reports its tags with work counts: %s", out.Text)
	}
	if len(body.Results[1].Matches) != 0 || len(body.Results[2].Matches) != 0 {
		t.Fatalf("an unknown keyword finds nothing and a sexual tag is hidden without nsfw: %s", out.Text)
	}
	var card map[string]any
	_ = json.Unmarshal(out.Cards[0].Data, &card)
	if len(out.Cards) != 1 || card["kind"] != "tag" || card["subtitle"] != "标签 · 22 部作品" {
		t.Fatalf("tag cards: %v", card)
	}
}

func TestSearchTagsForCharactersUsesTraits(t *testing.T) {
	c, seen := fakeBrowseCatalog(t)
	out, _ := (&SearchTags{Client: c}).Run(context.Background(), json.RawMessage(`{"keywords":["兽耳"],"for":"characters"}`))
	if !strings.Contains(out.Text, `"group":"毛发"`) || len(out.Cards) != 0 || (*seen)[0].Query().Get("object") != "trait" {
		t.Fatalf("traits carry their group and make no cards: %s", out.Text)
	}
}

func TestBrowseWorksFiltersByAllTagsAndReportsTheTotal(t *testing.T) {
	c, seen := fakeBrowseCatalog(t)
	out, err := (&BrowseWorks{Client: c}).Run(context.Background(), json.RawMessage(`{"tag_ids":["6296"," 90 "],"sort":"classic","released_after":"2020-01-01"}`))
	if err != nil {
		t.Fatal(err)
	}
	q := (*seen)[0].Query()
	if q.Get("tag_id") != "6296,90" || q.Get("sort") != "rating_vndb" || q.Get("released_after") != "2020-01-01" || q.Get("include_total") != "true" {
		t.Fatalf("query = %s", (*seen)[0].RawQuery)
	}
	if out.Total != 123 || !strings.Contains(out.Text, `"rating":"Bangumi 8.9（6522 人） · VNDB 8.7（11510 人）"`) {
		t.Fatalf("total and ratings: %d %s", out.Total, out.Text)
	}
	var card struct {
		Tags      []string
		Companies []string
		Ratings   []struct {
			Source string
			Votes  int
		}
	}
	_ = json.Unmarshal(out.Cards[0].Data, &card)
	if len(card.Tags) != 1 || card.Companies[0] != "Key" || len(card.Ratings) != 2 || card.Ratings[0].Source != "bangumi" {
		t.Fatalf("work card carries tags without sexual ones, companies and two ratings: %s", out.Cards[0].Data)
	}
	if bad, _ := (&BrowseWorks{Client: c}).Run(context.Background(), json.RawMessage(`{"tag_ids":["1"],"sort":"random"}`)); !strings.Contains(bad.Text, "error") {
		t.Fatalf("an unknown sort is refused: %s", bad.Text)
	}
}

func TestTraitVariantsResolveColourWithinItsGroup(t *testing.T) {
	for kw, want := range map[string][2]string{"银发": {"灰色", "Hair"}, "白髪": {"白色", "Hair"}, "红瞳": {"红色", "Eyes"}, "异色眼": {"异色", "Eyes"}} {
		q, g := traitVariants(kw)
		if g != want[1] || len(q) < 2 || q[1] != want[0] {
			t.Errorf("%s: got %v in %s, want %s in %s", kw, q, g, want[0], want[1])
		}
	}
	if q, g := traitVariants("傲娇"); len(q) != 1 || g != "" {
		t.Errorf("a personality keyword is searched as is, got %v %s", q, g)
	}
}

func TestBrowseCharactersByTraits(t *testing.T) {
	c, seen := fakeBrowseCatalog(t)
	out, _ := (&BrowseCharacters{Client: c}).Run(context.Background(), json.RawMessage(`{"trait_ids":["534","1"],"match":"any","gender":"female"}`))
	q := (*seen)[0].Query()
	if q.Get("trait_id") != "534,1" || q.Get("trait_match") != "any" || q.Get("gender") != "female" || q.Get("sort") != "popularity" {
		t.Fatalf("query = %s", (*seen)[0].RawQuery)
	}
	if out.Total != 1589 || len(out.Cards) != 1 {
		t.Fatalf("total %d cards %d", out.Total, len(out.Cards))
	}
}
