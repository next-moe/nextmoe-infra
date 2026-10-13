package handler

import (
	"context"
	"testing"

	"api/internal/platform/authz"
	catalogPerm "api/internal/platform/catalog/perm"

	"github.com/stretchr/testify/require"
)

func moderationKeyCtx(roles []string) context.Context {
	ctx := context.WithValue(context.Background(), ctxUserID, int64(7))
	ctx = context.WithValue(ctx, ctxClientID, "kungal-client")
	return context.WithValue(ctx, ctxRoles, roles)
}

var moderationKeys = []authz.Permission{catalogPerm.RatingModerate, catalogPerm.FolderModerate}

// Production's permission console grants creator every catalog review key, so
// the gate has to be checked against an overlay like that one, not the code
// bundles alone.
func TestModerationNeedsItsOwnKey(t *testing.T) {
	prev := catalogPerm.Resolver.Resolver()
	t.Cleanup(func() { catalogPerm.Resolver.Swap(prev) })
	catalogPerm.Resolver.Swap(authz.NewResolver(authz.Bundles{
		"creator": {catalogPerm.Review, catalogPerm.ClaimReview, catalogPerm.EditWorkReview,
			catalogPerm.EditTaxonomyReview, catalogPerm.EditCharacterReview, catalogPerm.EditReleaseReview},
		"rater":     {catalogPerm.RatingModerate},
		"moderator": moderationKeys,
	}))

	require.True(t, catalogPerm.Moderates([]string{"creator"}), "positive control: creator reviews")
	for _, key := range moderationKeys {
		require.Error(t, requireModerationKey(moderationKeyCtx([]string{"creator"}), key),
			"reviewing edits and claims must not reach anyone's %s", key)
		require.NoError(t, requireModerationKey(moderationKeyCtx([]string{"moderator"}), key), key)
		require.Error(t, requireModerationKey(context.WithValue(context.Background(), ctxRoles, []string{"moderator"}), key),
			"an application key carries no user to act as")
	}
	require.Error(t, requireModerationKey(moderationKeyCtx([]string{"rater"}), catalogPerm.FolderModerate),
		"each key opens its own faces")
}

func TestModerationFollowsTheCodeBundles(t *testing.T) {
	for _, key := range moderationKeys {
		for _, role := range []string{"moderator", "admin", "ren"} {
			require.NoError(t, requireModerationKey(moderationKeyCtx([]string{role}), key), "%s %s", role, key)
		}
		for _, role := range []string{"user", "creator"} {
			require.Error(t, requireModerationKey(moderationKeyCtx([]string{role}), key), "%s %s", role, key)
		}
	}
}
