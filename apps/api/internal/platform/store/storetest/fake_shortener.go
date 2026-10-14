package storetest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"

	"api/internal/platform/store/model"
	"api/internal/platform/store/shortener"
)

type MintCall struct {
	DestinationURL string
	Description    string
	// Reuse is nil when the caller omitted the field entirely. Both nil and
	// true are contract violations: the shortener would then reuse an existing
	// link for the same destination and every calling site's purchase link for
	// one product would collapse onto a single alias.
	Reuse *bool
	Auth  string
}

type StatsCall struct {
	Aliases []string
	From    string
	To      string
	Auth    string
}

// FakeShortener is an in-process stand-in for kungal-link-shortener speaking the
// 00-workflow §2 contract.
type FakeShortener struct {
	mu     sync.Mutex
	seq    int
	server *httptest.Server

	MintCalls  []MintCall
	StatsCalls []StatsCall

	// Series is the click history the fake reports, keyed by alias. An alias
	// absent from the map comes back as an empty series, never a 404.
	Series map[string][]shortener.DayStat

	// MintFails makes POST /s2s/links answer 503, standing in for the shortener
	// being down.
	MintFails bool

	// StickyAlias makes every mint return the same alias, standing in for a
	// shortener that stopped honouring reuse:false. Only the alias unique
	// indexes catch that, so the suite needs a way to produce it.
	StickyAlias string
}

func NewFakeShortener() *FakeShortener {
	f := &FakeShortener{Series: map[string][]shortener.DayStat{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/s2s/links", f.handleMint)
	mux.HandleFunc("/s2s/stats/daily", f.handleStats)
	f.server = httptest.NewServer(mux)
	return f
}

func (f *FakeShortener) Close() { f.server.Close() }

func (f *FakeShortener) Client(apiKey string) *shortener.Client {
	return shortener.New(f.server.URL, apiKey)
}

func (f *FakeShortener) Mints() []MintCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]MintCall(nil), f.MintCalls...)
}

func (f *FakeShortener) Stats() []StatsCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]StatsCall(nil), f.StatsCalls...)
}

func (f *FakeShortener) handleMint(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DestinationURL string `json:"destination_url"`
		Description    string `json:"description"`
		Reuse          *bool  `json:"reuse"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	f.MintCalls = append(f.MintCalls, MintCall{
		DestinationURL: body.DestinationURL,
		Description:    body.Description,
		Reuse:          body.Reuse,
		Auth:           r.Header.Get("Authorization"),
	})
	if f.MintFails {
		f.mu.Unlock()
		http.Error(w, "shortener down", http.StatusServiceUnavailable)
		return
	}
	f.seq++
	id := f.seq
	alias := fmt.Sprintf("a%d", id)
	if f.StickyAlias != "" {
		alias = f.StickyAlias
	}
	f.mu.Unlock()

	writeJSON(w, shortener.Link{
		ID:       int64(id),
		Alias:    alias,
		ShortURL: "https://s.test/" + alias,
		Reused:   false,
	})
}

func (f *FakeShortener) handleStats(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Aliases []string `json:"aliases"`
		From    string   `json:"from"`
		To      string   `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	f.StatsCalls = append(f.StatsCalls, StatsCall{
		Aliases: body.Aliases, From: body.From, To: body.To,
		Auth: r.Header.Get("Authorization"),
	})
	f.mu.Unlock()

	if len(body.Aliases) == 0 || len(body.Aliases) > shortener.MaxAliasesPerStatsCall {
		http.Error(w, "aliases out of range", http.StatusUnprocessableEntity)
		return
	}
	from, okFrom := model.ParseJSTDay(body.From)
	to, okTo := model.ParseJSTDay(body.To)
	if !okFrom || !okTo || to.Before(from) || model.DaySpan(from, to) > shortener.MaxStatsSpanDays {
		http.Error(w, "range out of bounds", http.StatusUnprocessableEntity)
		return
	}

	f.mu.Lock()
	out := map[string][]shortener.DayStat{}
	for _, alias := range body.Aliases {
		for _, p := range f.Series[alias] {
			if p.Date < body.From || p.Date > body.To {
				continue
			}
			out[alias] = append(out[alias], p)
		}
	}
	f.mu.Unlock()

	writeJSON(w, struct {
		Stats map[string][]shortener.DayStat `json:"stats"`
	}{Stats: out})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
