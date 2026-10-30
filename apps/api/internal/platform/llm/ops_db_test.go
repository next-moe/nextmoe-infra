package llm_test

import (
	"context"
	"testing"

	"api/internal/platform/llm"
)

func TestBudgetsStopSpendAndRaiseAlerts(t *testing.T) {
	db := gatewayDB(t)
	kek := testKEK(t)
	ctx := context.Background()
	g := llm.New(db, kek)
	a := llm.NewAdmin(db, kek, g)
	up := newFakeUpstream(t)
	str := func(s string) *string { return &s }
	i64 := func(n int64) *int64 { return &n }
	p, _ := a.CreateProvider(ctx, llm.ProviderInput{Name: str("p"), BaseURL: str(up.srv.URL), APIKey: str("k")})
	m, _ := a.CreateModel(ctx, llm.ModelInput{Key: str("chat"), Kind: str(llm.ModelChat)})
	if _, err := a.CreateRoute(ctx, llm.RouteInput{ModelID: i64(m.ID), ProviderID: i64(p.ID), UpstreamID: str("up"),
		Price: &llm.Price{InputMicro: 1_000_000, OutputMicro: 2_000_000}}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PutScene(ctx, "t.budget", llm.SceneInput{ModelID: m.ID, DailyBudgetMicro: 2000}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PutScene(ctx, "t.open", llm.SceneInput{ModelID: m.ID}); err != nil {
		t.Fatal(err)
	}
	budget := int64(1000)
	key, _, err := a.CreateAccessKey(ctx, llm.AccessKeyInput{Name: str("cron"), DailyBudgetMicro: &budget})
	if err != nil {
		t.Fatal(err)
	}
	_ = g.Refresh(ctx)
	ask := func(scene string, keyID int64) error {
		_, err := g.Stream(ctx, llm.Call{Scene: scene, AccessKeyID: keyID, Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}}, func(llm.Event) {})
		return err
	}
	for i := range 2 {
		if err := ask("t.budget", 0); err != nil {
			t.Fatalf("call %d within the scene budget: %v", i, err)
		}
	}
	if err := ask("t.budget", 0); llm.KindOf(err) != llm.ErrKindBudget {
		t.Fatalf("each call costs 1.02 milli-USD, so the third exceeds 2 milli-USD: err=%v", err)
	}
	if err := ask("t.open", key.ID); err != nil {
		t.Fatal(err)
	}
	if err := ask("t.open", key.ID); llm.KindOf(err) != llm.ErrKindBudget {
		t.Fatalf("the access key's own 1 milli-USD budget stops it on any scene: err=%v", err)
	}
	if err := ask("t.open", 0); err != nil {
		t.Fatalf("other callers of an unbudgeted scene are unaffected: %v", err)
	}
	events, _ := a.Events(ctx, 50)
	kinds := map[string]int{}
	for _, e := range events {
		kinds[e.Kind]++
	}
	if kinds["budget_80"] != 2 || kinds["budget_100"] != 2 {
		t.Fatalf("one 80%% and one 100%% event each for the scene and the key: %v", kinds)
	}
	scenes, _ := a.Scenes(ctx)
	for _, s := range scenes {
		if s.Key == "t.budget" && (s.SpentTodayNano != 2_040_000 || !s.Ready) {
			t.Fatalf("scene view: %+v", s)
		}
	}
	keys, _ := a.AccessKeys(ctx)
	if len(keys) != 1 || keys[0].SpentTodayNano != 1_020_000 || keys[0].DailyBudgetMicro != 1000 {
		t.Fatalf("key view: %+v", keys)
	}

	db.Exec("UPDATE llm_attempt SET created_at = now() - interval '2 hours' WHERE id IN (SELECT id FROM llm_attempt ORDER BY id LIMIT 2)")
	if err := a.RollupUsage(ctx); err != nil {
		t.Fatal(err)
	}
	var rolled int64
	db.Raw("SELECT coalesce(sum(calls), 0) FROM llm_usage_hourly").Scan(&rolled)
	if rolled != 2 {
		t.Fatalf("the two calls from two hours ago are rolled up, the current hour is not: %d", rolled)
	}
	// Between 00:00 and 02:00 in the budget zone, the calls moved back two hours fall on yesterday, outside a one-day report.
	report, err := a.Usage(ctx, 2, "scene")
	if err != nil {
		t.Fatal(err)
	}
	if report.Total.Calls != 4 || report.Total.CostNano != 4*1_020_000 || len(report.Daily) == 0 || report.Total.AvgMs <= 0 && report.Rows[0].AvgMs > 0 {
		t.Fatalf("rolled-up hours plus the live hour give every successful call once: %+v", report.Total)
	}
	byKey, _ := a.Usage(ctx, 7, "key")
	labels := map[string]int64{}
	for _, r := range byKey.Rows {
		labels[r.Label] = r.Calls
	}
	if labels["cron"] != 1 || labels["内部调用（ai / agent）"] != 3 {
		t.Fatalf("usage by key: %v", labels)
	}
	if _, err := a.Usage(ctx, 7, "nope"); err == nil {
		t.Fatal("an unknown group is refused")
	}
}

func TestConfigExportsAndReimportsByName(t *testing.T) {
	db := gatewayDB(t)
	kek := testKEK(t)
	ctx := context.Background()
	g := llm.New(db, kek)
	a := llm.NewAdmin(db, kek, g)
	str := func(s string) *string { return &s }
	i64 := func(n int64) *int64 { return &n }
	intp := func(n int) *int { return &n }
	p, _ := a.CreateProvider(ctx, llm.ProviderInput{Name: str("relay"), BaseURL: str("https://relay.example/v1"), APIKey: str("sk-1")})
	chat, _ := a.CreateModel(ctx, llm.ModelInput{Key: str("chat"), Kind: str(llm.ModelChat)})
	backup, _ := a.CreateModel(ctx, llm.ModelInput{Key: str("backup"), Kind: str(llm.ModelChat)})
	r, _ := a.CreateRoute(ctx, llm.RouteInput{ModelID: i64(chat.ID), ProviderID: i64(p.ID), UpstreamID: str("up"), Weight: intp(50), RPM: intp(60),
		Price: &llm.Price{InputMicro: 100, PerCallMicro: 5}})
	if _, err := a.PutScene(ctx, "cron.x", llm.SceneInput{ModelID: chat.ID, FallbackModelIDs: []int64{backup.ID}, Strategy: "cost", DailyBudgetMicro: 9}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.CreateAccessKey(ctx, llm.AccessKeyInput{Name: str("cron"), AllowedModels: &[]string{"cron.x"}}); err != nil {
		t.Fatal(err)
	}
	cfg, err := a.ExportConfig(ctx)
	if err != nil || len(cfg.Providers) != 1 || len(cfg.Routes) != 1 || cfg.Routes[0].Price.PerCallMicro != 5 || cfg.Scenes[0].Fallbacks[0] != "backup" {
		t.Fatalf("export: %+v err=%v", cfg, err)
	}

	if _, err := a.UpdateRoute(ctx, r.ID, llm.RouteInput{Weight: intp(999)}); err != nil {
		t.Fatal(err)
	}
	db.Exec("DELETE FROM llm_scene WHERE key = 'cron.x'")
	preview, err := a.ImportConfig(ctx, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]string{}
	for _, c := range preview.Changes {
		actions[c.Kind+":"+c.Name] = c.Action
	}
	if actions["scene:cron.x"] != "create" || actions["route:chat → relay:up"] != "update" || actions["provider:relay"] != "unchanged" {
		t.Fatalf("preview: %v", actions)
	}
	var scenes int64
	db.Raw("SELECT count(*) FROM llm_scene WHERE key = 'cron.x'").Scan(&scenes)
	if scenes != 0 {
		t.Fatal("a preview writes nothing")
	}
	if _, err := a.ImportConfig(ctx, cfg, true); err != nil {
		t.Fatal(err)
	}
	again, _ := a.ImportConfig(ctx, cfg, false)
	for _, c := range again.Changes {
		if c.Action != "unchanged" {
			t.Fatalf("after applying, a second import changes nothing: %+v", c)
		}
	}
	routes, _ := a.Routes(ctx)
	if routes[0].Weight != 50 || routes[0].RPM != 60 {
		t.Fatalf("route restored: %+v", routes[0])
	}

	cfg.Providers = append(cfg.Providers, llm.ConfigProvider{Name: "fresh", BaseURL: "https://fresh.example/v1", Enabled: true})
	res, err := a.ImportConfig(ctx, cfg, true)
	if err != nil || len(res.Warnings) == 0 {
		t.Fatalf("a new provider arrives without a key and says so: %+v err=%v", res, err)
	}
}
