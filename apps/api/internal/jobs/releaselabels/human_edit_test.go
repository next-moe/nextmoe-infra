package releaselabels

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

func visibleOn(t *testing.T, release int64) map[[2]int64]bool {
	t.Helper()
	var rows []model.CatalogReleaseLabel
	require.NoError(t, testDB.Raw(`SELECT rl.release_id, rl.label_id, rl.kind, rl.source_id FROM catalog_release_label rl
		WHERE rl.release_id = ? AND `+editspec.NotSuppressedReleaseLabelSQL("rl"), release).Scan(&rows).Error)
	out := map[[2]int64]bool{}
	for _, r := range rows {
		out[[2]int64{r.LabelID, int64(r.Kind)}] = true
	}
	return out
}

func TestHumanReleaseLabelEditsSurviveARerun(t *testing.T) {
	if testDB == nil {
		t.Skip("no test db")
	}
	ja, _, maker, localiser := seedEditions(t)
	require.NoError(t, testDB.Exec(
		"TRUNCATE edit_suppressed_row, edit_proposal_amendment, edit_proposal, edit_revision RESTART IDENTITY CASCADE").Error)
	_, err := Run(context.Background(), Opts{Apply: true, DSN: testDSN})
	require.NoError(t, err)

	reg := editing.NewRegistry()
	require.NoError(t, editspec.RegisterRelease(reg, testDB))
	e := editing.NewEngine(testDB, reg)
	ren := editing.PolicyContext{
		UserID: 100, Site: "editor",
		HasPerm: func(key string) bool { return perm.Resolver.Can([]string{"ren"}, authz.Permission(key)) },
	}
	_, rev, err := e.CreateProposal(context.Background(), editing.CreateProposalInput{
		EntityType: editspec.TypeRelease, EntityID: ja, Actor: ren,
		Patch: map[string]any{
			editspec.FieldReleaseLabelsSuppr: []any{editspec.ReleaseLabelIdentity(maker, model.WorkLabelKindDeveloper)},
			editspec.FieldReleaseLabels: []any{
				map[string]any{"label_id": float64(localiser), "kind": float64(model.WorkLabelKindDeveloper)},
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, rev, "a ren edit merges at once")

	want := map[[2]int64]bool{
		{maker, int64(model.WorkLabelKindPublisher)}:     true,
		{localiser, int64(model.WorkLabelKindDeveloper)}: true,
	}
	require.Equal(t, want, visibleOn(t, ja))

	st, err := Run(context.Background(), Opts{Apply: true, DSN: testDSN})
	require.NoError(t, err)
	assert.Zero(t, st.Written, "the importer re-inserts nothing: the hidden edge was never deleted")
	assert.Equal(t, want, visibleOn(t, ja), "the hidden developer stays hidden and the human one stays")
}
