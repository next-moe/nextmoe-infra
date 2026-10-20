package tagcanon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MergeOpts struct {
	DSN    string
	Merges string
	Apply  bool
}

type MergeStats struct {
	Records           int
	Planned           int
	AlreadyMerged     int
	MapsRepointed     int
	CuratedAliases    int
	SuppressionsMoved int
	IntrosMoved       int
	IntrosDropped     int
	CountsDropped     int
	ParentsMoved      int
	BlocksMoved       int
	TagsDeleted       int
	OpenProposals     int
	Errors            int
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
		"curated_aliases", st.CuratedAliases, "suppressions_moved", st.SuppressionsMoved, "intros_moved", st.IntrosMoved,
		"intros_dropped", st.IntrosDropped, "counts_dropped", st.CountsDropped, "parents_moved", st.ParentsMoved, "blocks_moved", st.BlocksMoved,
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

		moved, err := followWorkTagSuppressions(tx, r.FromID, r.IntoID)
		if err != nil {
			return err
		}
		st.SuppressionsMoved += moved

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
		moved, err = followTagParentSuppressions(tx, r.FromID, r.IntoID)
		if err != nil {
			return err
		}
		st.SuppressionsMoved += moved
		if err := editing.DeclineOpenProposals(tx, editspec.TypeTag, r.FromID, nil,
			fmt.Sprintf("条目已合并到 #%d，此提案随之关闭", r.IntoID)); err != nil {
			return err
		}

		if err := tx.Exec(`
			UPDATE catalog_user_tag_block w SET mode = ?, updated_at = now()
			FROM catalog_user_tag_block l
			WHERE w.tag_id = ? AND l.tag_id = ? AND l.actor_uid = w.actor_uid AND l.mode = ? AND w.mode <> ?`,
			model.TagBlockModeTree, r.IntoID, r.FromID, model.TagBlockModeTree, model.TagBlockModeTree).Error; err != nil {
			return err
		}
		res = tx.Exec(`
			UPDATE catalog_user_tag_block l SET tag_id = ?
			WHERE l.tag_id = ? AND NOT EXISTS (SELECT 1 FROM catalog_user_tag_block w
				WHERE w.actor_uid = l.actor_uid AND w.tag_id = ?)`, r.IntoID, r.FromID, r.IntoID)
		if res.Error != nil {
			return res.Error
		}
		st.BlocksMoved += int(res.RowsAffected)
		if err := tx.Exec(`DELETE FROM catalog_user_tag_block WHERE tag_id = ?`, r.FromID).Error; err != nil {
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

func followWorkTagSuppressions(tx *gorm.DB, fromID, intoID int64) (int, error) {
	from, into := editspec.WorkTagIdentity(fromID), editspec.WorkTagIdentity(intoID)
	res := tx.Exec(`
		UPDATE edit_suppressed_row s SET identity_key = ?
		WHERE s.entity_type = ? AND s.field_key = ? AND s.identity_key = ?
		  AND NOT EXISTS (SELECT 1 FROM edit_suppressed_row x
		                   WHERE x.entity_type = s.entity_type AND x.entity_id = s.entity_id
		                     AND x.field_key = s.field_key AND x.identity_key = ?)`,
		into, editspec.TypeWork, editspec.FieldWorkTagIDs, from, into)
	if res.Error != nil {
		return 0, res.Error
	}
	if err := tx.Exec(`DELETE FROM edit_suppressed_row WHERE entity_type = ? AND field_key = ? AND identity_key = ?`,
		editspec.TypeWork, editspec.FieldWorkTagIDs, from).Error; err != nil {
		return 0, err
	}
	var works []int64
	if err := tx.Raw(`SELECT entity_id FROM edit_suppressed_row WHERE entity_type = ? AND field_key = ? AND identity_key = ?`,
		editspec.TypeWork, editspec.FieldWorkTagIDs, into).Scan(&works).Error; err != nil {
		return 0, err
	}
	for _, workID := range works {
		if err := tx.Exec(editspec.PurgeSuppressedWorkTagsSQL, workID).Error; err != nil {
			return 0, err
		}
	}
	return int(res.RowsAffected), nil
}

// followTagParentSuppressions runs after the loser's edges moved to the
// winner: a suppression the winner or the loser held must keep hiding the
// edge it names, and the moved VNDB edges must not resurrect one.
func followTagParentSuppressions(tx *gorm.DB, fromID, intoID int64) (int, error) {
	from, into := editspec.ParentIdentity(fromID), editspec.ParentIdentity(intoID)
	moved := 0
	res := tx.Exec(`
		UPDATE edit_suppressed_row s SET identity_key = ?
		WHERE s.entity_type = ? AND s.field_key = ? AND s.identity_key = ? AND s.entity_id <> ?
		  AND NOT EXISTS (SELECT 1 FROM edit_suppressed_row x
		                   WHERE x.entity_type = s.entity_type AND x.entity_id = s.entity_id
		                     AND x.field_key = s.field_key AND x.identity_key = ?)`,
		into, editspec.TypeTag, editspec.FieldTagParents, from, intoID, into)
	if res.Error != nil {
		return 0, res.Error
	}
	moved += int(res.RowsAffected)
	res = tx.Exec(`
		UPDATE edit_suppressed_row s SET entity_id = ?
		WHERE s.entity_type = ? AND s.entity_id = ? AND s.identity_key <> ?
		  AND NOT EXISTS (SELECT 1 FROM edit_suppressed_row x
		                   WHERE x.entity_type = s.entity_type AND x.entity_id = ?
		                     AND x.field_key = s.field_key AND x.identity_key = s.identity_key)`,
		intoID, editspec.TypeTag, fromID, into, intoID)
	if res.Error != nil {
		return 0, res.Error
	}
	moved += int(res.RowsAffected)
	if err := tx.Exec(`DELETE FROM edit_suppressed_row
		WHERE entity_type = ? AND (entity_id = ? OR (field_key = ? AND identity_key = ?))`,
		editspec.TypeTag, fromID, editspec.FieldTagParents, from).Error; err != nil {
		return 0, err
	}
	if err := tx.Exec(`DELETE FROM catalog_tag_parent p
		WHERE (p.tag_id = ? OR p.parent_id = ?) AND p.origin = ? AND `+editspec.TagParentSuppressedSQL("p.tag_id", "p.parent_id"),
		intoID, intoID, model.TagParentOriginVNDB).Error; err != nil {
		return 0, err
	}
	return moved, nil
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
