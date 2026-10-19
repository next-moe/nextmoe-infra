package editing_test

import (
	"context"
	"errors"
	"testing"

	"api/internal/platform/editing"
)

const fOwned = "test.ownuser.name"

func ownUserSpec(entityType string, hook func(context.Context, int64) (*int64, error)) editing.EntityTypeSpec {
	return editing.EntityTypeSpec{
		Family: "test",
		Type:   entityType,
		LoadSnapshot: func(ctx context.Context, id int64) (map[string]any, error) {
			var w widgetRow
			if err := testDB.WithContext(ctx).First(&w, id).Error; err != nil {
				return nil, editing.ErrEntityNotFound
			}
			return map[string]any{entityType + ".name": w.Name}, nil
		},
		OwnerUserID: hook,
		DefaultPolicy: editing.Policy{
			Propose:     editing.ProposeOpen,
			Review:      editing.ReviewPerm(permReview),
			Automerge:   editing.AutomergeNever,
			OwnerReview: true,
		},
		Fields: []editing.FieldSpec{
			{Key: entityType + ".name", Kind: editing.KindText, DiffHint: editing.DiffHintInline,
				Validate: stringValidator, Apply: widgetApply("name")},
		},
	}
}

func deriveEngine(t *testing.T, owner *int64, hookErr error) *editing.Engine {
	t.Helper()
	cleanTables(t)
	reg := editing.NewRegistry()
	hook := func(ctx context.Context, id int64) (*int64, error) {
		if hookErr != nil {
			return nil, hookErr
		}
		return owner, nil
	}
	if err := reg.Register(ownUserSpec("test.ownuser", hook)); err != nil {
		t.Fatalf("register hooked spec: %v", err)
	}
	if err := reg.Register(ownUserSpec("test.noown", nil)); err != nil {
		t.Fatalf("register hookless spec: %v", err)
	}
	createWidget(t, 1)
	return editing.NewEngine(testDB, reg)
}

func fileProposal(t *testing.T, e *editing.Engine, entityType string, uid int64) *editing.Proposal {
	t.Helper()
	prop, rev, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: entityType, EntityID: 1,
		Patch: map[string]any{entityType + ".name": "proposed"}, Actor: anonActor(uid),
	})
	if err != nil {
		t.Fatalf("file proposal: %v", err)
	}
	if rev != nil {
		t.Fatalf("proposal must stay open, got an automerge")
	}
	return prop
}

func TestDeriveOwnershipFromTheSpecHook(t *testing.T) {
	uid := int64(900)

	t.Run("hook match promotes a plain actor to reviewer", func(t *testing.T) {
		e := deriveEngine(t, &uid, nil)
		prop := fileProposal(t, e, "test.ownuser", 901)
		if _, err := e.MergeProposal(testCtx, prop.ID, anonActor(uid), ""); err != nil {
			t.Fatalf("the derived owner must be allowed to merge: %v", err)
		}
		proj, err := e.SchemaProjection(testCtx, "test.ownuser", 1, anonActor(uid))
		if err != nil {
			t.Fatal(err)
		}
		if !proj[0].CanReview {
			t.Error("the derived owner must project can_review")
		}
		proj, err = e.SchemaProjection(testCtx, "test.ownuser", 0, anonActor(uid))
		if err != nil {
			t.Fatal(err)
		}
		if proj[0].CanReview {
			t.Error("a type-level projection must not derive ownership")
		}
	})

	t.Run("a different uid derives nothing", func(t *testing.T) {
		e := deriveEngine(t, &uid, nil)
		prop := fileProposal(t, e, "test.ownuser", 901)
		var permErr *editing.PermissionError
		if _, err := e.MergeProposal(testCtx, prop.ID, anonActor(uid+1), ""); !errors.As(err, &permErr) {
			t.Fatalf("a non-owner must be refused: %v", err)
		}
	})

	t.Run("an unowned entity derives nothing", func(t *testing.T) {
		e := deriveEngine(t, nil, nil)
		prop := fileProposal(t, e, "test.ownuser", 901)
		var permErr *editing.PermissionError
		if _, err := e.MergeProposal(testCtx, prop.ID, anonActor(uid), ""); !errors.As(err, &permErr) {
			t.Fatalf("a nil owner must grant nothing: %v", err)
		}
	})

	t.Run("no hook derives nothing", func(t *testing.T) {
		e := deriveEngine(t, &uid, nil)
		prop := fileProposal(t, e, "test.noown", 901)
		var permErr *editing.PermissionError
		if _, err := e.MergeProposal(testCtx, prop.ID, anonActor(uid), ""); !errors.As(err, &permErr) {
			t.Fatalf("a family without the hook must derive nothing: %v", err)
		}
	})

	t.Run("an asserted owner survives a hook that disagrees", func(t *testing.T) {
		e := deriveEngine(t, &uid, nil)
		prop := fileProposal(t, e, "test.ownuser", 901)
		asserted := anonActor(uid + 5)
		asserted.IsEntityOwner = true
		if _, err := e.MergeProposal(testCtx, prop.ID, asserted, ""); err != nil {
			t.Fatalf("an asserted owner must keep its flag: %v", err)
		}
	})

	t.Run("a hook failure fails the operation", func(t *testing.T) {
		e := deriveEngine(t, &uid, errors.New("boom"))
		_, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: "test.ownuser", EntityID: 1,
			Patch: map[string]any{fOwned: "proposed"}, Actor: anonActor(901),
		})
		var permErr *editing.PermissionError
		if err == nil || errors.As(err, &permErr) {
			t.Fatalf("a hook error must surface as an error: %v", err)
		}
	})
}
