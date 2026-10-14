package repository

import (
	"gorm.io/gorm"
)

func MergeUsage(tx *gorm.DB, entityType int16, sourceID, targetID int64) error {
	if err := tx.Exec(`
		UPDATE catalog_entity_usage t
		   SET first_used_at     = LEAST(t.first_used_at, s.first_used_at),
		       last_confirmed_at = GREATEST(t.last_confirmed_at, s.last_confirmed_at)
		  FROM catalog_entity_usage s
		 WHERE t.entity_type = ? AND t.entity_id = ?
		   AND s.entity_type = t.entity_type AND s.entity_id = ? AND s.site = t.site
	`, entityType, targetID, sourceID).Error; err != nil {
		return err
	}
	if err := tx.Exec(`
		UPDATE catalog_entity_usage s SET entity_id = ?
		 WHERE s.entity_type = ? AND s.entity_id = ?
		   AND NOT EXISTS (SELECT 1 FROM catalog_entity_usage t
		                    WHERE t.entity_type = s.entity_type AND t.entity_id = ? AND t.site = s.site)
	`, targetID, entityType, sourceID, targetID).Error; err != nil {
		return err
	}
	return tx.Exec(`DELETE FROM catalog_entity_usage WHERE entity_type = ? AND entity_id = ?`,
		entityType, sourceID).Error
}
