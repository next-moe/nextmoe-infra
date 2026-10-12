package bgmworkmeta

import (
	"context"
	"log/slog"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type writer struct {
	db         *gorm.DB
	stats      *Stats
	suppressed editspec.SuppressedWorkTags
	touched    []int64
}

func (w *writer) touch(ctx context.Context) error {
	return repository.TouchWorks(ctx, w.db, w.touched)
}

func (w *writer) noteError(err error) {
	w.stats.Errors++
	if w.stats.FirstError == "" {
		w.stats.FirstError = err.Error()
	}
}

type tagRow struct {
	WorkID   int64
	SourceID int16
	Name     string
}

func (w *writer) writeTag(ctx context.Context, p tagRow, apply bool) {
	if w.suppressed.Has(p.WorkID, p.Name) {
		w.stats.MetaSuppressed++
		return
	}
	if !apply {
		return
	}
	res := w.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "work_id"}, {Name: "name"}, {Name: "source_id"}},
		DoNothing: true,
	}).Create(&model.CatalogWorkTag{
		WorkID: p.WorkID, Name: p.Name, Count: 0, SourceID: p.SourceID,
	})
	if res.Error != nil {
		w.noteError(res.Error)
		slog.Warn("write meta tag", "work", p.WorkID, "name", p.Name, "err", res.Error)
		return
	}
	if res.RowsAffected == 0 {
		w.stats.MetaConflict++
		return
	}
	w.stats.MetaWritten++
	w.touched = append(w.touched, p.WorkID)
}

type favRow struct {
	WorkID   int64
	SourceID int16
	Metric   int16
	Value    int64
}

func (w *writer) writeFavorite(ctx context.Context, p favRow, apply bool) {
	if !apply {
		return
	}
	res := w.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "work_id"}, {Name: "source_id"}, {Name: "metric"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
		Where: clause.Where{Exprs: []clause.Expression{gorm.Expr(
			`catalog_work_popularity.value IS DISTINCT FROM excluded.value`)}},
	}).Create(&model.CatalogWorkPopularity{
		WorkID: p.WorkID, SourceID: p.SourceID, Metric: p.Metric, Value: p.Value,
	})
	if res.Error != nil {
		w.noteError(res.Error)
		slog.Warn("write favorite shelf", "work", p.WorkID, "metric", p.Metric, "err", res.Error)
		return
	}
	if res.RowsAffected == 0 {
		w.stats.FavUnchanged++
		return
	}
	w.stats.FavWritten++
	w.touched = append(w.touched, p.WorkID)
}
