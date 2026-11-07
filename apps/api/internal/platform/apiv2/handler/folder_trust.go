package handler

import (
	"context"
	stderrors "errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/catalog/model"
	catsvc "api/internal/platform/catalog/service"
	"api/pkg/trustclient"
)

const (
	FolderTrustSubjectKind     = "user_folder"
	FolderNoteTrustSubjectKind = "user_folder_note"

	folderReportTimeout = 5 * time.Second
)

type FolderTrust interface {
	RatingTrust
	Report(ctx context.Context, req trustclient.ReportRequest) (reportID int64, err error)
}

type folderText struct {
	text, pointer string
}

func (c *Catalog) checkFolderText(ctx context.Context, uid int64, fields ...folderText) error {
	if c.FolderTrust == nil {
		return nil
	}
	for _, f := range fields {
		if f.text == "" {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, ratingCheckTimeout)
		decision, matched, err := c.FolderTrust.Check(cctx, trustclient.CheckRequest{Site: RatingTrustSite, Text: f.text, AuthorID: &uid})
		cancel()
		switch {
		case err != nil:
			slog.Warn("folder trust-check fail-open", "uid", uid, "pointer", f.pointer, "err", err)
		case decision == "deny":
			p := problem.New(problem.CodeValidationFailed, "", "", "the folder text contains content that cannot be published.")
			p.Errors = []problem.FieldError{{Pointer: f.pointer, Reason: problem.ReasonNotAllowedValue,
				Detail: "refused by the content check; nothing was written."}}
			return p
		case decision == "hold":
			slog.Info("folder text held by trust", "uid", uid, "pointer", f.pointer, "matched", matched)
		}
	}
	return nil
}

func folderTrustText(row model.CatalogUserFolder) string {
	if row.Description == "" {
		return "名称: " + row.Name
	}
	return "名称: " + row.Name + "\n简介: " + row.Description
}

func folderNoteSubject(folderID, workID int64) string {
	return strconv.FormatInt(folderID, 10) + ":" + strconv.FormatInt(workID, 10)
}

func (c *Catalog) scanFolder(row model.CatalogUserFolder) {
	if c.FolderTrust == nil || row.Visibility != model.FolderVisibilityPublic {
		return
	}
	c.scanFolderTexts(row.OwnerUID, []trustclient.ScanRequest{{
		Site: RatingTrustSite, SubjectKind: FolderTrustSubjectKind,
		SubjectID: strconv.FormatInt(row.ID, 10), Text: folderTrustText(row),
	}})
}

func (c *Catalog) scanFolderNotes(ownerUID int64, items []model.CatalogUserFolderItem) {
	if c.FolderTrust == nil || len(items) == 0 {
		return
	}
	reqs := make([]trustclient.ScanRequest, 0, len(items))
	for _, it := range items {
		if it.Note == "" {
			continue
		}
		reqs = append(reqs, trustclient.ScanRequest{
			Site: RatingTrustSite, SubjectKind: FolderNoteTrustSubjectKind,
			SubjectID: folderNoteSubject(it.FolderID, it.WorkID), Text: it.Note,
		})
	}
	c.scanFolderTexts(ownerUID, reqs)
}

func (c *Catalog) scanFolderTexts(ownerUID int64, reqs []trustclient.ScanRequest) {
	if len(reqs) == 0 {
		return
	}
	go func() {
		for _, req := range reqs {
			req.AuthorID = &ownerUID
			ctx, cancel := context.WithTimeout(context.Background(), ratingScanTimeout)
			if _, _, err := c.FolderTrust.Scan(ctx, req); err != nil {
				slog.Warn("folder trust-scan", "kind", req.SubjectKind, "subject_id", req.SubjectID, "err", err)
			}
			cancel()
		}
	}()
}

func (c *Catalog) ReportFolder(ctx context.Context, folderID int64, reason, note string, workID int64) error {
	if c == nil || c.Folders == nil || c.FolderTrust == nil {
		return problem.New(problem.CodeServiceUnavailable, "", "", "folder reports are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return err
	}
	folder, gerr := c.Folders.GetPublic(ctx, folderID)
	if gerr != nil {
		return folderErr(gerr)
	}
	req := trustclient.ReportRequest{
		Site: RatingTrustSite, SubjectKind: FolderTrustSubjectKind, SubjectID: strconv.FormatInt(folder.ID, 10),
		ReasonKey: reason, AuthorID: &folder.OwnerUID, ReporterID: uid,
	}
	snapshot := folderTrustText(*folder)
	if workID > 0 {
		item, ierr := c.Folders.PublicItem(ctx, folderID, workID)
		if ierr == catsvc.ErrFolderItemNotFound {
			p := problem.New(problem.CodeValidationFailed, "", "", "the folder does not hold this work.")
			p.Errors = []problem.FieldError{{Pointer: "/work_id", Reason: problem.ReasonNotAllowedValue, Detail: "not in the folder"}}
			return p
		}
		if ierr != nil {
			return folderErr(ierr)
		}
		if item.Note == "" {
			p := problem.New(problem.CodeValidationFailed, "", "", "this work carries no note in the folder; report the folder instead.")
			p.Errors = []problem.FieldError{{Pointer: "/work_id", Reason: problem.ReasonNotAllowedValue, Detail: "the item has no note"}}
			return p
		}
		req.SubjectKind, req.SubjectID = FolderNoteTrustSubjectKind, folderNoteSubject(folderID, workID)
		snapshot = item.Note
	}
	req.Snapshot = &snapshot
	if note != "" {
		req.Note = &note
	}
	rctx, cancel := context.WithTimeout(ctx, folderReportTimeout)
	defer cancel()
	if _, err := c.FolderTrust.Report(rctx, req); err != nil {
		var se *trustclient.StatusError
		if stderrors.As(err, &se) && se.Status == http.StatusTooManyRequests {
			return problem.New(problem.CodeRateLimited, "", "", "too many reports from this account; try again later.")
		}
		slog.Error("folder report", "folder_id", folderID, "work_id", workID, "err", err)
		return problem.New(problem.CodeServiceUnavailable, "", "", "the report could not be filed; try again later.")
	}
	return nil
}
