package handler

import (
	"context"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/model"
	catsvc "api/internal/platform/catalog/service"
)

func folderView(row model.CatalogUserFolder, extra catsvc.FolderExtra) repr.UserFolder {
	token, _ := repr.ContentRating(extra.Rating)
	recentIDs := make([]string, 0, len(extra.RecentWorks))
	for _, id := range extra.RecentWorks {
		recentIDs = append(recentIDs, repr.ID(id))
	}
	tags := extra.Tags
	if tags == nil {
		tags = []string{}
	}
	var cover *string
	if extra.CoverWorkID > 0 {
		id := repr.ID(extra.CoverWorkID)
		cover = &id
	}
	var copiedFrom *string
	if row.CopiedFromID != nil {
		id := repr.ID(*row.CopiedFromID)
		copiedFrom = &id
	}
	return repr.UserFolder{
		Object: "folder", ID: repr.ID(row.ID), OwnerUID: repr.ID(row.OwnerUID),
		Name: row.Name, Description: row.Description,
		Visibility: folderVisibilityToken(row.Visibility), IsDefault: row.IsDefault,
		ItemCount: row.ItemCount, FollowerCount: row.FollowerCount, ContentRating: token,
		Tags: tags, CoverWorkID: cover, CopiedFromFolderID: copiedFrom, RecentWorkIDs: recentIDs,
		CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: row.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func (c *Catalog) folderViews(ctx context.Context, rows []model.CatalogUserFolder) ([]repr.UserFolder, error) {
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	extras, err := c.Folders.FolderExtras(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]repr.UserFolder, 0, len(rows))
	for _, r := range rows {
		out = append(out, folderView(r, extras[r.ID]))
	}
	return out, nil
}

func (c *Catalog) folderViewOf(ctx context.Context, row model.CatalogUserFolder) (repr.UserFolder, error) {
	views, err := c.folderViews(ctx, []model.CatalogUserFolder{row})
	if err != nil {
		return repr.UserFolder{}, err
	}
	return views[0], nil
}

func folderItemView(row model.CatalogUserFolderItem) repr.UserFolderItem {
	return repr.UserFolderItem{
		Object: "folder_item", FolderID: repr.ID(row.FolderID), WorkID: repr.ID(row.WorkID),
		Position: row.Position, Note: row.Note,
		CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: row.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// One reader and one writer for the folder-item keyset, shared by the owner
// lane and the public one. collect.Parse has already stripped the cur_
// envelope and finishList puts it back, so what travels here is the bare key;
// encoding it a second time shipped a double-wrapped cursor in 2.12.0 that
// round-tripped, so the limit=1 crawl stayed green and only the bytes were wrong.
func parseFolderItemCursor(q collect.Query) (catsvc.FolderItemPage, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = collect.DefaultLimit
	}
	p := catsvc.FolderItemPage{Limit: limit + 1}
	if q.Sort == "position" {
		p.Order = catsvc.FolderItemsByPosition
	}
	if q.Cursor == "" {
		return p, nil
	}
	head, workID := q.Cursor, int64(0)
	if i := strings.LastIndexByte(head, '|'); i >= 0 {
		id, ok := repr.ParseID(head[i+1:])
		if !ok {
			return p, collectInvalidCursor()
		}
		head, workID = head[:i], id
	}
	p.After, p.SinceWorkID = true, workID
	if p.Order == catsvc.FolderItemsByPosition {
		pos, err := strconv.ParseInt(head, 10, 64)
		if err != nil || workID == 0 {
			return p, collectInvalidCursor()
		}
		p.SincePosition = pos
		return p, nil
	}
	t, err := time.Parse(time.RFC3339, head)
	if err != nil {
		return p, collectInvalidCursor()
	}
	p.Since = t
	return p, nil
}

func folderItemNextCursor(p catsvc.FolderItemPage, last model.CatalogUserFolderItem) *string {
	head := last.UpdatedAt.UTC().Format(time.RFC3339Nano)
	if p.Order == catsvc.FolderItemsByPosition {
		head = strconv.FormatInt(last.Position, 10)
	}
	s := head + "|" + repr.ID(last.WorkID)
	return &s
}

func folderItemsList(rows []model.CatalogUserFolderItem, p catsvc.FolderItemPage) ([]repr.UserFolderItem, *string) {
	var next *string
	if limit := p.Limit - 1; len(rows) > limit {
		rows = rows[:limit]
		next = folderItemNextCursor(p, rows[len(rows)-1])
	}
	items := make([]repr.UserFolderItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, folderItemView(r))
	}
	return items, next
}

func parsePublicFolderCursor(sort catsvc.FolderSort, cursor string) (catsvc.FolderCursor, error) {
	if cursor == "" {
		return catsvc.FolderCursor{}, nil
	}
	head, tail, hasHead := strings.Cut(cursor, "|")
	if !hasHead {
		head, tail = "", cursor
	}
	id, ok := repr.ParseID(tail)
	if !ok {
		return catsvc.FolderCursor{}, collectInvalidCursor()
	}
	c := catsvc.FolderCursor{Set: true, ID: id}
	switch sort {
	case catsvc.FolderSortFollowers:
		n, err := strconv.Atoi(head)
		if err != nil || n < 0 {
			return catsvc.FolderCursor{}, collectInvalidCursor()
		}
		c.Followers = n
	case catsvc.FolderSortUpdated:
		t, err := time.Parse(time.RFC3339Nano, head)
		if err != nil {
			return catsvc.FolderCursor{}, collectInvalidCursor()
		}
		c.Updated = t
	default:
		if hasHead {
			return catsvc.FolderCursor{}, collectInvalidCursor()
		}
	}
	return c, nil
}

func publicFolderNextCursor(sort catsvc.FolderSort, last model.CatalogUserFolder) *string {
	s := repr.ID(last.ID)
	switch sort {
	case catsvc.FolderSortFollowers:
		s = strconv.Itoa(last.FollowerCount) + "|" + s
	case catsvc.FolderSortUpdated:
		s = last.UpdatedAt.UTC().Format(time.RFC3339Nano) + "|" + s
	}
	return &s
}

func folderSortOf(token string) catsvc.FolderSort {
	switch token {
	case "followers_desc":
		return catsvc.FolderSortFollowers
	case "updated_desc":
		return catsvc.FolderSortUpdated
	case "created_desc":
		return catsvc.FolderSortCreated
	}
	return catsvc.FolderSortID
}
