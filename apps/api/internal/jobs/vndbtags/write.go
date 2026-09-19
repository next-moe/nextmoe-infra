package vndbtags

import (
	"encoding/json"
	"fmt"
	"os"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type writer struct {
	db           *gorm.DB
	stats        *Stats
	apply        bool
	receiptsPath string
	recFile      *os.File
	sourceID     int16
}

type receiptOld struct {
	Spoiler int16 `json:"spoiler"`
	Count   int   `json:"count"`
}

type receiptNew struct {
	Spoiler int16 `json:"spoiler"`
	Count   int   `json:"count"`
	Sexual  bool  `json:"sexual"`
}

type receipt struct {
	Op     string      `json:"op"`
	WorkID int64       `json:"work_id"`
	Name   string      `json:"name"`
	Old    *receiptOld `json:"old"`
	New    *receiptNew `json:"new"`
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

func (w *writer) appendReceipts(recs []receipt) error {
	if w.receiptsPath == "" || !w.apply || len(recs) == 0 {
		return nil
	}
	if w.recFile == nil {
		f, err := os.OpenFile(w.receiptsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("receipts: %w", err)
		}
		w.recFile = f
	}
	enc := json.NewEncoder(w.recFile)
	for _, rec := range recs {
		if err := enc.Encode(rec); err != nil {
			return fmt.Errorf("receipts: %w", err)
		}
	}
	return nil
}

func applyInsert(tx *gorm.DB, workID int64, sourceID int16, ins Insert) (int64, receipt, error) {
	res := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "work_id"}, {Name: "name"}, {Name: "source_id"}},
		DoNothing: true,
	}).Create(&model.CatalogWorkTag{
		WorkID: workID, Name: ins.Name, Count: ins.Count, SourceID: sourceID,
		Spoiler: ins.Spoiler, Sexual: ins.Sexual,
	})
	if res.Error != nil {
		return 0, receipt{}, res.Error
	}
	if res.RowsAffected == 0 {
		return 0, receipt{}, nil
	}
	return res.RowsAffected, receipt{
		Op: "insert", WorkID: workID, Name: ins.Name,
		New: &receiptNew{Spoiler: ins.Spoiler, Count: ins.Count, Sexual: ins.Sexual},
	}, nil
}

func applyUpdate(tx *gorm.DB, workID int64, sourceID int16, u Update) (int64, receipt, error) {
	res := tx.Exec(
		`UPDATE catalog_work_tag SET spoiler = ?, count = ?, updated_at = now()
		 WHERE id = ? AND source_id = ? AND spoiler = ? AND count = ?`,
		u.NewSpoiler, u.NewCount, u.ID, sourceID, u.OldSpoiler, u.OldCount)
	if res.Error != nil {
		return 0, receipt{}, res.Error
	}
	if res.RowsAffected == 0 {
		return 0, receipt{}, nil
	}
	return res.RowsAffected, receipt{
		Op: "update", WorkID: workID, Name: u.Name,
		Old: &receiptOld{Spoiler: u.OldSpoiler, Count: u.OldCount},
		New: &receiptNew{Spoiler: u.NewSpoiler, Count: u.NewCount, Sexual: u.Sexual},
	}, nil
}

func applyDelete(tx *gorm.DB, workID int64, sourceID int16, d Delete, op string) (int64, receipt, error) {
	res := tx.Exec(
		`DELETE FROM catalog_work_tag WHERE id = ? AND source_id = ? AND spoiler = ? AND count = ?`,
		d.ID, sourceID, d.Spoiler, d.Count)
	if res.Error != nil {
		return 0, receipt{}, res.Error
	}
	if res.RowsAffected == 0 {
		return 0, receipt{}, nil
	}
	return res.RowsAffected, receipt{
		Op: op, WorkID: workID, Name: d.Name,
		Old: &receiptOld{Spoiler: d.Spoiler, Count: d.Count},
	}, nil
}
