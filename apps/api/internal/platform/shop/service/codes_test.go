package service

import (
	"context"
	"slices"
	"testing"
	"time"

	"api/internal/platform/shop/model"
	"api/pkg/errors"
)

func (f *fixture) plainItem(kind, name string) int64 {
	f.t.Helper()
	ctx := context.Background()
	it, err := f.shop.CreateItem(ctx, ItemInput{Kind: kind, Name: name}, 1)
	if err != nil {
		f.t.Fatalf("create %s: %v", kind, err)
	}
	if _, err := f.shop.TransitionItem(ctx, it.ID, ItemPublish); err != nil {
		f.t.Fatalf("publish %s: %v", kind, err)
	}
	f.t.Cleanup(func() {
		testDB.Exec(`DELETE FROM shop_codes WHERE item_id = ?`, it.ID)
		testDB.Exec(`DELETE FROM shop_entitlements WHERE item_id = ?`, it.ID)
		testDB.Exec(`DELETE FROM shop_items WHERE id = ?`, it.ID)
	})
	return it.ID
}

func (f *fixture) addCodes(item int64, expires *string, codes ...string) {
	f.t.Helper()
	if _, err := f.shop.AddCodes(context.Background(), AddCodesInput{ItemID: item, Codes: codes, ExpiresOn: expires}, 1); err != nil {
		f.t.Fatalf("add codes: %v", err)
	}
}

func day(t time.Time, days int) *string {
	s := t.In(codeZone).AddDate(0, 0, days).Format(time.DateOnly)
	return &s
}

func (f *fixture) remaining(offer int64) int {
	f.t.Helper()
	offers, err := f.shop.ListOffers(context.Background(), "")
	if err != nil {
		f.t.Fatalf("list offers: %v", err)
	}
	for _, o := range offers {
		if o.ID == offer {
			if o.Remaining == nil {
				f.t.Fatalf("offer %d has no remaining count", offer)
			}
			return *o.Remaining
		}
	}
	f.t.Fatalf("offer %d not listed", offer)
	return 0
}

func TestRedeemCodesSellFromThePoolUntilItRunsOut(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	item := f.plainItem(model.KindRedeemCode, "DLsite 1000 円券")
	f.addCodes(item, day(f.clock, 30), "LATE-1")
	f.addCodes(item, day(f.clock, 10), "SOON-1")
	f.addCodes(item, day(f.clock, 1), "TOO-SOON")
	f.addCodes(item, day(f.clock, -1), "EXPIRED")

	added, err := f.shop.AddCodes(ctx, AddCodesInput{ItemID: item, Codes: []string{" LATE-1 ", "NEW-1", "NEW-1"}}, 1)
	if err != nil || added.Added != 1 || !slices.Equal(added.Duplicates, []string{"LATE-1"}) {
		t.Fatalf("re-adding a known code: %+v %v", added, err)
	}
	testDB.Exec(`DELETE FROM shop_codes WHERE code = 'NEW-1'`)

	_, err = f.shop.CreateOffer(ctx, OfferInput{Price: 100, Rewards: []model.Reward{{ItemID: item}, {ItemID: f.frame("框")}}}, 1)
	wantCode(t, err, errors.ErrShopInvalidOffer, "a code bundled with another item")
	stock := 5
	_, err = f.shop.CreateOffer(ctx, OfferInput{Price: 100, Stock: &stock, Rewards: []model.Reward{{ItemID: item}}}, 1)
	wantCode(t, err, errors.ErrShopInvalidOffer, "a code offer with a hand-typed stock")

	offer := f.offer(OfferInput{Price: 100, Rewards: []model.Reward{{ItemID: item}}})
	if got := f.remaining(offer); got != 2 {
		t.Fatalf("remaining = %d, want the 2 codes with shelf life left", got)
	}

	u := f.user(1000)
	first, err := f.buy(u, offer, "c1")
	if err != nil || len(first.Order.Codes) != 1 || first.Order.Codes[0].Code != "SOON-1" {
		t.Fatalf("first purchase: %+v %v", first, err)
	}
	replay, err := f.buy(u, offer, "c1")
	if err != nil || !replay.Replay || len(replay.Order.Codes) != 1 || replay.Order.Codes[0].Code != "SOON-1" {
		t.Fatalf("a retried purchase: %+v %v", replay, err)
	}
	second, err := f.buy(u, offer, "c2")
	if err != nil || second.Order.Codes[0].Code != "LATE-1" || second.Balance != 800 {
		t.Fatalf("second purchase: %+v %v", second, err)
	}
	_, err = f.buy(u, offer, "c3")
	wantCode(t, err, errors.ErrShopSoldOut, "buying from an empty pool")
	if f.balance(u) != 800 || f.remaining(offer) != 0 {
		t.Fatalf("a sold-out purchase moved the balance to %d", f.balance(u))
	}

	inv, err := f.shop.Inventory(ctx, u)
	if err != nil || len(inv.Items) != 0 || len(inv.Orders) != 2 || len(inv.Orders[0].Codes) != 1 {
		t.Fatalf("inventory: %+v %v", inv, err)
	}
	_, err = f.shop.Refund(ctx, first.Order.ID, 1, "")
	wantCode(t, err, errors.ErrShopInvalidTransition, "refunding a code that was handed out")
	err = f.shop.Grant(ctx, u, item, 0, "", 1)
	wantCode(t, err, errors.ErrShopInvalidItem, "granting a code item")

	pool, err := f.shop.CodePool(ctx, item)
	if err != nil || pool.Total != 4 || pool.Sold != 2 || pool.Sellable != 0 {
		t.Fatalf("pool: %+v %v", pool, err)
	}
	var sold model.Code
	testDB.Where("code = ?", "LATE-1").First(&sold)
	wantCode(t, f.shop.DeleteCode(ctx, sold.ID), errors.ErrShopInvalidTransition, "deleting a sold code")
}

func TestAMonthlyLimitStartsOverWithTheMonth(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	item := f.plainItem(model.KindRedeemCode, "DLsite 5000 円券")
	f.addCodes(item, nil, "M-1", "M-2", "M-3", "M-4")

	_, err := f.shop.CreateOffer(ctx, OfferInput{Price: 300, LimitPeriod: model.LimitMonth, Rewards: []model.Reward{{ItemID: item}}}, 1)
	wantCode(t, err, errors.ErrShopInvalidOffer, "a monthly limit of nothing")
	offer := f.offer(OfferInput{Price: 300, PerUserLimit: 2, LimitPeriod: model.LimitMonth, Rewards: []model.Reward{{ItemID: item}}})

	f.clock = time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	u := f.user(2000)
	for _, k := range []string{"m1", "m2"} {
		if _, err := f.buy(u, offer, k); err != nil {
			t.Fatalf("purchase %s: %v", k, err)
		}
	}
	_, err = f.buy(u, offer, "m3")
	wantCode(t, err, errors.ErrShopLimitReached, "a third code in the same month")

	f.clock = time.Date(2026, 9, 30, 16, 30, 0, 0, time.UTC)
	if _, err := f.buy(u, offer, "m3"); err != nil {
		t.Fatalf("the first purchase of October (00:30 Beijing time): %v", err)
	}
}

func TestAPerkIsOwnedButNeverWorn(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, err := f.shop.UploadAsset(ctx, model.KindProfileAbout, framePNG(t, 384, false), 1)
	wantCode(t, err, errors.ErrShopInvalidAsset, "art for a perk")

	perk := f.plainItem(model.KindProfileAbout, "主页介绍")
	offer := f.offer(OfferInput{Price: 100, Rewards: []model.Reward{{ItemID: perk}}})
	u, other := f.user(300), f.user(300)
	if _, err := f.buy(u, offer, "p1"); err != nil {
		t.Fatalf("buy the perk: %v", err)
	}
	_, err = f.buy(u, offer, "p2")
	wantCode(t, err, errors.ErrShopAlreadyOwned, "buying a permanent perk twice")

	held, err := f.shop.PerksFor(ctx, []uint{u, other})
	if err != nil || !slices.Equal(held[u], []string{model.KindProfileAbout}) || held[other] != nil {
		t.Fatalf("perks: %+v %v", held, err)
	}
	for _, slot := range []string{"", model.KindProfileAbout} {
		err = f.shop.Equip(ctx, u, slot, model.EverySite, &perk)
		wantCode(t, err, errors.ErrShopInvalidItem, "wearing a perk in slot "+slot)
	}

	var e model.Entitlement
	testDB.Where("user_id = ? AND item_id = ?", u, perk).First(&e)
	if err := f.shop.Revoke(ctx, e.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	held, _ = f.shop.PerksFor(ctx, []uint{u})
	if held[u] != nil {
		t.Fatalf("a revoked perk still counts: %+v", held)
	}
}
