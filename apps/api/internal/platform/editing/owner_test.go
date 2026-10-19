package editing_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"api/internal/platform/editing"
)

func ownedSpec(ownerOf map[int64]*string, ownerErr error) editing.EntityTypeSpec {
	return editing.EntityTypeSpec{
		Family: "test",
		Type:   "test.owned",
		LoadSnapshot: func(ctx context.Context, id int64) (map[string]any, error) {
			var w widgetRow
			if err := testDB.WithContext(ctx).First(&w, id).Error; err != nil {
				return nil, editing.ErrEntityNotFound
			}
			return map[string]any{"test.owned.name": w.Name}, nil
		},
		OwnerSite: func(ctx context.Context, id int64) (*string, error) {
			if ownerErr != nil {
				return nil, ownerErr
			}
			return ownerOf[id], nil
		},
		DefaultPolicy: editing.Policy{
			Propose:   editing.ProposeOpen,
			Review:    editing.ReviewPerm(permReview),
			Automerge: editing.AutomergeOwner,
		},
		Fields: []editing.FieldSpec{
			{Key: "test.owned.name", Kind: editing.KindText, DiffHint: editing.DiffHintInline,
				Validate: stringValidator, Apply: widgetApply("name")},
		},
	}
}

func TestOwnerAutomerge(t *testing.T) {
	siteA := "site-a"
	ownerOf := map[int64]*string{
		1: &siteA,
		2: nil,
	}

	newOwnedEngine := func(t *testing.T, ownerErr error) *editing.Engine {
		t.Helper()
		cleanTables(t)
		createWidget(t, 1)
		createWidget(t, 2)
		spec := ownedSpec(ownerOf, ownerErr)
		reg := editing.NewRegistry()
		if err := reg.Register(spec); err != nil {
			t.Fatalf("register owned spec: %v", err)
		}
		return editing.NewEngine(testDB, reg)
	}

	actorOn := func(site string) editing.PolicyContext {
		pc := anonActor(700)
		pc.Site = site
		return pc
	}

	t.Run("owner hit direct-merges", func(t *testing.T) {
		e := newOwnedEngine(t, nil)
		prop, rev, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: "test.owned", EntityID: 1,
			Patch: map[string]any{"test.owned.name": "owned edit"}, Actor: actorOn("site-a"),
		})
		if err != nil {
			t.Fatalf("owner create: %v", err)
		}
		if rev == nil || rev.Action != editing.ActionDirect {
			t.Fatalf("owner-site proposal must automerge: rev=%+v", rev)
		}
		if prop.Status != editing.StatusMerged {
			t.Fatalf("proposal status: %d", prop.Status)
		}
		if w := loadWidget(t, 1); w.Name != "owned edit" {
			t.Fatalf("apply: %+v", w)
		}
	})

	t.Run("owner miss stays open", func(t *testing.T) {
		e := newOwnedEngine(t, nil)
		prop, rev, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: "test.owned", EntityID: 1,
			Patch: map[string]any{"test.owned.name": "other site"}, Actor: actorOn("site-b"),
		})
		if err != nil {
			t.Fatalf("other-site create: %v", err)
		}
		if rev != nil || prop.Status != editing.StatusOpen {
			t.Fatalf("other-site proposal must stay open: rev=%v status=%d", rev, prop.Status)
		}
		if w := loadWidget(t, 1); w.Name != "" {
			t.Fatalf("open proposal must not apply: %+v", w)
		}
	})

	t.Run("unclaimed entity stays open even for the caller site", func(t *testing.T) {
		e := newOwnedEngine(t, nil)
		prop, rev, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: "test.owned", EntityID: 2,
			Patch: map[string]any{"test.owned.name": "unclaimed"}, Actor: actorOn("site-a"),
		})
		if err != nil {
			t.Fatalf("unclaimed create: %v", err)
		}
		if rev != nil || prop.Status != editing.StatusOpen {
			t.Fatalf("unclaimed proposal must stay open: rev=%v status=%d", rev, prop.Status)
		}
	})

	t.Run("hook error fails the create", func(t *testing.T) {
		e := newOwnedEngine(t, errors.New("owner lookup boom"))
		_, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: "test.owned", EntityID: 1,
			Patch: map[string]any{"test.owned.name": "x"}, Actor: actorOn("site-a"),
		})
		if err == nil || !strings.Contains(err.Error(), "owner lookup boom") {
			t.Fatalf("hook error must fail the create, got %v", err)
		}
		var count int64
		if err := testDB.Table("edit_proposal").Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("proposal rows after hook error: %d err=%v", count, err)
		}
	})

	t.Run("schema projection is entity-aware", func(t *testing.T) {
		e := newOwnedEngine(t, nil)
		find := func(projs []editing.FieldProjection) editing.FieldProjection {
			for _, p := range projs {
				if p.Key == "test.owned.name" {
					return p
				}
			}
			t.Fatal("projection missing test.owned.name")
			return editing.FieldProjection{}
		}
		projs, err := e.SchemaProjection(testCtx, "test.owned", 1, actorOn("site-a"))
		if err != nil {
			t.Fatal(err)
		}
		if p := find(projs); !p.CanPropose || !p.WouldAutomerge {
			t.Errorf("owner on owned entity: %+v", p)
		}
		projs, err = e.SchemaProjection(testCtx, "test.owned", 1, actorOn("site-b"))
		if err != nil {
			t.Fatal(err)
		}
		if p := find(projs); !p.CanPropose || p.WouldAutomerge {
			t.Errorf("other site on owned entity: %+v", p)
		}
		projs, err = e.SchemaProjection(testCtx, "test.owned", 2, actorOn("site-a"))
		if err != nil {
			t.Fatal(err)
		}
		if p := find(projs); p.WouldAutomerge {
			t.Errorf("unclaimed entity: %+v", p)
		}
		projs, err = e.SchemaProjection(testCtx, "test.owned", 0, actorOn("site-a"))
		if err != nil {
			t.Fatal(err)
		}
		if p := find(projs); p.WouldAutomerge {
			t.Errorf("type-level projection: %+v", p)
		}
		eErr := newOwnedEngine(t, fmt.Errorf("projection boom"))
		if _, err := eErr.SchemaProjection(testCtx, "test.owned", 1, actorOn("site-a")); err == nil {
			t.Error("hook error must fail the entity-aware projection")
		}
	})
}

func TestOwnerRuleRequiresHook(t *testing.T) {
	ownerPol := editing.Policy{
		Propose:   editing.ProposeOpen,
		Review:    editing.ReviewPerm(permReview),
		Automerge: editing.AutomergeOwner,
	}

	base := func() editing.EntityTypeSpec {
		spec := widgetSpec(testDB)
		spec.OwnerSite = nil
		return spec
	}

	cases := []struct {
		name   string
		mutate func(*editing.EntityTypeSpec)
	}{
		{"default policy", func(s *editing.EntityTypeSpec) { s.DefaultPolicy = ownerPol }},
		{"field policy", func(s *editing.EntityTypeSpec) { s.Fields[0].Policy = &ownerPol }},
	}
	for _, c := range cases {
		spec := base()
		c.mutate(&spec)
		if err := editing.NewRegistry().Register(spec); err == nil ||
			!strings.Contains(err.Error(), "OwnerSite") {
			t.Errorf("%s: owner rule without hook must fail registration, got %v", c.name, err)
		}
	}

	reg := editing.NewRegistry()
	if err := reg.Register(base()); err != nil {
		t.Fatal(err)
	}
	owner := editing.AutomergeOwner
	if err := reg.ValidateOverride(editing.PolicyOverride{
		EntityType: "test.widget", FieldKey: fName, Automerge: &owner,
	}); err == nil || !strings.Contains(err.Error(), "owner hook") {
		t.Errorf("an automerge=owner override without a hook must be refused, got %v", err)
	}

	withHook := base()
	withHook.OwnerSite = func(ctx context.Context, id int64) (*string, error) { return nil, nil }
	withHook.DefaultPolicy = ownerPol
	if err := editing.NewRegistry().Register(withHook); err != nil {
		t.Errorf("owner rule with hook must register: %v", err)
	}
}
