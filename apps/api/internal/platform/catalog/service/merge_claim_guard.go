package service

import (
	"errors"
	"fmt"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

var ErrSurvivorClaim = errors.New("catalog merge: the survivor's claim cannot take the source's")

// ErrHiddenSurvivor refuses folding a work into one whose claim is hidden
// unless the source is hidden too: the merged work vanishes from every read
// face, and downstream sites cannot fold their rows into a page nobody can
// open. On 2026-10-12 seven forum folds retried every 30 minutes for exactly
// this reason; only two of their eight sources were claimed at all, and the
// survivors were hidden only by the 2026-07-31 w1-resite backfill.
var ErrHiddenSurvivor = fmt.Errorf("%w: the target's claim is hidden and the source's is not", ErrSurvivorClaim)

// ErrWeakerSurvivorClaim refuses folding a claimed work into one whose own
// claim ranks below it (model.ClaimWeight). An unclaimed target takes the
// source's claim; a claimed one keeps its own and drops the source's, so a
// published page folded into a draft would leave the merged work owned by an
// unpublished one.
var ErrWeakerSurvivorClaim = fmt.Errorf("%w: the source's claim outranks the target's", ErrSurvivorClaim)

func assertSurvivorClaim(db *gorm.DB, entityType int16, src, dst int64) error {
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
	if state[dst] != model.ClaimStateKeyNone && state[src] != model.ClaimStateKeyHidden &&
		model.ClaimWeight(state[src]) > model.ClaimWeight(state[dst]) {
		return fmt.Errorf("%w: work %d (%s) into %d (%s); file it the other way round",
			ErrWeakerSurvivorClaim, src, state[src], dst, state[dst])
	}
	return nil
}
