package service

import (
	"testing"

	"api/internal/platform/catalog/model"
	catsearch "api/internal/platform/catalog/search"
)

func TestClaimStateProjectionIsOneDefinition(t *testing.T) {
	site, wiki, pwid := "galgame_wiki", "galgame_wiki", int64(42)
	draft, hidden, live, bogus := model.ClaimStateDraft, model.ClaimStateHidden, model.ClaimStateLive, int16(99)
	pending, declined := model.ClaimStatePending, model.ClaimStateDeclined
	empty := ""

	for _, tc := range []struct {
		name  string
		site  *string
		pwid  *int64
		state *int16
		want  string
	}{
		{"unclaimed (site NULL)", nil, nil, nil, model.ClaimStateKeyNone},
		{"unclaimed (site empty)", &empty, &pwid, nil, model.ClaimStateKeyNone},
		{"site without a product work id", &site, nil, nil, model.ClaimStateKeyNone},
		{"claimed, column NULL", &wiki, &pwid, nil, model.ClaimStateKeyLive},
		{"claimed live", &wiki, &pwid, &live, model.ClaimStateKeyLive},
		{"claimed draft", &wiki, &pwid, &draft, model.ClaimStateKeyDraft},
		{"claimed hidden", &wiki, &pwid, &hidden, model.ClaimStateKeyHidden},
		{"claimed pending", &wiki, &pwid, &pending, model.ClaimStateKeyPending},
		{"claimed declined", &wiki, &pwid, &declined, model.ClaimStateKeyDeclined},
		{"unknown state is conservatively hidden", &wiki, &pwid, &bogus, model.ClaimStateKeyHidden},
	} {
		if got := model.ClaimStateKey(tc.site, tc.pwid, tc.state); got != tc.want {
			t.Fatalf("%s: ClaimStateKey = %q, want %q", tc.name, got, tc.want)
		}
		cb := claimedBy(tc.site, tc.pwid, tc.state, shelfFacts{}, model.ContentRatingAllAges)
		if tc.want == model.ClaimStateKeyNone {
			if cb != nil {
				t.Fatalf("%s: claimed_by = %+v, want null", tc.name, cb)
			}
			continue
		}
		if cb == nil || cb.State != tc.want {
			t.Fatalf("%s: claimed_by = %+v, want state %q", tc.name, cb, tc.want)
		}
	}
}

func TestClaimStateVocabularyIsClosed(t *testing.T) {
	for _, ok := range []string{"none", "live", "draft", "pending", "declined", "hidden"} {
		if !IsWorksSearchClaimState(ok) {
			t.Fatalf("%q must be a legal claim_state token", ok)
		}
	}
	for _, bad := range []string{"", "liev", "LIVE", "published", "true", "claimed"} {
		if IsWorksSearchClaimState(bad) {
			t.Fatalf("%q must NOT be a legal claim_state token", bad)
		}
	}
}

// The ban gate is a negated filter, and Meilisearch answers a negated filter
// with zero hits — no error — when no document in the index carries the
// attribute at all. Any caller that builds a work document without a claim state
// therefore removes itself from every search, and an index of them empties the
// whole works face.
func TestWorksSearchServesDocumentsBuiltWithoutAClaimState(t *testing.T) {
	cleanTables(t)
	cleanTagTables(t)
	cleanTaxonomyTables(t)
	idx := worksSearchIndexer(t)
	svc := newPublicSvc().WithWorksSearch(idx)

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "状態無し作品")
	indexWorks(t, idx, []catsearch.WorkDocInput{{
		ID: w.ID, DisplayName: "状態無し作品", OLang: "ja",
		ContentRating: model.ContentRatingAllAges, UpdatedTS: 1700000000,
	}})

	for _, q := range []string{"", "状態無し作品"} {
		data, err := svc.WorksSearch(t.Context(), WorksSearchFilter{Q: q, Sort: "id", Limit: 50})
		if err != nil {
			t.Fatalf("q=%q: WorksSearch: %v", q, err)
		}
		if len(data.Items) != 1 || data.Items[0].ID != w.ID {
			t.Fatalf("q=%q: a document built without a claim_state must still be served, got %d items",
				q, len(data.Items))
		}
	}
}

func TestWorksSearchClaimStateGate(t *testing.T) {
	cleanTables(t)
	cleanTagTables(t)
	cleanTaxonomyTables(t)
	idx := worksSearchIndexer(t)
	svc := newPublicSvc().WithWorksSearch(idx)

	bodyless := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "無認領作品")
	live := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "公開作品")
	draft := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "下書き作品")
	hidden := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "取下げ作品")
	for i, w := range []int64{live.ID, draft.ID, hidden.ID} {
		claimWork(t, w, "galgame_wiki", int64(7100+i))
	}
	setClaimState(t, draft.ID, i16(model.ClaimStateDraft))
	setClaimState(t, hidden.ID, i16(model.ClaimStateHidden))

	docs := make([]catsearch.WorkDocInput, 0, 4)
	for _, w := range []struct {
		id   int64
		name string
	}{
		{bodyless.ID, "無認領作品"}, {live.ID, "公開作品"},
		{draft.ID, "下書き作品"}, {hidden.ID, "取下げ作品"},
	} {
		var row struct {
			Site          *string `gorm:"column:site"`
			ProductWorkID *int64  `gorm:"column:product_work_id"`
			ClaimState    *int16  `gorm:"column:claim_state"`
		}
		if err := testDB.Raw(
			`SELECT site, product_work_id, claim_state FROM catalog_work WHERE id = ?`, w.id).
			Scan(&row).Error; err != nil {
			t.Fatalf("read claim columns: %v", err)
		}
		docs = append(docs, catsearch.WorkDocInput{
			ID: w.id, DisplayName: w.name, OLang: "ja",
			ContentRating: model.ContentRatingAllAges,
			Claimed:       row.Site != nil && *row.Site != "",
			ClaimState:    model.ClaimStateKey(row.Site, row.ProductWorkID, row.ClaimState),
			UpdatedTS:     1700000000,
		})
	}
	indexWorks(t, idx, docs)

	for _, tc := range []struct {
		states []string
		want   []int64
	}{
		// No claim_state= must serve every state but the banned one, matching
		// the browse lane; this row still asserted the pre-ban-gate contract
		// after the gate landed.
		{nil, []int64{bodyless.ID, live.ID, draft.ID}},
		{[]string{model.ClaimStateKeyLive}, []int64{live.ID}},
		{[]string{model.ClaimStateKeyLive, model.ClaimStateKeyDraft}, []int64{live.ID, draft.ID}},
		{[]string{model.ClaimStateKeyNone}, []int64{bodyless.ID}},
		{[]string{model.ClaimStateKeyHidden}, []int64{hidden.ID}},
	} {
		data, err := svc.WorksSearch(t.Context(), WorksSearchFilter{
			Sort: "id", ClaimStates: tc.states, Limit: 50,
		})
		if err != nil {
			t.Fatalf("claim_state=%v: WorksSearch: %v", tc.states, err)
		}
		got := make(map[int64]bool, len(data.Items))
		for _, it := range data.Items {
			got[it.ID] = true
		}
		if len(got) != len(tc.want) {
			t.Fatalf("claim_state=%v: got %d items, want %d", tc.states, len(got), len(tc.want))
		}
		for _, id := range tc.want {
			if !got[id] {
				t.Fatalf("claim_state=%v: work %d missing from %v", tc.states, id, got)
			}
		}
		if int(data.Total) != len(data.Items) {
			t.Fatalf("claim_state=%v: total=%d but page carries %d rows — total and items must share one filter",
				tc.states, data.Total, len(data.Items))
		}
	}
}
