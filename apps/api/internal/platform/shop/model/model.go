package model

import (
	"time"

	"gorm.io/datatypes"
)

const (
	KindAvatarFrame = "avatar_frame"
	SlotAvatarFrame = "avatar_frame"
)

func SlotOf(kind string) (string, bool) {
	switch kind {
	case KindAvatarFrame:
		return SlotAvatarFrame, true
	}
	return "", false
}

const (
	ItemDraft     = "draft"
	ItemReview    = "review"
	ItemPublished = "published"
	ItemRetired   = "retired"
)

const (
	OfferDraft   = "draft"
	OfferActive  = "active"
	OfferRetired = "retired"
)

const (
	OrderCompleted = "completed"
	OrderRefunded  = "refunded"
)

const (
	SourcePurchase = "purchase"
	SourceGrant    = "grant"
)

type Asset struct {
	Hash        string    `gorm:"size:64;primaryKey" json:"hash"`
	Key         string    `gorm:"size:160;not null;uniqueIndex" json:"key"`
	ContentType string    `gorm:"size:32;not null" json:"content_type"`
	Width       int       `gorm:"not null" json:"width"`
	Height      int       `gorm:"not null" json:"height"`
	Animated    bool      `gorm:"not null" json:"animated"`
	Bytes       int       `gorm:"not null" json:"bytes"`
	UploadedBy  uint      `gorm:"not null" json:"uploaded_by"`
	CreatedAt   time.Time `json:"created_at"`
}

func (Asset) TableName() string { return "shop_assets" }

type AvatarFrameRender struct {
	Static   string `json:"static"`
	Animated string `json:"animated,omitempty"`
}

type Item struct {
	ID          int64          `gorm:"primaryKey" json:"id"`
	Kind        string         `gorm:"size:32;not null;index" json:"kind"`
	SiteID      *uint          `gorm:"index" json:"site_id"`
	Status      string         `gorm:"size:16;not null;index" json:"status"`
	Name        string         `gorm:"size:64;not null" json:"name"`
	Description string         `gorm:"size:255;not null;default:''" json:"description"`
	Render      datatypes.JSON `gorm:"type:jsonb;not null" json:"render"`
	CreatedBy   uint           `gorm:"not null" json:"created_by"`
	PublishedAt *time.Time     `json:"published_at"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

func (Item) TableName() string { return "shop_items" }

type Cost struct {
	Asset  string `json:"asset"`
	Amount int64  `json:"amount"`
}

type Reward struct {
	ItemID       int64 `json:"item_id"`
	DurationDays int   `json:"duration_days,omitempty"`
}

type Offer struct {
	ID           int64          `gorm:"primaryKey" json:"id"`
	SiteID       *uint          `gorm:"index" json:"site_id"`
	Status       string         `gorm:"size:16;not null;index" json:"status"`
	Costs        datatypes.JSON `gorm:"type:jsonb;not null" json:"costs"`
	Rewards      datatypes.JSON `gorm:"type:jsonb;not null" json:"rewards"`
	StartsAt     *time.Time     `json:"starts_at"`
	EndsAt       *time.Time     `json:"ends_at"`
	PerUserLimit int            `gorm:"not null;default:0" json:"per_user_limit"`
	Stock        *int           `json:"stock"`
	Sold         int            `gorm:"not null;default:0" json:"sold"`
	SortOrder    int            `gorm:"not null;default:0" json:"sort_order"`
	CreatedBy    uint           `gorm:"not null" json:"created_by"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

func (Offer) TableName() string { return "shop_offers" }

type Order struct {
	ID               int64          `gorm:"primaryKey" json:"id"`
	UserID           uint           `gorm:"not null;uniqueIndex:uidx_shop_orders_idem,priority:1" json:"user_id"`
	IdempotencyKey   string         `gorm:"size:64;not null;uniqueIndex:uidx_shop_orders_idem,priority:2" json:"-"`
	RecipientUserID  uint           `gorm:"not null;index" json:"recipient_user_id"`
	OfferID          int64          `gorm:"not null;index" json:"offer_id"`
	SiteID           uint           `gorm:"not null;default:0" json:"site_id"`
	Costs            datatypes.JSON `gorm:"type:jsonb;not null" json:"costs"`
	Rewards          datatypes.JSON `gorm:"type:jsonb;not null" json:"rewards"`
	Prior            datatypes.JSON `gorm:"type:jsonb" json:"-"`
	TransferID       *int64         `json:"transfer_id"`
	Status           string         `gorm:"size:16;not null" json:"status"`
	RefundTransferID *int64         `json:"refund_transfer_id"`
	RefundedAt       *time.Time     `json:"refunded_at"`
	CreatedAt        time.Time      `json:"created_at"`
}

func (Order) TableName() string { return "shop_orders" }

// PriorHolding is what the buyer held of a reward before the order, so a
// refund of a permanent purchase can put a timed holding back.
type PriorHolding struct {
	ItemID    int64      `json:"item_id"`
	Held      bool       `json:"held"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type Entitlement struct {
	ID         int64      `gorm:"primaryKey" json:"id"`
	UserID     uint       `gorm:"not null;uniqueIndex:uidx_shop_entitlements_owner,priority:1" json:"user_id"`
	ItemID     int64      `gorm:"not null;uniqueIndex:uidx_shop_entitlements_owner,priority:2" json:"item_id"`
	Source     string     `gorm:"size:16;not null" json:"source"`
	OrderID    *int64     `json:"order_id"`
	GrantedBy  uint       `gorm:"not null;default:0" json:"granted_by"`
	Note       string     `gorm:"size:255;not null;default:''" json:"note"`
	AcquiredAt time.Time  `gorm:"not null" json:"acquired_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func (Entitlement) TableName() string { return "shop_entitlements" }

func (e *Entitlement) Active(now time.Time) bool {
	return e.RevokedAt == nil && (e.ExpiresAt == nil || e.ExpiresAt.After(now))
}

const EverySite uint = 0

type Loadout struct {
	UserID    uint      `gorm:"primaryKey;autoIncrement:false" json:"user_id"`
	Slot      string    `gorm:"primaryKey;size:32" json:"slot"`
	SiteID    uint      `gorm:"primaryKey;autoIncrement:false" json:"site_id"`
	ItemID    int64     `gorm:"not null;index" json:"item_id"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Loadout) TableName() string { return "shop_loadouts" }

type Decoration struct {
	ItemID      int64  `json:"item_id"`
	Name        string `json:"name"`
	StaticURL   string `json:"static_url"`
	AnimatedURL string `json:"animated_url,omitempty"`
}

type Cosmetics struct {
	AvatarFrame *Decoration `json:"avatar_frame,omitempty"`
}

func AllModels() []any {
	return []any{&Asset{}, &Item{}, &Offer{}, &Order{}, &Entitlement{}, &Loadout{}}
}
