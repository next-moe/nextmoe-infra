package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/editorigin"
	"api/internal/platform/settings/keys"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var (
	ErrSplitFamily   = errors.New("catalog split: only works can be split")
	ErrSplitPending  = errors.New("catalog split: the work already has an open or approved split request")
	ErrSplitReleases = errors.New("catalog split: the release selection is not valid")
	ErrSplitRef      = errors.New("catalog split: a source entry cannot be promoted")
	ErrSplitDraft    = errors.New("catalog split: the new work's name, language or titles are not valid")
	ErrSplitUndo     = errors.New("catalog split: this split can no longer be undone")
)

const (
	maxSplitTitles    = 32
	maxSplitTitleLen  = 512
	maxSplitPromotion = 8
)

var olangPattern = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z]{2,4})?$`)

type SplitInput struct {
	WorkID      int64
	ReleaseIDs  []int64
	DisplayName string
	OLang       string
	Titles      []model.SplitTitle
	PromoteRefs []model.SplitRef
	Note        string
}

type SplitService struct {
	db      *gorm.DB
	resolve *ResolveService
	merges  *MergeService
}

func NewSplitService(db *gorm.DB, resolve *ResolveService, merges *MergeService) *SplitService {
	return &SplitService{db: db, resolve: resolve, merges: merges}
}

type SplitRequestFilter struct {
	ProposedBy *int64
	Status     *int16
	EntityID   *int64
	OriginSite string
	BeforeID   int64
	Limit      int
}

func (s *SplitService) Request(ctx context.Context, in SplitInput, proposedBy int64) (*model.CatalogSplitRequest, error) {
	if s.resolve != nil {
		current, moved, err := s.resolve.Resolve(ctx, model.EntityTypeWork, in.WorkID)
		if err != nil {
			return nil, err
		}
		if moved {
			return nil, fmt.Errorf("%w: work %d was merged into %d", ErrEntityNotLive, in.WorkID, current)
		}
	}
	in = normalizeSplitInput(in)
	db := s.db.WithContext(ctx)
	if err := assertEntityAlive(db, model.EntityTypeWork, in.WorkID); err != nil {
		return nil, err
	}
	if err := validateSplit(db, in); err != nil {
		return nil, err
	}
	r := &model.CatalogSplitRequest{
		EntityType: model.EntityTypeWork,
		EntityID:   in.WorkID,
		Status:     model.ProposalStatusOpen,
		ProposedBy: proposedBy,
		Note:       in.Note,
		OriginSite: editorigin.Ptr(ctx),
	}
	if err := setSplitDraft(r, in); err != nil {
		return nil, err
	}
	if err := db.Create(r).Error; err != nil {
		if isUniqueViolation(err, "uq_catalog_split_request_pending") {
			return nil, ErrSplitPending
		}
		return nil, err
	}
	return r, nil
}

// Amend lets a reviewer change the selection and the draft before approval
// or during the cooling-off.
func (s *SplitService) Amend(ctx context.Context, id, by int64, in SplitInput) error {
	return s.transition(ctx, id, []int16{model.ProposalStatusOpen, model.ProposalStatusApproved}, func(tx *gorm.DB, r *model.CatalogSplitRequest) error {
		in.WorkID = r.EntityID
		in = normalizeSplitInput(in)
		if err := validateSplit(tx, in); err != nil {
			return err
		}
		if err := setSplitDraft(r, in); err != nil {
			return err
		}
		now := time.Now()
		return tx.Model(r).Updates(map[string]any{
			"release_ids": r.ReleaseIDs, "display_name": r.DisplayName, "olang": r.OLang,
			"titles": r.Titles, "promote_refs": r.PromoteRefs, "amended_by": by, "amended_at": now,
		}).Error
	})
}

func (s *SplitService) List(ctx context.Context, f SplitRequestFilter) ([]model.CatalogSplitRequest, error) {
	q := s.db.WithContext(ctx).Model(&model.CatalogSplitRequest{})
	if f.ProposedBy != nil {
		q = q.Where("proposed_by = ?", *f.ProposedBy)
	}
	if f.Status != nil {
		q = q.Where("status = ?", *f.Status)
	}
	if f.EntityID != nil {
		q = q.Where("entity_id = ?", *f.EntityID)
	}
	if f.OriginSite != "" {
		q = q.Where("origin_site = ?", f.OriginSite)
	}
	if f.BeforeID > 0 {
		q = q.Where("id < ?", f.BeforeID)
	}
	limit := f.Limit
	if limit <= 0 || limit > 101 {
		limit = 50
	}
	var rows []model.CatalogSplitRequest
	err := q.Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (s *SplitService) Get(ctx context.Context, id int64) (*model.CatalogSplitRequest, error) {
	var r model.CatalogSplitRequest
	err := s.db.WithContext(ctx).First(&r, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: split request %d", ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *SplitService) CountSince(ctx context.Context, proposedBy int64, since time.Time) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&model.CatalogSplitRequest{}).
		Where("proposed_by = ? AND proposed_at >= ?", proposedBy, since).Count(&n).Error
	return n, err
}

func (s *SplitService) Approve(ctx context.Context, id, by int64) error {
	return s.transition(ctx, id, []int16{model.ProposalStatusOpen}, func(tx *gorm.DB, r *model.CatalogSplitRequest) error {
		if err := assertEntityAlive(tx, model.EntityTypeWork, r.EntityID); err != nil {
			return err
		}
		in, err := splitInputOf(r)
		if err != nil {
			return err
		}
		if err := validateSplit(tx, in); err != nil {
			return err
		}
		now := time.Now()
		after := now.Add(time.Duration(keys.CatalogMergeCoolingOffHours.Get()) * time.Hour)
		return tx.Model(r).Updates(map[string]any{
			"status": model.ProposalStatusApproved, "execute_after": after,
			"decided_by": by, "decided_at": now,
		}).Error
	})
}

func (s *SplitService) Reject(ctx context.Context, id, by int64, reason string) error {
	return s.transition(ctx, id, []int16{model.ProposalStatusOpen, model.ProposalStatusApproved}, func(tx *gorm.DB, r *model.CatalogSplitRequest) error {
		return tx.Model(r).Updates(map[string]any{
			"status": model.ProposalStatusRejected, "decided_by": by, "decided_at": time.Now(),
			"decision_note": reason,
		}).Error
	})
}

func (s *SplitService) Withdraw(ctx context.Context, id int64) error {
	return s.transition(ctx, id, []int16{model.ProposalStatusOpen}, func(tx *gorm.DB, r *model.CatalogSplitRequest) error {
		return tx.Model(r).Update("status", model.ProposalStatusWithdrawn).Error
	})
}

// SplitExecution records every row a split created or changed, so undo can
// put each back exactly.
type SplitExecution struct {
	Releases   []int64           `json:"releases"`
	Tombstones []int64           `json:"tombstones"`
	Titles     []int64           `json:"titles"`
	Credits    []int64           `json:"credits"`
	Refs       []json.RawMessage `json:"refs"`
	Rejections []model.SplitRef  `json:"rejections"`
	Candidate  json.RawMessage   `json:"candidate,omitempty"`
}

// Execute creates the new work and moves the chosen releases to it. closed
// reports a request it closed instead because the work left the population.
func (s *SplitService) Execute(ctx context.Context, id int64, by *int64) (closed bool, err error) {
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		r, err := lockSplitRequest(tx, id)
		if err != nil {
			return err
		}
		if r.Status != model.ProposalStatusApproved {
			return fmt.Errorf("%w: split request %d is in status %d, want approved", ErrProposalState, id, r.Status)
		}
		if r.ExecuteAfter == nil || time.Now().Before(*r.ExecuteAfter) {
			return fmt.Errorf("%w: split request %d executable after %v", ErrCoolingOff, id, r.ExecuteAfter)
		}
		w, err := lockWork(tx, r.EntityID)
		if err != nil {
			return err
		}
		if w == nil || w.DeletedAt != nil {
			closed = true
			return tx.Model(r).Updates(map[string]any{
				"status": model.ProposalStatusRejected, "decided_at": time.Now(),
				"decision_note": "作品已被合并或删除，此请求随之关闭",
			}).Error
		}
		in, err := splitInputOf(r)
		if err != nil {
			return err
		}
		if err := validateSplit(tx, in); err != nil {
			return err
		}
		exec, newID, err := runSplit(ctx, tx, r, in, by)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(exec)
		if err != nil {
			return err
		}
		now := time.Now()
		return tx.Model(r).Updates(map[string]any{
			"status": model.ProposalStatusExecuted, "executed_at": now, "executed_by": by,
			"new_work_id": newID, "execution": datatypes.JSON(raw), "changed_at": now,
		}).Error
	})
	return closed, err
}

func runSplit(ctx context.Context, tx *gorm.DB, r *model.CatalogSplitRequest, in SplitInput, by *int64) (*SplitExecution, int64, error) {
	var orig model.CatalogWork
	if err := tx.First(&orig, r.EntityID).Error; err != nil {
		return nil, 0, err
	}
	origSnap, err := takeSnapshot(tx, model.EntityTypeWork, orig.ID)
	if err != nil {
		return nil, 0, fmt.Errorf("snapshot work: %w", err)
	}
	nw := model.CatalogWork{
		MediumID:        orig.MediumID,
		OLang:           in.OLang,
		DisplayName:     in.DisplayName,
		Status:          model.WorkStatusLive,
		ContentRating:   orig.ContentRating,
		DisplayNSFW:     orig.DisplayNSFW,
		Extra:           []byte(`{}`),
		FieldProvenance: []byte(`{}`),
	}
	if err := tx.Create(&nw).Error; err != nil {
		return nil, 0, fmt.Errorf("create work: %w", err)
	}
	if err := tx.Model(&nw).Update("product_work_id", nw.ID).Error; err != nil {
		return nil, 0, err
	}
	exec := &SplitExecution{Releases: in.ReleaseIDs}

	for _, t := range in.Titles {
		row := model.CatalogWorkTitle{WorkID: nw.ID, Lang: t.Lang, Title: t.Title, Kind: t.Kind, Provenance: model.WorkTitleProvenanceSource}
		if err := tx.Create(&row).Error; err != nil {
			return nil, 0, fmt.Errorf("title: %w", err)
		}
		exec.Titles = append(exec.Titles, row.ID)
	}

	if err := tx.Raw(`
		INSERT INTO catalog_release (work_id, kind, title, lang, platform, released_y, released_m, released_d,
		                             extra, field_provenance, deleted_at)
		SELECT work_id, kind, title, lang, platform, released_y, released_m, released_d,
		       jsonb_build_object('vndb_id', extra->>'vndb_id', 'moved_to_work', ?::bigint, 'moved_by', ?::text),
		       '{}'::jsonb, now()
		  FROM catalog_release
		 WHERE id IN ? AND work_id = ? AND extra->>'vndb_id' IS NOT NULL
		RETURNING id`, nw.ID, fmt.Sprintf("split:%d", r.ID), in.ReleaseIDs, orig.ID).Scan(&exec.Tombstones).Error; err != nil {
		return nil, 0, fmt.Errorf("tombstones: %w", err)
	}
	res := tx.Exec(`UPDATE catalog_release SET work_id = ? WHERE id IN ? AND work_id = ? AND deleted_at IS NULL`,
		nw.ID, in.ReleaseIDs, orig.ID)
	if res.Error != nil {
		return nil, 0, res.Error
	}
	if int(res.RowsAffected) != len(in.ReleaseIDs) {
		return nil, 0, fmt.Errorf("%w: moved %d of %d releases", ErrSplitReleases, res.RowsAffected, len(in.ReleaseIDs))
	}
	for _, rr := range releaseScopedWorkRows {
		var ids []int64
		if err := tx.Raw(fmt.Sprintf(`UPDATE %s SET work_id = ? WHERE work_id = ? AND release_id IN ? RETURNING id`, rr.table),
			nw.ID, orig.ID, in.ReleaseIDs).Scan(&ids).Error; err != nil {
			return nil, 0, fmt.Errorf("%s: %w", rr.table, err)
		}
		if rr.table == "catalog_credit" {
			exec.Credits = ids
		}
	}

	for _, ref := range in.PromoteRefs {
		srcID, err := sourceIDByKey(tx, ref.Source)
		if err != nil {
			return nil, 0, err
		}
		var found jsonRow
		if err := tx.Raw(`SELECT to_jsonb(t) AS row FROM catalog_external_ref t
			WHERE entity_type = ? AND entity_id = ? AND source_id = ? AND external_id = ?`,
			model.EntityTypeWork, orig.ID, srcID, ref.ExternalID).Scan(&found).Error; err != nil {
			return nil, 0, err
		}
		prior := json.RawMessage(found.Row)
		if len(prior) == 0 {
			return nil, 0, fmt.Errorf("%w: %s %s is not on work %d", ErrSplitRef, ref.Source, ref.ExternalID, orig.ID)
		}
		exec.Refs = append(exec.Refs, prior)
		if err := tx.Exec(`UPDATE catalog_external_ref SET entity_id = ?, link_kind = ?, matched_by = ?, verified_by = ?, verified_at = now()
			WHERE entity_type = ? AND entity_id = ? AND source_id = ? AND external_id = ?`,
			nw.ID, model.LinkKindExact, fmt.Sprintf("human:split-%d", r.ID), by,
			model.EntityTypeWork, orig.ID, srcID, ref.ExternalID).Error; err != nil {
			if isUniqueViolation(err, "uq_catalog_external_ref_exact") {
				return nil, 0, fmt.Errorf("%w: %s %s is already another work's identity", ErrSplitRef, ref.Source, ref.ExternalID)
			}
			return nil, 0, err
		}
		res := tx.Exec(`INSERT INTO catalog_match_rejection (entity_type, entity_id, source_id, external_id, reason, rejected_by)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
			model.EntityTypeWork, orig.ID, srcID, ref.ExternalID, fmt.Sprintf("split request %d", r.ID), by)
		if res.Error != nil {
			return nil, 0, res.Error
		}
		if res.RowsAffected > 0 {
			exec.Rejections = append(exec.Rejections, ref)
		}
	}

	a, b := orig.ID, nw.ID
	if b < a {
		a, b = b, a
	}
	var candidate jsonRow
	if err := tx.Raw(`SELECT to_jsonb(t) AS row FROM catalog_match_candidate t WHERE entity_type = ? AND a_id = ? AND b_id = ?`,
		model.EntityTypeWork, a, b).Scan(&candidate).Error; err != nil {
		return nil, 0, err
	}
	if len(candidate.Row) > 0 {
		exec.Candidate = json.RawMessage(candidate.Row)
	}
	if err := tx.Exec(`INSERT INTO catalog_match_candidate (entity_type, a_id, b_id, reason, status, created_at, decided_by, decided_at)
		VALUES (?, ?, ?, ?, ?, now(), ?, now())
		ON CONFLICT (entity_type, a_id, b_id) DO UPDATE SET status = EXCLUDED.status, decided_by = EXCLUDED.decided_by, decided_at = EXCLUDED.decided_at`,
		model.EntityTypeWork, a, b, model.CandidateReasonNameFuzzy, model.CandidateStatusRejected, by).Error; err != nil {
		return nil, 0, fmt.Errorf("park candidate: %w", err)
	}

	note := fmt.Sprintf("split request %d", r.ID)
	origChanged, err := marshalChangedFields(map[string]any{"split_to": nw.ID, "releases": in.ReleaseIDs})
	if err != nil {
		return nil, 0, err
	}
	if err := writeRevision(tx, model.EntityTypeWork, orig.ID, model.RevisionActionSplit, origSnap, origChanged, by, note); err != nil {
		return nil, 0, err
	}
	newSnap, err := takeSnapshot(tx, model.EntityTypeWork, nw.ID)
	if err != nil {
		return nil, 0, fmt.Errorf("snapshot new work: %w", err)
	}
	newChanged, err := marshalChangedFields(map[string]any{"split_from": orig.ID, "releases": in.ReleaseIDs})
	if err != nil {
		return nil, 0, err
	}
	if err := writeRevision(tx, model.EntityTypeWork, nw.ID, model.RevisionActionSplit, newSnap, newChanged, by, note); err != nil {
		return nil, 0, err
	}
	if err := tx.Exec(`UPDATE catalog_work SET updated_at = now() WHERE id IN ?`, []int64{orig.ID, nw.ID}).Error; err != nil {
		return nil, 0, err
	}
	if err := repository.TouchWorks(ctx, tx, []int64{orig.ID, nw.ID}); err != nil {
		return nil, 0, fmt.Errorf("touch works: %w", err)
	}
	return exec, nw.ID, nil
}

// Undo puts the split back: the recorded rows return to the original work,
// the split's own rows go, and whatever the new work gained since is merged
// into the original through an executed merge whose redirect sends the new
// id back to the original.
func (s *SplitService) Undo(ctx context.Context, id, by int64, note string) error {
	if s.merges == nil {
		return fmt.Errorf("%w: no merge service bound", ErrSplitUndo)
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		r, err := lockSplitRequest(tx, id)
		if err != nil {
			return err
		}
		if r.Status != model.ProposalStatusExecuted || r.NewWorkID == nil || r.ExecutedAt == nil {
			return fmt.Errorf("%w: split request %d is in status %d, want executed", ErrProposalState, id, r.Status)
		}
		window := time.Duration(keys.CatalogMergeUndoWindowDays.Get()) * 24 * time.Hour
		if time.Since(*r.ExecutedAt) > window {
			return fmt.Errorf("%w: executed %s ago, the window is %s", ErrSplitUndo, time.Since(*r.ExecutedAt).Round(time.Hour), window)
		}
		orig, nw := r.EntityID, *r.NewWorkID
		for _, wid := range []int64{orig, nw} {
			if err := assertEntityAlive(tx, model.EntityTypeWork, wid); err != nil {
				return fmt.Errorf("%w: %v", ErrSplitUndo, err)
			}
		}
		var exec SplitExecution
		if err := json.Unmarshal(r.Execution, &exec); err != nil {
			return fmt.Errorf("execution record: %w", err)
		}
		if err := unsplitRows(tx, r, &exec, orig, nw); err != nil {
			return err
		}

		mp := &model.CatalogMergeProposal{
			EntityType: model.EntityTypeWork, SourceEntityID: nw, TargetEntityID: orig,
			Status: model.ProposalStatusApproved, FieldResolution: datatypes.JSON(`{}`),
			ProposedBy: by, Note: fmt.Sprintf("undo split request %d", r.ID), Origin: "split_undo",
		}
		past := time.Now().Add(-time.Second)
		mp.ExecuteAfter = &past
		if err := tx.Create(mp).Error; err != nil {
			return fmt.Errorf("undo merge proposal: %w", err)
		}
		if err := s.merges.withDB(tx).ExecuteMerge(ctx, mp.ID, &by); err != nil {
			return fmt.Errorf("merge the new work back: %w", err)
		}
		now := time.Now()
		return tx.Model(r).Updates(map[string]any{
			"status": model.ProposalStatusUndone, "undone_by": by, "undone_at": now,
			"undo_note": note, "undo_merge_id": mp.ID, "changed_at": now,
		}).Error
	})
}

func unsplitRows(tx *gorm.DB, r *model.CatalogSplitRequest, exec *SplitExecution, orig, nw int64) error {
	for _, raw := range exec.Refs {
		var prior struct {
			SourceID   int16      `json:"source_id"`
			ExternalID string     `json:"external_id"`
			LinkKind   int16      `json:"link_kind"`
			MatchedBy  string     `json:"matched_by"`
			VerifiedBy *int64     `json:"verified_by"`
			VerifiedAt *time.Time `json:"verified_at"`
		}
		if err := json.Unmarshal(raw, &prior); err != nil {
			return fmt.Errorf("recorded ref: %w", err)
		}
		if err := tx.Exec(`UPDATE catalog_external_ref SET entity_id = ?, link_kind = ?, matched_by = ?, verified_by = ?, verified_at = ?
			WHERE entity_type = ? AND entity_id = ? AND source_id = ? AND external_id = ?`,
			orig, prior.LinkKind, prior.MatchedBy, prior.VerifiedBy, prior.VerifiedAt,
			model.EntityTypeWork, nw, prior.SourceID, prior.ExternalID).Error; err != nil {
			return fmt.Errorf("restore ref: %w", err)
		}
	}
	for _, ref := range exec.Rejections {
		srcID, err := sourceIDByKey(tx, ref.Source)
		if err != nil {
			return err
		}
		if err := tx.Exec(`DELETE FROM catalog_match_rejection WHERE entity_type = ? AND entity_id = ? AND source_id = ? AND external_id = ?`,
			model.EntityTypeWork, orig, srcID, ref.ExternalID).Error; err != nil {
			return err
		}
	}
	a, b := orig, nw
	if b < a {
		a, b = b, a
	}
	if err := tx.Exec(`DELETE FROM catalog_match_candidate WHERE entity_type = ? AND a_id = ? AND b_id = ?`,
		model.EntityTypeWork, a, b).Error; err != nil {
		return err
	}
	if len(exec.Candidate) > 0 && string(exec.Candidate) != "null" {
		if err := tx.Exec(`INSERT INTO catalog_match_candidate SELECT * FROM jsonb_populate_record(NULL::catalog_match_candidate, ?::jsonb)`,
			string(exec.Candidate)).Error; err != nil {
			return fmt.Errorf("restore candidate: %w", err)
		}
	}
	if len(exec.Tombstones) > 0 {
		if err := tx.Exec(`DELETE FROM catalog_release WHERE id IN ? AND work_id = ? AND deleted_at IS NOT NULL AND extra->>'moved_by' = ?`,
			exec.Tombstones, orig, fmt.Sprintf("split:%d", r.ID)).Error; err != nil {
			return fmt.Errorf("drop tombstones: %w", err)
		}
	}
	if len(exec.Credits) > 0 {
		if err := tx.Exec(`UPDATE catalog_credit c SET work_id = ? WHERE c.id IN ? AND c.work_id = ?
			AND NOT EXISTS (SELECT 1 FROM catalog_credit d WHERE d.work_id = ? AND d.credit_name_id = c.credit_name_id
			                AND d.role_id = c.role_id AND COALESCE(d.character_id, 0) = COALESCE(c.character_id, 0))`,
			orig, exec.Credits, nw, orig).Error; err != nil {
			return fmt.Errorf("return credits: %w", err)
		}
	}
	if len(exec.Titles) > 0 {
		if err := tx.Exec(`DELETE FROM catalog_work_title WHERE id IN ? AND work_id = ?`, exec.Titles, nw).Error; err != nil {
			return fmt.Errorf("drop titles: %w", err)
		}
	}
	if len(exec.Releases) > 0 {
		if err := tx.Exec(`UPDATE catalog_release SET work_id = ? WHERE id IN ? AND work_id = ?`, orig, exec.Releases, nw).Error; err != nil {
			return fmt.Errorf("return releases: %w", err)
		}
	}
	return nil
}

type jsonRow struct {
	Row datatypes.JSON `gorm:"column:row"`
}

type DueSplitReport struct {
	Due, Executed, Closed, Failed int
}

func (s *SplitService) ExecuteDue(ctx context.Context) (DueSplitReport, error) {
	var ids []int64
	if err := s.db.WithContext(ctx).Model(&model.CatalogSplitRequest{}).
		Where("status = ? AND execute_after <= ?", model.ProposalStatusApproved, time.Now()).
		Order("id").Pluck("id", &ids).Error; err != nil {
		return DueSplitReport{}, err
	}
	rep := DueSplitReport{Due: len(ids)}
	var firstErr error
	for _, id := range ids {
		closed, err := s.Execute(ctx, id, nil)
		switch {
		case err != nil:
			rep.Failed++
			if firstErr == nil {
				firstErr = fmt.Errorf("execute split request %d: %w", id, err)
			}
		case closed:
			rep.Closed++
		default:
			rep.Executed++
		}
	}
	return rep, firstErr
}

// Events lists executed and undone splits by the moment they last changed,
// oldest first, after the (changedAfter, afterID) keyset position.
func (s *SplitService) Events(ctx context.Context, changedAfter time.Time, afterID int64, limit int) ([]model.CatalogSplitRequest, error) {
	if limit <= 0 || limit > 101 {
		limit = 50
	}
	var rows []model.CatalogSplitRequest
	err := s.db.WithContext(ctx).
		Where("changed_at IS NOT NULL AND (changed_at, id) > (?, ?)", changedAfter, afterID).
		Order("changed_at, id").Limit(limit).Find(&rows).Error
	return rows, err
}

func (s *SplitService) WorkNames(ctx context.Context, ids []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		ID          int64
		DisplayName string
	}
	if err := s.db.WithContext(ctx).Raw(`SELECT id, display_name FROM catalog_work WHERE id IN ?`, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = r.DisplayName
	}
	return out, nil
}

func (s *SplitService) transition(ctx context.Context, id int64, allowed []int16, fn func(*gorm.DB, *model.CatalogSplitRequest) error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		r, err := lockSplitRequest(tx, id)
		if err != nil {
			return err
		}
		if !slices.Contains(allowed, r.Status) {
			return fmt.Errorf("%w: split request %d is in status %d", ErrProposalState, id, r.Status)
		}
		return fn(tx, r)
	})
}

func lockSplitRequest(tx *gorm.DB, id int64) (*model.CatalogSplitRequest, error) {
	var r model.CatalogSplitRequest
	if err := tx.Raw(`SELECT * FROM catalog_split_request WHERE id = ? FOR UPDATE`, id).Scan(&r).Error; err != nil {
		return nil, err
	}
	if r.ID == 0 {
		return nil, fmt.Errorf("%w: split request %d", ErrNotFound, id)
	}
	return &r, nil
}

func normalizeSplitInput(in SplitInput) SplitInput {
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	in.OLang = strings.TrimSpace(in.OLang)
	in.Note = strings.TrimSpace(in.Note)
	seen := map[int64]bool{}
	ids := make([]int64, 0, len(in.ReleaseIDs))
	for _, id := range in.ReleaseIDs {
		if id > 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	in.ReleaseIDs = ids
	titles := make([]model.SplitTitle, 0, len(in.Titles))
	seenTitle := map[model.SplitTitle]bool{}
	for _, t := range in.Titles {
		t.Lang = strings.TrimSpace(t.Lang)
		t.Title = strings.TrimSpace(t.Title)
		if t.Title == "" || seenTitle[t] {
			continue
		}
		seenTitle[t] = true
		titles = append(titles, t)
	}
	in.Titles = titles
	refs := make([]model.SplitRef, 0, len(in.PromoteRefs))
	seenRef := map[model.SplitRef]bool{}
	for _, ref := range in.PromoteRefs {
		ref.Source = strings.TrimSpace(ref.Source)
		ref.ExternalID = strings.TrimSpace(ref.ExternalID)
		if ref.Source == "" || ref.ExternalID == "" || seenRef[ref] {
			continue
		}
		seenRef[ref] = true
		refs = append(refs, ref)
	}
	in.PromoteRefs = refs
	return in
}

func validateSplit(db *gorm.DB, in SplitInput) error {
	if in.DisplayName == "" || len([]rune(in.DisplayName)) > maxSplitTitleLen {
		return fmt.Errorf("%w: display_name must be 1-%d characters", ErrSplitDraft, maxSplitTitleLen)
	}
	if !olangPattern.MatchString(in.OLang) {
		return fmt.Errorf("%w: olang %q is not a language tag", ErrSplitDraft, in.OLang)
	}
	if len(in.Titles) > maxSplitTitles {
		return fmt.Errorf("%w: at most %d titles", ErrSplitDraft, maxSplitTitles)
	}
	for _, t := range in.Titles {
		if len([]rune(t.Title)) > maxSplitTitleLen {
			return fmt.Errorf("%w: a title is longer than %d characters", ErrSplitDraft, maxSplitTitleLen)
		}
		if t.Kind != model.WorkTitleKindOfficial && t.Kind != model.WorkTitleKindAlias && t.Kind != model.WorkTitleKindAbbreviation {
			return fmt.Errorf("%w: title kind %d", ErrSplitDraft, t.Kind)
		}
	}
	if len(in.ReleaseIDs) == 0 {
		return fmt.Errorf("%w: choose at least one release", ErrSplitReleases)
	}
	var live []int64
	if err := db.Raw(`SELECT id FROM catalog_release WHERE work_id = ? AND deleted_at IS NULL`, in.WorkID).Scan(&live).Error; err != nil {
		return err
	}
	for _, id := range in.ReleaseIDs {
		if !slices.Contains(live, id) {
			return fmt.Errorf("%w: release %d is not a live release of work %d", ErrSplitReleases, id, in.WorkID)
		}
	}
	if len(in.ReleaseIDs) >= len(live) {
		return fmt.Errorf("%w: at least one release has to stay with the original work", ErrSplitReleases)
	}
	if len(in.PromoteRefs) > maxSplitPromotion {
		return fmt.Errorf("%w: at most %d source entries", ErrSplitRef, maxSplitPromotion)
	}
	for _, ref := range in.PromoteRefs {
		srcID, err := sourceIDByKey(db, ref.Source)
		if err != nil {
			return err
		}
		if !slices.Contains(model.EditionSplittingSourceIDs, srcID) {
			return fmt.Errorf("%w: %s does not list editions separately", ErrSplitRef, ref.Source)
		}
		var row struct {
			LinkKind int16
			Found    bool
		}
		if err := db.Raw(`SELECT link_kind, true AS found FROM catalog_external_ref
			WHERE entity_type = ? AND entity_id = ? AND source_id = ? AND external_id = ?`,
			model.EntityTypeWork, in.WorkID, srcID, ref.ExternalID).Scan(&row).Error; err != nil {
			return err
		}
		if !row.Found {
			return fmt.Errorf("%w: %s %s is not linked to work %d", ErrSplitRef, ref.Source, ref.ExternalID, in.WorkID)
		}
		if row.LinkKind == model.LinkKindExact {
			return fmt.Errorf("%w: %s %s is the original work's own identity", ErrSplitRef, ref.Source, ref.ExternalID)
		}
		var held int64
		if err := db.Raw(`SELECT entity_id FROM catalog_external_ref
			WHERE entity_type = ? AND source_id = ? AND external_id = ? AND link_kind = ?`,
			model.EntityTypeWork, srcID, ref.ExternalID, model.LinkKindExact).Scan(&held).Error; err != nil {
			return err
		}
		if held != 0 {
			return fmt.Errorf("%w: %s %s is already work %d's identity", ErrSplitRef, ref.Source, ref.ExternalID, held)
		}
	}
	return nil
}

func setSplitDraft(r *model.CatalogSplitRequest, in SplitInput) error {
	ids, err := json.Marshal(in.ReleaseIDs)
	if err != nil {
		return err
	}
	titles, err := json.Marshal(in.Titles)
	if err != nil {
		return err
	}
	refs, err := json.Marshal(in.PromoteRefs)
	if err != nil {
		return err
	}
	r.ReleaseIDs, r.Titles, r.PromoteRefs = ids, titles, refs
	r.DisplayName, r.OLang = in.DisplayName, in.OLang
	return nil
}

func splitInputOf(r *model.CatalogSplitRequest) (SplitInput, error) {
	in := SplitInput{WorkID: r.EntityID, DisplayName: r.DisplayName, OLang: r.OLang, Note: r.Note}
	if err := json.Unmarshal(r.ReleaseIDs, &in.ReleaseIDs); err != nil {
		return in, fmt.Errorf("release_ids: %w", err)
	}
	if err := json.Unmarshal(r.Titles, &in.Titles); err != nil {
		return in, fmt.Errorf("titles: %w", err)
	}
	if err := json.Unmarshal(r.PromoteRefs, &in.PromoteRefs); err != nil {
		return in, fmt.Errorf("promote_refs: %w", err)
	}
	return in, nil
}

func sourceIDByKey(db *gorm.DB, key string) (int16, error) {
	var id int16
	if err := db.Raw(`SELECT id FROM catalog_source WHERE key = ?`, key).Scan(&id).Error; err != nil {
		return 0, err
	}
	if id == 0 {
		return 0, fmt.Errorf("%w: unknown source %q", ErrSplitRef, key)
	}
	return id, nil
}

// SplitInputOf exposes a request's stored selection for faces that echo it.
func SplitInputOf(r *model.CatalogSplitRequest) (SplitInput, error) { return splitInputOf(r) }
