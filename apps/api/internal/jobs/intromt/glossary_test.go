package intromt

import (
	"context"
	"strings"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGlossaryCanonical(t *testing.T) {
	assert.Equal(t, "", Glossary(nil).Canonical(), "an empty glossary serializes to nothing")

	g := Glossary{{Src: "星空のメモリア", Zh: "星空的记忆"}, {Src: "ういんどみる", Zh: "风车"}}
	assert.Equal(t, "星空のメモリア\t星空的记忆\nういんどみる\t风车", g.Canonical())

	rev := Glossary{g[1], g[0]}
	assert.NotEqual(t, g.Canonical(), rev.Canonical())
}

func TestGlossaryPromptSection(t *testing.T) {
	assert.Equal(t, "", Glossary(nil).PromptSection(), "no terms → no section")
	assert.Equal(t, TranslateSystemPrompt, withGlossary(TranslateSystemPrompt, nil),
		"an empty glossary leaves the pinned prompt byte-identical")

	g := Glossary{{Src: "星空のメモリア", Zh: "星空的记忆"}, {Src: "水橋 かおり", Zh: "水桥香织"}}
	sec := g.PromptSection()
	assert.Equal(t,
		GlossaryHeader+"\n星空のメモリア → 星空的记忆\n水橋 かおり → 水桥香织\n"+GlossaryRule,
		sec)

	full := withGlossary(TranslateSystemPrompt, g)
	assert.True(t, strings.HasPrefix(full, TranslateSystemPrompt), "the base prompt is untouched")
	assert.Contains(t, full, GlossaryRule)
	assert.Contains(t, withGlossary(TranslateSystemPromptEn, g), GlossaryRule,
		"the English lane gets the same section")
}

func TestHashCandidateBackwardCompatible(t *testing.T) {
	const text = "これはあらすじです。"
	assert.Equal(t, hashSource(text), hashCandidate(text, nil),
		"empty glossary MUST hash exactly like the pre-glossary job")
	assert.Equal(t, hashSource(text), hashCandidate(text, Glossary{}),
		"a non-nil but empty glossary is the same case")

	g := Glossary{{Src: "星空のメモリア", Zh: "星空的记忆"}}
	assert.NotEqual(t, hashSource(text), hashCandidate(text, g),
		"a glossary changes the prompt, so it must change the hash exactly once")
	assert.Equal(t, hashSource(text+"\x00"+g.Canonical()), hashCandidate(text, g))

	g2 := Glossary{{Src: "星空のメモリア", Zh: "星空之记忆"}}
	assert.NotEqual(t, hashCandidate(text, g), hashCandidate(text, g2),
		"a changed rendering re-translates")
}

func TestGlossaryBuilderCapAndDedup(t *testing.T) {
	var b glossaryBuilder
	b.add("A", "甲")
	b.add("A", "乙")
	b.add(" B ", " 乙 ")
	b.add("", "丙")
	b.add("C", "")
	b.add("D", "D")
	assert.Equal(t, Glossary{{Src: "A", Zh: "甲"}, {Src: "B", Zh: "乙"}}, b.out)

	var capped glossaryBuilder
	for i := range maxGlossaryEntries + 10 {
		capped.add(strings.Repeat("x", i+1), "译")
	}
	assert.Len(t, capped.out, maxGlossaryEntries, "the cap holds")
	assert.Equal(t, "x", capped.out[0].Src, "the cap keeps the HIGHEST-priority entries")
}

func cleanEntities(t *testing.T) {
	t.Helper()
	for _, table := range []string{"catalog_character", "catalog_label"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" CASCADE").Error)
	}
}

func mkTitle(t *testing.T, workID int64, lang, title string, kind int16) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogWorkTitle{
		WorkID: workID, Lang: lang, Title: title, Kind: kind,
	}).Error)
}

func mkRosterCharacter(t *testing.T, workID int64, name, zh string) int64 {
	t.Helper()
	c := model.CatalogCharacter{DisplayName: name, Lang: "ja"}
	require.NoError(t, testDB.Create(&c).Error)
	if zh != "" {
		require.NoError(t, testDB.Create(&model.CatalogCharacterAlias{
			CharacterID: c.ID, Name: zh, Lang: "zh-Hans",
			Kind: model.AliasKindTranslation, IsPrimaryForLocale: true,
		}).Error)
	}
	require.NoError(t, testDB.Create(&model.CatalogWorkCharacter{
		WorkID: workID, CharacterID: c.ID, Kind: 0, Spoiler: 0, MatchedBy: "import:test",
	}).Error)
	return c.ID
}

func mkWorkLabel(t *testing.T, workID int64, name, zh string) int64 {
	t.Helper()
	l := model.CatalogLabel{DisplayName: name, Lang: "ja", Kind: 0}
	require.NoError(t, testDB.Create(&l).Error)
	if zh != "" {
		require.NoError(t, testDB.Create(&model.CatalogLabelAlias{
			LabelID: l.ID, Name: zh, Lang: "zh-Hans",
			Kind: model.AliasKindTranslation, IsPrimaryForLocale: true,
		}).Error)
	}
	require.NoError(t, testDB.Create(&model.CatalogWorkLabel{
		WorkID: workID, LabelID: l.ID, Kind: 0,
	}).Error)
	return l.ID
}

func TestLoadGlossaries(t *testing.T) {
	clean(t)
	cleanEntities(t)
	ctx := context.Background()
	medium, _, _ := reg(t)

	w := mkWork(t, medium, "glossary-work", nil)
	mkTitle(t, w, "ja", "星空のメモリア", model.WorkTitleKindOfficial)
	mkTitle(t, w, "zh-Hans", "星空的记忆", model.WorkTitleKindOfficial)
	mkTitle(t, w, "ja", "hoshimemo", model.WorkTitleKindSearchHint)
	mkTitle(t, w, "zh-Hans", "星空记忆搜索用", model.WorkTitleKindSearchHint)

	mkRosterCharacter(t, w, "水橋 かおり", "水桥香织")
	mkRosterCharacter(t, w, "橘 ひなた", "橘向日葵")
	mkRosterCharacter(t, w, "名無し", "")
	mkWorkLabel(t, w, "ういんどみる", "风车")

	bare := mkWork(t, medium, "no-glossary-work", nil)
	mkTitle(t, bare, "ja", "何もない作品", model.WorkTitleKindOfficial)

	gs, err := loadGlossaries(ctx, testDB, []int64{w, bare}, SourceJa)
	require.NoError(t, err)

	assert.Equal(t, Glossary{
		{Src: "星空のメモリア", Zh: "星空的记忆"},
		{Src: "水橋 かおり", Zh: "水桥香织"},
		{Src: "橘 ひなた", Zh: "橘向日葵"},
		{Src: "ういんどみる", Zh: "风车"},
	}, gs[w], "own title, then roster characters by id, then labels")
	assert.NotContains(t, gs, bare, "a work with no Chinese anywhere gets no glossary")

	again, err := loadGlossaries(ctx, testDB, []int64{w, bare}, SourceJa)
	require.NoError(t, err)
	assert.Equal(t, gs[w].Canonical(), again[w].Canonical())
}

func mkCharAlias(t *testing.T, cid int64, name, lang string, kind int16) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogCharacterAlias{
		CharacterID: cid, Name: name, Lang: lang, Kind: kind,
	}).Error)
}

func TestLoadGlossariesEnLatinKeys(t *testing.T) {
	clean(t)
	cleanEntities(t)
	ctx := context.Background()
	medium, _, _ := reg(t)

	w := mkWork(t, medium, "latin-work", nil)
	cid := mkRosterCharacter(t, w, "麻布 真澄", "麻布 真澄")
	mkCharAlias(t, cid, "Azabu Masumi", "ja", model.AliasKindSpellingVariant)

	ja, err := loadGlossaries(ctx, testDB, []int64{w}, SourceJa)
	require.NoError(t, err)
	assert.NotContains(t, ja, w,
		"ja lane must not gain latin keys: the identity kanji pair stays dropped, and ja hashes must not drift")

	en, err := loadGlossaries(ctx, testDB, []int64{w}, SourceEn)
	require.NoError(t, err)
	assert.Equal(t, Glossary{{Src: "Azabu Masumi", Zh: "麻布 真澄"}}, en[w],
		"en lane anchors the romaji spelling to the zh name")
}

func TestGlossaryReachesPromptAndHash(t *testing.T) {
	clean(t)
	cleanEntities(t)
	ctx := context.Background()
	medium, _, bangumi := reg(t)

	const jaText = "星空のメモリアのあらすじ。"
	const bareText = "何もない作品のあらすじ。"

	w := mkWork(t, medium, "glossary-hash", nil)
	mkIntro(t, w, "ja", jaText, bangumi)
	mkTitle(t, w, "ja", "星空のメモリア", model.WorkTitleKindOfficial)
	mkTitle(t, w, "zh-Hans", "星空的记忆", model.WorkTitleKindOfficial)

	bare := mkWork(t, medium, "bare-hash", nil)
	mkIntro(t, bare, "ja", bareText, bangumi)

	for _, seed := range []struct {
		id   int64
		text string
	}{{w, jaText}, {bare, bareText}} {
		require.NoError(t, testDB.Create(&model.CatalogWorkIntro{
			WorkID: seed.id, Lang: "zh-Hans", Intro: "旧的机器译文", SourceID: bangumi,
			Provenance: 1, SrcHash: hashSource(seed.text), MTModel: "old-mt",
		}).Error)
	}

	tr := &fakeTranslator{model: "gloss-mt", fn: func(ja string) string { return "[译] " + ja }}
	st, err := Run(ctx, tr, Opts{DSN: testDSN, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 2, st.Candidates)
	assert.Equal(t, 1, st.WithGlossary, "only the titled work has terms")
	assert.Equal(t, 1, st.WouldRetranslate, "the glossary changed the prompt → re-translate once")
	assert.Equal(t, 1, st.SkipUnchanged, "the glossary-less work keeps the plain hash → untouched")
	assert.Equal(t, 1, tr.calls)
	assert.Equal(t, Glossary{{Src: "星空のメモリア", Zh: "星空的记忆"}}, tr.gloss,
		"the candidate's term list reached the LLM seam")

	var row model.CatalogWorkIntro
	require.NoError(t, testDB.Where("work_id=? AND lang='zh-Hans'", w).First(&row).Error)
	assert.Equal(t, hashCandidate(jaText, tr.gloss), row.SrcHash)
	assert.NotEqual(t, hashSource(jaText), row.SrcHash)

	var untouched model.CatalogWorkIntro
	require.NoError(t, testDB.Where("work_id=? AND lang='zh-Hans'", bare).First(&untouched).Error)
	assert.Equal(t, "旧的机器译文", untouched.Intro, "no glossary → no re-translation")

	tr2 := &fakeTranslator{model: "gloss-mt", fn: func(string) string { return "SHOULD-NOT-BE-CALLED" }}
	st, err = Run(ctx, tr2, Opts{DSN: testDSN, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 2, st.SkipUnchanged)
	assert.Zero(t, tr2.calls)

	mkRosterCharacter(t, w, "水橋 かおり", "水桥香织")
	tr3 := &fakeTranslator{model: "gloss-mt-v2", fn: func(ja string) string { return "[新译] " + ja }}
	st, err = Run(ctx, tr3, Opts{DSN: testDSN, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Retranslated)
	assert.Equal(t, 1, st.SkipUnchanged)
	assert.Len(t, tr3.gloss, 2, "the new alias joined the term list")
}

func TestMockTranslatorShowsGlossary(t *testing.T) {
	m := MockTranslator{Model: "stub"}
	none, _, err := m.Translate(context.Background(), "原文", nil)
	require.NoError(t, err)
	assert.Contains(t, none, "[gloss:0]")

	some, _, err := m.Translate(context.Background(), "原文", Glossary{{Src: "A", Zh: "甲"}})
	require.NoError(t, err)
	assert.Contains(t, some, "[gloss:1]")
	assert.NotEqual(t, none, some, "glossary presence is visible in the mock output")
}
