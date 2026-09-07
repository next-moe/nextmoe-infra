package handler

import (
	"context"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
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

func (c *Catalog) ListPublicFolders(ctx context.Context, ownerUID int64, q collect.Query) (repr.List[repr.UserFolder], error) {
	if c == nil || c.Folders == nil {
		return repr.List[repr.UserFolder]{}, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	sinceID := int64(0)
	if q.Cursor != "" {
		id, ok := repr.ParseID(q.Cursor)
		if !ok {
			return repr.List[repr.UserFolder]{}, collectInvalidCursor()
		}
		sinceID = id
	}
	limit := q.Limit
	if limit <= 0 {
		limit = collect.DefaultLimit
	}
	rows, err := c.Folders.ListPublic(ctx, ownerUID, sinceID, limit+1)
	if err != nil {
		return repr.List[repr.UserFolder]{}, folderErr(err)
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		s := repr.ID(rows[len(rows)-1].ID)
		next = &s
	}
	items := make([]repr.UserFolder, 0, len(rows))
	for _, r := range rows {
		items = append(items, folderView(r))
	}
	var total int64
	if q.IncludeTotal {
		if total, err = c.Folders.CountPublic(ctx, ownerUID); err != nil {
			return repr.List[repr.UserFolder]{}, folderErr(err)
		}
	}
	return finishList(items, next, total, q, nil), nil
}

func (c *Catalog) GetPublicFolder(ctx context.Context, folderID int64) (repr.UserFolder, error) {
	if c == nil || c.Folders == nil {
		return repr.UserFolder{}, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	row, err := c.Folders.GetPublic(ctx, folderID)
	if err != nil {
		return repr.UserFolder{}, folderErr(err)
	}
	return folderView(*row), nil
}

func (c *Catalog) ListPublicFolderItems(ctx context.Context, folderID int64, q collect.Query) (repr.List[repr.UserFolderItem], error) {
	if c == nil || c.Folders == nil {
		return repr.List[repr.UserFolderItem]{}, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	since, sinceWorkID, cerr := parseFolderItemCursor(q.Cursor)
	if cerr != nil {
		return repr.List[repr.UserFolderItem]{}, cerr
	}
	limit := q.Limit
	if limit <= 0 {
		limit = collect.DefaultLimit
	}
	rows, err := c.Folders.ListPublicItems(ctx, folderID, since, sinceWorkID, limit+1)
	if err != nil {
		return repr.List[repr.UserFolderItem]{}, folderErr(err)
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		next = folderItemNextCursor(rows[len(rows)-1])
	}
	items := make([]repr.UserFolderItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, folderItemView(r))
	}
	var total int64
	if q.IncludeTotal {
		if total, err = c.Folders.CountPublicItems(ctx, folderID); err != nil {
			return repr.List[repr.UserFolderItem]{}, folderErr(err)
		}
	}
	return finishList(items, next, total, q, nil), nil
}
