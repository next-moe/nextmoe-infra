package handler

import (
	"context"
	"testing"

	"api/internal/platform/authz"
	catalogPerm "api/internal/platform/catalog/perm"

	"github.com/stretchr/testify/require"
)

func ratingModeratorCtx(roles []string) context.Context {
	ctx := context.WithValue(context.Background(), ctxUserID, int64(7))
	ctx = context.WithValue(ctx, ctxClientID, "kungal-client")
	return context.WithValue(ctx, ctxRoles, roles)
}

// Production's permission console grants creator every catalog review key, so
// the gate has to be checked against an overlay like that one, not the code
// bundles alone.
func TestRatingModerationNeedsItsOwnKey(t *testing.T) {
	prev := catalogPerm.Resolver.Resolver()
	t.Cleanup(func() { catalogPerm.Resolver.Swap(prev) })
	catalogPerm.Resolver.Swap(authz.NewResolver(authz.Bundles{
		"creator": {catalogPerm.Review, catalogPerm.ClaimReview, catalogPerm.EditWorkReview,
			catalogPerm.EditTaxonomyReview, catalogPerm.EditCharacterReview, catalogPerm.EditReleaseReview},
		"moderator": {catalogPerm.RatingModerate},
	}))

	require.True(t, catalogPerm.Moderates([]string{"creator"}), "positive control: creator reviews")
	require.Error(t, requireRatingModerator(ratingModeratorCtx([]string{"creator"})),
		"reviewing edits and claims must not reach anyone's rating")
	require.NoError(t, requireRatingModerator(ratingModeratorCtx([]string{"moderator"})))
	require.Error(t, requireRatingModerator(context.WithValue(context.Background(), ctxRoles, []string{"moderator"})),
		"an application key carries no user to act as")
}

func TestRatingModerationFollowsTheCodeBundles(t *testing.T) {
	for _, role := range []string{"moderator", "admin", "ren"} {
		require.NoError(t, requireRatingModerator(ratingModeratorCtx([]string{role})), role)
	}
	for _, role := range []string{"user", "creator"} {
		require.Error(t, requireRatingModerator(ratingModeratorCtx([]string{role})), role)
	}
}
