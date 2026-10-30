package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"api/internal/platform/llm"
)

const (
	modelOutputCap = 16000
	toolTimeout    = 20 * time.Second
)

type Card struct {
	ID   string          `json:"id"`
	Data json.RawMessage `json:"data"`
}

type Source struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

type Output struct {
	Text    string
	Cards   []Card
	Sources []Source
	Total   int
	Meta    map[string]any
	Browse  *BrowseLink
	Err     error
}

type BrowseLink struct {
	Target string   `json:"target" doc:"works or characters"`
	IDs    []string `json:"ids" doc:"Tag ids for works, trait ids for characters; all must match."`
	Names  []string `json:"names"`
	Sort   string   `json:"sort"`
	Total  int      `json:"total"`
}

type Tool interface {
	Spec() llm.Tool
	Run(ctx context.Context, args json.RawMessage) (Output, error)
}

type Registry struct {
	tools map[string]Tool
	order []string
}

func NewRegistry(ts ...Tool) *Registry {
	return (&Registry{tools: map[string]Tool{}}).Add(ts...)
}

func (r *Registry) Add(ts ...Tool) *Registry {
	for _, t := range ts {
		if t == nil {
			continue
		}
		name := t.Spec().Name
		r.tools[name] = t
		r.order = append(r.order, name)
	}
	return r
}

func (r *Registry) Specs(skip ...string) []llm.Tool {
	out := make([]llm.Tool, 0, len(r.order))
	for _, name := range r.order {
		if !contains(skip, name) {
			out = append(out, r.tools[name].Spec())
		}
	}
	return out
}

type SiteTool interface {
	SiteInstructions() string
}

// SiteNotes reports whether any site tool is registered, and the distinct
// instructions their servers gave.
func (r *Registry) SiteNotes() (bool, []string) {
	has := false
	var notes []string
	for _, name := range r.order {
		st, ok := r.tools[name].(SiteTool)
		if !ok {
			continue
		}
		has = true
		if n := st.SiteInstructions(); n != "" && !contains(notes, n) {
			notes = append(notes, n)
		}
	}
	return has, notes
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

type Run struct {
	reg    *Registry
	budget int
	mu     sync.Mutex
	calls  int
	byName map[string]int
	reuse  map[string]Output
}

func (r *Registry) NewRun(budget int) *Run {
	return &Run{reg: r, budget: budget, byName: map[string]int{}, reuse: map[string]Output{}}
}

func (r *Run) Exhausted() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls >= r.budget
}

func (r *Run) Call(ctx context.Context, name, rawArgs string) Output {
	t := r.reg.tools[name]
	if t == nil {
		return Output{Text: fmt.Sprintf(`{"error":"unknown tool %q"}`, name)}
	}
	args := json.RawMessage(strings.TrimSpace(rawArgs))
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	var probe map[string]any
	if err := json.Unmarshal(args, &probe); err != nil {
		return Output{Text: `{"error":"arguments must be a JSON object"}`}
	}
	key := name + "\x00" + canonical(probe)
	r.mu.Lock()
	if out, ok := r.reuse[key]; ok {
		r.mu.Unlock()
		return out
	}
	if r.calls >= r.budget {
		r.mu.Unlock()
		return Output{Text: `{"error":"tool budget spent; answer with what you have"}`}
	}
	if mc, ok := t.(interface{ MaxCalls() int }); ok && r.byName[name] >= mc.MaxCalls() {
		r.mu.Unlock()
		return Output{Text: fmt.Sprintf(`{"error":"%s may run only %d times per answer; answer with what you have"}`, name, mc.MaxCalls())}
	}
	r.calls++
	r.byName[name]++
	r.mu.Unlock()

	timeout := toolTimeout
	if tt, ok := t.(interface{ Timeout() time.Duration }); ok {
		timeout = tt.Timeout()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := t.Run(ctx, args)
	if err != nil {
		msg := err.Error()
		if errors.Is(err, context.DeadlineExceeded) {
			msg = "tool timed out"
		}
		out = Output{Text: fmt.Sprintf(`{"error":%q}`, msg), Err: err}
	}
	out.Text = clip(out.Text, modelOutputCap)
	r.mu.Lock()
	r.reuse[key] = out
	r.mu.Unlock()
	return out
}

func canonical(v map[string]any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

func schema(props map[string]any, required ...string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"type": "object", "properties": props, "required": required})
	return raw
}
