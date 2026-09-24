package handler

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"

	siteModel "api/internal/platform/site/model"
	suitelock "api/internal/platform/trust/dbtest"
	"api/internal/platform/trust/dto"
	"api/internal/platform/trust/migrate"
	"api/internal/platform/trust/model"
	"api/internal/platform/trust/service"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	release := func() {}
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("trust/handler")
	}
	if db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}); err == nil {
		sqlDB, _ := db.DB()
		release = suitelock.AcquireSuiteLock(sqlDB)
		if merr := migrate.Run(db); merr != nil {
			fmt.Fprintf(os.Stderr, "SKIP DB tests: trust migration failed: %v\n", merr)
		} else {
			testDB = db
		}
	} else {
		fmt.Fprintf(os.Stderr, "SKIP DB tests: cannot connect to test database: %v\n", err)
	}
	code := m.Run()
	release()
	os.Exit(code)
}

type fakeClients struct {
	byID map[string]*siteModel.OAuthClient
}

func (f *fakeClients) FindByClientID(_ context.Context, id string) (*siteModel.OAuthClient, error) {
	if c, ok := f.byID[id]; ok {
		return c, nil
	}
	return nil, nil
}

func scopedCtx(globalRoles []string, clientID string, adminID int64) context.Context {
	ctx := context.Background()
	ctx = context.WithValue(ctx, ctxKeyGlobalRoles, globalRoles)
	ctx = context.WithValue(ctx, ctxKeyClientID, clientID)
	ctx = context.WithValue(ctx, ctxKeyAdminID, adminID)
	return ctx
}

func statusOf(err error) int {
	if he, ok := err.(*houseError); ok {
		return he.GetStatus()
	}
	return 0
}

func TestAdminResolveScope(t *testing.T) {
	s := &AdminServer{clients: &fakeClients{byID: map[string]*siteModel.OAuthClient{
		"site-client":    {CatalogSite: "otokun"},
		"moyu-client":    {CatalogSite: "kungal", CommunitySite: "moyu"},
		"unbound-client": {},
	}}}

	for _, roles := range [][]string{{"moderator"}, {"admin"}, {"ren"}} {
		sc, he := s.resolveScope(scopedCtx(roles, "site-client", 1))
		if he != nil || sc.siteScoped() {
			t.Fatalf("global %v must be unrestricted: scope=%+v err=%v", roles, sc, he)
		}
	}

	sc, he := s.resolveScope(scopedCtx(nil, "site-client", 1))
	if he != nil || sc.site != "otokun" {
		t.Fatalf("site-only caller must be scoped to otokun: scope=%+v err=%v", sc, he)
	}

	sc, he = s.resolveScope(scopedCtx(nil, "moyu-client", 1))
	if he != nil || sc.site != "moyu" {
		t.Fatalf("a client with its own community must be scoped to it, not its catalog site: scope=%+v err=%v", sc, he)
	}

	for _, clientID := range []string{"unbound-client", "ghost", ""} {
		if _, he := s.resolveScope(scopedCtx(nil, clientID, 1)); statusOf(he) != http.StatusForbidden {
			t.Fatalf("client %q must be 403, got %v", clientID, he)
		}
	}
}

func TestAdminRegistriesPlatformOnly(t *testing.T) {
	s := &AdminServer{clients: &fakeClients{byID: map[string]*siteModel.OAuthClient{
		"site-client": {CatalogSite: "otokun"},
	}}}
	ctx := scopedCtx(nil, "site-client", 1)

	checks := map[string]func() error{
		"listSubjectKinds":  func() error { _, e := s.listSubjectKinds(ctx, &listSubjectKindsAdminInput{}); return e },
		"createSubjectKind": func() error { _, e := s.createSubjectKind(ctx, &createSubjectKindInput{}); return e },
		"patchSubjectKind":  func() error { _, e := s.patchSubjectKind(ctx, &patchSubjectKindInput{}); return e },
		"listReasons":       func() error { _, e := s.listReasons(ctx, &listReasonsInput{}); return e },
		"createReason":      func() error { _, e := s.createReason(ctx, &createReasonInput{}); return e },
		"patchReason":       func() error { _, e := s.patchReason(ctx, &patchReasonInput{}); return e },
		"listDispositions":  func() error { _, e := s.listDispositions(ctx, &listDispositionsInput{}); return e },
		"redeliver":         func() error { _, e := s.redeliverDisposition(ctx, &dispositionIDInput{ID: 1}); return e },
	}
	for name, call := range checks {
		if got := statusOf(call()); got != http.StatusForbidden {
			t.Errorf("%s site-scoped: want 403, got status %d", name, got)
		}
	}
}

func truncateReviewTables(t *testing.T) {
	t.Helper()
	for _, tbl := range []string{"trust_disposition", "trust_report", "trust_review_item"} {
		if err := testDB.Exec("TRUNCATE " + tbl + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatalf("truncate %s: %v", tbl, err)
		}
	}
}

func seedItem(t *testing.T, site, subject string) int64 {
	t.Helper()
	it := model.TrustReviewItem{
		Site: site, SubjectKind: "forum_topic", SubjectID: subject,
		Source: model.ReviewSourceReports, Priority: 2, Status: model.ReviewStatusPending,
	}
	if err := testDB.Create(&it).Error; err != nil {
		t.Fatalf("seed item in %s: %v", site, err)
	}
	return it.ID
}

func TestAdminReviewItemsSiteScoped(t *testing.T) {
	if testDB == nil {
		dbtest.Skipf(t, "the trust test database is unavailable")
	}
	truncateReviewTables(t)
	ownID := seedItem(t, "kungal", "own")
	foreignID := seedItem(t, "otokun", "foreign")

	s := &AdminServer{
		review:  service.NewReviewService(testDB),
		clients: &fakeClients{byID: map[string]*siteModel.OAuthClient{"kungal-client": {CatalogSite: "kungal"}}},
	}
	scoped := scopedCtx(nil, "kungal-client", 7)
	staff := scopedCtx([]string{"admin"}, "", 7)

	out, err := s.listReviewItems(scoped, &listReviewItemsInput{Site: "otokun", Status: -1, Source: -1})
	if err != nil {
		t.Fatalf("scoped list: %v", err)
	}
	if len(out.Body.Data.Items) == 0 {
		t.Fatal("scoped list returned nothing for own site")
	}
	for _, it := range out.Body.Data.Items {
		if it.Site != "kungal" {
			t.Fatalf("scoped list leaked site %q", it.Site)
		}
	}

	staffOut, err := s.listReviewItems(staff, &listReviewItemsInput{Status: -1, Source: -1})
	if err != nil {
		t.Fatalf("staff list: %v", err)
	}
	if len(staffOut.Body.Data.Items) < 2 {
		t.Fatalf("staff must see both sites, got %d", len(staffOut.Body.Data.Items))
	}

	if _, err := s.getReviewItem(scoped, &reviewItemIDInput{ID: ownID}); err != nil {
		t.Fatalf("get own-site scoped: %v", err)
	}
	if got := statusOf(mustErr(s.getReviewItem(scoped, &reviewItemIDInput{ID: foreignID}))); got != http.StatusNotFound {
		t.Fatalf("get foreign-site scoped: want 404, got %d", got)
	}

	if got := statusOf(mustErr(s.claimReviewItem(scoped, &reviewItemIDInput{ID: foreignID}))); got != http.StatusNotFound {
		t.Fatalf("claim foreign-site scoped: want 404, got %d", got)
	}
	if got := statusOf(mustErr(s.decideReviewItem(scoped, &decideInput{ID: foreignID, Body: dto.DecideRequest{Decision: "dismissed"}}))); got != http.StatusNotFound {
		t.Fatalf("decide foreign-site scoped: want 404, got %d", got)
	}

	if _, err := s.claimReviewItem(scoped, &reviewItemIDInput{ID: ownID}); err != nil {
		t.Fatalf("claim own-site scoped: %v", err)
	}
	if _, err := s.decideReviewItem(scoped, &decideInput{ID: ownID, Body: dto.DecideRequest{Decision: "dismissed"}}); err != nil {
		t.Fatalf("decide own-site scoped: %v", err)
	}
}

func mustErr[T any](_ T, err error) error { return err }
