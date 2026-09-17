package handler

import (
	"net/http"
	"testing"

	"api/internal/platform/trust/dto"
	"api/internal/platform/trust/service"
	"api/internal/testsupport/dbtest"
)

func TestAdminTermsPermGate(t *testing.T) {
	s := &AdminServer{}

	for _, roles := range [][]string{{"admin"}, {"ren"}} {
		ctx := scopedCtx(roles, "", 1)
		if he := s.requireTermManage(ctx); he != nil {
			t.Fatalf("global %v must pass term_manage, got %v", roles, he)
		}
	}
	if got := statusOf(s.requireTermManage(scopedCtx([]string{"moderator"}, "", 1))); got != http.StatusForbidden {
		t.Fatalf("moderator must be 403 on term_manage, got %d", got)
	}
	if got := statusOf(s.requireTermManage(scopedCtx(nil, "site-client", 1))); got != http.StatusForbidden {
		t.Fatalf("site-scoped caller must be 403 on term_manage, got %d", got)
	}
}

func TestAdminTermsHandlersRejectModerator(t *testing.T) {
	s := &AdminServer{}
	ctx := scopedCtx([]string{"moderator"}, "", 1)

	checks := map[string]func() error{
		"listTerms":     func() error { _, e := s.listTerms(ctx, &listTermsInput{Kind: -1}); return e },
		"createTerm":    func() error { _, e := s.createTerm(ctx, &createTermInput{}); return e },
		"deprecateTerm": func() error { _, e := s.deprecateTerm(ctx, &deprecateTermInput{ID: 1}); return e },
	}
	for name, fn := range checks {
		if got := statusOf(fn()); got != http.StatusForbidden {
			t.Errorf("%s as moderator: want 403, got %d", name, got)
		}
	}
}

func TestAdminTermsCRUD(t *testing.T) {
	if testDB == nil {
		dbtest.Skipf(t, "the trust test database is unavailable")
	}
	if err := testDB.Exec("TRUNCATE trust_term, trust_audit_log RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatalf("truncate: %v", err)
	}
	s := &AdminServer{terms: service.NewTermService(testDB, nil)}
	admin := scopedCtx([]string{"admin"}, "", 7)

	out, err := s.createTerm(admin, &createTermInput{Body: dto.CreateTermRequest{Term: "ＢＡＤ", Kind: 1}})
	if err != nil {
		t.Fatalf("admin create term: %v", err)
	}
	if out.Body.Data.TermNorm != "bad" {
		t.Fatalf("stored norm = %q, want bad", out.Body.Data.TermNorm)
	}
	id := out.Body.Data.ID

	list, err := s.listTerms(admin, &listTermsInput{Kind: -1})
	if err != nil {
		t.Fatalf("list terms: %v", err)
	}
	if len(list.Body.Data.Terms) != 1 {
		t.Fatalf("active list = %d, want 1", len(list.Body.Data.Terms))
	}
	if list.Body.Data.Total != 1 {
		t.Fatalf("list total = %d, want 1", list.Body.Data.Total)
	}
	miss, err := s.listTerms(admin, &listTermsInput{Kind: -1, Q: "nothing-matches-this"})
	if err != nil {
		t.Fatalf("list terms (search): %v", err)
	}
	if len(miss.Body.Data.Terms) != 0 || miss.Body.Data.Total != 0 {
		t.Fatalf("search miss = %d terms (total %d), want 0", len(miss.Body.Data.Terms), miss.Body.Data.Total)
	}

	if _, err := s.deprecateTerm(admin, &deprecateTermInput{ID: id}); err != nil {
		t.Fatalf("deprecate term: %v", err)
	}
	active, _ := s.listTerms(admin, &listTermsInput{Kind: -1})
	if len(active.Body.Data.Terms) != 0 {
		t.Fatalf("post-deprecate active list = %d, want 0", len(active.Body.Data.Terms))
	}
	withDep, _ := s.listTerms(admin, &listTermsInput{Kind: -1, IncludeDeprecated: true})
	if len(withDep.Body.Data.Terms) != 1 || !withDep.Body.Data.Terms[0].IsDeprecated {
		t.Fatalf("include_deprecated must show the retired term")
	}
}
