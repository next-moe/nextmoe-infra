package handler

import (
	"api/internal/platform/trust/dto"
	"api/internal/platform/trust/model"
	"api/internal/platform/trust/service"
)

const maxEnsureKinds = 50

func toEnsureItems(items []dto.EnsureSubjectKindItem) []service.EnsureSubjectKindItem {
	out := make([]service.EnsureSubjectKindItem, len(items))
	for i, it := range items {
		out[i] = service.EnsureSubjectKindItem{
			Key: it.Key, CallbackURL: it.CallbackURL,
			CallbackSecret: it.CallbackSecret, NotifyOnDismiss: it.NotifyOnDismiss,
		}
	}
	return out
}

func toEnsureResultViews(rs []service.EnsureSubjectKindResult) []dto.EnsureSubjectKindResultView {
	out := make([]dto.EnsureSubjectKindResultView, len(rs))
	for i, r := range rs {
		out[i] = dto.EnsureSubjectKindResultView{Key: r.Key, Result: string(r.Outcome)}
	}
	return out
}

func toSubjectKindView(k model.TrustSubjectKind) dto.SubjectKindView {
	return dto.SubjectKindView{
		ID: k.ID, Site: k.Site, Key: k.Key, CallbackURL: k.CallbackURL,
		HasSecret:    k.CallbackSecret != nil && *k.CallbackSecret != "",
		IsDeprecated: k.IsDeprecated, NotifyOnDismiss: k.NotifyOnDismiss, CreatedAt: k.CreatedAt,
	}
}

func toSubjectKindViews(ks []model.TrustSubjectKind) []dto.SubjectKindView {
	out := make([]dto.SubjectKindView, len(ks))
	for i, k := range ks {
		out[i] = toSubjectKindView(k)
	}
	return out
}

func toReasonView(r model.TrustReportReason) dto.ReasonView {
	return dto.ReasonView{
		ID: r.ID, Key: r.Key, NameCN: r.NameCN, Site: r.Site,
		Severity: r.Severity, IsDeprecated: r.IsDeprecated,
	}
}

func toReasonViews(rs []model.TrustReportReason) []dto.ReasonView {
	out := make([]dto.ReasonView, len(rs))
	for i, r := range rs {
		out[i] = toReasonView(r)
	}
	return out
}

func toReviewItemView(it model.TrustReviewItem) dto.ReviewItemView {
	return dto.ReviewItemView{
		ID: it.ID, Site: it.Site, SubjectKind: it.SubjectKind, SubjectID: it.SubjectID,
		Source: it.Source, Severity: it.Severity, ClassifierScore: it.ClassifierScore,
		ReportWeightSum: it.ReportWeightSum, SubjectReach: it.SubjectReach, SubjectAuthorID: it.SubjectAuthorID,
		Priority: it.Priority, ContextNote: it.ContextNote, Status: it.Status,
		ClaimedBy: it.ClaimedBy, ClaimedAt: it.ClaimedAt,
		DecidedBy: it.DecidedBy, DecidedAt: it.DecidedAt, CreatedAt: it.CreatedAt,
	}
}

func toReviewItemViews(its []model.TrustReviewItem) []dto.ReviewItemView {
	out := make([]dto.ReviewItemView, len(its))
	for i, it := range its {
		out[i] = toReviewItemView(it)
	}
	return out
}

func toReportView(r model.TrustReport) dto.ReportView {
	return dto.ReportView{
		ID: r.ID, Site: r.Site, SubjectKind: r.SubjectKind, SubjectID: r.SubjectID,
		ReporterID: r.ReporterID, ReasonID: r.ReasonID, Note: r.Note,
		SubjectSnapshot: r.SubjectSnapshot, SubjectURL: r.SubjectURL,
		SubjectAuthorID: r.SubjectAuthorID, Weight: r.Weight,
		ReviewItemID: r.ReviewItemID, Status: r.Status, CreatedAt: r.CreatedAt,
	}
}

func toReportViews(rs []model.TrustReport) []dto.ReportView {
	out := make([]dto.ReportView, len(rs))
	for i, r := range rs {
		out[i] = toReportView(r)
	}
	return out
}

func toDispositionView(d model.TrustDisposition) dto.DispositionView {
	return dto.DispositionView{
		ID: d.ID, ReviewItemID: d.ReviewItemID, Action: d.Action, ActedBy: d.ActedBy,
		ReasonCode: d.ReasonCode, Statement: d.Statement, CallbackStatus: d.CallbackStatus,
		CallbackAttempts: d.CallbackAttempts, NextAttemptAt: d.NextAttemptAt,
		DeliveredAt: d.DeliveredAt, CreatedAt: d.CreatedAt,
	}
}

func toDispositionViews(ds []model.TrustDisposition) []dto.DispositionView {
	out := make([]dto.DispositionView, len(ds))
	for i, d := range ds {
		out[i] = toDispositionView(d)
	}
	return out
}
