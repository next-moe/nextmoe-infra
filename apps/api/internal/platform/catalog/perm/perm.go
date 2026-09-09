package perm

import "api/internal/platform/authz"

const Review authz.Permission = "catalog.review"

const ClaimReview authz.Permission = "catalog.claim.review"

const (
	EditWork       authz.Permission = "edit.catalog.work"
	EditWorkReview authz.Permission = "edit.catalog.work.review"
)

const (
	EditTaxonomy       authz.Permission = "edit.catalog.taxonomy"
	EditTaxonomyReview authz.Permission = "edit.catalog.taxonomy.review"
)

const (
	EditCharacter       authz.Permission = "edit.catalog.character"
	EditCharacterReview authz.Permission = "edit.catalog.character.review"
)

const (
	EditRelease       authz.Permission = "edit.catalog.release"
	EditReleaseReview authz.Permission = "edit.catalog.release.review"
)

const EditTrusted authz.Permission = "catalog.edit.trusted"

// ClaimTrusted is deliberately not implied by EditTrusted: the two were one key
// until 2026-09, and the forum needed "submissions land live, edits still queue"
// for its moderators — grantable only if the mint lane reads its own key.
const ClaimTrusted authz.Permission = "catalog.claim.trusted"

var moderatorPerms = []authz.Permission{ClaimReview}

var adminPerms = append(append([]authz.Permission{}, moderatorPerms...),
	EditWork, EditWorkReview, EditTaxonomy, EditTaxonomyReview,
	EditCharacter, EditCharacterReview, EditRelease, EditReleaseReview,
	EditTrusted, ClaimTrusted)

var renPerms = append(append([]authz.Permission{}, adminPerms...), Review)

// The v2 moderation faces are one place, not one per family: a proposal queue
// row can be any registered entity type, and a snapshot is addressed by family.
// Standing to read them is therefore "can reach a verdict on something", which
// is the union below. It is a hand-maintained list over a growing permission
// set: a new review permission that is not added here is silently not
// moderation authority.
var moderationPerms = []authz.Permission{
	ClaimReview, Review,
	EditWorkReview, EditTaxonomyReview, EditCharacterReview, EditReleaseReview,
}

func Moderates(roles []string) bool {
	for _, p := range moderationPerms {
		if Resolver.Can(roles, p) {
			return true
		}
	}
	return false
}

var Bundles = authz.Bundles{
	"moderator": moderatorPerms,
	"admin":     adminPerms,
	"ren":       renPerms,
}

var Resolver = authz.NewHolder(Bundles)
