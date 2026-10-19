package service

import (
	"fmt"
	"strings"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openWorkProposal(t *testing.T, workID int64) int64 {
	t.Helper()
	var id int64
	require.NoError(t, testDB.Raw(`INSERT INTO edit_proposal
		(entity_family, entity_type, entity_id, base_revision_seq, patch, proposer_uid, note, site, status, decision_note, created_at, updated_at)
		VALUES ('catalog', ?, ?, 0, '{"catalog.work.olang":"en"}', 77, '', 'kungal', ?, '', now(), now())
		RETURNING id`, editspec.TypeWork, workID, editing.StatusOpen).Scan(&id).Error)
	return id
}

func suppress(t *testing.T, workID int64, field, key string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO edit_suppressed_row (entity_type, entity_id, field_key, identity_key, created_at)
		VALUES (?, ?, ?, ?, now())`, editspec.TypeWork, workID, field, key).Error)
}

func suppressedKeys(t *testing.T, workID int64) []string {
	t.Helper()
	var keys []string
	require.NoError(t, testDB.Raw(`SELECT field_key || '=' || identity_key FROM edit_suppressed_row
		WHERE entity_type = ? AND entity_id = ? ORDER BY 1`, editspec.TypeWork, workID).Scan(&keys).Error)
	return keys
}

func TestMergeDeclinesTheLosersOpenProposals(t *testing.T) {
	cleanTables(t)
	require.NoError(t, testDB.Exec(`TRUNCATE edit_proposal_amendment, edit_proposal RESTART IDENTITY CASCADE`).Error)
	l := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "loser").ID
	s := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "survivor").ID
	onLoser := openWorkProposal(t, l)
	onSurvivor := openWorkProposal(t, s)

	mergeProposal(t, model.EntityTypeWork, l, s)

	var got struct {
		Status       int16
		DecisionNote string
		Decided      bool
	}
	require.NoError(t, testDB.Raw(`SELECT status, decision_note, decided_at IS NOT NULL AS decided
		FROM edit_proposal WHERE id = ?`, onLoser).Scan(&got).Error)
	assert.Equal(t, editing.StatusDeclined, got.Status, "nothing can merge a proposal on a retired work")
	assert.True(t, got.Decided)
	assert.True(t, strings.Contains(got.DecisionNote, fmt.Sprintf("#%d", s)), "note %q names the survivor", got.DecisionNote)

	var survivorStatus int16
	require.NoError(t, testDB.Raw(`SELECT status FROM edit_proposal WHERE id = ?`, onSurvivor).Scan(&survivorStatus).Error)
	assert.Equal(t, editing.StatusOpen, survivorStatus, "the survivor's own queue is untouched")
}

func TestUnmergeHandsTheRebuiltWorkItsSuppressionsBack(t *testing.T) {
	cleanTables(t)
	l := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "loser").ID
	s := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "survivor").ID
	const titles, tags = "catalog.work.titles.suppressed", "catalog.work.tag_ids.suppressed"
	suppress(t, l, titles, "title:0:ja:loser only")
	suppress(t, l, tags, "tag:1002")
	suppress(t, s, tags, "tag:1002")
	pid := mergeProposal(t, model.EntityTypeWork, l, s)

	survivorAfterMerge := suppressedKeys(t, s)
	res, err := testMerge.Unmerge(t.Context(), pid, nil)
	require.NoError(t, err)

	assert.Equal(t, []string{tags + "=tag:1002", titles + "=title:0:ja:loser only"}, suppressedKeys(t, res.NewID),
		"the moved row and the one dropped as a duplicate both come back")
	assert.Equal(t, 2, res.Archive.Restored["edit_suppressed_row"])
	assert.Equal(t, survivorAfterMerge, suppressedKeys(t, s), "the survivor keeps what the merge gave it")
}

func TestEditTypeOfFamilyCoversTheRegistry(t *testing.T) {
	reg := editing.NewRegistry()
	require.NoError(t, editspec.RegisterAll(reg, testDB))
	for tag, family := range map[string]int16{
		editspec.TagWork:       model.EntityTypeWork,
		editspec.TagCharacter:  model.EntityTypeCharacter,
		editspec.TagLabel:      model.EntityTypeLabel,
		editspec.TagPerson:     model.EntityTypePerson,
		editspec.TagCreditName: model.EntityTypeCreditName,
	} {
		_, registered := reg.Type("catalog." + tag)
		mapped, ok := editTypeOfFamily[family]
		assert.Equal(t, registered, ok, "family %s: registered as an editing type vs closed on merge", tag)
		if ok {
			assert.Equal(t, "catalog."+tag, mapped, "family %s", tag)
		}
	}
}
