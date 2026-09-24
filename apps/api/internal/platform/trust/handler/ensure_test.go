package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	siteModel "api/internal/platform/site/model"
	"api/internal/platform/trust/dto"
	"api/internal/platform/trust/service"
	"api/internal/testsupport/dbtest"
)

func s2sCtx(site string) context.Context {
	return context.WithValue(context.Background(), ctxKeyClient, &siteModel.OAuthClient{CatalogSite: site})
}

func TestSiteBindingPrefersCommunitySite(t *testing.T) {
	cases := []struct {
		client *siteModel.OAuthClient
		want   string
	}{
		{&siteModel.OAuthClient{CatalogSite: "kungal", CommunitySite: "moyu"}, "moyu"},
		{&siteModel.OAuthClient{CatalogSite: "kungal"}, "kungal"},
	}
	for _, c := range cases {
		got, he := siteBinding(context.WithValue(context.Background(), ctxKeyClient, c.client))
		if he != nil || got != c.want {
			t.Fatalf("catalog_site=%q community_site=%q: want site %q, got %q (err %v)",
				c.client.CatalogSite, c.client.CommunitySite, c.want, got, he)
		}
	}
	if _, he := siteBinding(context.WithValue(context.Background(), ctxKeyClient, &siteModel.OAuthClient{})); statusOf(he) != http.StatusForbidden {
		t.Fatalf("unbound client must be 403, got %v", he)
	}
}

func overCapItems() []dto.EnsureSubjectKindItem {
	items := make([]dto.EnsureSubjectKindItem, maxEnsureKinds+1)
	for i := range items {
		items[i] = dto.EnsureSubjectKindItem{Key: fmt.Sprintf("k%d", i)}
	}
	return items
}

func TestEnsureSubjectKindsGuards(t *testing.T) {
	s := &Server{registry: service.NewRegistryService(testDB)}

	if got := statusOf(mustErr(s.ensureSubjectKinds(context.Background(), &ensureSubjectKindsInput{}))); got != http.StatusForbidden {
		t.Fatalf("unbound ensure: want 403, got %d", got)
	}

	over := &ensureSubjectKindsInput{Body: dto.EnsureSubjectKindsRequest{Kinds: overCapItems()}}
	if got := statusOf(mustErr(s.ensureSubjectKinds(s2sCtx("site-a"), over))); got != http.StatusUnprocessableEntity {
		t.Fatalf("over-cap ensure: want 422, got %d", got)
	}

	out, err := s.ensureSubjectKinds(s2sCtx("site-a"), &ensureSubjectKindsInput{})
	if err != nil {
		t.Fatalf("empty ensure: %v", err)
	}
	if len(out.Body.Data.Results) != 0 {
		t.Fatalf("empty ensure should return no results, got %d", len(out.Body.Data.Results))
	}
}

func TestEnsureSubjectKindsHandler(t *testing.T) {
	if testDB == nil {
		dbtest.Skipf(t, "the trust test database is unavailable")
	}
	truncateRegistry(t)
	s := &Server{registry: service.NewRegistryService(testDB)}
	ctx := s2sCtx("site-h")

	cb := "https://h.example/cb"
	in := &ensureSubjectKindsInput{Body: dto.EnsureSubjectKindsRequest{Kinds: []dto.EnsureSubjectKindItem{
		{Key: "topic"},
		{Key: "reply", CallbackURL: &cb},
		{Key: "resource"},
	}}}
	out, err := s.ensureSubjectKinds(ctx, in)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	got := out.Body.Data.Results
	if len(got) != 3 || got[0].Key != "topic" || got[1].Key != "reply" || got[2].Key != "resource" {
		t.Fatalf("results out of request order: %+v", got)
	}
	for i, r := range got {
		if r.Result != "created" {
			t.Fatalf("result[%d] = %q, want created", i, r.Result)
		}
	}
	var site string
	testDB.Raw(`SELECT site FROM trust_subject_kind WHERE key = 'reply'`).Scan(&site)
	if site != "site-h" {
		t.Fatalf("kind landed under %q, want the bound site-h", site)
	}
	var actor *int64
	testDB.Raw(`SELECT actor_id FROM trust_audit_log WHERE action='subject_kind_created' ORDER BY id DESC LIMIT 1`).Scan(&actor)
	if actor != nil {
		t.Fatalf("S2S ensure should audit a nil actor, got %d", *actor)
	}

	out2, err := s.ensureSubjectKinds(ctx, in)
	if err != nil {
		t.Fatalf("ensure round 2: %v", err)
	}
	for i, r := range out2.Body.Data.Results {
		if r.Result != "unchanged" {
			t.Fatalf("round2 result[%d] = %q, want unchanged", i, r.Result)
		}
	}
}

func TestBatchSubjectKindsPermissionGolden(t *testing.T) {
	s := &AdminServer{
		registry: service.NewRegistryService(testDB),
		clients: &fakeClients{byID: map[string]*siteModel.OAuthClient{
			"site-client": {CatalogSite: "otokun"},
		}},
	}
	scoped := scopedCtx(nil, "site-client", 1)
	staff := scopedCtx([]string{"admin"}, "", 42)

	scopedIn := &batchSubjectKindsInput{Body: dto.BatchSubjectKindsRequest{Site: "otokun", Kinds: []dto.EnsureSubjectKindItem{{Key: "x"}}}}
	if got := statusOf(mustErr(s.batchSubjectKinds(scoped, scopedIn))); got != http.StatusForbidden {
		t.Fatalf("site-scoped batch: want 403, got %d", got)
	}

	if got := statusOf(mustErr(s.batchSubjectKinds(staff, &batchSubjectKindsInput{Body: dto.BatchSubjectKindsRequest{Site: ""}}))); got != http.StatusUnprocessableEntity {
		t.Fatalf("missing-site batch: want 422, got %d", got)
	}

	overIn := &batchSubjectKindsInput{Body: dto.BatchSubjectKindsRequest{Site: "otokun", Kinds: overCapItems()}}
	if got := statusOf(mustErr(s.batchSubjectKinds(staff, overIn))); got != http.StatusUnprocessableEntity {
		t.Fatalf("over-cap batch: want 422, got %d", got)
	}
}

func TestBatchSubjectKindsConvergence(t *testing.T) {
	if testDB == nil {
		dbtest.Skipf(t, "the trust test database is unavailable")
	}
	truncateRegistry(t)
	s := &AdminServer{registry: service.NewRegistryService(testDB), clients: &fakeClients{}}
	staff := scopedCtx([]string{"admin"}, "", 42)

	cb := "https://x.example/cb"
	in := &batchSubjectKindsInput{Body: dto.BatchSubjectKindsRequest{Site: "explicit-site", Kinds: []dto.EnsureSubjectKindItem{
		{Key: "k1"},
		{Key: "k2", CallbackURL: &cb},
	}}}
	out, err := s.batchSubjectKinds(staff, in)
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	res := out.Body.Data.Results
	if len(res) != 2 || res[0].Result != "created" || res[1].Result != "created" {
		t.Fatalf("batch convergence: %+v", res)
	}

	var n int64
	testDB.Model(&struct{}{}).Table("trust_subject_kind").Where("site = ?", "explicit-site").Count(&n)
	if n != 2 {
		t.Fatalf("explicit-site kinds = %d, want 2", n)
	}
	var actor *int64
	testDB.Raw(`SELECT actor_id FROM trust_audit_log WHERE action='subject_kind_created' ORDER BY id DESC LIMIT 1`).Scan(&actor)
	if actor == nil || *actor != 42 {
		t.Fatalf("admin batch must audit the operator (42), got %v", actor)
	}

	out2, _ := s.batchSubjectKinds(staff, in)
	for i, r := range out2.Body.Data.Results {
		if r.Result != "unchanged" {
			t.Fatalf("round2 result[%d] = %q, want unchanged", i, r.Result)
		}
	}
}

func truncateRegistry(t *testing.T) {
	t.Helper()
	if err := testDB.Exec("TRUNCATE trust_subject_kind, trust_audit_log RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatalf("truncate registry: %v", err)
	}
}
