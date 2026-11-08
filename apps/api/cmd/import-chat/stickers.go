package main

import (
	"context"
	"log/slog"

	"gorm.io/gorm"
)

// The 2026-09-27 import stored the old sites' stickers as photos: chat had no
// sticker type yet. Only a sticker that stood alone (no text, no album, no
// card) becomes one; 42 of the 164 shared a message with text or an album,
// which a sticker message cannot carry, so they stay photos.
func reclassifyStickers(ctx context.Context, chat *gorm.DB, official map[string]bool, apply bool) (int, error) {
	type row struct {
		ID   int64  `gorm:"column:id"`
		Hash string `gorm:"column:hash"`
	}
	var rows []row
	if err := chat.WithContext(ctx).Raw(`
		SELECT m.id, m.media->>'image_hash' AS hash
		  FROM chat_message m JOIN chat_import_message i ON i.message_id = m.id
		 WHERE m.media->>'type' = 'photo' AND m.text = '' AND m.media_group_id IS NULL
		   AND m.context IS NULL AND m.deleted_at IS NULL`).Scan(&rows).Error; err != nil {
		return 0, err
	}
	var ids []int64
	for _, r := range rows {
		if official[r.Hash] {
			ids = append(ids, r.ID)
		}
	}
	slog.Info("import-chat stickers plan", "official_hashes", len(official), "imported_lone_photos", len(rows), "to_reclassify", len(ids))
	if !apply || len(ids) == 0 {
		return len(ids), nil
	}
	return len(ids), chat.WithContext(ctx).Exec(`
		UPDATE chat_message SET media = jsonb_set(media, '{type}', '"sticker"')
		 WHERE id IN ? AND media->>'type' = 'photo'`, ids).Error
}
