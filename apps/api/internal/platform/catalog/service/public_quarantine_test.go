package service

import (
	"testing"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicQuarantineInvisibleOnReadFaces(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	svc := newPublicSvc()

	live := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "公開側")
	q := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusQuarantine, "隔離側")

	labelID := addWorkLabel(t, live.ID, "共有ブランド", model.LabelKindGameBrand, model.WorkLabelKindBrand)
	require.NoError(t, testDB.Create(&model.CatalogWorkLabel{
		WorkID: q.ID, LabelID: labelID, Kind: model.WorkLabelKindBrand,
	}).Error)

	name := createCreditName(t, nil, "共有名義")
	roleID := seededRoleID(t)
	createCredit(t, live.ID, name.ID, roleID, nil)
	createCredit(t, q.ID, name.ID, roleID, nil)

	ch := createCharacter(t, "共有キャラ")
	createWorkCharacter(t, live.ID, ch.ID, model.WorkCharacterKindMain, model.SpoilerNone)
	createWorkCharacter(t, q.ID, ch.ID, model.WorkCharacterKindMain, model.SpoilerNone)

	var relTypeID int64
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_relation_type WHERE id <> ? ORDER BY id LIMIT 1`,
		seriesRelationTypeID).Scan(&relTypeID).Error)
	if relTypeID != 0 {
		require.NoError(t, testDB.Create(&model.CatalogWorkRelation{
			AWorkID: live.ID, BWorkID: q.ID, RelationTypeID: relTypeID,
		}).Error)
	}
	createSameSeriesEdge(t, live.ID, q.ID)

	addExternalRef(t, model.EntityTypeWork, live.ID, srcVNDB, "v9001", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeWork, q.ID, srcVNDB, "v9002", model.LinkKindExact)

	page, err := svc.WorksList(ctx, WorksListFilter{Sort: "id"}, "", 50)
	require.NoError(t, err)
	got := make([]int64, 0, len(page.Items))
	for _, it := range page.Items {
		got = append(got, it.ID)
	}
	assert.Equal(t, []int64{live.ID}, got)

	_, found, err := svc.WorkDetail(ctx, q.ID, PublicInclude{Relations: true}, true, 0, PublicFields{})
	require.NoError(t, err)
	assert.False(t, found)

	label, ok, err := svc.Label(ctx, labelID, true, true, 50, 0)
	require.NoError(t, err)
	require.True(t, ok)
	labelIDs := make([]int64, 0, len(label.Works))
	for _, w := range label.Works {
		labelIDs = append(labelIDs, w.Work.ID)
	}
	assert.Equal(t, []int64{live.ID}, labelIDs)

	nm, ok, err := svc.Name(ctx, name.ID, true, EntityWorksGate{NSFW: true}, 50, 0)
	require.NoError(t, err)
	require.True(t, ok)
	creditIDs := make([]int64, 0, len(nm.Credits))
	for _, c := range nm.Credits {
		creditIDs = append(creditIDs, c.Work.ID)
	}
	assert.Equal(t, []int64{live.ID}, creditIDs)

	charRec, ok, err := svc.Character(ctx, ch.ID, true, EntityWorksGate{NSFW: true}, 0, 50, 0)
	require.NoError(t, err)
	require.True(t, ok)
	appearIDs := make([]int64, 0, len(charRec.Works))
	for _, w := range charRec.Works {
		appearIDs = append(appearIDs, w.Work.ID)
	}
	assert.Equal(t, []int64{live.ID}, appearIDs)

	detail, found, err := svc.WorkDetail(ctx, live.ID, PublicInclude{Relations: true}, true, 0, PublicFields{})
	require.NoError(t, err)
	require.True(t, found)
	for _, r := range detail.Relations {
		assert.NotEqual(t, q.ID, r.Work.ID)
	}
	for _, sb := range detail.SeriesSiblings {
		assert.NotEqual(t, q.ID, sb.ID)
	}

	_, found, err = svc.Lookup(ctx, "vndb", "v9002", true)
	require.NoError(t, err)
	assert.False(t, found)
	batch, err := svc.LookupBatch(ctx, []dto.PublicLookupPair{{Source: "vndb", ExternalID: "v9002"}}, true)
	require.NoError(t, err)
	require.Len(t, batch, 1)
	assert.Nil(t, batch[0].Work)

	settleWorks(t)
	changes, err := svc.Changes(ctx, "", 500)
	require.NoError(t, err)
	var qGone *bool
	for _, it := range changes.Items {
		if it.ID == q.ID {
			g := it.Gone
			qGone = &g
		}
	}
	require.NotNil(t, qGone)
	assert.True(t, *qGone)
}
