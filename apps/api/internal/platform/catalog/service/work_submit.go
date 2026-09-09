package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/editing"
	"api/internal/platform/provenance"

	"gorm.io/gorm"
)

type SubmitWorkParams struct {
	Site          string
	ProductWorkID int64
	ActorUID      int64
	// Source-derived, written straight to the column with no provenance stamp
	// so the weekly releasemeta lane and human edits both still own the field;
	// a rating in Fields goes through editspec and stamps curated instead.
	ContentRating int16
	Fields        map[string]any
	Released      ReleaseDate
	Trusted       bool
	// ConfirmDuplicates mints past the soft duplicate gate: without it a
	// submission whose folded titles match live same-medium works is refused
	// with the suspects, nothing written.
	ConfirmDuplicates bool
}

type ReleaseDate struct {
	Y int16
	M int16
	D int16
}

func (d ReleaseDate) Given() bool { return d.Y != 0 || d.M != 0 || d.D != 0 }

func (d ReleaseDate) validate() error {
	if !d.Given() {
		return nil
	}
	switch {
	case d.Y < 1970 || d.Y > 2200:
		return fmt.Errorf("%w: released.y must be between 1970 and 2200", ErrSubmitInvalidDate)
	case d.M < 0 || d.M > 12:
		return fmt.Errorf("%w: released.m must be between 1 and 12", ErrSubmitInvalidDate)
	case d.D < 0 || d.D > 31:
		return fmt.Errorf("%w: released.d must be between 1 and 31", ErrSubmitInvalidDate)
	case d.D > 0 && d.M == 0:
		return fmt.Errorf("%w: released.d requires released.m", ErrSubmitInvalidDate)
	}
	return nil
}

type SubmitWorkResult struct {
	WorkID        int64  `json:"work_id"`
	ProductWorkID int64  `json:"product_work_id"`
	ClaimState    string `json:"claim_state"`
	EventID       int64  `json:"event_id"`
	ReleaseID     int64  `json:"release_id,omitempty"`
}

type ClaimExistsError struct {
	WorkID        int64
	Site          string
	ProductWorkID int64
	CurrentState  string
	MatchedBy     string
	Anchor        string
}

func (e *ClaimExistsError) Error() string {
	if e.MatchedBy == ClaimMatchAnchor {
		return fmt.Sprintf("%s is already registered for site %q by work %d (state %q)",
			e.Anchor, e.Site, e.WorkID, e.CurrentState)
	}
	return fmt.Sprintf("%s/%d is already claimed by work %d (state %q)",
		e.Site, e.ProductWorkID, e.WorkID, e.CurrentState)
}

const (
	ClaimMatchClaim  = "claim"
	ClaimMatchAnchor = "anchor"
)

// WorkDupeSuspectsError refuses a mint whose submitted titles collide with
// live works. Same-name distinct works are real, so this is a confirm prompt,
// not a verdict: resubmitting with ConfirmDuplicates mints and still files the
// pairs for reconciliation.
type WorkDupeSuspectsError struct {
	Suspects []WorkDupeSuspect
}

func (e *WorkDupeSuspectsError) Error() string {
	return fmt.Sprintf("%d live work(s) share a title with this submission; re-send with confirm_duplicates=true to mint anyway", len(e.Suspects))
}

var ErrSubmitTargetRequired = errors.New("site is required to submit")

var ErrSubmitDisplayNameRequired = errors.New("catalog.work.display_name is required to submit")

var ErrSubmitInvalidDate = errors.New("invalid release date")

func findClaimByAnchor(tx *gorm.DB, site string, anchors []editspec.SubmissionAnchor) (*model.CatalogWork, string, error) {
	keys := make([]string, 0, len(anchors))
	args := []any{model.EntityTypeWork, site}
	for _, a := range anchors {
		keys = append(keys, "(s.key = ? AND r.external_id = ?)")
		args = append(args, a.SourceKey, a.ExternalID)
	}
	var row struct {
		ID            int64
		Site          *string
		ProductWorkID *int64
		ClaimState    *int16
		SourceKey     string
		ExternalID    string
	}
	err := tx.Raw(`
		SELECT w.id, w.site, w.product_work_id, w.claim_state,
		       s.key AS source_key, r.external_id
		FROM catalog_work w
		JOIN catalog_external_ref r ON r.entity_id = w.id AND r.entity_type = ?
		JOIN catalog_source s ON s.id = r.source_id
		WHERE w.deleted_at IS NULL
		  AND w.site = ?
		  AND w.product_work_id IS NOT NULL
		  AND (`+strings.Join(keys, " OR ")+`)
		ORDER BY w.id
		LIMIT 1`, args...).Scan(&row).Error
	if err != nil {
		return nil, "", err
	}
	if row.ID == 0 {
		return nil, "", nil
	}
	return &model.CatalogWork{
		ID: row.ID, Site: row.Site, ProductWorkID: row.ProductWorkID, ClaimState: row.ClaimState,
	}, row.SourceKey + ":" + row.ExternalID, nil
}

func (s *ClaimLifecycleService) SubmitWork(ctx context.Context, p SubmitWorkParams) (*SubmitWorkResult, error) {
	if p.Site == "" {
		return nil, ErrSubmitTargetRequired
	}
	if p.ProductWorkID < 0 {
		return nil, ErrSubmitTargetRequired
	}
	if name, ok := p.Fields[editspec.FieldWorkDisplayName].(string); !ok || name == "" {
		return nil, ErrSubmitDisplayNameRequired
	}
	if err := p.Released.validate(); err != nil {
		return nil, err
	}
	var anchors []editspec.SubmissionAnchor
	if p.ProductWorkID == 0 {
		anchors = editspec.SubmissionAnchorsOf(p.Fields[editspec.FieldWorkLinks])
	}

	var out SubmitWorkResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if p.ProductWorkID > 0 {
			existing, err := repository.FindClaimed(tx, galgameMediumID, p.Site, p.ProductWorkID)
			if err != nil {
				return err
			}
			if existing != nil {
				return &ClaimExistsError{
					WorkID: existing.ID, Site: p.Site, ProductWorkID: p.ProductWorkID,
					CurrentState: model.ClaimStateKey(existing.Site, existing.ProductWorkID, existing.ClaimState),
					MatchedBy:    ClaimMatchClaim,
				}
			}
		} else if len(anchors) > 0 {
			existing, anchor, err := findClaimByAnchor(tx, p.Site, anchors)
			if err != nil {
				return err
			}
			if existing != nil {
				productID := int64(0)
				if existing.ProductWorkID != nil {
					productID = *existing.ProductWorkID
				}
				return &ClaimExistsError{
					WorkID: existing.ID, Site: p.Site, ProductWorkID: productID,
					CurrentState: model.ClaimStateKey(existing.Site, existing.ProductWorkID, existing.ClaimState),
					MatchedBy:    ClaimMatchAnchor, Anchor: anchor,
				}
			}
		}

		if !p.ConfirmDuplicates {
			names := []string{p.Fields[editspec.FieldWorkDisplayName].(string)}
			names = append(names, editspec.SubmissionTitleStrings(p.Fields[editspec.FieldWorkTitles])...)
			suspects, err := findWorkDupeSuspectsByNames(tx, galgameMediumID, names)
			if err != nil {
				return err
			}
			if len(suspects) > 0 {
				return &WorkDupeSuspectsError{Suspects: suspects}
			}
		}

		state := model.ClaimStatePending
		if p.Trusted {
			state = model.ClaimStateLive
		}
		w := model.CatalogWork{
			MediumID:        galgameMediumID,
			Site:            &p.Site,
			ClaimState:      &state,
			OLang:           model.OLangDefault,
			Status:          model.WorkStatusLive,
			ContentRating:   p.ContentRating,
			Extra:           []byte(`{}`),
			FieldProvenance: []byte(`{}`),
		}
		if p.ActorUID != 0 {
			w.OwnerUserID = &p.ActorUID
		}
		if err := tx.Create(&w).Error; err != nil {
			return err
		}
		productWorkID := p.ProductWorkID
		if productWorkID == 0 {
			productWorkID = w.ID
		}
		if err := tx.Model(&w).Update("product_work_id", productWorkID).Error; err != nil {
			return err
		}

		if err := editspec.ApplyWorkFields(ctx, tx, w.ID, p.Fields); err != nil {
			return err
		}
		if err := recordWorkDupeSuspects(tx, w.ID); err != nil {
			return err
		}

		if p.Released.Given() {
			fp, err := mintedReleaseDateProvenance()
			if err != nil {
				return err
			}
			rel := model.CatalogRelease{
				WorkID: w.ID, Kind: model.ReleaseKindDefault, Extra: []byte(`{}`),
				FieldProvenance: fp,
			}
			rel.ReleasedY = &p.Released.Y
			if p.Released.M > 0 {
				rel.ReleasedM = &p.Released.M
			}
			if p.Released.D > 0 {
				rel.ReleasedD = &p.Released.D
			}
			if err := tx.Create(&rel).Error; err != nil {
				return err
			}
			out.ReleaseID = rel.ID
		}

		snap, err := takeSnapshot(tx, model.EntityTypeWork, w.ID)
		if err != nil {
			return err
		}
		actor := p.ActorUID
		if err := writeRevision(tx, model.EntityTypeWork, w.ID, model.RevisionActionCreated, snap, nil, &actor,
			fmt.Sprintf("submitted by %s/%d", p.Site, productWorkID)); err != nil {
			return err
		}

		eventID, err := appendClaimEvent(tx, claimEventRow{
			WorkID: w.ID, To: state,
			ActorUID: p.ActorUID, Site: p.Site,
		})
		if err != nil {
			return err
		}
		out.WorkID, out.ProductWorkID = w.ID, productWorkID
		out.ClaimState, out.EventID = model.ClaimStateKey(&p.Site, &productWorkID, &state), eventID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func mintedReleaseDateProvenance() ([]byte, error) {
	entry, err := editing.StampJSON(provenance.SourceUser)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]json.RawMessage{
		"released_y": entry,
		"released_m": entry,
		"released_d": entry,
	})
}
