package service

import (
	"context"
	"encoding/json"
	"fmt"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

func (s *MergeService) Unmerge(ctx context.Context, proposalID int64, actorID *int64) (int64, error) {
	var newID int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		p, err := repository.LockProposal(tx, proposalID)
		if err != nil {
			return err
		}
		if p == nil {
			return fmt.Errorf("%w: proposal %d", ErrNotFound, proposalID)
		}
		if p.Status != model.ProposalStatusExecuted {
			return fmt.Errorf("%w: proposal %d is in status %d, want executed", ErrProposalState, proposalID, p.Status)
		}
		et := p.EntityType

		rev, err := repository.LatestRevisionByAction(tx, et, p.SourceEntityID, model.RevisionActionMergedSource)
		if err != nil {
			return err
		}
		if rev == nil {
			return fmt.Errorf("%w: merged_source revision for entity %d", ErrNotFound, p.SourceEntityID)
		}

		newID, err = rebuildFromSnapshot(tx, et, rev.Snapshot)
		if err != nil {
			return fmt.Errorf("rebuild: %w", err)
		}

		if err := repository.RepointRedirect(tx, et, p.SourceEntityID, newID); err != nil {
			return err
		}

		note := fmt.Sprintf("unmerge of proposal %d", p.ID)
		rebuiltSnap, err := takeSnapshot(tx, et, newID)
		if err != nil {
			return err
		}
		if err := writeRevision(tx, et, newID, model.RevisionActionReverted, rebuiltSnap, nil, actorID, note); err != nil {
			return err
		}
		// The proposal names the target as it stood when the merge ran. On
		// 2026-09-28 five of twelve cross-medium unmerges failed with "record
		// not found" because that target had since been merged on into another
		// work; the children to redistribute live on the current survivor.
		target, _, err := s.resolve.Resolve(ctx, et, p.TargetEntityID)
		if err != nil {
			return err
		}
		targetSnap, err := takeSnapshot(tx, et, target)
		if err != nil {
			return err
		}
		if err := writeRevision(tx, et, target, model.RevisionActionReverted, targetSnap, nil, actorID, note); err != nil {
			return err
		}

		return tx.Model(p).Update("note", appendNote(p.Note, fmt.Sprintf("unmerged into new entity %d", newID))).Error
	})
	return newID, err
}

func rebuildFromSnapshot(tx *gorm.DB, entityType int16, raw []byte) (int64, error) {
	switch entityType {
	case model.EntityTypePerson:
		var snap personSnapshot
		if err := json.Unmarshal(raw, &snap); err != nil {
			return 0, err
		}
		p := snap.Person
		p.ID = 0
		p.PrimaryCreditNameID = nil
		if err := tx.Create(&p).Error; err != nil {
			return 0, err
		}
		var primaryNew *int64
		for _, ns := range snap.CreditNames {
			n := ns.CreditName
			oldNameID := n.ID
			n.ID = 0
			n.PersonID = &p.ID
			if err := tx.Create(&n).Error; err != nil {
				return 0, err
			}
			if snap.Person.PrimaryCreditNameID != nil && *snap.Person.PrimaryCreditNameID == oldNameID {
				primaryNew = &n.ID
			}
			for _, a := range ns.Aliases {
				a.ID = 0
				a.CreditNameID = n.ID
				if err := tx.Create(&a).Error; err != nil {
					return 0, err
				}
			}
		}
		if primaryNew != nil {
			if err := tx.Model(&model.CatalogPerson{}).Where("id = ?", p.ID).
				Update("primary_credit_name_id", *primaryNew).Error; err != nil {
				return 0, err
			}
		}
		return p.ID, nil

	case model.EntityTypeCreditName:
		var snap creditNameSnapshot
		if err := json.Unmarshal(raw, &snap); err != nil {
			return 0, err
		}
		n := snap.CreditName
		n.ID = 0
		if err := tx.Create(&n).Error; err != nil {
			return 0, err
		}
		for _, a := range snap.Aliases {
			a.ID = 0
			a.CreditNameID = n.ID
			if err := tx.Create(&a).Error; err != nil {
				return 0, err
			}
		}
		return n.ID, nil

	case model.EntityTypeLabel:
		var snap labelSnapshot
		if err := json.Unmarshal(raw, &snap); err != nil {
			return 0, err
		}
		l := snap.Label
		l.ID = 0
		if err := tx.Create(&l).Error; err != nil {
			return 0, err
		}
		for _, a := range snap.Aliases {
			a.ID = 0
			a.LabelID = l.ID
			if err := tx.Create(&a).Error; err != nil {
				return 0, err
			}
		}
		return l.ID, nil

	case model.EntityTypeCharacter:
		var snap characterSnapshot
		if err := json.Unmarshal(raw, &snap); err != nil {
			return 0, err
		}
		c := snap.Character
		c.ID = 0
		if err := tx.Create(&c).Error; err != nil {
			return 0, err
		}
		for _, a := range snap.Aliases {
			a.ID = 0
			a.CharacterID = c.ID
			if err := tx.Create(&a).Error; err != nil {
				return 0, err
			}
		}
		return c.ID, nil

	case model.EntityTypeWork:
		var snap workSnapshot
		if err := json.Unmarshal(raw, &snap); err != nil {
			return 0, err
		}
		w := snap.Work
		w.ID = 0
		w.Site = nil
		w.ProductWorkID = nil
		if w.Status == model.WorkStatusMerged {
			w.Status = model.WorkStatusStub
		}
		if err := tx.Create(&w).Error; err != nil {
			return 0, err
		}
		for _, t := range snap.Titles {
			t.ID = 0
			t.WorkID = w.ID
			if err := tx.Create(&t).Error; err != nil {
				return 0, err
			}
		}
		return w.ID, nil
	}
	return 0, fmt.Errorf("catalog unmerge: unsupported entity type %d", entityType)
}
