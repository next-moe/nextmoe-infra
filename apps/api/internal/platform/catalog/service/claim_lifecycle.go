package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

type ClaimAction string

const (
	ClaimActionClaim    ClaimAction = "claim"
	ClaimActionSubmit   ClaimAction = "submit"
	ClaimActionPublish  ClaimAction = "publish"
	ClaimActionWithdraw ClaimAction = "withdraw"
	ClaimActionApprove  ClaimAction = "approve"
	ClaimActionDecline  ClaimAction = "decline"
	ClaimActionBan      ClaimAction = "ban"
	ClaimActionUnban    ClaimAction = "unban"
	// delete soft-deletes a draft rather than moving it to another state, so
	// it is deliberately absent from ClaimActions and transitions.
	ClaimActionDelete ClaimAction = "delete"
)

var ClaimActions = []ClaimAction{
	ClaimActionClaim, ClaimActionSubmit, ClaimActionPublish, ClaimActionWithdraw,
	ClaimActionApprove, ClaimActionDecline, ClaimActionBan, ClaimActionUnban,
}

var ReviewActions = map[ClaimAction]bool{
	ClaimActionApprove: true, ClaimActionDecline: true,
	ClaimActionBan: true, ClaimActionUnban: true,
}

var transitions = map[ClaimAction]struct {
	from []string
	to   string
}{
	ClaimActionClaim:    {from: []string{model.ClaimStateKeyNone}, to: model.ClaimStateKeyDraft},
	ClaimActionSubmit:   {from: []string{model.ClaimStateKeyDraft, model.ClaimStateKeyDeclined}, to: model.ClaimStateKeyPending},
	ClaimActionPublish:  {from: []string{model.ClaimStateKeyDraft}, to: model.ClaimStateKeyLive},
	ClaimActionWithdraw: {from: []string{model.ClaimStateKeyLive, model.ClaimStateKeyPending}, to: model.ClaimStateKeyDraft},
	ClaimActionApprove:  {from: []string{model.ClaimStateKeyPending}, to: model.ClaimStateKeyLive},
	ClaimActionDecline:  {from: []string{model.ClaimStateKeyPending}, to: model.ClaimStateKeyDeclined},
	ClaimActionBan: {from: []string{
		model.ClaimStateKeyLive, model.ClaimStateKeyDraft,
		model.ClaimStateKeyPending, model.ClaimStateKeyDeclined,
	}, to: model.ClaimStateKeyHidden},
	ClaimActionUnban: {from: []string{model.ClaimStateKeyHidden}},
}

var claimStateValue = map[string]int16{
	model.ClaimStateKeyLive:     model.ClaimStateLive,
	model.ClaimStateKeyDraft:    model.ClaimStateDraft,
	model.ClaimStateKeyPending:  model.ClaimStatePending,
	model.ClaimStateKeyDeclined: model.ClaimStateDeclined,
	model.ClaimStateKeyHidden:   model.ClaimStateHidden,
}

func TransitionRule(a ClaimAction) (from []string, known bool) {
	rule, ok := transitions[a]
	return rule.from, ok
}

type ClaimTransitionError struct {
	Action  ClaimAction
	Current string
	Allowed []string
}

func (e *ClaimTransitionError) Error() string {
	return fmt.Sprintf("cannot %s a claim in state %q (allowed from: %s)",
		e.Action, e.Current, strings.Join(e.Allowed, ", "))
}

type ClaimOwnershipError struct {
	WorkID     int64
	OwningSite string
}

func (e *ClaimOwnershipError) Error() string {
	return fmt.Sprintf("work %d is claimed by site %q", e.WorkID, e.OwningSite)
}

type ClaimNotOwnedError struct {
	WorkID   int64
	ActorUID int64
	OwnerUID int64
}

func (e *ClaimNotOwnedError) Error() string {
	return fmt.Sprintf("work %d belongs to user %d, not user %d", e.WorkID, e.OwnerUID, e.ActorUID)
}

var ownedActions = map[ClaimAction]bool{
	ClaimActionSubmit: true, ClaimActionPublish: true, ClaimActionWithdraw: true,
}

var ErrClaimReasonRequired = errors.New("a reason is required")

var ErrClaimTargetRequired = errors.New("site and product_work_id are required to claim")

type ClaimActionParams struct {
	WorkID        int64
	Action        ClaimAction
	Site          string
	ProductWorkID *int64
	ActorUID      int64
	Reason        string
	RequireOwner  bool
}

type ClaimActionResult struct {
	WorkID  int64   `json:"work_id"`
	From    *string `json:"from_state"`
	To      string  `json:"to_state"`
	EventID int64   `json:"event_id"`
}

type ClaimLifecycleService struct {
	db *gorm.DB
}

func NewClaimLifecycleService(db *gorm.DB) *ClaimLifecycleService {
	return &ClaimLifecycleService{db: db}
}

func (s *ClaimLifecycleService) Act(ctx context.Context, p ClaimActionParams) (*ClaimActionResult, error) {
	rule, known := transitions[p.Action]
	if !known {
		return nil, fmt.Errorf("unknown claim action %q", p.Action)
	}
	if p.Action == ClaimActionDecline && strings.TrimSpace(p.Reason) == "" {
		return nil, ErrClaimReasonRequired
	}
	if p.Action == ClaimActionClaim && (p.Site == "" || p.ProductWorkID == nil) {
		return nil, ErrClaimTargetRequired
	}

	var out ClaimActionResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var work struct {
			ID            int64
			Site          *string
			ProductWorkID *int64
			ClaimState    *int16
			OwnerUserID   *int64
		}
		if err := tx.Raw(`SELECT id, site, product_work_id, claim_state, owner_user_id FROM catalog_work
		                  WHERE id = ? AND deleted_at IS NULL FOR UPDATE`, p.WorkID).
			Scan(&work).Error; err != nil {
			return err
		}
		if work.ID == 0 {
			return gorm.ErrRecordNotFound
		}

		current := model.ClaimStateKey(work.Site, work.ProductWorkID, work.ClaimState)
		if !slices.Contains(rule.from, current) {
			return &ClaimTransitionError{Action: p.Action, Current: current, Allowed: rule.from}
		}
		if p.Site != "" && p.Action != ClaimActionClaim && work.Site != nil && *work.Site != p.Site {
			return &ClaimOwnershipError{WorkID: p.WorkID, OwningSite: *work.Site}
		}
		takeOwnership := false
		if p.RequireOwner && ownedActions[p.Action] {
			switch {
			case work.OwnerUserID != nil && *work.OwnerUserID != p.ActorUID:
				return &ClaimNotOwnedError{
					WorkID: p.WorkID, ActorUID: p.ActorUID, OwnerUID: *work.OwnerUserID,
				}
			case work.OwnerUserID == nil && p.ActorUID > 0:
				takeOwnership = true
			}
		}

		target := rule.to
		if p.Action == ClaimActionUnban {
			prior, err := s.priorState(tx, p.WorkID)
			if err != nil {
				return err
			}
			target = prior
		}
		updates := map[string]any{"claim_state": claimStateValue[target]}
		if takeOwnership {
			updates["owner_user_id"] = p.ActorUID
		}
		eventSite := p.Site
		if work.Site != nil && *work.Site != "" {
			eventSite = *work.Site
		}
		if p.Action == ClaimActionClaim {
			updates["site"] = p.Site
			updates["product_work_id"] = *p.ProductWorkID
			eventSite = p.Site
			if work.OwnerUserID == nil && p.ActorUID > 0 {
				updates["owner_user_id"] = p.ActorUID
			}
		}
		res := tx.Model(&model.CatalogWork{}).Where("id = ?", p.WorkID).Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}

		row := claimEventRow{
			WorkID:   p.WorkID,
			To:       claimStateValue[target],
			ActorUID: p.ActorUID,
			Site:     eventSite,
			Reason:   p.Reason,
		}
		if p.Action != ClaimActionClaim {
			prior := claimStateValue[current]
			row.From = &prior
		}
		eventID, err := appendClaimEvent(tx, row)
		if err != nil {
			return err
		}

		out = ClaimActionResult{WorkID: p.WorkID, To: target, EventID: eventID}
		if p.Action != ClaimActionClaim {
			from := current
			out.From = &from
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *ClaimLifecycleService) DeleteDraft(ctx context.Context, workID, actorUID int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var work struct {
			ID          int64
			Site        *string
			ClaimState  *int16
			OwnerUserID *int64
		}
		if err := tx.Raw(`SELECT id, site, claim_state, owner_user_id FROM catalog_work
		                  WHERE id = ? AND deleted_at IS NULL FOR UPDATE`, workID).
			Scan(&work).Error; err != nil {
			return err
		}
		if work.ID == 0 {
			return gorm.ErrRecordNotFound
		}
		if work.OwnerUserID == nil || *work.OwnerUserID != actorUID {
			ownerUID := int64(0)
			if work.OwnerUserID != nil {
				ownerUID = *work.OwnerUserID
			}
			return &ClaimNotOwnedError{WorkID: workID, ActorUID: actorUID, OwnerUID: ownerUID}
		}
		if work.ClaimState == nil || *work.ClaimState != model.ClaimStateDraft {
			return &ClaimTransitionError{
				Action:  ClaimActionDelete,
				Current: model.ClaimStateKey(work.Site, nil, work.ClaimState),
				Allowed: []string{model.ClaimStateKeyDraft},
			}
		}
		res := tx.Model(&model.CatalogWork{}).Where("id = ?", workID).Update("deleted_at", time.Now())
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

type claimEventRow struct {
	WorkID   int64
	From     *int16
	To       int16
	ActorUID int64
	Site     string
	Reason   string
}

func appendClaimEvent(tx *gorm.DB, row claimEventRow) (int64, error) {
	event := model.CatalogClaimEvent{
		WorkID:    row.WorkID,
		FromState: row.From,
		ToState:   row.To,
		ActorUID:  row.ActorUID,
		Site:      row.Site,
	}
	if reason := strings.TrimSpace(row.Reason); reason != "" {
		event.Reason = &reason
	}
	if err := tx.Create(&event).Error; err != nil {
		return 0, err
	}
	return event.ID, nil
}

func (s *ClaimLifecycleService) priorState(tx *gorm.DB, workID int64) (string, error) {
	var from *int16
	if err := tx.Raw(`SELECT from_state FROM catalog_claim_event
	                  WHERE work_id = ? AND to_state = ? ORDER BY id DESC LIMIT 1`,
		workID, model.ClaimStateHidden).Scan(&from).Error; err != nil {
		return "", err
	}
	if from == nil {
		return model.ClaimStateKeyLive, nil
	}
	site, product := "unban", int64(0)
	key := model.ClaimStateKey(&site, &product, from)
	if key == model.ClaimStateKeyHidden || key == model.ClaimStateKeyNone {
		return model.ClaimStateKeyLive, nil
	}
	return key, nil
}

type ClaimEventItem struct {
	ID            int64     `json:"id"`
	WorkID        int64     `json:"work_id"`
	FromState     *string   `json:"from_state"`
	ToState       string    `json:"to_state"`
	ActorUID      int64     `json:"actor_uid"`
	Reason        *string   `json:"reason"`
	Site          string    `json:"site"`
	ProductWorkID *int64    `json:"product_work_id"`
	CreatedAt     time.Time `json:"created_at"`
}

func (s *ClaimLifecycleService) EventsSince(ctx context.Context, since int64, limit int, site string, actorUID int64) ([]ClaimEventItem, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var rows []struct {
		ID            int64
		WorkID        int64
		FromState     *int16
		ToState       int16
		ActorUID      int64
		Reason        *string
		Site          string
		CreatedAt     time.Time
		WorkSite      *string
		ProductWorkID *int64
	}
	q := s.db.WithContext(ctx).Table("catalog_claim_event AS e").
		Select(`e.id, e.work_id, e.from_state, e.to_state, e.actor_uid, e.reason, e.site,
		        e.created_at, w.site AS work_site, w.product_work_id`).
		Joins("LEFT JOIN catalog_work w ON w.id = e.work_id").
		Where("e.id > ?", since)
	if site != "" {
		q = q.Where("e.site = ?", site)
	}
	if actorUID > 0 {
		q = q.Where("e.actor_uid = ?", actorUID)
	}
	if err := q.Order("e.id ASC").Limit(limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ClaimEventItem, 0, len(rows))
	for _, r := range rows {
		item := ClaimEventItem{
			ID: r.ID, WorkID: r.WorkID, ActorUID: r.ActorUID, Reason: r.Reason,
			Site: r.Site, ProductWorkID: r.ProductWorkID, CreatedAt: r.CreatedAt,
			ToState: stateKeyOf(r.ToState),
		}
		if r.FromState != nil {
			from := stateKeyOf(*r.FromState)
			item.FromState = &from
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *ClaimLifecycleService) PendingClaims(ctx context.Context, site string, limit int) ([]PendingClaimItem, int64, error) {
	return s.PendingClaimsAfter(ctx, site, limit, 0)
}

func (s *ClaimLifecycleService) PendingClaimsAfter(ctx context.Context, site string, limit int, before int64) ([]PendingClaimItem, int64, error) {
	return s.ModerationClaims(ctx, site, []int16{model.ClaimStatePending}, limit, before, 0)
}

// The sentinel sorts rows whose queue watermark is NULL after every real event
// id while keeping one totally ordered key, so the keyset can carry them.
// `submitted_event_id ASC NULLS LAST` with a scalar `> before` cursor put them
// on page 1 and then made them unreachable — the comparison is NULL for exactly
// those rows — and the handler also emitted no next_cursor whenever a page
// ended on one. Inlined as literal SQL rather than bound: gorm's Order() only
// understands a string or a clause.OrderByColumn and silently drops anything
// else, so a parameterised ORDER BY would have been no ORDER BY at all.
const nullEventSentinel = "9223372036854775807"

const claimWatermark = `COALESCE((SELECT max(e.id) FROM catalog_claim_event e
	WHERE e.work_id = w.id AND e.to_state = w.claim_state), ` + nullEventSentinel + `)`

// The queue used to be claim_state = pending only, while the decision face acts
// on live|draft|pending|declined (ban) and hidden (unban) — so no GET under
// /v2/moderation/ could list a hidden claim and unban was reachable only by
// already knowing the work id.
func (s *ClaimLifecycleService) ModerationClaims(ctx context.Context, site string, states []int16, limit int, beforeEvent, beforeWork int64) ([]PendingClaimItem, int64, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if len(states) == 0 {
		states = []int16{model.ClaimStatePending}
	}
	base := s.db.WithContext(ctx).Table("catalog_work AS w").
		Where("w.deleted_at IS NULL AND w.claim_state IN ?", states)
	if site != "" {
		base = base.Where("w.site = ?", site)
	}
	// Counted before the cursor predicate: total is the size of the queue, not
	// of what is left of it, which is what deviation 10 promises.
	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page := base.Session(&gorm.Session{})
	if beforeEvent > 0 {
		page = page.Where("("+claimWatermark+", w.id) > (?, ?)", beforeEvent, beforeWork)
	}
	var items []PendingClaimItem
	if err := page.
		Select(`w.id AS work_id, w.display_name, w.site, w.product_work_id, w.claim_state,
		        ` + claimWatermark + ` AS submitted_event_id`).
		Order(claimWatermark + " ASC, w.id ASC").
		Limit(limit).Scan(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

type PendingClaimItem struct {
	WorkID        int64   `json:"work_id"`
	DisplayName   string  `json:"display_name"`
	Site          *string `json:"site"`
	ProductWorkID *int64  `json:"product_work_id"`
	// Scan target for the v2 moderation queue, which lists several states at
	// once; json:"-" because the admin face this struct also serves lists only
	// pending claims, so publishing the column there would add a constant to a
	// first-party contract and drag apps/admin's generated types with it.
	ClaimState       *int16 `json:"-"`
	SubmittedEventID *int64 `json:"submitted_event_id"`
}

func (s *ClaimLifecycleService) ClaimByWorkID(ctx context.Context, workID int64, site string) (*UserClaimItem, error) {
	if workID <= 0 {
		return nil, nil
	}
	var row struct {
		WorkID        int64
		DisplayName   string
		Site          *string
		ProductWorkID *int64
		ClaimState    *int16
		LastEventID   *int64
		FromState     *int16
		ToState       *int16
		Reason        *string
		LastActorUID  *int64
		LastEventAt   *time.Time
	}
	// The newest event joins here so the write lane's pre-read and the me
	// lane's read agree on one monotonic number: without it a claim validator
	// could only be built from the state name, which an ABA round trip
	// (pending -> live -> draft -> pending) returns to unchanged.
	q := s.db.WithContext(ctx).Table("catalog_work AS w").
		Select(`w.id AS work_id, w.display_name, w.site, w.product_work_id, w.claim_state,
			le.id AS last_event_id, le.from_state, le.to_state, le.reason,
			le.actor_uid AS last_actor_uid, le.created_at AS last_event_at`).
		Joins(`LEFT JOIN LATERAL (
			SELECT e.id, e.from_state, e.to_state, e.reason, e.actor_uid, e.created_at
			FROM catalog_claim_event e
			WHERE e.work_id = w.id
			ORDER BY e.id DESC
			LIMIT 1
		) le ON true`).
		Where("w.id = ? AND w.deleted_at IS NULL", workID)
	if site != "" {
		q = q.Where("w.site = ?", site)
	}
	if err := q.Scan(&row).Error; err != nil {
		return nil, err
	}
	if row.WorkID == 0 {
		return nil, nil
	}
	item := UserClaimItem{
		WorkID: row.WorkID, DisplayName: row.DisplayName,
		ProductWorkID: row.ProductWorkID,
		ClaimState:    model.ClaimStateKey(row.Site, row.ProductWorkID, row.ClaimState),
	}
	if row.Site != nil {
		item.Site = *row.Site
	}
	if row.LastEventID != nil {
		item.LastEventID = *row.LastEventID
		if row.ToState != nil {
			item.LastToState = stateKeyOf(*row.ToState)
		}
		if row.FromState != nil {
			from := stateKeyOf(*row.FromState)
			item.LastFromState = &from
		}
		item.LastReason = row.Reason
		if row.LastActorUID != nil {
			item.LastActorUID = *row.LastActorUID
		}
		if row.LastEventAt != nil {
			item.LastEventAt = *row.LastEventAt
		}
	}
	return &item, nil
}

func stateKeyOf(state int16) string {
	site, product := "e", int64(0)
	return model.ClaimStateKey(&site, &product, &state)
}

type ClaimIdentity struct {
	Site          string
	ProductWorkID int64
}

func (s *ClaimLifecycleService) ClaimIdentities(ctx context.Context, workIDs []int64) (map[int64]ClaimIdentity, error) {
	out := map[int64]ClaimIdentity{}
	if len(workIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		ID            int64
		Site          string
		ProductWorkID int64
	}
	if err := s.db.WithContext(ctx).Model(&model.CatalogWork{}).
		Select("id, site, product_work_id").
		Where("id IN ? AND site IS NOT NULL AND product_work_id IS NOT NULL", workIDs).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = ClaimIdentity{Site: r.Site, ProductWorkID: r.ProductWorkID}
	}
	return out, nil
}
