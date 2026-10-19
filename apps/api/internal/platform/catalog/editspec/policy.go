package editspec

import (
	"api/internal/platform/authz"
	"api/internal/platform/editing"
)

func ownedEntityPolicy(review authz.Permission) editing.Policy {
	return editing.Policy{
		Propose:     editing.ProposeOpen,
		Review:      editing.ReviewPerm(string(review)),
		Automerge:   editing.AutomergeTrustedOrReview,
		OwnerReview: true,
	}
}

// A character or vocabulary row is shared by every work that references it, so
// even a reviewer's own edit waits for a second pair of eyes.
func sharedEntityPolicy(review authz.Permission) editing.Policy {
	return editing.Policy{
		Propose:   editing.ProposeOpen,
		Review:    editing.ReviewPerm(string(review)),
		Automerge: editing.AutomergeNever,
	}
}
