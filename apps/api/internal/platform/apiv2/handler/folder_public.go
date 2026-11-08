package handler

import (
	"context"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	catsvc "api/internal/platform/catalog/service"
)

func optionalIDParam(name, raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	id, ok := repr.ParseID(raw)
	if !ok {
		p := problem.New(problem.CodeInvalidParameter, "", "", name+" must be a positive decimal id.")
		p.Errors = []problem.FieldError{{Parameter: name, Reason: problem.ReasonInvalidFormat, Detail: raw}}
		return 0, p
	}
	return id, nil
}

func requiredIDParam(name, raw string) (int64, error) {
	if raw == "" {
		p := problem.New(problem.CodeInvalidParameter, "", "", name+" is required.")
		p.Errors = []problem.FieldError{{Parameter: name, Reason: problem.ReasonRequired, Detail: "send a positive decimal id"}}
		return 0, p
	}
	return optionalIDParam(name, raw)
}

func (c *Catalog) ListPublicFolders(ctx context.Context, q catsvc.PublicFolderQuery, raw collect.Query) (repr.List[repr.UserFolder], error) {
	if c == nil || c.Folders == nil {
		return repr.List[repr.UserFolder]{}, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	cur, err := parsePublicFolderCursor(q.Sort, raw.Cursor)
	if err != nil {
		return repr.List[repr.UserFolder]{}, err
	}
	limit := raw.Limit
	if limit <= 0 {
		limit = collect.DefaultLimit
	}
	q.Cursor, q.Limit = cur, limit+1
	rows, err := c.Folders.ListPublicFolders(ctx, q)
	if err != nil {
		return repr.List[repr.UserFolder]{}, folderErr(err)
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		next = publicFolderNextCursor(q.Sort, rows[len(rows)-1])
	}
	items, err := c.folderViews(ctx, rows)
	if err != nil {
		return repr.List[repr.UserFolder]{}, err
	}
	var total int64
	if raw.IncludeTotal {
		if total, err = c.Folders.CountPublicFolders(ctx, q); err != nil {
			return repr.List[repr.UserFolder]{}, folderErr(err)
		}
	}
	return finishList(items, next, total, raw, nil), nil
}

func (c *Catalog) GetPublicFolder(ctx context.Context, folderID int64) (repr.UserFolder, error) {
	if c == nil || c.Folders == nil {
		return repr.UserFolder{}, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	row, err := c.Folders.GetPublic(ctx, folderID)
	if err != nil {
		return repr.UserFolder{}, folderErr(err)
	}
	return c.folderViewOf(ctx, *row)
}

func (c *Catalog) ListPublicFolderItems(ctx context.Context, folderID int64, q collect.Query) (repr.List[repr.UserFolderItem], error) {
	if c == nil || c.Folders == nil {
		return repr.List[repr.UserFolderItem]{}, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	page, cerr := parseFolderItemCursor(q)
	if cerr != nil {
		return repr.List[repr.UserFolderItem]{}, cerr
	}
	rows, err := c.Folders.ListPublicItems(ctx, folderID, page)
	if err != nil {
		return repr.List[repr.UserFolderItem]{}, folderErr(err)
	}
	items, next := folderItemsList(rows, page)
	var total int64
	if q.IncludeTotal {
		if total, err = c.Folders.CountPublicItems(ctx, folderID); err != nil {
			return repr.List[repr.UserFolderItem]{}, folderErr(err)
		}
	}
	return finishList(items, next, total, q, nil), nil
}

func (c *Catalog) ListFolderTags(ctx context.Context, q collect.Query) (repr.List[repr.FolderTagCount], error) {
	if c == nil || c.Folders == nil {
		return repr.List[repr.FolderTagCount]{}, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	if q.Cursor != "" {
		return repr.List[repr.FolderTagCount]{}, collectInvalidCursor()
	}
	limit := q.Limit
	if limit <= 0 {
		limit = collect.DefaultLimit
	}
	rows, err := c.Folders.DirectoryTags(ctx, q.NSFW, limit)
	if err != nil {
		return repr.List[repr.FolderTagCount]{}, err
	}
	items := make([]repr.FolderTagCount, 0, len(rows))
	for _, r := range rows {
		items = append(items, repr.FolderTagCount{Object: "folder_tag", Tag: r.Tag, FolderCount: r.Count})
	}
	return finishList(items, nil, int64(len(items)), q, nil), nil
}
