package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/walkthrough"
	"api/internal/platform/editing"
	"api/internal/platform/provenance"

	"gorm.io/gorm"
)

const (
	repairRepoint   = "repoint"
	repairHide      = "hide"
	repairUnchanged = "unchanged"
	repairMissing   = "missing"
	repairMoved     = "moved"
	repairHuman     = "human_edited"
	repairBadTarget = "target_not_galgame"
)

var repairActions = []string{repairRepoint, repairHide, repairUnchanged, repairMissing, repairMoved, repairHuman, repairBadTarget}

type repairRow struct {
	WalkthroughID int64
	FromWork      int64
	ToWork        int64
}

type repairCurrent struct {
	ID              int64
	WorkID          int64
	SourceID        int16
	Status          int16
	Medium          string
	FieldProvenance string
	Chart           string
}

func parseRepairPlan(r io.Reader) ([]repairRow, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	seen := map[int64]bool{}
	var out []repairRow
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "" || n == 1 && strings.HasPrefix(line, "walkthrough_id") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 3 {
			return nil, fmt.Errorf("line %d: want walkthrough_id, work_id and a target work id or %q", n, repairHide)
		}
		var row repairRow
		var err error
		if row.WalkthroughID, err = strconv.ParseInt(f[0], 10, 64); err != nil || row.WalkthroughID <= 0 {
			return nil, fmt.Errorf("line %d: walkthrough_id %q", n, f[0])
		}
		if row.FromWork, err = strconv.ParseInt(f[1], 10, 64); err != nil || row.FromWork <= 0 {
			return nil, fmt.Errorf("line %d: work_id %q", n, f[1])
		}
		if f[2] != repairHide {
			if row.ToWork, err = strconv.ParseInt(f[2], 10, 64); err != nil || row.ToWork <= 0 {
				return nil, fmt.Errorf("line %d: target %q is neither a work id nor %q", n, f[2], repairHide)
			}
		}
		if seen[row.WalkthroughID] {
			return nil, fmt.Errorf("line %d: walkthrough %d is planned twice", n, row.WalkthroughID)
		}
		seen[row.WalkthroughID] = true
		out = append(out, row)
	}
	return out, sc.Err()
}

func loadRepairPlan(path string) ([]repairRow, error) {
	if path == "" {
		return nil, fmt.Errorf("--plan is required by the repair stage")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	plan, err := parseRepairPlan(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return plan, nil
}

func humanEdited(doc string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(doc), &fields) != nil {
		return true
	}
	for _, entries := range fields {
		if provenance.IsHuman(provenance.Head(entries)) {
			return true
		}
	}
	return false
}

func repairAction(r repairRow, c *repairCurrent, galgame map[int64]bool) string {
	switch {
	case c == nil:
		return repairMissing
	case r.ToWork != 0 && c.WorkID == r.ToWork,
		r.ToWork == 0 && c.WorkID == r.FromWork && c.Status == model.WalkthroughStatusHidden:
		return repairUnchanged
	case c.WorkID != r.FromWork:
		return repairMoved
	case humanEdited(c.FieldProvenance):
		return repairHuman
	case r.ToWork == 0:
		return repairHide
	case !galgame[r.ToWork]:
		return repairBadTarget
	}
	return repairRepoint
}

func runRepair(ctx context.Context, cat *gorm.DB, plan []repairRow, apply bool) error {
	ids := make([]int64, 0, len(plan))
	works := make([]int64, 0, 2*len(plan))
	for _, r := range plan {
		ids = append(ids, r.WalkthroughID)
		works = append(works, r.FromWork)
		if r.ToWork != 0 {
			works = append(works, r.ToWork)
		}
	}
	var rows []repairCurrent
	if len(ids) > 0 {
		if err := cat.WithContext(ctx).Raw(`
SELECT wt.id, wt.work_id, wt.source_id, wt.status, m.key AS medium,
  wt.field_provenance::text AS field_provenance, wt.chart::text AS chart
FROM catalog_walkthrough wt
JOIN catalog_work w ON w.id = wt.work_id
JOIN catalog_medium m ON m.id = w.medium_id
WHERE wt.id IN ?`, ids).Scan(&rows).Error; err != nil {
			return err
		}
	}
	current := make(map[int64]*repairCurrent, len(rows))
	for i := range rows {
		current[rows[i].ID] = &rows[i]
	}
	galgame, err := liveGalgameWorks(ctx, cat, works)
	if err != nil {
		return err
	}
	names, err := workNames(ctx, cat, works)
	if err != nil {
		return err
	}
	stamp, err := editing.StampJSON(provenance.SourceCurated)
	if err != nil {
		return err
	}
	mode := "DRY"
	if apply {
		mode = "APPLY"
	}
	fmt.Printf("\n=== import-walkthroughs repair (%s) plan=%d ===\n", mode, len(plan))

	counts := map[string]int{}
	var st applyStats
	for _, r := range plan {
		c := current[r.WalkthroughID]
		action := repairAction(r, c, galgame)
		counts[action]++
		from := fmt.Sprintf("%d %s", r.FromWork, names[r.FromWork])
		switch action {
		case repairRepoint:
			to := fmt.Sprintf("%d %s", r.ToWork, names[r.ToWork])
			if !apply {
				chart, pins, links, err := repairPins(ctx, cat, c, r.ToWork)
				if err != nil {
					return err
				}
				set, create := planLinks(chart, pins, links)
				st.Pinned += len(set)
				st.Created += len(create)
				fmt.Printf("%s\twt=%d\t%s (%s) -> %s\tcharacters=%d pinned=%d created=%d\n",
					action, r.WalkthroughID, from, c.Medium, to, len(chart.Characters), len(set), len(create))
				continue
			}
			before := st
			if err := cat.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				return repoint(ctx, tx, c, r.ToWork, string(stamp), &st)
			}); err != nil {
				return fmt.Errorf("walkthrough %d: %w", r.WalkthroughID, err)
			}
			fmt.Printf("%s\twt=%d\t%s (%s) -> %s\tpinned=%d created=%d\n",
				action, r.WalkthroughID, from, c.Medium, to, st.Pinned-before.Pinned, st.Created-before.Created)
		case repairHide:
			fmt.Printf("%s\twt=%d\t%s (%s)\n", action, r.WalkthroughID, from, c.Medium)
			if !apply {
				continue
			}
			if err := hideWalkthrough(ctx, cat, c, string(stamp)); err != nil {
				return fmt.Errorf("walkthrough %d: %w", r.WalkthroughID, err)
			}
		case repairUnchanged:
		default:
			now := "absent"
			if c != nil {
				now = fmt.Sprintf("%d (%s)", c.WorkID, c.Medium)
			}
			fmt.Printf("skip:%s\twt=%d\tplanned %s -> %d, now on %s\n", action, r.WalkthroughID, from, r.ToWork, now)
		}
	}
	if apply {
		if st.Retired, err = retireOrphanPlaceholders(cat.WithContext(ctx)); err != nil {
			return err
		}
	}
	var parts []string
	for _, a := range repairActions {
		parts = append(parts, fmt.Sprintf("%s=%d", a, counts[a]))
	}
	fmt.Printf("%s characters_pinned=%d characters_created=%d placeholders_retired=%d\n",
		strings.Join(parts, " "), st.Pinned, st.Created, st.Retired)
	if !apply {
		fmt.Println("[dry run] nothing written — re-run with --apply")
	}
	return nil
}

func workNames(ctx context.Context, cat *gorm.DB, ids []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		ID          int64
		DisplayName string
	}
	if err := cat.WithContext(ctx).Raw(`SELECT id, display_name FROM catalog_work WHERE id IN ?`, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = r.DisplayName
	}
	return out, nil
}

func repairPins(ctx context.Context, db *gorm.DB, c *repairCurrent, to int64) (walkthrough.Chart, map[string]characterPin, map[string]int64, error) {
	var chart walkthrough.Chart
	if err := json.Unmarshal([]byte(c.Chart), &chart); err != nil {
		return chart, nil, nil, fmt.Errorf("chart: %w", err)
	}
	roster, err := loadRoster(ctx, db, to)
	if err != nil {
		return chart, nil, nil, err
	}
	onRoster := make(map[int64]bool, len(roster))
	for _, r := range roster {
		onRoster[r.CharacterID] = true
	}
	var rows []model.CatalogWalkthroughCharacter
	if err := db.WithContext(ctx).Where("walkthrough_id = ?", c.ID).Find(&rows).Error; err != nil {
		return chart, nil, nil, err
	}
	links := map[string]int64{}
	for _, l := range rows {
		if onRoster[l.CharacterID] {
			links[l.ChartKey] = l.CharacterID
		}
	}
	return chart, pinCharacters(&chart, roster), links, nil
}

func repoint(ctx context.Context, tx *gorm.DB, c *repairCurrent, to int64, stamp string, st *applyStats) error {
	res := tx.WithContext(ctx).Exec(`
UPDATE catalog_walkthrough wt SET work_id = @to, updated_at = now(),
  release_id = CASE WHEN EXISTS (SELECT 1 FROM catalog_release r WHERE r.id = wt.release_id AND r.work_id = @to) THEN wt.release_id END,
  field_provenance = jsonb_set(wt.field_provenance, '{work_id}', CAST(@stamp AS jsonb) || COALESCE(wt.field_provenance -> 'work_id', '[]'::jsonb))
WHERE wt.id = @id AND wt.work_id = @from`,
		map[string]any{"to": to, "stamp": stamp, "id": c.ID, "from": c.WorkID})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return fmt.Errorf("moved off work %d during the repair", c.WorkID)
	}
	chart, pins, links, err := repairPins(ctx, tx, c, to)
	if err != nil {
		return err
	}
	return linkCharacters(tx, model.CatalogWalkthrough{ID: c.ID, WorkID: to, SourceID: c.SourceID}, chart, pins, links, st)
}

func hideWalkthrough(ctx context.Context, db *gorm.DB, c *repairCurrent, stamp string) error {
	res := db.WithContext(ctx).Exec(`
UPDATE catalog_walkthrough wt SET status = @hidden, updated_at = now(),
  field_provenance = jsonb_set(wt.field_provenance, '{status}', CAST(@stamp AS jsonb) || COALESCE(wt.field_provenance -> 'status', '[]'::jsonb))
WHERE wt.id = @id AND wt.work_id = @from`,
		map[string]any{"hidden": model.WalkthroughStatusHidden, "stamp": stamp, "id": c.ID, "from": c.WorkID})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return fmt.Errorf("moved off work %d during the repair", c.WorkID)
	}
	return nil
}
