package intromt

import (
	"context"
	"testing"
	"time"

	"api/internal/platform/authz"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/perm"
	"api/internal/platform/editing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const metaReply = "您好，我已理解您的翻译要求。但是您尚未提供需要翻译的英文作品简介正文。请您把需要翻译的英文简介原文贴出来。"

func machineZh(t *testing.T, workID int64) (string, bool) {
	t.Helper()
	var rows []string
	require.NoError(t, testDB.Raw(
		`SELECT intro FROM catalog_work_intro WHERE work_id = ? AND lang = 'zh-Hans' AND provenance = 1`,
		workID).Scan(&rows).Error)
	if len(rows) == 0 {
		return "", false
	}
	return rows[0], true
}

func mkMachine(t *testing.T, workID int64, source int16, intro, srcHash string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_work_intro
		(work_id, lang, intro, source_id, provenance, src_hash, mt_model, created_at, updated_at)
		VALUES (?, 'zh-Hans', ?, ?, 1, ?, 'mt', now(), now())`, workID, intro, source, srcHash).Error)
}

func TestAMetaReplyIsRefusedRememberedAndNotAskedAgain(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium, _, _ := reg(t)
	curated := curatedSource(t)
	claimed := "kungal"
	w := mkWork(t, medium, "王様恋愛 ミニファンディスク", &claimed)
	mkIntro(t, w, "en", "A short fandisc with four after stories.", curated)

	meta := &fakeTranslator{model: "glm-5.2", fn: func(string) string { return metaReply }}
	st, err := Run(ctx, meta, Opts{DSN: testDSN, Apply: true, Population: PopulationClaimed, SourceLang: SourceEn})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Rejected)
	assert.Zero(t, st.Inserted)
	assert.Zero(t, st.Errors, "a refusal is not a lane failure")
	_, ok := machineZh(t, w)
	assert.False(t, ok, "the model's reply to the requester never reaches the reader")

	st, err = Run(ctx, meta, Opts{DSN: testDSN, Apply: true, Population: PopulationClaimed, SourceLang: SourceEn})
	require.NoError(t, err)
	assert.Equal(t, 1, st.SkipRejected)
	assert.Equal(t, 1, meta.calls, "the refused source is not sent again")

	require.NoError(t, testDB.Exec(`UPDATE catalog_work_intro SET intro = ? WHERE work_id = ? AND lang = 'en'`,
		"A fandisc: four after stories, one for each pair of heroines.", w).Error)
	good := &fakeTranslator{model: "mt", fn: func(string) string { return "一部后日谈FD，每对女主角各一篇。" }}
	st, err = Run(ctx, good, Opts{DSN: testDSN, Apply: true, Population: PopulationClaimed, SourceLang: SourceEn})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Inserted, "a changed source is a new question")
}

func TestAJunkMachineRowIsTranslatedAgain(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium, _, bangumi := reg(t)
	w := mkWork(t, medium, "heal", nil)
	ja := "幼なじみと過ごす最後の夏休み。"
	mkIntro(t, w, "ja", ja, bangumi)
	mkMachine(t, w, bangumi, "You are an expert translator specializing in game localization.", hashSource(ja))

	tr := &fakeTranslator{model: "mt", fn: func(string) string { return "与青梅竹马度过的最后一个暑假。" }}
	st, err := Run(ctx, tr, Opts{DSN: testDSN, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 1, st.WouldHeal)
	assert.Equal(t, 1, st.Retranslated)
	zh, _ := machineZh(t, w)
	assert.Equal(t, "与青梅竹马度过的最后一个暑假。", zh)
}

func TestAJunkRowThatStaysJunkIsCleared(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium, _, _ := reg(t)
	curated := curatedSource(t)
	w := mkWork(t, medium, "copies", nil)
	en := "Eight incredible mothers at the Milf Mansion, from morning to evening."
	mkIntro(t, w, "en", en, curated)
	mkMachine(t, w, curated, en, hashSource(en))

	copier := &fakeTranslator{model: "mt", fn: func(s string) string { return s }}
	st, err := Run(ctx, copier, Opts{DSN: testDSN, Apply: true, SourceLang: SourceEn})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Rejected)
	assert.Equal(t, 1, st.Cleared)
	_, ok := machineZh(t, w)
	assert.False(t, ok, "an English copy is not a Chinese translation")
}

func TestAJunkJaIntroFallsBackToTheEnglishOne(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium, _, _ := reg(t)
	curated := curatedSource(t)
	w := mkWork(t, medium, "783", nil)
	mkIntro(t, w, "ja", ">\n\n", curated)
	mkIntro(t, w, "en", "During Masashi's last summer as a student, it was decided that he would move.", curated)
	mkMachine(t, w, curated, "“想更加了解我吗？”幼年时失去双亲的主人公……", "stale")

	tr := &fakeTranslator{model: "mt", fn: func(string) string { return "在正志学生时代的最后一个夏天，他决定搬家。" }}
	st, err := Run(ctx, tr, Opts{DSN: testDSN, Apply: true, SourceLang: SourceJa})
	require.NoError(t, err)
	assert.Zero(t, st.Candidates, "the ja lane leaves a work whose only translatable text is English")
	assert.Zero(t, tr.calls)

	st, err = Run(ctx, tr, Opts{DSN: testDSN, Apply: true, SourceLang: SourceEn})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Retranslated)
	zh, _ := machineZh(t, w)
	assert.Equal(t, "在正志学生时代的最后一个夏天，他决定搬家。", zh)
}

func TestAWorkWithNothingToTranslateLosesItsMachineRow(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium, _, _ := reg(t)
	curated := curatedSource(t)
	w := mkWork(t, medium, "voting", nil)
	mkIntro(t, w, "en", "Nobody has voted on this visual novel yet...", curated)
	mkMachine(t, w, curated, metaReply, "whatever")

	tr := &fakeTranslator{model: "mt", fn: func(string) string { return "不该被调用" }}
	st, err := Run(ctx, tr, Opts{DSN: testDSN, Apply: false, SourceLang: SourceEn})
	require.NoError(t, err)
	assert.Equal(t, 1, st.WouldClear)
	_, ok := machineZh(t, w)
	assert.True(t, ok, "a dry run writes nothing")

	st, err = Run(ctx, tr, Opts{DSN: testDSN, Apply: true, SourceLang: SourceEn})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Cleared)
	assert.Zero(t, tr.calls, "an untranslatable source costs no model call")
	_, ok = machineZh(t, w)
	assert.False(t, ok)

	st, err = Run(ctx, tr, Opts{DSN: testDSN, Apply: true, SourceLang: SourceJa})
	require.NoError(t, err)
	assert.Zero(t, st.Candidates, "an en-only work is cleared by the en lane alone")
}

func TestChangedSinceTakesOnlyRecentlyWrittenWorks(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium, _, bangumi := reg(t)
	old := mkWork(t, medium, "old", nil)
	mkIntro(t, old, "ja", "昔からあるあらすじ。", bangumi)
	require.NoError(t, testDB.Exec(`UPDATE catalog_work_intro SET updated_at = ? WHERE work_id = ?`,
		time.Now().Add(-48*time.Hour), old).Error)
	fresh := mkWork(t, medium, "fresh", nil)
	mkIntro(t, fresh, "ja", "さっき書いたあらすじ。", bangumi)

	tr := &fakeTranslator{model: "mt", fn: func(string) string { return "刚写的简介。" }}
	st, err := Run(ctx, tr, Opts{DSN: testDSN, Apply: true, ChangedSince: 2 * time.Hour})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Candidates)
	assert.Equal(t, 1, st.Inserted)
	_, ok := machineZh(t, old)
	assert.False(t, ok)

	require.NoError(t, testDB.Exec(`UPDATE catalog_work_intro SET updated_at = ?`, time.Now().Add(-48*time.Hour)).Error)
	st, err = Run(ctx, tr, Opts{DSN: testDSN, Apply: true, ChangedSince: 2 * time.Hour})
	require.NoError(t, err)
	assert.Zero(t, st.Candidates, "an empty window is no work, never the whole population")
}

func TestTheEditorRefusesACopiedVotePromptAndAJaEditClearsTheStaleMachineRow(t *testing.T) {
	clean(t)
	require.NoError(t, testDB.Exec(
		"TRUNCATE edit_proposal_amendment, edit_proposal, edit_revision RESTART IDENTITY CASCADE").Error)
	ctx := context.Background()
	medium, _, _ := reg(t)
	curated := curatedSource(t)

	registry := editing.NewRegistry()
	require.NoError(t, editspec.RegisterWork(registry, testDB))
	e := editing.NewEngine(testDB, registry)
	save := func(w int64, intros ...[2]string) error {
		value := make([]any, 0, len(intros))
		for _, in := range intros {
			value = append(value, map[string]any{"lang": in[0], "intro": in[1]})
		}
		_, _, err := e.CreateProposal(ctx, editing.CreateProposalInput{
			EntityType: editspec.TypeWork, EntityID: w,
			Patch: map[string]any{editspec.FieldWorkIntros: value},
			Actor: editing.PolicyContext{
				UserID: 100, Site: "kungal",
				HasPerm: func(key string) bool { return perm.Resolver.Can([]string{"ren"}, authz.Permission(key)) },
			},
		})
		return err
	}

	w := mkWork(t, medium, "279247", nil)
	err := save(w, [2]string{"en", "Nobody has voted on this visual novel yet..."})
	var verr *editing.ValidationError
	require.ErrorAs(t, err, &verr, "a copied VNDB voting prompt is not an intro")

	require.NoError(t, save(w, [2]string{"ja", "ふたりのメイドと無事ラブラブになった肇。"}))
	mkMachine(t, w, curated, "与两位女仆顺利相爱的肇。", "h1")

	require.NoError(t, save(w,
		[2]string{"ja", "ふたりのメイドと無事ラブラブになった肇。"},
		[2]string{"zh-Hant", "與兩位女僕順利相愛的肇。"}))
	_, ok := machineZh(t, w)
	assert.True(t, ok, "a save that keeps the ja text keeps its translation")

	require.NoError(t, save(w, [2]string{"ja", "01 むぎ＆緋華\nご主人様とイチャつきたいメイドたち"}))
	_, ok = machineZh(t, w)
	assert.False(t, ok, "the translation of the replaced ja text goes with it")
}
