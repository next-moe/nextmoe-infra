package orglabels

import (
	"context"
	"encoding/json"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func edgeKindFor(labelKind int16) int16 {
	switch labelKind {
	case model.LabelKindGameBrand:
		return model.WorkLabelKindBrand
	case model.LabelKindPublisher:
		return model.WorkLabelKindPublisher
	default:
		return model.WorkLabelKindCircle
	}
}

func mintLabel(ctx context.Context, db *gorm.DB, source int16, o *orgRec) (int, error) {
	rule := ruleSetFor(source).newLabel
	edgeKind := edgeKindFor(o.newKind)
	edgesWritten := 0

	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		label := model.CatalogLabel{
			DisplayName:     o.displayName,
			Latin:           o.latin,
			Lang:            o.lang,
			Kind:            o.newKind,
			FieldProvenance: mintProvenance(source, o),
		}
		if err := tx.Create(&label).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.CatalogExternalRef{
			EntityType: model.EntityTypeLabel,
			EntityID:   label.ID,
			SourceID:   source,
			ExternalID: o.extID,
			LinkKind:   model.LinkKindExact,
			MatchedBy:  rule,
		}).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.CatalogRevision{
			EntityType: model.EntityTypeLabel,
			EntityID:   label.ID,
			Revision:   1,
			Action:     model.RevisionActionImported,
			Snapshot:   labelSnapshot(label),
			IsMinor:    false,
		}).Error; err != nil {
			return err
		}
		attrib := o.attribWorks
		if !o.editionAware {
			attrib = o.works
		}
		edges := make([]model.CatalogWorkLabel, 0, len(attrib))
		src := source
		for _, w := range attrib {
			edges = append(edges, model.CatalogWorkLabel{
				WorkID: w, LabelID: label.ID, Kind: edgeKind, SourceID: &src,
			})
		}
		if len(edges) > 0 {
			res := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(edges, 1000)
			if res.Error != nil {
				return res.Error
			}
			edgesWritten = int(res.RowsAffected)
			if edgesWritten > 0 {
				if err := repository.TouchWorks(ctx, tx, attrib); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return edgesWritten, err
}

func mintProvenance(source int16, o *orgRec) datatypes.JSON {
	entry := []map[string]string{{"source": srcKey(source), "at": nowUTC().Format("2006-01-02T15:04:05Z")}}
	prov := map[string]any{
		"display_name": entry,
		"kind":         entry,
	}
	if o.lang != "" {
		prov["lang"] = entry
	}
	if o.latin != nil && *o.latin != "" {
		prov["latin"] = entry
	}
	b, _ := json.Marshal(prov)
	return b
}

func labelSnapshot(label model.CatalogLabel) datatypes.JSON {
	b, _ := json.Marshal(map[string]any{"label": label, "aliases": []any{}})
	return b
}

func ruleSetFor(source int16) ruleSet {
	switch source {
	case sourceVNDB:
		return ruleSet{ruleVNDBCoworks, ruleVNDBCoworkName, ruleVNDBNameOnly, ruleVNDBNameLoose, ruleVNDBNew}
	case sourceBangumi:
		return ruleSet{ruleBGMCoworks, ruleBGMCoworkName, ruleBGMNameOnly, ruleBGMNameLoose, ruleBGMNew}
	default:
		return ruleSet{ruleEGCoworks, ruleEGCoworkName, ruleEGNameOnly, ruleEGNameLoose, ruleEGNew}
	}
}
