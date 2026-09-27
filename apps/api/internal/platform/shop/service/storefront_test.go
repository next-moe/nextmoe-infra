package service

import (
	"context"
	"slices"
	"testing"
	"time"

	"api/internal/platform/shop/model"
	"api/pkg/errors"
)

func TestASiteStorefrontSellsTheSharedOffersAndItsOwn(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	here, there := f.site(), f.site()
	shared := f.offer(OfferInput{Price: 100, Rewards: []model.Reward{{ItemID: f.frame("全站框")}}})
	own := f.offer(OfferInput{SiteID: &here, Price: 120, Rewards: []model.Reward{{ItemID: f.background("本站背景", &here)}}})
	other := f.offer(OfferInput{SiteID: &there, Price: 130, Rewards: []model.Reward{{ItemID: f.background("别站背景", &there)}}})

	front, err := f.shop.Storefront(ctx, here)
	if err != nil {
		t.Fatal(err)
	}
	if front.Site.ID != here || front.Site.Name == "" {
		t.Fatalf("the storefront names its site as %+v", front.Site)
	}
	var listed []int64
	for _, o := range front.Offers {
		listed = append(listed, o.ID)
	}
	if !slices.Contains(listed, shared) || !slices.Contains(listed, own) || slices.Contains(listed, other) {
		t.Fatalf("storefront lists %v; want %d and %d but not %d", listed, shared, own, other)
	}

	u := f.user(1000)
	for _, o := range []int64{shared, own} {
		p, err := f.shop.Purchase(ctx, u, o, "here-"+time.Now().String(), here)
		if err != nil {
			t.Fatalf("buy offer %d at the site: %v", o, err)
		}
		if p.Order.SiteID != here {
			t.Fatalf("order placed at site %d, want %d", p.Order.SiteID, here)
		}
	}
	_, err = f.shop.Purchase(ctx, u, other, "there", here)
	wantCode(t, err, errors.ErrShopOfferUnavailable, "another site's offer bought at this site")
	if _, err := f.buy(u, other, "there"); err != nil {
		t.Fatalf("the account center still sells every site's offer: %v", err)
	}
}

func TestTheInventoryCountsPurchasesTowardEachLimit(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	item := f.plainItem(model.KindRedeemCode, "DLsite 1000 円券")
	f.addCodes(item, nil, "L-1", "L-2", "L-3")
	monthly := f.offer(OfferInput{Price: 100, PerUserLimit: 2, LimitPeriod: model.LimitMonth, Rewards: []model.Reward{{ItemID: item}}})
	unlimited := f.offer(OfferInput{Price: 100, Rewards: []model.Reward{{ItemID: f.plainItem(model.KindProfileAbout, "主页介绍")}}})

	f.clock = time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	u := f.user(1000)
	for _, k := range []string{"a", "b"} {
		if _, err := f.buy(u, monthly, k); err != nil {
			t.Fatalf("purchase %s: %v", k, err)
		}
	}
	if _, err := f.buy(u, unlimited, "c"); err != nil {
		t.Fatalf("purchase c: %v", err)
	}
	inv, err := f.shop.Inventory(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	if inv.LimitUsed[monthly] != 2 || len(inv.LimitUsed) != 1 {
		t.Fatalf("limit_used %v, want only offer %d at 2", inv.LimitUsed, monthly)
	}

	f.clock = time.Date(2026, 9, 30, 16, 30, 0, 0, time.UTC)
	if inv, err = f.shop.Inventory(ctx, u); err != nil {
		t.Fatal(err)
	}
	if len(inv.LimitUsed) != 0 {
		t.Fatalf("limit_used %v in a new month, want none", inv.LimitUsed)
	}
}
