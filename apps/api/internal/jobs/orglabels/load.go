package orglabels

import (
	"fmt"

	"gorm.io/gorm"
)

func loadLabelWorks(db *gorm.DB) (map[int64][]int64, error) {
	var rows []struct {
		WorkID  int64 `gorm:"column:work_id"`
		LabelID int64 `gorm:"column:label_id"`
	}
	if err := db.Raw(`
		SELECT DISTINCT wl.work_id, wl.label_id FROM catalog_work_label wl
		    JOIN catalog_label l ON l.id = wl.label_id AND l.deleted_at IS NULL`,
	).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[int64][]int64, len(rows))
	for _, r := range rows {
		m[r.WorkID] = append(m[r.WorkID], r.LabelID)
	}
	return m, nil
}

func loadLabelNorms(db *gorm.DB) (map[string][]int64, error) {
	var rows []struct {
		Norm    string `gorm:"column:norm"`
		LabelID int64  `gorm:"column:label_id"`
	}
	if err := db.Raw(`
		SELECT display_name_norm AS norm, id AS label_id FROM catalog_label
		    WHERE display_name_norm <> '' AND deleted_at IS NULL
		UNION
		SELECT a.name_norm AS norm, a.label_id FROM catalog_label_alias a
		    JOIN catalog_label l ON l.id = a.label_id AND l.deleted_at IS NULL
		    WHERE a.name_norm <> ''
	`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[string][]int64, len(rows))
	for _, r := range rows {
		m[r.Norm] = append(m[r.Norm], r.LabelID)
	}
	return m, nil
}

func loadLabelDisplayNorms(db *gorm.DB) (map[string][]int64, error) {
	var rows []struct {
		Norm    string `gorm:"column:norm"`
		LabelID int64  `gorm:"column:label_id"`
	}
	if err := db.Raw(`
		SELECT display_name_norm AS norm, id AS label_id FROM catalog_label
		    WHERE display_name_norm <> '' AND deleted_at IS NULL
	`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[string][]int64, len(rows))
	for _, r := range rows {
		m[r.Norm] = append(m[r.Norm], r.LabelID)
	}
	return m, nil
}

type existingAnchors struct {
	byExtID        map[string]int16
	claimedByLabel map[int64]bool
}

func loadExistingAnchors(db *gorm.DB, source int16) (*existingAnchors, error) {
	var rows []struct {
		ExternalID string `gorm:"column:external_id"`
		EntityID   int64  `gorm:"column:entity_id"`
		LinkKind   int16  `gorm:"column:link_kind"`
	}
	if err := db.Raw(`
		SELECT external_id, entity_id, link_kind FROM catalog_external_ref
		    WHERE entity_type = 3 AND source_id = ? AND link_kind IN (0, 1)`, source,
	).Scan(&rows).Error; err != nil {
		return nil, err
	}
	ea := &existingAnchors{
		byExtID:        make(map[string]int16, len(rows)),
		claimedByLabel: make(map[int64]bool),
	}
	for _, r := range rows {
		ea.byExtID[r.ExternalID] = r.LinkKind
		ea.claimedByLabel[r.EntityID] = true
	}
	return ea, nil
}

func rejKey(labelID int64, externalID string) string {
	return fmt.Sprintf("%d\x00%s", labelID, externalID)
}

func loadRejections(db *gorm.DB, source int16) (map[string]struct{}, error) {
	var rows []struct {
		EntityID   int64  `gorm:"column:entity_id"`
		ExternalID string `gorm:"column:external_id"`
	}
	if err := db.Raw(`
		SELECT entity_id, external_id FROM catalog_match_rejection
		    WHERE entity_type = 3 AND source_id = ?`, source,
	).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		out[rejKey(r.EntityID, r.ExternalID)] = struct{}{}
	}
	return out, nil
}
