package service

import (
	"testing"
	"time"

	"api/internal/platform/catalog/model"
)

type populationFixture struct {
	host, brand, imprint, bannedLabel int64
	engine, tag, bannedTag, series    int64
}

func seedPopulation(t *testing.T) populationFixture {
	t.Helper()
	cleanTables(t)
	cleanTagTables(t)
	cleanTaxonomyTables(t)

	f := populationFixture{
		brand:       mkLabel(t, "Alcot", ""),
		imprint:     mkLabel(t, "Alcot Honey", ""),
		bannedLabel: mkLabel(t, "Banned Only", ""),
		engine:      createEngine(t, "KiriKiri"),
		tag:         createCanonicalTag(t, "fantasy", model.TagTierCore, model.TagKindContent),
		bannedTag:   createCanonicalTag(t, "banned only", model.TagTierCore, model.TagKindContent),
	}
	relateMirrored(t, f.brand, f.imprint, model.LabelRelationImprint)
	series := &model.CatalogSeries{DisplayName: "Shuffle", SourceID: srcVNDB, ExternalID: "population"}
	if err := testDB.Create(series).Error; err != nil {
		t.Fatalf("create series: %v", err)
	}
	f.series = series.ID

	const srcBangumi int16 = 3
	for name, tagID := range map[string]int64{"ファンタジー": f.tag, "封禁": f.bannedTag} {
		if err := testDB.Create(&model.CatalogTagSourceMap{SourceID: srcBangumi, SourceName: name, TagID: tagID}).Error; err != nil {
			t.Fatalf("map source tag %s: %v", name, err)
		}
	}
	link := func(workID, labelID int64, tags ...string) {
		t.Helper()
		if err := testDB.Create(&model.CatalogWorkLabel{WorkID: workID, LabelID: labelID, Kind: model.WorkLabelKindBrand}).Error; err != nil {
			t.Fatalf("label %d on %d: %v", labelID, workID, err)
		}
		for _, name := range tags {
			if err := testDB.Create(&model.CatalogWorkTag{WorkID: workID, Name: name, Count: 1, SourceID: srcBangumi}).Error; err != nil {
				t.Fatalf("tag %s on %d: %v", name, workID, err)
			}
		}
	}
	member := func(workID int64) {
		t.Helper()
		link(workID, f.brand, "ファンタジー")
		attachEngine(t, workID, f.engine)
		if err := testDB.Create(&model.CatalogSeriesMember{SeriesID: f.series, WorkID: workID}).Error; err != nil {
			t.Fatalf("series member %d: %v", workID, err)
		}
	}
	update := func(sql string, args ...any) {
		t.Helper()
		if err := testDB.Exec(sql, args...).Error; err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	product := int64(9600)
	claim := func(workID int64, state *int16) {
		t.Helper()
		product++
		claimWork(t, workID, "galgame_wiki", product)
		setClaimState(t, workID, state)
	}
	for _, c := range []struct {
		name    string
		claimed bool
		state   *int16
	}{
		{"live", true, i16(model.ClaimStateLive)},
		{"live-null-state", true, nil},
		{"draft", true, i16(model.ClaimStateDraft)},
		{"pending", true, i16(model.ClaimStatePending)},
		{"declined", true, i16(model.ClaimStateDeclined)},
		{"hidden", true, i16(model.ClaimStateHidden)},
		{"unclaimed", false, nil},
	} {
		for _, rating := range []int16{model.ContentRatingAllAges, model.ContentRatingR18} {
			w := createWorkX(t, galgameMediumID, rating, model.WorkStatusLive, c.name+"-"+contentRatingKey(rating))
			if c.claimed {
				claim(w.ID, c.state)
			}
			member(w.ID)
			switch {
			case c.name == "live" && rating == model.ContentRatingAllAges:
				f.host = w.ID
			case c.name == "live":
				update(`UPDATE catalog_work SET display_nsfw = true WHERE id = ?`, w.ID)
			case c.name == "hidden" && rating == model.ContentRatingAllAges:
				link(w.ID, f.bannedLabel, "封禁")
			}
		}
	}

	english := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "english-unclaimed")
	update(`UPDATE catalog_work SET olang = 'en' WHERE id = ?`, english.ID)
	member(english.ID)

	stub := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusStub, "stub")
	deleted := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "deleted")
	asmr := createWorkX(t, 5, model.ContentRatingAllAges, model.WorkStatusLive, "asmr")
	update(`UPDATE catalog_work SET deleted_at = now() WHERE id = ?`, deleted.ID)
	for _, w := range []int64{stub.ID, deleted.ID, asmr.ID} {
		claim(w, i16(model.ClaimStateLive))
		member(w)
	}

	imprintDraft := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "imprint-draft")
	claim(imprintDraft.ID, i16(model.ClaimStateDraft))
	imprintLoose := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "imprint-unclaimed-r18")
	for _, w := range []int64{imprintDraft.ID, imprintLoose.ID} {
		link(w, f.imprint)
	}
	return f
}

func TestWorkCountIsTheWorksListPopulation(t *testing.T) {
	f := seedPopulation(t)
	svc := newPublicSvc()
	ctx := t.Context()

	same := func(face string, got int, lf WorksListFilter) {
		t.Helper()
		if want := worksListTotal(t, svc, lf); got != want {
			t.Fatalf("nsfw=%v %s = %d, but GET /works for the same filter holds %d", lf.NSFW, face, got, want)
		}
	}

	for nsfw, want := range map[bool]int{false: 7, true: 13} {
		eng, ok, err := svc.EngineDetail(ctx, f.engine, nsfw)
		if err != nil || !ok {
			t.Fatalf("EngineDetail: %v ok=%v", err, ok)
		}
		ser, ok, err := svc.SeriesDetail(ctx, f.series, false, nsfw, 20, 0)
		if err != nil || !ok {
			t.Fatalf("SeriesDetail: %v ok=%v", err, ok)
		}
		if eng.WorkCount != want || ser.WorkCount != want {
			t.Fatalf("nsfw=%v engine work_count %d, series work_count %d, want %d: every claim state but hidden, every language",
				nsfw, eng.WorkCount, ser.WorkCount, want)
		}
	}

	checkTags := func(stage string, nsfw bool) {
		t.Helper()
		tags, err := svc.TagsList(ctx, TagsListFilter{NSFW: nsfw}, "", 50)
		if err != nil {
			t.Fatalf("TagsList: %v", err)
		}
		for _, it := range tags.Items {
			same(stage+" tags lane", it.WorkCount, WorksListFilter{TagIDs: []int64{it.ID}, NSFW: nsfw})
		}
		withWorks, err := svc.TagsList(ctx, TagsListFilter{NSFW: nsfw, HasWorks: true}, "", 50)
		if err != nil {
			t.Fatalf("TagsList has_works: %v", err)
		}
		if len(withWorks.Items) != 1 || withWorks.Items[0].ID != f.tag {
			t.Fatalf("nsfw=%v %s has_works tags = %+v, want only %d", nsfw, stage, withWorks.Items, f.tag)
		}
		rec, ok, err := svc.TagDetail(ctx, f.tag, false, nsfw, 20, 0)
		if err != nil || !ok {
			t.Fatalf("TagDetail: %v ok=%v", err, ok)
		}
		same(stage+" tags/{id}", rec.WorkCount, WorksListFilter{TagIDs: []int64{f.tag}, NSFW: nsfw})
	}

	for _, nsfw := range []bool{false, true} {
		labels, err := svc.LabelsList(ctx, LabelsListFilter{NSFW: nsfw}, "", 50)
		if err != nil {
			t.Fatalf("LabelsList: %v", err)
		}
		withWorks, err := svc.LabelsList(ctx, LabelsListFilter{NSFW: nsfw, HasWorks: true}, "", 50)
		if err != nil {
			t.Fatalf("LabelsList has_works: %v", err)
		}
		kept := map[int64]bool{}
		for _, it := range withWorks.Items {
			kept[it.ID] = true
		}
		for _, it := range labels.Items {
			same("labels lane", it.WorkCount, WorksListFilter{LabelID: it.ID, NSFW: nsfw})
			if kept[it.ID] != (it.WorkCount > 0) {
				t.Fatalf("nsfw=%v label %d: has_works kept=%v with work_count %d", nsfw, it.ID, kept[it.ID], it.WorkCount)
			}
		}
		if kept[f.bannedLabel] || !kept[f.imprint] {
			t.Fatalf("nsfw=%v has_works = %v, want the imprint kept and the banned-only label dropped", nsfw, kept)
		}

		label, ok, err := svc.Label(ctx, f.brand, false, nsfw, 50, 0)
		if err != nil || !ok {
			t.Fatalf("Label: %v ok=%v", err, ok)
		}
		same("labels/{id}", label.WorkCount, WorksListFilter{LabelID: f.brand, NSFW: nsfw})
		same("labels/{id} work_count + imprint_work_count", label.WorkCount+label.ImprintWorkCount,
			WorksListFilter{LabelID: f.brand, LabelRollup: true, NSFW: nsfw})

		graph, ok, err := svc.LabelRelationGraph(ctx, f.brand, nsfw)
		if err != nil || !ok || len(graph.Nodes) != 2 {
			t.Fatalf("LabelRelationGraph: %v ok=%v nodes=%+v", err, ok, graph.Nodes)
		}
		for _, n := range graph.Nodes {
			same("relation-graph node", n.WorkCount, WorksListFilter{LabelID: n.ID, NSFW: nsfw})
		}

		engines, err := svc.EnginesList(ctx, EnginesListFilter{NSFW: nsfw}, "", 50)
		if err != nil || len(engines.Items) != 1 {
			t.Fatalf("EnginesList: %v items=%+v", err, engines.Items)
		}
		same("engines lane", engines.Items[0].WorkCount, WorksListFilter{EngineID: f.engine, NSFW: nsfw})

		bySeries := WorksListFilter{SeriesID: f.series, NSFW: nsfw}
		seriesPage, err := svc.SeriesList(ctx, nsfw, "", "", 50)
		if err != nil || len(seriesPage.Items) != 1 {
			t.Fatalf("SeriesList: %v items=%+v", err, seriesPage.Items)
		}
		same("series lane", seriesPage.Items[0].WorkCount, bySeries)
		seriesRec, ok, err := svc.SeriesDetail(ctx, f.series, false, nsfw, 20, 0)
		if err != nil || !ok {
			t.Fatalf("SeriesDetail: %v ok=%v", err, ok)
		}
		same("series/{id}", seriesRec.WorkCount, bySeries)
		_, displayNSFW, err := svc.workCountsWithNSFW(ctx, seriesWorkEdge, []int64{f.series}, nsfw)
		if err != nil {
			t.Fatalf("workCountsWithNSFW: %v", err)
		}
		same("series display-nsfw tally", displayNSFW[f.series], WorksListFilter{
			SeriesID: f.series, NSFW: true, DisplayLimits: []string{model.DisplayLimitKeyNSFW},
		})
		if displayNSFW[f.series] != 2 || !seriesRec.HasNSFW {
			t.Fatalf("nsfw=%v has_nsfw=%v over %d display-nsfw members, want true over 2 (the live r18 with display_nsfw and the unclaimed r18)",
				nsfw, seriesRec.HasNSFW, displayNSFW[f.series])
		}

		rec, ok, err := svc.WorkDetail(ctx, f.host, PublicInclude{}, nsfw, 0, PublicFields{})
		if err != nil || !ok {
			t.Fatalf("WorkDetail: %v ok=%v", err, ok)
		}
		if len(rec.Labels) != 1 || len(rec.Engines) != 1 || len(rec.Tags) != 1 || len(rec.Series) != 1 || rec.Tags[0].WorkCount == nil {
			t.Fatalf("host chips = labels %+v engines %+v tags %+v series %+v", rec.Labels, rec.Engines, rec.Tags, rec.Series)
		}
		same("works/{id} labels[]", rec.Labels[0].WorkCount, WorksListFilter{LabelID: f.brand, NSFW: nsfw})
		same("works/{id} engines[]", rec.Engines[0].WorkCount, WorksListFilter{EngineID: f.engine, NSFW: nsfw})
		same("works/{id} tags[]", *rec.Tags[0].WorkCount, WorksListFilter{TagIDs: []int64{f.tag}, NSFW: nsfw})
		same("works/{id} series[].member_count", rec.Series[0].MemberCount, bySeries)
		sub, ok, err := svc.WorkSeries(ctx, f.host, nsfw, 20, 0)
		if err != nil || !ok || len(sub.Items) != 1 {
			t.Fatalf("WorkSeries: %v ok=%v items=%+v", err, ok, sub.Items)
		}
		same("works/{id}/series member_count", sub.Items[0].MemberCount, bySeries)

		checkTags("live aggregate:", nsfw)
	}

	if _, err := svc.RefreshTagWorkCounts(ctx, time.Now()); err != nil {
		t.Fatalf("RefreshTagWorkCounts: %v", err)
	}
	for _, nsfw := range []bool{false, true} {
		checkTags("rollup:", nsfw)
		rec, _, err := svc.WorkDetail(ctx, f.host, PublicInclude{}, nsfw, 0, PublicFields{})
		if err != nil {
			t.Fatalf("WorkDetail: %v", err)
		}
		same("rollup: works/{id} tags[]", *rec.Tags[0].WorkCount, WorksListFilter{TagIDs: []int64{f.tag}, NSFW: nsfw})
	}
}
