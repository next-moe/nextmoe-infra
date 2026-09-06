package handler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/model"
	catsvc "api/internal/platform/catalog/service"
)

func folderVisibilityToken(v int16) string {
	if v == model.FolderVisibilityPublic {
		return "public"
	}
	return "private"
}

func folderVisibilityValue(token string) (int16, bool) {
	switch token {
	case "", "private":
		return model.FolderVisibilityPrivate, true
	case "public":
		return model.FolderVisibilityPublic, true
	}
	return 0, false
}

func folderView(row model.CatalogUserFolder) repr.UserFolder {
	return repr.UserFolder{
		Object: "folder", ID: repr.ID(row.ID), Name: row.Name, Description: row.Description,
		Visibility: folderVisibilityToken(row.Visibility), IsDefault: row.IsDefault,
		ItemCount: row.ItemCount,
		CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: row.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func folderItemView(row model.CatalogUserFolderItem) repr.UserFolderItem {
	return repr.UserFolderItem{
		Object: "folder_item", FolderID: repr.ID(row.FolderID), WorkID: repr.ID(row.WorkID),
		CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: row.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func (c *Catalog) ListFolders(ctx context.Context, q collect.Query) (repr.List[repr.UserFolder], error) {
	if c == nil || c.Folders == nil {
		return repr.List[repr.UserFolder]{}, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.List[repr.UserFolder]{}, err
	}
	sinceID := int64(0)
	if q.Cursor != "" {
		payload, perr := collect.DecodeCursor(q.Cursor)
		if perr != nil {
			return repr.List[repr.UserFolder]{}, perr
		}
		id, ok := repr.ParseID(payload)
		if !ok {
			return repr.List[repr.UserFolder]{}, collectInvalidCursor()
		}
		sinceID = id
	}
	limit := q.Limit
	if limit <= 0 {
		limit = collect.DefaultLimit
	}
	rows, lerr := c.Folders.ListMine(ctx, uid, sinceID, limit+1)
	if lerr != nil {
		return repr.List[repr.UserFolder]{}, lerr
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		s := collect.EncodeCursor(repr.ID(rows[len(rows)-1].ID))
		next = &s
	}
	items := make([]repr.UserFolder, 0, len(rows))
	for _, r := range rows {
		items = append(items, folderView(r))
	}
	var total int64
	if q.IncludeTotal {
		if total, lerr = c.Folders.CountMine(ctx, uid); lerr != nil {
			return repr.List[repr.UserFolder]{}, lerr
		}
	}
	return finishList(items, next, total, q, nil), nil
}

func (c *Catalog) CreateFolder(ctx context.Context, name, description, visibility string, isDefault bool) (repr.UserFolder, error) {
	if c == nil || c.Folders == nil {
		return repr.UserFolder{}, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.UserFolder{}, err
	}
	vis, ok := folderVisibilityValue(visibility)
	if !ok {
		return repr.UserFolder{}, folderErr(catsvc.ErrFolderBadVisibility)
	}
	row, cerr := c.Folders.Create(ctx, catsvc.FolderCreate{
		OwnerUID: uid, Name: name, Description: description, Visibility: vis, IsDefault: isDefault,
	})
	if cerr != nil {
		return repr.UserFolder{}, folderErr(cerr)
	}
	return folderView(*row), nil
}

func (c *Catalog) GetFolder(ctx context.Context, folderID int64) (repr.UserFolder, error) {
	if c == nil || c.Folders == nil {
		return repr.UserFolder{}, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.UserFolder{}, err
	}
	row, gerr := c.Folders.Get(ctx, uid, folderID)
	if gerr != nil {
		return repr.UserFolder{}, folderErr(gerr)
	}
	if row == nil {
		return repr.UserFolder{}, problem.New(problem.CodeNotFound, "", "", "No such folder.")
	}
	return folderView(*row), nil
}

func (c *Catalog) PatchFolder(ctx context.Context, folderID int64, name, description, visibility *string, isDefault *bool) (repr.UserFolder, error) {
	if c == nil || c.Folders == nil {
		return repr.UserFolder{}, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.UserFolder{}, err
	}
	p := catsvc.FolderPatch{Name: name, Description: description, IsDefault: isDefault}
	if visibility != nil {
		vis, ok := folderVisibilityValue(*visibility)
		if !ok {
			return repr.UserFolder{}, folderErr(catsvc.ErrFolderBadVisibility)
		}
		p.Visibility = &vis
	}
	row, perr := c.Folders.Patch(ctx, uid, folderID, p)
	if perr != nil {
		return repr.UserFolder{}, folderErr(perr)
	}
	return folderView(*row), nil
}

func (c *Catalog) DeleteFolder(ctx context.Context, folderID int64) error {
	if c == nil || c.Folders == nil {
		return problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return err
	}
	return folderErr(c.Folders.Delete(ctx, uid, folderID))
}

func (c *Catalog) ListFolderItems(ctx context.Context, folderID int64, q collect.Query) (repr.List[repr.UserFolderItem], error) {
	if c == nil || c.Folders == nil {
		return repr.List[repr.UserFolderItem]{}, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.List[repr.UserFolderItem]{}, err
	}
	since, sinceWorkID := time.Time{}, int64(0)
	if q.Cursor != "" {
		// The playtime lane this family was cloned from emits its raw
		// RFC3339Nano|work_id key as next_cursor, violating repr.List's
		// published ^cur_ pattern — that raw form was copied here and caught
		// in review. Folder cursors wrap the same key via EncodeCursor.
		ts, perr := collect.DecodeCursor(q.Cursor)
		if perr != nil {
			return repr.List[repr.UserFolderItem]{}, perr
		}
		if i := strings.LastIndexByte(ts, '|'); i >= 0 {
			id, ok := repr.ParseID(ts[i+1:])
			if !ok {
				return repr.List[repr.UserFolderItem]{}, collectInvalidCursor()
			}
			ts, sinceWorkID = ts[:i], id
		}
		t, terr := time.Parse(time.RFC3339, ts)
		if terr != nil {
			return repr.List[repr.UserFolderItem]{}, collectInvalidCursor()
		}
		since = t
	}
	limit := q.Limit
	if limit <= 0 {
		limit = collect.DefaultLimit
	}
	rows, lerr := c.Folders.ListItems(ctx, uid, folderID, since, sinceWorkID, limit+1)
	if lerr != nil {
		return repr.List[repr.UserFolderItem]{}, folderErr(lerr)
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		s := collect.EncodeCursor(last.UpdatedAt.UTC().Format(time.RFC3339Nano) + "|" + repr.ID(last.WorkID))
		next = &s
	}
	items := make([]repr.UserFolderItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, folderItemView(r))
	}
	var total int64
	if q.IncludeTotal {
		if total, lerr = c.Folders.CountItems(ctx, uid, folderID); lerr != nil {
			return repr.List[repr.UserFolderItem]{}, folderErr(lerr)
		}
	}
	return finishList(items, next, total, q, nil), nil
}

func (c *Catalog) PutFolderItem(ctx context.Context, folderID, workID int64) (repr.UserFolderItem, error) {
	if c == nil || c.Folders == nil {
		return repr.UserFolderItem{}, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.UserFolderItem{}, err
	}
	row, perr := c.Folders.PutItem(ctx, uid, folderID, workID)
	if perr != nil {
		return repr.UserFolderItem{}, folderErr(perr)
	}
	return folderItemView(*row), nil
}

func (c *Catalog) DeleteFolderItem(ctx context.Context, folderID, workID int64) error {
	if c == nil || c.Folders == nil {
		return problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return err
	}
	return folderErr(c.Folders.DeleteItem(ctx, uid, folderID, workID))
}

func (c *Catalog) BatchFolderItems(ctx context.Context, folderID int64, workIDs []string) (repr.List[repr.FolderItemBatchItem], error) {
	out := make([]repr.FolderItemBatchItem, 0, len(workIDs))
	// maxItems on the request schema refuses this first with a 422; the guard
	// stays because the schema and this function are two different authorities
	// and only one of them is the one that writes rows.
	if len(workIDs) > collect.MaxBatchItems {
		return repr.List[repr.FolderItemBatchItem]{}, problem.New(problem.CodeTooManyIDs, "", "", "batch folder items named more than 100 items.")
	}
	for i, s := range workIDs {
		id, ok := repr.ParseID(s)
		if !ok {
			p := problem.New(problem.CodeValidationFailed, "", "", "work_id must be a decimal catalog id.")
			p.Errors = []problem.FieldError{{Pointer: fmt.Sprintf("/items/%d/work_id", i), Reason: problem.ReasonInvalidFormat, Detail: s}}
			out = append(out, repr.FolderItemBatchItem{Status: 422, Problem: p})
			continue
		}
		rec, err := c.PutFolderItem(ctx, folderID, id)
		if err != nil {
			p, ok := err.(*problem.Problem)
			if !ok {
				p = problem.New(problem.CodeInternalError, "", "", err.Error())
			}
			out = append(out, repr.FolderItemBatchItem{Status: p.Status, Problem: p})
			continue
		}
		obj, wid := "folder_item", rec.WorkID
		out = append(out, repr.FolderItemBatchItem{Status: 200, Object: &obj, WorkID: &wid})
	}
	return repr.NewList(out, nil), nil
}

func folderErr(err error) error {
	if err == nil {
		return nil
	}
	switch err {
	case catsvc.ErrFolderNotFound:
		return problem.New(problem.CodeNotFound, "", "", "No such folder.")
	case catsvc.ErrFolderWorkUnavailable:
		return problem.New(problem.CodeNotFound, "", "", "work is not available for folders.")
	case catsvc.ErrFolderActorRequired:
		return problem.New(problem.CodeUserIdentityRequired, "", "", err.Error())
	case catsvc.ErrFolderNameRequired:
		p := problem.New(problem.CodeValidationFailed, "", "", "folder name is required.")
		p.Errors = []problem.FieldError{{Pointer: "/name", Reason: problem.ReasonRequired, Detail: "send a non-blank name"}}
		return p
	case catsvc.ErrFolderBadVisibility:
		p := problem.New(problem.CodeValidationFailed, "", "", "visibility must be private or public.")
		p.Errors = []problem.FieldError{{Pointer: "/visibility", Reason: problem.ReasonInvalidFormat, Detail: "private or public"}}
		return p
	case catsvc.ErrFolderDefaultOnly:
		p := problem.New(problem.CodeValidationFailed, "", "", "is_default can only be set to true; set it on another folder to move it.")
		p.Errors = []problem.FieldError{{Pointer: "/is_default", Reason: problem.ReasonInvalidFormat, Detail: "only true is accepted"}}
		return p
	case catsvc.ErrFolderNothingToUpdate:
		p := problem.New(problem.CodeValidationFailed, "", "", "the patch names no field to update.")
		p.Errors = []problem.FieldError{{Pointer: "", Reason: problem.ReasonRequired, Detail: "send at least one of name, description, visibility, is_default"}}
		return p
	case catsvc.ErrFolderDefaultDeletion:
		p := problem.New(problem.CodeValidationFailed, "", "", "the default folder cannot be deleted; make another folder the default first.")
		p.Errors = []problem.FieldError{{Parameter: "id", Reason: problem.ReasonInvalidFormat, Detail: "this folder is the default"}}
		return p
	case catsvc.ErrFolderLimit:
		p := problem.New(problem.CodeValidationFailed, "", "", fmt.Sprintf("a user may keep at most %d folders.", model.FoldersPerUserMax))
		return p
	case catsvc.ErrFolderItemLimit:
		p := problem.New(problem.CodeValidationFailed, "", "", fmt.Sprintf("a folder may hold at most %d works.", model.FolderItemsMax))
		return p
	}
	return err
}
