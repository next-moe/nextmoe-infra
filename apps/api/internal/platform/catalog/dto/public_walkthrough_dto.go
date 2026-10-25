package dto

import (
	"time"

	"api/internal/platform/catalog/walkthrough"
)

type PublicWalkthrough struct {
	ID              int64
	WorkID          int64
	Source          string
	SourceURL       string
	SourceAuthor    string
	SourceUpdatedOn *time.Time
	UpdatedAt       time.Time
	Characters      []PublicWalkthroughCharacter
	EndingCount     int
	SaveCount       int
	SegmentCount    int
	Chart           *walkthrough.Chart
}

type PublicWalkthroughCharacter struct {
	Key         string
	Name        string
	CharacterID *int64
	DisplayName string
	Localized   map[string]PublicLocalizedName
}

type PublicWalkthroughsListData struct {
	Items      []PublicWalkthrough
	NextCursor *string
	Total      int64
}
