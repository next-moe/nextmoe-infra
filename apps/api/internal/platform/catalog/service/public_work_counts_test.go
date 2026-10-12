package service

import (
	"testing"

	"api/internal/platform/catalog/model"
)

func TestWorkChipCountsMatchTheirLandingPages(t *testing.T) {
	cleanTables(t)
	cleanTagTables(t)
	cleanTaxonomyTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	safe := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "ChipSafe")
	r18 := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "ChipR18")
	stub := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusStub, "ChipStub")
	for i, id := range []int64{safe.ID, r18.ID, stub.ID} {
		claimLive(t, id, int64(9320+i))
	}

	brandID := addWorkLabel(t, safe.ID, "Alcot", model.LabelKindGameBrand, model.WorkLabelKindBrand)
	for _, w := range []int64{r18.ID, stub.ID} {
		if err := testDB.Create(&model.CatalogWorkLabel{
			WorkID: w, LabelID: brandID, Kind: model.WorkLabelKindBrand,
		}).Error; err != nil {
			t.Fatalf("attach label to %d: %v", w, err)
		}
	}

	engineID := createEngine(t, "KiriKiri")
	for _, w := range []int64{safe.ID, r18.ID, stub.ID} {
		attachEngine(t, w, engineID)
	}

	const srcBangumi int16 = 3
	tagID := createCanonicalTag(t, "fantasy", model.TagTierCore, model.TagKindContent)
	if err := testDB.Create(&model.CatalogTagSourceMap{
		SourceID: srcBangumi, SourceName: "ファンタジー", TagID: tagID,
	}).Error; err != nil {
		t.Fatalf("map source tag: %v", err)
	}
	for _, w := range []int64{safe.ID, r18.ID, stub.ID} {
		for _, name := range []string{"ファンタジー", "未映射タグ"} {
			if err := testDB.Create(&model.CatalogWorkTag{
				WorkID: w, Name: name, Count: 1, SourceID: srcBangumi,
			}).Error; err != nil {
				t.Fatalf("work tag %s: %v", name, err)
			}
		}
	}

	for _, tc := range []struct {
		nsfw bool
		want int
	}{{false, 1}, {true, 2}} {
		rec, found, err := svc.WorkDetail(ctx, safe.ID, PublicInclude{}, tc.nsfw, 0, PublicFields{})
		if err != nil || !found {
			t.Fatalf("nsfw=%v: WorkDetail = %v, %v", tc.nsfw, found, err)
		}

		if len(rec.Labels) != 1 || rec.Labels[0].WorkCount != tc.want {
			t.Fatalf("nsfw=%v: labels[] = %+v, want one chip with work_count %d", tc.nsfw, rec.Labels, tc.want)
		}
		assertCountMatchesWorksList(t, svc,
			WorksListFilter{Sort: "id", LabelID: brandID, NSFW: tc.nsfw}, rec.Labels[0].WorkCount)

		if len(rec.Engines) != 1 || rec.Engines[0].WorkCount != tc.want {
			t.Fatalf("nsfw=%v: engines[] = %+v, want one chip with work_count %d", tc.nsfw, rec.Engines, tc.want)
		}
		assertCountMatchesWorksList(t, svc,
			WorksListFilter{Sort: "id", EngineID: engineID, NSFW: tc.nsfw}, rec.Engines[0].WorkCount)

		var mapped, unmapped int
		for _, tag := range rec.Tags {
			if tag.CanonicalID == 0 {
				unmapped++
				if tag.WorkCount != nil {
					t.Fatalf("nsfw=%v: unmapped tag %q carries work_count %d — it has no landing page",
						tc.nsfw, tag.Name, *tag.WorkCount)
				}
				continue
			}
			mapped++
			if tag.WorkCount == nil || *tag.WorkCount != tc.want {
				t.Fatalf("nsfw=%v: mapped tag %q work_count = %v, want %d", tc.nsfw, tag.Name, tag.WorkCount, tc.want)
			}
			assertCountMatchesWorksList(t, svc,
				WorksListFilter{Sort: "id", TagIDs: []int64{tagID}, NSFW: tc.nsfw}, *tag.WorkCount)
		}
		if mapped != 1 || unmapped != 1 {
			t.Fatalf("nsfw=%v: tags[] = %+v, want one mapped + one unmapped", tc.nsfw, rec.Tags)
		}

		tagRec, ok, err := svc.TagDetail(ctx, tagID, false, tc.nsfw, 20, 0)
		if err != nil || !ok {
			t.Fatalf("nsfw=%v: TagDetail = %v, %v", tc.nsfw, ok, err)
		}
		if tagRec.WorkCount != tc.want {
			t.Fatalf("nsfw=%v: tags/{id}.work_count = %d, want %d (same aggregate as the chip)",
				tc.nsfw, tagRec.WorkCount, tc.want)
		}
		engRec, ok, err := svc.EngineDetail(ctx, engineID, tc.nsfw)
		if err != nil || !ok {
			t.Fatalf("nsfw=%v: EngineDetail = %v, %v", tc.nsfw, ok, err)
		}
		if engRec.WorkCount != tc.want {
			t.Fatalf("nsfw=%v: engines/{id}.work_count = %d, want %d", tc.nsfw, engRec.WorkCount, tc.want)
		}

		page, err := svc.WorksList(ctx, WorksListFilter{
			Sort: "id", NSFW: tc.nsfw, Include: ParseWorksListInclude("labels"),
		}, "", 100)
		if err != nil {
			t.Fatalf("nsfw=%v: WorksList include=labels: %v", tc.nsfw, err)
		}
		var seen bool
		for _, it := range page.Items {
			if it.ID != safe.ID {
				continue
			}
			seen = true
			if len(it.Labels) != 1 || it.Labels[0].WorkCount != rec.Labels[0].WorkCount {
				t.Fatalf("nsfw=%v: list labels = %+v, detail said %d",
					tc.nsfw, it.Labels, rec.Labels[0].WorkCount)
			}
		}
		if !seen {
			t.Fatalf("nsfw=%v: the work under test is missing from the list page", tc.nsfw)
		}
	}
}
