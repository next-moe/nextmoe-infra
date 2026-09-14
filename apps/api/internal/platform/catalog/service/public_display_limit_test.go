// public_display_limit_test.go — A2-R5: the EDITORIAL DISPLAY axis
// (refs/proj/140).
//
// The incident this closes (doc 106 §38): a downstream mapped the catalog's AGE
// axis (content_rating) onto its own DISPLAY gate (content_limit) and hid every
// r18 game — 94.5% of the claimed live population — collapsing its indexable
// surface from 6,117 works to 599. The two axes are different questions, and on
// production they disagree in bulk: 5,568 works are r18 games whose wiki display
// material is editorially sfw, and 50 are all_ages games the wiki marked nsfw.
//
// The cases below pin the axis where it actually runs: the Go projection, its
// SQL twin (cross-checked row-for-row, exactly as the claim_state axis is), the
// three-gate conjunction, and the value that reaches the wire.
//
// The axis's authority is catalog_work.display_nsfw since the W1-pre nativization
// (refs/proj/140 §5b) — it was galgame.content_limit, read out of the wiki body per
// request, and these cases are the same cases re-pointed at the native column.
package service

import (
	"fmt"
	"testing"

	"api/internal/platform/catalog/model"
	catsearch "api/internal/platform/catalog/search"
)

func setDisplayNSFW(t *testing.T, workID int64, nsfw bool) {
	t.Helper()
	if err := testDB.Exec(
		`UPDATE catalog_work SET display_nsfw = ? WHERE id = ?`, nsfw, workID).Error; err != nil {
		t.Fatalf("set display_nsfw on work %d: %v", workID, err)
	}
}

func declareDisplayLimit(t *testing.T, productWorkID int64, contentLimit string) {
	t.Helper()
	res := testDB.Exec(
		`UPDATE catalog_work SET display_nsfw = ? WHERE site = ? AND product_work_id = ?`,
		contentLimit == model.WikiContentLimitNSFW, siteGalgameWiki, productWorkID)
	if res.Error != nil {
		t.Fatalf("declare display limit for body %d: %v", productWorkID, res.Error)
	}
	if res.RowsAffected != 1 {
		t.Fatalf("declare display limit for body %d touched %d rows, want 1 (is the work claimed?)",
			productWorkID, res.RowsAffected)
	}
}

// coverArt is the third input to the axis, and the fixture realizes it the way
// production does — by writing cover rows and letting the trigger derive the
// column — rather than by setting the column, which GORM cannot do anyway.
type coverArt int

const (
	noCoverArt coverArt = iota
	someSafeCoverArt
	allExplicitCoverArt
)

func (c coverArt) apply(t *testing.T, workID int64, seed string) {
	t.Helper()
	switch c {
	case noCoverArt:
	case someSafeCoverArt:
		addWorkCover(t, workID, hash64(seed+"s"), 0, "main", false, model.SexualSafe, srcVNDB)
		addWorkCover(t, workID, hash64(seed+"x"), 1, "main", false, model.SexualExplicit, srcVNDB)
	case allExplicitCoverArt:
		addWorkCover(t, workID, hash64(seed+"x"), 0, "main", false, model.SexualExplicit, srcVNDB)
	}
}

func (c coverArt) allExplicit() bool { return c == allExplicitCoverArt }

func (c coverArt) String() string {
	return [...]string{"no cover art", "some safe cover art", "all cover art explicit"}[c]
}

// displayLimitFixture builds EVERY combination of the axis's inputs, because
// the Go projection and its SQL twin are two hand-written copies of one rule
// and a table of interesting cases only cross-checks the cases someone thought
// of. 90 rows is the whole input space.
func displayLimitFixture(t *testing.T) (byLimit map[string][]int64, all []int64) {
	t.Helper()
	wiki, empty, letmoe := "galgame_wiki", "", "letmoe"
	pw := func(n int64) *int64 { return &n }

	claims := []struct {
		name string
		site *string
		pwid func(int64) *int64
	}{
		{"bodyless", nil, func(int64) *int64 { return nil }},
		{"empty site", &empty, pw},
		{"site without a product work id", &wiki, func(int64) *int64 { return nil }},
		{"claimed by the wiki", &wiki, pw},
		{"claimed by another site", &letmoe, pw},
	}
	ratings := []int16{model.ContentRatingAllAges, model.ContentRatingSensitive, model.ContentRatingR18}
	arts := []coverArt{noCoverArt, someSafeCoverArt, allExplicitCoverArt}

	byLimit = map[string][]int64{}
	var n int64
	for _, cl := range claims {
		for _, rating := range ratings {
			for _, display := range []bool{false, true} {
				for _, art := range arts {
					n++
					name := fmt.Sprintf("%s / rating %d / display_nsfw %v / %s", cl.name, rating, display, art)
					w := createWorkX(t, galgameMediumID, rating, model.WorkStatusLive, name)
					setClaimColumns(t, w.ID, cl.site, cl.pwid(9400+n), nil)
					if display {
						setDisplayNSFW(t, w.ID, true)
					}
					art.apply(t, w.ID, fmt.Sprintf("%04x", n))
					key := model.DisplayLimitKey(model.WorkShelf{
						Site: cl.site, ProductWorkID: cl.pwid(9400 + n), DisplayNSFW: display,
						ContentRating: rating, CoverArtAllExplicit: art.allExplicit(),
					})
					byLimit[key] = append(byLimit[key], w.ID)
					all = append(all, w.ID)
				}
			}
		}
	}
	return byLimit, all
}

// TestCoverArtAllExplicitReachesTheProjection is the fixture's own positive
// control: without it a trigger that never fired would leave every work with
// cover_art_all_explicit false, the Go side would agree with the SQL side on
// all 72 rows, and the cross-check above would pass while testing two thirds
// of nothing.
// TestDisplayLimitWhereMatchesProjection is the anti-drift gate: the axis has
// exactly two implementations — model.WorkShelf.NSFW and displayLimitNSFWSQL —
// and this walks the whole input space through both, so a change to one that
// is not made to the other cannot reach main.
func TestDisplayLimitWhereMatchesProjection(t *testing.T) {
	cleanTables(t)
	cleanTagTables(t)
	byLimit, all := displayLimitFixture(t)

	for _, lim := range []string{model.DisplayLimitKeySFW, model.DisplayLimitKeyNSFW} {
		if len(byLimit[lim]) == 0 {
			t.Fatalf("fixture covers no %q row", lim)
		}
	}

	seen := map[int64]int{}
	for lim, want := range byLimit {
		got := idSet(listIDs(t, WorksListFilter{Sort: "id", NSFW: true, DisplayLimits: []string{lim}}))
		if len(got) != len(want) {
			t.Fatalf("content_limit=%s selected %d rows, want %d (%v vs %v)", lim, len(got), len(want), got, want)
		}
		for _, id := range want {
			if !got[id] {
				t.Fatalf("content_limit=%s must select work %d (model.DisplayLimitKey says %s)", lim, id, lim)
			}
			seen[id]++
		}
	}
	for _, id := range all {
		if seen[id] != 1 {
			t.Fatalf("work %d matched %d display limits, want exactly 1", id, seen[id])
		}
	}
	if n := len(idSet(listIDs(t, WorksListFilter{
		Sort: "id", NSFW: true,
		DisplayLimits: []string{model.DisplayLimitKeySFW, model.DisplayLimitKeyNSFW},
	}))); n != len(all) {
		t.Fatalf("both values selected %d rows, want the whole set of %d", n, len(all))
	}
}

func TestCoverArtAllExplicitReachesTheProjection(t *testing.T) {
	cleanTables(t)
	cleanTagTables(t)

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "全年齢・成人素材のみ")
	claimWork(t, w.ID, "galgame_wiki", 9390)
	declareDisplayLimit(t, 9390, "sfw")
	if got := idSet(listIDs(t, WorksListFilter{
		Sort: "id", NSFW: true, DisplayLimits: []string{model.DisplayLimitKeySFW},
	})); !got[w.ID] {
		t.Fatalf("an editorially sfw work with no cover art at all belongs on the sfw shelf")
	}

	addWorkCover(t, w.ID, hash64("ff01"), 0, "main", false, model.SexualExplicit, srcVNDB)
	var stored bool
	if err := testDB.Raw(`SELECT cover_art_all_explicit FROM catalog_work WHERE id = ?`, w.ID).
		Scan(&stored).Error; err != nil {
		t.Fatalf("read cover_art_all_explicit: %v", err)
	}
	if !stored {
		t.Fatalf("the trigger did not derive cover_art_all_explicit — run the catalog migration")
	}
	if got := idSet(listIDs(t, WorksListFilter{
		Sort: "id", NSFW: true, DisplayLimits: []string{model.DisplayLimitKeySFW},
	})); got[w.ID] {
		t.Fatalf("work %d promised safe display material and owns none, yet the sfw shelf still serves it "+
			"— this is work 208100, where the election fell through to the blurred stand-in", w.ID)
	}
}

func TestWorksListDisplayLimitIsNotTheAgeAxis(t *testing.T) {
	cleanTables(t)
	cleanTagTables(t)

	safeR18 := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "成人ゲーム・素材は安全")
	spicySFW := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "全年齢ゲーム・素材は成人")
	bodylessR18 := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "無認領・成人")
	for i, id := range []int64{safeR18.ID, spicySFW.ID} {
		claimWork(t, id, "galgame_wiki", int64(9500+i))
	}
	declareDisplayLimit(t, 9500, "sfw")
	declareDisplayLimit(t, 9501, "nsfw")

	sfw := idSet(listIDs(t, WorksListFilter{
		Sort: "id", NSFW: true, DisplayLimits: []string{model.DisplayLimitKeySFW},
	}))
	if !sfw[safeR18.ID] {
		t.Fatalf("content_limit=sfw dropped the claimed r18 work with safe material — the exact 5,568-row incident")
	}
	if sfw[spicySFW.ID] {
		t.Fatalf("content_limit=sfw served an all_ages work the wiki marked nsfw — the reverse leak")
	}
	if sfw[bodylessR18.ID] {
		t.Fatalf("content_limit=sfw served a BODYLESS r18 work: with no editorial flag the rating is the only signal")
	}

	nsfw := idSet(listIDs(t, WorksListFilter{
		Sort: "id", NSFW: true, DisplayLimits: []string{model.DisplayLimitKeyNSFW},
	}))
	if !nsfw[spicySFW.ID] || !nsfw[bodylessR18.ID] || nsfw[safeR18.ID] {
		t.Fatalf("content_limit=nsfw = %v, want exactly the wiki-nsfw work and the bodyless r18 one", nsfw)
	}

	if n := len(idSet(listIDs(t, WorksListFilter{Sort: "id", NSFW: true}))); n != 3 {
		t.Fatalf("no content_limit selected %d rows, want all 3", n)
	}
}

func TestWorksListThreeGatesAreOrthogonal(t *testing.T) {
	cleanTables(t)
	cleanTagTables(t)

	safeLive := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "成人・安全・公開")
	safeDraft := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "成人・安全・下書き")
	spicyLive := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "成人・成人素材・公開")
	sfwLive := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "全年齢・安全・公開")
	for i, id := range []int64{safeLive.ID, safeDraft.ID, spicyLive.ID, sfwLive.ID} {
		claimWork(t, id, "galgame_wiki", int64(9600+i))
	}
	for id, limit := range map[int64]string{9600: "sfw", 9601: "sfw", 9602: "nsfw", 9603: "sfw"} {
		declareDisplayLimit(t, id, limit)
	}
	setClaimState(t, safeLive.ID, i16(model.ClaimStateLive))
	setClaimState(t, safeDraft.ID, i16(model.ClaimStateDraft))
	setClaimState(t, spicyLive.ID, i16(model.ClaimStateLive))
	setClaimState(t, sfwLive.ID, i16(model.ClaimStateLive))

	live := []string{model.ClaimStateKeyLive}
	sfwOnly := []string{model.DisplayLimitKeySFW}
	for _, tc := range []struct {
		name   string
		nsfw   bool
		limits []string
		states []string
		want   []int64
	}{
		{"no gate at all", true, nil, nil, []int64{safeLive.ID, safeDraft.ID, spicyLive.ID, sfwLive.ID}},
		{"age gate alone drops every r18 game", false, nil, nil, []int64{sfwLive.ID}},
		{"display gate alone keeps the safe r18 games", true, sfwOnly, nil, []int64{safeLive.ID, safeDraft.ID, sfwLive.ID}},
		{"display + claim gate", true, sfwOnly, live, []int64{safeLive.ID, sfwLive.ID}},
		{"the display gate never reopens the age gate", false, sfwOnly, nil, []int64{sfwLive.ID}},
		{"the claim gate never reopens the display gate", true, sfwOnly, live, []int64{safeLive.ID, sfwLive.ID}},
		{"nsfw display + live claim", true, []string{model.DisplayLimitKeyNSFW}, live, []int64{spicyLive.ID}},
	} {
		got := idSet(listIDs(t, WorksListFilter{
			Sort: "id", NSFW: tc.nsfw, DisplayLimits: tc.limits, ClaimStates: tc.states,
		}))
		if len(got) != len(tc.want) {
			t.Fatalf("%s: got %d rows %v, want %d %v", tc.name, len(got), got, len(tc.want), tc.want)
		}
		for _, id := range tc.want {
			if !got[id] {
				t.Fatalf("%s: work %d missing", tc.name, id)
			}
		}
	}

	f := WorksListFilter{Sort: "id", NSFW: true, DisplayLimits: sfwOnly, ClaimStates: live}
	walked := idSet(listIDs(t, f))
	onePage, err := newPublicSvc().WorksList(t.Context(), f, "", 100)
	if err != nil {
		t.Fatalf("WorksList single page: %v", err)
	}
	if len(onePage.Items) != len(walked) {
		t.Fatalf("single page served %d rows but the keyset walk served %d — the gate must not depend on paging",
			len(onePage.Items), len(walked))
	}
}

func TestClaimedByContentLimitOnEveryFace(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	safeR18 := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "LimitSafeR18")
	spicySFW := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "LimitSpicySFW")
	bodyless := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "LimitBodyless")
	claimWork(t, safeR18.ID, "galgame_wiki", 9700)
	claimWork(t, spicySFW.ID, "galgame_wiki", 9701)
	declareDisplayLimit(t, 9700, "sfw")
	declareDisplayLimit(t, 9701, "nsfw")
	setClaimState(t, spicySFW.ID, i16(model.ClaimStateDraft))

	want := map[int64]string{safeR18.ID: "sfw", spicySFW.ID: "nsfw"}

	page, err := svc.WorksList(ctx, WorksListFilter{Sort: "id", NSFW: true}, "", 50)
	if err != nil {
		t.Fatalf("WorksList: %v", err)
	}
	seen := 0
	for _, it := range page.Items {
		if it.ID == bodyless.ID {
			if it.ClaimedBy != nil {
				t.Fatalf("bodyless work got claimed_by %+v, want null", it.ClaimedBy)
			}
			continue
		}
		if it.ClaimedBy == nil || it.ClaimedBy.ContentLimit != want[it.ID] {
			t.Fatalf("list work %d claimed_by = %+v, want content_limit %q", it.ID, it.ClaimedBy, want[it.ID])
		}
		seen++
	}
	if seen != len(want) {
		t.Fatalf("list covered %d claimed works, want %d", seen, len(want))
	}

	for id, limit := range want {
		rec, found, err := svc.WorkDetail(ctx, id, PublicInclude{}, true, 0, PublicFields{})
		if err != nil || !found {
			t.Fatalf("WorkDetail %d: found=%v err=%v", id, found, err)
		}
		if rec.ClaimedBy == nil || rec.ClaimedBy.ContentLimit != limit {
			t.Fatalf("detail work %d claimed_by = %+v, want content_limit %q", id, rec.ClaimedBy, limit)
		}
	}

	addExternalRef(t, model.EntityTypeWork, safeR18.ID, srcVNDB, "v70501", model.LinkKindExact)
	single, found, err := svc.Lookup(ctx, "vndb", "v70501", true)
	if err != nil || !found {
		t.Fatalf("Lookup: found=%v err=%v", found, err)
	}
	if single.ClaimedBy == nil || single.ClaimedBy.ContentLimit != "sfw" {
		t.Fatalf("lookup claimed_by = %+v, want content_limit sfw", single.ClaimedBy)
	}
	if single.Work == nil || single.Work.ClaimedBy == nil || single.Work.ClaimedBy.ContentLimit != "sfw" {
		t.Fatalf("lookup brief claimed_by = %+v, want content_limit sfw", single.Work)
	}

	claims, err := svc.claimedByFor(ctx, []int64{safeR18.ID, spicySFW.ID, bodyless.ID})
	if err != nil {
		t.Fatalf("claimedByFor: %v", err)
	}
	if len(claims) != 2 {
		t.Fatalf("claimedByFor returned %d claims, want 2 (the bodyless row has none)", len(claims))
	}
	for id, limit := range want {
		if claims[id] == nil || claims[id].ContentLimit != limit {
			t.Fatalf("batch claim %d = %+v, want content_limit %q", id, claims[id], limit)
		}
	}
}

func TestCalendarDisplayLimitGateAndETag(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	safe := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "CalSafe")
	spicy := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "CalSpicy")
	claimWork(t, safe.ID, "galgame_wiki", 9800)
	claimWork(t, spicy.ID, "galgame_wiki", 9801)
	declareDisplayLimit(t, 9800, "sfw")
	declareDisplayLimit(t, 9801, "nsfw")
	createRelease(t, safe.ID, 2024, 6, 14)
	createRelease(t, spicy.ID, 2025, 6, 14)

	june2024 := CalendarBucket{Kind: CalendarMonthBucket, Year: 2024, Month: 6}
	june2025 := CalendarBucket{Kind: CalendarMonthBucket, Year: 2025, Month: 6}
	ungated := CalendarFilter{NSFW: true}
	sfwOnly := CalendarFilter{NSFW: true, DisplayLimits: []string{model.DisplayLimitKeySFW}}

	for _, tc := range []struct {
		name   string
		bucket CalendarBucket
		f      CalendarFilter
		want   int64
	}{
		{"ungated june 2024", june2024, ungated, 1},
		{"ungated june 2025", june2025, ungated, 1},
		{"sfw-gated june 2024", june2024, sfwOnly, 1},
		{"sfw-gated june 2025", june2025, sfwOnly, 0},
	} {
		count, _, err := svc.CalendarMeta(ctx, tc.bucket, tc.f)
		if err != nil {
			t.Fatalf("%s: CalendarMeta: %v", tc.name, err)
		}
		if count != tc.want {
			t.Fatalf("%s: count = %d, want %d", tc.name, count, tc.want)
		}
		page, err := svc.CalendarPage(ctx, tc.bucket, tc.f, "", 50)
		if err != nil {
			t.Fatalf("%s: CalendarPage: %v", tc.name, err)
		}
		if int64(len(page.Items)) != tc.want {
			t.Fatalf("%s: page carries %d rows but the count says %d — one gate, two queries",
				tc.name, len(page.Items), tc.want)
		}
	}

	_, maxOrd, found, err := svc.CalendarBounds(ctx, sfwOnly)
	if err != nil || !found {
		t.Fatalf("CalendarBounds: found=%v err=%v", found, err)
	}
	if maxOrd != 202406_00 {
		t.Fatalf("sfw-gated max month = %d, want 202406_00 (the nsfw 2025 work is outside this population)", maxOrd)
	}

	ungatedCount, ungatedMax, err := svc.CalendarMeta(ctx, june2024, ungated)
	if err != nil {
		t.Fatalf("CalendarMeta ungated: %v", err)
	}
	gatedCount, gatedMax, err := svc.CalendarMeta(ctx, june2024, sfwOnly)
	if err != nil {
		t.Fatalf("CalendarMeta gated: %v", err)
	}
	if ungatedCount != gatedCount {
		t.Fatalf("the two populations must have EQUAL counts here (%d vs %d) — that is what makes the key load-bearing",
			ungatedCount, gatedCount)
	}
	a := CalendarETag("month-2024-06", ungated.PopulationKey(), ungatedCount, ungatedMax)
	b := CalendarETag("month-2024-06", sfwOnly.PopulationKey(), gatedCount, gatedMax)
	if a == b {
		t.Fatalf("ungated and content_limit=sfw share the ETag %s — a cross-gate cache smear", a)
	}
	nsfwOnly := CalendarFilter{NSFW: true, DisplayLimits: []string{model.DisplayLimitKeyNSFW}}
	if sfwOnly.PopulationKey() == nsfwOnly.PopulationKey() {
		t.Fatalf("sfw and nsfw share the population key %q", sfwOnly.PopulationKey())
	}
}

func TestDisplayLimitVocabularyIsClosed(t *testing.T) {
	for _, ok := range []string{"sfw", "nsfw"} {
		if !IsDisplayLimit(ok) {
			t.Fatalf("%q must be a legal content_limit token", ok)
		}
	}
	for _, bad := range []string{"", "all", "SFW", "NSFW", "r18", "safe", "true"} {
		if IsDisplayLimit(bad) {
			t.Fatalf("%q must NOT be a legal content_limit token", bad)
		}
	}
}

func TestWorksSearchDisplayLimitGate(t *testing.T) {
	cleanTables(t)
	cleanTagTables(t)
	cleanTaxonomyTables(t)
	idx := worksSearchIndexer(t)
	svc := newPublicSvc().WithWorksSearch(idx)

	safeR18 := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "検索・成人・安全素材")
	spicySFW := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "検索・全年齢・成人素材")
	bodylessR18 := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "検索・無認領・成人")
	claimWork(t, safeR18.ID, "galgame_wiki", 9900)
	claimWork(t, spicySFW.ID, "galgame_wiki", 9901)
	declareDisplayLimit(t, 9900, "sfw")
	declareDisplayLimit(t, 9901, "nsfw")

	docs := make([]catsearch.WorkDocInput, 0, 3)
	for _, w := range []struct {
		id   int64
		name string
	}{
		{safeR18.ID, "検索・成人・安全素材"}, {spicySFW.ID, "検索・全年齢・成人素材"},
		{bodylessR18.ID, "検索・無認領・成人"},
	} {
		var row struct {
			Site                *string `gorm:"column:site"`
			ProductWorkID       *int64  `gorm:"column:product_work_id"`
			ClaimState          *int16  `gorm:"column:claim_state"`
			ContentRating       int16   `gorm:"column:content_rating"`
			DisplayNSFW         bool    `gorm:"column:display_nsfw"`
			CoverArtAllExplicit bool    `gorm:"column:cover_art_all_explicit"`
		}
		if err := testDB.Raw(`
			SELECT w.site, w.product_work_id, w.claim_state, w.content_rating, w.display_nsfw,
			       w.cover_art_all_explicit
			FROM catalog_work w WHERE w.id = ?`, w.id).Scan(&row).Error; err != nil {
			t.Fatalf("read claim columns: %v", err)
		}
		docs = append(docs, catsearch.WorkDocInput{
			ID: w.id, DisplayName: w.name, OLang: "ja",
			ContentRating: row.ContentRating,
			Claimed:       row.Site != nil && *row.Site != "",
			ClaimState:    model.ClaimStateKey(row.Site, row.ProductWorkID, row.ClaimState),
			ContentLimit: model.DisplayLimitKey(model.WorkShelf{
				Site: row.Site, ProductWorkID: row.ProductWorkID, DisplayNSFW: row.DisplayNSFW,
				ContentRating: row.ContentRating, CoverArtAllExplicit: row.CoverArtAllExplicit,
			}),
			UpdatedTS: 1700000000,
		})
	}
	indexWorks(t, idx, docs)

	for _, tc := range []struct {
		limits []string
		want   []int64
	}{
		{nil, []int64{safeR18.ID, spicySFW.ID, bodylessR18.ID}},
		{[]string{model.DisplayLimitKeySFW}, []int64{safeR18.ID}},
		{[]string{model.DisplayLimitKeyNSFW}, []int64{spicySFW.ID, bodylessR18.ID}},
		{[]string{model.DisplayLimitKeySFW, model.DisplayLimitKeyNSFW}, []int64{safeR18.ID, spicySFW.ID, bodylessR18.ID}},
	} {
		data, err := svc.WorksSearch(t.Context(), WorksSearchFilter{
			Sort: "id", NSFW: true, DisplayLimits: tc.limits, Limit: 50,
		})
		if err != nil {
			t.Fatalf("content_limit=%v: WorksSearch: %v", tc.limits, err)
		}
		got := make(map[int64]bool, len(data.Items))
		for _, it := range data.Items {
			got[it.ID] = true
		}
		if len(got) != len(tc.want) {
			t.Fatalf("content_limit=%v: got %d items %v, want %d", tc.limits, len(got), got, len(tc.want))
		}
		for _, id := range tc.want {
			if !got[id] {
				t.Fatalf("content_limit=%v: work %d missing from %v", tc.limits, id, got)
			}
		}
		if int(data.Total) != len(data.Items) {
			t.Fatalf("content_limit=%v: total=%d but page carries %d rows — total and items must share one filter",
				tc.limits, data.Total, len(data.Items))
		}
	}
}
