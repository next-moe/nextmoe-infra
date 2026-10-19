package editspec_test

import (
	"errors"
	"testing"

	"api/internal/platform/authz"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/perm"
	"api/internal/platform/editing"
)

func kungalActor(uid int64, roles ...string) editing.PolicyContext {
	return editing.PolicyContext{
		UserID: uid, Site: "kungal", TrustTier: 0,
		HasPerm: func(key string) bool {
			return perm.Resolver.Can(roles, authz.Permission(key))
		},
	}
}

func TestKungalParity(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "初期名")

	prop, rev, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: work.ID,
		Patch: map[string]any{editspec.FieldWorkDisplayName: "用户提案标题"},
		Note:  "fix title",
		Actor: kungalActor(101),
	})
	if err != nil {
		t.Fatalf("plain-user propose: %v", err)
	}
	if rev != nil {
		t.Fatal("a plain user's kungal proposal must never automerge")
	}
	if prop.Status != editing.StatusOpen {
		t.Fatalf("status = %d, want open", prop.Status)
	}

	_, rev, err = e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: work.ID,
		Patch: map[string]any{editspec.FieldWorkOLang: "en"},
		Actor: kungalActor(102, "admin"),
	})
	if err != nil {
		t.Fatalf("admin direct edit: %v", err)
	}
	if rev == nil || rev.Action != editing.ActionDirect {
		t.Fatalf("admin (review perm) must direct-edit on kungal: %+v", rev)
	}
	// Production granted moderators the review keys through the permission
	// overlay all along; the code bundle now says so, so their edits land.
	_, rev, err = e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: work.ID,
		Patch: map[string]any{editspec.FieldWorkContentRating: float64(model.ContentRatingR18)},
		Actor: kungalActor(103, "moderator"),
	})
	if err != nil {
		t.Fatalf("moderator edit: %v", err)
	}
	if rev == nil || rev.Action != editing.ActionDirect {
		t.Fatalf("moderator (review perm in code) must direct-edit: %+v", rev)
	}

	var permErr *editing.PermissionError
	if _, err := e.MergeProposal(testCtx, prop.ID, kungalActor(104, "editor"), ""); !errors.As(err, &permErr) {
		t.Fatalf("editor merge: %v, want PermissionError", err)
	}

	if _, err := e.AmendProposal(testCtx, prop.ID, editing.AmendInput{
		Set:   map[string]any{editspec.FieldWorkDisplayName: "修正后标题"},
		Note:  "typo fixed in review",
		Actor: kungalActor(102, "admin"),
	}); err != nil {
		t.Fatalf("amend: %v", err)
	}
	merged, err := e.MergeProposal(testCtx, prop.ID, kungalActor(102, "admin"), "looks good")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if merged.ActorUID != 101 {
		t.Fatalf("revision actor = %d, want proposer 101", merged.ActorUID)
	}
	if merged.AmenderUID == nil || *merged.AmenderUID != 102 {
		t.Fatalf("revision amender = %v, want 102", merged.AmenderUID)
	}
	var after model.CatalogWork
	if err := testDB.First(&after, work.ID).Error; err != nil {
		t.Fatalf("reload work: %v", err)
	}
	if after.DisplayName != "修正后标题" {
		t.Fatalf("display_name = %q, want the amended value", after.DisplayName)
	}
}

func TestKungalOwnerReview(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "初期名")

	asOwner := func(pc editing.PolicyContext) editing.PolicyContext {
		pc.IsEntityOwner = true
		return pc
	}
	owner := asOwner(kungalActor(200))

	_, rev, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: work.ID,
		Patch: map[string]any{editspec.FieldWorkDisplayName: "创建者直接改名"},
		Actor: owner,
	})
	if err != nil {
		t.Fatalf("owner direct edit: %v", err)
	}
	if rev == nil || rev.Action != editing.ActionDirect {
		t.Fatalf("owner must direct-edit their game on kungal: %+v", rev)
	}
	if rev.ActorUID != 200 {
		t.Fatalf("owner direct-edit actor = %d, want the owner 200", rev.ActorUID)
	}

	prop, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: work.ID,
		Patch: map[string]any{editspec.FieldWorkOLang: "en"},
		Actor: kungalActor(101),
	})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	if _, err := e.AmendProposal(testCtx, prop.ID, editing.AmendInput{
		Set:   map[string]any{editspec.FieldWorkOLang: "zh"},
		Note:  "owner fix",
		Actor: owner,
	}); err != nil {
		t.Fatalf("owner amend: %v", err)
	}
	merged, err := e.MergeProposal(testCtx, prop.ID, owner, "")
	if err != nil {
		t.Fatalf("owner merge: %v", err)
	}
	if merged.ActorUID != 101 || merged.AmenderUID == nil || *merged.AmenderUID != 200 {
		t.Fatalf("double signature: actor=%d amender=%v", merged.ActorUID, merged.AmenderUID)
	}

	_, rev, err = e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: work.ID,
		Patch: map[string]any{editspec.FieldWorkDisplayName: "どこからでも"},
		Actor: asOwner(realActor(200, "user")),
	})
	if err != nil || rev == nil {
		t.Fatalf("the owner channel is the same on every site: rev=%v err=%v", rev, err)
	}
}

func TestTaxonomyPolicy(t *testing.T) {
	e := newTaxonomyEngine(t)
	tag := model.CatalogTag{Name: "泣きゲー", Tier: model.TagTierCore, Kind: model.TagKindContent}
	if err := testDB.Create(&tag).Error; err != nil {
		t.Fatal(err)
	}
	patch := map[string]any{editspec.FieldTagIntros: []any{
		map[string]any{"lang": "zh-Hans", "intro": "以催泪剧情为核心"},
	}}

	if _, rev, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeTag, EntityID: tag.ID,
		Patch: patch, Actor: realActor(101, "user"),
	}); err != nil || rev != nil {
		t.Fatalf("a plain user proposes taxonomy on any site and it queues: rev=%v err=%v", rev, err)
	}

	prop, rev, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeTag, EntityID: tag.ID,
		Patch: patch, Note: "add intro", Actor: kungalActor(101),
	})
	if err != nil {
		t.Fatalf("plain kungal user propose: %v", err)
	}
	if rev != nil {
		t.Fatal("a plain user's kungal taxonomy proposal must never automerge")
	}

	_, rev, err = e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeTag, EntityID: tag.ID,
		Patch: map[string]any{editspec.FieldTagIntros: []any{
			map[string]any{"lang": "ja", "intro": "泣ける"},
		}},
		Actor: kungalActor(102, "admin"),
	})
	if err != nil {
		t.Fatalf("admin propose: %v", err)
	}
	if rev != nil {
		t.Fatal("kungal taxonomy is automerge=never even for a review-perm holder")
	}

	var permErr *editing.PermissionError
	if _, err := e.MergeProposal(testCtx, prop.ID, kungalActor(104, "editor"), ""); !errors.As(err, &permErr) {
		t.Fatalf("editor merge: %v, want PermissionError", err)
	}
	if _, err := e.MergeProposal(testCtx, prop.ID, kungalActor(103, "moderator"), "ok"); err != nil {
		t.Fatalf("moderator merge: %v", err)
	}
	snap, err := e.CurrentSnapshot(testCtx, editspec.TypeTag, tag.ID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	sameJSON(t, "tag intros", snap[editspec.FieldTagIntros], patch[editspec.FieldTagIntros])
}
