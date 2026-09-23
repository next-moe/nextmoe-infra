package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	authModel "api/internal/platform/auth/model"
	"api/internal/platform/ledger/ledgertest"
	ledgerModel "api/internal/platform/ledger/model"
	ledgerService "api/internal/platform/ledger/service"
	"api/internal/platform/shop/model"
	siteModel "api/internal/platform/site/model"
	"api/internal/testsupport/dbtest"
	"api/pkg/errors"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		os.Exit(m.Run())
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("shop suite", "connect: %v", err)
	}
	if err := ledgertest.Migrate(db); err != nil {
		dbtest.SkipMainf("shop suite", "migrate ledger: %v", err)
	}
	if err := db.AutoMigrate(append([]any{&siteModel.Site{}}, model.AllModels()...)...); err != nil {
		dbtest.SkipMainf("shop suite", "migrate shop: %v", err)
	}
	testDB = db
	os.Exit(m.Run())
}

type memStore struct{ puts atomic.Int64 }

func (m *memStore) PutWithCacheControl(context.Context, string, []byte, string, string) error {
	m.puts.Add(1)
	return nil
}

type fixture struct {
	t      *testing.T
	shop   *Shop
	ledger *ledgerService.Ledger
	clock  time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	if testDB == nil {
		dbtest.Skip(t)
	}
	l := ledgerService.New(testDB)
	f := &fixture{t: t, ledger: l, clock: time.Now()}
	f.shop = New(testDB, l, &memStore{}, "https://cdn.test")
	f.shop.now = func() time.Time { return f.clock }
	return f
}

var seq atomic.Int64

func (f *fixture) user(balance int64) uint {
	f.t.Helper()
	tag := strconv.FormatInt(time.Now().UnixNano(), 36) + strconv.FormatInt(seq.Add(1), 36)
	u := &authModel.User{Name: "sh" + tag[len(tag)-10:], Email: "sh-" + tag + "@test.local"}
	if err := testDB.Create(u).Error; err != nil {
		f.t.Fatalf("create user: %v", err)
	}
	f.t.Cleanup(func() {
		testDB.Exec(`DELETE FROM shop_loadouts WHERE user_id = ?`, u.ID)
		testDB.Exec(`DELETE FROM shop_entitlements WHERE user_id = ?`, u.ID)
		testDB.Exec(`DELETE FROM shop_orders WHERE user_id = ?`, u.ID)
		_ = ledgertest.Purge(testDB, u.ID)
		testDB.Exec(`DELETE FROM users WHERE id = ?`, u.ID)
	})
	ledgertest.Fund(f.t, f.ledger, u.ID, balance)
	return u.ID
}

func (f *fixture) frame(name string) int64 {
	f.t.Helper()
	ctx := context.Background()
	static, err := f.shop.UploadAsset(ctx, model.KindAvatarFrame, framePNG(f.t, 384, false), 1)
	if err != nil {
		f.t.Fatalf("upload static: %v", err)
	}
	anim, err := f.shop.UploadAsset(ctx, model.KindAvatarFrame, animatedWebPHeader(384, 0x12), 1)
	if err != nil {
		f.t.Fatalf("upload animated: %v", err)
	}
	render, _ := json.Marshal(model.Render{Static: static.Key, Animated: anim.Key})
	it, err := f.shop.CreateItem(ctx, ItemInput{Kind: model.KindAvatarFrame, Name: name, Render: render}, 1)
	if err != nil {
		f.t.Fatalf("create item: %v", err)
	}
	if _, err := f.shop.TransitionItem(ctx, it.ID, ItemPublish); err != nil {
		f.t.Fatalf("publish item: %v", err)
	}
	f.t.Cleanup(func() {
		testDB.Exec(`DELETE FROM shop_entitlements WHERE item_id = ?`, it.ID)
		testDB.Exec(`DELETE FROM shop_items WHERE id = ?`, it.ID)
	})
	return it.ID
}

func (f *fixture) background(name string, siteID *uint) int64 {
	f.t.Helper()
	ctx := context.Background()
	static, err := f.shop.UploadAsset(ctx, model.KindProfileBackground, bannerJPEG(f.t, 1500, 500), 1)
	if err != nil {
		f.t.Fatalf("upload banner: %v", err)
	}
	render, _ := json.Marshal(model.Render{Static: static.Key})
	it, err := f.shop.CreateItem(ctx, ItemInput{Kind: model.KindProfileBackground, SiteID: siteID, Name: name, Render: render}, 1)
	if err != nil {
		f.t.Fatalf("create background: %v", err)
	}
	if _, err := f.shop.TransitionItem(ctx, it.ID, ItemPublish); err != nil {
		f.t.Fatalf("publish background: %v", err)
	}
	f.t.Cleanup(func() {
		testDB.Exec(`DELETE FROM shop_entitlements WHERE item_id = ?`, it.ID)
		testDB.Exec(`DELETE FROM shop_items WHERE id = ?`, it.ID)
	})
	return it.ID
}

func (f *fixture) site() uint {
	f.t.Helper()
	site := siteModel.Site{Name: "t" + strconv.FormatInt(seq.Add(1), 10), Domain: fmt.Sprintf("s%d.test.local", time.Now().UnixNano())}
	if err := testDB.Create(&site).Error; err != nil {
		f.t.Fatalf("create site: %v", err)
	}
	f.t.Cleanup(func() { testDB.Exec(`DELETE FROM sites WHERE id = ?`, site.ID) })
	return site.ID
}

func (f *fixture) offer(in OfferInput) int64 {
	f.t.Helper()
	o, err := f.shop.CreateOffer(context.Background(), in, 1)
	if err != nil {
		f.t.Fatalf("create offer: %v", err)
	}
	if _, err := f.shop.TransitionOffer(context.Background(), o.ID, OfferActivate); err != nil {
		f.t.Fatalf("activate offer: %v", err)
	}
	f.t.Cleanup(func() { testDB.Exec(`DELETE FROM shop_offers WHERE id = ?`, o.ID) })
	return o.ID
}

func (f *fixture) buy(user uint, offer int64, key string) (*Purchased, error) {
	return f.shop.Purchase(context.Background(), user, offer, key)
}

func (f *fixture) balance(user uint) int64 {
	f.t.Helper()
	b, err := f.ledger.UserBalance(context.Background(), user)
	if err != nil {
		f.t.Fatalf("balance: %v", err)
	}
	return b
}

func (f *fixture) worn(user uint, site uint) *model.Decoration {
	f.t.Helper()
	c, err := f.shop.CosmeticsFor(context.Background(), []uint{user}, site)
	if err != nil {
		f.t.Fatalf("cosmetics: %v", err)
	}
	return c[user][model.SlotAvatarFrame]
}

func wantCode(t *testing.T, err error, code int, what string) {
	t.Helper()
	if !errors.Is(err, code) {
		t.Fatalf("%s: got %v, want code %d", what, err, code)
	}
}

func TestOfferRespectsTheMinimumPriceAndPublishedItems(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	item := f.frame("樱花")
	_, err := f.shop.CreateOffer(ctx, OfferInput{Price: 99, Rewards: []model.Reward{{ItemID: item}}}, 1)
	wantCode(t, err, errors.ErrShopPriceBelowMinimum, "a price under shop.min_price")

	render, _ := json.Marshal(model.Render{Static: "decorations/missing.png"})
	_, err = f.shop.CreateItem(ctx, ItemInput{Kind: model.KindAvatarFrame, Name: "x", Render: render}, 1)
	wantCode(t, err, errors.ErrShopInvalidItem, "an item whose art was never uploaded")

	static, _ := f.shop.UploadAsset(ctx, model.KindAvatarFrame, framePNG(t, 384, false), 1)
	render, _ = json.Marshal(model.Render{Static: static.Key})
	draft, err := f.shop.CreateItem(ctx, ItemInput{Kind: model.KindAvatarFrame, Name: "草稿", Render: render}, 1)
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	t.Cleanup(func() { testDB.Exec(`DELETE FROM shop_items WHERE id = ?`, draft.ID) })
	o, err := f.shop.CreateOffer(ctx, OfferInput{Price: 100, Rewards: []model.Reward{{ItemID: draft.ID}}}, 1)
	if err != nil {
		t.Fatalf("create offer on a draft: %v", err)
	}
	t.Cleanup(func() { testDB.Exec(`DELETE FROM shop_offers WHERE id = ?`, o.ID) })
	_, err = f.shop.TransitionOffer(ctx, o.ID, OfferActivate)
	wantCode(t, err, errors.ErrShopInvalidOffer, "putting a draft item on sale")

	anim, _ := f.shop.UploadAsset(ctx, model.KindAvatarFrame, animatedWebPHeader(384, 0x12), 1)
	moved, _ := json.Marshal(model.Render{Static: static.Key, Animated: anim.Key})
	if _, err := f.shop.TransitionItem(ctx, draft.ID, ItemPublish); err != nil {
		t.Fatalf("publish: %v", err)
	}
	_, err = f.shop.UpdateItem(ctx, draft.ID, ItemInput{Name: "草稿", Render: moved})
	wantCode(t, err, errors.ErrShopInvalidTransition, "swapping a published item's art")
	err = f.shop.DeleteItem(ctx, draft.ID)
	wantCode(t, err, errors.ErrShopInvalidTransition, "deleting an item that was published")
}

func TestPurchaseChargesOnceAndGrantsTheItem(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	item := f.frame("星轨")
	offer := f.offer(OfferInput{Price: 120, Rewards: []model.Reward{{ItemID: item}}})

	poor := f.user(119)
	_, err := f.buy(poor, offer, "k1")
	wantCode(t, err, errors.ErrMoemoepointInsufficient, "a purchase the user cannot pay")
	var orders int64
	testDB.Model(&model.Order{}).Where("user_id = ?", poor).Count(&orders)
	if orders != 0 || f.balance(poor) != 119 {
		t.Fatalf("a refused purchase left %d orders and balance %d", orders, f.balance(poor))
	}

	u := f.user(300)
	got, err := f.buy(u, offer, "k1")
	if err != nil || got.Balance != 180 || got.Replay || got.Order.Price != 120 {
		t.Fatalf("purchase: %+v %v", got, err)
	}
	again, err := f.buy(u, offer, "k1")
	if err != nil || !again.Replay || again.Order.ID != got.Order.ID || f.balance(u) != 180 {
		t.Fatalf("a retried purchase: %+v %v balance %d", again, err, f.balance(u))
	}
	_, err = f.buy(u, offer, "k2")
	wantCode(t, err, errors.ErrShopAlreadyOwned, "buying a permanent item twice")
	if f.balance(u) != 180 {
		t.Fatalf("a refused second purchase moved the balance to %d", f.balance(u))
	}

	var tr ledgerModel.Transfer
	testDB.First(&tr, *got.Order.TransferID)
	if tr.Reason != ledgerModel.ReasonPurchase || tr.SourceApp != "shop" {
		t.Fatalf("ledger transfer: %+v", tr)
	}
	inv, err := f.shop.Inventory(ctx, u)
	if err != nil || len(inv.Items) != 1 || !inv.Items[0].Active || inv.Items[0].ExpiresAt != nil {
		t.Fatalf("inventory: %+v %v", inv, err)
	}
}

func TestEquipIsSiteScopedAndNeedsOwnership(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.frame("猫耳")
	b := f.frame("月桂")
	oa := f.offer(OfferInput{Price: 100, Rewards: []model.Reward{{ItemID: a}}})
	u := f.user(1000)

	wantCode(t, f.shop.Equip(ctx, u, model.SlotAvatarFrame, model.EverySite, &a), errors.ErrShopNotOwned, "wearing an unowned item")
	if _, err := f.buy(u, oa, "a"); err != nil {
		t.Fatalf("buy a: %v", err)
	}
	if err := f.shop.Grant(ctx, u, b, 0, "test", 1); err != nil {
		t.Fatalf("grant b: %v", err)
	}
	site := siteModel.Site{Name: "t" + strconv.FormatInt(seq.Add(1), 10), Domain: fmt.Sprintf("s%d.test.local", time.Now().UnixNano())}
	if err := testDB.Create(&site).Error; err != nil {
		t.Fatalf("create site: %v", err)
	}
	t.Cleanup(func() { testDB.Exec(`DELETE FROM sites WHERE id = ?`, site.ID) })

	if err := f.shop.Equip(ctx, u, model.SlotAvatarFrame, model.EverySite, &a); err != nil {
		t.Fatalf("equip a everywhere: %v", err)
	}
	if err := f.shop.Equip(ctx, u, model.SlotAvatarFrame, site.ID, &b); err != nil {
		t.Fatalf("equip b on one site: %v", err)
	}
	if d := f.worn(u, model.EverySite); d == nil || d.ItemID != a || d.StaticURL == "" || d.AnimatedURL == "" {
		t.Fatalf("everywhere shows %+v, want item %d with both URLs", d, a)
	}
	if d := f.worn(u, site.ID); d == nil || d.ItemID != b {
		t.Fatalf("the site shows %+v, want its own choice %d", d, b)
	}
	if d := f.worn(u, site.ID+100000); d == nil || d.ItemID != a {
		t.Fatalf("another site shows %+v, want the default %d", d, a)
	}
	if err := f.shop.Equip(ctx, u, model.SlotAvatarFrame, site.ID, nil); err != nil {
		t.Fatalf("unequip: %v", err)
	}
	if d := f.worn(u, site.ID); d == nil || d.ItemID != a {
		t.Fatalf("after clearing the site choice it shows %+v, want the default", d)
	}

	if _, err := f.shop.TransitionItem(ctx, a, ItemRetire); err != nil {
		t.Fatalf("retire: %v", err)
	}
	if d := f.worn(u, model.EverySite); d == nil || d.ItemID != a {
		t.Fatalf("a retired item must stay on its owners, got %+v", d)
	}
	cat, _ := f.shop.Catalog(ctx)
	for _, o := range cat {
		if o.ID == oa {
			t.Fatal("an offer for a retired item is still in the catalog")
		}
	}
}

func TestTimedItemsExtendAndLapse(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	item := f.frame("限时")
	offer := f.offer(OfferInput{Price: 100, Rewards: []model.Reward{{ItemID: item, DurationDays: 7}}})
	u := f.user(500)
	start := f.clock
	for _, k := range []string{"t1", "t2"} {
		if _, err := f.buy(u, offer, k); err != nil {
			t.Fatalf("buy %s: %v", k, err)
		}
	}
	var e model.Entitlement
	testDB.Where("user_id = ? AND item_id = ?", u, item).First(&e)
	if e.ExpiresAt == nil || e.ExpiresAt.Sub(start) < 14*24*time.Hour-time.Minute {
		t.Fatalf("two 7-day purchases expire at %v, want 14 days after %v", e.ExpiresAt, start)
	}
	if err := f.shop.Equip(ctx, u, model.SlotAvatarFrame, model.EverySite, &item); err != nil {
		t.Fatalf("equip: %v", err)
	}
	f.clock = start.Add(15 * 24 * time.Hour)
	if d := f.worn(u, model.EverySite); d != nil {
		t.Fatalf("a lapsed item is still worn: %+v", d)
	}
}

func TestRefundReturnsPointsAndTakesTheItemBack(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	item := f.frame("爱心")
	offer := f.offer(OfferInput{Price: 150, Rewards: []model.Reward{{ItemID: item}}})
	u := f.user(150)
	got, err := f.buy(u, offer, "r1")
	if err != nil {
		t.Fatalf("buy: %v", err)
	}
	if err := f.shop.Equip(ctx, u, model.SlotAvatarFrame, model.EverySite, &item); err != nil {
		t.Fatalf("equip: %v", err)
	}
	if _, err := f.shop.Refund(ctx, got.Order.ID, 1, "test"); err != nil {
		t.Fatalf("refund: %v", err)
	}
	if f.balance(u) != 150 || f.worn(u, model.EverySite) != nil {
		t.Fatalf("after refund: balance %d, worn %+v", f.balance(u), f.worn(u, model.EverySite))
	}
	_, err = f.shop.Refund(ctx, got.Order.ID, 1, "again")
	wantCode(t, err, errors.ErrShopInvalidTransition, "refunding twice")
	if _, err := f.buy(u, offer, "r2"); err != nil {
		t.Fatalf("buying again after a refund: %v", err)
	}
}

func TestStockIsNeverOversoldUnderConcurrency(t *testing.T) {
	f := newFixture(t)
	item := f.frame("限量")
	stock := 3
	offer := f.offer(OfferInput{Price: 100, Rewards: []model.Reward{{ItemID: item}}, Stock: &stock})
	var ok, soldOut atomic.Int64
	var wg sync.WaitGroup
	for i := range 8 {
		u := f.user(100)
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.buy(u, offer, "s"+strconv.Itoa(i))
			switch {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, errors.ErrShopSoldOut):
				soldOut.Add(1)
			default:
				t.Errorf("buyer %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 3 || soldOut.Load() != 5 {
		t.Fatalf("%d sold and %d refused, want 3 and 5", ok.Load(), soldOut.Load())
	}
}

func TestOneUserBuyingTwiceAtOnceIsChargedOnce(t *testing.T) {
	f := newFixture(t)
	item := f.frame("并发")
	offer := f.offer(OfferInput{Price: 100, Rewards: []model.Reward{{ItemID: item}}})
	u := f.user(1000)
	var ok atomic.Int64
	var wg sync.WaitGroup
	for i := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.buy(u, offer, "c"+strconv.Itoa(i))
			if err == nil {
				ok.Add(1)
			} else if !errors.Is(err, errors.ErrShopAlreadyOwned) {
				t.Errorf("attempt %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || f.balance(u) != 900 {
		t.Fatalf("%d purchases went through and the balance is %d, want 1 and 900", ok.Load(), f.balance(u))
	}
}

// Two offers lock two different rows, so only the entitlement check inside
// the grant stands between a user and paying twice for the same item.
func TestTheSameItemInTwoOffersIsBoughtOnce(t *testing.T) {
	f := newFixture(t)
	item := f.frame("双店")
	offers := []int64{
		f.offer(OfferInput{Price: 100, Rewards: []model.Reward{{ItemID: item}}}),
		f.offer(OfferInput{Price: 110, Rewards: []model.Reward{{ItemID: item}}}),
	}
	u := f.user(1000)
	var ok atomic.Int64
	var wg sync.WaitGroup
	for i := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.buy(u, offers[i%2], "x"+strconv.Itoa(i))
			if err == nil {
				ok.Add(1)
			} else if !errors.Is(err, errors.ErrShopAlreadyOwned) {
				t.Errorf("attempt %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	if b := f.balance(u); ok.Load() != 1 || (b != 900 && b != 890) {
		t.Fatalf("%d purchases went through and the balance is %d, want exactly one", ok.Load(), b)
	}
}

func TestRefundTakesBackOnlyWhatTheOrderBought(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	item := f.frame("续期")
	timed := f.offer(OfferInput{Price: 100, Rewards: []model.Reward{{ItemID: item, DurationDays: 30}}})
	forever := f.offer(OfferInput{Price: 300, Rewards: []model.Reward{{ItemID: item}}})
	u := f.user(1000)
	start := f.clock

	if _, err := f.buy(u, timed, "first"); err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := f.buy(u, timed, "second")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if _, err := f.shop.Refund(ctx, second.Order.ID, 1, "t"); err != nil {
		t.Fatalf("refund second: %v", err)
	}
	var e model.Entitlement
	testDB.Where("user_id = ? AND item_id = ?", u, item).First(&e)
	if e.RevokedAt != nil || e.ExpiresAt == nil || e.ExpiresAt.Sub(start) < 29*24*time.Hour || e.ExpiresAt.Sub(start) > 31*24*time.Hour {
		t.Fatalf("refunding the second 30 days left revoked=%v expires=%v, want the first 30 days", e.RevokedAt, e.ExpiresAt)
	}

	upgrade, err := f.buy(u, forever, "upgrade")
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if _, err := f.shop.Refund(ctx, upgrade.Order.ID, 1, "t"); err != nil {
		t.Fatalf("refund upgrade: %v", err)
	}
	testDB.Where("user_id = ? AND item_id = ?", u, item).First(&e)
	if e.RevokedAt != nil || e.ExpiresAt == nil || e.ExpiresAt.Sub(start) < 29*24*time.Hour {
		t.Fatalf("refunding the upgrade left revoked=%v expires=%v, want the timed holding back", e.RevokedAt, e.ExpiresAt)
	}
	if f.balance(u) != 900 {
		t.Fatalf("balance %d, want 900 (only the first purchase kept)", f.balance(u))
	}
}

func TestAConcurrentRetryGetsTheFirstOrder(t *testing.T) {
	f := newFixture(t)
	item := f.frame("重试")
	offer := f.offer(OfferInput{Price: 100, Rewards: []model.Reward{{ItemID: item}}})
	u := f.user(1000)
	var replays, fresh atomic.Int64
	var wg sync.WaitGroup
	for i := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := f.buy(u, offer, "same-key")
			switch {
			case err != nil:
				t.Errorf("attempt %d: %v", i, err)
			case got.Replay:
				replays.Add(1)
			default:
				fresh.Add(1)
			}
		}()
	}
	wg.Wait()
	if fresh.Load() != 1 || replays.Load() != 5 || f.balance(u) != 900 {
		t.Fatalf("%d fresh, %d replays, balance %d; want 1, 5, 900", fresh.Load(), replays.Load(), f.balance(u))
	}
}

func TestAFrameAndABackgroundAreWornTogether(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	frame, bg := f.frame("并存框"), f.background("并存背景", nil)
	u := f.user(1000)
	for _, item := range []int64{frame, bg} {
		if _, err := f.buy(u, f.offer(OfferInput{Price: 100, Rewards: []model.Reward{{ItemID: item}}}), fmt.Sprint("k", item)); err != nil {
			t.Fatalf("buy %d: %v", item, err)
		}
	}

	wantCode(t, f.shop.Equip(ctx, u, model.SlotProfileBackground, model.EverySite, &frame), errors.ErrShopInvalidItem, "a frame in the background slot")
	if err := f.shop.Equip(ctx, u, model.SlotAvatarFrame, model.EverySite, &frame); err != nil {
		t.Fatalf("equip frame: %v", err)
	}
	if err := f.shop.Equip(ctx, u, model.SlotProfileBackground, model.EverySite, &bg); err != nil {
		t.Fatalf("equip background: %v", err)
	}

	c, err := f.shop.CosmeticsFor(ctx, []uint{u}, model.EverySite)
	if err != nil {
		t.Fatal(err)
	}
	if d := c[u][model.SlotAvatarFrame]; d == nil || d.ItemID != frame {
		t.Fatalf("frame slot shows %+v", d)
	}
	if d := c[u][model.SlotProfileBackground]; d == nil || d.ItemID != bg || d.StaticURL == "" || d.AnimatedURL != "" {
		t.Fatalf("background slot shows %+v", d)
	}
	raw, _ := json.Marshal(c[u])
	var wire map[string]json.RawMessage
	_ = json.Unmarshal(raw, &wire)
	if len(wire) != 2 || wire["avatar_frame"] == nil || wire["profile_background"] == nil {
		t.Fatalf("cosmetics on the wire: %s", raw)
	}
}

func TestAnAssetOfTheOtherKindIsRefused(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	frameArt, err := f.shop.UploadAsset(ctx, model.KindAvatarFrame, framePNG(t, 384, false), 1)
	if err != nil {
		t.Fatal(err)
	}
	banner, err := f.shop.UploadAsset(ctx, model.KindProfileBackground, bannerJPEG(t, 1500, 500), 1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.shop.UploadAsset(ctx, model.KindProfileBackground, framePNG(t, 384, false), 1)
	wantCode(t, err, errors.ErrShopInvalidAsset, "a frame uploaded as a background")

	render, _ := json.Marshal(model.Render{Static: frameArt.Key})
	_, err = f.shop.CreateItem(ctx, ItemInput{Kind: model.KindProfileBackground, Name: "错配", Render: render}, 1)
	wantCode(t, err, errors.ErrShopInvalidAsset, "a background whose art is a frame")
	render, _ = json.Marshal(model.Render{Static: banner.Key})
	_, err = f.shop.CreateItem(ctx, ItemInput{Kind: model.KindAvatarFrame, Name: "错配", Render: render}, 1)
	wantCode(t, err, errors.ErrShopInvalidItem, "a frame whose art is a JPEG banner")
	_, err = f.shop.CreateItem(ctx, ItemInput{Kind: "nameplate", Name: "未知", Render: render}, 1)
	wantCode(t, err, errors.ErrShopInvalidItem, "an unregistered kind")
}

func TestASiteOfferSellsInTheAccountCenterAndPaysTheSite(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	site := f.site()
	bg := f.background("站点背景", &site)
	oid := f.offer(OfferInput{SiteID: &site, Price: 150, Rewards: []model.Reward{{ItemID: bg}}})

	cat, err := f.shop.Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var listed *OfferView
	for i := range cat {
		if cat[i].ID == oid {
			listed = &cat[i]
		}
	}
	if listed == nil || listed.Site == nil || listed.Site.ID != site || listed.Site.Name == "" {
		t.Fatalf("the site offer is listed as %+v", listed)
	}

	sink := fmt.Sprintf("shop:site:%d", site)
	before := sinkBalance(t, sink)
	u := f.user(500)
	if _, err := f.buy(u, oid, "site-offer"); err != nil {
		t.Fatalf("buy the site offer: %v", err)
	}
	if got := f.balance(u); got != 350 {
		t.Fatalf("buyer balance %d, want 350", got)
	}
	if got := sinkBalance(t, sink) - before; got != 150 {
		t.Fatalf("the site's sink gained %d, want 150", got)
	}
	if err := f.shop.Equip(ctx, u, model.SlotProfileBackground, model.EverySite, &bg); err != nil {
		t.Fatalf("a site item is worn everywhere: %v", err)
	}
}

func sinkBalance(t *testing.T, code string) int64 {
	t.Helper()
	var b int64
	if err := testDB.Raw(`SELECT COALESCE(SUM(balance), 0) FROM ledger_accounts WHERE kind = ? AND code = ?`,
		ledgerModel.KindSink, code).Scan(&b).Error; err != nil {
		t.Fatal(err)
	}
	return b
}
