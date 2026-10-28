package llm

import (
	"time"

	"gorm.io/datatypes"
)

const (
	KindCompatible = "compatible"

	ProtocolChat = "chat"

	RouteActive    = "active"
	RouteDisabled  = "disabled"
	RouteSuspended = "suspended"

	ClassModeration  = "moderation"
	ClassInteractive = "interactive"
	ClassSearch      = "search"
	ClassBatch       = "batch"
)

type Provider struct {
	ID                 int64          `gorm:"primaryKey"`
	Name               string         `gorm:"size:64;not null;uniqueIndex"`
	Kind               string         `gorm:"size:16;not null"`
	BaseURL            string         `gorm:"size:512;not null"`
	KeyEnv             string         `gorm:"size:64;not null;default:''"`
	KeyCipher          []byte         `gorm:"type:bytea"`
	Headers            datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'"`
	PriceMultiplierPPM int64          `gorm:"not null;default:1000000"`
	Enabled            bool           `gorm:"not null;default:true"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (Provider) TableName() string { return "llm_provider" }

type Model struct {
	ID            int64  `gorm:"primaryKey"`
	Key           string `gorm:"size:64;not null;uniqueIndex"`
	ContextTokens int    `gorm:"not null;default:0"`
	MaxOutput     int    `gorm:"not null;default:0"`
	ToolCall      bool   `gorm:"not null;default:false"`
	Vision        bool   `gorm:"not null;default:false"`
	Reasoning     bool   `gorm:"not null;default:false"`
	Enabled       bool   `gorm:"not null;default:true"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (Model) TableName() string { return "llm_model" }

type Route struct {
	ID             int64  `gorm:"primaryKey"`
	ModelID        int64  `gorm:"not null;uniqueIndex:uq_llm_route,priority:1"`
	ProviderID     int64  `gorm:"not null;uniqueIndex:uq_llm_route,priority:2"`
	UpstreamID     string `gorm:"size:128;not null;uniqueIndex:uq_llm_route,priority:3"`
	Protocol       string `gorm:"size:16;not null;default:'chat'"`
	Priority       int    `gorm:"not null;default:0"`
	Weight         int    `gorm:"not null;default:100"`
	Status         string `gorm:"size:16;not null;default:'active'"`
	StatusReason   string `gorm:"size:255;not null;default:''"`
	MaxConcurrency int    `gorm:"not null;default:0"`
	PriceID        *int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (Route) TableName() string { return "llm_route" }

type PriceRevision struct {
	ID          int64  `gorm:"primaryKey"`
	RouteID     int64  `gorm:"not null;index"`
	InputMicro  int64  `gorm:"not null"`
	CachedMicro int64  `gorm:"not null"`
	OutputMicro int64  `gorm:"not null"`
	Source      string `gorm:"size:16;not null;default:'manual'"`
	CreatedAt   time.Time
}

func (PriceRevision) TableName() string { return "llm_price_revision" }

type Scene struct {
	Key              string         `gorm:"primaryKey;size:64"`
	ModelID          int64          `gorm:"not null"`
	FallbackModelIDs datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	Class            string         `gorm:"size:16;not null;default:'interactive'"`
	Temperature      *float64
	MaxOutput        int `gorm:"not null;default:0"`
	TTFTMs           int `gorm:"not null;default:20000"`
	IdleMs           int `gorm:"not null;default:30000"`
	TotalMs          int `gorm:"not null;default:120000"`
	UpdatedAt        time.Time
}

func (Scene) TableName() string { return "llm_scene" }

type Attempt struct {
	ID                int64  `gorm:"primaryKey"`
	CallID            string `gorm:"size:36;not null;index"`
	Scene             string `gorm:"size:64;not null"`
	RouteID           int64  `gorm:"not null"`
	ProviderID        int64  `gorm:"not null"`
	ModelKey          string `gorm:"size:64;not null"`
	ServedModel       string `gorm:"size:128;not null;default:''"`
	UpstreamRequestID string `gorm:"size:128;not null;default:''"`
	TraceID           string `gorm:"size:32;not null;default:''"`
	Site              string `gorm:"size:64;not null;default:''"`
	UserID            int64  `gorm:"not null;default:0"`
	OK                bool   `gorm:"not null"`
	ErrorKind         string `gorm:"size:32;not null;default:''"`
	HTTPStatus        int    `gorm:"not null;default:0"`
	TTFTMs            int    `gorm:"not null;default:0"`
	DurationMs        int    `gorm:"not null;default:0"`
	InputTokens       int    `gorm:"not null;default:0"`
	CachedTokens      int    `gorm:"not null;default:0"`
	OutputTokens      int    `gorm:"not null;default:0"`
	ReasoningTokens   int    `gorm:"not null;default:0"`
	CostNano          int64  `gorm:"not null;default:0"`
	UsageEstimated    bool   `gorm:"not null;default:false"`
	PriceID           *int64
	CreatedAt         time.Time `gorm:"not null;index"`
}

func (Attempt) TableName() string { return "llm_attempt" }

type RouteEvent struct {
	ID         int64  `gorm:"primaryKey"`
	RouteID    int64  `gorm:"not null;default:0;index"`
	ProviderID int64  `gorm:"not null;default:0"`
	Kind       string `gorm:"size:32;not null"`
	Detail     string `gorm:"type:text;not null;default:''"`
	CreatedAt  time.Time
}

func (RouteEvent) TableName() string { return "llm_event" }

func Tables() []any {
	return []any{&Provider{}, &Model{}, &Route{}, &PriceRevision{}, &Scene{}, &Attempt{}, &RouteEvent{}}
}
