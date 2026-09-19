package repr

type StorePurchaseLinks struct {
	_           struct{}       `json:"-" additionalProperties:"true"`
	Object      string         `json:"object" enum:"purchase_links" doc:"Type discriminant. Always purchase_links."`
	ProductID   string         `json:"product_id" pattern:"^(RJ|VJ)[0-9]{6,8}$" maxLength:"10" doc:"The DLsite product number that was asked for, echoed back."`
	PurchaseURL string         `json:"purchase_url" format:"uri" maxLength:"1024" doc:"Short link to send readers to. It belongs to your application alone; use it verbatim."`
	CouponURL   *string        `json:"coupon_url" format:"uri" maxLength:"1024" doc:"Short link to the coupon claim page. null when no campaign is running."`
	Campaign    *StoreCampaign `json:"campaign" doc:"The campaign coupon_url belongs to. null when no campaign is running."`
}

type StoreCampaign struct {
	_      struct{} `json:"-" additionalProperties:"true"`
	Object string   `json:"object" enum:"campaign" doc:"Type discriminant. Always campaign."`
	ID     string   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Campaign id. Pair it with coupon_url when you render the offer."`
	Name   string   `json:"name" maxLength:"256" doc:"Human-readable campaign name, safe to show to readers. Must not be used as a discriminant."`
}

type StoreStats struct {
	_        struct{}         `json:"-" additionalProperties:"true"`
	Object   string           `json:"object" enum:"store_stats" doc:"Type discriminant. Always store_stats."`
	FromDate string           `json:"from_date" format:"date" maxLength:"10" doc:"First JST day covered, inclusive. The from= query parameter echoed back."`
	ToDate   string           `json:"to_date" format:"date" maxLength:"10" doc:"Last JST day covered, inclusive. The to= query parameter echoed back."`
	Rows     []StoreStatRow   `json:"rows" doc:"One row per link per day; days with no clicks are absent. Empty array, never null."`
	Totals   StoreStatTotal   `json:"totals" doc:"Grand total over the range."`
	ByKind   []StoreStatTotal `json:"by_kind" doc:"The same totals split into purchase and coupon. Empty array, never null."`
}

// link_kind, not kind: G8 forbids a bare kind property, the same reason
// image.cover_kind carries its prefix. StoreStats spells the range from_date /
// to_date for the same class of reason — bare from/to already belong to
// FieldDiff, where they are untyped, and G8 wants one shape per property name.
type StoreStatRow struct {
	_          struct{} `json:"-" additionalProperties:"true"`
	Object     string   `json:"object" enum:"store_stat" doc:"Type discriminant. Always store_stat."`
	LinkKind   string   `json:"link_kind" enum:"purchase,coupon" doc:"purchase is a product link, coupon is a campaign's claim link."`
	ProductID  *string  `json:"product_id" pattern:"^(RJ|VJ)[0-9]{6,8}$" maxLength:"10" doc:"The DLsite product number on a purchase row. null on coupon rows."`
	CampaignID *string  `json:"campaign_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The campaign id on a coupon row. null on purchase rows."`
	Date       string   `json:"date" format:"date" maxLength:"10" doc:"JST calendar day."`
	Total      int      `json:"total" minimum:"0" doc:"Clicks that day, before de-duplication and crawlers included."`
	Uniques    int      `json:"uniques" minimum:"0" doc:"Distinct (day, fingerprint) clicks that day, clients whose User-Agent announces a crawler, link preview or HTTP library left out — the number settlement uses."`
}

type StoreStatTotal struct {
	_        struct{} `json:"-" additionalProperties:"true"`
	LinkKind *string  `json:"link_kind" enum:"purchase,coupon" doc:"Which half of the range this total covers. null on the grand total."`
	Total    int      `json:"total" minimum:"0" doc:"Clicks in the range, before de-duplication and crawlers included."`
	Uniques  int      `json:"uniques" minimum:"0" doc:"De-duplicated clicks in the range, declared crawlers left out — the number settlement uses."`
}
