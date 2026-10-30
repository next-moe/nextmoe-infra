package model

import (
	"time"

	"gorm.io/datatypes"
)

const (
	ModeAssistant = "assistant"
	ModeSearch    = "search"

	RoleUser      = "user"
	RoleAssistant = "assistant"

	MessageComplete  = "complete"
	MessageStreaming = "streaming"
	MessageFailed    = "failed"
	MessageCanceled  = "canceled"

	FeedbackUp   = "up"
	FeedbackDown = "down"

	RunRunning     = "running"
	RunCompleted   = "completed"
	RunFailed      = "failed"
	RunCanceled    = "canceled"
	RunInterrupted = "interrupted"

	SettleNone    = "none"
	SettleFree    = "free"
	SettlePaid    = "paid"
	SettlePending = "pending"
	SettleUnpaid  = "unpaid"

	PersonaMascot = "mascot"
)

type Conversation struct {
	ID            int64  `gorm:"primaryKey"`
	UserID        int64  `gorm:"not null;index:idx_agent_conversation_user,priority:1"`
	SiteID        int64  `gorm:"not null"`
	Title         string `gorm:"size:200;not null;default:''"`
	Mode          string `gorm:"size:16;not null;default:'assistant'"`
	Starred       bool   `gorm:"not null;default:false"`
	HeadMessageID *int64
	PersonaID     *int64
	CreatedAt     time.Time
	UpdatedAt     time.Time `gorm:"index:idx_agent_conversation_user,priority:2"`
}

func (Conversation) TableName() string { return "agent_conversation" }

type Message struct {
	ID             int64          `gorm:"primaryKey"`
	ConversationID int64          `gorm:"not null;index"`
	ParentID       *int64         `gorm:"index"`
	Role           string         `gorm:"size:16;not null"`
	Parts          datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	Status         string         `gorm:"size:16;not null;default:'complete'"`
	RunID          string         `gorm:"size:36;not null;default:''"`
	Feedback       string         `gorm:"size:8;not null;default:''"`
	ChosenAt       *time.Time
	CreatedAt      time.Time
}

func (Message) TableName() string { return "agent_message" }

type Run struct {
	ID                 string `gorm:"primaryKey;size:36"`
	ConversationID     int64  `gorm:"not null;index"`
	UserID             int64  `gorm:"not null;uniqueIndex:uq_agent_run_idem,priority:1"`
	SiteID             int64  `gorm:"not null"`
	UserMessageID      int64  `gorm:"not null"`
	AssistantMessageID int64  `gorm:"not null"`
	Mode               string `gorm:"size:16;not null"`
	WebSearch          bool   `gorm:"not null;default:false"`
	Status             string `gorm:"size:16;not null"`
	ErrorKind          string `gorm:"size:32;not null;default:''"`
	IdemKey            string `gorm:"size:128;not null;uniqueIndex:uq_agent_run_idem,priority:2"`
	IdemHash           string `gorm:"size:64;not null"`
	Price              int    `gorm:"not null;default:0"`
	FreeUnits          int    `gorm:"not null;default:0"`
	PaidUnits          int    `gorm:"not null;default:0"`
	Settle             string `gorm:"size:16;not null;default:'none'"`
	Steps              int    `gorm:"not null;default:0"`
	InputTokens        int    `gorm:"not null;default:0"`
	OutputTokens       int    `gorm:"not null;default:0"`
	CostNano           int64  `gorm:"not null;default:0"`
	CreatedAt          time.Time
	FinishedAt         *time.Time
}

func (Run) TableName() string { return "agent_run" }

type Persona struct {
	ID          int64          `gorm:"primaryKey"`
	SiteID      int64          `gorm:"not null;index"`
	Kind        string         `gorm:"size:16;not null;default:'mascot'"`
	Name        string         `gorm:"size:64;not null"`
	Avatar      string         `gorm:"size:128;not null;default:''"`
	Prompt      string         `gorm:"type:text;not null"`
	Greetings   datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	Suggestions datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	Active      bool           `gorm:"not null;default:true"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (Persona) TableName() string { return "agent_persona" }

const (
	UploadImage    = "image"
	UploadText     = "text"
	UploadDocument = "document"
)

type Upload struct {
	ID             string `gorm:"primaryKey;size:36"`
	UserID         int64  `gorm:"not null;index"`
	SiteID         int64  `gorm:"not null"`
	ConversationID *int64 `gorm:"index"`
	Kind           string `gorm:"size:16;not null"`
	MediaType      string `gorm:"size:100;not null"`
	Filename       string `gorm:"size:255;not null;default:''"`
	Size           int64  `gorm:"not null"`
	Data           []byte `gorm:"type:bytea"`
	Text           string `gorm:"type:text;not null;default:''"`
	Truncated      bool   `gorm:"not null;default:false"`
	CreatedAt      time.Time
}

func (Upload) TableName() string { return "agent_upload" }

const (
	SiteToolAuthUser = "user_token"
	SiteToolAuthNone = "none"
)

type SiteTool struct {
	ID         int64          `gorm:"primaryKey"`
	SiteID     int64          `gorm:"not null;uniqueIndex:uq_agent_site_tool,priority:1"`
	Name       string         `gorm:"size:32;not null;uniqueIndex:uq_agent_site_tool,priority:2"`
	URL        string         `gorm:"size:512;not null"`
	Auth       string         `gorm:"size:16;not null;default:'user_token'"`
	CardPrefix string         `gorm:"size:32;not null"`
	Allowed    datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	TimeoutMs  int            `gorm:"not null;default:20000"`
	Enabled    bool           `gorm:"not null;default:true"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (SiteTool) TableName() string { return "agent_site_tool" }
