package vndbtitles

import (
	"context"
	"fmt"
	"log/slog"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

type preparedWork struct {
	row  loadedWork
	plan decision
}

type writeDelta struct {
	olangInserted int
	enInserted    int
	titlesLost    int
	displayFilled int
	displayLost   int
	changed       int
	touched       []int64
}

func applyPrepared(ctx context.Context, db *gorm.DB, prepared []preparedWork, st *Stats) error {
	var touched []int64
	for start := 0; start < len(prepared); start += writeChunk {
		end := min(start+writeChunk, len(prepared))
		chunk := prepared[start:end]
		var d writeDelta
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var err error
			d, err = writeChunkTx(tx, chunk)
			return err
		})
		if err != nil {
			st.noteError(err)
			slog.Warn("backfill-vndb-work-titles write chunk", "start", start, "err", err)
			continue
		}
		st.TitlesOLangInserted += d.olangInserted
		st.TitlesENInserted += d.enInserted
		st.TitlesLost += d.titlesLost
		st.DisplayFilled += d.displayFilled
		st.DisplayLost += d.displayLost
		st.WorksChanged += d.changed
		touched = append(touched, d.touched...)
	}
	if err := repository.TouchWorks(ctx, db, touched); err != nil {
		return fmt.Errorf("touch works: %w", err)
	}
	return nil
}

func writeChunkTx(tx *gorm.DB, chunk []preparedWork) (writeDelta, error) {
	var d writeDelta
	for _, p := range chunk {
		wrote := false
		if p.plan.InsertOLang {
			n, err := insertTitle(tx, p.row.WorkID, p.row.OLang, p.row.OLangTitle)
			if err != nil {
				return writeDelta{}, err
			}
			if n > 0 {
				d.olangInserted++
				wrote = true
			} else {
				d.titlesLost++
			}
		}
		if p.plan.InsertEN {
			n, err := insertTitle(tx, p.row.WorkID, vndbLangEnglish, p.row.ENTitle)
			if err != nil {
				return writeDelta{}, err
			}
			if n > 0 {
				d.enInserted++
				wrote = true
			} else {
				d.titlesLost++
			}
		}
		if p.plan.FillDisplay {
			n, err := fillDisplay(tx, p.row.WorkID, p.row.OLangTitle)
			if err != nil {
				return writeDelta{}, err
			}
			if n > 0 {
				d.displayFilled++
				wrote = true
			} else {
				d.displayLost++
			}
		}
		if wrote {
			d.changed++
			d.touched = append(d.touched, p.row.WorkID)
		}
	}
	return d, nil
}

func insertTitle(tx *gorm.DB, workID int64, lang, title string) (int64, error) {
	res := tx.Exec(`
		INSERT INTO catalog_work_title (work_id, lang, title, kind, provenance)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (work_id, lang, title, kind) DO NOTHING`,
		workID, lang, title, model.WorkTitleKindOfficial, model.WorkTitleProvenanceSource)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

func fillDisplay(tx *gorm.DB, workID int64, title string) (int64, error) {
	humanSQL := editspec.HumanFieldProvenanceSQL("field_provenance", "display_name")
	res := tx.Exec(
		`UPDATE catalog_work SET display_name = ? WHERE id = ? AND btrim(display_name) = '' AND NOT COALESCE(`+humanSQL+`, false)`,
		title, workID)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}
