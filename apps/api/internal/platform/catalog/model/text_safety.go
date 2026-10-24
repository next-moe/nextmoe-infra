package model

import "time"

// SafeTitleConfidence is the bar a "safe" title verdict must clear before a name
// counts as safe; below it the name is treated as unsafe.
const SafeTitleConfidence = 0.8

const (
	TitleSafetySafe       int16 = 1
	TitleSafetySuggestive int16 = 2
	TitleSafetyExplicit   int16 = 3
)

const (
	IntroSafetySafe     int16 = 1
	IntroSafetyMild     int16 = 2
	IntroSafetyExplicit int16 = 3
)

const (
	IntroSFWOriginal  int16 = 0
	IntroSFWRedacted  int16 = 1
	IntroSFWRewritten int16 = 2
	IntroSFWDropped   int16 = 3
)

type CatalogWorkTitleSafety struct {
	TitleHash     string    `gorm:"primaryKey;type:text" json:"title_hash"`
	Verdict       int16     `gorm:"not null" json:"verdict"`
	Confidence    float32   `gorm:"type:real;not null" json:"confidence"`
	Model         string    `gorm:"not null" json:"model"`
	PromptVersion string    `gorm:"not null" json:"prompt_version"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (CatalogWorkTitleSafety) TableName() string { return "catalog_work_title_safety" }

// IntroHash must equal Postgres md5(catalog_work_intro.intro): the read face
// joins on that expression.
type CatalogWorkIntroSafety struct {
	IntroHash     string    `gorm:"primaryKey;type:text" json:"intro_hash"`
	Verdict       int16     `gorm:"not null" json:"verdict"`
	Confidence    float32   `gorm:"type:real;not null" json:"confidence"`
	SFWMode       int16     `gorm:"column:sfw_mode;not null" json:"sfw_mode"`
	SFWIntro      string    `gorm:"column:sfw_intro;type:text;not null;default:''" json:"sfw_intro"`
	Model         string    `gorm:"not null" json:"model"`
	PromptVersion string    `gorm:"not null" json:"prompt_version"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (CatalogWorkIntroSafety) TableName() string { return "catalog_work_intro_safety" }

type CatalogCharacterIntroSafety CatalogWorkIntroSafety

func (CatalogCharacterIntroSafety) TableName() string { return "catalog_character_intro_safety" }
