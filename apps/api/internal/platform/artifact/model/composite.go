package model

import "time"

type Composite struct {
	ID            uint   `gorm:"primaryKey"`
	PublicID      string `gorm:"column:public_id;size:32;not null;uniqueIndex"`
	SiteKey       string `gorm:"column:site_key;size:32;not null;index"`
	CreatorClient string `gorm:"column:creator_client;size:64;not null;default:''"`

	Filename     string `gorm:"size:255;not null"`
	ContentType  string `gorm:"column:content_type;size:100;not null"`
	Size         int64  `gorm:"not null"`
	BLAKE3       string `gorm:"column:blake3;size:64;not null"`
	SegmentCount int    `gorm:"column:segment_count;not null"`
	InlineBytes  int64  `gorm:"column:inline_bytes;not null;default:0"`

	CreatedAt time.Time

	Sources []CompositeSource `gorm:"foreignKey:CompositeID;constraint:OnDelete:CASCADE"`
}

func (Composite) TableName() string { return "composites" }

type CompositeSource struct {
	CompositeID uint `gorm:"primaryKey"`
	Position    int  `gorm:"primaryKey"`
	ArtifactID  uint `gorm:"not null;index"`

	Artifact Artifact `gorm:"foreignKey:ArtifactID;constraint:OnDelete:RESTRICT"`
}

func (CompositeSource) TableName() string { return "composite_sources" }

func All() []any {
	return []any{&Artifact{}, &Manifest{}, &Composite{}, &CompositeSource{}}
}
