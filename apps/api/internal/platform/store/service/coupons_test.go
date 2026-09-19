package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"api/internal/platform/store/model"
	"api/internal/platform/store/storetest"
)

// seedClicks gives each site one purchase link with the given uniques on one
// JST day, plus bot hits that must not count.
func seedClicks(t *testing.T, day string, uniques map[string]int64) {
	t.Helper()
	now := time.Now()
	for client, u := range uniques {
		alias := "a-" + client
		if err := testDB.Create(&model.PurchaseLink{
			ClientID: client, ProductID: "RJ100001", Alias: alias, ShortURL: "https://s.test/" + alias,
		}).Error; err != nil {
			t.Fatalf("seed link: %v", err)
		}
		if err := testDB.Create(&model.LinkDailyStat{
			Alias: alias, Day: day, Total: u + 50, Uniques: u, Bots: 50, SyncedAt: now,
		}).Error; err != nil {
			t.Fatalf("seed stat: %v", err)
		}
	}
}

func couponFixture(t *testing.T) (*Service, []AdminApp) {
	t.Helper()
	if err := storetest.Truncate(testDB); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	seedClicks(t, "2026-09-10", map[string]int64{"forum": 3060, "patch": 3357, "yukihub": 7, "outsider": 900})
	apps := []AdminApp{
		{ClientID: "forum", Name: "论坛", SettlementEligible: true},
		{ClientID: "patch", Name: "补丁", SettlementEligible: true},
		{ClientID: "yukihub", Name: "YukiHub", SettlementEligible: true},
		{ClientID: "outsider", Name: "未参与", SettlementEligible: false},
	}
	return New(testDB, nil, Options{}), apps
}

func firstBatchInput() CreateBatchInput {
	in := CreateBatchInput{Name: "2026-09 第一批", PeriodFrom: "2026-09-01", PeriodTo: "2026-09-30"}
	for i := range 10 {
		in.Coupons = append(in.Coupons, CouponInput{FaceValue: 5000, Code: "BIG-" + string(rune('A'+i))})
	}
	for i := range 21 {
		in.Coupons = append(in.Coupons, CouponInput{FaceValue: 1000, Code: "SMALL-" + string(rune('A'+i))})
	}
	return in
}

func TestCouponBatchFromDraftToTheOwnersView(t *testing.T) {
	svc, apps := couponFixture(t)
	ctx := context.Background()

	batch, err := svc.CreateCouponBatch(ctx, 2, firstBatchInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if batch.Status != model.BatchDraft {
		t.Fatalf("status = %q, want draft", batch.Status)
	}

	detail, err := svc.CouponBatchDetail(ctx, batch.ID, apps)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail.CouponCount != 31 || detail.Points != 71_000 {
		t.Fatalf("summary = %d coupons / %d points, want 31 / 71000", detail.CouponCount, detail.Points)
	}
	proposal := map[string]SplitRow{}
	var proposed int64
	for _, row := range detail.Split {
		proposal[row.ClientID] = row
		proposed += row.AllocatedPoints
	}
	if proposed != 71_000 {
		t.Errorf("proposal places %d points, want all 71,000", proposed)
	}
	if out := proposal["outsider"]; out.SettlementEligible || out.AllocatedPoints != 0 || out.Uniques != 900 {
		t.Errorf("an ineligible site is listed with its clicks and nothing else: %+v", out)
	}
	if proposal["patch"].Uniques != 3357 {
		t.Errorf("uniques read bots in: %+v", proposal["patch"])
	}

	var grants []GrantInput
	for _, row := range detail.Split {
		for _, g := range row.Grants {
			grants = append(grants, GrantInput{ClientID: row.ClientID, FaceValue: g.FaceValue, Count: g.Count})
		}
	}
	if err := svc.PublishCouponBatch(ctx, batch.ID, apps, grants, time.Now()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := svc.PublishCouponBatch(ctx, batch.ID, apps, grants, time.Now()); !errors.Is(err, ErrBatchNotDraft) {
		t.Fatalf("second publish: want ErrBatchNotDraft, got %v", err)
	}
	if err := svc.DeleteCouponBatch(ctx, batch.ID); !errors.Is(err, ErrBatchNotDraft) {
		t.Fatalf("deleting a published batch: want ErrBatchNotDraft, got %v", err)
	}

	published, err := svc.CouponBatchDetail(ctx, batch.ID, apps)
	if err != nil {
		t.Fatalf("published detail: %v", err)
	}
	if published.Allocated != 31 {
		t.Errorf("allocated = %d, want 31", published.Allocated)
	}
	for _, row := range published.Split {
		if !row.SettlementEligible {
			t.Errorf("a published split holds eligible sites only: %+v", row)
		}
		if want := proposal[row.ClientID].AllocatedPoints; row.AllocatedPoints != want {
			t.Errorf("%s received %d points, proposal said %d", row.ClientID, row.AllocatedPoints, want)
		}
	}

	owner := []OwnerApp{{ClientID: "forum", Name: "论坛"}}
	mine, err := svc.OwnerCoupons(ctx, owner)
	if err != nil {
		t.Fatalf("owner coupons: %v", err)
	}
	var mineTotal int64
	for _, c := range mine.Coupons {
		if c.ClientID != "forum" || c.Code == "" || c.BatchName != "2026-09 第一批" {
			t.Fatalf("owner sees a coupon that is not theirs or is incomplete: %+v", c)
		}
		mineTotal += int64(c.FaceValue)
	}
	if mineTotal != proposal["forum"].AllocatedPoints {
		t.Errorf("owner coupons total %d, want %d", mineTotal, proposal["forum"].AllocatedPoints)
	}
	if len(mine.Shares) != 1 || mine.Shares[0].Uniques != 3060 {
		t.Errorf("owner share = %+v, want one row with 3060 uniques", mine.Shares)
	}

	first := mine.Coupons[0].ID
	if err := svc.SetCouponDelivered(ctx, owner, first, true, time.Now()); err != nil {
		t.Fatalf("mark delivered: %v", err)
	}
	patchOwner := []OwnerApp{{ClientID: "patch", Name: "补丁"}}
	if err := svc.SetCouponDelivered(ctx, patchOwner, first, true, time.Now()); !errors.Is(err, ErrCouponNotFound) {
		t.Fatalf("another site's coupon: want ErrCouponNotFound, got %v", err)
	}
	again, _ := svc.OwnerCoupons(ctx, owner)
	if again.Coupons[0].DeliveredAt == nil {
		t.Fatal("delivered_at not set")
	}
	if err := svc.SetCouponDelivered(ctx, owner, first, false, time.Now()); err != nil {
		t.Fatalf("unmark: %v", err)
	}
	again, _ = svc.OwnerCoupons(ctx, owner)
	if again.Coupons[0].DeliveredAt != nil {
		t.Fatal("delivered_at not cleared")
	}
}

func TestDraftCodesStayHiddenFromOwners(t *testing.T) {
	svc, _ := couponFixture(t)
	ctx := context.Background()
	if _, err := svc.CreateCouponBatch(ctx, 2, firstBatchInput()); err != nil {
		t.Fatalf("create: %v", err)
	}
	mine, err := svc.OwnerCoupons(ctx, []OwnerApp{{ClientID: "forum", Name: "论坛"}})
	if err != nil {
		t.Fatalf("owner coupons: %v", err)
	}
	if len(mine.Coupons) != 0 || len(mine.Shares) != 0 {
		t.Fatalf("a draft leaked to an owner: %+v", mine)
	}
}

func TestCreateCouponBatchRejectsBadInput(t *testing.T) {
	svc, _ := couponFixture(t)
	ctx := context.Background()
	if _, err := svc.CreateCouponBatch(ctx, 2, firstBatchInput()); err != nil {
		t.Fatalf("create: %v", err)
	}

	cases := map[string]func(*CreateBatchInput){
		"a code entered in an earlier batch": func(in *CreateBatchInput) {},
		"a duplicate inside the batch": func(in *CreateBatchInput) {
			in.Coupons = []CouponInput{{FaceValue: 1000, Code: "NEW-1"}, {FaceValue: 1000, Code: "NEW-1"}}
		},
		"a code with whitespace": func(in *CreateBatchInput) {
			in.Coupons = []CouponInput{{FaceValue: 1000, Code: "NEW 2"}}
		},
		"a non-positive face value": func(in *CreateBatchInput) {
			in.Coupons = []CouponInput{{FaceValue: 0, Code: "NEW-3"}}
		},
		"a reversed period": func(in *CreateBatchInput) {
			in.PeriodFrom, in.PeriodTo = "2026-09-30", "2026-09-01"
			in.Coupons = []CouponInput{{FaceValue: 1000, Code: "NEW-4"}}
		},
		"a malformed expiry": func(in *CreateBatchInput) {
			in.Coupons = []CouponInput{{FaceValue: 1000, Code: "NEW-5", ExpiresOn: "12/31"}}
		},
		"no coupons": func(in *CreateBatchInput) { in.Coupons = nil },
		"no name":    func(in *CreateBatchInput) { in.Name = "  " },
	}
	for name, mutate := range cases {
		in := firstBatchInput()
		mutate(&in)
		_, err := svc.CreateCouponBatch(ctx, 2, in)
		var input *InputError
		if !errors.As(err, &input) {
			t.Errorf("%s: want an InputError, got %v", name, err)
		}
	}
	var batches int64
	testDB.Model(&model.CouponBatch{}).Count(&batches)
	if batches != 1 {
		t.Errorf("rejected input left %d batches behind, want 1", batches)
	}
}

func TestPublishRefusesAnImpossibleSplit(t *testing.T) {
	svc, apps := couponFixture(t)
	ctx := context.Background()
	batch, err := svc.CreateCouponBatch(ctx, 2, firstBatchInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cases := map[string][]GrantInput{
		"more coupons than the batch holds": {{ClientID: "forum", FaceValue: 5000, Count: 11}},
		"an ineligible site":                {{ClientID: "outsider", FaceValue: 1000, Count: 1}},
		"a face value the batch lacks":      {{ClientID: "forum", FaceValue: 3000, Count: 1}},
		"a negative count":                  {{ClientID: "forum", FaceValue: 1000, Count: -1}},
		"one site listed twice": {
			{ClientID: "forum", FaceValue: 1000, Count: 1}, {ClientID: "forum", FaceValue: 1000, Count: 1},
		},
	}
	for name, grants := range cases {
		err := svc.PublishCouponBatch(ctx, batch.ID, apps, grants, time.Now())
		var input *InputError
		if !errors.As(err, &input) {
			t.Errorf("%s: want an InputError, got %v", name, err)
		}
	}
	var assigned int64
	testDB.Model(&model.Coupon{}).Where("client_id IS NOT NULL").Count(&assigned)
	if assigned != 0 {
		t.Errorf("a refused publish assigned %d coupons", assigned)
	}
}

func TestPublishKeepsBackWhatNoGrantCovers(t *testing.T) {
	svc, apps := couponFixture(t)
	ctx := context.Background()
	in := firstBatchInput()
	in.Coupons[0].ExpiresOn = "2026-10-31"
	batch, err := svc.CreateCouponBatch(ctx, 2, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	grants := []GrantInput{{ClientID: "patch", FaceValue: 5000, Count: 1}}
	if err := svc.PublishCouponBatch(ctx, batch.ID, apps, grants, time.Now()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	var got model.Coupon
	testDB.Where("batch_id = ? AND client_id = ?", batch.ID, "patch").Take(&got)
	if got.Code != "BIG-A" {
		t.Errorf("the soonest-expiring code goes out first: got %q", got.Code)
	}
	var kept int64
	testDB.Model(&model.Coupon{}).Where("batch_id = ? AND client_id IS NULL", batch.ID).Count(&kept)
	if kept != 30 {
		t.Errorf("kept back %d coupons, want 30", kept)
	}
	var shares []model.CouponShare
	testDB.Where("batch_id = ?", batch.ID).Order("client_id").Find(&shares)
	var names []string
	for _, s := range shares {
		names = append(names, s.ClientID)
	}
	if strings.Join(names, ",") != "forum,patch,yukihub" {
		t.Errorf("shares = %v, want every eligible site with clicks", names)
	}
}

func TestDeleteDraftRemovesItsCodes(t *testing.T) {
	svc, _ := couponFixture(t)
	ctx := context.Background()
	batch, err := svc.CreateCouponBatch(ctx, 2, firstBatchInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.DeleteCouponBatch(ctx, batch.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var left int64
	testDB.Model(&model.Coupon{}).Count(&left)
	if left != 0 {
		t.Fatalf("%d codes survived their batch", left)
	}
	if _, err := svc.CreateCouponBatch(ctx, 2, firstBatchInput()); err != nil {
		t.Fatalf("the same codes must be enterable again after a draft is deleted: %v", err)
	}
}

func TestAdminUsageSplitsEverySiteAndCountsBotsApart(t *testing.T) {
	svc, apps := couponFixture(t)
	usage, err := svc.AdminUsage(context.Background(), apps, "2026-09-01", "2026-09-30")
	if err != nil {
		t.Fatalf("admin usage: %v", err)
	}
	if usage.Uniques != 3060+3357+7+900 || usage.Bots != 200 {
		t.Fatalf("totals = %d uniques / %d bots, want 7324 / 200", usage.Uniques, usage.Bots)
	}
	if len(usage.Daily) != 30 {
		t.Errorf("daily = %d, want 30 dense days", len(usage.Daily))
	}
	if len(usage.ByApp) != 4 || usage.ByApp[0].ClientID != "patch" {
		t.Fatalf("by_app = %+v, want four sites, most uniques first", usage.ByApp)
	}
	var ppm int64
	for _, a := range usage.ByApp {
		ppm += a.SharePPM
	}
	if ppm < 999_998 || ppm > 1_000_002 {
		t.Errorf("shares sum to %d ppm", ppm)
	}
	if len(usage.TopLinks) != 4 {
		t.Errorf("top links = %d, want 4", len(usage.TopLinks))
	}
}
