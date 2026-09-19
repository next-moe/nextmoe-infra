package releasemeta

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"api/internal/platform/catalog/repository"
	"api/internal/platform/provenance"

	"gorm.io/gorm"
)

type writer struct {
	db           *gorm.DB
	stats        *Stats
	touched      []int64
	apply        bool
	receiptsPath string
	recFile      *os.File
}

func (w *writer) touch(ctx context.Context) error {
	return repository.TouchWorks(ctx, w.db, w.touched)
}

func (w *writer) closeReceipts() error {
	if w.recFile == nil {
		return nil
	}
	err := w.recFile.Close()
	w.recFile = nil
	if err != nil {
		return fmt.Errorf("receipts: %w", err)
	}
	return nil
}

type dateWrite struct {
	releaseID        int64
	workID           int64
	lane, ext, kind  string
	oldY, oldM, oldD *int16
	oldVal, newVal   dateVal
}

func (w *writer) writeDate(ctx context.Context, p dateWrite) error {
	if !w.apply {
		return nil
	}
	var newY, newM, newD *int16
	if !p.newVal.empty {
		y := p.newVal.y
		newY = &y
		newM, newD = p.newVal.m, p.newVal.d
	}
	var hosts []int64
	res := w.db.WithContext(ctx).Raw(
		`UPDATE catalog_release SET released_y = ?, released_m = ?, released_d = ?
		 WHERE id = ? AND deleted_at IS NULL
		   AND released_y IS NOT DISTINCT FROM ? AND released_m IS NOT DISTINCT FROM ? AND released_d IS NOT DISTINCT FROM ?
		   AND COALESCE(field_provenance -> 'released_y' -> 0 ->> 'source', '') NOT IN ?
		 RETURNING work_id`,
		newY, newM, newD, p.releaseID, p.oldY, p.oldM, p.oldD, provenance.HumanSources()).Scan(&hosts)
	if res.Error != nil {
		w.stats.Errors++
		slog.Warn("write release date", "release", p.releaseID, "err", res.Error)
		return nil
	}
	if len(hosts) == 0 {
		w.stats.DatesLost++
		return nil
	}
	w.touched = append(w.touched, hosts...)
	w.stats.DatesWritten++
	workID := p.workID
	if len(hosts) > 0 {
		workID = hosts[0]
	}
	return w.appendReceipt(dateReceipt{
		ReleaseID: p.releaseID,
		WorkID:    workID,
		Lane:      p.lane,
		Ext:       p.ext,
		Kind:      p.kind,
		Old:       p.oldVal.receipt(),
		New:       p.newVal.receipt(),
	})
}

type receiptYMD struct {
	Y *int16 `json:"y"`
	M *int16 `json:"m"`
	D *int16 `json:"d"`
}

type dateReceipt struct {
	ReleaseID int64      `json:"release_id"`
	WorkID    int64      `json:"work_id"`
	Lane      string     `json:"lane"`
	Ext       string     `json:"ext"`
	Kind      string     `json:"kind"`
	Old       receiptYMD `json:"old"`
	New       receiptYMD `json:"new"`
}

func (w *writer) appendReceipt(rec dateReceipt) error {
	if w.receiptsPath == "" || !w.apply {
		return nil
	}
	if w.recFile == nil {
		f, err := os.OpenFile(w.receiptsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("receipts: %w", err)
		}
		w.recFile = f
	}
	if err := json.NewEncoder(w.recFile).Encode(rec); err != nil {
		return fmt.Errorf("receipts: %w", err)
	}
	return nil
}

func (w *writer) fillRating(ctx context.Context, workID int64, rating int16, apply bool) {
	if !apply {
		return
	}
	// content_rating = 0 cannot tell "nobody set it" from "a human ruled
	// all_ages", so the zero test on its own let an upstream r18 verdict
	// overwrite the human decision.
	res := w.db.WithContext(ctx).Exec(
		`UPDATE catalog_work SET content_rating = ?, updated_at = now()
		 WHERE id = ? AND deleted_at IS NULL AND content_rating = 0
		   AND COALESCE(field_provenance -> 'content_rating' -> 0 ->> 'source', '') NOT IN ?`,
		rating, workID, provenance.HumanSources())
	if res.Error != nil {
		w.stats.Errors++
		slog.Warn("fill content_rating", "work", workID, "err", res.Error)
		return
	}
	if res.RowsAffected == 0 {
		w.stats.RatingSkippedNonEmpty++
		return
	}
	w.stats.RatingFilled++
}
