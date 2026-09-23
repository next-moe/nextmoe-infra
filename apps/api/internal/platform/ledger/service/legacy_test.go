package service_test

import (
	"context"
	"testing"
	"time"

	"api/internal/platform/ledger/model"
	"api/internal/platform/ledger/service"
)

type legacyRow struct {
	user   uint
	delta  int
	reason string
	source string
}

func writeLegacy(t *testing.T, rows ...legacyRow) {
	t.Helper()
	at := time.Now().Add(-time.Hour)
	for i, r := range rows {
		row := model.LegacyLog{
			UserID: r.user, Delta: r.delta, Reason: r.reason, SourceApp: r.source,
			IdempotencyKey: key(t, r.user, i, time.Now().UnixNano()),
			CreatedAt:      at.Add(time.Duration(i) * time.Second),
		}
		if err := testDB.Create(&row).Error; err != nil {
			t.Fatalf("write legacy row: %v", err)
		}
	}
}

// The old binary moved users.moemoepoint and appended its log row in one
// transaction; a legacy row in a test has to arrive the same way.
func oldBinaryAward(t *testing.T, r legacyRow) {
	t.Helper()
	writeLegacy(t, r)
	if err := testDB.Exec(`UPDATE users SET moemoepoint = moemoepoint + ? WHERE id = ?`, r.delta, r.user).Error; err != nil {
		t.Fatalf("bump users.moemoepoint: %v", err)
	}
}

func importLegacy(t *testing.T) service.Imported {
	t.Helper()
	got, err := service.ImportLegacy(context.Background(), testDB)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return got
}

func history(t *testing.T, l *service.Ledger, u uint) []service.HistoryItem {
	t.Helper()
	items, _, err := l.UserHistory(context.Background(), u, 100, 0, "")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	return items
}

func TestImportLegacyCarriesTheLogAndExplainsEveryBalance(t *testing.T) {
	l := service.New(testDB)
	importLegacy(t)

	earned := newUser(t, 15)
	writeLegacy(t,
		legacyRow{earned, 10, model.ReasonLiked, "forum-test"},
		legacyRow{earned, 5, model.ReasonDailyCheckin, "forum-test"})
	unexplained := newUser(t, 20)
	writeLegacy(t, legacyRow{unexplained, 7, model.ReasonRegisterGift, "oauth"})
	clawedBack := newUser(t, -3)
	writeLegacy(t,
		legacyRow{clawedBack, 2, model.ReasonContentApproved, "patch-test"},
		legacyRow{clawedBack, -5, model.ReasonContentRemoved, "patch-test"})
	renamed := newUser(t, 3)
	writeLegacy(t,
		legacyRow{renamed, 20, model.ReasonMigration, "oauth"},
		legacyRow{renamed, -17, model.ReasonNameChange, "oauth"})
	silent := newUser(t, 9)

	got := importLegacy(t)
	if got.Legacy != 7 || got.Openings != 2 {
		t.Fatalf("imported %+v, want 7 legacy rows and 2 openings", got)
	}
	for u, want := range map[uint]int64{earned: 15, unexplained: 20, clawedBack: -3, renamed: 3, silent: 9} {
		wantBalance(t, l, u, want)
	}

	h := history(t, l, earned)
	if len(h) != 2 || h[0].BalanceAfter != 15 || h[1].BalanceAfter != 10 || h[1].SourceApp != "forum-test" {
		t.Fatalf("earned history: %+v", h)
	}
	h = history(t, l, unexplained)
	if len(h) != 2 || h[1].Reason != model.ReasonOpeningBalance || h[1].Delta != 13 || h[0].BalanceAfter != 20 {
		t.Fatalf("an unexplained balance must open before the log that follows it: %+v", h)
	}

	var sinkEntries int64
	testDB.Raw(`SELECT COUNT(*) FROM ledger_entries e
		JOIN ledger_accounts a ON a.id = e.account_id
		JOIN ledger_transfers t ON t.id = e.transfer_id
		WHERE a.kind = ? AND a.code = 'oauth' AND t.reason = ?`,
		model.KindSink, model.ReasonNameChange).Scan(&sinkEntries)
	if sinkEntries < 1 {
		t.Fatal("a legacy rename charge must land in the oauth sink, not back in the issuer")
	}

	if again := importLegacy(t); again.Legacy != 0 || again.Openings != 0 {
		t.Fatalf("a second import found %+v", again)
	}
	audit(t)
}

// Between the migrate job's import and cmd/oauth's, the binary being replaced
// is still serving: whatever it wrote in that window must still arrive.
func TestImportLegacyCarriesWhatTheOldBinaryWroteAfterward(t *testing.T) {
	l := service.New(testDB)
	importLegacy(t)

	existing := newUser(t, 0)
	oldBinaryAward(t, legacyRow{existing, 6, model.ReasonLiked, "forum-test"})
	importLegacy(t)
	wantBalance(t, l, existing, 6)

	oldBinaryAward(t, legacyRow{existing, 4, model.ReasonLiked, "forum-test"})
	registered := newUser(t, 0)
	oldBinaryAward(t, legacyRow{registered, 7, model.ReasonRegisterGift, "oauth"})

	got := importLegacy(t)
	if got.Legacy != 2 || got.Openings != 0 {
		t.Fatalf("imported %+v, want the 2 stragglers and no openings", got)
	}
	wantBalance(t, l, existing, 10)
	wantBalance(t, l, registered, 7)
	if h := history(t, l, existing); h[0].BalanceAfter != 10 || h[1].BalanceAfter != 6 {
		t.Fatalf("straggler history: %+v", h)
	}
	audit(t)
}

func TestImportLegacySkipsARowTheLedgerAlreadyAnswered(t *testing.T) {
	l := service.New(testDB)
	importLegacy(t)

	u := newUser(t, 0)
	award := service.Award{UserID: u, Delta: 5, Reason: model.ReasonLiked,
		SourceApp: "forum-test", IdempotencyKey: key(t, "retried")}
	if _, err := l.Award(context.Background(), award); err != nil {
		t.Fatalf("award: %v", err)
	}
	dup := model.LegacyLog{UserID: u, Delta: 5, Reason: model.ReasonLiked,
		SourceApp: "forum-test", IdempotencyKey: award.IdempotencyKey, CreatedAt: time.Now()}
	if err := testDB.Create(&dup).Error; err != nil {
		t.Fatalf("write duplicate legacy row: %v", err)
	}

	if got := importLegacy(t); got.Legacy != 0 {
		t.Fatalf("imported %+v; the retried award would have been paid twice", got)
	}
	if b, _ := l.UserBalance(context.Background(), u); b != 5 {
		t.Fatalf("balance %d, want 5", b)
	}
	audit(t)
}

func TestInstalledSeesTheLedger(t *testing.T) {
	ok, err := service.Installed(context.Background(), testDB)
	if err != nil || !ok {
		t.Fatalf("Installed = %v, %v on a migrated database", ok, err)
	}
}
