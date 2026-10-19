package service

import (
	"fmt"
	"strings"
	"testing"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

func suppressEntity(t *testing.T, entityType string, entityID int64, field, key string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO edit_suppressed_row (entity_type, entity_id, field_key, identity_key, created_at)
		VALUES (?, ?, ?, ?, now())`, entityType, entityID, field, key).Error)
}

func openProposalOn(t *testing.T, entityType string, entityID int64, patch string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, testDB.Raw(`INSERT INTO edit_proposal
		(entity_family, entity_type, entity_id, base_revision_seq, patch, proposer_uid, note, site, status, decision_note, created_at, updated_at)
		VALUES ('catalog', ?, ?, 0, ?::jsonb, 77, '', 'editor', ?, '', now(), now())
		RETURNING id`, entityType, entityID, patch, editing.StatusOpen).Scan(&id).Error)
	return id
}

func TestNameFaceDropsSuppressedAliasesAndPersonLinks(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()

	pe := &model.CatalogPerson{DisplayName: "歌手", FieldProvenance: datatypes.JSON(`{}`)}
	require.NoError(t, testDB.Create(pe).Error)
	addExternalRef(t, model.EntityTypePerson, pe.ID, srcBangumiPub, "4423", model.LinkKindExact)
	addExternalRef(t, model.EntityTypePerson, pe.ID, int16(10), "stale_handle", model.LinkKindRelated)
	addExternalRef(t, model.EntityTypePerson, pe.ID, int16(10), "live_handle", model.LinkKindRelated)
	n := &model.CatalogCreditName{PersonID: &pe.ID, Name: "歌手", Lang: "ja", FieldProvenance: datatypes.JSON(`{}`)}
	require.NoError(t, testDB.Create(n).Error)
	bgm := srcBangumiPub
	for _, a := range []model.CatalogNameAlias{
		{CreditNameID: n.ID, Name: "Kashu", Lang: "en", Kind: model.AliasKindTranslation, SourceID: &bgm},
		{CreditNameID: n.ID, Name: "Wrong Name", Lang: "en", Kind: model.AliasKindTranslation, SourceID: &bgm},
	} {
		require.NoError(t, testDB.Create(&a).Error)
	}

	suppressEntity(t, editspec.TypePerson, pe.ID, editspec.FieldPersonLinks,
		editspec.PersonLinkIdentity("twitter", "stale_handle"))
	suppressEntity(t, editspec.TypeCreditName, n.ID, editspec.FieldCreditNameAliases,
		editspec.CharacterAliasIdentity(model.AliasKindTranslation, "en", "Wrong Name"))

	rec, found, err := svc.Name(t.Context(), n.ID, false, false, 50, 0)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []dto.PublicLink{
		{Source: "bangumi", URL: "https://bgm.tv/person/4423", Identity: true, PageID: "4423"},
		{Source: "twitter", URL: "https://x.com/live_handle"},
	}, rec.Links, "the suppressed related link is gone, the identity anchor and the other related link stay")
	values := make([]string, 0, len(rec.Aliases))
	for _, a := range rec.Aliases {
		values = append(values, a.Value)
	}
	assert.Equal(t, []string{"Kashu"}, values, "the suppressed upstream alias is not rendered")
}

func TestLinkLanesLeaveAHumanDetachedNameAlone(t *testing.T) {
	cleanTables(t)
	host := createPerson(t, "Host")
	x := createCreditName(t, &host.ID, "Host Name")
	require.NoError(t, testDB.Model(&host).Update("primary_credit_name_id", x.ID).Error)
	detached := createCreditName(t, nil, "Detached Name")
	require.NoError(t, testDB.Model(detached).Update("field_provenance",
		datatypes.JSON(`{"person_id":[{"source":"curated","at":"2026-10-19T00:00:00Z"}]}`)).Error)
	mkCreditCandidate(t, x.ID, detached.ID)

	link := acceptLink(t, x.ID, detached.ID)
	assert.True(t, link.NeedsManual, "a person detached this name in the editor; the candidate lane must not relink it")
	assert.Nil(t, personIDOfName(t, detached.ID))
}

func TestCreditNameMergeCarriesEditStateToTheSurvivor(t *testing.T) {
	cleanTables(t)
	require.NoError(t, testDB.Exec(`TRUNCATE edit_proposal_amendment, edit_proposal RESTART IDENTITY CASCADE`).Error)
	loser := createCreditName(t, nil, "旧名義")
	survivor := createCreditName(t, nil, "新名義")
	bgm, curated := srcBangumiPub, int16(12)
	require.NoError(t, testDB.Create(&model.CatalogNameAlias{CreditNameID: survivor.ID, Name: "Twin", Lang: "en",
		Kind: model.AliasKindTranslation, SourceID: &bgm}).Error)
	require.NoError(t, testDB.Create(&model.CatalogNameAlias{CreditNameID: loser.ID, Name: "Twin", Lang: "en",
		Kind: model.AliasKindTranslation, SourceID: &curated}).Error)
	key := editspec.CharacterAliasIdentity(model.AliasKindTranslation, "en", "Hidden")
	suppressEntity(t, editspec.TypeCreditName, loser.ID, editspec.FieldCreditNameAliases, key)
	onLoser := openProposalOn(t, editspec.TypeCreditName, loser.ID, `{"catalog.credit_name.note":"x"}`)

	mergeProposal(t, model.EntityTypeCreditName, loser.ID, survivor.ID)

	var status int16
	var note string
	require.NoError(t, testDB.Raw(`SELECT status, decision_note FROM edit_proposal WHERE id = ?`, onLoser).
		Row().Scan(&status, &note))
	assert.Equal(t, editing.StatusDeclined, status)
	assert.True(t, strings.Contains(note, fmt.Sprintf("#%d", survivor.ID)), "note %q names the survivor", note)

	var keys []string
	require.NoError(t, testDB.Raw(`SELECT identity_key FROM edit_suppressed_row WHERE entity_type = ? AND entity_id = ?`,
		editspec.TypeCreditName, survivor.ID).Scan(&keys).Error)
	assert.Equal(t, []string{key}, keys, "the loser's suppression moves to the survivor")

	var twinSource *int16
	require.NoError(t, testDB.Raw(`SELECT source_id FROM catalog_name_alias WHERE credit_name_id = ? AND name = 'Twin'`,
		survivor.ID).Scan(&twinSource).Error)
	require.NotNil(t, twinSource)
	assert.Equal(t, curated, *twinSource, "the human lane's alias wins the collision with the survivor's upstream twin")
}

func TestPersonMergeKeepsAHandEnteredBirthDate(t *testing.T) {
	cleanTables(t)
	y, m, d := int16(1985), int16(3), int16(9)
	sy := int16(1990)
	target := &model.CatalogPerson{DisplayName: "本人", BirthY: &y, BirthM: &m, BirthD: &d,
		FieldProvenance: datatypes.JSON(`{"birth_y":[{"source":"curated","at":"2026-10-19T00:00:00Z"}]}`)}
	source := &model.CatalogPerson{DisplayName: "重複", BirthY: &sy,
		FieldProvenance: datatypes.JSON(`{"birth":[{"source":"vndb","at":"2026-01-01T00:00:00Z"}]}`)}
	require.NoError(t, testDB.Create(target).Error)
	require.NoError(t, testDB.Create(source).Error)

	p, err := testMerge.ProposeMerge(t.Context(), model.EntityTypePerson, source.ID, target.ID, 7, "dup")
	require.NoError(t, err)
	resolveToSource(t, p.ID, "birth")
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(t.Context(), p.ID, nil))

	var merged model.CatalogPerson
	require.NoError(t, testDB.First(&merged, target.ID).Error)
	require.NotNil(t, merged.BirthY)
	assert.Equal(t, int16(1985), *merged.BirthY,
		"a birth date the editor stamped per column must survive a merge resolved to the source")
}
