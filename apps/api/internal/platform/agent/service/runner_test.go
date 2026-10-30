package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"api/internal/platform/agent/model"
	"api/internal/platform/agent/stream"
	"api/internal/platform/agent/tools"
	aimigrate "api/internal/platform/ai/migrate"
	"api/internal/platform/llm"
	"api/internal/platform/settings"
	"api/internal/platform/settings/keys"
)

func scriptedLLM(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "text/event-stream")
		send := func(frames ...string) {
			for _, f := range frames {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", f)
			}
		}
		hasTool := false
		for _, m := range body.Messages {
			hasTool = hasTool || m.Role == "tool"
		}
		switch {
		case strings.Contains(body.Messages[0].Content, "作为标题"):
			send(`{"choices":[{"delta":{"content":"猫耳妹妹推荐"},"finish_reason":"stop"}]}`, "[DONE]")
		case hasTool:
			send(`{"choices":[{"delta":{"content":"推荐《千恋＊万花》。"},"finish_reason":"stop"}],"usage":{"prompt_tokens":50,"completion_tokens":8}}`, "[DONE]")
		default:
			send(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"search_catalog","arguments":"{\"query\":\"猫耳妹妹\"}"}}]},"finish_reason":"tool_calls"}]}`, "[DONE]")
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func fakeCatalog(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("search_mode") != "hybrid" {
			t.Errorf("search_catalog must ask for hybrid, got %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"15","display_name":"千恋＊万花","release_date":"2016-07-29","content_rating":"sensitive","content_limit":"sfw","localized":{"zh-Hans":{"value":"千恋万花"}}}],"search_mode":"hybrid"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRunnerAnswersWithToolsStreamsAndSettles(t *testing.T) {
	resetAgent(t)
	ctx := context.Background()
	if err := aimigrate.Run(testDB); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"llm_attempt", "llm_scene", "llm_price_revision", "llm_route", "llm_model", "llm_provider"} {
		testDB.Exec("TRUNCATE " + tbl + " RESTART IDENTITY CASCADE")
	}
	llmSrv := scriptedLLM(t)
	t.Setenv("AGENT_TEST_LLM_KEY", "sk-test")
	if err := llm.SeedDefaults(ctx, testDB, nil, llm.Bootstrap{UpstreamBaseURL: llmSrv.URL, UpstreamKeyEnv: "AGENT_TEST_LLM_KEY"}); err != nil {
		t.Fatal(err)
	}
	gw := llm.New(testDB, nil)
	if err := gw.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	settings.Override(t, keys.AgentFreeUnitsPerDay, int64(5))
	rdb := testRedis(t)
	quota := NewQuota(rdb, nil)
	cat := &tools.CatalogClient{Base: fakeCatalog(t).URL, HTTP: http.DefaultClient}
	reg := tools.NewRegistry(&tools.SearchCatalog{Client: cat}, &tools.GetWork{Client: cat})
	store := NewStore(testDB)
	broker := stream.NewBroker()
	runner := NewRunner(store, gw, broker, quota, func(int64, string) *tools.Registry { return reg })

	user := time.Now().UnixNano()
	defer rdb.Del(ctx, quota.freeKey(user))
	conv, _ := store.CreateConversation(ctx, user, 14, model.ModeAssistant, nil)
	res, err := quota.Reserve(ctx, user, 1)
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := store.StartRun(ctx, StartRequest{Conversation: conv, Parts: []Part{{Type: "text", Text: "有猫耳妹妹的催泪作品"}}, Mode: model.ModeAssistant, IdemKey: "x", Price: 1})
	if err != nil {
		t.Fatal(err)
	}
	persona, _ := store.Persona(ctx, 14)
	conv, _ = store.Conversation(ctx, user, 14, conv.ID)
	history, _ := store.Branch(ctx, conv.HeadMessageID)
	runner.Start(Turn{Run: run, Conv: conv, Persona: persona, History: history[:len(history)-1], Reservation: res, SiteName: "LetMoe"})

	var types []string
	var text strings.Builder
	err = broker.Subscribe(ctx, run.ID, 0, func(f stream.Frame) error {
		var m map[string]any
		_ = json.Unmarshal(f.Data, &m)
		types = append(types, m["type"].(string))
		if m["type"] == "text-delta" {
			text.WriteString(m["delta"].(string))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	runner.Wait()
	joined := strings.Join(types, ",")
	for _, want := range []string{"start", "tool-input-available", "tool-output-available", "data-entity-card", "text-start", "data-title", "data-quota", "finish"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("stream lacks %s: %s", want, joined)
		}
	}
	if text.String() != "推荐《千恋＊万花》。" {
		t.Fatalf("streamed text = %q", text.String())
	}
	done, _ := store.Run(ctx, user, run.ID)
	if done.Status != model.RunCompleted || done.Settle != model.SettleFree || done.FreeUnits != 1 || done.Steps != 2 {
		t.Fatalf("run = %+v", done)
	}
	conv, _ = store.Conversation(ctx, user, 14, conv.ID)
	branch, _ := store.Branch(ctx, conv.HeadMessageID)
	reply := decodeParts(branch[len(branch)-1])
	var kinds []string
	for _, p := range reply {
		kinds = append(kinds, p.Type)
	}
	if strings.Join(kinds, ",") != "step-start,tool-search_catalog,data-entity-card,step-start,text" || reply[4].Text != "推荐《千恋＊万花》。" {
		t.Fatalf("persisted reply parts = %v", kinds)
	}
	tool := reply[1]
	var in struct{ Query string }
	var out struct{ Cards, Sources int }
	_ = json.Unmarshal(tool.Input, &in)
	_ = json.Unmarshal(tool.Output, &out)
	if tool.ToolCallID != "c1" || tool.State != "output-available" || in.Query != "猫耳妹妹" || out.Cards != 1 || out.Sources != 0 {
		t.Fatalf("a stored tool step must match the streamed frames: %+v input=%s output=%s", tool, tool.Input, tool.Output)
	}
	if got := partsText(reply); strings.Contains(got, "search_catalog") {
		t.Fatalf("tool steps must stay out of the model's history: %q", got)
	}
	reply = reply[2:]
	var card struct {
		ID           string `json:"id"`
		Title        string `json:"title"`
		ContentLimit string `json:"content_limit"`
	}
	if err := json.Unmarshal(reply[0].Data, &card); err != nil || reply[0].ID != "work-15" || card.ID != "15" || card.Title != "千恋＊万花" || card.ContentLimit != "sfw" {
		t.Fatalf("a stored card must match the streamed frame {type, id, data}: id=%q data=%s", reply[0].ID, reply[0].Data)
	}
	if conv.Title != "猫耳妹妹推荐" {
		t.Fatalf("title = %q", conv.Title)
	}
	if quota.FreeLeft(ctx, user) != 4 {
		t.Fatalf("free left = %d", quota.FreeLeft(ctx, user))
	}
}
