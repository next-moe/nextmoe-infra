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

const (
	EditPerson       authz.Permission = "edit.catalog.person"
	EditPersonReview authz.Permission = "edit.catalog.person.review"
)

const EditTrusted authz.Permission = "catalog.edit.trusted"

// ClaimTrusted is deliberately not implied by EditTrusted: the two were one key
// until 2026-09, and the forum needed "submissions land live, edits still queue"
// for its moderators — grantable only if the mint lane reads its own key.
const ClaimTrusted authz.Permission = "catalog.claim.trusted"

const RatingModerate authz.Permission = "catalog.rating.moderate"

const FolderModerate authz.Permission = "catalog.folder.moderate"

const EditPolicyManage authz.Permission = "edit.policy.manage"

const MergeReview authz.Permission = "catalog.merge.review"

var editProposePerms = []authz.Permission{EditWork, EditTaxonomy, EditCharacter, EditRelease, EditPerson}

var editReviewPerms = []authz.Permission{EditWorkReview, EditTaxonomyReview, EditCharacterReview, EditReleaseReview, EditPersonReview}

var editorPerms = append(append([]authz.Permission{}, editProposePerms...), EditTrusted)

var moderatorPerms = concat(
	[]authz.Permission{ClaimReview, RatingModerate, FolderModerate, EditTrusted, ClaimTrusted, MergeReview},
	editProposePerms, editReviewPerms)

var adminPerms = append(append([]authz.Permission{}, moderatorPerms...), EditPolicyManage)

var renPerms = append(append([]authz.Permission{}, adminPerms...), Review)

func concat(parts ...[]authz.Permission) []authz.Permission {
	var out []authz.Permission
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func ReviewsEdits(roles []string) bool {
	for _, p := range editReviewPerms {
		if Resolver.Can(roles, p) {
			return true
		}
	}
	return false
}

// The v2 moderation faces are one place, not one per family: a proposal queue
// row can be any registered entity type, and a snapshot is addressed by family.
// Standing to read them is therefore "can reach a verdict on something", which
// is the union below. It is a hand-maintained list over a growing permission
// set: a new review permission that is not added here is silently not
// moderation authority.
var moderationPerms = concat([]authz.Permission{ClaimReview, Review, MergeReview}, editReviewPerms)

func Moderates(roles []string) bool {
	for _, p := range moderationPerms {
		if Resolver.Can(roles, p) {
			return true
		}
	}
	return false
}

var Bundles = authz.Bundles{
	"editor":    editorPerms,
	"moderator": moderatorPerms,
	"admin":     adminPerms,
	"ren":       renPerms,
}

func Known(key string) bool {
	for _, perms := range Bundles {
		for _, p := range perms {
			if string(p) == key {
				return true
			}
		}
	}
	return false
}

var Resolver = authz.NewHolder(Bundles)
