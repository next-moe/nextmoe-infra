package main

import (
	"log/slog"
	"strings"
	"time"

	"api/internal/platform/catalog/model"
)

type itemRow struct {
	FolderID int64
	WorkID   int64
	OwnerUID int64
	Created  time.Time
	Updated  time.Time
}

// itemBatch buffers memberships and writes them with LEAST/GREATEST on
// conflict: a work that reaches the same folder twice keeps the earlier
// created_at and the later updated_at, which is the sync watermark and must
// not travel backwards.
type itemBatch struct {
	imp  *importer
	rows []itemRow
	at   map[[2]int64]int
}

// add folds a pair the batch already carries instead of appending it twice.
// Postgres refuses a statement whose ON CONFLICT target matches one row more
// than once ("cannot affect row a second time"), and the collection lane can
// re-emit a pair it has already placed: two source items whose galgame ids
// merged into one survivor resolve to the same (folder, work). The since-
// removed moyu lane hit exactly this on its first 2026-09-07 production flush
// (two patches sharing one vndb anchor); folding here applies the same
// LEAST/GREATEST the conflict clause would.
func (b *itemBatch) add(r itemRow) error {
	key := [2]int64{r.FolderID, r.WorkID}
	if n, held := b.at[key]; held {
		if r.Created.Before(b.rows[n].Created) {
			b.rows[n].Created = r.Created
		}
		if r.Updated.After(b.rows[n].Updated) {
			b.rows[n].Updated = r.Updated
		}
		return nil
	}
	if b.at == nil {
		b.at = make(map[[2]int64]int, b.imp.batch)
	}
	b.at[key] = len(b.rows)
	b.rows = append(b.rows, r)
	if len(b.rows) >= b.imp.batch {
		return b.flush()
	}
	return nil
}

func (b *itemBatch) reset() {
	b.rows = b.rows[:0]
	clear(b.at)
}

func (b *itemBatch) flush() error {
	if len(b.rows) == 0 || !b.imp.apply {
		b.reset()
		return nil
	}
	var sb strings.Builder
	sb.WriteString(`INSERT INTO catalog_user_folder_item (folder_id, work_id, owner_uid, created_at, updated_at) VALUES `)
	args := make([]any, 0, len(b.rows)*5)
	for n, r := range b.rows {
		if n > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("(?, ?, ?, ?, ?)")
		args = append(args, r.FolderID, r.WorkID, r.OwnerUID, r.Created, r.Updated)
	}
	sb.WriteString(` ON CONFLICT (folder_id, work_id) DO UPDATE SET
		created_at = LEAST(catalog_user_folder_item.created_at, EXCLUDED.created_at),
		updated_at = GREATEST(catalog_user_folder_item.updated_at, EXCLUDED.updated_at)`)
	if err := b.imp.cat.Exec(sb.String(), args...).Error; err != nil {
		return err
	}
	b.reset()
	return nil
}

func (i *importer) newBatch() *itemBatch { return &itemBatch{imp: i} }

type forumCollection struct {
	ID          int64
	UserID      int64
	Name        string
	Description string
	Visibility  string
	IsDefault   bool
	Created     time.Time
	Updated     time.Time
}

func (i *importer) importForumCollections() (counters, error) {
	var c counters
	var colls []forumCollection
	if err := i.forum.Raw(`SELECT id, user_id, name, description, visibility, is_default, created, updated
		FROM galgame_collection ORDER BY id`).Scan(&colls).Error; err != nil {
		return c, err
	}
	var prov []model.CatalogUserFolderImport
	if err := i.cat.Where("site = ?", model.FolderImportSiteForum).Find(&prov).Error; err != nil {
		return c, err
	}
	folderOf := make(map[int64]int64, len(colls))
	for _, p := range prov {
		folderOf[p.SourceID] = p.FolderID
	}

	for _, src := range colls {
		if id, ok := folderOf[src.ID]; ok {
			c.FoldersReused++
			if src.IsDefault {
				i.defaults[src.UserID] = id
			}
			continue
		}
		// Nothing in the database keeps a user to one default folder — the
		// service alone enforces it — so a user who already owns a default
		// (the 2026-09-07 runs created unnamed ones for flat-lane users, and
		// those folders persist) must have this collection adopt it rather
		// than gain a second default nobody can clear through the API.
		if src.IsDefault {
			if existing, ok := i.defaults[src.UserID]; ok {
				if err := i.adoptAsDefault(existing, src); err != nil {
					return c, err
				}
				folderOf[src.ID] = existing
				c.FoldersReused++
				continue
			}
		}
		if i.perUser[src.UserID] >= model.FoldersPerUserMax {
			c.SkippedOverCap++
			continue
		}
		f := model.CatalogUserFolder{
			OwnerUID:    src.UserID,
			Name:        truncate(src.Name, model.FolderNameMax),
			Description: truncate(src.Description, model.FolderDescriptionMax),
			Visibility:  visibilityOf(src.Visibility),
			IsDefault:   src.IsDefault,
			CreatedAt:   src.Created,
			UpdatedAt:   src.Updated,
		}
		if i.apply {
			if err := i.cat.Create(&f).Error; err != nil {
				return c, err
			}
			if err := i.cat.Create(&model.CatalogUserFolderImport{
				Site: model.FolderImportSiteForum, SourceID: src.ID,
				FolderID: f.ID, OwnerUID: src.UserID,
			}).Error; err != nil {
				return c, err
			}
		} else {
			f.ID = i.nextDryRunID()
		}
		folderOf[src.ID] = f.ID
		i.perUser[src.UserID]++
		if src.IsDefault {
			i.defaults[src.UserID] = f.ID
		}
		c.FoldersCreated++
	}

	rows, err := i.forum.Raw(`SELECT collection_id, galgame_id, user_id, created, updated
		FROM galgame_collection_item ORDER BY collection_id, galgame_id`).Rows()
	if err != nil {
		return c, err
	}
	defer rows.Close()
	batch := i.newBatch()
	for rows.Next() {
		var collID, galgameID, uid int64
		var created, updated time.Time
		if err := rows.Scan(&collID, &galgameID, &uid, &created, &updated); err != nil {
			return c, err
		}
		folderID, ok := folderOf[collID]
		if !ok {
			c.SkippedOverCap++
			continue
		}
		work, live, redirected := i.works.resolve(galgameID)
		if redirected {
			c.Redirected++
		}
		if !live {
			c.SkippedNotLive++
			continue
		}
		if err := i.place(batch, &c, folderID, work, uid, created, updated); err != nil {
			return c, err
		}
	}
	if err := rows.Err(); err != nil {
		return c, err
	}
	return c, batch.flush()
}

// adoptAsDefault gives an existing default folder the source collection's own
// name, note and dates, and files the provenance row so the next run reuses it.
func (i *importer) adoptAsDefault(folderID int64, src forumCollection) error {
	if !i.apply {
		return nil
	}
	if err := i.cat.Model(&model.CatalogUserFolder{}).Where("id = ?", folderID).
		Updates(map[string]any{
			"name":        truncate(src.Name, model.FolderNameMax),
			"description": truncate(src.Description, model.FolderDescriptionMax),
			"visibility":  visibilityOf(src.Visibility),
			"created_at":  src.Created,
			"updated_at":  src.Updated,
		}).Error; err != nil {
		return err
	}
	return i.cat.Create(&model.CatalogUserFolderImport{
		Site: model.FolderImportSiteForum, SourceID: src.ID,
		FolderID: folderID, OwnerUID: src.UserID,
	}).Error
}

// recountItems restates item_count from the membership table: an incremented
// counter would be wrong on any re-run, so the stored counter is only ever a
// cache of this query.
func (i *importer) recountItems() error {
	res := i.cat.Exec(`UPDATE catalog_user_folder f
		SET item_count = COALESCE((SELECT count(*) FROM catalog_user_folder_item i WHERE i.folder_id = f.id), 0)
		WHERE f.item_count IS DISTINCT FROM COALESCE((SELECT count(*) FROM catalog_user_folder_item i WHERE i.folder_id = f.id), 0)`)
	if res.Error != nil {
		return res.Error
	}
	slog.Info("recounted item_count", "folders_updated", res.RowsAffected)
	return nil
}
