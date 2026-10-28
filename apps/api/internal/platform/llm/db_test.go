package llm_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	aimigrate "api/internal/platform/ai/migrate"
	"api/internal/platform/llm"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSeededGatewayStreamsAndRecordsTheAttempt(t *testing.T) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
		return
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := aimigrate.Run(db); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"llm_attempt", "llm_event", "llm_scene", "llm_price_revision", "llm_route", "llm_model", "llm_provider"} {
		if err := db.Exec("TRUNCATE " + tbl + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatal(err)
		}
	}

	var gotAuth, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var body struct {
			Model string `json:"model"`
		}
		_ = decodeJSON(r, &body)
		gotModel = body.Model
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":10}}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()
	t.Setenv("LLM_TEST_KEY", "sk-test")

	ctx := context.Background()
	if err := llm.SeedFromEnv(ctx, db, srv.URL, "LLM_TEST_KEY"); err != nil {
		t.Fatal(err)
	}
	if err := llm.SeedFromEnv(ctx, db, srv.URL, "LLM_TEST_KEY"); err != nil {
		t.Fatal(err)
	}
	var providers int64
	db.Model(&llm.Provider{}).Count(&providers)
	if providers != 1 {
		t.Fatalf("seed must run once, got %d providers", providers)
	}

	g := llm.New(db, nil)
	if err := g.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	res, err := g.Stream(ctx, llm.Call{Scene: llm.SceneAssistant, Site: "letmoe", UserID: 7,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hello"}}}, func(llm.Event) {})
	if err != nil || res.Text != "hi" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if gotAuth != "Bearer sk-test" || gotModel != "@cf/deepseek-ai/deepseek-v4-flash" {
		t.Fatalf("upstream saw auth=%q model=%q", gotAuth, gotModel)
	}
	var a llm.Attempt
	if err := db.Where("call_id = ?", res.CallID).Take(&a).Error; err != nil {
		t.Fatal(err)
	}
	if !a.OK || a.Site != "letmoe" || a.UserID != 7 || a.InputTokens != 1000 || a.CostNano != (1000*440_000+10*1_320_000)/1000 {
		t.Fatalf("attempt = %+v", a)
	}
}
