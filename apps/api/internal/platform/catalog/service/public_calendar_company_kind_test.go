package service

import (
	"slices"
	"testing"

	"api/internal/platform/catalog/model"
)

func linkCompanies(t *testing.T, workID int64, labelIDs ...int64) {
	t.Helper()
	for _, id := range labelIDs {
		if err := testDB.Create(&model.CatalogWorkLabel{WorkID: workID, LabelID: id, Kind: model.WorkLabelKindBrand}).Error; err != nil {
			t.Fatalf("link label %d to work %d: %v", id, workID, err)
		}
	}
}

func TestCalendarExcludeCompanyKind(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()

	circleA := createLabel(t, "Circle A", model.LabelKindDoujinCircle)
	circleB := createLabel(t, "Circle B", model.LabelKindDoujinCircle)
	brand := createLabel(t, "Brand", model.LabelKindGameBrand)
	publisher := createLabel(t, "Publisher", model.LabelKindPublisher)
	gonePublisher := createLabel(t, "Gone Publisher", model.LabelKindPublisher)
	goneCircle := createLabel(t, "Gone Circle", model.LabelKindDoujinCircle)
	if err := testDB.Exec(`UPDATE catalog_label SET deleted_at = now() WHERE id IN ?`, []int64{gonePublisher, goneCircle}).Error; err != nil {
		t.Fatalf("soft delete labels: %v", err)
	}

	dated := func(name string, d int16, labels ...int64) int64 {
		t.Helper()
		w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, name)
		createRelease(t, w.ID, 2024, 6, d)
		linkCompanies(t, w.ID, labels...)
		return w.ID
	}
	circleOnly := dated("CircleOnly", 1, circleA)
	twoCircles := dated("TwoCircles", 2, circleA, circleB)
	mixed := dated("BrandAndCircle", 3, brand, circleA)
	brandOnly := dated("BrandOnly", 4, brand)
	noCompany := dated("NoCompany", 5)
	circleWithGonePublisher := dated("CircleWithGonePublisher", 6, circleA, gonePublisher)
	goneCircleOnly := dated("GoneCircleOnly", 7, goneCircle)
	publisherOnly := dated("PublisherOnly", 8, publisher)

	all := calIDs(t, svc, june2024(), CalendarFilter{})
	if len(all) != 8 {
		t.Fatalf("unfiltered june = %v, want all 8 works", all)
	}

	circles := CalendarFilter{ExcludeCompanyKinds: []string{"doujin_circle"}}
	want := []int64{mixed, brandOnly, noCompany, goneCircleOnly, publisherOnly}
	if got := calIDs(t, svc, june2024(), circles); !slices.Equal(got, want) {
		t.Fatalf("june without circles = %v, want %v (dropped: circle-only %d, two circles %d, circle beside a deleted publisher %d)",
			got, want, circleOnly, twoCircles, circleWithGonePublisher)
	}
	if n, _, err := svc.CalendarMeta(t.Context(), june2024(), circles); err != nil || n != int64(len(want)) {
		t.Fatalf("meta count = %d (err %v), want %d: total must count the page's population", n, err, len(want))
	}

	both := CalendarFilter{ExcludeCompanyKinds: []string{"doujin_circle", "game_brand"}}
	want = []int64{noCompany, goneCircleOnly, publisherOnly}
	if got := calIDs(t, svc, june2024(), both); !slices.Equal(got, want) {
		t.Fatalf("june without circles and brands = %v, want %v", got, want)
	}

	yearOnly := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "YearCircle")
	createRelease(t, yearOnly.ID, 2024, 0, 0)
	linkCompanies(t, yearOnly.ID, circleB)
	tbaCircle := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "TBACircle")
	createRelease(t, tbaCircle.ID, 0, 0, 0)
	linkCompanies(t, tbaCircle.ID, circleA)
	tbaBrand := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "TBABrand")
	createRelease(t, tbaBrand.ID, 0, 0, 0)
	linkCompanies(t, tbaBrand.ID, brand)

	pending := CalendarBucket{Kind: CalendarPendingBucket, Year: 2024}
	if got := calIDs(t, svc, pending, circles); len(got) != 0 {
		t.Fatalf("year bucket without circles = %v, want empty", got)
	}
	tba := CalendarBucket{Kind: CalendarTBABucket}
	if got := calIDs(t, svc, tba, circles); !slices.Equal(got, []int64{tbaBrand.ID}) {
		t.Fatalf("undated bucket without circles = %v, want [%d]", got, tbaBrand.ID)
	}

	if (CalendarFilter{}).PopulationKey() == circles.PopulationKey() {
		t.Fatal("the population key must tell the excluded population apart")
	}
	if got := (CalendarFilter{}).PopulationKey(); got != "sfw-jazh-all" {
		t.Fatalf("population key without the parameter = %q, want the unchanged sfw-jazh-all", got)
	}
}

func TestCalendarBoundsFollowTheExcludedPopulation(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()

	circle := createLabel(t, "Circle", model.LabelKindDoujinCircle)
	brand := createLabel(t, "Brand", model.LabelKindGameBrand)
	work := func(name string, y, m int16, labels ...int64) {
		t.Helper()
		w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, name)
		createRelease(t, w.ID, y, m, 1)
		linkCompanies(t, w.ID, labels...)
	}
	work("EarliestCircle", 2019, 1, circle)
	work("EarlyBrand", 2020, 3, brand)
	work("LateNoCompany", 2025, 9)
	work("LatestCircle", 2031, 12, circle)

	mn, mx, found, err := svc.CalendarBounds(t.Context(), CalendarFilter{})
	if err != nil || !found || mn != 20190100 || mx != 20311200 {
		t.Fatalf("unfiltered bounds = (%d, %d, %v, %v), want (20190100, 20311200)", mn, mx, found, err)
	}
	circles := CalendarFilter{ExcludeCompanyKinds: []string{"doujin_circle"}}
	mn, mx, found, err = svc.CalendarBounds(t.Context(), circles)
	if err != nil || !found || mn != 20200300 || mx != 20250900 {
		t.Fatalf("bounds without circles = (%d, %d, %v, %v), want (20200300, 20250900): navigation must not step onto months the filter empties", mn, mx, found, err)
	}

	everything := CalendarFilter{ExcludeCompanyKinds: []string{"doujin_circle", "game_brand"}}
	if err := testDB.Exec(`DELETE FROM catalog_release WHERE work_id IN (SELECT id FROM catalog_work WHERE display_name = 'LateNoCompany')`).Error; err != nil {
		t.Fatalf("drop the company-less work's release: %v", err)
	}
	if _, _, found, err = svc.CalendarBounds(t.Context(), everything); err != nil || found {
		t.Fatalf("bounds with every dated work excluded: found=%v err=%v, want not found", found, err)
	}
}

func TestCalendarBoundsAreCachedPerPopulation(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	bounds := func(f CalendarFilter) (int64, int64) {
		t.Helper()
		mn, mx, found, err := svc.CalendarBounds(t.Context(), f)
		if err != nil || !found {
			t.Fatalf("CalendarBounds %+v: found=%v err=%v", f, found, err)
		}
		return mn, mx
	}

	circle := createLabel(t, "Circle", model.LabelKindDoujinCircle)
	first := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "First")
	createRelease(t, first.ID, 2020, 3, 1)
	circles := CalendarFilter{ExcludeCompanyKinds: []string{"doujin_circle"}}
	if mn, mx := bounds(CalendarFilter{}); mn != 20200300 || mx != 20200300 {
		t.Fatalf("first read = (%d, %d)", mn, mx)
	}
	bounds(circles)

	later := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "LaterCircle")
	createRelease(t, later.ID, 2025, 9, 1)
	linkCompanies(t, later.ID, circle)
	if _, mx := bounds(CalendarFilter{}); mx != 20200300 {
		t.Fatalf("a read inside the TTL recomputed: max = %d, want the cached 20200300", mx)
	}

	svc.FlushTotals()
	if _, mx := bounds(CalendarFilter{}); mx != 20250900 {
		t.Fatalf("after a flush max = %d, want 20250900", mx)
	}
	if _, mx := bounds(circles); mx != 20200300 {
		t.Fatalf("excluded population max = %d, want 20200300: the circle work must not reach it", mx)
	}
}
