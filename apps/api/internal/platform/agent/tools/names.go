package tools

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

type nameHit struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"display_name"`
	Latin       *string   `json:"latin"`
	Localized   localized `json:"localized"`
}

func (c *CatalogClient) searchNames(ctx context.Context, kind, query string, limit int) ([]nameHit, error) {
	var res struct {
		Items []nameHit `json:"items"`
	}
	err := c.get(ctx, "/v2/catalog/search", url.Values{"object": {entityObjects[kind]}, "q": {query}, "limit": {strconv.Itoa(limit)}}, &res)
	return res.Items, err
}

func nameKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(norm.NFKC.String(s)) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (h nameHit) named(query string) bool {
	q := nameKey(query)
	if q == "" {
		return false
	}
	bare := strings.TrimSuffix(q, "社")
	names := []string{h.DisplayName}
	if h.Latin != nil {
		names = append(names, *h.Latin)
	}
	for _, t := range h.Localized {
		names = append(names, t.Value)
	}
	for _, n := range names {
		if k := nameKey(n); k != "" && (k == q || k == bare) {
			return true
		}
	}
	return false
}

type namedAs struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (c *CatalogClient) sameNameIn(ctx context.Context, query string, kinds []string) []namedAs {
	found := make([][]namedAs, len(kinds))
	var wg sync.WaitGroup
	for i, kind := range kinds {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hits, err := c.searchNames(ctx, kind, query, 3)
			if err != nil {
				return
			}
			for _, h := range hits {
				if h.named(query) {
					found[i] = append(found[i], namedAs{Kind: kind, ID: h.ID, Name: h.Localized.name(h.DisplayName)})
				}
			}
		}()
	}
	wg.Wait()
	var out []namedAs
	for _, f := range found {
		out = append(out, f...)
	}
	return out
}

const nameMissNote = "Nothing in the catalog goes by this name. Catalog names are mostly the original Japanese or English, so retry before answering: " +
	"the Japanese spelling (宫泽 → 宮沢, 樱 → 桜), the original or English name, the name without a suffix such as 社, or another kind. " +
	"Do not answer from memory; if the retries also find nothing, tell the user it was not found and ask for the original name."

func otherKinds(kind string) []string {
	var out []string
	for _, k := range []string{"company", "person", "character", "series"} {
		if k != kind {
			out = append(out, k)
		}
	}
	return out
}
