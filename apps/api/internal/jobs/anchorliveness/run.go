package anchorliveness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

type Stats struct {
	Source       string
	Apply        bool
	Per          map[string]LaneStats
	MarkedTotal  int64
	ClearedTotal int64
	LanesRefused int64
	Errors       int64
}

type LaneStats struct {
	Refs  int64
	Mark  int64
	Clear int64
}

func (st *Stats) LogArgs() []any {
	args := []any{"source", st.Source, "apply", st.Apply}
	for _, e := range EntityKeys {
		ls := st.Per[e]
		args = append(args, e+"_refs", ls.Refs, e+"_to_mark", ls.Mark, e+"_to_clear", ls.Clear)
	}
	return append(args,
		"marked_total", st.MarkedTotal,
		"cleared_total", st.ClearedTotal,
		"lanes_refused", st.LanesRefused,
		"errors", st.Errors)
}

type receipt struct {
	Action     string `json:"action"`
	Source     string `json:"source"`
	Entity     string `json:"entity"`
	EntityID   int64  `json:"entity_id"`
	ExternalID string `json:"external_id"`
	LinkKind   int16  `json:"link_kind"`
}

type refHit struct {
	EntityID   int64  `gorm:"column:entity_id"`
	ExternalID string `gorm:"column:external_id"`
	LinkKind   int16  `gorm:"column:link_kind"`
}

func Run(ctx context.Context, o Opts) (*Stats, error) {
	if err := ValidateOpts(o); err != nil {
		return nil, err
	}
	db, err := database.OpenJob(o.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	defer closeGorm(db)
	var eg *gorm.DB
	if o.Source == SourceEG {
		eg, err = database.OpenJob(o.EGDSN)
		if err != nil {
			return nil, fmt.Errorf("connect eg mirror: %w", err)
		}
		defer closeGorm(eg)
	}
	return RunWithDB(ctx, db, eg, o)
}

func RunWithDB(ctx context.Context, db, eg *gorm.DB, o Opts) (*Stats, error) {
	st := &Stats{Source: o.Source, Apply: o.Apply, Per: map[string]LaneStats{}}
	if err := ValidateOpts(o); err != nil {
		return st, err
	}
	if o.Source == SourceEG && eg == nil {
		return st, fmt.Errorf("erogamescape run needs an eg mirror connection")
	}
	lanes := o.Lanes
	if len(lanes) == 0 {
		lanes = LanesFor(o.Source)
	}
	var srcID int16
	if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_source WHERE key = ?`, o.Source).Scan(&srcID).Error; err != nil {
		return st, fmt.Errorf("look up %s source id: %w", o.Source, err)
	}
	if srcID == 0 {
		return st, fmt.Errorf("catalog_source has no %q row", o.Source)
	}
	r := &runner{ctx: ctx, db: db, eg: eg, opts: o, srcID: srcID, st: st}
	var errs []error
	for _, ln := range lanes {
		if err := r.runLane(ln); err != nil {
			errs = append(errs, err)
		}
	}
	if o.Apply {
		if err := r.closeReceipts(); err != nil {
			errs = append(errs, err)
		}
	}
	slog.Info("audit-anchor-liveness summary", st.LogArgs()...)
	return st, errors.Join(errs...)
}

type runner struct {
	ctx   context.Context
	db    *gorm.DB
	eg    *gorm.DB
	opts  Opts
	srcID int16
	st    *Stats
	rec   *os.File
}

func (r *runner) runLane(ln Lane) error {
	floorN, err := r.countMembership(ln)
	if err != nil {
		r.st.Errors++
		return fmt.Errorf("%s/%s floor: %w", ln.Source, ln.Entity, err)
	}
	var refused error
	if floorN < ln.Floor {
		refused = fmt.Errorf(
			"%s/%s: refusing: %s holds %d rows, below the floor of %d — the mirror looks partially loaded, and applying against it would mark live anchors dead wholesale",
			ln.Source, ln.Entity, ln.Table, floorN, ln.Floor)
	}
	if ln.Freshness && refused == nil {
		total, fresh, ferr := r.countFresh(ln)
		if ferr != nil {
			r.st.Errors++
			return fmt.Errorf("%s/%s freshness: %w", ln.Source, ln.Entity, ferr)
		}
		if !FreshEnough(fresh, total) {
			pct := 0.0
			if total > 0 {
				pct = 100 * float64(fresh) / float64(total)
			}
			refused = fmt.Errorf(
				"%s/%s: refusing: %s is %.1f%% fresh (want >= 97%% under the 48-hour rule) — a crawl that died half way looks like mass deletion",
				ln.Source, ln.Entity, ln.Table, pct)
		}
	}

	write := r.opts.Apply && refused == nil
	var ls LaneStats
	var hits []receipt
	txErr := r.withLaneTx(write, func(tx *gorm.DB) error {
		if ln.Freshness {
			ids, err := r.loadAlive(ln)
			if err != nil {
				return err
			}
			if err := fillAliveTemp(tx, ids); err != nil {
				return err
			}
		}
		exists := LiveExistsSQL(ln)
		if err := tx.Raw(CountRefsSQL(), r.srcID, ln.Type).Scan(&ls.Refs).Error; err != nil {
			return fmt.Errorf("count refs: %w", err)
		}
		if err := tx.Raw(CountMarkSQL(exists), r.srcID, ln.Type).Scan(&ls.Mark).Error; err != nil {
			return fmt.Errorf("count to mark: %w", err)
		}
		if err := tx.Raw(CountClearSQL(exists), r.srcID, ln.Type).Scan(&ls.Clear).Error; err != nil {
			return fmt.Errorf("count to clear: %w", err)
		}
		if !write {
			return nil
		}
		var err error
		hits, err = r.applyLane(tx, ln, exists)
		return err
	})
	if txErr != nil {
		r.st.Errors++
		r.st.Per[ln.Entity] = LaneStats{Refs: ls.Refs}
		return fmt.Errorf("%s/%s: %w", ln.Source, ln.Entity, txErr)
	}
	if r.opts.Apply {
		if refused != nil {
			ls.Mark, ls.Clear = 0, 0
		} else {
			var marked, cleared int64
			for _, h := range hits {
				if h.Action == "mark" {
					marked++
				} else {
					cleared++
				}
			}
			ls.Mark, ls.Clear = marked, cleared
			r.st.MarkedTotal += marked
			r.st.ClearedTotal += cleared
			if err := r.appendReceipts(hits); err != nil {
				r.st.Errors++
				r.st.Per[ln.Entity] = ls
				return err
			}
		}
	} else {
		r.st.MarkedTotal += ls.Mark
		r.st.ClearedTotal += ls.Clear
	}
	r.st.Per[ln.Entity] = ls
	if refused != nil {
		r.st.LanesRefused++
		return refused
	}
	return nil
}

func (r *runner) applyLane(tx *gorm.DB, ln Lane, exists string) ([]receipt, error) {
	var marked []refHit
	if err := tx.Raw(MarkSQL(exists), r.srcID, ln.Type).Scan(&marked).Error; err != nil {
		return nil, fmt.Errorf("mark dead: %w", err)
	}
	var cleared []refHit
	if err := tx.Raw(ClearSQL(exists), r.srcID, ln.Type).Scan(&cleared).Error; err != nil {
		return nil, fmt.Errorf("clear revived: %w", err)
	}
	ids := make([]int64, 0, len(marked)+len(cleared))
	hits := make([]receipt, 0, len(marked)+len(cleared))
	for _, h := range marked {
		ids = append(ids, h.EntityID)
		hits = append(hits, receipt{
			Action: "mark", Source: ln.Source, Entity: ln.Entity,
			EntityID: h.EntityID, ExternalID: h.ExternalID, LinkKind: h.LinkKind,
		})
	}
	for _, h := range cleared {
		ids = append(ids, h.EntityID)
		hits = append(hits, receipt{
			Action: "clear", Source: ln.Source, Entity: ln.Entity,
			EntityID: h.EntityID, ExternalID: h.ExternalID, LinkKind: h.LinkKind,
		})
	}
	if ln.touchesWork() {
		var err error
		if ln.Type == model.EntityTypeRelease {
			err = repository.TouchReleaseWorks(r.ctx, tx, ids)
		} else {
			err = repository.TouchWorks(r.ctx, tx, ids)
		}
		if err != nil {
			return nil, fmt.Errorf("touch works: %w", err)
		}
	}
	return hits, nil
}

func (r *runner) withLaneTx(commit bool, fn func(*gorm.DB) error) error {
	tx := r.db.WithContext(r.ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	if commit {
		return tx.Commit().Error
	}
	return tx.Rollback().Error
}

func (r *runner) countMembership(ln Lane) (int64, error) {
	db := r.db
	if ln.Freshness {
		db = r.eg
	}
	var n int64
	if err := db.WithContext(r.ctx).Raw(FloorSQL(ln.Table)).Scan(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

func (r *runner) countFresh(ln Lane) (total, fresh int64, err error) {
	var row struct {
		Total int64 `gorm:"column:total"`
		Fresh int64 `gorm:"column:fresh"`
	}
	if err = r.eg.WithContext(r.ctx).Raw(FreshnessSQL(ln.Table)).Scan(&row).Error; err != nil {
		return 0, 0, err
	}
	return row.Total, row.Fresh, nil
}

func (r *runner) loadAlive(ln Lane) ([]string, error) {
	var rows []struct {
		ID string `gorm:"column:id"`
	}
	if err := r.eg.WithContext(r.ctx).Raw(AliveSQL(ln.Table, ln.IDColumn, ln.NumericID)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	return ids, nil
}

func fillAliveTemp(tx *gorm.DB, ids []string) error {
	if err := tx.Exec(CreateAliveTempSQL()).Error; err != nil {
		return err
	}
	const chunk = 500
	for start := 0; start < len(ids); start += chunk {
		end := min(start+chunk, len(ids))
		var b strings.Builder
		b.WriteString(`INSERT INTO ` + AliveTempTable + ` (id) VALUES `)
		args := make([]any, 0, end-start)
		for i, id := range ids[start:end] {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString("(?)")
			args = append(args, id)
		}
		if err := tx.Exec(b.String(), args...).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) appendReceipts(hits []receipt) error {
	if r.opts.Receipts == "" || !r.opts.Apply || len(hits) == 0 {
		return nil
	}
	if r.rec == nil {
		f, err := os.OpenFile(r.opts.Receipts, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("receipts: %w", err)
		}
		r.rec = f
	}
	enc := json.NewEncoder(r.rec)
	for _, h := range hits {
		if err := enc.Encode(h); err != nil {
			return fmt.Errorf("receipts: %w", err)
		}
	}
	return nil
}

func (r *runner) closeReceipts() error {
	if r.rec == nil {
		return nil
	}
	err := r.rec.Close()
	r.rec = nil
	if err != nil {
		return fmt.Errorf("receipts: %w", err)
	}
	return nil
}

func closeGorm(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.Close()
	}
}
