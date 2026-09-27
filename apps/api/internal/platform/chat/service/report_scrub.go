package service

import (
	"api/internal/platform/chat/model"

	"gorm.io/gorm"
)

// scrubReportSnapshots erases a deleted account's words from the context of
// reports it neither filed nor was the subject of.
func scrubReportSnapshots(tx *gorm.DB, uid int64) error {
	var reports []model.ChatReport
	if err := tx.Where(`snapshot->'messages' @> ?::jsonb`, jsonOf([]map[string]any{{"sender_id": uid}})).Find(&reports).Error; err != nil {
		return err
	}
	for _, r := range reports {
		snap := decodeJSON[struct {
			Messages []snapshotMessage `json:"messages"`
		}](r.Snapshot)
		if snap == nil {
			continue
		}
		for i := range snap.Messages {
			if snap.Messages[i].SenderID == uid {
				snap.Messages[i].Text, snap.Messages[i].Entities, snap.Messages[i].MediaHash = "", nil, ""
			}
		}
		if err := tx.Model(&model.ChatReport{}).Where("id = ?", r.ID).Update("snapshot", jsonOf(snap)).Error; err != nil {
			return err
		}
	}
	return nil
}
