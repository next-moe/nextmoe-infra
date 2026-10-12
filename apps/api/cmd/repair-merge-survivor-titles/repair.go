package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

type opts struct {
	Apply     bool
	Batch     int
	Receipts  io.Writer
	Leftovers io.Writer
}

type stats struct {
	lineageStats
	Survivors             int
	SurvivorsNotLive      int
	SurvivorsSnapshotless int
	Works                 int
	WorksClaimed          int
	Rows                  int
	RowsDemoted           int
	RowsDeleted           int
	RowsPromoted          int
	RowsBySnapshot        int
	RowsByDisplayName     int
	WorksByDisplayName    int
	WorksPromoted         int
	Leftovers             int
	LeftoversClaimed      int
}

func (s stats) logArgs() []any {
	return []any{
		"losers", s.Losers, "losers_with_snapshot", s.LosersWithSnapshot, "losers_snapshotless", s.LosersSnapshotless,
		"survivors", s.Survivors, "survivors_not_live", s.SurvivorsNotLive,
		"survivors_snapshotless", s.SurvivorsSnapshotless,
		"works", s.Works, "works_claimed", s.WorksClaimed,
		"rows", s.Rows, "rows_demoted", s.RowsDemoted, "rows_deleted", s.RowsDeleted, "rows_promoted", s.RowsPromoted,
		"rows_reason_snapshot", s.RowsBySnapshot, "rows_reason_display_name", s.RowsByDisplayName,
		"works_reason_display_name", s.WorksByDisplayName, "works_own_alias_promoted", s.WorksPromoted,
		"leftovers", s.Leftovers, "leftovers_claimed", s.LeftoversClaimed,
	}
}

type leftover struct {
	WorkID       int64  `json:"work_id"`
	OLang        string `json:"olang"`
	DisplayName  string `json:"display_name"`
	Shown        string `json:"shown"`
	ShownKind    int16  `json:"shown_kind"`
	Claimed      bool   `json:"claimed"`
	Snapshotless bool   `json:"snapshotless"`
	Losers       int    `json:"losers"`
}

func run(ctx context.Context, db *gorm.DB, o opts) (stats, error) {
	lineages, ls, err := loadLineages(ctx, db)
	if err != nil {
		return stats{}, fmt.Errorf("load merge lineage: %w", err)
	}
	st := stats{lineageStats: ls, Survivors: len(lineages)}
	survivors := make([]int64, 0, len(lineages))
	for id, lin := range lineages {
		survivors = append(survivors, id)
		if lin.snapshotless {
			st.SurvivorsSnapshotless++
		}
	}
	slices.Sort(survivors)

	receipts := json.NewEncoder(o.Receipts)
	leftovers := json.NewEncoder(o.Leftovers)
	for ids := range slices.Chunk(survivors, o.Batch) {
		works, plans, err := repairBatch(ctx, db, ids, lineages, o.Apply)
		if err != nil {
			return st, fmt.Errorf("batch from work %d: %w", ids[0], err)
		}
		st.SurvivorsNotLive += len(ids) - len(works)
		for i, w := range works {
			if err := st.record(w, lineages[w.ID], plans[i], receipts, leftovers); err != nil {
				return st, err
			}
		}
	}
	return st, nil
}

func repairBatch(ctx context.Context, db *gorm.DB, ids []int64, lineages map[int64]*lineage, apply bool) ([]work, [][]change, error) {
	plan := func(tx *gorm.DB) ([]work, [][]change, error) {
		works, err := loadWorks(ctx, tx, ids, apply)
		if err != nil {
			return nil, nil, err
		}
		plans := make([][]change, len(works))
		for i, w := range works {
			plans[i] = planWork(w, lineages[w.ID])
		}
		return works, plans, nil
	}
	if !apply {
		return plan(db)
	}
	var works []work
	var plans [][]change
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		if works, plans, err = plan(tx); err != nil {
			return err
		}
		var touched []int64
		for i, w := range works {
			if len(plans[i]) == 0 {
				continue
			}
			if err := writeChanges(tx, plans[i]); err != nil {
				return fmt.Errorf("work %d: %w", w.ID, err)
			}
			touched = append(touched, w.ID)
		}
		return repository.TouchWorks(ctx, tx, touched)
	})
	return works, plans, err
}

func (st *stats) record(w work, lin *lineage, plan []change, receipts, leftovers *json.Encoder) error {
	if len(plan) > 0 {
		st.Works++
		if w.Claimed {
			st.WorksClaimed++
		}
		byDisplay, promoted := false, false
		for _, c := range plan {
			st.Rows++
			switch c.Action {
			case actionDelete:
				st.RowsDeleted++
			case actionDemote:
				st.RowsDemoted++
			case actionPromote:
				st.RowsPromoted++
				promoted = true
			}
			switch c.Reason {
			case reasonSnapshot:
				st.RowsBySnapshot++
			case reasonDisplayName:
				st.RowsByDisplayName++
				byDisplay = true
			}
			if err := receipts.Encode(c); err != nil {
				return fmt.Errorf("write receipt: %w", err)
			}
		}
		if byDisplay {
			st.WorksByDisplayName++
		}
		if promoted {
			st.WorksPromoted++
		}
	}
	shown, ok := shownTitle(w.Titles, plan, w.OLang)
	if !ok || shown.Title == w.DisplayName {
		return nil
	}
	st.Leftovers++
	if w.Claimed {
		st.LeftoversClaimed++
	}
	if err := leftovers.Encode(leftover{
		WorkID: w.ID, OLang: w.OLang, DisplayName: w.DisplayName, Shown: shown.Title, ShownKind: shown.Kind,
		Claimed: w.Claimed, Snapshotless: lin.snapshotless, Losers: lin.losers,
	}); err != nil {
		return fmt.Errorf("write leftover: %w", err)
	}
	return nil
}

func loadWorks(ctx context.Context, db *gorm.DB, ids []int64, lock bool) ([]work, error) {
	q := `SELECT id, olang, display_name, site IS NOT NULL AS claimed FROM catalog_work
		WHERE id IN ? AND deleted_at IS NULL AND status <> ? ORDER BY id`
	if lock {
		q += ` FOR UPDATE`
	}
	var rows []struct {
		ID          int64  `gorm:"column:id"`
		OLang       string `gorm:"column:olang"`
		DisplayName string `gorm:"column:display_name"`
		Claimed     bool   `gorm:"column:claimed"`
	}
	if err := db.WithContext(ctx).Raw(q, ids, model.WorkStatusMerged).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	live := make([]int64, len(rows))
	for i, r := range rows {
		live[i] = r.ID
	}
	var titles []titleRow
	if err := db.WithContext(ctx).Raw(`
		SELECT t.id, t.work_id, t.lang, t.title, coalesce(t.latin, '') AS latin, t.kind, t.provenance,
		       NOT (`+editspec.NotSuppressedWorkTitleSQL("t")+`) AS suppressed
		FROM catalog_work_title t WHERE t.work_id IN ? ORDER BY t.work_id, t.id`, live).
		Scan(&titles).Error; err != nil {
		return nil, err
	}
	byWork := map[int64][]titleRow{}
	for _, t := range titles {
		byWork[t.WorkID] = append(byWork[t.WorkID], t)
	}
	suppressed, err := editspec.SuppressedWorkTitles(ctx, db, live)
	if err != nil {
		return nil, err
	}
	out := make([]work, len(rows))
	for i, r := range rows {
		out[i] = work{ID: r.ID, OLang: r.OLang, DisplayName: r.DisplayName, Claimed: r.Claimed,
			Titles: byWork[r.ID], Suppressed: suppressed[r.ID]}
	}
	return out, nil
}

func writeChanges(tx *gorm.DB, plan []change) error {
	for _, c := range plan {
		var res *gorm.DB
		if c.Action == actionDelete {
			res = tx.Exec(`DELETE FROM catalog_work_title WHERE id = ? AND work_id = ? AND kind = ?`,
				c.TitleID, c.WorkID, c.FromKind)
		} else {
			res = tx.Exec(`UPDATE catalog_work_title SET kind = ?, lang = ? WHERE id = ? AND work_id = ? AND kind = ?`,
				*c.ToKind, *c.ToLang, c.TitleID, c.WorkID, c.FromKind)
		}
		if res.Error != nil {
			return fmt.Errorf("title %d: %w", c.TitleID, res.Error)
		}
		if res.RowsAffected != 1 {
			return fmt.Errorf("title %d: %s touched %d rows, want 1", c.TitleID, c.Action, res.RowsAffected)
		}
	}
	return nil
}
