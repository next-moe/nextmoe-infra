package worktags

import (
	"context"
	"log/slog"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type writer struct {
	db      *gorm.DB
	source  int16
	stats   *Stats
	touched []int64
}

type plannedRow struct {
	WorkID   int64
	SourceID int16
	Name     string
	Count    int
}

func (w *writer) write(ctx context.Context, p plannedRow, apply bool) {
	if !apply {
		return
	}
	res := w.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "work_id"}, {Name: "name"}, {Name: "source_id"}},
		DoNothing: true,
	}).Create(&model.CatalogWorkTag{
		WorkID: p.WorkID, Name: p.Name, Count: p.Count, SourceID: p.SourceID,
	})
	if res.Error != nil {
		w.noteError(res.Error)
		slog.Warn("write tag", "work", p.WorkID, "name", p.Name, "source", p.SourceID, "err", res.Error)
		return
	}
	if res.RowsAffected == 0 {
		w.stats.Conflict++
		return
	}
	w.stats.Written++
	w.touched = append(w.touched, p.WorkID)
}

func (w *writer) noteError(err error) {
	w.stats.Errors++
	if w.stats.FirstError == "" {
		w.stats.FirstError = err.Error()
	}
}

func (w *writer) finish(ctx context.Context, apply bool) error {
	if apply {
		n, err := repository.InheritTagSexual(ctx, w.db, w.source)
		if err != nil {
			return err
		}
		w.stats.SexualInherited = int(n)
	}
	return repository.TouchWorks(ctx, w.db, w.touched)
}
