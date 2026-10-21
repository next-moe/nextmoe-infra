package service

import (
	"context"
	"fmt"
	"log/slog"

	"api/internal/platform/catalog/discussion"
	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

type AccountPurged struct {
	Folders, FolderItems, Playtimes, WorkStates, Ratings, CoverVotes, EntityFollows, TagBlocks, Privacy int64
	DiscussionPosts, DiscussionWatches                                                                  int64
	MergeCopies                                                                                         int64
}

// Every kun_catalog table that names a user is purged here, except the edit
// history, which stays on purpose: catalog_work.owner_user_id (the claim),
// catalog_claim_event, catalog_revision, and the editing engine's proposal,
// amendment and revision rows. OAuth has already renamed the account they name.
// Discussion posts are blanked rather than deleted, so replies keep their place.
// A new per-user table belongs in this function or in that list.
var purgedUserTables = []string{
	"catalog_user_work_rating", "catalog_user_folder", "catalog_user_folder_item", "catalog_user_playtime",
	"catalog_user_work_state", "catalog_cover_vote", "catalog_user_entity_follow", "catalog_user_tag_block",
}

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
		if out.Ratings, err = deleteRatingsTx(tx, "actor_uid = ?", uid); err != nil {
			return err
		}
		for _, step := range []struct {
			model any
			n     *int64
		}{
			{&model.CatalogUserPlaytime{}, &out.Playtimes},
			{&model.CatalogUserWorkState{}, &out.WorkStates},
			{&model.CatalogCoverVote{}, &out.CoverVotes},
			{&model.CatalogUserEntityFollow{}, &out.EntityFollows},
			{&model.CatalogUserTagBlock{}, &out.TagBlocks},
			{&model.CatalogUserPrivacy{}, &out.Privacy},
		} {
			res := tx.Where("actor_uid = ?", uid).Delete(step.model)
			if res.Error != nil {
				return res.Error
			}
			*step.n = res.RowsAffected
		}
		res := tx.Model(&discussion.Post{}).
			Where("author_uid = ? AND status <> ?", uid, discussion.StatusDeleted).
			Updates(map[string]any{"status": discussion.StatusDeleted, "body": ""})
		if res.Error != nil {
			return res.Error
		}
		out.DiscussionPosts = res.RowsAffected
		res = tx.Where("user_id = ?", uid).Delete(&discussion.Watcher{})
		if res.Error != nil {
			return res.Error
		}
		out.DiscussionWatches = res.RowsAffected
		// A merge keeps copies of the rows it drops (catalog_merge_archive) and
		// of the rows it moves (catalog_merge_movement) so it can be undone;
		// an undo after the account is erased would bring them back.
		for _, table := range []string{"catalog_merge_archive", "catalog_merge_movement"} {
			res = tx.Exec(`DELETE FROM `+table+` WHERE table_name IN ?
				AND (row->>'actor_uid' = ? OR row->>'owner_uid' = ?)`,
				purgedUserTables, fmt.Sprint(uid), fmt.Sprint(uid))
			if res.Error != nil {
				return res.Error
			}
			out.MergeCopies += res.RowsAffected
		}
		return nil
	})
	if err != nil {
		return AccountPurged{}, err
	}
	if out != (AccountPurged{}) {
		slog.Info("catalog account purge", "user_id", uid,
			"folders", out.Folders, "folder_items", out.FolderItems, "playtimes", out.Playtimes,
			"work_states", out.WorkStates, "ratings", out.Ratings, "cover_votes", out.CoverVotes, "entity_follows", out.EntityFollows,
			"tag_blocks", out.TagBlocks, "privacy", out.Privacy,
			"discussion_posts", out.DiscussionPosts, "discussion_watches", out.DiscussionWatches,
			"merge_copies", out.MergeCopies)
	}
	return out, nil
}
