package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

const bangumiSourceID = 3

type stats struct {
	Moved, Deleted, Works int
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
