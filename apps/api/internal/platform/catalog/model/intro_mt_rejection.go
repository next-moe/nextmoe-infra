package model

import "time"

// CatalogIntroMTRejection records a source text whose machine translation was
// refused (introguard.Reject), so the lanes stop asking the model again every
// night. A changed source hashes differently and is translated afresh.
type CatalogIntroMTRejection struct {
	ID         int64  `gorm:"primaryKey"`
	IntroTable string `gorm:"type:text;not null;uniqueIndex:uq_catalog_intro_mt_rejection,priority:1"`
	OwnerID    int64  `gorm:"not null;uniqueIndex:uq_catalog_intro_mt_rejection,priority:2"`
	SrcHash    string `gorm:"type:text;not null;uniqueIndex:uq_catalog_intro_mt_rejection,priority:3"`
	SourceID   int16  `gorm:"not null"`
	Reason     string `gorm:"type:text;not null"`
	Model      string `gorm:"type:text;not null;default:''"`
	Output     string `gorm:"type:text;not null;default:''"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (CatalogIntroMTRejection) TableName() string { return "catalog_intro_mt_rejection" }
