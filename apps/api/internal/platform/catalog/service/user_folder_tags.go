package service

import (
	"context"
	stderrors "errors"
	"strings"
	"unicode/utf8"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

var (
	ErrFolderTooManyTags = stderrors.New("catalog: too many folder tags")
	ErrFolderTagTooLong  = stderrors.New("catalog: folder tag too long")
)

func NormalizeFolderTags(raw []string) ([]string, error) {
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, t := range raw {
		t = strings.Join(strings.Fields(strings.TrimLeft(strings.TrimSpace(t), "#＃")), " ")
		if t == "" {
			continue
		}
		if utf8.RuneCountInString(t) > model.FolderTagMaxRunes {
			return nil, ErrFolderTagTooLong
		}
		key := strings.ToLower(t)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	if len(out) > model.FolderTagsMax {
		return nil, ErrFolderTooManyTags
	}
	return out, nil
}

func setFolderTagsTx(tx *gorm.DB, folderID int64, tags []string) error {
	if err := tx.Where("folder_id = ?", folderID).Delete(&model.CatalogUserFolderTag{}).Error; err != nil {
		return err
	}
	if len(tags) == 0 {
		return nil
	}
	rows := make([]model.CatalogUserFolderTag, len(tags))
	for i, t := range tags {
		rows[i] = model.CatalogUserFolderTag{FolderID: folderID, Tag: t, Position: int16(i)}
	}
	return tx.Create(&rows).Error
}

func (s *UserFolderService) FolderTags(ctx context.Context, folderIDs []int64) (map[int64][]string, error) {
	out := make(map[int64][]string, len(folderIDs))
	if len(folderIDs) == 0 {
		return out, nil
	}
	var rows []model.CatalogUserFolderTag
	err := s.db.WithContext(ctx).Where("folder_id IN ?", folderIDs).Order("folder_id, position").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.FolderID] = append(out[r.FolderID], r.Tag)
	}
	return out, nil
}

type FolderTagCount struct {
	Tag   string
	Count int
}

func (s *UserFolderService) DirectoryTags(ctx context.Context, nsfw bool, limit int) ([]FolderTagCount, error) {
	ids := s.publicFolders(ctx, PublicFolderQuery{NSFW: nsfw}).Select("catalog_user_folder.id")
	var rows []FolderTagCount
	err := s.db.WithContext(ctx).Model(&model.CatalogUserFolderTag{}).
		Select("tag, count(*) AS count").Where("folder_id IN (?)", ids).
		Group("tag").Order("count DESC, tag ASC").Limit(limit).Scan(&rows).Error
	return rows, err
}
