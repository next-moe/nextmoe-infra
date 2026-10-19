package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

const bangumiSourceID = 3

type stats struct {
	Moved, Deleted, Works                  int
	SuppressionsMoved, SuppressionsDropped int
}

type creditRow struct {
	ID           int64     `json:"id"`
	WorkID       int64     `json:"work_id"`
	CreditNameID int64     `json:"credit_name_id"`
	LabelID      *int64    `json:"label_id"`
	RoleID       int64     `json:"role_id"`
	CharacterID  *int64    `json:"character_id"`
	ReleaseID    *int64    `json:"release_id"`
	Spoiler      int16     `json:"spoiler"`
	Note         string    `json:"note"`
	SourceID     *int16    `json:"source_id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	TwinID       *int64    `json:"-"`
}

type receipt struct {
	Action string    `json:"action"`
	ToRole int64     `json:"to_role_id"`
	TwinID *int64    `json:"twin_id,omitempty"`
	Before creditRow `json:"before"`
}

const gameWorkSQL = `w.medium_id IN (SELECT id FROM catalog_medium WHERE key IN ('galgame', 'doujin_game'))`

func run(ctx context.Context, db *gorm.DB, gameRoleToShared map[int64]int64, apply bool, out io.Writer) (stats, error) {
	var st stats
	if len(gameRoleToShared) == 0 {
		return st, fmt.Errorf("no game roles to move")
	}
	from := make([]int64, 0, len(gameRoleToShared))
	for id := range gameRoleToShared {
		from = append(from, id)
	}
	sort.Slice(from, func(i, j int) bool { return from[i] < from[j] })

	enc := json.NewEncoder(out)
	works := map[int64]bool{}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, old := range from {
			shared := gameRoleToShared[old]
			var rows []creditRow
			if err := tx.Raw(`
				SELECT c.*,
				       (SELECT d.id FROM catalog_credit d
				         WHERE d.work_id = c.work_id AND d.credit_name_id = c.credit_name_id
				           AND d.role_id = ? AND COALESCE(d.character_id, 0) = COALESCE(c.character_id, 0)) AS twin_id
				FROM catalog_credit c JOIN catalog_work w ON w.id = c.work_id
				WHERE c.role_id = ? AND c.source_id = ? AND `+gameWorkSQL+`
				ORDER BY c.id FOR UPDATE OF c`, shared, old, bangumiSourceID).Scan(&rows).Error; err != nil {
				return err
			}
			var moveIDs, deleteIDs []int64
			for _, r := range rows {
				works[r.WorkID] = true
				rec := receipt{Action: "move", ToRole: shared, Before: r}
				if r.TwinID != nil {
					rec.Action, rec.TwinID = "delete", r.TwinID
					deleteIDs = append(deleteIDs, r.ID)
				} else {
					moveIDs = append(moveIDs, r.ID)
				}
				if err := enc.Encode(rec); err != nil {
					return err
				}
			}
			st.Moved += len(moveIDs)
			st.Deleted += len(deleteIDs)
			carried, dropped, err := carrySuppressions(tx, shared, moveIDs, deleteIDs, apply)
			if err != nil {
				return err
			}
			st.SuppressionsMoved += carried
			st.SuppressionsDropped += dropped
			if !apply {
				continue
			}
			if len(deleteIDs) > 0 {
				if err := tx.Exec(`DELETE FROM catalog_credit WHERE id IN ?`, deleteIDs).Error; err != nil {
					return err
				}
			}
			if len(moveIDs) > 0 {
				if err := tx.Exec(`UPDATE catalog_credit SET role_id = ?, updated_at = now() WHERE id IN ?`, shared, moveIDs).Error; err != nil {
					return err
				}
			}
		}
		st.Works = len(works)
		if !apply {
			return nil
		}
		ids := make([]int64, 0, len(works))
		for id := range works {
			ids = append(ids, id)
		}
		return repository.TouchWorks(ctx, tx, ids)
	})
	return st, err
}

// carrySuppressions runs before the credits change, while catalog_credit still
// holds the old role_id the suppression keys were built from. A moved credit's
// suppression is re-keyed to the shared role; a twin-deleted credit's is
// dropped, because the twin was visible when the credit was hidden.
func carrySuppressions(tx *gorm.DB, shared int64, moveIDs, deleteIDs []int64, apply bool) (carried, dropped int, err error) {
	all := append(append([]int64{}, moveIDs...), deleteIDs...)
	if len(all) == 0 {
		return 0, 0, nil
	}
	oldKey := editspec.CreditIdentitySQL("c")
	newKey := `('credit:' || ?::bigint || ':' || c.credit_name_id || ':' || COALESCE(c.character_id, 0))`
	onCredit := `esr.entity_type = ? AND esr.field_key = ? AND esr.entity_id = c.work_id AND esr.identity_key = ` + oldKey
	newKeyFree := `NOT EXISTS (SELECT 1 FROM edit_suppressed_row x
		WHERE x.entity_type = esr.entity_type AND x.entity_id = esr.entity_id
		  AND x.field_key = esr.field_key AND x.identity_key = ` + newKey + `)`

	var total, movable int64
	if err := tx.Raw(`SELECT count(*) FROM edit_suppressed_row esr JOIN catalog_credit c ON `+onCredit+`
		WHERE c.id IN ?`, editspec.TypeWork, editspec.FieldWorkCredits, all).Scan(&total).Error; err != nil {
		return 0, 0, err
	}
	if len(moveIDs) > 0 {
		if err := tx.Raw(`SELECT count(*) FROM edit_suppressed_row esr JOIN catalog_credit c ON `+onCredit+`
			WHERE c.id IN ? AND `+newKeyFree,
			editspec.TypeWork, editspec.FieldWorkCredits, moveIDs, shared).Scan(&movable).Error; err != nil {
			return 0, 0, err
		}
	}
	if apply {
		if len(moveIDs) > 0 {
			if err := tx.Exec(`UPDATE edit_suppressed_row esr SET identity_key = `+newKey+`
				FROM catalog_credit c WHERE `+onCredit+` AND c.id IN ? AND `+newKeyFree,
				shared, editspec.TypeWork, editspec.FieldWorkCredits, moveIDs, shared).Error; err != nil {
				return 0, 0, err
			}
		}
		if err := tx.Exec(`DELETE FROM edit_suppressed_row esr USING catalog_credit c
			WHERE `+onCredit+` AND c.id IN ?`,
			editspec.TypeWork, editspec.FieldWorkCredits, all).Error; err != nil {
			return 0, 0, err
		}
	}
	return int(movable), int(total - movable), nil
}
