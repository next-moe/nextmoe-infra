package service

import (
	"context"
	"testing"

	"api/internal/platform/trust/model"
)

func submitBy(t *testing.T, svc *ReportService, reporterID int64, subject string, author *int64) ReportResult {
	t.Helper()
	res, err := svc.Submit(context.Background(), ReportParams{
		Site: tSite, SubjectKind: tKind, SubjectID: subject, ReasonKey: "abuse",
		ReporterID: reporterID, AuthorID: author,
	})
	if err != nil {
		t.Fatalf("submit(reporter=%d, subj=%s): %v", reporterID, subject, err)
	}
	return res
}

func wantAuthor(t *testing.T, itemID int64, want *int64) {
	t.Helper()
	got := getItem(t, itemID).SubjectAuthorID
	switch {
	case want == nil && got != nil:
		t.Fatalf("item %d subject_author_id = %d, want NULL", itemID, *got)
	case want != nil && (got == nil || *got != *want):
		t.Fatalf("item %d subject_author_id = %v, want %d", itemID, got, *want)
	}
}

func TestReportAuthorOpensItemFromEarliestNamedReport(t *testing.T) {
	cleanTables(t)
	registerKind(t, tSite, tKind, nil, nil)
	svc := newReportSvc(newWeigher())

	submitBy(t, svc, 1, "a1", nil)
	submitBy(t, svc, 2, "a1", i64(700))
	res := submitBy(t, svc, 3, "a1", i64(701))
	if res.ReviewItemID == nil {
		t.Fatal("the third report should open the item")
	}
	wantAuthor(t, *res.ReviewItemID, i64(700))

	var stored model.TrustReport
	if err := testDB.Where("subject_id = ? AND reporter_id = ?", "a1", 3).Take(&stored).Error; err != nil {
		t.Fatalf("reload report: %v", err)
	}
	if stored.SubjectAuthorID == nil || *stored.SubjectAuthorID != 701 {
		t.Fatalf("report keeps the author it was submitted with: got %v, want 701", stored.SubjectAuthorID)
	}
}

func TestReportAuthorFillsButNeverOverwritesAnOpenItem(t *testing.T) {
	cleanTables(t)
	registerKind(t, tSite, tKind, nil, nil)
	w := newWeigher()
	w.set(1, ReporterWeight{Weight: 1.0, Staff: true})
	svc := newReportSvc(w)

	opened := submitBy(t, svc, 1, "a2", nil)
	if opened.ReviewItemID == nil {
		t.Fatal("a staff report should open the item")
	}
	wantAuthor(t, *opened.ReviewItemID, nil)

	submitBy(t, svc, 2, "a2", i64(800))
	wantAuthor(t, *opened.ReviewItemID, i64(800))

	submitBy(t, svc, 3, "a2", i64(801))
	wantAuthor(t, *opened.ReviewItemID, i64(800))
}

func TestReportAuthorFoldsIntoADismissedItem(t *testing.T) {
	cleanTables(t)
	registerKind(t, tSite, tKind, nil, nil)
	w := newWeigher()
	w.set(1, ReporterWeight{Weight: 1.0, Staff: true})
	svc := newReportSvc(w)

	opened := submitBy(t, svc, 1, "a3", nil)
	if _, err := NewReviewService(testDB).Decide(context.Background(), DecideParams{
		ID: *opened.ReviewItemID, DecidedBy: 9, Decision: "dismissed",
	}); err != nil {
		t.Fatalf("dismiss: %v", err)
	}

	folded := submitBy(t, svc, 2, "a3", i64(900))
	if folded.ReviewItemID == nil || *folded.ReviewItemID != *opened.ReviewItemID {
		t.Fatalf("a report inside the fold window should fold into item %d, got %v", *opened.ReviewItemID, folded.ReviewItemID)
	}
	wantAuthor(t, *opened.ReviewItemID, i64(900))
}

func TestScanAuthorOpensAndFillsItems(t *testing.T) {
	cleanTables(t)
	registerKind(t, tSite, tKind, nil, nil)

	seed := func(subject string, author *int64) {
		t.Helper()
		r := model.TrustScanResult{
			Site: tSite, SubjectKind: tKind, SubjectID: subject, AuthorID: author,
			ContentText: "flag me", Status: model.ScanStatusPending, Mode: model.ScanModeShadow,
		}
		if err := testDB.Create(&r).Error; err != nil {
			t.Fatalf("seed scan: %v", err)
		}
	}

	seed("s1", i64(1001))
	existing := mkOpenItem(t, "s2")
	seed("s2", i64(1002))
	if _, err := liveWorker(0.9).ScorePending(context.Background()); err != nil {
		t.Fatalf("score pending: %v", err)
	}

	var opened model.TrustReviewItem
	if err := testDB.Where("subject_id = ?", "s1").Take(&opened).Error; err != nil {
		t.Fatalf("scan should open an item: %v", err)
	}
	wantAuthor(t, opened.ID, i64(1001))
	wantAuthor(t, existing, i64(1002))
}

func TestForwardAuthorOpensAndFillsItems(t *testing.T) {
	cleanTables(t)
	registerKind(t, tSite, "community_post", nil, nil)
	svc := NewForwardService(testDB, allowFwd())

	p := fwdParams("f1")
	p.AuthorID = i64(1100)
	res, err := svc.Forward(context.Background(), p)
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	wantAuthor(t, res.ReviewItemID, i64(1100))

	p.AuthorID = i64(1101)
	if _, err := svc.Forward(context.Background(), p); err != nil {
		t.Fatalf("re-forward: %v", err)
	}
	wantAuthor(t, res.ReviewItemID, i64(1100))

	anon := fwdParams("f2")
	res2, err := svc.Forward(context.Background(), anon)
	if err != nil {
		t.Fatalf("forward without author: %v", err)
	}
	wantAuthor(t, res2.ReviewItemID, nil)
	anon.AuthorID = i64(1102)
	if _, err := svc.Forward(context.Background(), anon); err != nil {
		t.Fatalf("re-forward with author: %v", err)
	}
	wantAuthor(t, res2.ReviewItemID, i64(1102))
}

func TestListFiltersBySubjectAuthorWithinSite(t *testing.T) {
	cleanTables(t)
	mk := func(site, subject string, author *int64, status int16) {
		t.Helper()
		it := model.TrustReviewItem{
			Site: site, SubjectKind: tKind, SubjectID: subject, SubjectAuthorID: author,
			Source: model.ReviewSourceReports, Priority: 1, Status: status,
		}
		if err := testDB.Create(&it).Error; err != nil {
			t.Fatalf("insert item: %v", err)
		}
	}
	mk("kungal", "h1", i64(42), model.ReviewStatusActioned)
	mk("kungal", "h2", i64(42), model.ReviewStatusActioned)
	mk("kungal", "h3", i64(42), model.ReviewStatusDismissed)
	mk("kungal", "h4", i64(43), model.ReviewStatusActioned)
	mk("kungal", "h5", nil, model.ReviewStatusActioned)
	mk("moyu", "h6", i64(42), model.ReviewStatusActioned)

	svc := NewReviewService(testDB)
	count := func(site string, status int16) int64 {
		t.Helper()
		items, total, err := svc.List(context.Background(), ReviewFilters{
			Site: site, Status: &status, SubjectAuthorID: 42, Limit: 1,
		})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, it := range items {
			if it.SubjectAuthorID == nil || *it.SubjectAuthorID != 42 || (site != "" && it.Site != site) {
				t.Fatalf("filter leaked item %d (site %s, author %v)", it.ID, it.Site, it.SubjectAuthorID)
			}
		}
		return total
	}
	if got := count("kungal", model.ReviewStatusActioned); got != 2 {
		t.Fatalf("kungal actioned for author 42 = %d, want 2", got)
	}
	if got := count("kungal", model.ReviewStatusDismissed); got != 1 {
		t.Fatalf("kungal dismissed for author 42 = %d, want 1", got)
	}
	if got := count("", model.ReviewStatusActioned); got != 3 {
		t.Fatalf("all-site actioned for author 42 = %d, want 3", got)
	}
}
