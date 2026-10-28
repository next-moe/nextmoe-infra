package editspec_test

import (
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
)

func TestEditorCoverSaveKeepsTheVotesOnKeptCovers(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "投票の残る作品")
	mergeField(t, e, work.ID, editspec.FieldWorkCovers, []any{
		map[string]any{"image_hash": "vote-a"}, map[string]any{"image_hash": "vote-b"},
	})
	var kept model.CatalogWorkCover
	if err := testDB.Where("work_id = ? AND image_hash = ?", work.ID, "vote-a").Take(&kept).Error; err != nil {
		t.Fatal(err)
	}
	if err := testDB.Create(&model.CatalogCoverVote{WorkID: work.ID, CoverID: kept.ID, ActorUID: 77, Site: "kungal"}).Error; err != nil {
		t.Fatal(err)
	}

	mergeField(t, e, work.ID, editspec.FieldWorkCovers, []any{
		map[string]any{"image_hash": "vote-c"}, map[string]any{"image_hash": "vote-a", "sexual": int64(1)},
	})

	var rows []model.CatalogWorkCover
	if err := testDB.Where("work_id = ? AND source_id = ?", work.ID, curatedSource).Order("sort_order").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ImageHash != "vote-c" || rows[1].ImageHash != "vote-a" {
		t.Fatalf("rows = %+v, want vote-c then vote-a, vote-b gone", rows)
	}
	if rows[1].ID != kept.ID || rows[1].SortOrder != 1 || rows[1].Sexual != 1 {
		t.Fatalf("kept cover = %+v, want id %d updated in place", rows[1], kept.ID)
	}
	var votes int64
	if err := testDB.Model(&model.CatalogCoverVote{}).Where("cover_id = ?", kept.ID).Count(&votes).Error; err != nil {
		t.Fatal(err)
	}
	if votes != 1 {
		t.Fatalf("votes on the kept cover = %d, want 1: a save must not erase them", votes)
	}
}
