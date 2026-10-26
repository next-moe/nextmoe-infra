package service

import (
	"slices"
	"testing"

	"api/internal/platform/catalog/model"
)

func createReleaseKind(t *testing.T, workID int64, kind, y, m, d int16) *model.CatalogRelease {
	t.Helper()
	r := createRelease(t, workID, y, m, d)
	if err := testDB.Model(r).Update("kind", kind).Error; err != nil {
		t.Fatalf("set release kind: %v", err)
	}
	r.Kind = kind
	return r
}

func TestWorkReleaseDateIsTheFullRelease(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	trialFirst := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "TrialBeforeFull")
	trialRel := createReleaseKind(t, trialFirst.ID, model.ReleaseKindTrial, 2026, 9, 4)
	fullRel := createRelease(t, trialFirst.ID, 2026, 10, 30)
	undatedFull := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "UndatedFull")
	createRelease(t, undatedFull.ID, 0, 0, 0)
	createReleaseKind(t, undatedFull.ID, model.ReleaseKindTrial, 2026, 5, 10)
	trialOnly := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "TrialOnly")
	onlyTrialRel := createReleaseKind(t, trialOnly.ID, model.ReleaseKindTrial, 2024, 3, 2)
	patchFirst := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "PatchBeforeDigital")
	createReleaseKind(t, patchFirst.ID, model.ReleaseKindPatch, 2023, 1, 1)
	createReleaseKind(t, patchFirst.ID, model.ReleaseKindDigital, 2023, 6, 1)

	month := func(y, m int) []int64 {
		return calIDs(t, svc, CalendarBucket{Kind: CalendarMonthBucket, Year: y, Month: m}, CalendarFilter{})
	}
	if got := month(2026, 10); !slices.Equal(got, []int64{trialFirst.ID}) {
		t.Fatalf("2026-10 = %v, want [%d] (its 通常版 month)", got, trialFirst.ID)
	}
	if got := month(2026, 9); len(got) != 0 {
		t.Fatalf("2026-09 = %v, want empty: a trial does not date a work that has a full release", got)
	}
	if got := month(2026, 5); len(got) != 0 {
		t.Fatalf("2026-05 = %v, want empty: an undated full release leaves the work undated", got)
	}
	if got := calIDs(t, svc, CalendarBucket{Kind: CalendarTBABucket}, CalendarFilter{}); !slices.Equal(got, []int64{undatedFull.ID}) {
		t.Fatalf("tba = %v, want [%d]", got, undatedFull.ID)
	}
	if got := month(2024, 3); !slices.Equal(got, []int64{trialOnly.ID}) {
		t.Fatalf("2024-03 = %v, want [%d]: a work known only as a trial is dated by it", got, trialOnly.ID)
	}
	if got := month(2023, 1); len(got) != 0 {
		t.Fatalf("2023-01 = %v, want empty: a patch does not date a work that has a full release", got)
	}
	if got := month(2023, 6); !slices.Equal(got, []int64{patchFirst.ID}) {
		t.Fatalf("2023-06 = %v, want [%d]", got, patchFirst.ID)
	}
	lo, hi, found, err := svc.CalendarBounds(ctx, CalendarFilter{})
	if err != nil || !found || lo != 20230600 || hi != 20261000 {
		t.Fatalf("bounds = %d..%d found=%v err=%v, want 20230600..20261000", lo, hi, found, err)
	}

	want := map[int64]string{
		trialFirst.ID: "2026-10-30", undatedFull.ID: "", trialOnly.ID: "2024-03-02", patchFirst.ID: "2023-06-01",
	}
	page, err := svc.WorksList(ctx, WorksListFilter{Sort: "id"}, "", 50)
	if err != nil {
		t.Fatalf("WorksList: %v", err)
	}
	for _, it := range page.Items {
		got := ""
		if it.ReleaseDate != nil {
			got = *it.ReleaseDate
		}
		if got != want[it.ID] {
			t.Errorf("list release_date of %d = %q, want %q", it.ID, got, want[it.ID])
		}
	}
	for id, w := range want {
		rec, ok, err := svc.WorkDetail(ctx, id, PublicInclude{}, false, 0, PublicFields{})
		if err != nil || !ok {
			t.Fatalf("WorkDetail %d: ok=%v err=%v", id, ok, err)
		}
		got := ""
		if rec.ReleaseDate != nil {
			got = *rec.ReleaseDate
		}
		if got != w {
			t.Errorf("detail release_date of %d = %q, want %q", id, got, w)
		}
	}

	page, err = svc.WorksList(ctx, WorksListFilter{Sort: "id", ReleasedAfter: 20260901, ReleasedBefore: 20260930}, "", 50)
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("released in 2026-09 = %+v err=%v, want none", page.Items, err)
	}

	feed, err := svc.ReleaseFeed(ctx, ReleaseFeedFilter{Kinds: []int16{
		model.ReleaseKindDefault, model.ReleaseKindDigital, model.ReleaseKindPhysical,
		model.ReleaseKindTrial, model.ReleaseKindPatch,
	}}, "", 50)
	if err != nil {
		t.Fatalf("ReleaseFeed: %v", err)
	}
	first := map[int64]bool{}
	for _, it := range feed.Items {
		first[it.ID] = it.IsFirst
	}
	if first[trialRel.ID] || !first[fullRel.ID] || !first[onlyTrialRel.ID] {
		t.Fatalf("is_first: trial=%v full=%v only-trial=%v, want false true true",
			first[trialRel.ID], first[fullRel.ID], first[onlyTrialRel.ID])
	}
}
