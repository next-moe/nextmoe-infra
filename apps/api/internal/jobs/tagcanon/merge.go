package tagcanon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MergeOpts struct {
	DSN    string
	Merges string
	Apply  bool
}

type MergeStats struct {
	Records        int
	Planned        int
	AlreadyMerged  int
	MapsRepointed  int
	CuratedAliases int
	IntrosMoved    int
	IntrosDropped  int
	CountsDropped  int
	ParentsMoved   int
	TagsDeleted    int
	OpenProposals  int
	Errors         int
}

// mergeRec folds the losing canonical into the winner. Both the id and the name
// are given on each side: a merge file outlives the ids it was written against,
// and a stale id would otherwise silently dissolve an unrelated tag.
type mergeRec struct {
	FromID int64  `json:"from_id"`
	From   string `json:"from"`
	IntoID int64  `json:"into_id"`
	Into   string `json:"into"`
	Reason string `json:"reason,omitempty"`
}

func Merge(ctx context.Context, opts MergeOpts) (*MergeStats, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("catalog DSN is required (--dsn)")
	}
	if opts.Merges == "" {
		return nil, fmt.Errorf("merges path is required (--merges)")
	}
	recs, err := readMergeRecords(opts.Merges)
	if err != nil {
		return nil, err
	}
	db, err := openGorm(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	if sqlDB, e := db.DB(); e == nil {
		defer sqlDB.Close()
	}
	src, err := resolveSources(ctx, db)
	if err != nil {
		return nil, err
	}

	st := &MergeStats{Records: len(recs)}
	for _, r := range recs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		state, err := planMerge(ctx, db, r)
		if err != nil {
			st.Errors++
			slog.Warn("merge refused", "from", r.From, "from_id", r.FromID,
				"into", r.Into, "into_id", r.IntoID, "err", err)
			continue
		}
		if state == mergeDone {
			st.AlreadyMerged++
			continue
		}
		st.Planned++

		open := openProposalsFor(ctx, db, r.FromID)
		st.OpenProposals += open
		if open > 0 {
			slog.Warn("open edit proposals still name the losing tag — their authors must re-pick it",
				"from", r.From, "from_id", r.FromID, "open", open)
		}
		maps, intros, counts := mergeVolume(ctx, db, r)
		slog.Info("merge planned", "from", r.From, "from_id", r.FromID, "into", r.Into,
			"into_id", r.IntoID, "maps", maps, "intros", intros, "counts", counts, "reason", r.Reason)
		if !opts.Apply {
			continue
		}
		if err := applyMerge(ctx, db, src.curated, r, st); err != nil {
			st.Errors++
			slog.Warn("merge failed", "from", r.From, "into", r.Into, "err", err)
		}
	}

	slog.Info("tag merge done", "apply", opts.Apply, "records", st.Records, "planned", st.Planned,
		"already_merged", st.AlreadyMerged, "maps_repointed", st.MapsRepointed,
		"curated_aliases", st.CuratedAliases, "intros_moved", st.IntrosMoved,
		"intros_dropped", st.IntrosDropped, "counts_dropped", st.CountsDropped, "parents_moved", st.ParentsMoved,
		"tags_deleted", st.TagsDeleted, "open_proposals", st.OpenProposals, "errors", st.Errors)
	return st, nil
}

type mergeState int

const (
	mergePlanned mergeState = iota
	mergeDone
)

func planMerge(ctx context.Context, db *gorm.DB, r mergeRec) (mergeState, error) {
	if r.FromID <= 0 || r.IntoID <= 0 || r.From == "" || r.Into == "" {
		return mergePlanned, fmt.Errorf("malformed record (need from_id, from, into_id, into)")
	}
	if r.FromID == r.IntoID {
		return mergePlanned, fmt.Errorf("from_id == into_id")
	}
	from, err := tagNameByID(ctx, db, r.FromID)
	if err != nil {
		return mergePlanned, err
	}
	into, err := tagNameByID(ctx, db, r.IntoID)
	if err != nil {
		return mergePlanned, err
	}
	if into == "" {
		return mergePlanned, fmt.Errorf("winner %d does not exist", r.IntoID)
	}
	if into != r.Into {
		return mergePlanned, fmt.Errorf("winner %d is named %q, the record says %q", r.IntoID, into, r.Into)
	}
	if from == "" {
		return mergeDone, nil
	}
	if from != r.From {
		return mergePlanned, fmt.Errorf("loser %d is named %q, the record says %q", r.FromID, from, r.From)
	}
	return mergePlanned, nil
}

func applyMerge(ctx context.Context, db *gorm.DB, curated int16, r mergeRec, st *MergeStats) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(`UPDATE catalog_tag_source_map SET tag_id = ? WHERE tag_id = ?`, r.IntoID, r.FromID)
		if res.Error != nil {
			return res.Error
		}
		st.MapsRepointed += int(res.RowsAffected)

		// The losing NAME stays resolvable through the curated lane: curated
		// catalog_work_tag rows carry the canonical name they were written with,
		// and nothing else would point them at the winner once the tag is gone.
		res = tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "source_id"}, {Name: "source_name"}},
			DoNothing: true,
		}).Create(&model.CatalogTagSourceMap{SourceID: curated, SourceName: r.From, TagID: r.IntoID})
		if res.Error != nil {
			return res.Error
		}
		st.CuratedAliases += int(res.RowsAffected)

		res = tx.Exec(`
			UPDATE catalog_tag_intro i SET tag_id = ? WHERE i.tag_id = ?
			AND NOT EXISTS (SELECT 1 FROM catalog_tag_intro j
				WHERE j.tag_id = ? AND j.lang = i.lang AND j.source_id = i.source_id)`,
			r.IntoID, r.FromID, r.IntoID)
		if res.Error != nil {
			return res.Error
		}
		st.IntrosMoved += int(res.RowsAffected)

		res = tx.Exec(`DELETE FROM catalog_tag_intro WHERE tag_id = ?`, r.FromID)
		if res.Error != nil {
			return res.Error
		}
		st.IntrosDropped += int(res.RowsAffected)

		// catalog_tag_work_count has no foreign key, so a stale row would keep
		// answering for a tag that no longer exists; the hourly refresh recomputes
		// the winner.
		res = tx.Exec(`DELETE FROM catalog_tag_work_count WHERE tag_id = ?`, r.FromID)
		if res.Error != nil {
			return res.Error
		}
		st.CountsDropped += int(res.RowsAffected)

		res = tx.Exec(`
			INSERT INTO catalog_tag_parent (tag_id, parent_id, origin, note, created_at)
			SELECT CASE WHEN tag_id = ? THEN ? ELSE tag_id END,
			       CASE WHEN parent_id = ? THEN ? ELSE parent_id END,
			       origin, note, created_at
			FROM catalog_tag_parent
			WHERE (tag_id = ? AND parent_id <> ?) OR (parent_id = ? AND tag_id <> ?)
			ON CONFLICT DO NOTHING`,
			r.FromID, r.IntoID, r.FromID, r.IntoID, r.FromID, r.IntoID, r.FromID, r.IntoID)
		if res.Error != nil {
			return res.Error
		}
		st.ParentsMoved += int(res.RowsAffected)
		if err := tx.Exec(`DELETE FROM catalog_tag_parent WHERE tag_id = ? OR parent_id = ?`, r.FromID, r.FromID).Error; err != nil {
			return err
		}

		res = tx.Exec(`DELETE FROM catalog_tag WHERE id = ?`, r.FromID)
		if res.Error != nil {
			return res.Error
		}
		st.TagsDeleted += int(res.RowsAffected)
		return nil
	})
}

func tagNameByID(ctx context.Context, db *gorm.DB, id int64) (string, error) {
	var names []string
	if err := db.WithContext(ctx).Raw(`SELECT name FROM catalog_tag WHERE id = ?`, id).Scan(&names).Error; err != nil {
		return "", err
	}
	if len(names) == 0 {
		return "", nil
	}
	return names[0], nil
}

func mergeVolume(ctx context.Context, db *gorm.DB, r mergeRec) (maps, intros, counts int) {
	var row struct {
		Maps   int `gorm:"column:maps"`
		Intros int `gorm:"column:intros"`
		Counts int `gorm:"column:counts"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT
		(SELECT count(*) FROM catalog_tag_source_map WHERE tag_id = ?) AS maps,
		(SELECT count(*) FROM catalog_tag_intro WHERE tag_id = ?) AS intros,
		(SELECT count(*) FROM catalog_tag_work_count WHERE tag_id = ?) AS counts`,
		r.FromID, r.FromID, r.FromID).Scan(&row).Error; err != nil {
		slog.Warn("merge volume", "from_id", r.FromID, "err", err)
		return 0, 0, 0
	}
	return row.Maps, row.Intros, row.Counts
}

func openProposalsFor(ctx context.Context, db *gorm.DB, tagID int64) int {
	var n int
	if err := db.WithContext(ctx).Raw(`
		SELECT count(*) FROM edit_proposal
		WHERE status = 0 AND patch->'catalog.work.tag_ids' @> ?::jsonb`,
		fmt.Sprintf("%d", tagID)).Scan(&n).Error; err != nil {
		slog.Warn("open proposal probe", "tag_id", tagID, "err", err)
		return 0
	}
	return n
}

func readMergeRecords(path string) ([]mergeRec, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []mergeRec
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for sc.Scan() {
		line++
		b := sc.Bytes()
		if len(trimSpaceBytes(b)) == 0 {
			continue
		}
		var r mergeRec
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		out = append(out, r)
	}
	return out, sc.Err()
}
