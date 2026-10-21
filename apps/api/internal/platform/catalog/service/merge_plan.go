package service

import (
	"errors"
	"fmt"
	"strings"

	"api/internal/platform/catalog/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var (
	ErrMergeMode = errors.New("catalog merge: unsupported merge mode")
	// ErrAbsorbVNDBAnchor refuses an absorb whose source holds a VNDB exact
	// anchor: VNDB keeps one VN per game across editions, so such a source is a
	// duplicate of the target, not an edition of it, and takes a normal merge.
	ErrAbsorbVNDBAnchor = errors.New("catalog merge: absorb refused, the source work holds a VNDB exact anchor")
)

const (
	mergeModeAbsorb    = "absorb"
	absorbRedirectNote = "absorb: "
	absorbMatchedBy    = "merge:absorb"
)

type mergePlan struct {
	entityType   int16
	src, dst     int64
	proposalID   int64
	absorb       bool
	hideReleases []int64
}

func planFromProposal(p *model.CatalogMergeProposal) (mergePlan, error) {
	plan := mergePlan{entityType: p.EntityType, src: p.SourceEntityID, dst: p.TargetEntityID, proposalID: p.ID}
	doc, err := parseMergeDoc(p.FieldResolution)
	if err != nil {
		return plan, err
	}
	plan.hideReleases = doc.HideReleases
	switch doc.mode {
	case "":
	case mergeModeAbsorb:
		if p.EntityType != model.EntityTypeWork {
			return plan, fmt.Errorf("%w: absorb applies to works only, proposal %d is entity type %d",
				ErrMergeMode, p.ID, p.EntityType)
		}
		plan.absorb = true
	default:
		return plan, fmt.Errorf("%w: %q on proposal %d", ErrMergeMode, doc.mode, p.ID)
	}
	return plan, nil
}

func absorbResolution() datatypes.JSON {
	return datatypes.JSON(`{"mode":"` + mergeModeAbsorb + `"}`)
}

func redirectReason(plan mergePlan) string {
	reason := fmt.Sprintf("merge proposal %d", plan.proposalID)
	if plan.absorb {
		return absorbRedirectNote + reason
	}
	return reason
}

func redirectIsAbsorb(reason string) bool {
	return strings.HasPrefix(reason, absorbRedirectNote)
}

func assertAbsorbable(db *gorm.DB, src int64) error {
	var held bool
	if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM catalog_external_ref
		WHERE entity_type = ? AND entity_id = ? AND source_id = ? AND link_kind = ?)`,
		model.EntityTypeWork, src, model.SourceVNDB, model.LinkKindExact).Scan(&held).Error; err != nil {
		return err
	}
	if held {
		return fmt.Errorf("%w: work %d", ErrAbsorbVNDBAnchor, src)
	}
	return nil
}

func humanLaneSQL(column string) string {
	return fmt.Sprintf("COALESCE(%s IN (%d, %d), false)", column, model.SourceUser, model.SourceCurated)
}
