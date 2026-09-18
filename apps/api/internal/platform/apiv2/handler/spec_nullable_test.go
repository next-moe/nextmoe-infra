package handler

import (
	"bytes"
	"encoding/json"
	"testing"

	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gofiber/fiber/v3"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
)

const testImageHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestG18FailsWithoutNullablePasses(t *testing.T) {
	errs := CheckG18(v2DocBeforeNullable(t))
	require.NotEmpty(t, errs, "G18 must see the unpatched document's missing nulls")
}

func TestSpecNullableAdmitsDocumentedNulls(t *testing.T) {
	after := Setup(fiber.New()).OpenAPI()
	before := v2DocBeforeNullable(t)

	news := repr.NewsItem{
		Object:  "news_item",
		ID:      "1",
		Title:   "title",
		Summary: "summary",
		Source: repr.NewsSource{
			Object:      "news_source",
			Name:        "hihyou",
			DisplayName: "hihyou",
			HomepageURL: "https://example.com/",
			Attribution: "attribution",
			ColumnURL:   "https://example.com/column",
		},
		SourceURL:   "https://example.com/item",
		Lane:        "news",
		Banner:      nil,
		PublishedAt: "2026-09-17T00:00:00Z",
	}
	img := repr.Image{
		URL:       "https://img.example.com/" + testImageHash + ".webp",
		Hash:      testImageHash,
		Source:    "vndb",
		Sexual:    nil,
		Violence:  nil,
		Width:     nil,
		Height:    nil,
		Thumbhash: nil,
	}

	newsInst := asJSON(t, news)
	imgInst := asJSON(t, img)

	require.Error(t, compileComponent(t, before, "NewsItem").Validate(newsInst),
		"positive control: NewsItem.banner null must fail the unpatched document")
	require.Error(t, compileComponent(t, before, "Image").Validate(imgInst),
		"positive control: Image.sexual/violence null must fail the unpatched document")

	require.NoError(t, compileComponent(t, after, "NewsItem").Validate(newsInst))
	require.NoError(t, compileComponent(t, after, "Image").Validate(imgInst))

	raw, err := json.Marshal(news)
	require.NoError(t, err)
	var newsObj map[string]any
	require.NoError(t, json.Unmarshal(raw, &newsObj))
	newsObj["banner"] = "not-an-image"
	require.Error(t, compileComponent(t, after, "NewsItem").Validate(newsObj),
		"banner as a string must still fail after the fix")

	bogus := "bogus"
	imgBad := img
	imgBad.Sexual = &bogus
	require.Error(t, compileComponent(t, after, "Image").Validate(asJSON(t, imgBad)),
		"sexual=bogus must still fail after the fix")
}

func v2DocBeforeNullable(t *testing.T) *huma.OpenAPI {
	t.Helper()
	skipNullablePasses = true
	t.Cleanup(func() { skipNullablePasses = false })
	return Setup(fiber.New()).OpenAPI()
}

func compileComponent(t *testing.T, doc *huma.OpenAPI, name string) *jsonschema.Schema {
	t.Helper()
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	root, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	require.NoError(t, err)
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	require.NoError(t, c.AddResource("mem://v2.json", root))
	sch, err := c.Compile("mem://v2.json#/components/schemas/" + name)
	require.NoError(t, err)
	return sch
}

func asJSON(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	require.NoError(t, err)
	return inst
}
