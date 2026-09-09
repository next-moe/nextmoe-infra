package handler

import (
	"testing"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

// v1's submit carried released and wave R3 dropped it, so a v2 mint had no way
// to date a new work at all: catalog.release proposals only edit existing rows,
// and a freshly minted work has none.
func TestLiveMintClaimWithReleased(t *testing.T) {
	env := liveCatalog(t)

	status, rec, p := liveCreateClaim(t, env, livePlainToken,
		`{"display_name":"Dated Mint","field_values":{"catalog.work.olang":"ja"},"released":{"y":2024,"m":7,"d":26}}`)
	require.Equal(t, 201, status, p.Detail)

	var rel model.CatalogRelease
	require.NoError(t, env.db.Where("work_id = ?", liveNum(t, rec.ID)).First(&rel).Error)
	require.EqualValues(t, model.ReleaseKindDefault, rel.Kind)
	require.NotNil(t, rel.ReleasedY)
	require.EqualValues(t, 2024, *rel.ReleasedY)
	require.NotNil(t, rel.ReleasedM)
	require.EqualValues(t, 7, *rel.ReleasedM)
	require.NotNil(t, rel.ReleasedD)
	require.EqualValues(t, 26, *rel.ReleasedD)

	// The suite shares one database and TestLiveCalendarMeta asserts the global
	// month range; a leftover 2024-07 release row widens it and fails that test.
	require.NoError(t, env.db.Unscoped().Delete(&rel).Error)
}

func TestLiveMintClaimReleasedRefusals(t *testing.T) {
	env := liveCatalog(t)

	// A date without a mint has nowhere to go; the row it would date exists.
	status, _, p := liveCreateClaim(t, env, livePlainToken,
		`{"work_id":"`+idstr(env.fx.Claimable)+`","released":{"y":2024}}`)
	require.Equal(t, 422, status)
	require.Equal(t, problem.CodeValidationFailed, p.Code)
	require.Len(t, p.Errors, 1)
	require.Equal(t, "/released", p.Errors[0].Pointer)

	// d requires m: the service refusal must surface as a field error, not a 500.
	status, _, p = liveCreateClaim(t, env, livePlainToken,
		`{"display_name":"Bad Date Mint","released":{"y":2024,"d":4}}`)
	require.Equal(t, 422, status)
	require.Equal(t, problem.CodeValidationFailed, p.Code)
	require.Len(t, p.Errors, 1)
	require.Equal(t, "/released", p.Errors[0].Pointer)

	// refs that resolve claim the match, which would silently drop the date —
	// the same 409 field_values answers on this lane.
	target := liveRefTarget(t, env, "Released Ref Target", "v66603")
	status, _, p = liveCreateClaim(t, env, livePlainToken,
		`{"refs":[{"source":"vndb","external_id":"v66603"}],"released":{"y":2024}}`)
	require.Equal(t, 409, status)
	require.Equal(t, problem.CodeAlreadyExists, p.Code)
	require.Contains(t, p.Detail, idstr(target))
}
