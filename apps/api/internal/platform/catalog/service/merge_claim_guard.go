package service

import (
	"errors"
	"fmt"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

// ErrHiddenSurvivor refuses folding a work into one whose claim is hidden
// unless the source is hidden too: the merged work vanishes from every read
// face, and downstream sites cannot fold their rows into a page nobody can
// open. On 2026-10-12 seven forum folds retried every 30 minutes for exactly
// this reason; only two of their eight sources were claimed at all, and the
// survivors were hidden only by the 2026-07-31 w1-resite backfill.
var ErrHiddenSurvivor = errors.New("catalog merge: the target's claim is hidden and the source's is not")

func assertSurvivorNotHidden(db *gorm.DB, entityType int16, src, dst int64) error {
	if entityType != model.EntityTypeWork {
		return nil
	}
	var rows []struct {
		ID            int64   `gorm:"column:id"`
		Site          *string `gorm:"column:site"`
		ProductWorkID *int64  `gorm:"column:product_work_id"`
		ClaimState    *int16  `gorm:"column:claim_state"`
	}
	if err := db.Raw(`SELECT id, site, product_work_id, claim_state FROM catalog_work WHERE id IN ?`,
		[]int64{src, dst}).Scan(&rows).Error; err != nil {
		return err
	}
	state := map[int64]string{}
	for _, r := range rows {
		state[r.ID] = model.ClaimStateKey(r.Site, r.ProductWorkID, r.ClaimState)
	}
	if state[dst] == model.ClaimStateKeyHidden && state[src] != model.ClaimStateKeyHidden {
		return fmt.Errorf("%w: work %d into %d; file it the other way round, or unhide %d first", ErrHiddenSurvivor, src, dst, dst)
	}
	return nil
}
