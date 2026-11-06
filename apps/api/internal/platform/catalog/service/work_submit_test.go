package service

import (
	"errors"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/provenance"
)

const submitSite = "kungal"

func submitFields(name string) map[string]any {
	return map[string]any{
		editspec.FieldWorkDisplayName:   name,
		editspec.FieldWorkOLang:         "ja",
		editspec.FieldWorkContentRating: float64(model.ContentRatingR18),
		editspec.FieldWorkTitles: []any{
			map[string]any{"lang": "ja", "title": name, "kind": float64(0)},
			map[string]any{"lang": "zh-Hans", "title": "投稿作品", "kind": float64(1)},
		},
		editspec.FieldWorkIntros: []any{
			map[string]any{"lang": "zh-Hans", "intro": "投稿者が書いた紹介。"},
		},
		editspec.FieldWorkLinks: []any{"https://vndb.org/v19658"},
	}
}

func submitFieldsNoAnchor(name string) map[string]any {
	f := submitFields(name)
	delete(f, editspec.FieldWorkLinks)
	return f
}

func TestSubmitWorkMintsPendingClaim(t *testing.T) {
	s := newLifecycle(t)
	ctx := t.Context()

	res, err := s.SubmitWork(ctx, SubmitWorkParams{
		Site: submitSite, ProductWorkID: 90001, ActorUID: 7,
		Fields:   submitFields("新作ゲーム"),
		Released: ReleaseDate{Y: 2019, M: 5},
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if res.ClaimState != model.ClaimStateKeyPending || res.WorkID == 0 || res.EventID == 0 || res.ReleaseID == 0 {
		t.Fatalf("result: %+v", res)
	}
	if res.ProductWorkID != 90001 {
		t.Fatalf("a supplied product id must be echoed verbatim: %+v", res)
	}

	var work model.CatalogWork
	if err := testDB.First(&work, res.WorkID).Error; err != nil {
		t.Fatal(err)
	}
	if work.MediumID != galgameMediumID || work.Site == nil || *work.Site != submitSite ||
		work.ProductWorkID == nil || *work.ProductWorkID != 90001 {
		t.Fatalf("claim identity: %+v", work)
	}
	if work.ClaimState == nil || *work.ClaimState != model.ClaimStatePending {
		t.Fatalf("claim_state: %v", work.ClaimState)
	}
	if work.Status != model.WorkStatusLive {
		t.Fatalf("registry status: %d", work.Status)
	}
	if work.DisplayName != "新作ゲーム" || work.OLang != "ja" || work.ContentRating != model.ContentRatingR18 {
		t.Fatalf("scalars: %+v", work)
	}

	var titles int64
	testDB.Raw(`SELECT count(*) FROM catalog_work_title WHERE work_id = ?`, res.WorkID).Scan(&titles)
	if titles != 2 {
		t.Fatalf("titles: %d", titles)
	}
	var intros int64
	testDB.Raw(`SELECT count(*) FROM catalog_work_intro WHERE work_id = ? AND lang = 'zh-Hans'`, res.WorkID).Scan(&intros)
	if intros != 1 {
		t.Fatalf("intros: %d", intros)
	}
	var linkKind int16
	if err := testDB.Raw(`SELECT link_kind FROM catalog_external_ref
	                      WHERE entity_id = ? AND entity_type = ? AND matched_by = 'curated'`,
		res.WorkID, model.EntityTypeWork).Scan(&linkKind).Error; err != nil {
		t.Fatal(err)
	}
	if linkKind != model.LinkKindProbable {
		t.Fatalf("submitted vndb link must be a candidate, got link_kind %d", linkKind)
	}

	var rel model.CatalogRelease
	if err := testDB.First(&rel, res.ReleaseID).Error; err != nil {
		t.Fatal(err)
	}
	if rel.WorkID != res.WorkID || rel.ReleasedY == nil || *rel.ReleasedY != 2019 ||
		rel.ReleasedM == nil || *rel.ReleasedM != 5 || rel.ReleasedD != nil {
		t.Fatalf("release: %+v", rel)
	}

	var revisions int64
	testDB.Raw(`SELECT count(*) FROM catalog_revision WHERE entity_type = ? AND entity_id = ? AND action = ?`,
		model.EntityTypeWork, res.WorkID, model.RevisionActionCreated).Scan(&revisions)
	if revisions != 1 {
		t.Fatalf("registry revisions: %d", revisions)
	}

	events, err := s.EventsSince(ctx, 0, 10, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events: %+v", events)
	}
	e := events[0]
	if e.FromState != nil || e.ToState != model.ClaimStateKeyPending ||
		e.WorkID != res.WorkID || e.ActorUID != 7 || e.Site != submitSite {
		t.Fatalf("birth event: %+v", e)
	}
}

func TestSubmitWorkTrustedMintsLiveClaim(t *testing.T) {
	s := newLifecycle(t)
	ctx := t.Context()

	res, err := s.SubmitWork(ctx, SubmitWorkParams{
		Site: submitSite, ProductWorkID: 90010, ActorUID: 7,
		Fields: submitFields("直接公開ゲーム"), Trusted: true,
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if res.ClaimState != model.ClaimStateKeyLive || res.WorkID == 0 || res.EventID == 0 {
		t.Fatalf("result: %+v", res)
	}

	var work model.CatalogWork
	if err := testDB.First(&work, res.WorkID).Error; err != nil {
		t.Fatal(err)
	}
	if work.ClaimState == nil || *work.ClaimState != model.ClaimStateLive {
		t.Fatalf("claim_state: %v", work.ClaimState)
	}

	events, err := s.EventsSince(ctx, 0, 10, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events: %+v", events)
	}
	e := events[0]
	if e.FromState != nil || e.ToState != model.ClaimStateKeyLive ||
		e.WorkID != res.WorkID || e.ActorUID != 7 || e.Site != submitSite {
		t.Fatalf("birth event: %+v", e)
	}
}

func TestSubmitWorkStampsReleaseDateAsUser(t *testing.T) {
	s := newLifecycle(t)
	res, err := s.SubmitWork(t.Context(), SubmitWorkParams{
		Site: submitSite, ProductWorkID: 90011, ActorUID: 7,
		Fields:   submitFields("日付投稿"),
		Released: ReleaseDate{Y: 2019, M: 5},
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	var rel model.CatalogRelease
	if err := testDB.First(&rel, res.ReleaseID).Error; err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"released_y", "released_m", "released_d"} {
		if head := provenance.FirstSource(rel.FieldProvenance, column); head != provenance.SourceUser {
			t.Errorf("submit-minted field_provenance[%s] = %q, want %q",
				column, head, provenance.SourceUser)
		}
	}

	var wouldFill int64
	if err := testDB.Raw(
		`SELECT count(*) FROM catalog_release
		 WHERE id = ? AND COALESCE(field_provenance -> 'released_y' -> 0 ->> 'source', '') NOT IN ?`,
		res.ReleaseID, provenance.HumanSources()).Scan(&wouldFill).Error; err != nil {
		t.Fatal(err)
	}
	if wouldFill != 0 {
		t.Fatal("the exact C7 guard expression must skip a submit-minted row")
	}
}

func TestSubmitWorkIsIdempotent(t *testing.T) {
	s := newLifecycle(t)
	ctx := t.Context()
	params := SubmitWorkParams{
		Site: submitSite, ProductWorkID: 90002, ActorUID: 7, Fields: submitFields("二重投稿"),
	}
	first, err := s.SubmitWork(ctx, params)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	_, err = s.SubmitWork(ctx, params)
	var exists *ClaimExistsError
	if !errors.As(err, &exists) {
		t.Fatalf("second submit: %v", err)
	}
	if exists.WorkID != first.WorkID || exists.CurrentState != model.ClaimStateKeyPending {
		t.Fatalf("conflict echo: %+v", exists)
	}
	act(t, s, first.WorkID, ClaimActionApprove, ClaimActionParams{ActorUID: 99})
	_, err = s.SubmitWork(ctx, params)
	if !errors.As(err, &exists) || exists.CurrentState != model.ClaimStateKeyLive {
		t.Fatalf("post-approval conflict: %v / %+v", err, exists)
	}
	var works int64
	testDB.Raw(`SELECT count(*) FROM catalog_work WHERE site = ? AND product_work_id = ?`,
		submitSite, 90002).Scan(&works)
	if works != 1 {
		t.Fatalf("mint count: %d", works)
	}
}

func TestSubmitWorkIssuesTheIdentity(t *testing.T) {
	s := newLifecycle(t)
	ctx := t.Context()

	res, err := s.SubmitWork(ctx, SubmitWorkParams{
		Site: submitSite, ActorUID: 7, Fields: submitFieldsNoAnchor("番号なし投稿"),
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if res.ProductWorkID != res.WorkID {
		t.Fatalf("the claim must adopt the minted work id: %+v", res)
	}
	if res.ClaimState != model.ClaimStateKeyPending {
		t.Fatalf("result: %+v", res)
	}

	var work model.CatalogWork
	if err := testDB.First(&work, res.WorkID).Error; err != nil {
		t.Fatal(err)
	}
	if work.ProductWorkID == nil || *work.ProductWorkID != res.WorkID {
		t.Fatalf("stored claim: %+v", work.ProductWorkID)
	}
	if got := model.ClaimStateKey(work.Site, work.ProductWorkID, work.ClaimState); got != model.ClaimStateKeyPending {
		t.Fatalf("projection: %s", got)
	}

	events, err := s.EventsSince(ctx, 0, 10, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].FromState != nil ||
		events[0].ToState != model.ClaimStateKeyPending || events[0].WorkID != res.WorkID {
		t.Fatalf("birth event: %+v", events)
	}
	if events[0].ProductWorkID == nil || *events[0].ProductWorkID != res.WorkID {
		t.Fatalf("feed snapshot: %+v", events[0])
	}
	items, total, err := s.PendingClaims(ctx, submitSite, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].WorkID != res.WorkID {
		t.Fatalf("queue: total=%d items=%+v", total, items)
	}
}

func TestSubmitWorkIssuedIdempotency(t *testing.T) {
	s := newLifecycle(t)
	ctx := t.Context()

	anchored := SubmitWorkParams{Site: submitSite, ActorUID: 7, Fields: submitFields("锚あり")}
	first, err := s.SubmitWork(ctx, anchored)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	_, err = s.SubmitWork(ctx, anchored)
	var exists *ClaimExistsError
	if !errors.As(err, &exists) {
		t.Fatalf("anchored retry: %v", err)
	}
	if exists.WorkID != first.WorkID || exists.MatchedBy != ClaimMatchAnchor ||
		exists.CurrentState != model.ClaimStateKeyPending || exists.Anchor != "vndb:v19658" {
		t.Fatalf("conflict echo: %+v", exists)
	}
	if exists.ProductWorkID != first.WorkID {
		t.Fatalf("the conflict must carry the issued product id: %+v", exists)
	}

	other := anchored
	other.ActorUID = 8
	if _, err := s.SubmitWork(ctx, other); !errors.As(err, &exists) {
		t.Fatalf("second submitter: %v", err)
	}

	// The three lanes below re-send a title the first mint already owns, which
	// the duplicate gate refuses on its own. They are anchor assertions, so they
	// go through the confirm door rather than around the gate.
	foreign := anchored
	foreign.Site = "moyu"
	foreign.ConfirmDuplicates = true
	if _, err := s.SubmitWork(ctx, foreign); err != nil {
		t.Fatalf("another tenant must still be able to submit: %v", err)
	}

	supplied := anchored
	supplied.ProductWorkID = 90777
	supplied.ConfirmDuplicates = true
	if _, err := s.SubmitWork(ctx, supplied); err != nil {
		t.Fatalf("a supplied id must not be diverted by the anchor: %v", err)
	}

	bare := SubmitWorkParams{
		Site: submitSite, ActorUID: 7, Fields: submitFieldsNoAnchor("锚なし"),
		ConfirmDuplicates: true,
	}
	a, err := s.SubmitWork(ctx, bare)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.SubmitWork(ctx, bare)
	if err != nil {
		t.Fatal(err)
	}
	if a.WorkID == b.WorkID {
		t.Fatal("unexpectedly idempotent — update the endpoint documentation")
	}
}

func TestSubmitWorkOnTheFormerMirrorSite(t *testing.T) {
	s := newLifecycle(t)
	ctx := t.Context()
	fields := submitFields("鏡面作品")
	if _, ok := fields[editspec.FieldWorkTitles]; !ok {
		t.Fatal("this test is only meaningful while titles is in the submission set")
	}

	res, err := s.SubmitWork(ctx, SubmitWorkParams{
		Site: "galgame_wiki", ProductWorkID: 90003, ActorUID: 7, Fields: fields,
	})
	if err != nil {
		t.Fatalf("submit with the formerly gated facets: %v", err)
	}
	if res.ClaimState != model.ClaimStateKeyPending {
		t.Fatalf("result: %+v", res)
	}
	var titles int64
	testDB.Raw(`SELECT count(*) FROM catalog_work_title WHERE work_id = ?`, res.WorkID).Scan(&titles)
	if titles == 0 {
		t.Fatal("titles must actually be written, not silently dropped")
	}
}

func TestSubmitWorkRejectsPayloads(t *testing.T) {
	s := newLifecycle(t)
	ctx := t.Context()
	base := func() SubmitWorkParams {
		return SubmitWorkParams{Site: submitSite, ProductWorkID: 90004, ActorUID: 7, Fields: submitFields("拒否")}
	}

	cases := []struct {
		name    string
		mutate  func(*SubmitWorkParams)
		wantErr error
	}{
		{"no site", func(p *SubmitWorkParams) { p.Site = "" }, ErrSubmitTargetRequired},
		{"negative product id", func(p *SubmitWorkParams) { p.ProductWorkID = -1 }, ErrSubmitTargetRequired},
		{"no display name", func(p *SubmitWorkParams) {
			delete(p.Fields, editspec.FieldWorkDisplayName)
		}, ErrSubmitDisplayNameRequired},
		{"day without month", func(p *SubmitWorkParams) {
			p.Released = ReleaseDate{Y: 2019, D: 4}
		}, ErrSubmitInvalidDate},
		{"impossible month", func(p *SubmitWorkParams) {
			p.Released = ReleaseDate{Y: 2019, M: 13}
		}, ErrSubmitInvalidDate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := base()
			tc.mutate(&p)
			if _, err := s.SubmitWork(ctx, p); !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v want %v", err, tc.wantErr)
			}
		})
	}

	t.Run("covers are not submittable", func(t *testing.T) {
		p := base()
		p.Fields[editspec.FieldWorkCovers] = []any{}
		var fieldErr *editspec.SubmissionFieldError
		if _, err := s.SubmitWork(ctx, p); !errors.As(err, &fieldErr) ||
			fieldErr.Field != editspec.FieldWorkCovers {
			t.Fatalf("covers: %v", err)
		}
	})
	t.Run("unregistered key", func(t *testing.T) {
		p := base()
		p.Fields["catalog.work.status"] = 1
		var fieldErr *editspec.SubmissionFieldError
		if _, err := s.SubmitWork(ctx, p); !errors.As(err, &fieldErr) {
			t.Fatalf("status: %v", err)
		}
	})
	t.Run("field validator still runs", func(t *testing.T) {
		p := base()
		p.Fields[editspec.FieldWorkOLang] = "klingon"
		var fieldErr *editspec.SubmissionFieldError
		if _, err := s.SubmitWork(ctx, p); !errors.As(err, &fieldErr) ||
			fieldErr.Field != editspec.FieldWorkOLang || fieldErr.Unwrap() == nil {
			t.Fatalf("olang: %v", err)
		}
	})

	var works int64
	testDB.Raw(`SELECT count(*) FROM catalog_work WHERE product_work_id = ?`, 90004).Scan(&works)
	if works != 0 {
		t.Fatalf("refusals minted %d rows", works)
	}
}

func TestSubmitWorkFeedsTheReviewQueue(t *testing.T) {
	s := newLifecycle(t)
	ctx := t.Context()
	res, err := s.SubmitWork(ctx, SubmitWorkParams{
		Site: submitSite, ProductWorkID: 90005, ActorUID: 7, Fields: submitFields("審査待ち"),
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	items, total, err := s.PendingClaims(ctx, submitSite, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].WorkID != res.WorkID {
		t.Fatalf("queue: total=%d items=%+v", total, items)
	}
	if items[0].SubmittedEventID == nil || *items[0].SubmittedEventID != res.EventID {
		t.Fatalf("queue must point at the birth event: %+v", items[0])
	}
	approved := act(t, s, res.WorkID, ClaimActionApprove, ClaimActionParams{ActorUID: 99})
	if approved.From == nil || *approved.From != model.ClaimStateKeyPending ||
		approved.To != model.ClaimStateKeyLive {
		t.Fatalf("approve: %+v", approved)
	}
}

func countAll(t *testing.T, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := testDB.Raw(query, args...).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSubmitWorkGateRefusesSpacingVariantOfLiveTitle(t *testing.T) {
	s := newLifecycle(t)
	existing := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "門番検証タイトル")

	_, err := s.SubmitWork(t.Context(), SubmitWorkParams{
		Site: submitSite, ProductWorkID: 90601, ActorUID: 7,
		Fields: submitFieldsNoAnchor("門番　検証 タイトル"),
	})
	var dupes *WorkDupeSuspectsError
	if !errors.As(err, &dupes) {
		t.Fatalf("a whitespace variant of a live title must be refused, got %v", err)
	}
	if len(dupes.Suspects) != 1 || dupes.Suspects[0].WorkID != existing.ID ||
		dupes.Suspects[0].DisplayName != "門番検証タイトル" {
		t.Fatalf("suspects: %+v", dupes.Suspects)
	}
	if n := countAll(t, `SELECT count(*) FROM catalog_work`); n != 1 {
		t.Fatalf("the refusal must roll the whole mint back, got %d works", n)
	}
	if n := countAll(t, `SELECT count(*) FROM catalog_match_candidate`); n != 0 {
		t.Fatalf("a refused mint files no receipt, got %d", n)
	}
}

func TestSubmitWorkGateFiresOnSubmittedTitlesField(t *testing.T) {
	s := newLifecycle(t)
	existing := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "別名一致検証作品")

	fields := submitFieldsNoAnchor("重ならない表示名")
	fields[editspec.FieldWorkTitles] = []any{
		map[string]any{"lang": "ja", "title": "別名 一致 検証 作品", "kind": float64(0)},
	}
	_, err := s.SubmitWork(t.Context(), SubmitWorkParams{
		Site: submitSite, ProductWorkID: 90602, ActorUID: 7, Fields: fields,
	})
	var dupes *WorkDupeSuspectsError
	if !errors.As(err, &dupes) {
		t.Fatalf("a titles-only collision must be refused, got %v", err)
	}
	if len(dupes.Suspects) != 1 || dupes.Suspects[0].WorkID != existing.ID {
		t.Fatalf("suspects: %+v", dupes.Suspects)
	}
}

func TestSubmitWorkConfirmDuplicatesMintsAndFilesCandidate(t *testing.T) {
	s := newLifecycle(t)
	existing := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "確認後鋳造検証作品")

	params := SubmitWorkParams{
		Site: submitSite, ProductWorkID: 90603, ActorUID: 7,
		Fields: submitFieldsNoAnchor("確認後　鋳造 検証 作品"),
	}
	var dupes *WorkDupeSuspectsError
	if _, err := s.SubmitWork(t.Context(), params); !errors.As(err, &dupes) {
		t.Fatalf("unconfirmed: %v", err)
	}

	params.ConfirmDuplicates = true
	res, err := s.SubmitWork(t.Context(), params)
	if err != nil {
		t.Fatalf("confirmed submit: %v", err)
	}
	if res.WorkID == 0 || res.ClaimState != model.ClaimStateKeyPending {
		t.Fatalf("result: %+v", res)
	}

	var cands []model.CatalogMatchCandidate
	if err := testDB.Order("a_id, b_id").Find(&cands).Error; err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 {
		t.Fatalf("a confirmed mint still files the pair for reconciliation, got %+v", cands)
	}
	if cands[0].AID != min(existing.ID, res.WorkID) || cands[0].BID != max(existing.ID, res.WorkID) ||
		cands[0].Status != model.CandidateStatusPending {
		t.Fatalf("candidate: %+v", cands[0])
	}
}

func TestSubmitWorkGateIgnoresOtherMedia(t *testing.T) {
	s := newLifecycle(t)
	createWorkX(t, animeMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "他媒体同名検証作品")

	res, err := s.SubmitWork(t.Context(), SubmitWorkParams{
		Site: submitSite, ProductWorkID: 90605, ActorUID: 7,
		Fields: submitFieldsNoAnchor("他媒体 同名 検証 作品"),
	})
	if err != nil {
		t.Fatalf("a same-title anime is a deliberate cross-media adaptation, not a duplicate: %v", err)
	}
	if res.WorkID == 0 {
		t.Fatalf("result: %+v", res)
	}
}

func TestSubmitWorkGateIgnoresAOneRuneTitle(t *testing.T) {
	s := newLifecycle(t)
	createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "x")

	fields := submitFieldsNoAnchor("x")
	fields[editspec.FieldWorkTitles] = []any{
		map[string]any{"lang": "ja", "title": "x", "kind": float64(0)},
	}
	res, err := s.SubmitWork(t.Context(), SubmitWorkParams{
		Site: submitSite, ProductWorkID: 90604, ActorUID: 7, Fields: fields,
	})
	if err != nil {
		t.Fatalf("a one-rune name is below the official floor and must mint: %v", err)
	}
	if res.WorkID == 0 {
		t.Fatalf("result: %+v", res)
	}
}
