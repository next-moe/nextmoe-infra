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

const (
	kun     = uint(2)
	yuki    = uint(89136)
	someone = uint(5)
)

// couponFixture mirrors production: the forum and the patch site share an
// owner, a partner owns one site, one owner is off the roster, and one
// eligible application has no owner at all.
func couponFixture(t *testing.T) (*Service, []AdminApp) {
	t.Helper()
	if err := storetest.Truncate(testDB); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	seedClicks(t, "2026-09-10", map[string]int64{"forum": 3060, "patch": 3357, "yukihub": 7, "outsider": 900, "orphan": 40})
	owned := func(id uint) *uint { return &id }
	apps := []AdminApp{
		{ClientID: "forum", Name: "论坛", OwnerUserID: owned(kun), OwnerName: "鲲", SettlementEligible: true},
		{ClientID: "patch", Name: "补丁", OwnerUserID: owned(kun), OwnerName: "鲲", SettlementEligible: true},
		{ClientID: "yukihub", Name: "YukiHub", OwnerUserID: owned(yuki), OwnerName: "永雏小夜", SettlementEligible: true},
		{ClientID: "outsider", Name: "未参与", OwnerUserID: owned(someone), OwnerName: "路人", SettlementEligible: false},
		{ClientID: "orphan", Name: "无主", SettlementEligible: true},
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
	proposal := map[uint]SplitRow{}
	var proposed int64
	for _, row := range detail.Split {
		proposal[row.UserID] = row
		proposed += row.AllocatedPoints
	}
	// 6,417 of 6,424 counted uniques: floor(71,000·6417/6424) = 70,922 points,
	// which 10×5,000 + 20×1,000 fill; the last 1,000 fits nobody.
	if proposed != 70_000 || proposal[kun].EntitledPoints != 70_922 {
		t.Errorf("proposal places %d points of an entitlement of %d, want 70,000 of 70,922",
			proposed, proposal[kun].EntitledPoints)
	}
	if got := proposal[kun]; got.Uniques != 3060+3357 || len(got.Apps) != 2 || got.Apps[0].ClientID != "patch" || got.Name != "鲲" {
		t.Errorf("one owner's two sites make one claim: %+v", got)
	}
	if got := proposal[yuki]; got.EntitledPoints != 77 || got.AllocatedPoints != 0 {
		t.Errorf("a 77-point entitlement takes no coupon: %+v", got)
	}
	if len(detail.Excluded) != 2 || detail.Excluded[0].ClientID != "outsider" || detail.Excluded[1].ClientID != "orphan" {
		t.Errorf("excluded = %+v, want the off-roster site and the ownerless one", detail.Excluded)
	}

	var grants []GrantInput
	for _, row := range detail.Split {
		for _, g := range row.Grants {
			grants = append(grants, GrantInput{UserID: row.UserID, FaceValue: g.FaceValue, Count: g.Count})
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
	if published.Allocated != 30 {
		t.Errorf("allocated = %d, want 30", published.Allocated)
	}
	for _, row := range published.Split {
		if want := proposal[row.UserID].AllocatedPoints; row.AllocatedPoints != want {
			t.Errorf("user %d received %d points, proposal said %d", row.UserID, row.AllocatedPoints, want)
		}
	}
	for _, c := range published.CouponList {
		if c.UserID != nil && c.UserName != "鲲" {
			t.Errorf("coupon %s names its owner %q", c.Code, c.UserName)
		}
	}

	mine, err := svc.OwnerCoupons(ctx, kun)
	if err != nil {
		t.Fatalf("owner coupons: %v", err)
	}
	var mineTotal int64
	for _, c := range mine.Coupons {
		if c.Code == "" || c.BatchName != "2026-09 第一批" {
			t.Fatalf("owner sees an incomplete coupon: %+v", c)
		}
		mineTotal += int64(c.FaceValue)
	}
	if mineTotal != 70_000 {
		t.Errorf("owner coupons total %d, want 70,000", mineTotal)
	}
	if len(mine.Shares) != 1 || mine.Shares[0].Uniques != 6417 || len(mine.Shares[0].Apps) != 2 {
		t.Errorf("owner share = %+v, want one row of 6417 uniques over two sites", mine.Shares)
	}

	first := mine.Coupons[0].ID
	if err := svc.SetCouponDelivered(ctx, kun, first, true, time.Now()); err != nil {
		t.Fatalf("mark delivered: %v", err)
	}
	if err := svc.SetCouponDelivered(ctx, yuki, first, true, time.Now()); !errors.Is(err, ErrCouponNotFound) {
		t.Fatalf("another owner's coupon: want ErrCouponNotFound, got %v", err)
	}
	again, _ := svc.OwnerCoupons(ctx, kun)
	if again.Coupons[0].DeliveredAt == nil {
		t.Fatal("delivered_at not set")
	}
	if err := svc.SetCouponDelivered(ctx, kun, first, false, time.Now()); err != nil {
		t.Fatalf("unmark: %v", err)
	}
	again, _ = svc.OwnerCoupons(ctx, kun)
	if again.Coupons[0].DeliveredAt != nil {
		t.Fatal("delivered_at not cleared")
	}
	theirs, _ := svc.OwnerCoupons(ctx, yuki)
	if len(theirs.Coupons) != 0 || len(theirs.Shares) != 1 || theirs.Shares[0].EntitledPoints != 77 {
		t.Errorf("the partner sees its share and no coupon: %+v", theirs)
	}
}

func TestDraftCodesStayHiddenFromOwners(t *testing.T) {
	svc, _ := couponFixture(t)
	ctx := context.Background()
	if _, err := svc.CreateCouponBatch(ctx, 2, firstBatchInput()); err != nil {
		t.Fatalf("create: %v", err)
	}
	mine, err := svc.OwnerCoupons(ctx, kun)
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
		"more coupons than the batch holds": {{UserID: kun, FaceValue: 5000, Count: 11}},
		"an owner off the roster":           {{UserID: someone, FaceValue: 1000, Count: 1}},
		"an account with no application":    {{UserID: 999, FaceValue: 1000, Count: 1}},
		"a face value the batch lacks":      {{UserID: kun, FaceValue: 3000, Count: 1}},
		"a negative count":                  {{UserID: kun, FaceValue: 1000, Count: -1}},
		"one owner listed twice": {
			{UserID: kun, FaceValue: 1000, Count: 1}, {UserID: kun, FaceValue: 1000, Count: 1},
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
	testDB.Model(&model.Coupon{}).Where("user_id IS NOT NULL").Count(&assigned)
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
	// The operator may hand an account more than its share by hand.
	grants := []GrantInput{{UserID: yuki, FaceValue: 5000, Count: 1}}
	if err := svc.PublishCouponBatch(ctx, batch.ID, apps, grants, time.Now()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	var got model.Coupon
	testDB.Where("batch_id = ? AND user_id = ?", batch.ID, yuki).Take(&got)
	if got.Code != "BIG-A" {
		t.Errorf("the soonest-expiring code goes out first: got %q", got.Code)
	}
	var kept int64
	testDB.Model(&model.Coupon{}).Where("batch_id = ? AND user_id IS NULL", batch.ID).Count(&kept)
	if kept != 30 {
		t.Errorf("kept back %d coupons, want 30", kept)
	}
	var shares []model.CouponShare
	testDB.Where("batch_id = ?", batch.ID).Order("user_id").Find(&shares)
	var names []string
	for _, s := range shares {
		names = append(names, s.UserName)
	}
	if strings.Join(names, ",") != "鲲,永雏小夜" || shares[1].AllocatedPoints != 5000 || shares[1].EntitledPoints != 77 {
		t.Errorf("shares = %+v, want every eligible owner with clicks, frozen with what they received", shares)
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
	if usage.Uniques != 3060+3357+7+900+40 || usage.Bots != 250 {
		t.Fatalf("totals = %d uniques / %d bots, want 7364 / 250", usage.Uniques, usage.Bots)
	}
	if len(usage.Daily) != 30 {
		t.Errorf("daily = %d, want 30 dense days", len(usage.Daily))
	}
	if len(usage.ByApp) != 5 || usage.ByApp[0].ClientID != "patch" || usage.ByApp[0].OwnerName != "鲲" {
		t.Fatalf("by_app = %+v, want five sites, most uniques first, with their owners", usage.ByApp)
	}
	var ppm int64
	for _, a := range usage.ByApp {
		ppm += a.SharePPM
	}
	if ppm < 999_998 || ppm > 1_000_002 {
		t.Errorf("shares sum to %d ppm", ppm)
	}
	if len(usage.TopLinks) != 5 {
		t.Errorf("top links = %d, want 5", len(usage.TopLinks))
	}
}
