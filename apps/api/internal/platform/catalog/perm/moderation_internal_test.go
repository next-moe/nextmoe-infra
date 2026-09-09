package perm

import (
	"slices"
	"testing"

	"api/internal/platform/authz"
)

// Every permission any bundle grants is either moderation authority or named
// here as deliberately not. The role spot-checks in perm_test.go stay green
// when an entry falls out of moderationPerms, which is exactly the failure the
// comment on that list claims is caught; this is the assertion that looks at
// the list itself.
var notModeration = map[authz.Permission]string{
	EditWork:      "propose side; the .review twin is what reaches a verdict",
	EditTaxonomy:  "propose side",
	EditCharacter: "propose side",
	EditRelease:   "propose side",
	EditTrusted:   "lands the holder's OWN edit without review; it judges nobody else",
	ClaimTrusted:  "lands the holder's OWN mint live without review; it judges nobody else",
}

func TestModerationPermsCoversEveryBundledPermission(t *testing.T) {
	seen := 0
	for role, perms := range Bundles {
		for _, p := range perms {
			seen++
			if slices.Contains(moderationPerms, p) {
				continue
			}
			if _, named := notModeration[p]; named {
				continue
			}
			t.Errorf("%s grants %q, which is neither moderation authority nor a named exclusion", role, p)
		}
	}
	if seen == 0 {
		t.Fatal("positive control: the bundles must grant something for this to assert over")
	}
	for _, p := range moderationPerms {
		if reason, both := notModeration[p]; both {
			t.Errorf("%q is moderation authority and is also excluded as %q", p, reason)
		}
	}
	for p := range notModeration {
		granted := false
		for _, perms := range Bundles {
			if slices.Contains(perms, p) {
				granted = true
			}
		}
		if !granted {
			t.Errorf("%q is excluded but no bundle grants it; the exclusion is stale", p)
		}
	}
}
