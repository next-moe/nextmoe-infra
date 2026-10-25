package dto

import (
	"time"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/walkthrough"
)

type PublicWalkthrough struct {
	ID               int64
	WorkID           int64
	WorkContentLimit string
	WorkRouteHints   bool
	Source           string
	SourceURL        string
	SourceAuthor     string
	SourceUpdatedOn  *time.Time
	Lang             string
	UpdatedAt        time.Time
	Characters       []PublicWalkthroughCharacter
	EndingCount      int
	SaveCount        int
	SegmentCount     int
	Chart            *walkthrough.Chart
	ImportReview     *model.WalkthroughImportReview
	Release          *PublicWalkthroughRelease
}

type PublicWalkthroughRelease struct {
	ID        int64
	Title     *string
	Kind      string
	Lang      *string
	Companies []string
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
