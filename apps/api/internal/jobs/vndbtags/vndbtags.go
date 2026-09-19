package vndbtags

import (
	"context"
	"fmt"
	"log/slog"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/catalog/vndbtagmap"

	"gorm.io/gorm"
)

const (
	sourceKeyVNDB = "vndb"
	batchSize     = 500
)

type Opts struct {
	Apply         bool
	DSN           string
	Receipts      string
	MinMirrorRows int64
	Limit         int
}

type Stats struct {
	WorksPopulation   int
	WorksMultiAnchor  int
	WorksVNMissing    int
	WorksChanged      int
	OrphanWorks       int
	TagsDesired       int
	TagsSame          int
	TagsInserted      int
	TagsUpdated       int
	TagsDeleted       int
	TagsLost          int
	OrphanRowsRemoved int
	SexualInherited   int
	Errors            int
	FirstError        string
}

// The weekly cron reads a counter with a greedy
// sed -n "s/.*${name}=\([0-9]*\).*/\1/p", so a key that is a suffix of
// another key reads the wrong number.
func (st *Stats) LogArgs() []any {
	return []any{
		"works_population", st.WorksPopulation,
		"works_multi_anchor", st.WorksMultiAnchor,
		"works_vn_missing", st.WorksVNMissing,
		"works_changed", st.WorksChanged,
		"orphan_works", st.OrphanWorks,
		"tags_desired", st.TagsDesired,
		"tags_same", st.TagsSame,
		"tags_inserted", st.TagsInserted,
		"tags_updated", st.TagsUpdated,
		"tags_deleted", st.TagsDeleted,
		"tags_lost", st.TagsLost,
		"orphan_rows_removed", st.OrphanRowsRemoved,
		"sexual_inherited", st.SexualInherited,
		"errors", st.Errors,
	}
}

func Run(ctx context.Context, opts Opts) (*Stats, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("catalog DSN is required (--dsn); refusing to guess")
	}
	db, err := database.OpenJob(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	if sqlDB, e := db.DB(); e == nil {
		defer sqlDB.Close()
	}

	st := &Stats{}
	var mirrorRows int64
	if err := db.WithContext(ctx).Raw(`SELECT count(*) FROM src_vndb.tags_vn`).Scan(&mirrorRows).Error; err != nil {
		return nil, fmt.Errorf("count src_vndb.tags_vn (is the VNDB mirror loaded?): %w", err)
	}
	// audit-vndb-anchors carries the same guard because a partially loaded
	// mirror would otherwise have marked all ~64k anchors dead in one
	// transaction; here it would delete every VNDB tag.
	if mirrorRows < opts.MinMirrorRows {
		return nil, fmt.Errorf(
			"refusing to run: src_vndb.tags_vn holds %d rows, below the --min-mirror-rows floor of %d — "+
				"a partial mirror would delete every VNDB tag",
			mirrorRows, opts.MinMirrorRows)
	}

	var vndbID int16
	if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_source WHERE key = ?`, sourceKeyVNDB).Scan(&vndbID).Error; err != nil {
		return nil, fmt.Errorf("look up vndb source id: %w", err)
	}
	if vndbID == 0 {
		return nil, fmt.Errorf("catalog_source has no %q row", sourceKeyVNDB)
	}

	tagMeta, err := loadTagMeta(ctx, db)
	if err != nil {
		return nil, err
	}
	sexualByName, err := loadSexualByName(ctx, db, vndbID)
	if err != nil {
		return nil, err
	}
	pop, multi, missing, err := classifyWorks(ctx, db, vndbID)
	if err != nil {
		return nil, err
	}
	st.WorksMultiAnchor = multi
	st.WorksVNMissing = missing
	if opts.Limit > 0 && opts.Limit < len(pop) {
		pop = pop[:opts.Limit]
	}
	st.WorksPopulation = len(pop)

	w := &writer{db: db, stats: st, apply: opts.Apply, receiptsPath: opts.Receipts, sourceID: vndbID}
	defer func() { _ = w.closeReceipts() }()
	tagMap := vndbtagmap.Embedded()

	for start := 0; start < len(pop); start += batchSize {
		end := min(start+batchSize, len(pop))
		if err := processPopBatch(ctx, db, w, pop[start:end], tagMeta, tagMap, sexualByName); err != nil {
			w.noteError(err)
			slog.Warn("sync-vndb-tags batch", "from_work_id", pop[start].WorkID, "err", err)
		}
	}

	orphans, err := loadOrphans(ctx, db, vndbID)
	if err != nil {
		return nil, err
	}
	st.OrphanWorks = len(orphans)
	for start := 0; start < len(orphans); start += batchSize {
		end := min(start+batchSize, len(orphans))
		if err := processOrphanBatch(ctx, db, w, orphans[start:end]); err != nil {
			w.noteError(err)
			slog.Warn("sync-vndb-tags orphan batch", "from_work_id", orphans[start].WorkID, "err", err)
		}
	}

	if opts.Apply {
		n, err := repository.InheritTagSexual(ctx, db, vndbID)
		if err != nil {
			return st, fmt.Errorf("inherit tag sexual: %w", err)
		}
		st.SexualInherited = int(n)
	}
	if err := w.closeReceipts(); err != nil {
		return st, err
	}
	return st, nil
}

func (w *writer) noteError(err error) {
	w.stats.Errors++
	if w.stats.FirstError == "" {
		w.stats.FirstError = err.Error()
	}
}

type popWork struct {
	WorkID int64
	VID    string
}

type orphanWork struct {
	WorkID int64
	Rows   []Existing
}

func processPopBatch(
	ctx context.Context,
	db *gorm.DB,
	w *writer,
	batch []popWork,
	tagMeta map[string]TagMeta,
	tagMap map[string]string,
	sexualByName map[string]bool,
) error {
	vids := make([]string, 0, len(batch))
	workIDs := make([]int64, 0, len(batch))
	for _, pw := range batch {
		vids = append(vids, pw.VID)
		workIDs = append(workIDs, pw.WorkID)
	}
	votes, err := loadVotes(ctx, db, vids)
	if err != nil {
		return err
	}
	existing, err := loadExisting(ctx, db, w.sourceID, workIDs)
	if err != nil {
		return err
	}
	byVID := map[string][]Vote{}
	for _, v := range votes {
		byVID[v.VID] = append(byVID[v.VID], v)
	}

	plans := make([]workPlan, 0, len(batch))
	for _, pw := range batch {
		desired := desiredTags(byVID[pw.VID], tagMeta, tagMap)
		w.stats.TagsDesired += len(desired)
		plan := diffTags(existing[pw.WorkID], desired, sexualByName)
		w.stats.TagsSame += plan.Same
		for _, ins := range plan.Inserts {
			if prev, ok := sexualByName[ins.Name]; ok {
				sexualByName[ins.Name] = prev || ins.Sexual
			} else {
				sexualByName[ins.Name] = ins.Sexual
			}
		}
		plans = append(plans, workPlan{workID: pw.WorkID, plan: plan})
		if !w.apply && plan.hasWrites() {
			w.stats.WorksChanged++
			w.stats.TagsInserted += len(plan.Inserts)
			w.stats.TagsUpdated += len(plan.Updates)
			w.stats.TagsDeleted += len(plan.Deletes)
		}
	}
	if !w.apply {
		return nil
	}
	var out planWrites
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		out, err = writePlans(tx, w.sourceID, plans)
		if err != nil {
			return err
		}
		return repository.TouchWorks(ctx, tx, out.touched)
	})
	if err != nil {
		return err
	}
	w.stats.TagsInserted += out.inserted
	w.stats.TagsUpdated += out.updated
	w.stats.TagsDeleted += out.deleted
	w.stats.TagsLost += out.lost
	w.stats.WorksChanged += len(out.touched)
	return w.appendReceipts(out.recs)
}

type workPlan struct {
	workID int64
	plan   Plan
}

type planWrites struct {
	inserted, updated, deleted, lost int
	touched                          []int64
	recs                             []receipt
}

func writePlans(tx *gorm.DB, sourceID int16, plans []workPlan) (planWrites, error) {
	var out planWrites
	for _, wp := range plans {
		wrote := false
		for _, ins := range wp.plan.Inserts {
			n, rec, err := applyInsert(tx, wp.workID, sourceID, ins)
			if err != nil {
				return planWrites{}, err
			}
			if n == 0 {
				out.lost++
				continue
			}
			out.inserted++
			wrote = true
			out.recs = append(out.recs, rec)
		}
		for _, u := range wp.plan.Updates {
			n, rec, err := applyUpdate(tx, wp.workID, sourceID, u)
			if err != nil {
				return planWrites{}, err
			}
			if n == 0 {
				out.lost++
				continue
			}
			out.updated++
			wrote = true
			out.recs = append(out.recs, rec)
		}
		for _, d := range wp.plan.Deletes {
			n, rec, err := applyDelete(tx, wp.workID, sourceID, d, "delete")
			if err != nil {
				return planWrites{}, err
			}
			if n == 0 {
				out.lost++
				continue
			}
			out.deleted++
			wrote = true
			out.recs = append(out.recs, rec)
		}
		if wrote {
			out.touched = append(out.touched, wp.workID)
		}
	}
	return out, nil
}

func processOrphanBatch(ctx context.Context, db *gorm.DB, w *writer, batch []orphanWork) error {
	planned := 0
	for _, ow := range batch {
		planned += len(ow.Rows)
	}
	if !w.apply {
		w.stats.OrphanRowsRemoved += planned
		return nil
	}
	var touched []int64
	var recs []receipt
	removed, lost := 0, 0
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		touched = nil
		recs = nil
		removed, lost = 0, 0
		for _, ow := range batch {
			wrote := false
			for _, row := range ow.Rows {
				n, rec, err := applyDelete(tx, ow.WorkID, w.sourceID, Delete{
					ID: row.ID, Name: row.Name, Spoiler: row.Spoiler, Count: row.Count,
				}, "orphan_delete")
				if err != nil {
					return err
				}
				if n == 0 {
					lost++
					continue
				}
				removed++
				wrote = true
				recs = append(recs, rec)
			}
			if wrote {
				touched = append(touched, ow.WorkID)
			}
		}
		return repository.TouchWorks(ctx, tx, touched)
	})
	if err != nil {
		return err
	}
	w.stats.OrphanRowsRemoved += removed
	w.stats.TagsLost += lost
	return w.appendReceipts(recs)
}

func classifyWorks(ctx context.Context, db *gorm.DB, vndbID int16) ([]popWork, int, int, error) {
	var rows []struct {
		WorkID int64  `gorm:"column:work_id"`
		VID    string `gorm:"column:vid"`
		VNOK   bool   `gorm:"column:vn_ok"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT w.id AS work_id, r.external_id AS vid,
		       EXISTS (SELECT 1 FROM src_vndb.vn v WHERE v.id = r.external_id) AS vn_ok
		FROM catalog_work w
		JOIN catalog_external_ref r
		  ON r.entity_type = ? AND r.entity_id = w.id
		 AND r.source_id = ? AND r.link_kind = ? AND r.dead_at IS NULL
		WHERE w.deleted_at IS NULL AND w.status <> ?
		ORDER BY w.id, r.external_id`,
		model.EntityTypeWork, vndbID, model.LinkKindExact, model.WorkStatusMerged,
	).Scan(&rows).Error; err != nil {
		return nil, 0, 0, fmt.Errorf("load vndb work anchors: %w", err)
	}
	var pop []popWork
	multi, missing := 0, 0
	i := 0
	for i < len(rows) {
		id := rows[i].WorkID
		first := i
		for i < len(rows) && rows[i].WorkID == id {
			i++
		}
		if i-first > 1 {
			multi++
			continue
		}
		if !rows[first].VNOK {
			missing++
			continue
		}
		pop = append(pop, popWork{WorkID: id, VID: rows[first].VID})
	}
	return pop, multi, missing, nil
}
