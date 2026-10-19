package editing_test

import (
	"errors"
	"testing"

	"api/internal/platform/editing"
)

func TestPolicyEvaluationMatrix(t *testing.T) {
	anon := anonActor(1)
	trusted := trustedActor(2)
	editor := editorActor(3)
	reviewer := reviewerActor(4)

	proposeCases := []struct {
		rule                            string
		anon, trusted, editor, reviewer bool
	}{
		{editing.ProposeOpen, true, true, true, true},
		{editing.ProposeTrusted, false, true, false, false},
		{editing.ProposePerm(permPropose), false, false, true, true},
		{editing.ProposeLocked, false, false, false, false},
	}
	for _, c := range proposeCases {
		p := editing.Policy{Propose: c.rule, Review: editing.ReviewPerm(permReview), Automerge: editing.AutomergeNever}
		for actor, want := range map[*editing.PolicyContext]bool{
			&anon: c.anon, &trusted: c.trusted, &editor: c.editor, &reviewer: c.reviewer,
		} {
			if got := p.AllowsPropose(*actor); got != want {
				t.Errorf("propose %q for uid %d = %v, want %v", c.rule, actor.UserID, got, want)
			}
		}
	}

	automergeCases := []struct {
		rule          string
		anon, trusted bool
	}{
		{editing.AutomergeNever, false, false},
		{editing.AutomergeTrusted, false, true},
		{editing.AutomergeAlways, true, true},
	}
	for _, c := range automergeCases {
		p := editing.Policy{Propose: editing.ProposeOpen, Review: editing.ReviewPerm(permReview), Automerge: c.rule}
		if got := p.AllowsAutomerge(anon); got != c.anon {
			t.Errorf("automerge %q for anon = %v, want %v", c.rule, got, c.anon)
		}
		if got := p.AllowsAutomerge(trusted); got != c.trusted {
			t.Errorf("automerge %q for trusted = %v, want %v", c.rule, got, c.trusted)
		}
	}

	either := editing.Policy{Propose: editing.ProposeOpen, Review: editing.ReviewPerm(permReview), Automerge: editing.AutomergeTrustedOrReview}
	if either.AllowsAutomerge(anon) {
		t.Error("automerge=trusted_or_review must reject a caller who is neither")
	}
	if !either.AllowsAutomerge(trusted) || !either.AllowsAutomerge(reviewer) {
		t.Error("automerge=trusted_or_review must accept the trusted tier and a review-perm holder alike")
	}
	cappedTrusted := trusted
	cappedTrusted.ModerationCapped = true
	if either.AllowsAutomerge(cappedTrusted) {
		t.Error("automerge=trusted_or_review must not land a capped caller's write")
	}

	reviewMerge := editing.Policy{Propose: editing.ProposeOpen, Review: editing.ReviewPerm(permReview), Automerge: editing.AutomergeReview}
	if reviewMerge.AllowsAutomerge(anon) || reviewMerge.AllowsAutomerge(trusted) {
		t.Error("automerge=review must reject a non-reviewer")
	}
	if !reviewMerge.AllowsAutomerge(reviewer) {
		t.Error("automerge=review must accept a review-perm holder")
	}
	owner := anonActor(5)
	owner.IsEntityOwner = true
	if reviewMerge.AllowsAutomerge(owner) {
		t.Error("automerge=review WITHOUT OwnerReview must reject a bare owner")
	}
	ownerMerge := editing.Policy{Propose: editing.ProposeOpen, Review: editing.ReviewPerm(permReview), Automerge: editing.AutomergeReview, OwnerReview: true}
	if !ownerMerge.AllowsAutomerge(owner) {
		t.Error("automerge=review + OwnerReview must accept the asserted owner")
	}

	rp := editing.Policy{Propose: editing.ProposeOpen, Review: editing.ReviewPerm(permReview), Automerge: editing.AutomergeNever}
	if rp.AllowsReview(editor) {
		t.Error("review must require the review perm")
	}
	if !rp.AllowsReview(reviewer) {
		t.Error("reviewer must pass the review rule")
	}
	if rp.AllowsReview(editing.PolicyContext{UserID: 9}) {
		t.Error("nil HasPerm must grant nothing")
	}
}

func TestRegistryValidation(t *testing.T) {
	base := func() editing.EntityTypeSpec { return widgetSpec(testDB) }

	cases := []struct {
		name   string
		mutate func(*editing.EntityTypeSpec)
	}{
		{"type not family-prefixed", func(s *editing.EntityTypeSpec) { s.Type = "other.widget" }},
		{"missing closures", func(s *editing.EntityTypeSpec) { s.LoadSnapshot = nil }},
		{"no fields", func(s *editing.EntityTypeSpec) { s.Fields = nil }},
		{"uppercase key", func(s *editing.EntityTypeSpec) { s.Fields[0].Key = "test.widget.Name" }},
		{"key not type-prefixed", func(s *editing.EntityTypeSpec) { s.Fields[0].Key = "test.other.name" }},
		{"duplicate key", func(s *editing.EntityTypeSpec) { s.Fields[1].Key = s.Fields[0].Key }},
		{"missing validate", func(s *editing.EntityTypeSpec) { s.Fields[0].Validate = nil }},
		{"bad propose rule", func(s *editing.EntityTypeSpec) { s.DefaultPolicy.Propose = "sometimes" }},
		{"bare perm propose", func(s *editing.EntityTypeSpec) { s.DefaultPolicy.Propose = "perm:" }},
		{"bad review rule", func(s *editing.EntityTypeSpec) { s.DefaultPolicy.Review = "open" }},
		{"bad automerge rule", func(s *editing.EntityTypeSpec) { s.DefaultPolicy.Automerge = "maybe" }},
	}
	for _, c := range cases {
		spec := base()
		c.mutate(&spec)
		if err := editing.NewRegistry().Register(spec); err == nil {
			t.Errorf("%s: registration must fail", c.name)
		}
	}

	reg := editing.NewRegistry()
	if err := reg.Register(base()); err != nil {
		t.Fatalf("valid spec: %v", err)
	}
	if err := reg.Register(base()); err == nil {
		t.Error("duplicate type must fail")
	}
}

func TestPolicyOverride(t *testing.T) {
	e := newEngine(t)
	createWidget(t, 1)

	var permErr *editing.PermissionError
	if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1,
		Patch: map[string]any{fName: "y"}, Actor: anonActor(300),
	}); !errors.As(err, &permErr) {
		t.Fatalf("code policy must perm-gate name: %v", err)
	}

	applyOverrides(t, e, lockOpenOpenName())

	var lockedErr *editing.LockedFieldError
	if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1,
		Patch: map[string]any{fOpen: "x"}, Actor: anonActor(300),
	}); !errors.As(err, &lockedErr) {
		t.Fatalf("override-locked field: %v", err)
	}

	_, rev, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1,
		Patch: map[string]any{fName: "override direct"}, Actor: anonActor(300),
	})
	if err != nil {
		t.Fatalf("override-open field: %v", err)
	}
	if rev == nil || rev.Action != editing.ActionDirect {
		t.Fatal("override must grant open+always on name")
	}
	if w := loadWidget(t, 1); w.Name != "override direct" {
		t.Fatalf("apply: %+v", w)
	}

	applyOverrides(t, e, nil)
	if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1,
		Patch: map[string]any{fName: "z"}, Actor: anonActor(300),
	}); !errors.As(err, &permErr) {
		t.Fatalf("clearing the overrides must restore the code policy: %v", err)
	}
}

func TestPolicyOverrideLayers(t *testing.T) {
	e := newEngine(t)
	spec, _ := e.Registry().Type("test.widget")

	applyOverrides(t, e, []editing.PolicyOverride{
		{EntityType: "test.widget", Automerge: strp(editing.AutomergeTrustedOrReview), OwnerReview: boolp(true)},
		{EntityType: "test.widget", FieldKey: fName, Automerge: strp(editing.AutomergeNever)},
	})
	if got := spec.EffectivePolicy(fOLang); got.Automerge != editing.AutomergeTrustedOrReview || !got.OwnerReview ||
		got.Propose != editing.ProposePerm(permPropose) {
		t.Errorf("type-level override must layer over the code default: %+v", got)
	}
	if got := spec.EffectivePolicy(fName); got.Automerge != editing.AutomergeNever || !got.OwnerReview {
		t.Errorf("field override must win over the type-level one for the attributes it sets: %+v", got)
	}
	if got := spec.CodePolicy(fName); got.Automerge != editing.AutomergeNever || got.OwnerReview {
		t.Errorf("CodePolicy must ignore overrides: %+v", got)
	}

	var typeRow, nameRow *editing.PolicyMatrixRow
	matrix := e.Registry().PolicyMatrix()
	for i := range matrix {
		switch {
		case matrix[i].EntityType == "test.widget" && matrix[i].FieldKey == "":
			typeRow = &matrix[i]
		case matrix[i].FieldKey == fName:
			nameRow = &matrix[i]
		}
	}
	if typeRow == nil || typeRow.Override == nil || typeRow.Effective.Automerge != editing.AutomergeTrustedOrReview {
		t.Fatalf("type-level matrix row: %+v", typeRow)
	}
	if nameRow == nil || nameRow.Override == nil || nameRow.Effective.Automerge != editing.AutomergeNever {
		t.Fatalf("field matrix row: %+v", nameRow)
	}
}

func TestValidateOverride(t *testing.T) {
	reg := editing.NewRegistry()
	if err := reg.Register(widgetSpec(testDB)); err != nil {
		t.Fatal(err)
	}
	reg.SetPermissionValidator(func(key string) bool { return key == permPropose || key == permReview })
	bad := []editing.PolicyOverride{
		{EntityType: "test.ghost", Propose: strp(editing.ProposeOpen)},
		{EntityType: "test.widget", FieldKey: "test.widget.ghost", Propose: strp(editing.ProposeOpen)},
		{EntityType: "test.widget"},
		{EntityType: "test.widget", Propose: strp("sometimes")},
		{EntityType: "test.widget", Propose: strp("perm:")},
		{EntityType: "test.widget", Propose: strp("perm:nobody.knows")},
		{EntityType: "test.widget", Review: strp(editing.ProposeOpen)},
		{EntityType: "test.widget", Automerge: strp("maybe")},
		{EntityType: "test.widget", Automerge: strp(editing.AutomergeOwner)},
	}
	for _, o := range bad {
		if err := reg.ValidateOverride(o); err == nil {
			t.Errorf("override %+v must be refused", o)
		}
	}
	if err := reg.ValidateOverride(editing.PolicyOverride{
		EntityType: "test.widget", FieldKey: fName, Review: strp(editing.ReviewPerm(permReview)),
	}); err != nil {
		t.Errorf("a known review permission must pass: %v", err)
	}

	skipped := reg.SetPolicyOverrides([]editing.PolicyOverride{
		bad[1],
		{EntityType: "test.widget", FieldKey: fName, Propose: strp(editing.ProposeLocked)},
	})
	if len(skipped) != 1 {
		t.Fatalf("one orphaned row must be skipped, not the table: %v", skipped)
	}
	spec, _ := reg.Type("test.widget")
	if spec.EffectivePolicy(fName).Propose != editing.ProposeLocked {
		t.Error("the valid row beside an orphan must still install")
	}
}

func TestSchemaProjection(t *testing.T) {
	e := newEngine(t)

	find := func(projs []editing.FieldProjection, key string) editing.FieldProjection {
		t.Helper()
		for _, p := range projs {
			if p.Key == key {
				return p
			}
		}
		t.Fatalf("projection missing %s", key)
		return editing.FieldProjection{}
	}

	anonProj, err := e.SchemaProjection(testCtx, "test.widget", 0, anonActor(1))
	if err != nil {
		t.Fatal(err)
	}
	if p := find(anonProj, fName); p.CanPropose || p.CanReview || p.Locked {
		t.Errorf("anon on name: %+v", p)
	}
	if p := find(anonProj, fOpen); !p.CanPropose || !p.WouldAutomerge {
		t.Errorf("anon on open_note: %+v", p)
	}
	if p := find(anonProj, fTrusted); !p.CanPropose || p.WouldAutomerge {
		t.Errorf("anon on trusted_note: %+v", p)
	}
	if p := find(anonProj, fLocked); !p.Locked || p.CanPropose {
		t.Errorf("anon on locked_code: %+v", p)
	}
	if p := find(anonProj, fLegacy); !p.Deprecated || p.CanPropose {
		t.Errorf("anon on legacy: %+v", p)
	}

	revProj, err := e.SchemaProjection(testCtx, "test.widget", 0, reviewerActor(2))
	if err != nil {
		t.Fatal(err)
	}
	if p := find(revProj, fName); !p.CanPropose || !p.CanReview || p.WouldAutomerge {
		t.Errorf("reviewer on name: %+v", p)
	}

	trustedProj, err := e.SchemaProjection(testCtx, "test.widget", 0, trustedActor(3))
	if err != nil {
		t.Fatal(err)
	}
	if p := find(trustedProj, fTrusted); !p.WouldAutomerge {
		t.Errorf("trusted on trusted_note: %+v", p)
	}

	applyOverrides(t, e, lockOpenOpenName())
	overrideProj, err := e.SchemaProjection(testCtx, "test.widget", 0, anonActor(4))
	if err != nil {
		t.Fatal(err)
	}
	if p := find(overrideProj, fOpen); !p.Locked {
		t.Errorf("override on open_note: %+v", p)
	}
	if p := find(overrideProj, fName); !p.CanPropose || !p.WouldAutomerge {
		t.Errorf("override on name: %+v", p)
	}

	if _, err := e.SchemaProjection(testCtx, "test.ghost", 0, anonActor(1)); !errors.Is(err, editing.ErrUnknownEntityType) {
		t.Fatalf("unknown type: %v", err)
	}
}

func TestListAndDiff(t *testing.T) {
	e := newEngine(t)
	createWidget(t, 1)

	for i, note := range []string{"one", "two"} {
		if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: "test.widget", EntityID: 1,
			Patch: map[string]any{fOpen: note}, Actor: anonActor(int64(300 + i)),
		}); err != nil {
			t.Fatal(err)
		}
	}

	revs, err := e.ListRevisions(testCtx, "test.widget", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 2 || revs[0].Seq != 2 || revs[1].Seq != 1 {
		t.Fatalf("revisions newest-first: %+v", revs)
	}

	diffs, err := e.Diff(testCtx, "test.widget", 1, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 {
		t.Fatalf("diffs: %+v", diffs)
	}
	d := diffs[0]
	if d.Key != fOpen || d.From != "one" || d.To != "two" || d.DiffHint != editing.DiffHintLines {
		t.Fatalf("diff: %+v", d)
	}

	diffs, err = e.Diff(testCtx, "test.widget", 1, 2, 2)
	if err != nil || len(diffs) != 0 {
		t.Fatalf("self diff: %v %v", diffs, err)
	}
	if _, err := e.Diff(testCtx, "test.widget", 1, 1, 42); !errors.Is(err, editing.ErrRevisionNotFound) {
		t.Fatalf("missing revision: %v", err)
	}

	props, err := e.ListProposals(testCtx, editing.ProposalFilter{
		EntityType: "test.widget", EntityID: 1, Status: editing.StatusMerged,
	})
	if err != nil || len(props) != 2 {
		t.Fatalf("merged proposals: %d err=%v", len(props), err)
	}
	props, err = e.ListProposals(testCtx, editing.ProposalFilter{Status: editing.StatusOpen})
	if err != nil || len(props) != 0 {
		t.Fatalf("open proposals: %d err=%v", len(props), err)
	}
	props, err = e.ListProposals(testCtx, editing.ProposalFilter{Site: "test-site", Status: -1})
	if err != nil || len(props) != 2 {
		t.Fatalf("site proposals: %d err=%v", len(props), err)
	}
}
