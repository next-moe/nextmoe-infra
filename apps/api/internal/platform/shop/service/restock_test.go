package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"api/internal/platform/notice"
	"api/internal/platform/shop/model"
	"api/pkg/errors"
)

func noticesFor(t *testing.T, users ...uint) []notice.Notice {
	t.Helper()
	var rows []notice.Notice
	if err := testDB.Where("user_id IN ?", users).Order("id").Find(&rows).Error; err != nil {
		t.Fatalf("notices: %v", err)
	}
	return rows
}

func siteDomain(t *testing.T, site uint) string {
	t.Helper()
	var domain string
	if err := testDB.Table("sites").Where("id = ?", site).Pluck("domain", &domain).Error; err != nil {
		t.Fatalf("site domain: %v", err)
	}
	return domain
}

func TestARefilledPoolTellsTheSubscribersWhoCanStillBuy(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	site := f.site()
	item := f.plainItem(model.KindRedeemCode, "DLsite 1000 円券")
	f.addCodes(item, nil, "R-1", "R-2", "R-3")
	offer := f.offer(OfferInput{Price: 100, PerUserLimit: 2, LimitPeriod: model.LimitMonth, Rewards: []model.Reward{{ItemID: item}}})

	full, waiting, gone, quiet := f.user(1000), f.user(1000), f.user(1000), f.user(1000)
	for _, u := range []uint{full, waiting, gone, waiting} {
		if err := f.shop.SubscribeRestock(ctx, u, offer, site, "kungal"); err != nil {
			t.Fatalf("subscribe %d: %v", u, err)
		}
	}
	testDB.Exec(`UPDATE users SET anonymized_at = now() WHERE id = ?`, gone)
	for _, k := range []string{"a", "b"} {
		if _, err := f.buy(full, offer, k); err != nil {
			t.Fatalf("buy %s: %v", k, err)
		}
	}
	if _, err := f.buy(waiting, offer, "c"); err != nil {
		t.Fatalf("buy the last code: %v", err)
	}
	if got := f.remaining(offer); got != 0 {
		t.Fatalf("remaining = %d after the pool ran out", got)
	}
	if pool, err := f.shop.CodePool(ctx, item); err != nil || pool.Waiting != 3 {
		t.Fatalf("pool waiting: %+v %v", pool, err)
	}
	if got := noticesFor(t, full, waiting, gone, quiet); len(got) != 0 {
		t.Fatalf("selling out raised notices: %+v", got)
	}

	before := time.Now()
	f.addCodes(item, nil, "R-4", "R-5")
	got := noticesFor(t, full, waiting, gone, quiet)
	if len(got) != 1 {
		t.Fatalf("refill raised %d notices, want one for the subscriber under the limit: %+v", len(got), got)
	}
	n := got[0]
	if n.UserID != int64(waiting) || n.Site != "kungal" || n.Topic != NoticeTopicRestock ||
		n.Ref != fmt.Sprintf("offer:%d", offer) || !strings.Contains(n.Title, "「DLsite 1000 円券」") ||
		n.URL != "https://"+siteDomain(t, site)+"/shop" {
		t.Fatalf("notice: %+v", n)
	}

	f.addCodes(item, nil, "R-6")
	if got := noticesFor(t, full, waiting, gone, quiet); len(got) != 1 {
		t.Fatalf("topping up a pool still on sale notified again: %+v", got)
	}

	catalog, err := f.shop.Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(catalog, func(v OfferView) bool { return v.ID == offer })
	if i < 0 || catalog[i].Sold != 3 || catalog[i].RestockedAt == nil || catalog[i].RestockedAt.Before(before) {
		t.Fatalf("catalog offer: %+v", catalog[i])
	}

	inv, err := f.shop.Inventory(ctx, waiting)
	if err != nil || !slices.Equal(inv.RestockAlerts, []int64{offer}) {
		t.Fatalf("inventory alerts: %v %v", inv.RestockAlerts, err)
	}
	if err := f.shop.UnsubscribeRestock(ctx, waiting, offer); err != nil {
		t.Fatal(err)
	}
	if err := f.shop.UnsubscribeRestock(ctx, waiting, offer); err != nil {
		t.Fatalf("unsubscribing twice: %v", err)
	}
	if inv, err = f.shop.Inventory(ctx, waiting); err != nil || len(inv.RestockAlerts) != 0 {
		t.Fatalf("inventory alerts after unsubscribing: %v %v", inv.RestockAlerts, err)
	}
}

func TestAStockedOfferBackOnSaleTellsItsSubscribers(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	here, there := f.site(), f.site()
	u, buyer := f.user(1000), f.user(1000)

	unlimited := f.offer(OfferInput{Price: 100, Rewards: []model.Reward{{ItemID: f.plainItem(model.KindProfileAbout, "主页介绍")}}})
	wantCode(t, f.shop.SubscribeRestock(ctx, u, unlimited, here, "kungal"), errors.ErrShopInvalidOffer, "an unlimited offer")

	stock := 1
	in := OfferInput{SiteID: &there, Price: 100, Stock: &stock, Rewards: []model.Reward{{ItemID: f.background("限量背景", &there)}}}
	offer := f.offer(in)
	wantCode(t, f.shop.SubscribeRestock(ctx, u, offer, here, "kungal"), errors.ErrShopOfferUnavailable, "another site's offer")
	wantCode(t, f.shop.SubscribeRestock(ctx, u, offer, there, ""), errors.ErrShopNotStorefront, "a site with no inbox")
	if err := f.shop.SubscribeRestock(ctx, u, offer, there, "letmoe"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.shop.Purchase(ctx, buyer, offer, "only", there); err != nil {
		t.Fatalf("buy the only copy: %v", err)
	}

	stock = 2
	if _, err := f.shop.UpdateOffer(ctx, offer, in, true); err != nil {
		t.Fatal(err)
	}
	got := noticesFor(t, u, buyer)
	if len(got) != 1 || got[0].UserID != int64(u) || got[0].Site != "letmoe" ||
		got[0].URL != "https://"+siteDomain(t, there)+"/shop" {
		t.Fatalf("raising the stock: %+v", got)
	}

	if _, err := f.shop.TransitionOffer(ctx, offer, OfferRetire); err != nil {
		t.Fatal(err)
	}
	wantCode(t, f.shop.SubscribeRestock(ctx, buyer, offer, there, "letmoe"), errors.ErrShopOfferUnavailable, "a retired offer")
	if got := noticesFor(t, u); len(got) != 1 {
		t.Fatalf("retiring notified: %+v", got)
	}
	if _, err := f.shop.TransitionOffer(ctx, offer, OfferActivate); err != nil {
		t.Fatal(err)
	}
	if got := noticesFor(t, u); len(got) != 2 {
		t.Fatalf("relisting with a copy left: %+v", got)
	}
}
