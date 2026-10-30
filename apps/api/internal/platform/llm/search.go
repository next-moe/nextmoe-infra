package llm

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

type Citation struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

type SearchResult struct {
	Text      string
	Citations []Citation
	Model     string
	CallID    string
	Usage     Usage
}

func (g *Gateway) WebSearch(ctx context.Context, scene, query, instructions, site string, userID int64) (SearchResult, error) {
	q, _ := json.Marshal(query)
	body := map[string]json.RawMessage{
		"input": q,
		"tools": json.RawMessage(`[{"type":"web_search"}]`),
	}
	if instructions != "" {
		raw, _ := json.Marshal(instructions)
		body["instructions"] = raw
	}
	res, err := g.unary(ctx, Request{Scene: scene, Body: body, Site: site, UserID: userID}, ModelSearch, "/responses")
	if err != nil {
		return SearchResult{CallID: res.CallID}, err
	}
	out := parseResponses(res.Body)
	out.Model, out.CallID, out.Usage = res.Model, res.CallID, res.Usage
	return out, nil
}

func parseResponses(body []byte) SearchResult {
	var r struct {
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Annotations []struct {
					Type  string `json:"type"`
					URL   string `json:"url"`
					Title string `json:"title"`
				} `json:"annotations"`
			} `json:"content"`
		} `json:"output"`
	}
	_ = json.Unmarshal(body, &r)
	var out SearchResult
	var text strings.Builder
	seen := map[string]bool{}
	for _, item := range r.Output {
		if item.Type != "message" {
			continue
		}
		for _, c := range item.Content {
			if c.Type != "output_text" {
				continue
			}
			text.WriteString(c.Text)
			for _, a := range c.Annotations {
				if a.Type != "url_citation" || a.URL == "" || seen[a.URL] {
					continue
				}
				seen[a.URL] = true
				out.Citations = append(out.Citations, Citation{URL: a.URL, Title: citationTitle(a.Title, a.URL)})
			}
		}
	}
	out.Text = strings.TrimSpace(text.String())
	return out
}

func citationTitle(title, raw string) string {
	t := strings.TrimSpace(title)
	if t != "" && t != raw && strings.Trim(t, "0123456789[]") != "" {
		return t
	}
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return strings.TrimPrefix(u.Host, "www.")
	}
	return raw
}
