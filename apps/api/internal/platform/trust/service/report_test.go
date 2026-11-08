package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"api/internal/platform/settings/keys"
	"api/internal/platform/trust/model"
)

const (
	tSite = "kungal"
	tKind = "forum_topic"
	tSubj = "t123"
)

func newReportSvc(w Weigher) *ReportService { return NewReportService(testDB, w) }

func submit(t *testing.T, svc *ReportService, reporterID int64, subject string) ReportResult {
	t.Helper()
	res, err := svc.Submit(context.Background(), ReportParams{
		Site: tSite, SubjectKind: tKind, SubjectID: subject, ReasonKey: "abuse", ReporterID: reporterID,
	})
	if err != nil {
		t.Fatalf("submit(reporter=%d, subj=%s): %v", reporterID, subject, err)
	}
	return res
}

func TestDedup(t *testing.T) {
	cleanTables(t)
	registerKind(t, tSite, tKind, nil, nil)
	svc := newReportSvc(newWeigher())

	first := submit(t, svc, 1, tSubj)
	second := submit(t, svc, 1, tSubj)

	if first.ReportID != second.ReportID {
		t.Fatalf("dedup should return the same report id: %d vs %d", first.ReportID, second.ReportID)
	}
	if n := countReports(t, tSite, tKind, tSubj); n != 1 {
		t.Fatalf("dedup should leave exactly one report row, got %d", n)
	}
}

func TestAggregationThresholds(t *testing.T) {
	cleanTables(t)
	registerKind(t, tSite, tKind, nil, nil)
	svc := newReportSvc(newWeigher())

	submit(t, svc, 1, tSubj)
	submit(t, svc, 2, tSubj)
	if n := countOpenItems(t, tSite, tKind, tSubj); n != 0 {
		t.Fatalf("2×1.0 must not open an item (sum 2.0 < 3.0), got %d", n)
	}
	third := submit(t, svc, 3, tSubj)
	if n := countOpenItems(t, tSite, tKind, tSubj); n != 1 {
		t.Fatalf("3×1.0 must open exactly one item, got %d", n)
	}
	if third.ReviewItemID == nil {
		t.Fatal("third report should carry the opened review item id")
	}

	w := newWeigher()
	w.set(10, ReporterWeight{Weight: 0.5})
	w.set(11, ReporterWeight{Weight: 0.5})
	svc2 := newReportSvc(w)
	submit(t, svc2, 10, "new1")
	submit(t, svc2, 11, "new1")
	if n := countOpenItems(t, tSite, tKind, "new1"); n != 0 {
		t.Fatalf("2×0.5 new accounts must not open an item, got %d", n)
	}

	ws := newWeigher()
	ws.set(20, ReporterWeight{Weight: 1.0, Staff: true})
	svc3 := newReportSvc(ws)
	res := submit(t, svc3, 20, "staffsubj")
	if n := countOpenItems(t, tSite, tKind, "staffsubj"); n != 1 {
		t.Fatalf("staff single report must open an item, got %d", n)
	}
	if res.ReviewItemID == nil {
		t.Fatal("staff report should carry the opened review item id")
	}
}

func TestOneOpenItemAndConcurrency(t *testing.T) {
	cleanTables(t)
	registerKind(t, tSite, tKind, nil, nil)

	ws := newWeigher()
	ws.set(1, ReporterWeight{Weight: 1.0, Staff: true})
	svc := newReportSvc(ws)
	first := submit(t, svc, 1, tSubj)
	second := submit(t, svc, 2, tSubj)
	if second.ReviewItemID == nil || *second.ReviewItemID != *first.ReviewItemID {
		t.Fatalf("second report must link to the same open item")
	}
	if n := countOpenItems(t, tSite, tKind, tSubj); n != 1 {
		t.Fatalf("still exactly one open item, got %d", n)
	}

	wc := newWeigher()
	wc.set(101, ReporterWeight{Weight: 1.0, Staff: true})
	wc.set(102, ReporterWeight{Weight: 1.0, Staff: true})
	svcC := newReportSvc(wc)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, rid := range []int64{101, 102} {
		wg.Add(1)
		go func(idx int, reporter int64) {
			defer wg.Done()
			_, errs[idx] = svcC.Submit(context.Background(), ReportParams{
				Site: tSite, SubjectKind: tKind, SubjectID: "race", ReasonKey: "abuse", ReporterID: reporter,
			})
		}(i, rid)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent submit %d failed: %v", i, err)
		}
	}
	if n := countOpenItems(t, tSite, tKind, "race"); n != 1 {
		t.Fatalf("two concurrent staff reports must open exactly one item, got %d", n)
	}
	if n := countReports(t, tSite, tKind, "race"); n != 2 {
		t.Fatalf("both reports should persist, got %d", n)
	}
}

func TestFoldAfterDismiss(t *testing.T) {
	cleanTables(t)
	registerKind(t, tSite, tKind, nil, nil)
	ws := newWeigher()
	ws.set(1, ReporterWeight{Weight: 1.0, Staff: true})
	svc := newReportSvc(ws)

	first := submit(t, svc, 1, tSubj)
	itemID := *first.ReviewItemID

	if _, err := NewReviewService(testDB).Decide(context.Background(), DecideParams{
		ID: itemID, DecidedBy: 999, Decision: "dismissed",
	}); err != nil {
		t.Fatalf("dismiss: %v", err)
	}

	res, err := svc.Submit(context.Background(), ReportParams{
		Site: tSite, SubjectKind: tKind, SubjectID: tSubj, ReasonKey: "abuse", ReporterID: 2,
	})
	if err != nil {
		t.Fatalf("fold submit: %v", err)
	}
	if res.ReviewItemID == nil || *res.ReviewItemID != itemID {
		t.Fatalf("folded report should point at the dismissed item %d, got %v", itemID, res.ReviewItemID)
	}
	var status int16
	testDB.Raw(`SELECT status FROM trust_report WHERE id = ?`, res.ReportID).Scan(&status)
	if status != model.ReportStatusFolded {
		t.Fatalf("folded report status = %d, want %d", status, model.ReportStatusFolded)
	}
	if n := countOpenItems(t, tSite, tKind, tSubj); n != 0 {
		t.Fatalf("fold must not reopen an item, got %d open", n)
	}
}

func TestTenantFailLoud(t *testing.T) {
	cleanTables(t)
	registerKind(t, "siteA", tKind, nil, nil)
	svc := newReportSvc(newWeigher())

	_, err := svc.Submit(context.Background(), ReportParams{
		Site: "siteA", SubjectKind: "unknown_kind", SubjectID: "x", ReasonKey: "abuse", ReporterID: 1,
	})
	if !errors.Is(err, ErrSubjectKindNotRegistered) {
		t.Fatalf("unregistered kind: want ErrSubjectKindNotRegistered, got %v", err)
	}

	_, err = svc.Submit(context.Background(), ReportParams{
		Site: "siteB", SubjectKind: tKind, SubjectID: "x", ReasonKey: "abuse", ReporterID: 1,
	})
	if !errors.Is(err, ErrSubjectKindNotRegistered) {
		t.Fatalf("cross-tenant kind: want ErrSubjectKindNotRegistered, got %v", err)
	}
}

func TestReportWireSiteIsForwarderOnly(t *testing.T) {
	cleanTables(t)
	registerKind(t, "nextmoe", "user_folder", nil, nil)
	svc := NewReportService(testDB, newWeigher(), WithReportForwarders(map[string]bool{"catalog-fwd": true}))
	params := ReportParams{
		CallerClientID: "stranger", WireSite: "nextmoe",
		SubjectKind: "user_folder", SubjectID: "42", ReasonKey: "spam", ReporterID: 1,
	}
	if _, err := svc.Submit(context.Background(), params); !errors.Is(err, ErrForwarderNotAllowed) {
		t.Fatalf("a client off the allowlist must not name a site: %v", err)
	}
	params.CallerClientID = "catalog-fwd"
	if _, err := svc.Submit(context.Background(), params); err != nil {
		t.Fatalf("an allowlisted forwarder files under the wire site: %v", err)
	}
	if n := countReports(t, "nextmoe", "user_folder", "42"); n != 1 {
		t.Fatalf("the report must land on the wire site, got %d rows", n)
	}
}

func TestRateLimit(t *testing.T) {
	cleanTables(t)
	registerKind(t, tSite, tKind, nil, nil)
	svc := newReportSvc(newWeigher())

	max := int(keys.TrustReportRateMaxPerWindow.Get())
	for i := 0; i < max; i++ {
		submit(t, svc, 1, fmt.Sprintf("s%d", i))
	}
	_, err := svc.Submit(context.Background(), ReportParams{
		Site: tSite, SubjectKind: tKind, SubjectID: "over", ReasonKey: "abuse", ReporterID: 1,
	})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("report %d should be rate-limited, got %v", max+1, err)
	}
}

func TestSubjectURL(t *testing.T) {
	if testDB == nil {
		t.Skip("TEST_DATABASE_DSN not set")
	}
	cleanTables(t)
	registerKind(t, "site-a", "thing", nil, nil)
	svc := NewReportService(testDB, newWeigher())
	ctx := context.Background()

	link := "https://www.kungal.com/topic/123?reply=45"
	res, err := svc.Submit(ctx, ReportParams{
		Site: "site-a", SubjectKind: "thing", SubjectID: "1",
		ReasonKey: "abuse", ReporterID: 42, SubjectURL: &link,
	})
	if err != nil {
		t.Fatalf("submit with url: %v", err)
	}
	var got model.TrustReport
	if err := testDB.Take(&got, res.ReportID).Error; err != nil {
		t.Fatalf("read report: %v", err)
	}
	if got.SubjectURL == nil || *got.SubjectURL != link {
		t.Fatalf("subject_url not persisted: %v", got.SubjectURL)
	}

	bad := "javascript:alert(1)"
	if _, err := svc.Submit(ctx, ReportParams{
		Site: "site-a", SubjectKind: "thing", SubjectID: "2",
		ReasonKey: "abuse", ReporterID: 42, SubjectURL: &bad,
	}); !errors.Is(err, ErrInvalidSubjectURL) {
		t.Fatalf("non-http scheme must fail loud, got %v", err)
	}

	long := "https://www.kungal.com/topic/1?x=" + strings.Repeat("a", 512)
	if _, err := svc.Submit(ctx, ReportParams{
		Site: "site-a", SubjectKind: "thing", SubjectID: "3",
		ReasonKey: "abuse", ReporterID: 42, SubjectURL: &long,
	}); !errors.Is(err, ErrInvalidSubjectURL) {
		t.Fatalf("overlong url must fail loud, got %v", err)
	}
}
