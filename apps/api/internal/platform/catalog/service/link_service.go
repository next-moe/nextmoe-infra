package service

import (
	"context"
	"fmt"

	"api/internal/platform/catalog/model"
	"api/internal/platform/provenance"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type LinkService struct {
	db *gorm.DB
}

func NewLinkService(db *gorm.DB) *LinkService { return &LinkService{db: db} }

type PersonLinkResult struct {
	PersonID    int64 `json:"person_id"`
	Created     bool  `json:"created"`
	NeedsManual bool  `json:"needs_manual"`
}

func (s *LinkService) linkCreditsTx(tx *gorm.DB, aID, bID int64, actorID *int64) (PersonLinkResult, error) {
	a, err := lockCreditName(tx, aID)
	if err != nil {
		return PersonLinkResult{}, err
	}
	b, err := lockCreditName(tx, bID)
	if err != nil {
		return PersonLinkResult{}, err
	}
	if humanDetached(a) || humanDetached(b) {
		return PersonLinkResult{NeedsManual: true}, nil
	}

	switch {
	case a.PersonID != nil && b.PersonID != nil:
		if *a.PersonID == *b.PersonID {
			return PersonLinkResult{PersonID: *a.PersonID}, nil
		}
		return PersonLinkResult{NeedsManual: true}, nil
	case a.PersonID != nil:
		return s.attachOrphan(tx, *a.PersonID, b, actorID)
	case b.PersonID != nil:
		return s.attachOrphan(tx, *b.PersonID, a, actorID)
	default:
		return s.createAndAttach(tx, a, b, actorID)
	}
}

// Two orphan names do not make a new person while someone already holds their
// spelling on their works. On 2026-09-17 the alias lane minted P6549 from
// 木村貴宏(すがわらあわじ、すがわらあわぢ) and すがわらあわぢ while P3177 held
// 木村 貴宏 on the same works; 80 duplicate people were merged on 2026-10-11.
func (s *LinkService) createAndAttach(tx *gorm.DB, a, b *model.CatalogCreditName, actorID *int64) (PersonLinkResult, error) {
	hosts, err := coCreditHosts(tx, []int64{a.ID, b.ID})
	if err != nil {
		return PersonLinkResult{}, err
	}
	switch {
	case len(hosts) > 1:
		return PersonLinkResult{NeedsManual: true}, nil
	case len(hosts) == 1 && hosts[0].Strong:
		for _, n := range []*model.CatalogCreditName{a, b} {
			if _, err := s.attachOrphan(tx, hosts[0].PersonID, n, actorID); err != nil {
				return PersonLinkResult{}, err
			}
		}
		return PersonLinkResult{PersonID: hosts[0].PersonID}, nil
	}

	primary := a
	aCredits, err := creditCount(tx, a.ID)
	if err != nil {
		return PersonLinkResult{}, err
	}
	bCredits, err := creditCount(tx, b.ID)
	if err != nil {
		return PersonLinkResult{}, err
	}
	if bCredits > aCredits {
		primary = b
	}

	p := model.CatalogPerson{DisplayName: primary.Name, FieldProvenance: []byte(`{}`)}
	if err := tx.Create(&p).Error; err != nil {
		return PersonLinkResult{}, err
	}
	if err := tx.Model(&model.CatalogCreditName{}).Where("id IN ?", []int64{a.ID, b.ID}).
		Update("person_id", p.ID).Error; err != nil {
		return PersonLinkResult{}, err
	}
	if err := tx.Model(&model.CatalogPerson{}).Where("id = ?", p.ID).
		Update("primary_credit_name_id", primary.ID).Error; err != nil {
		return PersonLinkResult{}, err
	}

	if err := revisionOf(tx, model.EntityTypePerson, p.ID, model.RevisionActionCreated, nil, actorID,
		"person created by linking candidate names"); err != nil {
		return PersonLinkResult{}, err
	}
	for _, n := range []*model.CatalogCreditName{a, b} {
		if err := revisionOfNamePersonChange(tx, n.ID, p.ID, actorID); err != nil {
			return PersonLinkResult{}, err
		}
	}
	return PersonLinkResult{PersonID: p.ID, Created: true}, nil
}

func (s *LinkService) attachOrphan(tx *gorm.DB, personID int64, orphan *model.CatalogCreditName, actorID *int64) (PersonLinkResult, error) {
	if err := tx.Model(&model.CatalogCreditName{}).Where("id = ?", orphan.ID).
		Update("person_id", personID).Error; err != nil {
		return PersonLinkResult{}, err
	}
	if err := revisionOfNamePersonChange(tx, orphan.ID, personID, actorID); err != nil {
		return PersonLinkResult{}, err
	}
	if err := revisionOf(tx, model.EntityTypePerson, personID, model.RevisionActionUpdated, nil, actorID,
		"credit name attached by linking candidate"); err != nil {
		return PersonLinkResult{}, err
	}
	return PersonLinkResult{PersonID: personID}, nil
}

func (s *LinkService) DetachName(ctx context.Context, creditNameID int64, actorID *int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		n, err := lockCreditName(tx, creditNameID)
		if err != nil {
			return err
		}
		if n.PersonID == nil {
			return fmt.Errorf("%w: credit name %d is not attached to a person", ErrProposalState, creditNameID)
		}
		personID := *n.PersonID

		if err := tx.Model(&model.CatalogCreditName{}).Where("id = ?", creditNameID).
			Update("person_id", nil).Error; err != nil {
			return err
		}
		if err := revisionOfNameDetach(tx, creditNameID, actorID); err != nil {
			return err
		}

		var remaining int64
		if err := tx.Model(&model.CatalogCreditName{}).Where("person_id = ?", personID).
			Count(&remaining).Error; err != nil {
			return err
		}
		if remaining == 0 {
			if err := revisionOf(tx, model.EntityTypePerson, personID, model.RevisionActionDeleted, nil, actorID,
				"empty person removed after detach"); err != nil {
				return err
			}
			return tx.Unscoped().Delete(&model.CatalogPerson{}, personID).Error
		}
		return s.repointPrimaryIfDetached(tx, personID, creditNameID, actorID)
	})
}

func (s *LinkService) repointPrimaryIfDetached(tx *gorm.DB, personID, detachedID int64, actorID *int64) error {
	var p model.CatalogPerson
	if err := tx.First(&p, personID).Error; err != nil {
		return err
	}
	if p.PrimaryCreditNameID != nil && *p.PrimaryCreditNameID == detachedID {
		primary, err := primaryOfPerson(tx, personID)
		if err != nil {
			return err
		}
		if err := tx.Model(&model.CatalogPerson{}).Where("id = ?", personID).
			Updates(map[string]any{"primary_credit_name_id": primary.ID, "display_name": primary.Name}).Error; err != nil {
			return err
		}
	}
	return revisionOf(tx, model.EntityTypePerson, personID, model.RevisionActionUpdated, nil, actorID,
		"credit name detached")
}

// A name a person detached in the editor stays an orphan until a person
// re-attaches it; the candidate lanes would otherwise relink it on their next run.
func humanDetached(n *model.CatalogCreditName) bool {
	return n.PersonID == nil && provenance.IsHuman(provenance.FirstSource(n.FieldProvenance, "person_id"))
}

func lockCreditName(tx *gorm.DB, id int64) (*model.CatalogCreditName, error) {
	var n model.CatalogCreditName
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&n, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("%w: credit name %d", ErrNotFound, id)
		}
		return nil, err
	}
	return &n, nil
}

func primaryOfPerson(tx *gorm.DB, personID int64) (*model.CatalogCreditName, error) {
	var names []model.CatalogCreditName
	if err := tx.Where("person_id = ?", personID).Order("id").Find(&names).Error; err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("%w: person %d has no names", ErrNotFound, personID)
	}
	best := &names[0]
	bestCredits, err := creditCount(tx, best.ID)
	if err != nil {
		return nil, err
	}
	for i := 1; i < len(names); i++ {
		c, err := creditCount(tx, names[i].ID)
		if err != nil {
			return nil, err
		}
		if c > bestCredits {
			best, bestCredits = &names[i], c
		}
	}
	return best, nil
}

func creditCount(tx *gorm.DB, creditNameID int64) (int64, error) {
	var n int64
	err := tx.Model(&model.CatalogCredit{}).Where("credit_name_id = ?", creditNameID).Count(&n).Error
	return n, err
}

func revisionOf(tx *gorm.DB, entityType int16, entityID int64, action int16, changed map[string]any, actorID *int64, note string) error {
	snap, err := takeSnapshot(tx, entityType, entityID)
	if err != nil {
		return err
	}
	changedFields, err := marshalChangedFields(changed)
	if err != nil {
		return err
	}
	return writeRevision(tx, entityType, entityID, action, snap, changedFields, actorID, note)
}

func revisionOfNamePersonChange(tx *gorm.DB, creditNameID, personID int64, actorID *int64) error {
	return revisionOf(tx, model.EntityTypeCreditName, creditNameID, model.RevisionActionUpdated,
		map[string]any{"person_id": personID}, actorID, "attached to person")
}

func revisionOfNameDetach(tx *gorm.DB, creditNameID int64, actorID *int64) error {
	return revisionOf(tx, model.EntityTypeCreditName, creditNameID, model.RevisionActionUpdated,
		map[string]any{"person_id": nil}, actorID, "detached from person")
}
