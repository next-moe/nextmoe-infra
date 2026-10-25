package service

import (
	"testing"

	"api/internal/platform/catalog/model"
)

func setClaimColumns(t *testing.T, workID int64, site *string, productWorkID *int64, state *int16) {
	t.Helper()
	if err := testDB.Exec(
		`UPDATE catalog_work SET site = ?, product_work_id = ?, claim_state = ? WHERE id = ?`,
		site, productWorkID, state, workID).Error; err != nil {
		t.Fatalf("set claim columns: %v", err)
	}
}

type claimRow struct {
	name  string
	site  *string
	pwid  *int64
	state *int16
}

func claimStateFixture(t *testing.T) (byState map[string][]int64, all []int64) {
	t.Helper()
	wiki, empty := "galgame_wiki", ""
	pw := func(n int64) *int64 { return &n }
	bogus := int16(99)

	rows := []claimRow{
		{"unclaimed (all columns NULL)", nil, nil, nil},
		{"unclaimed (site empty)", &empty, pw(9101), nil},
		{"site without a product work id", &wiki, nil, nil},
		{"draft-stamped half claim", &wiki, nil, i16(model.ClaimStateDraft)},
		{"claimed, column NULL", &wiki, pw(9105), nil},
		{"claimed live", &wiki, pw(9106), i16(model.ClaimStateLive)},
		{"claimed draft", &wiki, pw(9107), i16(model.ClaimStateDraft)},
		{"claimed hidden", &wiki, pw(9108), i16(model.ClaimStateHidden)},
		{"claimed pending", &wiki, pw(9110), i16(model.ClaimStatePending)},
		{"claimed declined", &wiki, pw(9111), i16(model.ClaimStateDeclined)},
		{"claimed, state outside the vocabulary", &wiki, pw(9109), &bogus},
	}

	byState = map[string][]int64{}
	for _, r := range rows {
		w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, r.name)
		setClaimColumns(t, w.ID, r.site, r.pwid, r.state)
		key := model.ClaimStateKey(r.site, r.pwid, r.state)
		byState[key] = append(byState[key], w.ID)
		all = append(all, w.ID)
	}
	return byState, all
}

func listIDs(t *testing.T, f WorksListFilter) []int64 {
	t.Helper()
	svc := newPublicSvc()
	var out []int64
	cursor := ""
	for i := 0; i < 500; i++ {
		page, err := svc.WorksList(t.Context(), f, cursor, 2)
		if err != nil {
			t.Fatalf("WorksList(%v): %v", f.ClaimStates, err)
		}
		for _, it := range page.Items {
			out = append(out, it.ID)
		}
		if page.NextCursor == nil {
			return out
		}
		cursor = *page.NextCursor
	}
	t.Fatalf("works list did not terminate within 500 pages")
	return nil
}

func idSet(ids []int64) map[int64]bool {
	out := make(map[int64]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

func TestClaimStateWhereMatchesProjection(t *testing.T) {
	cleanTables(t)
	cleanTagTables(t)
	byState, all := claimStateFixture(t)

	for _, st := range []string{
		model.ClaimStateKeyNone, model.ClaimStateKeyLive,
		model.ClaimStateKeyDraft, model.ClaimStateKeyPending,
		model.ClaimStateKeyDeclined, model.ClaimStateKeyHidden,
	} {
		if len(byState[st]) == 0 {
			t.Fatalf("fixture covers no %q row", st)
		}
	}

	seen := map[int64]int{}
	for st, want := range byState {
		got := idSet(listIDs(t, WorksListFilter{Sort: "id", ClaimStates: []string{st}}))
		if len(got) != len(want) {
			t.Fatalf("claim_state=%s selected %d rows, want %d (%v vs %v)", st, len(got), len(want), got, want)
		}
		for _, id := range want {
			if !got[id] {
				t.Fatalf("claim_state=%s must select work %d (model.ClaimStateKey says %s)", st, id, st)
			}
			seen[id]++
		}
	}
	for _, id := range all {
		if seen[id] != 1 {
			t.Fatalf("work %d matched %d states, want exactly 1", id, seen[id])
		}
	}
	if n := len(idSet(listIDs(t, WorksListFilter{
		Sort: "id",
		ClaimStates: []string{
			model.ClaimStateKeyNone, model.ClaimStateKeyLive,
			model.ClaimStateKeyDraft, model.ClaimStateKeyPending,
			model.ClaimStateKeyDeclined, model.ClaimStateKeyHidden,
		},
	}))); n != len(all) {
		t.Fatalf("all six states selected %d rows, want the whole set of %d", n, len(all))
	}
}

func TestWorksListClaimStateGate(t *testing.T) {
	cleanTables(t)
	cleanTagTables(t)
	byState, all := claimStateFixture(t)

	// Was "no claim_state = no gate". A ban writes only claim_state and never
	// status, so an unfiltered GET /v2/catalog/works kept serving banned works;
	// the decision face promises ban hides the work "from any state", so the
	// exclusion does not wait for the caller to pass claim_state=.
	banned := idSet(byState[model.ClaimStateKeyHidden])
	if len(banned) == 0 {
		t.Fatalf("fixture covers no banned row, so this assertion proves nothing")
	}
	ungated := listIDs(t, WorksListFilter{Sort: "id"})
	if len(ungated) != len(all)-len(banned) {
		t.Fatalf("no claim_state must serve every row but the banned ones: got %d, want %d",
			len(ungated), len(all)-len(banned))
	}
	for _, id := range ungated {
		if banned[id] {
			t.Fatalf("the default page leaked banned work %d", id)
		}
	}
	if got := idSet(listIDs(t, WorksListFilter{Sort: "id", ClaimStates: []string{model.ClaimStateKeyHidden}})); len(got) != len(banned) {
		t.Fatalf("an explicit claim_state=hidden must still reach all %d banned rows, got %d", len(banned), len(got))
	}

	live := idSet(listIDs(t, WorksListFilter{Sort: "id", ClaimStates: []string{model.ClaimStateKeyLive}}))
	for _, st := range []string{
		model.ClaimStateKeyDraft, model.ClaimStateKeyPending,
		model.ClaimStateKeyDeclined, model.ClaimStateKeyHidden, model.ClaimStateKeyNone,
	} {
		for _, id := range byState[st] {
			if live[id] {
				t.Fatalf("claim_state=live leaked a %s work (%d) — the incident this gate closes", st, id)
			}
		}
	}
	for _, id := range byState[model.ClaimStateKeyLive] {
		if !live[id] {
			t.Fatalf("claim_state=live dropped work %d, which renders state=live", id)
		}
	}

	csv := idSet(listIDs(t, WorksListFilter{
		Sort:        "id",
		ClaimStates: []string{model.ClaimStateKeyLive, model.ClaimStateKeyDraft},
	}))
	want := append(append([]int64{}, byState[model.ClaimStateKeyLive]...), byState[model.ClaimStateKeyDraft]...)
	if len(csv) != len(want) {
		t.Fatalf("claim_state=live,draft selected %d rows, want %d", len(csv), len(want))
	}
	for _, id := range want {
		if !csv[id] {
			t.Fatalf("claim_state=live,draft missing work %d", id)
		}
	}

	svc := newPublicSvc()
	onePage, err := svc.WorksList(t.Context(), WorksListFilter{
		Sort: "id", ClaimStates: []string{model.ClaimStateKeyLive, model.ClaimStateKeyDraft},
	}, "", 100)
	if err != nil {
		t.Fatalf("WorksList single page: %v", err)
	}
	if len(onePage.Items) != len(csv) {
		t.Fatalf("single page served %d rows but the keyset walk served %d — the gate must not depend on paging",
			len(onePage.Items), len(csv))
	}
}

func TestWorksListBBucketSupply(t *testing.T) {
	cleanTables(t)
	cleanTagTables(t)
	byState, _ := claimStateFixture(t)

	bucket := []string{model.ClaimStateKeyLive, model.ClaimStateKeyDraft, model.ClaimStateKeyPending}
	got := idSet(listIDs(t, WorksListFilter{Sort: "id", ClaimStates: bucket}))

	var want []int64
	for _, st := range bucket {
		want = append(want, byState[st]...)
	}
	if len(got) != len(want) {
		t.Fatalf("B bucket selected %d rows, want %d (%v)", len(got), len(want), got)
	}
	for _, id := range want {
		if !got[id] {
			t.Fatalf("B bucket missing work %d", id)
		}
	}
	for _, st := range []string{model.ClaimStateKeyDeclined, model.ClaimStateKeyHidden, model.ClaimStateKeyNone} {
		for _, id := range byState[st] {
			if got[id] {
				t.Fatalf("B bucket leaked a %s work (%d)", st, id)
			}
		}
	}
}

func TestWorksListClaimStateOrthogonalToNSFW(t *testing.T) {
	cleanTables(t)
	cleanTagTables(t)

	sfwLive := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "全年齢公開")
	r18Live := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "成人公開")
	r18Draft := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "成人下書き")
	for i, id := range []int64{sfwLive.ID, r18Live.ID, r18Draft.ID} {
		claimWork(t, id, "galgame_wiki", int64(9200+i))
	}
	setClaimState(t, sfwLive.ID, i16(model.ClaimStateLive))
	setClaimState(t, r18Live.ID, i16(model.ClaimStateLive))
	setClaimState(t, r18Draft.ID, i16(model.ClaimStateDraft))

	for _, tc := range []struct {
		name  string
		nsfw  bool
		state []string
		want  []int64
	}{
		{"sfw, no claim gate", false, nil, []int64{sfwLive.ID}},
		{"nsfw, no claim gate", true, nil, []int64{sfwLive.ID, r18Live.ID, r18Draft.ID}},
		{"sfw + live", false, []string{model.ClaimStateKeyLive}, []int64{sfwLive.ID}},
		{"nsfw + live", true, []string{model.ClaimStateKeyLive}, []int64{sfwLive.ID, r18Live.ID}},
		{"sfw + draft", false, []string{model.ClaimStateKeyDraft}, nil},
		{"nsfw + draft", true, []string{model.ClaimStateKeyDraft}, []int64{r18Draft.ID}},
	} {
		got := idSet(listIDs(t, WorksListFilter{Sort: "id", NSFW: tc.nsfw, ClaimStates: tc.state}))
		if len(got) != len(tc.want) {
			t.Fatalf("%s: got %d rows %v, want %d %v", tc.name, len(got), got, len(tc.want), tc.want)
		}
		for _, id := range tc.want {
			if !got[id] {
				t.Fatalf("%s: work %d missing", tc.name, id)
			}
		}
	}
}
