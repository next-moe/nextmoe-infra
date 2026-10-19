package personphotos

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func resolveSourceID(ctx context.Context, db *gorm.DB) (int16, error) {
	var id int16
	if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_source WHERE key = ?`, sourceKey).Scan(&id).Error; err != nil {
		return 0, fmt.Errorf("resolve %s source: %w", sourceKey, err)
	}
	if id == 0 {
		return 0, fmt.Errorf("registry not seeded (%s source not found in catalog_source)", sourceKey)
	}
	return id, nil
}

type candidate struct {
	PersonID        int64          `gorm:"column:person_id"`
	ExternalID      string         `gorm:"column:external_id"`
	FieldProvenance datatypes.JSON `gorm:"column:field_provenance"`
}

func loadCandidates(ctx context.Context, db *gorm.DB, sourceID int16) ([]candidate, error) {
	var out []candidate
	err := db.WithContext(ctx).Raw(`
		SELECT DISTINCT ON (p.id)
		       p.id AS person_id, r.external_id AS external_id, p.field_provenance AS field_provenance
		FROM catalog_person p
		JOIN catalog_external_ref r ON r.entity_id = p.id
		     AND r.entity_type = ? AND r.source_id = ? AND r.link_kind = ?
		WHERE p.deleted_at IS NULL AND p.photo_hash = ''
		  AND `+editspec.NotHumanFieldSQL("p.field_provenance", provField)+`
		ORDER BY p.id, r.external_id`,
		model.EntityTypePerson, sourceID, model.LinkKindExact).Scan(&out).Error
	if err != nil {
		return nil, err
	}
	return out, nil
}

func writeIDs(path string, cands []candidate, m *mirror) (int, error) {
	seen := map[string]bool{}
	ids := make([]string, 0, len(cands))
	for _, c := range cands {
		if seen[c.ExternalID] || m.has(c.ExternalID) {
			continue
		}
		seen[c.ExternalID] = true
		ids = append(ids, c.ExternalID)
	}
	sort.Strings(ids)
	body := ""
	if len(ids) > 0 {
		body = strings.Join(ids, "\n") + "\n"
	}
	return len(ids), os.WriteFile(path, []byte(body), 0o644)
}
