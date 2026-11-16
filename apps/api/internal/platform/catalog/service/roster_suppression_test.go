package service

import (
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func suppressRosterEdge(t *testing.T, workID, characterID int64) {
	t.Helper()
	require.NoError(t, testDB.Create(&editing.SuppressedRow{
		EntityType: editspec.TypeWork, EntityID: workID, FieldKey: editspec.FieldWorkRoster,
		IdentityKey: editspec.RosterIdentity(characterID),
	}).Error)
}

func suppressedRosterKeys(t *testing.T, workID int64) []string {
	t.Helper()
	keys, err := editing.LoadSuppressedKeys(t.Context(), testDB, editspec.TypeWork, workID, editspec.FieldWorkRoster)
	require.NoError(t, err)
	return keys
}

// TestRosterSuppressionFollowsCharacterMerge runs pillar 7's follow statements
// against the real registry and the real tables for catalog.work.roster, whose
// key is a single character id. The suppression hangs off works that are not
// themselves part of the merge.
func TestRosterSuppressionFollowsCharacterMerge(t *testing.T) {
	cleanTables(t)
	w1, w2 := createWork(t, "作品1"), createWork(t, "作品2")
	src, dst := createCharacter(t, "旧キャラ"), createCharacter(t, "新キャラ")
	bystander := createCharacter(t, "無関係")

	createWorkCharacter(t, w1.ID, src.ID, model.WorkCharacterKindMain, model.SpoilerNone)
	createWorkCharacter(t, w2.ID, src.ID, model.WorkCharacterKindMain, model.SpoilerNone)
	createWorkCharacter(t, w2.ID, dst.ID, model.WorkCharacterKindAppears, model.SpoilerNone)

	suppressRosterEdge(t, w1.ID, src.ID)
	suppressRosterEdge(t, w2.ID, src.ID)
	// w2 already carries the key the rewrite would produce: the source key has
	// nowhere to land and must be dropped rather than raise 23505.
	suppressRosterEdge(t, w2.ID, dst.ID)
	suppressRosterEdge(t, w1.ID, bystander.ID)

	executeMerge(t, model.EntityTypeCharacter, src.ID, dst.ID, "same character")

	assert.ElementsMatch(t, []string{
		editspec.RosterIdentity(dst.ID),
		editspec.RosterIdentity(bystander.ID),
	}, suppressedRosterKeys(t, w1.ID))
	assert.ElementsMatch(t, []string{
		editspec.RosterIdentity(dst.ID),
	}, suppressedRosterKeys(t, w2.ID), "the collided source key is dropped, not duplicated")
}

// TestRosterIdentityRoundTrips is bidirectional on purpose: identity is optional
// on this face, so "present resolves to one row" alone would be satisfied by a
// face that hands the key to everybody.
func TestRosterIdentityRoundTrips(t *testing.T) {
	f := seedCreditReads(t)
	read := NewReadService(testDB)
	pub := newPublicSvc()
	ctx := t.Context()

	rowsFor := func(t *testing.T, workID int64, identity string) int64 {
		t.Helper()
		var n int64
		require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_work_character wc
			WHERE wc.work_id = ? AND `+editspec.RosterIdentitySQL("wc")+` = ?`, workID, identity).
			Scan(&n).Error)
		return n
	}
	check := func(t *testing.T, workID int64, characterID int64, identity string) {
		t.Helper()
		if identity == "" {
			assert.EqualValuesf(t, 0, rowsFor(t, workID, editspec.RosterIdentity(characterID)),
				"character %d carries no identity but is on the roster", characterID)
			return
		}
		assert.EqualValuesf(t, 1, rowsFor(t, workID, identity),
			"identity %q resolved to a number of rows other than one", identity)
	}

	chars, err := read.loadWorkCharacters(ctx, f.work.ID)
	require.NoError(t, err)
	require.Len(t, chars, 2, "one roster edge + one credit-only character")
	seen := 0
	for _, c := range chars {
		check(t, f.work.ID, c.CharacterID, c.Identity)
		if c.Identity != "" {
			seen++
		}
	}
	assert.Equal(t, 1, seen, "exactly the roster half of the union carries an identity")

	rec, _, err := pub.WorkDetail(ctx, f.work.ID, PublicInclude{}, true, 0, PublicFields{})
	require.NoError(t, err)
	require.Len(t, rec.Characters, 2)
	for _, c := range rec.Characters {
		check(t, f.work.ID, c.ID, c.Identity)
	}

	cw, err := read.CharacterWorks(ctx, f.rosterCh.ID, EntityWorksGate{NSFW: true, WithTotal: true}, 50, 0)
	require.NoError(t, err)
	require.NotEmpty(t, cw.Works)
	for _, w := range cw.Works {
		check(t, w.Brief.WorkID, f.rosterCh.ID, w.Identity)
	}
	only, err := read.CharacterWorks(ctx, f.onlyCh.ID, EntityWorksGate{NSFW: true, WithTotal: true}, 50, 0)
	require.NoError(t, err)
	require.NotEmpty(t, only.Works)
	for _, w := range only.Works {
		assert.Empty(t, w.Identity, "a credit-only work must not hand out a roster identity")
		check(t, w.Brief.WorkID, f.onlyCh.ID, w.Identity)
	}
}

// TestSuppressedRosterEdgeLeavesCreditsAlone is the mirror of
// TestSuppressedVACreditLeavesRosterUnion: the two suppression sets address
// different tables and neither may reach into the other.
func TestSuppressedRosterEdgeLeavesCreditsAlone(t *testing.T) {
	f := seedCreditReads(t)
	read := NewReadService(testDB)
	ctx := t.Context()

	suppressRosterEdge(t, f.work.ID, f.rosterCh.ID)

	chars, err := read.loadWorkCharacters(ctx, f.work.ID)
	require.NoError(t, err)
	byID := map[int64]WorkCharacterRow{}
	for _, c := range chars {
		byID[c.CharacterID] = c
	}
	roster, ok := byID[f.rosterCh.ID]
	require.True(t, ok, "the character keeps its two VA credits, so it stays in the union")
	assert.Equal(t, model.WorkCharacterKindUnknown, roster.Kind, "without the edge it is a credit-only character")
	assert.EqualValues(t, model.SpoilerNone, roster.Spoiler)
	assert.Empty(t, roster.Identity, "and it must not hand out the identity of a row it no longer shows")
	assert.Len(t, roster.Va, 2, "suppressing the roster edge touches no credit")

	credits, err := read.WorkCredits(ctx, f.work.ID)
	require.NoError(t, err)
	assert.Len(t, credits, 4, "every credit row of this work is still rendered")
}
