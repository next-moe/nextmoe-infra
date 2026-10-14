package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrivacyDefaultsPublicAndStampsOnlyARealChange(t *testing.T) {
	require.NoError(t, testDB.Exec("TRUNCATE catalog_user_privacy").Error)
	svc := NewUserWorkRecordService(testDB)
	ctx := context.Background()

	p, err := svc.Privacy(ctx, 41)
	require.NoError(t, err)
	require.Equal(t, PlayRecordsPrivacy{}, p, "no row means public, never changed")

	set, err := svc.SetPrivacy(ctx, 41, true)
	require.NoError(t, err)
	require.True(t, set.Private)
	require.NotNil(t, set.UpdatedAt)

	require.NoError(t, testDB.Exec("UPDATE catalog_user_privacy SET updated_at = updated_at - interval '1 day' WHERE actor_uid = 41").Error)
	before, err := svc.Privacy(ctx, 41)
	require.NoError(t, err)

	again, err := svc.SetPrivacy(ctx, 41, true)
	require.NoError(t, err)
	require.Equal(t, before, again, "writing the value it already has changes nothing")

	flipped, err := svc.SetPrivacy(ctx, 41, false)
	require.NoError(t, err)
	require.False(t, flipped.Private)
	require.True(t, flipped.UpdatedAt.After(*before.UpdatedAt))

	other, err := svc.Privacy(ctx, 42)
	require.NoError(t, err)
	require.False(t, other.Private)
}
