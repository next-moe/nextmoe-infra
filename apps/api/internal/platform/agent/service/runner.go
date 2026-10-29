package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"api/internal/platform/agent/model"
	"api/internal/platform/agent/stream"
	"api/internal/platform/agent/tools"
	"api/internal/platform/llm"
	"api/internal/platform/settings/keys"
)

const runTimeout = 5 * time.Minute

var webTools = []string{"web_search", "read_url"}

type Runner struct {
	store  *Store
	gw     *llm.Gateway
	broker *stream.Broker
	quota  *Quota
	tools  func(siteID int64, userToken string) *tools.Registry

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
	wg      sync.WaitGroup
}

func NewRunner(store *Store, gw *llm.Gateway, broker *stream.Broker, quota *Quota, toolsFor func(siteID int64, userToken string) *tools.Registry) *Runner {
	return &Runner{store: store, gw: gw, broker: broker, quota: quota, tools: toolsFor, cancels: map[string]context.CancelFunc{}}
}

type Turn struct {
	Run         *model.Run
	Conv        *model.Conversation
	Persona     *model.Persona
	History     []model.Message
	Reservation Reservation
	SiteName    string
	Uploads     map[string]*model.Upload
	UserToken   string
	Web         bool
}

func (r *Runner) Start(t Turn) {
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	r.mu.Lock()
	r.cancels[t.Run.ID] = cancel
	r.mu.Unlock()
	w := r.broker.Open(t.Run.ID)
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer func() {
			r.mu.Lock()
			delete(r.cancels, t.Run.ID)
			r.mu.Unlock()
			cancel()
		}()
		r.execute(ctx, t, w)
	}()
}

func (r *Runner) Cancel(runID string) bool {
	r.mu.Lock()
	cancel := r.cancels[runID]
	r.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (r *Runner) Wait() { r.wg.Wait() }

type stepState struct {
	w       *stream.Writer
	textID  string
	reasID  string
	textOn  bool
	reasOn  bool
	textBuf strings.Builder
}

func (s *stepState) on(ev llm.Event) {
	switch ev.Kind {
	case llm.EventText:
		if !s.textOn {
			if strings.TrimSpace(ev.Text) == "" {
				return
			}
			ev.Text = strings.TrimLeft(ev.Text, " \t\r\n")
			s.textOn = true
			s.w.Emit(map[string]any{"type": "text-start", "id": s.textID})
		}
		s.textBuf.WriteString(ev.Text)
		s.w.Delta("text-delta", s.textID, ev.Text)
	case llm.EventReasoning:
		if !s.reasOn {
			s.reasOn = true
			s.w.Emit(map[string]any{"type": "reasoning-start", "id": s.reasID})
		}
		s.w.Delta("reasoning-delta", s.reasID, ev.Text)
	}
}

func (s *stepState) close() {
	if s.reasOn {
		s.w.Emit(map[string]any{"type": "reasoning-end", "id": s.reasID})
	}
	if s.textOn {
		s.w.Emit(map[string]any{"type": "text-end", "id": s.textID})
	}
}

func (r *Runner) execute(ctx context.Context, t Turn, w *stream.Writer) {
	defer w.Close()
	run := t.Run
	w.Emit(map[string]any{"type": "start", "messageId": strconv.FormatInt(run.AssistantMessageID, 10)})
	w.Emit(map[string]any{"type": "data-run", "data": map[string]any{"run_id": run.ID, "conversation_id": strconv.FormatInt(run.ConversationID, 10)}})

	reg := r.tools(run.SiteID, t.UserToken)
	var skip []string
	if !t.Web {
		skip = webTools
	}
	maxSteps := int(keys.AgentTurnMaxSteps.Get())
	trun := reg.NewRun(maxSteps * 2)
	msgs, vision := buildMessages(t, time.Now())
	scene := llm.SceneAssistant
	if vision {
		scene = llm.SceneVision
	}
	var parts []Part
	var found []foundCards
	usedWeb := false
	fin := Finish{Status: model.RunCompleted}
	titled := make(chan bool, 1)
	if t.Conv.Title == "" {
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			titled <- r.titleConversation(t, w)
		}()
	} else {
		titled <- true
	}

	for step := 0; step < maxSteps; step++ {
		w.Emit(map[string]any{"type": "start-step"})
		parts = append(parts, Part{Type: "step-start"})
		st := &stepState{w: w, textID: fmt.Sprintf("text-%d", step), reasID: fmt.Sprintf("reasoning-%d", step)}
		last := step == maxSteps-1 || trun.Exhausted()
		call := llm.Call{
			Scene: scene, AffinityKey: strconv.FormatInt(run.ConversationID, 10), Messages: msgs,
			MaxTokens: int(keys.AgentTurnMaxOutputTokens.Get()), Site: t.SiteName, UserID: run.UserID,
		}
		if !last {
			call.Tools, call.ToolChoice = reg.Specs(skip...), llm.ToolChoiceAuto
		} else if note := strings.TrimSpace(keys.AgentPromptLastStep.ForSite(uint(run.SiteID))); step > 0 && note != "" {
			call.Messages = append(slices.Clone(msgs), llm.Message{Role: llm.RoleUser, Content: note})
		}
		res, err := r.gw.Stream(ctx, call, st.on)
		st.close()
		fin.Steps++
		fin.InputTokens += res.Usage.InputTokens
		fin.OutputTokens += res.Usage.OutputTokens
		if err != nil {
			slog.Warn("agent: run step failed", "run", run.ID, "step", step, "err", err)
			fin.Status, fin.ErrorKind = model.RunFailed, string(llm.KindOf(err))
			if errors.Is(ctx.Err(), context.Canceled) {
				fin.Status = model.RunCanceled
			}
			if text := strings.TrimSpace(st.textBuf.String()); text != "" {
				parts = append(parts, Part{Type: "text", Text: text})
			}
			break
		}
		if text := strings.TrimSpace(res.Text); text != "" {
			parts = append(parts, Part{Type: "text", Text: text})
		}
		if len(res.ToolCalls) == 0 {
			w.Emit(map[string]any{"type": "finish-step"})
			break
		}
		msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Content: res.Text, ToolCalls: res.ToolCalls})
		for _, tc := range res.ToolCalls {
			var input any
			_ = json.Unmarshal([]byte(tc.Arguments), &input)
			w.Emit(map[string]any{"type": "tool-input-available", "toolCallId": tc.ID, "toolName": tc.Name, "input": input})
			if contains(webTools, tc.Name) {
				usedWeb = true
			}
			out := trun.Call(ctx, tc.Name, tc.Arguments)
			summary := stepOutput(out)
			w.Emit(map[string]any{"type": "tool-output-available", "toolCallId": tc.ID, "output": summary})
			parts = append(parts, toolPart(tc, input, summary))
			found = append(found, foundCards{cards: out.Cards, lookup: isLookup(tc.Name)})
			for _, s := range out.Sources {
				w.Emit(map[string]any{"type": "source-url", "sourceId": s.URL, "url": s.URL, "title": s.Title})
				parts = append(parts, Part{Type: "source-url", SourceID: s.URL, URL: s.URL, Title: s.Title})
			}
			msgs = append(msgs, llm.Message{Role: llm.RoleTool, ToolCallID: tc.ID, Content: out.Text})
		}
		w.Emit(map[string]any{"type": "finish-step"})
	}

	for _, c := range pickCards(found, answerText(parts)) {
		w.Emit(map[string]any{"type": "data-entity-card", "id": c.ID, "data": c.Data})
		parts = append(parts, Part{Type: "data-entity-card", ID: c.ID, Data: c.Data})
	}
	fin.Parts = dedupeCards(parts)
	settleCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if fin.Status == model.RunCompleted {
		price := t.Reservation.Price
		if t.Web && !usedWeb {
			price = max(price-int(keys.AgentPriceWebSurcharge.Get()), 0)
		}
		s := r.quota.Settle(settleCtx, run.UserID, t.Reservation, price, run.ID, run.Mode)
		fin.FreeUnits, fin.PaidUnits, fin.Settle = s.Free, s.Paid, s.State
		w.Emit(map[string]any{"type": "data-quota", "data": map[string]any{
			"charged_free": s.Free, "charged_points": s.Paid, "free_left": r.quota.FreeLeft(settleCtx, run.UserID),
		}})
		w.Emit(map[string]any{"type": "finish"})
	} else {
		r.quota.Release(settleCtx, run.UserID, t.Reservation)
		if fin.Status == model.RunCanceled {
			w.Emit(map[string]any{"type": "abort"})
		} else {
			w.Emit(map[string]any{"type": "error", "errorText": userFacingError(fin.ErrorKind)})
		}
	}
	if err := r.store.FinishRun(settleCtx, run, fin); err != nil {
		slog.Error("agent: persist finished run", "run", run.ID, "err", err)
	}
	if ok := <-titled; !ok && fin.Status == model.RunCompleted {
		r.titleConversation(t, nil)
	}
}

const maxStepEntities = 12

func stepOutput(out tools.Output) map[string]any {
	entities := make([]json.RawMessage, 0, min(len(out.Cards), maxStepEntities))
	for _, c := range out.Cards {
		if len(entities) == maxStepEntities {
			break
		}
		entities = append(entities, c.Data)
	}
	links := make([]tools.Source, 0, len(out.Sources))
	links = append(links, out.Sources...)
	return map[string]any{"cards": len(out.Cards), "sources": len(out.Sources), "entities": entities, "links": links}
}

func toolPart(tc llm.ToolCall, input any, summary map[string]any) Part {
	in, err := json.Marshal(input)
	if err != nil || input == nil {
		in, _ = json.Marshal(tc.Arguments)
	}
	out, _ := json.Marshal(summary)
	return Part{Type: "tool-" + tc.Name, ToolCallID: tc.ID, State: "output-available", Input: in, Output: out}
}

func userFacingError(kind string) string {
	switch llm.ErrorKind(kind) {
	case llm.ErrKindContentFiltered:
		return "这个问题模型无法回答，换个说法试试吧。"
	case llm.ErrKindTimeout:
		return "模型响应超时了，请稍后重试。"
	default:
		return "模型暂时不可用，请稍后重试。"
	}
}

func dedupeCards(parts []Part) []Part {
	seen := map[string]bool{}
	out := parts[:0:0]
	for _, p := range parts {
		if p.Type == "data-entity-card" || p.Type == "source-url" {
			key := p.Type + p.ID + p.URL
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		out = append(out, p)
	}
	return out
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func (r *Runner) titleConversation(t Turn, w *stream.Writer) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var typed []string
	if n := len(t.History); n > 0 {
		for _, p := range decodeParts(t.History[n-1]) {
			if p.Type == "text" {
				typed = append(typed, p.Text)
			}
		}
	}
	content := clipRunes(strings.Join(typed, "\n"), 500)
	if strings.TrimSpace(content) == "" {
		return false
	}
	res, err := r.gw.Stream(ctx, llm.Call{
		Scene: llm.SceneTitle, Buffered: true, MaxTokens: 512, Site: t.SiteName, UserID: t.Run.UserID,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: keys.AgentPromptTitle.ForSite(uint(t.Run.SiteID))},
			{Role: llm.RoleUser, Content: content + "\n/no_think"},
		},
	}, nil)
	if err != nil {
		slog.Warn("agent: title", "conversation", t.Conv.ID, "err", err)
		return false
	}
	title := tidyTitle(firstLine(res.Text))
	if title == "" {
		return false
	}
	conv := *t.Conv
	if err := r.store.RenameConversation(ctx, &conv, title); err != nil {
		slog.Warn("agent: save title", "conversation", t.Conv.ID, "err", err)
		return false
	}
	if w != nil {
		w.Emit(map[string]any{"type": "data-title", "data": map[string]any{"title": title}})
	}
	return true
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

var titlePairs = [][2]string{{"「", "」"}, {"《", "》"}, {"『", "』"}, {"“", "”"}, {"\"", "\""}, {"'", "'"}}

func tidyTitle(s string) string {
	s = strings.TrimRight(strings.TrimSpace(s), "。.")
	for _, p := range titlePairs {
		if len(s) > len(p[0])+len(p[1]) && strings.HasPrefix(s, p[0]) && strings.HasSuffix(s, p[1]) {
			s = s[len(p[0]) : len(s)-len(p[1])]
		}
	}
	s = clipRunes(strings.TrimSpace(s), 40)
	for _, p := range titlePairs[:4] {
		if strings.Count(s, p[0]) > strings.Count(s, p[1]) {
			s = strings.TrimSpace(s[:strings.LastIndex(s, p[0])])
		}
	}
	return s
}
