package importer

import (
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/service"

	"gorm.io/gorm"
)

// screenMinted runs service.ScreenMintedWorks over works a lane has just
// written with their refs, and reports how many it moved from live to
// quarantine.
func screenMinted(tx *gorm.DB, works []model.CatalogWork) (int, error) {
	minted := make([]service.MintedWork, len(works))
	live := make(map[int64]bool, len(works))
	for i, w := range works {
		minted[i] = service.MintedWork{ID: w.ID}
		live[w.ID] = w.Status == model.WorkStatusLive
	}
	hits, err := service.ScreenMintedWorks(tx, minted)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range service.ScreenedWorkIDs(hits) {
		if live[id] {
			n++
		}
	}
	return n, nil
}
