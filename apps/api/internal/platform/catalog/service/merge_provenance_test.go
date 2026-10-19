package service

import (
	"testing"

	"api/internal/platform/authz"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/perm"
	"api/internal/platform/editing"
	"api/internal/platform/provenance"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func provEngine(t *testing.T) *editing.Engine {
	t.Helper()
	require.NoError(t, testDB.Exec(
		"TRUNCATE edit_proposal_amendment, edit_proposal, edit_revision RESTART IDENTITY CASCADE").Error)
	reg := editing.NewRegistry()
	require.NoError(t, editspec.RegisterWork(reg, testDB))
	require.NoError(t, editspec.RegisterTaxonomy(reg, testDB))
	return editing.NewEngine(testDB, reg)
}

func provActor(uid int64, roles ...string) editing.PolicyContext {
	return editing.PolicyContext{
		UserID: uid, Site: "nextmoe",
		HasPerm: func(key string) bool { return perm.Resolver.Can(roles, authz.Permission(key)) },
	}
}

func humanEdit(t *testing.T, e *editing.Engine, entityType string, entityID int64, patch map[string]any) {
	t.Helper()
	ctx := t.Context()
	prop, _, err := e.CreateProposal(ctx, editing.CreateProposalInput{
		EntityType: entityType, EntityID: entityID, Patch: patch, Actor: provActor(100),
	})
	require.NoError(t, err)
	_, err = e.MergeProposal(ctx, prop.ID, provActor(200, "ren"), "")
	require.NoError(t, err)
}

func resolveToSource(t *testing.T, proposalID int64, field string) {
	t.Helper()
	require.NoError(t, testDB.Exec(
		`UPDATE catalog_merge_proposal SET field_resolution = ?::jsonb WHERE id = ?`,
		`{"`+field+`":"source"}`, proposalID).Error)
}

func TestMergeKeepsEngineEditedWorkName(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	e := provEngine(t)

	target := createWork(t, "上流の暫定名")
	source := createWork(t, "上流の別名")
	require.NoError(t, testDB.Exec(
		`UPDATE catalog_work SET field_provenance =
		 '{"display_name":[{"source":"vndb","at":"2026-01-01T00:00:00Z"}]}' WHERE id = ?`,
		source.ID).Error)

	humanEdit(t, e, editspec.TypeWork, target.ID,
		map[string]any{editspec.FieldWorkDisplayName: "人が決めた正式名"})

	p, err := testMerge.ProposeMerge(ctx, model.EntityTypeWork, source.ID, target.ID, 7, "dup")
	require.NoError(t, err)
	resolveToSource(t, p.ID, "display_name")
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

	var merged model.CatalogWork
	require.NoError(t, testDB.First(&merged, target.ID).Error)
	assert.Equal(t, "人が決めた正式名", merged.DisplayName,
		"an engine edit must survive a merge that resolves the field to the source")
	assert.Equal(t, provenance.SourceCurated, firstProvSource(merged.FieldProvenance, "display_name"))
}

func TestMergeKeepsEngineEditedLabelName(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	e := provEngine(t)

	target := createLabel(t, "上流ブランド名", model.LabelKindGameBrand)
	source := createLabel(t, "上流の別ブランド名", model.LabelKindGameBrand)
	require.NoError(t, testDB.Exec(
		`UPDATE catalog_label SET field_provenance =
		 '{"display_name":[{"source":"vndb","at":"2026-01-01T00:00:00Z"}]}' WHERE id = ?`,
		source).Error)

	humanEdit(t, e, editspec.TypeLabel, target, map[string]any{editspec.FieldLabelName: "人が決めたブランド名"})

	p, err := testMerge.ProposeMerge(ctx, model.EntityTypeLabel, source, target, 7, "same brand")
	require.NoError(t, err)
	resolveToSource(t, p.ID, "display_name")
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

	var merged model.CatalogLabel
	require.NoError(t, testDB.First(&merged, target).Error)
	assert.Equal(t, "人が決めたブランド名", merged.DisplayName,
		"an engine edit must survive a merge that resolves the field to the source")
	assert.Equal(t, provenance.SourceCurated, firstProvSource(merged.FieldProvenance, "display_name"))
}

func TestMergeStillTakesMachineValueOverMachineValue(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()

	target := createWork(t, "bangumi name")
	source := createWork(t, "vndb name")
	require.NoError(t, testDB.Exec(
		`UPDATE catalog_work SET field_provenance =
		 '{"display_name":[{"source":"bangumi","at":"2026-01-01T00:00:00Z"}]}' WHERE id = ?`,
		target.ID).Error)
	require.NoError(t, testDB.Exec(
		`UPDATE catalog_work SET field_provenance =
		 '{"display_name":[{"source":"vndb","at":"2026-02-01T00:00:00Z"}]}' WHERE id = ?`,
		source.ID).Error)

	p, err := testMerge.ProposeMerge(ctx, model.EntityTypeWork, source.ID, target.ID, 7, "dup")
	require.NoError(t, err)
	resolveToSource(t, p.ID, "display_name")
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

	var merged model.CatalogWork
	require.NoError(t, testDB.First(&merged, target.ID).Error)
	assert.Equal(t, "vndb name", merged.DisplayName,
		"widening the predicate must not start protecting machine-owned fields")
	assert.Equal(t, "vndb", firstProvSource(merged.FieldProvenance, "display_name"))
}
