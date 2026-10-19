package editspec_test

import (
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/perm"
	"api/internal/platform/editing"
)

func TestOnePlatformPolicyForEveryType(t *testing.T) {
	reg := editing.NewRegistry()
	if err := editspec.RegisterAll(reg, testDB); err != nil {
		t.Fatalf("register: %v", err)
	}
	owned := map[string]string{
		editspec.TypeWork:    string(perm.EditWorkReview),
		editspec.TypeRelease: string(perm.EditReleaseReview),
	}
	shared := map[string]string{
		editspec.TypeCharacter:  string(perm.EditCharacterReview),
		editspec.TypeLabel:      string(perm.EditTaxonomyReview),
		editspec.TypeTag:        string(perm.EditTaxonomyReview),
		editspec.TypeEngine:     string(perm.EditTaxonomyReview),
		editspec.TypeSeries:     string(perm.EditTaxonomyReview),
		editspec.TypePerson:     string(perm.EditPersonReview),
		editspec.TypeCreditName: string(perm.EditPersonReview),
	}
	if len(owned)+len(shared) != len(reg.Types()) {
		t.Fatalf("registered types %v: every one must be classified owned or shared here", reg.Types())
	}
	for _, typ := range reg.Types() {
		spec, _ := reg.Type(typ)
		for i := range spec.Fields {
			key := spec.Fields[i].Key
			got := spec.EffectivePolicy(key)
			if got.Propose != editing.ProposeOpen {
				t.Errorf("%s: propose = %q, want open", key, got.Propose)
			}
			if review, ok := owned[typ]; ok {
				if got.Review != editing.ReviewPerm(review) || got.Automerge != editing.AutomergeTrustedOrReview || !got.OwnerReview {
					t.Errorf("%s: %+v, want {open, perm:%s, trusted_or_review, owner review}", key, got, review)
				}
				continue
			}
			review := shared[typ]
			if got.Review != editing.ReviewPerm(review) || got.Automerge != editing.AutomergeNever || got.OwnerReview {
				t.Errorf("%s: %+v, want {open, perm:%s, never}", key, got, review)
			}
		}
	}
}

func TestPolicyOverridesNameOnlyCatalogPermissions(t *testing.T) {
	reg := editing.NewRegistry()
	if err := editspec.RegisterAll(reg, testDB); err != nil {
		t.Fatalf("register: %v", err)
	}
	unknown := editing.ReviewPerm("edit.catalog.nothing")
	if err := reg.ValidateOverride(editing.PolicyOverride{EntityType: editspec.TypeWork, Review: &unknown}); err == nil {
		t.Error("a review rule naming no catalog permission must be refused")
	}
	known := editing.ReviewPerm(string(perm.EditTaxonomyReview))
	if err := reg.ValidateOverride(editing.PolicyOverride{EntityType: editspec.TypeWork, Review: &known}); err != nil {
		t.Errorf("a catalog review permission must pass: %v", err)
	}
}
