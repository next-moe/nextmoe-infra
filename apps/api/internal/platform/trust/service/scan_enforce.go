package service

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"api/internal/platform/trust/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func scanModeName(mode int16) string {
	if mode == model.ScanModeLive {
		return "live"
	}
	return "shadow"
}

const scanActorSystem int64 = 0

const scanReasonCode = "ai_scan_flagged"

const scanNoteExcerptRunes = 200

func (w *ScanWorker) enforceFlagged(tx *gorm.DB, r *model.TrustScanResult, v GatewayVerdict, pol ResolvedPolicy) error {
	itemID, created, err := w.openScanReviewItem(tx, r, v)
	if err != nil {
		return err
	}
	if !created {
		return nil
	}
	if !pol.AutoHideEnabled {
		return AppendAudit(tx, AuditEntry{
			Action: "scan_auto_queued",
			Site:   strptr(r.Site), SubjectKind: strptr(r.SubjectKind), SubjectID: strptr(r.SubjectID),
			ReasonCode: strptr(scanReasonCode),
		})
	}

	var kind model.TrustSubjectKind
	kerr := tx.Where("site = ? AND key = ?", r.Site, r.SubjectKind).Take(&kind).Error
	if kerr != nil && !errors.Is(kerr, gorm.ErrRecordNotFound) {
		return kerr
	}
	now := w.now()
	disp := model.TrustDisposition{
		ReviewItemID: itemID, Action: model.ActionHide, ActedBy: scanActorSystem,
		ReasonCode: scanReasonCode,
	}
	if kind.CallbackURL != nil && *kind.CallbackURL != "" {
		pending := model.CallbackStatusPending
		disp.CallbackStatus = &pending
		disp.NextAttemptAt = &now
	}
	if err := tx.Create(&disp).Error; err != nil {
		return err
	}
	return AppendAudit(tx, AuditEntry{
		Action: "scan_auto_actioned",
		Site:   strptr(r.Site), SubjectKind: strptr(r.SubjectKind), SubjectID: strptr(r.SubjectID),
		ReasonCode: strptr(scanReasonCode),
	})
}

func (w *ScanWorker) openScanReviewItem(tx *gorm.DB, r *model.TrustScanResult, v GatewayVerdict) (int64, bool, error) {
	note := scanContextNote(r, v)
	openStates := []int16{model.ReviewStatusPending, model.ReviewStatusClaimed}

	var open model.TrustReviewItem
	lerr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("site = ? AND subject_kind = ? AND subject_id = ? AND status IN ?",
			r.Site, r.SubjectKind, r.SubjectID, openStates).
		Limit(1).Take(&open).Error
	if lerr == nil {
		updates := map[string]any{}
		if v.Score != nil {
			updates["classifier_score"] = gorm.Expr("GREATEST(COALESCE(classifier_score, 0), ?)", *v.Score)
		}
		updates["context_note"] = gorm.Expr(
			"CASE WHEN context_note IS NULL OR context_note = '' THEN ? ELSE context_note END", note)
		if r.AuthorID != nil {
			updates["subject_author_id"] = gorm.Expr("COALESCE(subject_author_id, ?)", *r.AuthorID)
		}
		if reach := maxReach(open.SubjectReach, r.SubjectReach); reach != nil {
			updates["subject_reach"] = *reach
			updates["priority"] = repriceForReach(open.Priority, open.SubjectReach, reach)
		}
		if err := tx.Model(&model.TrustReviewItem{}).Where("id = ?", open.ID).Updates(updates).Error; err != nil {
			return 0, false, err
		}
		return open.ID, false, nil
	} else if !errors.Is(lerr, gorm.ErrRecordNotFound) {
		return 0, false, lerr
	}

	item := model.TrustReviewItem{
		Site: r.Site, SubjectKind: r.SubjectKind, SubjectID: r.SubjectID,
		SubjectAuthorID: r.AuthorID, Source: model.ReviewSourceAIText, ClassifierScore: v.Score,
		ContextNote: &note, SubjectReach: r.SubjectReach,
		Priority: rankPriority(scanPriority(v.Score), r.SubjectReach),
		Status:   model.ReviewStatusPending,
	}
	res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&item)
	if res.Error != nil {
		return 0, false, res.Error
	}
	if res.RowsAffected == 0 {
		if err := tx.Where("site = ? AND subject_kind = ? AND subject_id = ? AND status IN ?",
			r.Site, r.SubjectKind, r.SubjectID, openStates).
			Limit(1).Take(&item).Error; err != nil {
			return 0, false, err
		}
		if err := adoptSubjectAuthor(tx, item.ID, r.AuthorID); err != nil {
			return 0, false, err
		}
		return item.ID, false, nil
	}
	return item.ID, true, nil
}

func (w *ScanWorker) maybeSampleClean(tx *gorm.DB, r *model.TrustScanResult, v GatewayVerdict, pol ResolvedPolicy) error {
	if pol.SampleRate <= 0 || w.rand() >= pol.SampleRate {
		return nil
	}

	var open int64
	if err := tx.Model(&model.TrustReviewItem{}).
		Where("site = ? AND subject_kind = ? AND subject_id = ? AND status IN ?",
			r.Site, r.SubjectKind, r.SubjectID,
			[]int16{model.ReviewStatusPending, model.ReviewStatusClaimed}).
		Count(&open).Error; err != nil {
		return err
	}
	if open > 0 {
		return nil
	}

	note := scanSampleNote(r, v)
	item := model.TrustReviewItem{
		Site: r.Site, SubjectKind: r.SubjectKind, SubjectID: r.SubjectID,
		SubjectAuthorID: r.AuthorID, Source: model.ReviewSourceAISample, ClassifierScore: v.Score,
		ContextNote: &note, SubjectReach: r.SubjectReach,
		Priority: scanSamplePriority, Status: model.ReviewStatusPending,
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&item).Error; err != nil {
		return err
	}
	slog.Info("trust scan sampled clean verdict",
		"scan_id", r.ID, "site", r.Site, "subject_kind", r.SubjectKind, "subject_id", r.SubjectID)
	return AppendAudit(tx, AuditEntry{
		Action: "scan_sampled_clean",
		Site:   strptr(r.Site), SubjectKind: strptr(r.SubjectKind), SubjectID: strptr(r.SubjectID),
	})
}

func scanSampleNote(r *model.TrustScanResult, v GatewayVerdict) string {
	var b strings.Builder
	b.WriteString("[calibration] the classifier cleared this")
	if v.Score != nil {
		fmt.Fprintf(&b, " (score=%.3f)", *v.Score)
	}
	b.WriteString(" — it was drawn at random to check that verdict. ")
	b.WriteString("Dismiss if it is genuinely fine; action it if we missed something.\n")
	b.WriteString(excerptRunes(r.ContentText, scanNoteExcerptRunes))
	return b.String()
}

func scanPriority(score *float32) float32 {
	if score == nil {
		return 1
	}
	return *score * 5
}

func scanContextNote(r *model.TrustScanResult, v GatewayVerdict) string {
	var b strings.Builder
	b.WriteString("[ai]")
	if len(v.Categories) > 0 {
		fmt.Fprintf(&b, " %s", strings.Join(v.Categories, ", "))
	}
	if v.Score != nil {
		fmt.Fprintf(&b, " score=%.3f", *v.Score)
	}
	if r.AuthorID != nil {
		fmt.Fprintf(&b, " by user %d", *r.AuthorID)
	}
	b.WriteString(": ")
	b.WriteString(excerptRunes(r.ContentText, scanNoteExcerptRunes))
	return b.String()
}

func excerptRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i] + "…"
		}
		count++
	}
	return s
}
