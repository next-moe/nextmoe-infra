package envconfig

import (
	"time"

	"gorm.io/datatypes"
)

type ConfigSecret struct {
	ID              uint   `gorm:"primaryKey"`
	Scope           string `gorm:"size:32;not null;uniqueIndex:uq_config_secret,priority:1"`
	Key             string `gorm:"size:128;not null;uniqueIndex:uq_config_secret,priority:2"`
	Cipher          []byte `gorm:"type:bytea;not null"`
	Fingerprint     string `gorm:"size:16;not null"`
	Hint            string `gorm:"size:16;not null"`
	Version         int64  `gorm:"not null"`
	UpdatedByUserID uint   `gorm:"not null"`
	Note            string `gorm:"size:512;not null"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (ConfigSecret) TableName() string { return "config_secrets" }

const (
	ActionSet    = "set"
	ActionReset  = "reset"
	ActionImport = "import"
)

type ConfigSecretAuditLog struct {
	ID             uint      `gorm:"primaryKey"`
	ActorUserID    uint      `gorm:"not null;index"`
	Action         string    `gorm:"size:8;not null"`
	Scope          string    `gorm:"size:32;not null"`
	Key            string    `gorm:"size:128;not null;index"`
	OldFingerprint string    `gorm:"size:16;not null"`
	NewFingerprint string    `gorm:"size:16;not null"`
	OldValue       string    `gorm:"type:text;not null"`
	NewValue       string    `gorm:"type:text;not null"`
	Note           string    `gorm:"size:512;not null"`
	CreatedAt      time.Time `gorm:"index"`
}

func (ConfigSecretAuditLog) TableName() string { return "config_secret_audit_logs" }

type BootSnapshot struct {
	Service     string         `gorm:"primaryKey;size:32"`
	Hostname    string         `gorm:"size:128;not null"`
	StartedAt   time.Time      `gorm:"not null"`
	StoreLoaded bool           `gorm:"not null"`
	Entries     datatypes.JSON `gorm:"type:jsonb;not null"`
	Unread      datatypes.JSON `gorm:"type:jsonb;not null"`
}

func (BootSnapshot) TableName() string { return "config_boot_snapshots" }
