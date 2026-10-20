package main

import (
	"context"
	"fmt"
	"time"

	v2handler "api/internal/platform/apiv2/handler"
	"api/internal/platform/apiv2/repr"
	"api/pkg/imageclient"

	"gorm.io/gorm"
)

func erasedUserName(id int64) string { return fmt.Sprintf("已注销#%d", id) }

func usersOf(db *gorm.DB, cdnBase string) v2handler.UsersOf {
	return func(ctx context.Context, ids []int64) (map[int64]repr.UserRef, error) {
		var rows []struct {
			ID              int64      `gorm:"column:id"`
			Name            string     `gorm:"column:name"`
			AvatarImageHash *string    `gorm:"column:avatar_image_hash"`
			AnonymizedAt    *time.Time `gorm:"column:anonymized_at"`
			DeletedAt       *time.Time `gorm:"column:deleted_at"`
		}
		if err := db.WithContext(ctx).Raw(
			`SELECT id, name, avatar_image_hash, anonymized_at, deleted_at FROM users WHERE id IN ?`, ids,
		).Scan(&rows).Error; err != nil {
			return nil, err
		}
		out := make(map[int64]repr.UserRef, len(ids))
		for _, id := range ids {
			out[id] = repr.UserRef{Object: "user", ID: repr.ID(id), Name: erasedUserName(id)}
		}
		for _, r := range rows {
			if r.AnonymizedAt != nil || r.DeletedAt != nil {
				continue
			}
			u := repr.UserRef{Object: "user", ID: repr.ID(r.ID), Name: r.Name}
			if r.AvatarImageHash != nil {
				if url := imageclient.MainURL(cdnBase, *r.AvatarImageHash, "webp"); url != "" {
					u.Avatar = &url
				}
			}
			out[r.ID] = u
		}
		return out, nil
	}
}
