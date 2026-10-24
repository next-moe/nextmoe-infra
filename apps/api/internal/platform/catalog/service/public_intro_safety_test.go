package service

import (
	"reflect"
	"testing"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/model"
)

const (
	srcBangumiIntro int16 = 3
	srcGetchuIntro  int16 = 17
)

// seedJudgedIntros gives a work one intro per verdict outcome, plus a language
// whose first source was dropped and whose second was cleared.
func seedJudgedIntros(t *testing.T, workID int64, tag string) {
	t.Helper()
	addUnjudgedWorkIntro(t, workID, "ja", tag+"露骨な原文", srcBangumiIntro, 0)
	markIntroSafety(t, tag+"露骨な原文", model.IntroSFWDropped, "")
	addUnjudgedWorkIntro(t, workID, "ja", tag+"安全な別ソース", srcGetchuIntro, 0)
	markIntroSafety(t, tag+"安全な別ソース", model.IntroSFWOriginal, "")
	addUnjudgedWorkIntro(t, workID, "zh-Hans", tag+"含有成人句子的原文", srcVNDB, 0)
	markIntroSafety(t, tag+"含有成人句子的原文", model.IntroSFWRedacted, tag+"删句后的版本")
	addUnjudgedWorkIntro(t, workID, "en", tag+"An explicit original", srcVNDB, 0)
	markIntroSafety(t, tag+"An explicit original", model.IntroSFWRewritten, tag+"A rewritten summary")
	addUnjudgedWorkIntro(t, workID, "ko", tag+"아직 판정 전", srcVNDB, 0)
}

func sfwIntros(tag string) []dto.PublicIntro {
	return []dto.PublicIntro{
		{Lang: "en", Intro: tag + "A rewritten summary", Source: "vndb", Machine: true},
		{Lang: "ja", Intro: tag + "安全な別ソース", Source: "getchu"},
		{Lang: "zh-Hans", Intro: tag + "删句后的版本", Source: "vndb"},
	}
}

func introsByWork(t *testing.T, svc *PublicService, ids []int64, nsfw bool) map[int64][]dto.PublicIntro {
	t.Helper()
	page, err := svc.WorksList(t.Context(), WorksListFilter{
		Sort: "id", NSFW: nsfw, IDs: ids, Include: ParseWorksListInclude("intros"),
	}, "", 50)
	if err != nil {
		t.Fatalf("WorksList: %v", err)
	}
	out := map[int64][]dto.PublicIntro{}
	for _, it := range page.Items {
		out[it.ID] = it.Intros
	}
	return out
}

// The downstreams hydrate every page with nsfw=true and gate on content_limit
// themselves, so nsfw=true cannot mean "an adult page": an sfw-shelf work shows
// only judged text even then, exactly as its covers already do.
func TestSFWShelfIntrosAreTheJudgedFace(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvcCDN()
	w := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "SFW shelf")
	setTitleNSFW(t, w.ID, new(bool))
	seedJudgedIntros(t, w.ID, "s")
	want := sfwIntros("s")

	rec, found, err := svc.WorkDetail(t.Context(), w.ID, PublicInclude{}, true, 0, PublicFields{})
	if err != nil || !found {
		t.Fatalf("WorkDetail: found=%v err=%v", found, err)
	}
	if rec.ContentLimit != model.DisplayLimitKeySFW {
		t.Fatalf("content_limit = %q, want sfw (title judged safe, unclaimed r18)", rec.ContentLimit)
	}
	if !reflect.DeepEqual(rec.Intros, want) {
		t.Fatalf("detail intros = %+v, want %+v", rec.Intros, want)
	}
	if got := introsByWork(t, svc, []int64{w.ID}, true)[w.ID]; !reflect.DeepEqual(got, want) {
		t.Fatalf("list intros = %+v, want %+v", got, want)
	}
	sub, found, err := svc.WorkIntros(t.Context(), w.ID, true, 50, 0)
	if err != nil || !found {
		t.Fatalf("WorkIntros: found=%v err=%v", found, err)
	}
	if !reflect.DeepEqual(sub.Items, want) {
		t.Fatalf("intros sub-resource = %+v, want %+v", sub.Items, want)
	}
}

func TestNSFWShelfIntrosAreOriginalOnlyWithNSFW(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvcCDN()
	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "NSFW shelf")
	yes := true
	setTitleNSFW(t, w.ID, &yes)
	seedJudgedIntros(t, w.ID, "n")

	all := []dto.PublicIntro{
		{Lang: "en", Intro: "nAn explicit original", Source: "vndb"},
		{Lang: "ja", Intro: "n露骨な原文", Source: "bangumi"},
		{Lang: "ko", Intro: "n아직 판정 전", Source: "vndb"},
		{Lang: "zh-Hans", Intro: "n含有成人句子的原文", Source: "vndb"},
	}
	for nsfw, want := range map[bool][]dto.PublicIntro{true: all, false: sfwIntros("n")} {
		rec, found, err := svc.WorkDetail(t.Context(), w.ID, PublicInclude{}, nsfw, 0, PublicFields{})
		if err != nil || !found {
			t.Fatalf("WorkDetail nsfw=%v: found=%v err=%v", nsfw, found, err)
		}
		if rec.ContentLimit != model.DisplayLimitKeyNSFW {
			t.Fatalf("content_limit = %q, want nsfw (title judged nsfw)", rec.ContentLimit)
		}
		if !reflect.DeepEqual(rec.Intros, want) {
			t.Fatalf("detail intros nsfw=%v = %+v, want %+v", nsfw, rec.Intros, want)
		}
		if got := introsByWork(t, svc, []int64{w.ID}, nsfw)[w.ID]; !reflect.DeepEqual(got, want) {
			t.Fatalf("list intros nsfw=%v = %+v, want %+v", nsfw, got, want)
		}
		sub, _, err := svc.WorkIntros(t.Context(), w.ID, nsfw, 50, 0)
		if err != nil {
			t.Fatalf("WorkIntros nsfw=%v: %v", nsfw, err)
		}
		if !reflect.DeepEqual(sub.Items, want) {
			t.Fatalf("intros sub-resource nsfw=%v = %+v, want %+v", nsfw, sub.Items, want)
		}
	}
}

func TestUnjudgedTitleKeepsTheAgeRule(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvcCDN()
	r18 := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "not judged yet")
	rec, found, err := svc.WorkDetail(t.Context(), r18.ID, PublicInclude{}, true, 0, PublicFields{})
	if err != nil || !found {
		t.Fatalf("WorkDetail: found=%v err=%v", found, err)
	}
	if rec.ContentLimit != model.DisplayLimitKeyNSFW {
		t.Fatalf("content_limit = %q, want nsfw: a NULL title_nsfw must fall back to the age rating", rec.ContentLimit)
	}
}

func markCharacterIntroSafety(t *testing.T, intro string, mode int16, sfwIntro string) {
	t.Helper()
	if err := testDB.Exec(`INSERT INTO catalog_character_intro_safety
		(intro_hash, verdict, confidence, sfw_mode, sfw_intro, model, prompt_version, updated_at)
		VALUES (md5(?), 3, 0.99, ?, ?, 'test', 'test', now())`, intro, mode, sfwIntro).Error; err != nil {
		t.Fatalf("mark character intro safety: %v", err)
	}
}

func addCharacterIntro(t *testing.T, characterID int64, lang, intro string) {
	t.Helper()
	if err := testDB.Create(&model.CatalogCharacterIntro{
		CharacterID: characterID, Lang: lang, Intro: intro, SourceID: srcVNDB, Provenance: model.IntroProvenanceSource,
	}).Error; err != nil {
		t.Fatalf("create character intro: %v", err)
	}
}

// A character shown in any sfw work has an sfw page; only a character whose
// every live work is nsfw gets its original profile, and only with nsfw=true.
func TestCharacterIntrosFollowTheirWorksShelves(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvcCDN()
	no, yes := false, true
	sfwWork := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "sfw work")
	setTitleNSFW(t, sfwWork.ID, &no)
	nsfwWork := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "nsfw work")
	setTitleNSFW(t, nsfwWork.ID, &yes)

	shared := createCharacter(t, "both shelves")
	createWorkCharacter(t, sfwWork.ID, shared.ID, 0, 0)
	createWorkCharacter(t, nsfwWork.ID, shared.ID, 0, 0)
	adultOnly := createCharacter(t, "nsfw only")
	createWorkCharacter(t, nsfwWork.ID, adultOnly.ID, 0, 0)

	for _, c := range []*model.CatalogCharacter{shared, adultOnly} {
		addCharacterIntro(t, c.ID, "ja", c.DisplayName+"の露骨なプロフィール")
		markCharacterIntroSafety(t, c.DisplayName+"の露骨なプロフィール", model.IntroSFWRedacted, c.DisplayName+"の安全なプロフィール")
		addCharacterIntro(t, c.ID, "en", c.DisplayName+" not judged yet")
	}

	for _, tc := range []struct {
		c     *model.CatalogCharacter
		nsfw  bool
		wants []dto.PublicIntro
	}{
		{shared, true, []dto.PublicIntro{{Lang: "ja", Intro: "both shelvesの安全なプロフィール", Source: "vndb"}}},
		{shared, false, []dto.PublicIntro{{Lang: "ja", Intro: "both shelvesの安全なプロフィール", Source: "vndb"}}},
		{adultOnly, false, []dto.PublicIntro{{Lang: "ja", Intro: "nsfw onlyの安全なプロフィール", Source: "vndb"}}},
		{adultOnly, true, []dto.PublicIntro{
			{Lang: "ja", Intro: "nsfw onlyの露骨なプロフィール", Source: "vndb"},
			{Lang: "en", Intro: "nsfw only not judged yet", Source: "vndb"},
		}},
	} {
		got, err := svc.characterIntros(t.Context(), tc.c.ID, tc.nsfw)
		if err != nil {
			t.Fatalf("characterIntros(%s, nsfw=%v): %v", tc.c.DisplayName, tc.nsfw, err)
		}
		if !introSetEqual(got, tc.wants) {
			t.Fatalf("characterIntros(%s, nsfw=%v) = %+v, want %+v", tc.c.DisplayName, tc.nsfw, got, tc.wants)
		}
		pub, found, err := svc.Character(t.Context(), tc.c.ID, false, tc.nsfw, model.SpoilerSevere, 10, 0)
		if err != nil || !found {
			t.Fatalf("Character(%s): found=%v err=%v", tc.c.DisplayName, found, err)
		}
		if !introSetEqual(pub.Intros, tc.wants) {
			t.Fatalf("Character(%s, nsfw=%v).Intros = %+v, want %+v", tc.c.DisplayName, tc.nsfw, pub.Intros, tc.wants)
		}
	}
}

func introSetEqual(a, b []dto.PublicIntro) bool {
	if len(a) != len(b) {
		return false
	}
	for _, x := range b {
		found := false
		for _, y := range a {
			if reflect.DeepEqual(x, y) {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}
