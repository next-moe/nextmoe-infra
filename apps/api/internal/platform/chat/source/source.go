// Package source adapts the databases and services chat reads but does not
// own: account names from the main database, the follow graph, blocks and
// trust from kun_community, and image metadata from the image service.
package source

import (
	"context"
	"time"

	"api/internal/platform/chat/service"
	"api/pkg/imageclient"

	"gorm.io/gorm"
)

type Users struct{ db *gorm.DB }

func NewUsers(mainDB *gorm.DB) *Users { return &Users{db: mainDB} }

func (u *Users) Profiles(ctx context.Context, ids []int64) (map[int64]service.Profile, error) {
	out := map[int64]service.Profile{}
	if len(ids) == 0 {
		return out, nil
	}
	type row struct {
		ID           int64      `gorm:"column:id"`
		Name         string     `gorm:"column:name"`
		Avatar       string     `gorm:"column:avatar"`
		AnonymizedAt *time.Time `gorm:"column:anonymized_at"`
		DeletedAt    *time.Time `gorm:"column:deleted_at"`
		CreatedAt    time.Time  `gorm:"column:created_at"`
	}
	var rows []row
	if err := u.db.WithContext(ctx).Raw(`
		SELECT id, name, avatar, anonymized_at, deleted_at, created_at FROM users WHERE id IN ?`, ids).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = service.Profile{
			ID: r.ID, Name: r.Name, Avatar: r.Avatar, CreatedAt: r.CreatedAt,
			Deleted: r.AnonymizedAt != nil || r.DeletedAt != nil,
		}
	}
	return out, nil
}

type Relationships struct{ db *gorm.DB }

func NewRelationships(communityDB *gorm.DB) *Relationships { return &Relationships{db: communityDB} }

func (r *Relationships) BlockedEitherWay(ctx context.Context, a, b int64) (bool, error) {
	var blocked bool
	err := r.db.WithContext(ctx).Raw(`
		SELECT EXISTS (SELECT 1 FROM community_user_block
		                WHERE (blocker_id = ? AND blocked_id = ?) OR (blocker_id = ? AND blocked_id = ?))`,
		a, b, b, a).Scan(&blocked).Error
	return blocked, err
}

func (r *Relationships) Follows(ctx context.Context, follower, followee int64) (bool, error) {
	var follows bool
	err := r.db.WithContext(ctx).Raw(`
		SELECT EXISTS (SELECT 1 FROM community_user_follow WHERE follower_id = ? AND followee_id = ?)`,
		follower, followee).Scan(&follows).Error
	return follows, err
}

func (r *Relationships) TrustLevel(ctx context.Context, uid int64) (int16, error) {
	var levels []int16
	if err := r.db.WithContext(ctx).Raw(`SELECT level FROM community_trust WHERE user_id = ?`, uid).
		Scan(&levels).Error; err != nil {
		return 0, err
	}
	if len(levels) == 0 {
		return 0, nil
	}
	return levels[0], nil
}

type Images struct{ cli *imageclient.Client }

func NewImages(cli *imageclient.Client) *Images {
	if cli == nil {
		return nil
	}
	return &Images{cli: cli}
}

func (i *Images) Meta(ctx context.Context, hashes []string) (map[string]service.ImageMeta, error) {
	metas, err := i.cli.MetaBatch(ctx, hashes)
	if err != nil {
		return nil, err
	}
	out := make(map[string]service.ImageMeta, len(metas))
	for h, m := range metas {
		out[h] = service.ImageMeta{Width: m.Width, Height: m.Height, Thumbhash: m.Thumbhash}
	}
	return out, nil
}
