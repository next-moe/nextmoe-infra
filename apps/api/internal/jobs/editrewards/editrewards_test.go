package editrewards_test

import (
	"context"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"api/internal/jobs/editrewards"
	authModel "api/internal/platform/auth/model"
	catmigrate "api/internal/platform/catalog/migrate"
	"api/internal/platform/editing"
	"api/internal/platform/ledger/ledgertest"
	ledgerModel "api/internal/platform/ledger/model"
	"api/internal/platform/ledger/service"
	siteModel "api/internal/platform/site/model"
	"api/internal/testsupport/dbtest"

	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("editrewards suite")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("editrewards suite", "connect: %v", err)
	}
	if err := ledgertest.Migrate(db); err != nil {
		dbtest.SkipMainf("editrewards suite", "migrate ledger: %v", err)
	}
	if err := editing.AutoMigrate(db); err != nil {
		dbtest.SkipMainf("editrewards suite", "migrate editing: %v", err)
	}
	if err := catmigrate.EditLegacyColumns(db); err != nil {
		dbtest.SkipMainf("editrewards suite", "migrate editing legacy columns: %v", err)
	}
	if err := db.AutoMigrate(&siteModel.Site{}, &siteModel.OAuthClient{}); err != nil {
		dbtest.SkipMainf("editrewards suite", "migrate sites: %v", err)
	}
	testDB = db
	os.Exit(m.Run())
}

var seq atomic.Int64

func newUser(t *testing.T) uint {
	t.Helper()
	tag := strconv.FormatInt(time.Now().UnixNano(), 36) + strconv.FormatInt(seq.Add(1), 36)
	u := &authModel.User{Name: "er" + tag[len(tag)-10:], Email: "er-" + tag + "@test.local"}
	if err := testDB.Create(u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		if err := ledgertest.Purge(testDB, u.ID); err != nil {
			t.Errorf("purge: %v", err)
		}
		testDB.Exec(`DELETE FROM users WHERE id = ?`, u.ID)
	})
	return u.ID
}

func newProposal(t *testing.T, proposer, decider uint, status int16, decidedAt time.Time) int64 {
	t.Helper()
	by := int64(decider)
	p := &editing.Proposal{
		EntityFamily: "work", EntityType: "catalog.work", EntityID: 4,
		Patch: datatypes.JSON(`{}`), ProposerUID: int64(proposer),
		Site: "editor", Status: status, DecidedByUID: &by, DecidedAt: &decidedAt,
	}
	if err := testDB.Create(p).Error; err != nil {
		t.Fatalf("create proposal: %v", err)
	}
	t.Cleanup(func() {
		testDB.Exec(`DELETE FROM edit_revision WHERE proposal_id = ?`, p.ID)
		testDB.Exec(`DELETE FROM edit_proposal WHERE id = ?`, p.ID)
	})
	return p.ID
}

func run(t *testing.T, amount int64) editrewards.Report {
	t.Helper()
	rep, err := editrewards.Run(context.Background(), testDB, testDB, editrewards.Opts{
		Amount: amount,
		Since:  time.Now().Add(-7 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return rep
}

func balance(t *testing.T, userID uint) int64 {
	t.Helper()
	b, err := service.New(testDB).UserBalance(context.Background(), userID)
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	return b
}

func TestAwardsOnceAndRerunIsIdempotent(t *testing.T) {
	proposer, reviewer := newUser(t), newUser(t)
	id := newProposal(t, proposer, reviewer, editing.StatusMerged, time.Now().Add(-time.Hour))

	rep := run(t, 3)
	if rep.Candidates != 1 || rep.Awarded != 1 {
		t.Fatalf("first run: %+v", rep)
	}
	if got := balance(t, proposer); got != 3 {
		t.Fatalf("balance after first run = %d, want 3", got)
	}
	var tr ledgerModel.Transfer
	if err := testDB.Where("source_app = ? AND idempotency_key = ?",
		editrewards.SourceApp, editrewards.KeyPrefix+strconv.FormatInt(id, 10)).
		First(&tr).Error; err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if tr.Reason != ledgerModel.ReasonContentApproved || tr.Ref != editrewards.RefPrefix+strconv.FormatInt(id, 10) ||
		tr.ActorUserID != reviewer {
		t.Fatalf("transfer = %+v", tr)
	}

	rep = run(t, 3)
	if rep.Awarded != 0 || rep.AlreadyAwarded != 1 {
		t.Fatalf("second run: %+v", rep)
	}
	if got := balance(t, proposer); got != 3 {
		t.Fatalf("balance after rerun = %d, want 3", got)
	}
}

func TestSelfMergeAutomergeAndRevertEarnNothing(t *testing.T) {
	proposer, reviewer := newUser(t), newUser(t)
	newProposal(t, proposer, proposer, editing.StatusMerged, time.Now().Add(-time.Hour))
	reverted := newProposal(t, proposer, reviewer, editing.StatusMerged, time.Now().Add(-time.Hour))
	if err := testDB.Create(&editing.Revision{
		EntityFamily: "work", EntityType: "catalog.work", EntityID: 4, Seq: 1,
		Action: editing.ActionReverted, ChangedFields: datatypes.JSON(`[]`), Snapshot: datatypes.JSON(`{}`),
		ActorUID: int64(reviewer), ProposalID: &reverted, Site: "editor",
	}).Error; err != nil {
		t.Fatalf("revision: %v", err)
	}
	newProposal(t, proposer, reviewer, editing.StatusDeclined, time.Now().Add(-time.Hour))
	newProposal(t, proposer, 0, editing.StatusMerged, time.Now().Add(-time.Hour))

	if rep := run(t, 1); rep.Candidates != 0 {
		t.Fatalf("expected no candidates, got %+v", rep)
	}
	if got := balance(t, proposer); got != 0 {
		t.Fatalf("balance = %d, want 0", got)
	}
}

func TestSkipsProposalsTheForumAlreadyAwarded(t *testing.T) {
	proposer, reviewer := newUser(t), newUser(t)
	id := newProposal(t, proposer, reviewer, editing.StatusMerged, time.Now().Add(-time.Hour))

	site := siteModel.Site{Name: "鲲 Galgame 论坛", Domain: "www.kungal.com"}
	if err := testDB.Where("domain = ?", site.Domain).FirstOrCreate(&site).Error; err != nil {
		t.Fatalf("site: %v", err)
	}
	clientID := "kungal-test-" + strconv.FormatInt(seq.Add(1), 10)
	if err := testDB.Create(&siteModel.OAuthClient{ID: clientID, SiteID: &site.ID, Name: "kungal", Secret: "x", RedirectURIs: datatypes.JSON(`[]`), Grants: datatypes.JSON(`[]`)}).Error; err != nil {
		t.Fatalf("client: %v", err)
	}
	t.Cleanup(func() { testDB.Exec(`DELETE FROM oauth_clients WHERE id = ?`, clientID) })
	if _, err := service.New(testDB).Award(context.Background(), service.Award{
		UserID: proposer, Delta: 1, Reason: ledgerModel.ReasonContentApproved, SourceApp: clientID,
		IdempotencyKey: "galgame_edit_merged:" + strconv.FormatInt(id, 10),
	}); err != nil {
		t.Fatalf("forum award: %v", err)
	}

	rep := run(t, 1)
	if rep.ForumAwarded != 1 || rep.Awarded != 0 {
		t.Fatalf("run: %+v", rep)
	}
	if got := balance(t, proposer); got != 1 {
		t.Fatalf("balance = %d, want 1 (the forum's award only)", got)
	}
}

func TestZeroAmountDisables(t *testing.T) {
	proposer, reviewer := newUser(t), newUser(t)
	newProposal(t, proposer, reviewer, editing.StatusMerged, time.Now().Add(-time.Hour))

	if rep := run(t, 0); rep != (editrewards.Report{}) {
		t.Fatalf("run: %+v", rep)
	}
	if got := balance(t, proposer); got != 0 {
		t.Fatalf("balance = %d, want 0", got)
	}
}

func TestWindowBoundsTheScan(t *testing.T) {
	proposer, reviewer := newUser(t), newUser(t)
	newProposal(t, proposer, reviewer, editing.StatusMerged, time.Now().Add(-10*24*time.Hour))

	if rep := run(t, 1); rep.Candidates != 0 {
		t.Fatalf("a merge outside the window was scanned: %+v", rep)
	}
}

func TestDeletedProposerIsSkipped(t *testing.T) {
	proposer, reviewer := newUser(t), newUser(t)
	newProposal(t, proposer, reviewer, editing.StatusMerged, time.Now().Add(-time.Hour))
	if err := testDB.Exec(`UPDATE users SET deleted_at = now() WHERE id = ?`, proposer).Error; err != nil {
		t.Fatalf("delete user: %v", err)
	}

	rep := run(t, 1)
	if rep.ProposerGone != 1 || rep.Awarded != 0 || rep.Failed != 0 {
		t.Fatalf("run: %+v", rep)
	}
}
