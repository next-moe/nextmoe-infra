package handler

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/model"
	catsvc "api/internal/platform/catalog/service"
)

func folderVisibilityToken(v int16) string {
	switch v {
	case model.FolderVisibilityPublic:
		return "public"
	case model.FolderVisibilityUnlisted:
		return "unlisted"
	}
	return "private"
}

func folderVisibilityValue(token string) (int16, bool) {
	switch token {
	case "", "private":
		return model.FolderVisibilityPrivate, true
	case "public":
		return model.FolderVisibilityPublic, true
	case "unlisted":
		return model.FolderVisibilityUnlisted, true
	}
	return 0, false
}

func (c *Catalog) ListFolders(ctx context.Context, q collect.Query, containsWorkID int64) (repr.List[repr.UserFolder], error) {
	if c == nil || c.Folders == nil {
		return repr.List[repr.UserFolder]{}, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.List[repr.UserFolder]{}, err
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
	list, count := c.Folders.ListMine, c.Folders.CountMine
	if containsWorkID > 0 {
		list = func(ctx context.Context, uid, since int64, n int) ([]model.CatalogUserFolder, error) {
			return c.Folders.ListMineContaining(ctx, uid, containsWorkID, since, n)
		}
		count = func(ctx context.Context, uid int64) (int64, error) {
			return c.Folders.CountMineContaining(ctx, uid, containsWorkID)
		}
	}
	rows, lerr := list(ctx, uid, sinceID, limit+1)
	if lerr != nil {
		return repr.List[repr.UserFolder]{}, lerr
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		s := repr.ID(rows[len(rows)-1].ID)
		next = &s
	}
	items, verr := c.folderViews(ctx, rows)
	if verr != nil {
		return repr.List[repr.UserFolder]{}, verr
	}
	var total int64
	if q.IncludeTotal {
		if total, lerr = count(ctx, uid); lerr != nil {
			return repr.List[repr.UserFolder]{}, lerr
		}
	}
	return finishList(items, next, total, q, nil), nil
}

type folderWrite struct {
	Name, Description, Visibility *string
	IsDefault                     *bool
	Tags                          *[]string
	CoverWorkID                   *string
}

func (c *Catalog) CreateFolder(ctx context.Context, name, description, visibility string, isDefault bool, tags []string) (repr.UserFolder, error) {
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
	tags, terr := catsvc.NormalizeFolderTags(tags)
	if terr != nil {
		return repr.UserFolder{}, folderErr(terr)
	}
	if err := c.checkFolderText(ctx, uid, folderText{name, "/name"}, folderText{description, "/description"},
		folderText{strings.Join(tags, " "), "/tags"}); err != nil {
		return repr.UserFolder{}, err
	}
	row, cerr := c.Folders.Create(ctx, catsvc.FolderCreate{
		OwnerUID: uid, Name: name, Description: description, Visibility: vis, IsDefault: isDefault, Tags: tags,
	})
	if cerr != nil {
		return repr.UserFolder{}, folderErr(cerr)
	}
	c.scanFolder(*row, tags)
	return c.folderViewOf(ctx, *row)
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
	return c.folderViewOf(ctx, *row)
}

func (c *Catalog) PatchFolder(ctx context.Context, folderID int64, w folderWrite) (repr.UserFolder, error) {
	if c == nil || c.Folders == nil {
		return repr.UserFolder{}, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.UserFolder{}, err
	}
	name, description := w.Name, w.Description
	p := catsvc.FolderPatch{Name: name, Description: description, IsDefault: w.IsDefault}
	if w.Visibility != nil {
		vis, ok := folderVisibilityValue(*w.Visibility)
		if !ok {
			return repr.UserFolder{}, folderErr(catsvc.ErrFolderBadVisibility)
		}
		p.Visibility = &vis
	}
	if w.Tags != nil {
		tags, terr := catsvc.NormalizeFolderTags(*w.Tags)
		if terr != nil {
			return repr.UserFolder{}, folderErr(terr)
		}
		p.Tags = &tags
	}
	if w.CoverWorkID != nil {
		cover := int64(0)
		if *w.CoverWorkID != "" {
			id, ok := repr.ParseID(*w.CoverWorkID)
			if !ok {
				return repr.UserFolder{}, problemInvalidID(*w.CoverWorkID)
			}
			cover = id
		}
		p.CoverWorkID = &cover
	}
	prev, gerr := c.Folders.Get(ctx, uid, folderID)
	if gerr != nil {
		return repr.UserFolder{}, folderErr(gerr)
	}
	if prev == nil {
		return repr.UserFolder{}, folderErr(catsvc.ErrFolderNotFound)
	}
	var texts []folderText
	if name != nil && *name != prev.Name {
		texts = append(texts, folderText{*name, "/name"})
	}
	if description != nil && *description != prev.Description {
		texts = append(texts, folderText{*description, "/description"})
	}
	prevTags, terr := c.Folders.FolderTags(ctx, []int64{folderID})
	if terr != nil {
		return repr.UserFolder{}, terr
	}
	tagsChanged := p.Tags != nil && !slices.Equal(*p.Tags, prevTags[folderID])
	if tagsChanged {
		texts = append(texts, folderText{strings.Join(*p.Tags, " "), "/tags"})
	}
	if err := c.checkFolderText(ctx, uid, texts...); err != nil {
		return repr.UserFolder{}, err
	}
	row, perr := c.Folders.Patch(ctx, uid, folderID, p)
	if perr != nil {
		return repr.UserFolder{}, folderErr(perr)
	}
	published := model.FolderReadable(row.Visibility) && !model.FolderReadable(prev.Visibility)
	if published || tagsChanged || row.Name != prev.Name || row.Description != prev.Description {
		tags := prevTags[folderID]
		if p.Tags != nil {
			tags = *p.Tags
		}
		c.scanFolder(*row, tags)
	}
	if published {
		if noted, nerr := c.Folders.NotedItems(ctx, row.ID); nerr == nil {
			c.scanFolderNotes(row.OwnerUID, noted)
		}
	}
	return c.folderViewOf(ctx, *row)
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
	page, cerr := parseFolderItemCursor(q)
	if cerr != nil {
		return repr.List[repr.UserFolderItem]{}, cerr
	}
	rows, lerr := c.Folders.ListItems(ctx, uid, folderID, page)
	if lerr != nil {
		return repr.List[repr.UserFolderItem]{}, folderErr(lerr)
	}
	items, next := folderItemsList(rows, page)
	var total int64
	if q.IncludeTotal {
		if total, lerr = c.Folders.CountItems(ctx, uid, folderID); lerr != nil {
			return repr.List[repr.UserFolderItem]{}, folderErr(lerr)
		}
	}
	return finishList(items, next, total, q, nil), nil
}

func (c *Catalog) GetFolderItem(ctx context.Context, folderID, workID int64) (repr.UserFolderItem, error) {
	if c == nil || c.Folders == nil {
		return repr.UserFolderItem{}, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.UserFolderItem{}, err
	}
	row, gerr := c.Folders.Item(ctx, uid, folderID, workID)
	if gerr != nil {
		return repr.UserFolderItem{}, folderErr(gerr)
	}
	return folderItemView(*row), nil
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

func (c *Catalog) PatchFolderItem(ctx context.Context, folderID, workID int64, note, afterWorkID, beforeWorkID *string) (repr.UserFolderItem, error) {
	if c == nil || c.Folders == nil {
		return repr.UserFolderItem{}, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.UserFolderItem{}, err
	}
	p := catsvc.FolderItemPatch{}
	for _, a := range []struct {
		raw     *string
		dst     **int64
		pointer string
	}{{afterWorkID, &p.AfterWorkID, "/after_work_id"}, {beforeWorkID, &p.BeforeWorkID, "/before_work_id"}} {
		if a.raw == nil {
			continue
		}
		id, ok := repr.ParseID(*a.raw)
		if !ok {
			pr := problem.New(problem.CodeValidationFailed, "", "", "the move anchor must be a decimal catalog id.")
			pr.Errors = []problem.FieldError{{Pointer: a.pointer, Reason: problem.ReasonInvalidFormat, Detail: *a.raw}}
			return repr.UserFolderItem{}, pr
		}
		*a.dst = &id
	}
	folder, gerr := c.Folders.Get(ctx, uid, folderID)
	if gerr != nil {
		return repr.UserFolderItem{}, folderErr(gerr)
	}
	if folder == nil {
		return repr.UserFolderItem{}, folderErr(catsvc.ErrFolderNotFound)
	}
	if note != nil {
		normalized, nerr := catsvc.NormalizeFolderItemNote(*note)
		if nerr != nil {
			return repr.UserFolderItem{}, folderErr(nerr)
		}
		if err := c.checkFolderText(ctx, uid, folderText{normalized, "/note"}); err != nil {
			return repr.UserFolderItem{}, err
		}
		p.Note = &normalized
	}
	row, noteChanged, perr := c.Folders.PatchItem(ctx, uid, folderID, workID, p)
	if perr != nil {
		return repr.UserFolderItem{}, folderErr(perr)
	}
	if noteChanged && model.FolderReadable(folder.Visibility) {
		c.scanFolderNotes(uid, []model.CatalogUserFolderItem{*row})
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
		p := problem.New(problem.CodeValidationFailed, "", "", "visibility must be private, unlisted or public.")
		p.Errors = []problem.FieldError{{Pointer: "/visibility", Reason: problem.ReasonInvalidFormat, Detail: "private, unlisted or public"}}
		return p
	case catsvc.ErrFolderDefaultOnly:
		p := problem.New(problem.CodeValidationFailed, "", "", "is_default can only be set to true; set it on another folder to move it.")
		p.Errors = []problem.FieldError{{Pointer: "/is_default", Reason: problem.ReasonInvalidFormat, Detail: "only true is accepted"}}
		return p
	case catsvc.ErrFolderNothingToUpdate:
		p := problem.New(problem.CodeValidationFailed, "", "", "the patch names no field to update.")
		p.Errors = []problem.FieldError{{Pointer: "", Reason: problem.ReasonRequired, Detail: "send at least one of name, description, visibility, is_default, tags, cover_work_id"}}
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
	case catsvc.ErrFolderItemNotFound:
		return problem.New(problem.CodeNotFound, "", "", "work is not in this folder.")
	case catsvc.ErrFolderItemAnchor:
		p := problem.New(problem.CodeValidationFailed, "", "", "the move anchor must be another work in this folder.")
		p.Errors = []problem.FieldError{{Pointer: "", Reason: problem.ReasonNotAllowedValue, Detail: "after_work_id / before_work_id must name another work this folder holds"}}
		return p
	case catsvc.ErrFolderItemAnchorBoth:
		p := problem.New(problem.CodeValidationFailed, "", "", "send after_work_id or before_work_id, not both.")
		p.Errors = []problem.FieldError{{Pointer: "/before_work_id", Reason: problem.ReasonNotAllowedValue, Detail: "after_work_id is already set"}}
		return p
	case catsvc.ErrFolderItemNoteTooLong:
		p := problem.New(problem.CodeValidationFailed, "", "", fmt.Sprintf("a note may be at most %d characters.", model.FolderItemNoteMax))
		p.Errors = []problem.FieldError{{Pointer: "/note", Reason: problem.ReasonInvalidFormat, Detail: "too long"}}
		return p
	case catsvc.ErrFolderItemNothingToSet:
		p := problem.New(problem.CodeValidationFailed, "", "", "the patch names no field to update.")
		p.Errors = []problem.FieldError{{Pointer: "", Reason: problem.ReasonRequired, Detail: "send at least one of note, after_work_id, before_work_id"}}
		return p
	case catsvc.ErrFolderFollowOwn:
		p := problem.New(problem.CodeValidationFailed, "", "", "a folder's owner cannot follow it.")
		p.Errors = []problem.FieldError{{Parameter: "folder_id", Reason: problem.ReasonNotAllowedValue, Detail: "this is the bearer's own folder"}}
		return p
	case catsvc.ErrFolderTooManyTags:
		p := problem.New(problem.CodeValidationFailed, "", "", fmt.Sprintf("a folder may carry at most %d tags.", model.FolderTagsMax))
		p.Errors = []problem.FieldError{{Pointer: "/tags", Reason: problem.ReasonInvalidFormat, Detail: "too many tags"}}
		return p
	case catsvc.ErrFolderTagTooLong:
		p := problem.New(problem.CodeValidationFailed, "", "", fmt.Sprintf("a tag may be at most %d characters.", model.FolderTagMaxRunes))
		p.Errors = []problem.FieldError{{Pointer: "/tags", Reason: problem.ReasonInvalidFormat, Detail: "tag too long"}}
		return p
	case catsvc.ErrFolderCoverNotMember:
		p := problem.New(problem.CodeValidationFailed, "", "", "the cover must be a work this folder holds.")
		p.Errors = []problem.FieldError{{Pointer: "/cover_work_id", Reason: problem.ReasonNotAllowedValue, Detail: "not in the folder"}}
		return p
	case catsvc.ErrFolderFollowLimit:
		return problem.New(problem.CodeValidationFailed, "", "", fmt.Sprintf("a user may follow at most %d folders.", model.FolderFollowsPerUserMax))
	}
	return err
}
