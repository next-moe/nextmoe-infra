package importer

import (
	"context"
	"encoding/json"
	"testing"

	"api/internal/platform/authz"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/perm"
	"api/internal/platform/editing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVNDBReleaseRerunKeepsHumanExtras(t *testing.T) {
	clean(t)
	cleanReleases(t)
	require.NoError(t, testDB.Exec(
		"TRUNCATE edit_proposal_amendment, edit_proposal, edit_revision RESTART IDENTITY CASCADE").Error)
	seedVNDBWork(t, "v10")
	insRelease(t, "r1", "ja", 20200115, i16(18), false, false, true)
	insReleaseVN(t, "r1", "v10", "complete")
	insReleaseTitle(t, "r1", "ja", false, "タイトルA", "")
	insReleasePlatform(t, "r1", "win")
	insReleasePlatform(t, "r1", "swi")

	first, err := New(testDB, nil, Options{}).RunReleases()
	require.NoError(t, err)
	require.Equal(t, 1, first.ReleasesWritten)
	var rel model.CatalogRelease
	require.NoError(t, testDB.Where("extra->>'vndb_id' = 'r1'").First(&rel).Error)

	reg := editing.NewRegistry()
	require.NoError(t, editspec.RegisterRelease(reg, testDB))
	ren := editing.PolicyContext{
		UserID: 100, Site: "editor",
		HasPerm: func(key string) bool { return perm.Resolver.Can([]string{"ren"}, authz.Permission(key)) },
	}
	_, rev, err := editing.NewEngine(testDB, reg).CreateProposal(context.Background(), editing.CreateProposalInput{
		EntityType: editspec.TypeRelease, EntityID: rel.ID, Actor: ren,
		Patch: map[string]any{
			editspec.FieldReleasePlatforms: []any{"ps4"},
			editspec.FieldReleaseLanguages: []any{"en"},
			editspec.FieldReleaseFreeware:  true,
			editspec.FieldReleaseMinAge:    nil,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, rev)

	second, err := New(testDB, nil, Options{}).RunReleases()
	require.NoError(t, err)
	assert.Zero(t, second.ReleasesWritten)
	assert.Equal(t, 1, second.SkippedExisting)

	var after model.CatalogRelease
	require.NoError(t, testDB.First(&after, rel.ID).Error)
	var extra map[string]any
	require.NoError(t, json.Unmarshal(after.Extra, &extra))
	assert.Equal(t, []any{"ps4"}, extra["platforms"])
	assert.Equal(t, []any{"en"}, extra["languages"])
	assert.Equal(t, true, extra["freeware"])
	assert.NotContains(t, extra, "minage", "a minimum age a person cleared stays cleared")
	assert.Equal(t, "r1", extra["vndb_id"], "the identity payload is never edited")
	require.NotNil(t, after.Platform)
	assert.Equal(t, "ps4", *after.Platform)
}
