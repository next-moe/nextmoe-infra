package service

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

func TestFollowCompanyIsIdempotent(t *testing.T) {
	cleanTables(t)
	svc := NewUserEntityFollowService(testDB)
	ctx := t.Context()
	company := createLabel(t, "Idempotent Brand", model.LabelKindGameBrand)

	first, err := svc.FollowCompany(ctx, 7, company, "first-client", "first-site")
	require.NoError(t, err)
	second, err := svc.FollowCompany(ctx, 7, company, "second-client", "second-site")
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	require.True(t, first.CreatedAt.Equal(second.CreatedAt))

	var n int64
	require.NoError(t, testDB.Model(&model.CatalogUserEntityFollow{}).Count(&n).Error)
	require.Equal(t, int64(1), n)
	var row model.CatalogUserEntityFollow
	require.NoError(t, testDB.Take(&row).Error)
	require.Equal(t, "first-client", row.ClientID)
	require.Equal(t, "first-site", row.Site)
}

func TestFollowCompanyRefusesAGoneCompany(t *testing.T) {
	cleanTables(t)
	svc := NewUserEntityFollowService(testDB)
	ctx := t.Context()
	gone := createLabel(t, "Gone Brand", model.LabelKindGameBrand)
	require.NoError(t, testDB.Exec(`UPDATE catalog_label SET deleted_at = now() WHERE id = ?`, gone).Error)

	_, err := svc.FollowCompany(ctx, 7, gone, "client", "site")
	require.ErrorIs(t, err, ErrFollowCompanyUnavailable)
	_, err = svc.FollowCompany(ctx, 7, 999_999_999, "client", "site")
	require.ErrorIs(t, err, ErrFollowCompanyUnavailable)

	var n int64
	require.NoError(t, testDB.Model(&model.CatalogUserEntityFollow{}).Count(&n).Error)
	require.Zero(t, n)
}

func TestFollowCompanyCap(t *testing.T) {
	cleanTables(t)
	svc := NewUserEntityFollowService(testDB)
	ctx := t.Context()
	const uid int64 = 7
	const start int64 = 1_000_000
	require.NoError(t, testDB.Exec(`
		INSERT INTO catalog_user_entity_follow (actor_uid, entity_type, entity_id, client_id, site, created_at)
		SELECT ?, ?, g, 'seed', '', now()
		FROM generate_series(?::bigint, ?::bigint) AS g`,
		uid, model.EntityTypeLabel, start, start+model.EntityFollowsPerUserMax-1).Error)

	live := createLabel(t, "One More Brand", model.LabelKindGameBrand)
	_, err := svc.FollowCompany(ctx, uid, live, "other", "other-site")
	require.ErrorIs(t, err, ErrFollowLimit)

	again, err := svc.FollowCompany(ctx, uid, start, "other", "other-site")
	require.NoError(t, err)
	var kept model.CatalogUserEntityFollow
	require.NoError(t, testDB.First(&kept, again.ID).Error)
	require.Equal(t, start, kept.EntityID)
	require.Equal(t, "seed", kept.ClientID)
	require.Equal(t, "", kept.Site)

	_, err = svc.FollowCompany(ctx, 8, live, "other", "other-site")
	require.NoError(t, err)

	mine, err := svc.CountMine(ctx, uid)
	require.NoError(t, err)
	require.Equal(t, int64(model.EntityFollowsPerUserMax), mine)
	other, err := svc.CountMine(ctx, 8)
	require.NoError(t, err)
	require.Equal(t, int64(1), other)
}

func TestListMyCompanyFollowsNewestFirst(t *testing.T) {
	cleanTables(t)
	svc := NewUserEntityFollowService(testDB)
	ctx := t.Context()
	const uid int64 = 7
	var companies []int64
	for _, name := range []string{"First", "Second", "Third"} {
		id := createLabel(t, name, model.LabelKindGameBrand)
		companies = append(companies, id)
		_, err := svc.FollowCompany(ctx, uid, id, "client", "site")
		require.NoError(t, err)
	}

	page, err := svc.ListMine(ctx, uid, 0, 2)
	require.NoError(t, err)
	require.Len(t, page, 2)
	require.Equal(t, companies[2], page[0].EntityID)
	require.Equal(t, companies[1], page[1].EntityID)
	require.Greater(t, page[0].ID, page[1].ID)

	rest, err := svc.ListMine(ctx, uid, page[1].ID, 2)
	require.NoError(t, err)
	require.Len(t, rest, 1)
	require.Equal(t, companies[0], rest[0].EntityID)
	require.Greater(t, page[1].ID, rest[0].ID)
}

func TestUnfollowCompanyIsIdempotent(t *testing.T) {
	cleanTables(t)
	svc := NewUserEntityFollowService(testDB)
	ctx := t.Context()
	company := createLabel(t, "Leaving Brand", model.LabelKindGameBrand)
	_, err := svc.FollowCompany(ctx, 7, company, "client", "site")
	require.NoError(t, err)

	require.NoError(t, svc.UnfollowCompany(ctx, 7, company))
	require.NoError(t, svc.UnfollowCompany(ctx, 7, company))
	never := createLabel(t, "Never Followed", model.LabelKindPublisher)
	require.NoError(t, svc.UnfollowCompany(ctx, 7, never))

	var n int64
	require.NoError(t, testDB.Model(&model.CatalogUserEntityFollow{}).Count(&n).Error)
	require.Zero(t, n)
}

func TestCountFollowers(t *testing.T) {
	cleanTables(t)
	svc := NewUserEntityFollowService(testDB)
	ctx := t.Context()
	a := createLabel(t, "Counted A", model.LabelKindGameBrand)
	b := createLabel(t, "Counted B", model.LabelKindGameBrand)
	_, err := svc.FollowCompany(ctx, 7, a, "client", "site")
	require.NoError(t, err)
	_, err = svc.FollowCompany(ctx, 8, a, "client", "site")
	require.NoError(t, err)
	_, err = svc.FollowCompany(ctx, 7, b, "client", "site")
	require.NoError(t, err)

	got, err := svc.CountFollowers(ctx, a)
	require.NoError(t, err)
	require.Equal(t, int64(2), got)
	got, err = svc.CountFollowers(ctx, b)
	require.NoError(t, err)
	require.Equal(t, int64(1), got)
	mine, err := svc.CountMine(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, int64(2), mine)
}
