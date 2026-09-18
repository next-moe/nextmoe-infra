package intromt

import (
	"context"
	"testing"

	"api/internal/platform/authz"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/perm"
	"api/internal/platform/editing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkIntrosSurviveTheMachineTranslator(t *testing.T) {
	clean(t)
	require.NoError(t, testDB.Exec(
		"TRUNCATE edit_proposal_amendment, edit_proposal, edit_revision RESTART IDENTITY CASCADE").Error)
	ctx := context.Background()
	medium, _, _ := reg(t)
	var curated int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = 'curated'`).Scan(&curated).Error)
	require.NotZero(t, curated)

	registry := editing.NewRegistry()
	require.NoError(t, editspec.RegisterWork(registry, testDB))
	e := editing.NewEngine(testDB, registry)
	actor := func(uid int64, role string) editing.PolicyContext {
		return editing.PolicyContext{
			UserID: uid, Site: "kungal",
			HasPerm: func(key string) bool { return perm.Resolver.Can([]string{role}, authz.Permission(key)) },
		}
	}

	w := mkWork(t, medium, "src-zh-guard", nil)
	_, rev, err := e.CreateProposal(ctx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: w,
		Patch: map[string]any{editspec.FieldWorkIntros: []any{
			map[string]any{"lang": "ja", "intro": "あらすじです。"},
			map[string]any{"lang": "zh-Hans", "intro": "人工写的中文简介。"},
		}},
		Actor: actor(100, "ren"),
	})
	require.NoError(t, err)
	require.NotNil(t, rev, "the kungal overlay automerges a reviewer's own proposal")

	r := &runner{db: testDB, tr: nil, stats: &Stats{}}
	rows, _, err := r.writeMachine(ctx, candidate{WorkID: w, JaSourceID: curated},
		"机翻不该落地", hashSource("あらすじです。"), "test-mt")
	require.NoError(t, err)
	assert.Zero(t, rows, "DO UPDATE WHERE provenance=1 refuses an engine-written row")

	var row model.CatalogWorkIntro
	require.NoError(t, testDB.Where("work_id = ? AND lang = 'zh-Hans'", w).First(&row).Error)
	assert.Equal(t, "人工写的中文简介。", row.Intro)
	assert.EqualValues(t, model.IntroProvenanceSource, row.Provenance)
	assert.EqualValues(t, curated, row.SourceID)
	assert.Empty(t, row.MTModel)

	require.NoError(t, testDB.Create(&model.CatalogWorkIntro{
		WorkID: w, Lang: "zh-Hant", Intro: "機器譯文。", SourceID: curated,
		Provenance: model.IntroProvenanceMachine, SrcHash: "deadbeef", MTModel: "test-mt",
	}).Error)
	snap, err := e.CurrentSnapshot(ctx, editspec.TypeWork, w)
	require.NoError(t, err)
	langs := make([]string, 0, 2)
	for _, el := range snap[editspec.FieldWorkIntros].([]any) {
		langs = append(langs, el.(map[string]any)["lang"].(string))
	}
	assert.Equal(t, []string{"ja", "zh-Hans"}, langs,
		"a machine row on the curated lane must not come back as a human intro")
}
