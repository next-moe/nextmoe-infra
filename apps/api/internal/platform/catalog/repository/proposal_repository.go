package repository

import (
	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

func LockProposal(tx *gorm.DB, id int64) (*model.CatalogMergeProposal, error) {
	var row model.CatalogMergeProposal
	err := tx.Raw(`SELECT * FROM catalog_merge_proposal WHERE id = ? FOR UPDATE`, id).Scan(&row).Error
	if err != nil {
		return nil, err
	}
	if row.ID == 0 {
		return nil, nil
	}
	return &row, nil
}
