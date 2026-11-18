package handler

import (
	"context"
	"testing"

	"api/internal/platform/apiv2/problem"
	catsvc "api/internal/platform/catalog/service"

	"github.com/stretchr/testify/require"
)

func TestPlaytimeSourcesGateNamesAndDeletes(t *testing.T) {
	env := liveCatalog(t)
	work := env.fx.Work
	const uid = int64(990071)
	clean := func() {
		require.NoError(t, env.db.Exec("DELETE FROM catalog_user_playtime WHERE actor_uid = ?", uid).Error)
	}
	clean()
	t.Cleanup(clean)
	for client, minutes := range map[string]int{"forum-site": 300, "timer-app": 600, "gone-app": 20} {
		_, err := env.cat.Playtime.Report(t.Context(), catsvc.PlaytimeReport{ActorUID: uid, WorkID: work, ClientID: client, Minutes: minutes})
		require.NoError(t, err)
	}
	cat := *env.cat
	cat.AppNameOf = func(_ context.Context, id string) (string, error) {
		return map[string]string{"forum-site": "鲲 Galgame 论坛", "timer-app": "Timer"}[id], nil
	}
	firstParty := context.WithValue(contextWithUser(t.Context(), uid, "forum-site"), ctxSite, "kungal")

	list, err := cat.ListPlaytimeSources(firstParty, work)
	require.NoError(t, err)
	require.Len(t, list.Items, 3)
	require.Equal(t, "timer-app", list.Items[0].ClientID)
	require.Equal(t, "Timer", *list.Items[0].AppName)
	require.False(t, list.Items[0].Self)
	require.Equal(t, "forum-site", list.Items[1].ClientID)
	require.True(t, list.Items[1].Self, "the caller's own row")
	require.Nil(t, list.Items[2].AppName, "an app that no longer exists has no name")

	unbound := contextWithUser(t.Context(), uid, "timer-app")
	thirdParty := context.WithValue(context.WithValue(unbound, ctxSite, "kungal"), ctxThirdParty, true)
	for name, ctx := range map[string]context.Context{"no site": unbound, "third party": thirdParty} {
		_, err := cat.ListPlaytimeSources(ctx, work)
		requireProblemStatus(t, err, 403, name)
		requireProblemStatus(t, cat.DeletePlaytimeSource(ctx, work, "forum-site"), 403, name)
		requireProblemStatus(t, cat.DeletePlaytimeSources(ctx, work), 403, name)
	}

	require.NoError(t, cat.DeletePlaytimeSource(firstParty, work, "timer-app"), "a first-party app deletes another app's row")
	requireProblemStatus(t, cat.DeletePlaytimeSource(firstParty, work, "timer-app"), 404, "already gone")
	merged, err := cat.GetPlaytime(firstParty, work)
	require.NoError(t, err)
	require.Equal(t, 300, merged.Minutes)

	require.NoError(t, cat.DeletePlaytimeSources(firstParty, work))
	require.NoError(t, cat.DeletePlaytimeSources(firstParty, work), "idempotent")
	list, err = cat.ListPlaytimeSources(firstParty, work)
	require.NoError(t, err)
	require.Empty(t, list.Items)
}

func requireProblemStatus(t *testing.T, err error, status int, msg string) {
	t.Helper()
	p, ok := err.(*problem.Problem)
	require.True(t, ok, "%s: %v", msg, err)
	require.Equal(t, status, p.Status, "%s: %s", msg, p.Code)
}
