package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeNameCatalog(t *testing.T) *CatalogClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/v2/catalog/search" && q.Get("object") == "company" && q.Get("q") == "甜茶社":
			_, _ = w.Write([]byte(`{"items":[{"id":"414","display_name":"SWEET&TEA","localized":{"zh-Hans":{"value":"甜茶社"}}}]}`))
		case r.URL.Path == "/v2/catalog/search" && q.Get("object") == "credit_name" && q.Get("q") == "甜茶社":
			_, _ = w.Write([]byte(`{"items":[{"id":"9","display_name":"甜茶 太郎"}]}`))
		case r.URL.Path == "/v2/catalog/search" && q.Get("object") == "credit_name" && q.Get("q") == "花澤香菜":
			_, _ = w.Write([]byte(`{"items":[{"id":"505","display_name":"花澤香菜"}]}`))
		case r.URL.Path == "/v2/catalog/search":
			_, _ = w.Write([]byte(`{"items":[]}`))
		case r.URL.Path == "/v2/catalog/credit-names":
			_, _ = w.Write([]byte(`{"items":[{"id":"505","display_name":"花澤香菜"}]}`))
		case r.URL.Path == "/v2/catalog/credit-names/505":
			_, _ = w.Write([]byte(`{"id":"505","display_name":"花澤香菜"}`))
		case r.URL.Path == "/v2/catalog/credit-names/505/credits":
			_, _ = w.Write([]byte(`{"items":[{"work":{"id":"86","display_name":"アマツツミ","content_limit":"sfw"},"roles":[{"role_name":"声优","character_name":"朝比奈 空"}]}]}`))
		case r.URL.Path == "/v2/catalog/works" && q.Get("company_id") == "414":
			if q.Get("sort") == "released_desc" {
				_, _ = w.Write([]byte(`{"items":[{"id":"3","display_name":"ネコ神さまと、ななつぼし","release_date":"2019-02-22"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[{"id":"1","display_name":"枯れない世界と終わる花","release_date":"2016-11-25"}]}`))
		case r.URL.Path == "/v2/catalog/works":
			_, _ = w.Write([]byte(`{"items":[{"id":"7","display_name":"甜狱！"}]}`))
		case r.URL.Path == "/v2/catalog/companies/414":
			_, _ = w.Write([]byte(`{"id":"414","display_name":"SWEET&TEA","company_kind":"publisher"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &CatalogClient{Base: srv.URL, HTTP: srv.Client()}
}

type nameBody struct {
	Results    []any     `json:"results"`
	Works      []any     `json:"works"`
	SameNameAs []namedAs `json:"same_name_as"`
	Note       string    `json:"note"`
	Detail     struct {
		Works []struct{ Title string } `json:"works"`
	} `json:"detail"`
}

func runText(t *testing.T, tool Tool, args string) nameBody {
	t.Helper()
	out, err := tool.Run(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	var b nameBody
	if err := json.Unmarshal([]byte(out.Text), &b); err != nil {
		t.Fatalf("%v: %s", err, out.Text)
	}
	return b
}

func TestSearchEntitiesMissPointsAtTheKindThatHasTheName(t *testing.T) {
	c := fakeNameCatalog(t)
	b := runText(t, &SearchEntities{Client: c}, `{"kind":"series","query":"甜茶社"}`)
	if len(b.SameNameAs) != 1 || b.SameNameAs[0] != (namedAs{Kind: "company", ID: "414", Name: "甜茶社"}) || !strings.Contains(b.Note, "get_entity") {
		t.Fatalf("a series miss names the company called 甜茶社 and not the person merely sharing words: %+v", b)
	}
	b = runText(t, &SearchEntities{Client: c}, `{"kind":"person","query":"花泽香菜"}`)
	if len(b.SameNameAs) != 0 || !strings.Contains(b.Note, "original Japanese") || b.Results == nil {
		t.Fatalf("a plain miss tells the model to retry with the original name: %+v", b)
	}
}

func TestSearchEntitiesAttachesTheDetailOfASingleHit(t *testing.T) {
	b := runText(t, &SearchEntities{Client: fakeNameCatalog(t)}, `{"kind":"person","query":"花澤香菜"}`)
	if len(b.Results) != 1 || len(b.Detail.Works) != 1 || b.Detail.Works[0].Title != "アマツツミ" {
		t.Fatalf("a single hit comes with its works, so the model cannot stop at the name: %+v", b)
	}
}

func TestSearchCatalogFlagsACompanyName(t *testing.T) {
	b := runText(t, &SearchCatalog{Client: fakeNameCatalog(t)}, `{"query":"甜茶社"}`)
	if len(b.Works) != 1 || len(b.SameNameAs) != 1 || b.SameNameAs[0].Kind != "company" || !strings.Contains(b.Note, "get_entity") {
		t.Fatalf("a title search for a company name says it is a company: %+v", b)
	}
	b = runText(t, &SearchCatalog{Client: fakeNameCatalog(t)}, `{"query":"千恋万花"}`)
	if len(b.SameNameAs) != 0 || b.Note != "" {
		t.Fatalf("a title search that finds works and no namesake adds nothing: %+v", b)
	}
}

func TestGetEntityCompanyListsLatestWorks(t *testing.T) {
	out, err := (&GetEntity{Client: fakeNameCatalog(t)}).Run(context.Background(), json.RawMessage(`{"kind":"company","id":"414"}`))
	if err != nil {
		t.Fatal(err)
	}
	var b struct {
		Works  []struct{ Title string } `json:"works"`
		Latest []struct{ Title string } `json:"latest_works"`
	}
	_ = json.Unmarshal([]byte(out.Text), &b)
	if len(b.Works) != 1 || b.Works[0].Title != "枯れない世界と終わる花" || len(b.Latest) != 1 || b.Latest[0].Title != "ネコ神さまと、ななつぼし" {
		t.Fatalf("works by popularity and latest_works by release date: %s", out.Text)
	}
}

func TestSearchTagsNotesWhenNothingMatched(t *testing.T) {
	c, _ := fakeBrowseCatalog(t)
	b := runText(t, &SearchTags{Client: c}, `{"keywords":["软萌","小只"]}`)
	if !strings.Contains(b.Note, "never from memory") {
		t.Fatalf("an all-miss tells the model not to recommend from memory: %+v", b)
	}
	out, err := (&SearchTags{Client: c}).Run(context.Background(), json.RawMessage(`{"keywords":["催泪","软萌"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var withWorks struct {
		Popular struct {
			Works []struct{ Title string } `json:"works"`
		} `json:"popular_works"`
	}
	_ = json.Unmarshal([]byte(out.Text), &withWorks)
	if len(withWorks.Popular.Works) != 1 || withWorks.Popular.Works[0].Title != "クラナド" || out.Browse == nil || out.Browse.IDs[0] != "6296" {
		t.Fatalf("matched tags come with their popular works and the browse link: %s", out.Text)
	}
}

func TestSearchTagsErrorIsNotAMiss(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }))
	t.Cleanup(srv.Close)
	b := runText(t, &SearchTags{Client: &CatalogClient{Base: srv.URL, HTTP: srv.Client()}}, `{"keywords":["催泪"]}`)
	if b.Note != "" {
		t.Fatalf("a failed lookup must not tell the model the tag does not exist: %+v", b)
	}
}
