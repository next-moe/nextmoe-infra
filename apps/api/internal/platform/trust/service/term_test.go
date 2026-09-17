package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"api/internal/platform/trust/model"
)

func sp(s string) *string { return &s }

func mkTerm(t *testing.T, svc *TermService, site *string, raw string, kind int16) *model.TrustTerm {
	t.Helper()
	term, err := svc.Create(context.Background(), CreateTermParams{Site: site, Term: raw, Kind: kind})
	if err != nil {
		t.Fatalf("create term %q: %v", raw, err)
	}
	return term
}

func check(t *testing.T, svc *TermService, site, text string) CheckResult {
	t.Helper()
	res, err := svc.Check(context.Background(), CheckParams{Site: site, Text: text})
	if err != nil {
		t.Fatalf("check %q: %v", text, err)
	}
	return res
}

func TestTermMatcherDecisions(t *testing.T) {
	cleanTables(t)
	svc := NewTermService(testDB, nil)
	mkTerm(t, svc, nil, "banned", model.TermKindBanned)
	mkTerm(t, svc, nil, "suspect", model.TermKindSuspect)

	if r := check(t, svc, tSite, "clean content"); r.Decision != DecisionAllow || len(r.Matched) != 0 {
		t.Fatalf("clean → %+v, want allow/[]", r)
	}
	if r := check(t, svc, tSite, "has a suspect word"); r.Decision != DecisionHold {
		t.Fatalf("suspect-only → %s, want hold", r.Decision)
	}
	if r := check(t, svc, tSite, "has a banned word"); r.Decision != DecisionDeny {
		t.Fatalf("banned → %s, want deny", r.Decision)
	}
	r := check(t, svc, tSite, "both banned and suspect here")
	if r.Decision != DecisionDeny {
		t.Fatalf("banned+suspect → %s, want deny (deny wins)", r.Decision)
	}
	if len(r.Matched) != 2 {
		t.Fatalf("matched = %v, want both terms", r.Matched)
	}
}

func TestTermMatcherNormalizedMatch(t *testing.T) {
	cleanTables(t)
	svc := NewTermService(testDB, nil)
	mkTerm(t, svc, nil, "bad", model.TermKindBanned)

	if r := check(t, svc, tSite, "the ＢＡＤ thing"); r.Decision != DecisionDeny {
		t.Fatalf("fullwidth text → %s, want deny", r.Decision)
	}
	mkTerm(t, svc, nil, "坏词", model.TermKindBanned)
	zwsp := string(rune(0x200B))
	if r := check(t, svc, tSite, "含坏"+zwsp+"词的文本"); r.Decision != DecisionDeny {
		t.Fatalf("zero-width-split CJK → %s, want deny", r.Decision)
	}
}

func TestTermScoping(t *testing.T) {
	cleanTables(t)
	svc := NewTermService(testDB, nil)
	mkTerm(t, svc, nil, "globalbad", model.TermKindBanned)
	mkTerm(t, svc, sp("kungal"), "kungalbad", model.TermKindBanned)

	if r := check(t, svc, "otokun", "x globalbad y"); r.Decision != DecisionDeny {
		t.Fatalf("global term must fire for otokun, got %s", r.Decision)
	}
	if r := check(t, svc, "kungal", "x kungalbad y"); r.Decision != DecisionDeny {
		t.Fatalf("kungal term must fire for kungal, got %s", r.Decision)
	}
	if r := check(t, svc, "otokun", "x kungalbad y"); r.Decision != DecisionAllow {
		t.Fatalf("kungal term must NOT fire for otokun, got %s", r.Decision)
	}
}

func TestTermDeprecatedNotMatched(t *testing.T) {
	cleanTables(t)
	svc := NewTermService(testDB, nil)
	term := mkTerm(t, svc, nil, "retired", model.TermKindBanned)
	if r := check(t, svc, tSite, "x retired y"); r.Decision != DecisionDeny {
		t.Fatalf("active term must match, got %s", r.Decision)
	}
	if _, err := svc.Deprecate(context.Background(), 0, term.ID); err != nil {
		t.Fatalf("deprecate: %v", err)
	}
	if r := check(t, svc, tSite, "x retired y"); r.Decision != DecisionAllow {
		t.Fatalf("deprecated term must NOT match, got %s", r.Decision)
	}
}

func TestTermCacheInvalidatedOnMutation(t *testing.T) {
	cleanTables(t)
	svc := NewTermService(testDB, nil)
	if r := check(t, svc, tSite, "freshword"); r.Decision != DecisionAllow {
		t.Fatalf("pre-create must allow, got %s", r.Decision)
	}
	mkTerm(t, svc, nil, "freshword", model.TermKindBanned)
	if r := check(t, svc, tSite, "x freshword y"); r.Decision != DecisionDeny {
		t.Fatalf("create must take effect immediately, got %s", r.Decision)
	}
}

func TestTermCacheTTLRefresh(t *testing.T) {
	cleanTables(t)
	clock := time.Unix(1_700_000_000, 0)
	svc := NewTermService(testDB, nil)
	svc.now = func() time.Time { return clock }

	if r := check(t, svc, tSite, "ttlword"); r.Decision != DecisionAllow {
		t.Fatalf("t0 must allow, got %s", r.Decision)
	}
	if err := testDB.Create(&model.TrustTerm{TermNorm: "ttlword", Kind: model.TermKindBanned}).Error; err != nil {
		t.Fatalf("direct insert: %v", err)
	}
	clock = clock.Add(30 * time.Second)
	if r := check(t, svc, tSite, "x ttlword y"); r.Decision != DecisionAllow {
		t.Fatalf("within TTL must serve stale snapshot (allow), got %s", r.Decision)
	}
	clock = clock.Add(31 * time.Second)
	if r := check(t, svc, tSite, "x ttlword y"); r.Decision != DecisionDeny {
		t.Fatalf("past TTL must reload and deny, got %s", r.Decision)
	}
}

func TestTermCreateNormalizesAndStores(t *testing.T) {
	cleanTables(t)
	svc := NewTermService(testDB, nil)
	term := mkTerm(t, svc, nil, "ＢＡＤ", model.TermKindBanned)
	if term.TermNorm != "bad" {
		t.Fatalf("stored norm = %q, want bad", term.TermNorm)
	}
}

func TestTermCreateRejectsEmptyAndBadKind(t *testing.T) {
	cleanTables(t)
	svc := NewTermService(testDB, nil)
	if _, err := svc.Create(context.Background(), CreateTermParams{Term: "   ", Kind: model.TermKindBanned}); !errors.Is(err, ErrTermEmpty) {
		t.Fatalf("whitespace term → %v, want ErrTermEmpty", err)
	}
	if _, err := svc.Create(context.Background(), CreateTermParams{Term: "ok", Kind: 9}); !errors.Is(err, ErrTermInvalidKind) {
		t.Fatalf("bad kind → %v, want ErrTermInvalidKind", err)
	}
}

func TestTermCreateDuplicateConflict(t *testing.T) {
	cleanTables(t)
	svc := NewTermService(testDB, nil)
	first := mkTerm(t, svc, sp("kungal"), "dup", model.TermKindBanned)

	if _, err := svc.Create(context.Background(), CreateTermParams{Site: sp("kungal"), Term: "dup", Kind: model.TermKindSuspect}); !errors.Is(err, ErrTermExists) {
		t.Fatalf("duplicate active term → %v, want ErrTermExists", err)
	}
	if _, err := svc.Create(context.Background(), CreateTermParams{Term: "dup", Kind: model.TermKindBanned}); err != nil {
		t.Fatalf("global dup must be allowed (different keyspace): %v", err)
	}
	if _, err := svc.Deprecate(context.Background(), 0, first.ID); err != nil {
		t.Fatalf("deprecate: %v", err)
	}
	if _, err := svc.Create(context.Background(), CreateTermParams{Site: sp("kungal"), Term: "dup", Kind: model.TermKindBanned}); err != nil {
		t.Fatalf("re-create after deprecate must be allowed: %v", err)
	}
}

func TestTermListFilters(t *testing.T) {
	cleanTables(t)
	svc := NewTermService(testDB, nil)
	mkTerm(t, svc, nil, "g-suspect", model.TermKindSuspect)
	mkTerm(t, svc, sp("kungal"), "k-banned", model.TermKindBanned)
	dep := mkTerm(t, svc, sp("kungal"), "k-retired", model.TermKindSuspect)
	if _, err := svc.Deprecate(context.Background(), 0, dep.ID); err != nil {
		t.Fatalf("deprecate: %v", err)
	}

	active, total, err := svc.List(context.Background(), TermFilters{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(active) != 2 || total != 2 {
		t.Fatalf("active list = %d (total %d), want 2 (retired excluded)", len(active), total)
	}
	all, allTotal, _ := svc.List(context.Background(), TermFilters{IncludeDeprecated: true})
	if len(all) != 3 || allTotal != 3 {
		t.Fatalf("full list = %d (total %d), want 3", len(all), allTotal)
	}
	kungal, _, _ := svc.List(context.Background(), TermFilters{Site: "kungal", IncludeDeprecated: true})
	if len(kungal) != 2 {
		t.Fatalf("kungal list = %d, want 2", len(kungal))
	}
	banned := model.TermKindBanned
	onlyBanned, _, _ := svc.List(context.Background(), TermFilters{Kind: &banned, IncludeDeprecated: true})
	if len(onlyBanned) != 1 || onlyBanned[0].TermNorm != "k-banned" {
		t.Fatalf("banned-only list = %v, want [k-banned]", onlyBanned)
	}
}

func TestTermDeprecateNotFound(t *testing.T) {
	cleanTables(t)
	svc := NewTermService(testDB, nil)
	if _, err := svc.Deprecate(context.Background(), 0, 999999); !errors.Is(err, ErrTermNotFound) {
		t.Fatalf("deprecate absent → %v, want ErrTermNotFound", err)
	}
}

func TestCheckSiteBoundWhenNoWire(t *testing.T) {
	cleanTables(t)
	svc := NewTermService(testDB, allowFwd())
	mkTerm(t, svc, sp(tSite), "bound", model.TermKindBanned)

	res, err := svc.Check(context.Background(), CheckParams{
		CallerClientID: "not-a-forwarder", Site: tSite, WireSite: "", Text: "x bound y",
	})
	if err != nil {
		t.Fatalf("bound check: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Fatalf("bound-site check → %s, want deny", res.Decision)
	}
}

func TestCheckWireSiteRejectedForNonForwarder(t *testing.T) {
	cleanTables(t)
	svc := NewTermService(testDB, allowFwd())
	_, err := svc.Check(context.Background(), CheckParams{
		CallerClientID: "stranger", Site: tSite, WireSite: "letmoe", Text: "hi",
	})
	if !errors.Is(err, ErrForwarderNotAllowed) {
		t.Fatalf("wire site by stranger → %v, want ErrForwarderNotAllowed", err)
	}
}

func TestCheckWireSiteAcceptedForForwarder(t *testing.T) {
	cleanTables(t)
	const wireSite = "letmoe"
	svc := NewTermService(testDB, allowFwd())
	mkTerm(t, svc, sp(wireSite), "letmoebad", model.TermKindBanned)

	res, err := svc.Check(context.Background(), CheckParams{
		CallerClientID: fwdClient, Site: "ignored-bound", WireSite: wireSite, Text: "x letmoebad y",
	})
	if err != nil {
		t.Fatalf("forwarder check: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Fatalf("forwarder relay check → %s, want deny (wire-site term)", res.Decision)
	}
}

func TestCheckForwarderNoWireBinds(t *testing.T) {
	cleanTables(t)
	svc := NewTermService(testDB, allowFwd())
	mkTerm(t, svc, sp(tSite), "bound2", model.TermKindSuspect)

	res, err := svc.Check(context.Background(), CheckParams{
		CallerClientID: fwdClient, Site: tSite, WireSite: "", Text: "x bound2 y",
	})
	if err != nil {
		t.Fatalf("forwarder bound check: %v", err)
	}
	if res.Decision != DecisionHold {
		t.Fatalf("forwarder bound check → %s, want hold", res.Decision)
	}
}

func TestTermListPaging(t *testing.T) {
	cleanTables(t)
	svc := NewTermService(testDB, nil)
	for i := range 7 {
		mkTerm(t, svc, nil, fmt.Sprintf("page-term-%d", i), model.TermKindSuspect)
	}
	mkTerm(t, svc, nil, "unrelated", model.TermKindSuspect)

	first, total, err := svc.List(context.Background(), TermFilters{Limit: 3})
	if err != nil {
		t.Fatalf("list page 1: %v", err)
	}
	if total != 8 {
		t.Fatalf("total = %d, want 8 (the count ignores the page window)", total)
	}
	if len(first) != 3 {
		t.Fatalf("page 1 = %d terms, want 3", len(first))
	}

	second, _, _ := svc.List(context.Background(), TermFilters{Limit: 3, Page: 2})
	third, _, _ := svc.List(context.Background(), TermFilters{Limit: 3, Page: 3})
	seen := map[int64]bool{}
	for _, page := range [][]model.TrustTerm{first, second, third} {
		for _, term := range page {
			if seen[term.ID] {
				t.Fatalf("term %d (%s) appeared on two pages", term.ID, term.TermNorm)
			}
			seen[term.ID] = true
		}
	}
	if len(seen) != 8 {
		t.Fatalf("three pages covered %d distinct terms, want all 8", len(seen))
	}

	found, foundTotal, _ := svc.List(context.Background(), TermFilters{Query: "page-term"})
	if foundTotal != 7 || len(found) != 7 {
		t.Fatalf("search = %d terms (total %d), want 7", len(found), foundTotal)
	}

	mkTerm(t, svc, nil, "SPAM", model.TermKindBanned)
	wide, wideTotal, _ := svc.List(context.Background(), TermFilters{Query: "ＳＰＡ"})
	if wideTotal != 1 || len(wide) != 1 || wide[0].TermNorm != "spam" {
		t.Fatalf("normalized search = %v (total %d), want [spam]", wide, wideTotal)
	}

	_, pctTotal, _ := svc.List(context.Background(), TermFilters{Query: "%"})
	if pctTotal != 0 {
		t.Fatalf("searching %q matched %d terms; wildcards must be escaped", "%", pctTotal)
	}
}
