package service

import (
	"context"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
)

func suppressReleaseLabel(t *testing.T, releaseID, labelID int64, kind int16) {
	t.Helper()
	if err := testDB.Exec(`INSERT INTO edit_suppressed_row (entity_type, entity_id, field_key, identity_key, created_at)
		VALUES (?, ?, ?, ?, now())`, editspec.TypeRelease, releaseID, editspec.FieldReleaseLabels,
		editspec.ReleaseLabelIdentity(labelID, kind)).Error; err != nil {
		t.Fatalf("suppress release label: %v", err)
	}
}

func TestSuppressedReleaseLabelLeavesEveryFace(t *testing.T) {
	cleanTables(t)
	if err := testDB.Exec("TRUNCATE edit_suppressed_row RESTART IDENTITY").Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	svc := newPublicSvcCDN()

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "Hidden Chip Work")
	claimLive(t, w.ID, 9501)
	rel := createRelease(t, w.ID, 2024, 5, 5)
	kept := createLabel(t, "Kept Publisher", model.LabelKindGameBrand)
	hidden := createLabel(t, "Wrong Developer", model.LabelKindGameBrand)
	addReleaseLabel(t, rel.ID, kept, model.WorkLabelKindPublisher)
	addReleaseLabel(t, rel.ID, hidden, model.WorkLabelKindDeveloper)
	suppressReleaseLabel(t, rel.ID, hidden, model.WorkLabelKindDeveloper)

	rec, found, err := svc.WorkDetail(ctx, w.ID, PublicInclude{}, false, 0, PublicFields{})
	if err != nil || !found {
		t.Fatalf("WorkDetail = %v, %v", found, err)
	}
	if got := rec.Releases[0].Labels; len(got) != 1 || got[0].ID != kept {
		t.Fatalf("work detail release labels = %+v, want only the kept publisher", got)
	}
	feed, err := svc.ReleaseFeed(ctx, ReleaseFeedFilter{}, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Items) != 1 || len(feed.Items[0].Labels) != 1 || feed.Items[0].Labels[0].ID != kept {
		t.Fatalf("feed labels = %+v, want only the kept publisher", feed.Items)
	}
}

func TestPlatformFiltersMatchEveryListedPlatform(t *testing.T) {
	cleanTables(t)
	ctx := context.Background()
	svc := newPublicSvcCDN()

	multi := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "Multi Platform")
	claimLive(t, multi.ID, 9502)
	multiRel := createRelease(t, multi.ID, 2023, 1, 1)
	headOnly := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "Head Only")
	claimLive(t, headOnly.ID, 9503)
	headRel := createRelease(t, headOnly.ID, 2023, 2, 2)
	if err := testDB.Exec(`UPDATE catalog_release SET platform = 'win',
		extra = '{"platforms":["win","ps4"],"languages":["ja","en"],"freeware":false,"official":true,"minage":18}'
		WHERE id = ?`, multiRel.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := testDB.Exec(`UPDATE catalog_release SET platform = 'swi' WHERE id = ?`, headRel.ID).Error; err != nil {
		t.Fatal(err)
	}

	page, err := svc.WorksList(ctx, WorksListFilter{Sort: "id", OLang: PublicOLang{All: true}, Platform: "ps4"}, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != multi.ID {
		t.Fatalf("works?platform=ps4 = %+v, want the work whose release lists ps4 second", page.Items)
	}
	feed, err := svc.ReleaseFeed(ctx, ReleaseFeedFilter{Platform: "ps4"}, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Items) != 1 || feed.Items[0].ID != multiRel.ID {
		t.Fatalf("releases?platform=ps4 = %+v, want the multi-platform release", feed.Items)
	}
	it := feed.Items[0]
	if len(it.Languages) != 2 || it.Freeware == nil || *it.Freeware || it.Official == nil || !*it.Official ||
		it.MinAge == nil || *it.MinAge != 18 {
		t.Fatalf("feed attributes = %+v", it.PublicReleaseAttributes)
	}

	all, err := svc.ReleaseFeed(ctx, ReleaseFeedFilter{}, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range all.Items {
		if it.ID == headRel.ID && (len(it.Platforms) != 1 || it.Platforms[0] != "swi") {
			t.Fatalf("a release with only a platform head lists %v, want [swi]", it.Platforms)
		}
	}
}
