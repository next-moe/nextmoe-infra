package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateReport(t *testing.T) {
	base := PlaytimeReport{ActorUID: 7, WorkID: 1, ClientID: "kurumi", Minutes: 600}

	cases := []struct {
		name string
		mut  func(*PlaytimeReport)
		want error
	}{
		{"a well-formed report", func(*PlaytimeReport) {}, nil},
		{"zero minutes", func(r *PlaytimeReport) { r.Minutes = 0 }, nil},
		{"exactly the ceiling", func(r *PlaytimeReport) { r.Minutes = model.PlaytimeMinutesMax }, nil},
		{"past the ceiling", func(r *PlaytimeReport) { r.Minutes = model.PlaytimeMinutesMax + 1 }, ErrPlaytimeMinutesRange},
		{"negative minutes", func(r *PlaytimeReport) { r.Minutes = -1 }, ErrPlaytimeMinutesRange},
		{"no user", func(r *PlaytimeReport) { r.ActorUID = 0 }, ErrPlaytimeActorRequired},
		{"no client", func(r *PlaytimeReport) { r.ClientID = "" }, ErrPlaytimeClientRequired},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := base
			c.mut(&r)
			err := validateReport(r)
			if c.want == nil {
				require.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, c.want)
		})
	}
}

func TestDeleteMineLeavesOtherClientsRows(t *testing.T) {
	require.NoError(t, testDB.Exec("TRUNCATE catalog_user_playtime RESTART IDENTITY").Error)
	svc := NewUserPlaytimeService(testDB)
	ctx := context.Background()
	w := createWork(t, "playtime-delete-own-client")

	for _, r := range []PlaytimeReport{
		{ActorUID: 7, WorkID: w.ID, ClientID: "kungal", Minutes: 600},
		{ActorUID: 7, WorkID: w.ID, ClientID: "moyu", Minutes: 90},
		{ActorUID: 8, WorkID: w.ID, ClientID: "kungal", Minutes: 30},
	} {
		_, err := svc.Report(ctx, r)
		require.NoError(t, err)
	}

	require.NoError(t, svc.DeleteMine(ctx, 7, w.ID, "kungal"))

	got, err := svc.GetMine(ctx, 7, w.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 90, got.Minutes)
	assert.Equal(t, 1, got.Clients)

	other, err := svc.GetMine(ctx, 8, w.ID)
	require.NoError(t, err)
	require.NotNil(t, other)
	assert.Equal(t, 30, other.Minutes)

	require.NoError(t, svc.DeleteMine(ctx, 7, w.ID, "moyu"))
	got, err = svc.GetMine(ctx, 7, w.ID)
	require.NoError(t, err)
	assert.Nil(t, got)

	assert.ErrorIs(t, svc.DeleteMine(ctx, 8, w.ID, ""), ErrPlaytimeClientRequired)
}

func TestPlaytimeSourcesAcrossApps(t *testing.T) {
	cleanFeed(t)
	svc := NewUserPlaytimeService(testDB)
	ctx := context.Background()
	w := createWork(t, "playtime-sources")
	key := fmt.Sprintf("pt:7:%d:%s", w.ID, time.Now().In(beijing).Format("20060102"))

	for _, r := range []PlaytimeReport{
		{ActorUID: 7, WorkID: w.ID, ClientID: "timer", Minutes: 600},
		{ActorUID: 7, WorkID: w.ID, ClientID: "kungal-app", Minutes: 90},
		{ActorUID: 7, WorkID: w.ID, ClientID: "forum", Minutes: 300},
		{ActorUID: 8, WorkID: w.ID, ClientID: "timer", Minutes: 30},
	} {
		_, err := svc.Report(ctx, r)
		require.NoError(t, err)
	}

	got, err := svc.ListSources(ctx, 7, w.ID)
	require.NoError(t, err)
	clients := make([]string, 0, len(got))
	for _, r := range got {
		clients = append(clients, r.ClientID)
	}
	assert.Equal(t, []string{"timer", "forum", "kungal-app"}, clients, "most minutes first, the user's rows only")

	require.NoError(t, svc.DeleteSource(ctx, 7, w.ID, "timer"), "any app may delete another app's row")
	merged, err := svc.GetMine(ctx, 7, w.ID)
	require.NoError(t, err)
	assert.Equal(t, 300, merged.Minutes, "the merged total falls to the largest remaining row")
	require.False(t, feedItems(t)[key].Removed, "rows remain, so the day stays")

	assert.ErrorIs(t, svc.DeleteSource(ctx, 7, w.ID, "timer"), ErrPlaytimeSourceNotFound)
	assert.ErrorIs(t, svc.DeleteSource(ctx, 7, w.ID, ""), ErrPlaytimeClientRequired)

	require.NoError(t, svc.DeleteSources(ctx, 7, w.ID))
	left, err := svc.ListSources(ctx, 7, w.ID)
	require.NoError(t, err)
	assert.Empty(t, left)
	require.True(t, feedItems(t)[key].Removed, "with no row left the day goes")
	require.NoError(t, svc.DeleteSources(ctx, 7, w.ID), "deleting nothing is not an error")

	other, err := svc.ListSources(ctx, 8, w.ID)
	require.NoError(t, err)
	require.Len(t, other, 1, "another user's rows are untouched")
}
