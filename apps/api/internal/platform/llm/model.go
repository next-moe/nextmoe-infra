package llm

import (
	"time"

	"gorm.io/datatypes"
)

const (
	KindCompatible = "compatible"

	ModelChat       = "chat"
	ModelEmbedding  = "embedding"
	ModelModeration = "moderation"
	ModelSearch     = "search"
	ModelImage      = "image"

	ProtocolChat        = "chat"
	ProtocolEmbeddings  = "embeddings"
	ProtocolModerations = "moderations"
	ProtocolResponses   = "responses"
	ProtocolImages      = "images"

	StrategyWeighted  = "weighted"
	StrategyLeastBusy = "least_busy"
	StrategyLatency   = "latency"
	StrategyCost      = "cost"

	RouteActive   = "active"
	RouteDisabled = "disabled"

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
	KeyHint            string         `gorm:"size:16;not null;default:''"`
	Headers            datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'"`
	PriceMultiplierPPM int64          `gorm:"not null;default:1000000"`
	Enabled            bool           `gorm:"not null;default:true"`
	CatalogSyncedAt    *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (Provider) TableName() string { return "llm_provider" }

type Model struct {
	ID            int64  `gorm:"primaryKey"`
	Key           string `gorm:"size:64;not null;uniqueIndex"`
	Kind          string `gorm:"size:16;not null;default:'chat'"`
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

func (m *Model) kind() string {
	if m.Kind == "" {
		return ModelChat
	}
	return m.Kind
}

type Route struct {
	ID              int64  `gorm:"primaryKey"`
	ModelID         int64  `gorm:"not null;uniqueIndex:uq_llm_route,priority:1"`
	ProviderID      int64  `gorm:"not null;uniqueIndex:uq_llm_route,priority:2"`
	UpstreamID      string `gorm:"size:128;not null;uniqueIndex:uq_llm_route,priority:3"`
	Protocol        string `gorm:"size:16;not null;default:'chat'"`
	Priority        int    `gorm:"not null;default:0"`
	Weight          int    `gorm:"not null;default:100"`
	Status          string `gorm:"size:16;not null;default:'active'"`
	StatusReason    string `gorm:"size:255;not null;default:''"`
	MaxConcurrency  int    `gorm:"not null;default:0"`
	RPM             int    `gorm:"column:rpm;not null;default:0"`
	ThinkingControl string `gorm:"size:16;not null;default:'auto'"`
	PriceID         *int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (Route) TableName() string { return "llm_route" }

type PriceRevision struct {
	ID           int64  `gorm:"primaryKey"`
	RouteID      int64  `gorm:"not null;index"`
	InputMicro   int64  `gorm:"not null"`
	CachedMicro  int64  `gorm:"not null"`
	OutputMicro  int64  `gorm:"not null"`
	PerCallMicro int64  `gorm:"not null;default:0"`
	Source       string `gorm:"size:16;not null;default:'manual'"`
	CreatedAt    time.Time
}

func (PriceRevision) TableName() string { return "llm_price_revision" }

type ProviderModel struct {
	ID               int64          `gorm:"primaryKey"`
	ProviderID       int64          `gorm:"not null;uniqueIndex:uq_llm_provider_model,priority:1"`
	UpstreamID       string         `gorm:"size:200;not null;uniqueIndex:uq_llm_provider_model,priority:2"`
	Kind             string         `gorm:"size:16;not null"`
	KindSource       string         `gorm:"size:16;not null"`
	InputModalities  datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	OutputModalities datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	ContextTokens    int            `gorm:"not null;default:0"`
	OwnedBy          string         `gorm:"size:100;not null;default:''"`
	Raw              datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'"`
	Probe            datatypes.JSON `gorm:"type:jsonb"`
	Listed           bool           `gorm:"not null;default:true"`
	FirstSeenAt      time.Time      `gorm:"not null"`
	LastSeenAt       time.Time      `gorm:"not null"`
	DelistedAt       *time.Time
}

func (ProviderModel) TableName() string { return "llm_provider_model" }

type UsageHourly struct {
	Hour         time.Time `gorm:"primaryKey"`
	Scene        string    `gorm:"primaryKey;size:64"`
	RouteID      int64     `gorm:"primaryKey"`
	AccessKeyID  int64     `gorm:"primaryKey"`
	ProviderID   int64     `gorm:"not null"`
	ModelKey     string    `gorm:"size:64;not null"`
	Calls        int64     `gorm:"not null"`
	Errors       int64     `gorm:"not null"`
	InputTokens  int64     `gorm:"not null"`
	OutputTokens int64     `gorm:"not null"`
	CostNano     int64     `gorm:"not null"`
	DurationMs   int64     `gorm:"not null"`
}

func (UsageHourly) TableName() string { return "llm_usage_hourly" }

type Scene struct {
	Key              string         `gorm:"primaryKey;size:64"`
	ModelID          int64          `gorm:"not null"`
	FallbackModelIDs datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	ContextFallback  datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	ContentFallback  datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	Strategy         string         `gorm:"size:16;not null;default:'weighted'"`
	Reasoning        string         `gorm:"size:8;not null;default:'off'"`
	DailyBudgetMicro int64          `gorm:"not null;default:0"`
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
	AccessKeyID       int64  `gorm:"not null;default:0"`
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
	Actor      string `gorm:"size:64;not null;default:''"`
	CreatedAt  time.Time
}

func (RouteEvent) TableName() string { return "llm_event" }

type AccessKey struct {
	ID               int64          `gorm:"primaryKey"`
	Name             string         `gorm:"size:64;not null;uniqueIndex"`
	Prefix           string         `gorm:"size:16;not null"`
	Hash             string         `gorm:"size:64;not null;uniqueIndex"`
	AllowedModels    datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	Enabled          bool           `gorm:"not null;default:true"`
	DailyBudgetMicro int64          `gorm:"not null;default:0"`
	LastUsedAt       *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (AccessKey) TableName() string { return "llm_access_key" }

func Tables() []any {
	return []any{&Provider{}, &Model{}, &Route{}, &PriceRevision{}, &Scene{}, &Attempt{}, &RouteEvent{}, &AccessKey{}, &ProviderModel{}, &UsageHourly{}}
}
