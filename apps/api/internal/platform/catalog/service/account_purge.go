package service

import (
	"context"
	"log/slog"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

type AccountPurged struct {
	Folders, FolderItems, Playtimes, WorkStates, CoverVotes int64
}

// Every kun_catalog table that names a user is purged here, except the edit
// history, which stays on purpose: catalog_work.owner_user_id (the claim),
// catalog_claim_event, catalog_revision, and the editing engine's proposal,
// amendment and revision rows. OAuth has already renamed the account they name.
// A new per-user table belongs in this function or in that list.
func PurgeAccount(ctx context.Context, db *gorm.DB, uid int64) (AccountPurged, error) {
	var out AccountPurged
	if uid <= 0 {
		return out, ErrFolderActorRequired
	}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		if out.Folders, out.FolderItems, err = purgeOwnerFoldersTx(tx, uid); err != nil {
			return err
		}
		for _, step := range []struct {
			model any
			n     *int64
		}{
			{&model.CatalogUserPlaytime{}, &out.Playtimes},
			{&model.CatalogUserWorkState{}, &out.WorkStates},
			{&model.CatalogCoverVote{}, &out.CoverVotes},
		} {
			res := tx.Where("actor_uid = ?", uid).Delete(step.model)
			if res.Error != nil {
				return res.Error
			}
			*step.n = res.RowsAffected
		}
		return nil
	})
	if err != nil {
		return AccountPurged{}, err
	}
	if out != (AccountPurged{}) {
		slog.Info("catalog account purge", "user_id", uid,
			"folders", out.Folders, "folder_items", out.FolderItems, "playtimes", out.Playtimes,
			"work_states", out.WorkStates, "cover_votes", out.CoverVotes)
	}
	return out, nil
}
