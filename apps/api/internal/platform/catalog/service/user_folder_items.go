package service

import (
	"context"
	stderrors "errors"
	"strings"
	"time"
	"unicode/utf8"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrFolderItemNotFound     = stderrors.New("catalog: work is not in this folder")
	ErrFolderItemAnchor       = stderrors.New("catalog: the move anchor is not another work in this folder")
	ErrFolderItemAnchorBoth   = stderrors.New("catalog: name after_work_id or before_work_id, not both")
	ErrFolderItemNoteTooLong  = stderrors.New("catalog: folder item note is too long")
	ErrFolderItemNothingToSet = stderrors.New("catalog: the item patch names nothing to update")
)

type FolderItemOrder int8

const (
	FolderItemsByUpdated FolderItemOrder = iota
	FolderItemsByPosition
)

type FolderItemPage struct {
	Order         FolderItemOrder
	After         bool
	Since         time.Time
	SincePosition int64
	SinceWorkID   int64
	Limit         int
}

func folderItemsPage(q *gorm.DB, folderID int64, p FolderItemPage) ([]model.CatalogUserFolderItem, error) {
	q = q.Where("folder_id = ?", folderID).Where(liveWorkCond("catalog_user_folder_item.work_id"))
	var rows []model.CatalogUserFolderItem
	if p.Order == FolderItemsByPosition {
		if p.After {
			q = q.Where("(position, work_id) > (?, ?)", p.SincePosition, p.SinceWorkID)
		}
		err := q.Order("position ASC, work_id ASC").Limit(p.Limit).Find(&rows).Error
		return rows, err
	}
	if p.After {
		if p.SinceWorkID > 0 {
			q = q.Where("updated_at > ? OR (updated_at = ? AND work_id > ?)", p.Since, p.Since, p.SinceWorkID)
		} else {
			q = q.Where("updated_at > ?", p.Since)
		}
	}
	err := q.Order("updated_at ASC, work_id ASC").Limit(p.Limit).Find(&rows).Error
	return rows, err
}

func nextFolderItemPosition(tx *gorm.DB, folderID int64) (int64, error) {
	var top int64
	err := tx.Model(&model.CatalogUserFolderItem{}).Where("folder_id = ?", folderID).
		Select("COALESCE(max(position), 0)").Scan(&top).Error
	return top + model.FolderItemPositionGap, err
}

type FolderItemPatch struct {
	Note         *string
	AfterWorkID  *int64
	BeforeWorkID *int64
}

func NormalizeFolderItemNote(raw string) (string, error) {
	note := strings.TrimSpace(raw)
	if utf8.RuneCountInString(note) > model.FolderItemNoteMax {
		return "", ErrFolderItemNoteTooLong
	}
	return note, nil
}

func (s *UserFolderService) PatchItem(ctx context.Context, uid, folderID, workID int64, p FolderItemPatch) (*model.CatalogUserFolderItem, bool, error) {
	if uid <= 0 {
		return nil, false, ErrFolderActorRequired
	}
	if p.Note == nil && p.AfterWorkID == nil && p.BeforeWorkID == nil {
		return nil, false, ErrFolderItemNothingToSet
	}
	if p.AfterWorkID != nil && p.BeforeWorkID != nil {
		return nil, false, ErrFolderItemAnchorBoth
	}
	var note string
	if p.Note != nil {
		n, err := NormalizeFolderItemNote(*p.Note)
		if err != nil {
			return nil, false, err
		}
		note = n
	}
	var out model.CatalogUserFolderItem
	var noteChanged bool
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ownedFolder(tx.Clauses(clause.Locking{Strength: "UPDATE"}), uid, folderID, &model.CatalogUserFolder{}); err != nil {
			return err
		}
		if err := folderItem(tx, folderID, workID, &out); err != nil {
			return err
		}
		updates := map[string]any{}
		if p.Note != nil && note != out.Note {
			updates["note"] = note
			noteChanged = true
		}
		if p.AfterWorkID != nil || p.BeforeWorkID != nil {
			pos, err := placeFolderItem(tx, folderID, workID, p.AfterWorkID, p.BeforeWorkID)
			if err != nil {
				return err
			}
			if pos != out.Position {
				updates["position"] = pos
			}
		}
		if len(updates) == 0 {
			return nil
		}
		now := time.Now()
		updates["updated_at"] = now
		if err := tx.Model(&model.CatalogUserFolderItem{}).Where("id = ?", out.ID).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.CatalogUserFolder{}).Where("id = ?", folderID).Update("updated_at", now).Error; err != nil {
			return err
		}
		return tx.Take(&out, "id = ?", out.ID).Error
	})
	if err != nil {
		return nil, false, err
	}
	return &out, noteChanged, nil
}

func folderItem(tx *gorm.DB, folderID, workID int64, dst *model.CatalogUserFolderItem) error {
	err := tx.Where("folder_id = ? AND work_id = ?", folderID, workID).
		Where(liveWorkCond("catalog_user_folder_item.work_id")).Take(dst).Error
	if stderrors.Is(err, gorm.ErrRecordNotFound) {
		return ErrFolderItemNotFound
	}
	return err
}

// placeFolderItem answers the position that puts workID directly after (or
// before) the anchor. Positions stay strictly positive: 0 is the column
// default the migration reads as "never placed".
func placeFolderItem(tx *gorm.DB, folderID, workID int64, after, before *int64) (int64, error) {
	anchorID := after
	if anchorID == nil {
		anchorID = before
	}
	if *anchorID == workID {
		return 0, ErrFolderItemAnchor
	}
	for attempt := 0; attempt < 2; attempt++ {
		var anchor model.CatalogUserFolderItem
		if err := folderItem(tx, folderID, *anchorID, &anchor); err != nil {
			if stderrors.Is(err, ErrFolderItemNotFound) {
				return 0, ErrFolderItemAnchor
			}
			return 0, err
		}
		var lower, upper int64
		var haveUpper bool
		if after != nil {
			lower = anchor.Position
			n, ok, err := neighbourPosition(tx, folderID, workID, anchor, true)
			if err != nil {
				return 0, err
			}
			upper, haveUpper = n, ok
		} else {
			upper, haveUpper = anchor.Position, true
			n, _, err := neighbourPosition(tx, folderID, workID, anchor, false)
			if err != nil {
				return 0, err
			}
			lower = n
		}
		if !haveUpper {
			return lower + model.FolderItemPositionGap, nil
		}
		if upper-lower >= 2 {
			return lower + (upper-lower)/2, nil
		}
		if err := renumberFolderItems(tx, folderID); err != nil {
			return 0, err
		}
	}
	return 0, stderrors.New("catalog: folder item positions did not open a gap after renumbering")
}

func neighbourPosition(tx *gorm.DB, folderID, movingWorkID int64, anchor model.CatalogUserFolderItem, above bool) (int64, bool, error) {
	q := tx.Model(&model.CatalogUserFolderItem{}).Select("position").
		Where("folder_id = ? AND work_id <> ?", folderID, movingWorkID).
		Where(liveWorkCond("catalog_user_folder_item.work_id"))
	if above {
		q = q.Where("(position, work_id) > (?, ?)", anchor.Position, anchor.WorkID).Order("position ASC, work_id ASC")
	} else {
		q = q.Where("(position, work_id) < (?, ?)", anchor.Position, anchor.WorkID).Order("position DESC, work_id DESC")
	}
	var pos []int64
	if err := q.Limit(1).Pluck("position", &pos).Error; err != nil {
		return 0, false, err
	}
	if len(pos) == 0 {
		return 0, false, nil
	}
	return pos[0], true, nil
}

func renumberFolderItems(tx *gorm.DB, folderID int64) error {
	return tx.Exec(`
		UPDATE catalog_user_folder_item i SET position = r.pos, updated_at = now()
		  FROM (SELECT id, ? * row_number() OVER (ORDER BY position, work_id) AS pos
		          FROM catalog_user_folder_item WHERE folder_id = ?) r
		 WHERE i.id = r.id AND i.position <> r.pos`, model.FolderItemPositionGap, folderID).Error
}

func (s *UserFolderService) PublicItem(ctx context.Context, folderID, workID int64) (*model.CatalogUserFolderItem, error) {
	if _, err := s.GetPublic(ctx, folderID); err != nil {
		return nil, err
	}
	var row model.CatalogUserFolderItem
	if err := folderItem(s.db.WithContext(ctx), folderID, workID, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *UserFolderService) NotedItems(ctx context.Context, folderID int64) ([]model.CatalogUserFolderItem, error) {
	var rows []model.CatalogUserFolderItem
	err := s.db.WithContext(ctx).Where("folder_id = ? AND note <> ''", folderID).
		Where(liveWorkCond("catalog_user_folder_item.work_id")).Order("position ASC, work_id ASC").Find(&rows).Error
	return rows, err
}

func (s *UserFolderService) ModerationSetItemNote(ctx context.Context, folderID, workID int64, raw string) (*model.CatalogUserFolderItem, error) {
	note, err := NormalizeFolderItemNote(raw)
	if err != nil {
		return nil, err
	}
	var out model.CatalogUserFolderItem
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := folderItem(tx, folderID, workID, &out); err != nil {
			return err
		}
		if out.Note == note {
			return nil
		}
		if err := tx.Model(&model.CatalogUserFolderItem{}).Where("id = ?", out.ID).
			Updates(map[string]any{"note": note, "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		return tx.Take(&out, "id = ?", out.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}
