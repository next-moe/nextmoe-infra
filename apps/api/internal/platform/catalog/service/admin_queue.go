package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

type AdminQueueService struct {
	db    *gorm.DB
	merge *MergeService
	link  *LinkService
}

func NewAdminQueueService(db *gorm.DB, merge *MergeService) *AdminQueueService {
	return &AdminQueueService{db: db, merge: merge, link: NewLinkService(db)}
}

func (s *AdminQueueService) DetachName(ctx context.Context, creditNameID int64, actorID *int64) error {
	return s.link.DetachName(ctx, creditNameID, actorID)
}

var ErrExactTaken = fmt.Errorf("catalog: external identity is exact-linked to another entity")

var ErrCuratedRef = errors.New("ref is human-linked (curated); remove it through the editor")

const matchedByCurated = "curated"

// NeedsManual belongs here: it is the status the machine lanes park a pair in
// precisely because a human has to decide it. Leaving it out made every such
// candidate permanently undecidable in the console — a queue with no exit.
var decidableCandidateStatuses = []int16{
	model.CandidateStatusPending,
	model.CandidateStatusDeferred,
	model.CandidateStatusNeedsManual,
}

type EntitySummary struct {
	ID          int64  `json:"id"`
	DisplayName string `json:"display_name"`
	CreditCount *int64 `json:"credit_count,omitempty"`
	SourceID    *int16 `json:"source_id,omitempty"`
	WorkStatus  *int16 `json:"work_status,omitempty"`

	MediumID      *int16             `json:"medium_id,omitempty"`
	OLang         string             `json:"olang,omitempty"`
	ContentRating *int16             `json:"content_rating,omitempty"`
	Site          string             `json:"site,omitempty"`
	ClaimState    *int16             `json:"claim_state,omitempty"`
	ReleaseYear   *int16             `json:"release_year,omitempty"`
	Refs          []EntityRefSummary `json:"refs,omitempty"`
}

func entitySummary(db *gorm.DB, entityType int16, id int64) EntitySummary {
	table, ok := entityTableName(entityType)
	if !ok {
		return EntitySummary{ID: id}
	}
	if entityType == model.EntityTypeWork {
		var row struct {
			DisplayName string
			Status      int16
		}
		if res := db.Raw(`SELECT display_name, status FROM catalog_work WHERE id = ?`, id).Scan(&row); res.Error != nil || res.RowsAffected == 0 {
			return EntitySummary{ID: id}
		}
		return EntitySummary{ID: id, DisplayName: row.DisplayName, WorkStatus: &row.Status}
	}
	column := "display_name"
	if entityType == model.EntityTypeCreditName {
		column = "name"
	}
	var name string
	_ = db.Raw(`SELECT `+column+` FROM `+table+` WHERE id = ?`, id).Scan(&name).Error
	return EntitySummary{ID: id, DisplayName: name}
}

type CandidateFilters struct {
	Status     *int16
	EntityType *int16
	Reason     *int16
	Page       int
	Limit      int
}

type CandidateItem struct {
	model.CatalogMatchCandidate
	A EntitySummary `json:"a"`
	B EntitySummary `json:"b"`
}

func (s *AdminQueueService) ListCandidates(ctx context.Context, f CandidateFilters) ([]CandidateItem, int64, error) {
	page, limit := normalizePage(f.Page, f.Limit)
	q := s.db.WithContext(ctx).Model(&model.CatalogMatchCandidate{})
	if f.Status != nil {
		q = q.Where("status = ?", *f.Status)
	}
	if f.EntityType != nil {
		q = q.Where("entity_type = ?", *f.EntityType)
	}
	if f.Reason != nil {
		q = q.Where("reason = ?", *f.Reason)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.CatalogMatchCandidate
	if err := q.Order("created_at, entity_type, a_id, b_id").
		Offset((page - 1) * limit).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	items := make([]CandidateItem, len(rows))
	for i, row := range rows {
		items[i] = CandidateItem{
			CatalogMatchCandidate: row,
			A:                     entitySummary(s.db.WithContext(ctx).Unscoped(), row.EntityType, row.AID),
			B:                     entitySummary(s.db.WithContext(ctx).Unscoped(), row.EntityType, row.BID),
		}
	}
	s.enrichCreditNameContext(ctx, items)
	s.enrichWorkContext(ctx, items)
	return items, total, nil
}

func (s *AdminQueueService) enrichCreditNameContext(ctx context.Context, items []CandidateItem) {
	var ids []int64
	for _, it := range items {
		if it.EntityType == model.EntityTypeCreditName {
			ids = append(ids, it.AID, it.BID)
		}
	}
	if len(ids) == 0 {
		return
	}
	db := s.db.WithContext(ctx)

	credits := map[int64]int64{}
	var creditRows []struct {
		ID    int64 `gorm:"column:credit_name_id"`
		Count int64 `gorm:"column:count"`
	}
	if err := db.Raw(`SELECT credit_name_id, count(*) AS count FROM catalog_credit
		WHERE credit_name_id IN ? GROUP BY credit_name_id`, ids).Scan(&creditRows).Error; err == nil {
		for _, r := range creditRows {
			credits[r.ID] = r.Count
		}
	}

	sources := map[int64]int16{}
	var sourceRows []struct {
		ID     int64 `gorm:"column:entity_id"`
		Source int16 `gorm:"column:source_id"`
	}
	if err := db.Raw(`SELECT entity_id, min(source_id) AS source_id FROM catalog_external_ref
		WHERE entity_type = ? AND link_kind = ? AND entity_id IN ? GROUP BY entity_id`,
		model.EntityTypeCreditName, model.LinkKindExact, ids).Scan(&sourceRows).Error; err == nil {
		for _, r := range sourceRows {
			sources[r.ID] = r.Source
		}
	}

	fill := func(sum *EntitySummary) {
		c := credits[sum.ID]
		sum.CreditCount = &c
		if src, ok := sources[sum.ID]; ok {
			sum.SourceID = &src
		}
	}
	for i := range items {
		if items[i].EntityType == model.EntityTypeCreditName {
			fill(&items[i].A)
			fill(&items[i].B)
		}
	}
}

type CandidateDecision struct {
	EntityType         int16
	AID, BID           int64
	Action             string
	SourceID, TargetID int64
	Note               string
	DecidedBy          int64
}

type CandidateOutcome struct {
	Proposal *model.CatalogMergeProposal
	Link     *PersonLinkResult
	Released []int64
}

func (s *AdminQueueService) DecideCandidate(ctx context.Context, d CandidateDecision) (*CandidateOutcome, error) {
	switch d.Action {
	case "accept", "reject", "defer":
	default:
		return nil, fmt.Errorf("%w: unknown action %q", ErrProposalState, d.Action)
	}

	var cand model.CatalogMatchCandidate
	err := s.db.WithContext(ctx).Raw(`SELECT * FROM catalog_match_candidate
	                WHERE entity_type = ? AND a_id = ? AND b_id = ?`,
		d.EntityType, d.AID, d.BID).Scan(&cand).Error
	if err != nil {
		return nil, err
	}
	if cand.AID == 0 {
		return nil, fmt.Errorf("%w: candidate (%d, %d, %d)", ErrNotFound, d.EntityType, d.AID, d.BID)
	}
	if !slices.Contains(decidableCandidateStatuses, cand.Status) {
		return nil, fmt.Errorf("%w: candidate already decided (status %d)", ErrProposalState, cand.Status)
	}

	if d.Action == "accept" && d.EntityType == model.EntityTypeCreditName {
		return s.acceptAsPersonLink(ctx, d)
	}
	if d.Action == "reject" && d.EntityType == model.EntityTypeWork {
		return s.rejectWorkCandidate(ctx, d)
	}
	if d.Action == "defer" && d.EntityType == model.EntityTypeWork {
		return s.deferWorkCandidate(ctx, d)
	}

	status := model.CandidateStatusRejected
	var proposal *model.CatalogMergeProposal
	switch d.Action {
	case "accept":
		status = model.CandidateStatusAccepted
		if !(d.SourceID == d.AID && d.TargetID == d.BID) && !(d.SourceID == d.BID && d.TargetID == d.AID) {
			return nil, fmt.Errorf("%w: source/target must be the candidate pair", ErrProposalState)
		}
		proposal, err = s.merge.ProposeMerge(ctx, d.EntityType, d.SourceID, d.TargetID, d.DecidedBy, d.Note)
		if err != nil {
			return nil, err
		}
	case "defer":
		status = model.CandidateStatusDeferred
	}

	rows, err := s.flipCandidate(s.db.WithContext(ctx), d, status)
	if err != nil {
		return nil, err
	}
	if rows == 0 {
		return &CandidateOutcome{Proposal: proposal}, fmt.Errorf("%w: candidate decided concurrently", ErrProposalState)
	}
	return &CandidateOutcome{Proposal: proposal}, nil
}

func (s *AdminQueueService) acceptAsPersonLink(ctx context.Context, d CandidateDecision) (*CandidateOutcome, error) {
	var link PersonLinkResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		r, err := s.link.linkCreditsTx(tx, d.AID, d.BID, &d.DecidedBy)
		if err != nil {
			return err
		}
		link = r
		status := model.CandidateStatusAccepted
		if r.NeedsManual {
			status = model.CandidateStatusNeedsManual
		}
		rows, err := s.flipCandidate(tx, d, status)
		if err != nil {
			return err
		}
		if rows == 0 {
			return fmt.Errorf("%w: candidate decided concurrently", ErrProposalState)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &CandidateOutcome{Link: &link}, nil
}

func (s *AdminQueueService) flipCandidate(db *gorm.DB, d CandidateDecision, status int16) (int64, error) {
	res := db.Model(&model.CatalogMatchCandidate{}).
		Where("entity_type = ? AND a_id = ? AND b_id = ? AND status IN ?",
			d.EntityType, d.AID, d.BID, decidableCandidateStatuses).
		Updates(map[string]any{"status": status, "decided_by": d.DecidedBy, "decided_at": time.Now()})
	return res.RowsAffected, res.Error
}

type ProposalFilters struct {
	Status     *int16
	EntityType *int16
	Page       int
	Limit      int
}

func (s *AdminQueueService) GetProposal(ctx context.Context, id int64) (*model.CatalogMergeProposal, error) {
	var row model.CatalogMergeProposal
	err := s.db.WithContext(ctx).First(&row, id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

type ProposalItem struct {
	model.CatalogMergeProposal
	Source EntitySummary `json:"source"`
	Target EntitySummary `json:"target"`
}

func (s *AdminQueueService) ListProposals(ctx context.Context, f ProposalFilters) ([]ProposalItem, int64, error) {
	page, limit := normalizePage(f.Page, f.Limit)
	q := s.db.WithContext(ctx).Model(&model.CatalogMergeProposal{})
	if f.Status != nil {
		q = q.Where("status = ?", *f.Status)
	}
	if f.EntityType != nil {
		q = q.Where("entity_type = ?", *f.EntityType)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.CatalogMergeProposal
	if err := q.Order("proposed_at, id").
		Offset((page - 1) * limit).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	items := make([]ProposalItem, len(rows))
	for i, row := range rows {
		items[i] = ProposalItem{
			CatalogMergeProposal: row,
			Source:               entitySummary(s.db.WithContext(ctx).Unscoped(), row.EntityType, row.SourceEntityID),
			Target:               entitySummary(s.db.WithContext(ctx).Unscoped(), row.EntityType, row.TargetEntityID),
		}
	}
	return items, total, nil
}

type RefFilters struct {
	SourceID   *int16
	EntityType *int16
	Page       int
	Limit      int
}

// ExactSlotFreeSQL separates a probable ref a reviewer can act on from one that
// no action will ever resolve. ConfirmRef promotes probable to exact, and the
// partial unique uq_catalog_external_ref_exact allows a single exact holder per
// (source_id, external_id, entity_type) — so once another entity holds that
// slot, confirming returns ErrExactTaken however often it is retried.
//
// Measured on prod 2026-09-14: the queue held 20,156 rows and only 9,263 of
// them could ever be acted on. The 10,893 removed are overwhelmingly bundle
// releases — entity_type 6 alone drops from 11,160 to 386 — because one VNDB
// collection release legitimately spans up to 16 works, the importer fans it
// into that many catalog_release rows, and only one of them can hold exact.
// They are correct data that simply has no confirm action.
const ExactSlotFreeSQL = `NOT EXISTS (
	SELECT 1 FROM catalog_external_ref taken
	WHERE taken.entity_type = catalog_external_ref.entity_type
	  AND taken.source_id   = catalog_external_ref.source_id
	  AND taken.external_id = catalog_external_ref.external_id
	  AND taken.link_kind   = 0
	  AND taken.entity_id  <> catalog_external_ref.entity_id
)`

type ProbableRefItem struct {
	model.CatalogExternalRef
	Entity EntitySummary `json:"entity"`
}

func (s *AdminQueueService) ListProbableRefs(ctx context.Context, f RefFilters) ([]ProbableRefItem, int64, error) {
	page, limit := normalizePage(f.Page, f.Limit)
	q := s.db.WithContext(ctx).Model(&model.CatalogExternalRef{}).
		Where("link_kind = ? AND verified_at IS NULL", model.LinkKindProbable).
		Where(ExactSlotFreeSQL)
	if f.SourceID != nil {
		q = q.Where("source_id = ?", *f.SourceID)
	}
	if f.EntityType != nil {
		q = q.Where("entity_type = ?", *f.EntityType)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.CatalogExternalRef
	if err := q.Order("created_at, entity_type, entity_id, source_id, external_id").
		Offset((page - 1) * limit).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	items := make([]ProbableRefItem, len(rows))
	for i, row := range rows {
		items[i] = ProbableRefItem{
			CatalogExternalRef: row,
			Entity:             entitySummary(s.db.WithContext(ctx).Unscoped(), row.EntityType, row.EntityID),
		}
	}
	return items, total, nil
}

type RefKey struct {
	EntityType int16
	EntityID   int64
	SourceID   int16
	ExternalID string
}

func (s *AdminQueueService) ConfirmRef(ctx context.Context, key RefKey, verifiedBy int64) error {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ref model.CatalogExternalRef
		err := tx.Raw(`SELECT * FROM catalog_external_ref
		                WHERE entity_type = ? AND entity_id = ? AND source_id = ? AND external_id = ? FOR UPDATE`,
			key.EntityType, key.EntityID, key.SourceID, key.ExternalID).Scan(&ref).Error
		if err != nil {
			return err
		}
		if ref.ExternalID == "" {
			return fmt.Errorf("%w: external ref", ErrNotFound)
		}
		if ref.LinkKind != model.LinkKindProbable {
			return fmt.Errorf("%w: ref is not probable (kind %d)", ErrProposalState, ref.LinkKind)
		}
		now := time.Now()
		res := tx.Model(&model.CatalogExternalRef{}).
			Where("entity_type = ? AND entity_id = ? AND source_id = ? AND external_id = ?",
				key.EntityType, key.EntityID, key.SourceID, key.ExternalID).
			Updates(map[string]any{"link_kind": model.LinkKindExact, "verified_by": verifiedBy, "verified_at": now})
		if res.Error != nil || res.RowsAffected == 0 {
			return res.Error
		}
		return repository.TouchRefHosts(ctx, tx, key.EntityType, key.EntityID)
	})
	if isUniqueViolation(err, "uq_catalog_external_ref_exact") {
		var holder int64
		_ = s.db.WithContext(ctx).Raw(`SELECT entity_id FROM catalog_external_ref
		     WHERE source_id = ? AND external_id = ? AND entity_type = ? AND link_kind = ?`,
			key.SourceID, key.ExternalID, key.EntityType, model.LinkKindExact).Scan(&holder).Error
		return fmt.Errorf("%w: held by entity %d", ErrExactTaken, holder)
	}
	return err
}

// VerifyRefAsRelated records that a probable ref was judged correct without
// promoting it to exact. ConfirmRef is the only other exit and it always
// promotes, so a ref whose exact slot another entity legitimately holds had no
// exit at all: a VNDB collection release fans out across up to 16 works and
// only one catalog_release row can hold the slot. On 2026-09-14 that was 10,684
// of the 16,654 rows in the queue, every one already judged chain-verified at
// confidence >= 0.90 and retried, and refused, on every nightly pass.
//
// verified_at on a probable row is what ends the retry: ExactSlotFreeSQL keeps
// those rows out of the reviewer's queue but not out of the apply set, and no
// reader treats verified_at alone as a claim of exactness — every one pairs it
// with an explicit link_kind.
func (s *AdminQueueService) VerifyRefAsRelated(ctx context.Context, key RefKey, verifiedBy int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ref model.CatalogExternalRef
		err := tx.Raw(`SELECT * FROM catalog_external_ref
		                WHERE entity_type = ? AND entity_id = ? AND source_id = ? AND external_id = ? FOR UPDATE`,
			key.EntityType, key.EntityID, key.SourceID, key.ExternalID).Scan(&ref).Error
		if err != nil {
			return err
		}
		if ref.ExternalID == "" {
			return fmt.Errorf("%w: external ref", ErrNotFound)
		}
		if ref.LinkKind != model.LinkKindProbable {
			return fmt.Errorf("%w: ref is not probable (kind %d)", ErrProposalState, ref.LinkKind)
		}
		if ref.VerifiedAt != nil {
			return fmt.Errorf("%w: ref is already verified", ErrProposalState)
		}
		now := time.Now()
		res := tx.Model(&model.CatalogExternalRef{}).
			Where("entity_type = ? AND entity_id = ? AND source_id = ? AND external_id = ? AND verified_at IS NULL",
				key.EntityType, key.EntityID, key.SourceID, key.ExternalID).
			Updates(map[string]any{"verified_by": verifiedBy, "verified_at": now})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("%w: ref verified concurrently", ErrProposalState)
		}
		return repository.TouchRefHosts(ctx, tx, key.EntityType, key.EntityID)
	})
}

func (s *AdminQueueService) RejectRef(ctx context.Context, key RefKey, reason string, rejectedBy int64) error {
	if reason == "" {
		return fmt.Errorf("%w: rejection reason is required", ErrProposalState)
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var matchedBy string
		err := tx.Raw(`SELECT matched_by FROM catalog_external_ref
		                WHERE entity_type = ? AND entity_id = ? AND source_id = ? AND external_id = ? FOR UPDATE`,
			key.EntityType, key.EntityID, key.SourceID, key.ExternalID).Scan(&matchedBy).Error
		if err != nil {
			return err
		}
		if matchedBy == "" {
			return fmt.Errorf("%w: external ref", ErrNotFound)
		}
		if matchedBy == matchedByCurated {
			return fmt.Errorf("%w: %w", ErrProposalState, ErrCuratedRef)
		}
		res := tx.Exec(`DELETE FROM catalog_external_ref
		                 WHERE entity_type = ? AND entity_id = ? AND source_id = ? AND external_id = ?`,
			key.EntityType, key.EntityID, key.SourceID, key.ExternalID)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("%w: external ref", ErrNotFound)
		}
		if err := tx.Create(&model.CatalogMatchRejection{
			EntityType: key.EntityType,
			EntityID:   key.EntityID,
			SourceID:   key.SourceID,
			ExternalID: key.ExternalID,
			Reason:     reason,
			RejectedBy: &rejectedBy,
		}).Error; err != nil {
			return err
		}
		return repository.TouchRefHosts(ctx, tx, key.EntityType, key.EntityID)
	})
}

func normalizePage(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	return page, limit
}
