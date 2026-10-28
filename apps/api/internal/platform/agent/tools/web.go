package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"

	"api/internal/platform/llm"
)

type WebSearch struct {
	APIKey string
	HTTP   *http.Client
}

func (t *WebSearch) Spec() llm.Tool {
	return llm.Tool{
		Name:        "web_search",
		Description: "Search the public web. Use it only when the catalog has no answer or the question is about recent news, releases or facts outside the catalog. Cite the URLs you rely on.",
		Parameters:  schema(map[string]any{"query": map[string]any{"type": "string"}}, "query"),
	}
}

func (t *WebSearch) Run(ctx context.Context, args json.RawMessage) (Output, error) {
	var in struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.Query) == "" {
		return Output{Text: `{"error":"query is required"}`}, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.search.brave.com/res/v1/web/search?"+url.Values{"q": {in.Query}, "count": {"6"}}.Encode(), nil)
	if err != nil {
		return Output{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", t.APIKey)
	resp, err := t.HTTP.Do(req)
	if err != nil {
		return Output{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Output{}, fmt.Errorf("web search: http %d", resp.StatusCode)
	}
	var body struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&body); err != nil {
		return Output{}, err
	}
	out := Output{}
	type hit struct{ Title, URL, Snippet string }
	var hits []hit
	for _, r := range body.Web.Results {
		hits = append(hits, hit{stripTags(r.Title), r.URL, stripTags(r.Description)})
		out.Sources = append(out.Sources, Source{URL: r.URL, Title: stripTags(r.Title)})
	}
	raw, _ := json.Marshal(map[string]any{"results": hits})
	out.Text = string(raw)
	return out, nil
}

var errBlockedAddress = errors.New("address not allowed")

func publicOnlyDialer() func(ctx context.Context, network, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 10 * time.Second}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if !publicAddr(ip) {
				return nil, fmt.Errorf("%w: %s", errBlockedAddress, ip)
			}
		}
		return d.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
}

func publicAddr(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsValid() && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() && !netip.MustParsePrefix("100.64.0.0/10").Contains(ip)
}

func NewPublicHTTPClient() *http.Client {
	return &http.Client{
		Timeout:   15 * time.Second,
		Transport: &http.Transport{DialContext: publicOnlyDialer(), Proxy: nil},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("redirect to a non-web scheme")
			}
			return nil
		},
	}
}

type ReadURL struct{ HTTP *http.Client }

func (t *ReadURL) Spec() llm.Tool {
	return llm.Tool{
		Name:        "read_url",
		Description: "Fetch a public web page and return its readable text. Use it on a URL from web_search or one the user gave.",
		Parameters:  schema(map[string]any{"url": map[string]any{"type": "string"}}, "url"),
	}
}

var (
	reDropBlocks = regexp.MustCompile(`(?is)<(script|style|noscript|svg|head)[^>]*>.*?</(script|style|noscript|svg|head)>`)
	reTags       = regexp.MustCompile(`(?s)<[^>]+>`)
	reSpace      = regexp.MustCompile(`[ \t\r\f\v]+`)
	reLines      = regexp.MustCompile(`\n\s*\n+`)
)

func stripTags(s string) string {
	s = reDropBlocks.ReplaceAllString(s, " ")
	s = reTags.ReplaceAllString(s, "\n")
	s = html.UnescapeString(s)
	s = reSpace.ReplaceAllString(s, " ")
	return strings.TrimSpace(reLines.ReplaceAllString(s, "\n"))
}

func (t *ReadURL) Run(ctx context.Context, args json.RawMessage) (Output, error) {
	var in struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return Output{Text: `{"error":"url is required"}`}, nil
	}
	u, err := url.Parse(strings.TrimSpace(in.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Output{Text: `{"error":"an http(s) URL is required"}`}, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Output{}, err
	}
	req.Header.Set("User-Agent", "NextMoeAgent/1.0 (+https://nextmoe.dev)")
	resp, err := t.HTTP.Do(req)
	if err != nil {
		if errors.Is(err, errBlockedAddress) {
			return Output{Text: `{"error":"that address is not reachable from here"}`}, nil
		}
		return Output{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Output{Text: fmt.Sprintf(`{"error":"http %d"}`, resp.StatusCode)}, nil
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/") && !strings.Contains(ct, "html") && !strings.Contains(ct, "json") {
		return Output{Text: fmt.Sprintf(`{"error":"unsupported content type %q"}`, ct)}, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return Output{}, err
	}
	text := string(body)
	if strings.Contains(ct, "html") {
		text = stripTags(text)
	}
	raw, _ := json.Marshal(map[string]any{"url": resp.Request.URL.String(), "text": clip(text, 12000)})
	return Output{Text: string(raw), Sources: []Source{{URL: resp.Request.URL.String()}}}, nil
}
