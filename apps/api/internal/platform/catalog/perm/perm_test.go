package perm_test

import (
	"testing"

	"api/internal/platform/authz"
	"api/internal/platform/catalog/perm"
)

var goldenGrants = map[authz.Permission][]string{
	perm.Review:              {"ren"},
	perm.ClaimReview:         {"moderator", "admin", "ren"},
	perm.EditWork:            {"editor", "moderator", "admin", "ren"},
	perm.EditWorkReview:      {"moderator", "admin", "ren"},
	perm.EditTaxonomy:        {"editor", "moderator", "admin", "ren"},
	perm.EditTaxonomyReview:  {"moderator", "admin", "ren"},
	perm.EditCharacter:       {"editor", "moderator", "admin", "ren"},
	perm.EditCharacterReview: {"moderator", "admin", "ren"},
	perm.EditRelease:         {"editor", "moderator", "admin", "ren"},
	perm.EditReleaseReview:   {"moderator", "admin", "ren"},
	perm.EditPerson:          {"editor", "moderator", "admin", "ren"},
	perm.EditPersonReview:    {"moderator", "admin", "ren"},
	perm.EditTrusted:         {"editor", "moderator", "admin", "ren"},
	perm.ClaimTrusted:        {"moderator", "admin", "ren"},
	perm.RatingModerate:      {"moderator", "admin", "ren"},
	perm.FolderModerate:      {"moderator", "admin", "ren"},
	perm.EditPolicyManage:    {"admin", "ren"},
	perm.MergeReview:         {"moderator", "admin", "ren"},
	perm.DiscussionModerate:  {"moderator", "admin", "ren"},
	perm.DeleteReview:        {"moderator", "admin", "ren"},
}

var allRoles = []string{"user", "creator", "editor", "moderator", "admin", "ren"}

// goldenGrants is the contract, and every other test here only asserts over the
// keys it already lists — so a key added to a bundle but forgotten here is
// granted to roles nobody ever checked, silently. This is the assertion that
// looks at the list itself.
func TestGoldenCoversEveryBundledPermission(t *testing.T) {
	for bundle, perms := range perm.Bundles {
		for _, p := range perms {
			if _, listed := goldenGrants[p]; !listed {
				t.Errorf("bundle %q grants %q, which no golden row covers", bundle, p)
			}
		}
	}
	for p := range goldenGrants {
		granted := false
		for _, perms := range perm.Bundles {
			for _, bundled := range perms {
				if bundled == p {
					granted = true
				}
			}
		}
		if !granted {
			t.Errorf("golden row %q is in no bundle", p)
		}
	}
}

func TestGoldenBundles(t *testing.T) {
	for p, granted := range goldenGrants {
		grantedSet := make(map[string]bool, len(granted))
		for _, r := range granted {
			grantedSet[r] = true
		}
		for _, role := range allRoles {
			want := grantedSet[role]
			if got := perm.Resolver.Can([]string{role}, p); got != want {
				t.Errorf("Can([%q], %q) = %v, want %v", role, p, got, want)
			}
		}
	}
}

func TestNonBundleRolesGrantNothing(t *testing.T) {
	for _, role := range []string{"user", "", "legacy_top_tier_alias"} {
		for p := range goldenGrants {
			if perm.Resolver.Can([]string{role}, p) {
				t.Errorf("non-bundle role %q must grant nothing, but grants %q", role, p)
			}
		}
	}
}

func TestManagementAxisContainment(t *testing.T) {
	for p := range goldenGrants {
		if perm.Resolver.Can([]string{"moderator"}, p) && !perm.Resolver.Can([]string{"admin"}, p) {
			t.Errorf("admin must grant everything moderator grants; missing %q", p)
		}
		if perm.Resolver.Can([]string{"admin"}, p) && !perm.Resolver.Can([]string{"ren"}, p) {
			t.Errorf("ren must grant everything admin grants; missing %q", p)
		}
	}
}

func TestCreatorGrantsNothing(t *testing.T) {
	for p := range goldenGrants {
		if perm.Resolver.Can([]string{"creator"}, p) {
			t.Errorf("creator must grant nothing on the catalog surface, but grants %q", p)
		}
	}
}

func TestModeratesTracksTheReviewBundles(t *testing.T) {
	for _, role := range []string{"moderator", "admin", "ren"} {
		if !perm.Moderates([]string{role}) {
			t.Errorf("%s can reach a verdict on something and must read the moderation faces", role)
		}
	}
	for _, role := range []string{"user", "creator", "editor"} {
		if perm.Moderates([]string{role}) {
			t.Errorf("%s holds no review permission and must not read the moderation queue", role)
		}
	}
	if perm.Moderates(nil) {
		t.Error("a token with no roles is not moderation authority")
	}
}

func TestReviewsEditsPlacesTheReviewerTier(t *testing.T) {
	for _, role := range []string{"moderator", "admin", "ren"} {
		if !perm.ReviewsEdits([]string{role}) {
			t.Errorf("%s reviews edits", role)
		}
	}
	for _, role := range []string{"user", "creator", "editor"} {
		if perm.ReviewsEdits([]string{role}) {
			t.Errorf("%s reviews no edits in code", role)
		}
	}
}

func TestKnownIsTheBundledSet(t *testing.T) {
	for p := range goldenGrants {
		if !perm.Known(string(p)) {
			t.Errorf("%q is bundled but not Known", p)
		}
	}
	if perm.Known("edit.catalog.nothing") {
		t.Error("an unbundled key must not be Known")
	}
}
