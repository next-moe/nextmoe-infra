package permissions_test

import (
	"strings"
	"testing"

	"api/internal/platform/authz"
	"api/internal/platform/permissions"
)

const (
	tAlpha  authz.Permission = "demo.alpha"
	tBeta   authz.Permission = "demo.beta"
	tGamma  authz.Permission = "demo.gamma"
	tDelta  authz.Permission = "demo.delta"
	tLocked authz.Permission = "demo.locked"
)

type testRig struct {
	reg    *permissions.Registry
	holder *authz.Holder
	base   authz.Bundles
	own    map[authz.Permission]bool
	val    *permissions.Validator
}

func newRig() *testRig {
	base := authz.Bundles{
		"moderator": {tDelta},
		"admin":     {tAlpha, tDelta},
		"ren":       {tAlpha, tBeta, tLocked, tDelta},
	}
	holder := authz.NewHolder(base)
	reg := permissions.NewRegistry(permissions.Domain{
		Name:         "demo",
		TitleZH:      "演示",
		Bundles:      base,
		Holder:       holder,
		NonDelegable: authz.NonDelegable{tLocked: true},
		Keys: []permissions.Key{
			{Permission: tAlpha, DescEN: "alpha", DescZH: "alpha"},
			{Permission: tBeta, DescEN: "beta", DescZH: "beta"},
			{Permission: tGamma, DescEN: "gamma", DescZH: "gamma"},
			{Permission: tDelta, DescEN: "delta", DescZH: "delta"},
			{Permission: tLocked, DescEN: "locked", DescZH: "locked"},
		},
	})
	return &testRig{
		reg: reg, holder: holder, base: base,
		own: map[authz.Permission]bool{
			tAlpha: true, tBeta: true, tGamma: true, tDelta: true, tLocked: true,
		},
		val: permissions.NewValidator(reg),
	}
}

type rows map[string][]authz.Permission

func (r *testRig) apply(grants, denies rows) permissions.OverlayState {
	snap := permissions.NewSnapshot()
	st := permissions.OverlayState{}
	for effect, m := range map[string]rows{
		permissions.EffectGrant: grants,
		permissions.EffectDeny:  denies,
	} {
		for role, perms := range m {
			for _, p := range perms {
				snap.Add(role, p, effect)
				st.Set(role, p, effect)
			}
		}
	}
	r.holder.Swap(authz.NewResolver(permissions.Merge(r.base, snap, r.own)))
	return st
}

func (r *testRig) clean() permissions.OverlayState { return r.apply(nil, nil) }

func act(op permissions.Op, role string, p authz.Permission) permissions.Action {
	return permissions.Action{Op: op, Role: role, Permission: p}
}

func grant(role string, p authz.Permission) permissions.Action {
	return act(permissions.OpGrant, role, p)
}

func revoke(role string, p authz.Permission) permissions.Action {
	return act(permissions.OpRevokeGrant, role, p)
}

func deny(role string, p authz.Permission) permissions.Action {
	return act(permissions.OpDeny, role, p)
}

func restore(role string, p authz.Permission) permissions.Action {
	return act(permissions.OpRevokeDeny, role, p)
}

var (
	renCaller = permissions.Caller{UserID: 1, Roles: []string{"admin", "ren"}}
	adminUser = permissions.Caller{UserID: 2, Roles: []string{"admin"}}
	modCaller = permissions.Caller{UserID: 3, Roles: []string{"moderator"}}
)

func TestRule1RejectsImmutableAndUnknownRoles(t *testing.T) {
	rig := newRig()
	st := rig.clean()

	cases := []struct {
		name string
		role string
		want string
	}{
		{"user rows are immutable", "user", "roles 为空数组"},
		{"ren rows are immutable", "ren", "上界"},
		{"unknown role", "ghost", "未知角色"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := rig.val.Validate(renCaller, st, grant(c.role, tBeta))
			assertRejected(t, err, c.want)
		})
	}
}

func TestRule1RejectsUnknownPermission(t *testing.T) {
	rig := newRig()
	st := rig.clean()
	err := rig.val.Validate(renCaller, st, grant("moderator", authz.Permission("demo.nonexistent")))
	assertRejected(t, err, "未知权限键")
}

func TestRule3NonDelegableIsRefusedEvenForRen(t *testing.T) {
	rig := newRig()
	st := rig.clean()
	err := rig.val.Validate(renCaller, st, grant("admin", tLocked))
	assertRejected(t, err, "不可委派")
}

func TestRule2TargetMustBeStrictlyBelowCaller(t *testing.T) {
	rig := newRig()
	st := rig.clean()

	assertRejected(t, rig.val.Validate(adminUser, st, grant("admin", tBeta)), "严格低于")
	assertRejected(t, rig.val.Validate(modCaller, st, grant("moderator", tAlpha)), "严格低于")
}

func TestRule2CreatorRowIsRenOnly(t *testing.T) {
	rig := newRig()
	st := rig.clean()

	assertRejected(t, rig.val.Validate(adminUser, st, grant("creator", tAlpha)), "仅 ren 可编辑")

	if err := rig.val.Validate(renCaller, st, grant("creator", tAlpha)); err != nil {
		t.Errorf("ren must be able to grant on the creator row: %v", err)
	}
}

func TestRule2EditorRowIsAdminEditable(t *testing.T) {
	rig := newRig()
	st := rig.clean()

	if err := rig.val.Validate(adminUser, st, grant("editor", tAlpha)); err != nil {
		t.Errorf("an admin holding the permission must be able to grant it on the editor row: %v", err)
	}
	assertRejected(t, rig.val.Validate(adminUser, st, grant("editor", tBeta)), "自己都不持有")
}

func TestRule2CallerMustHoldThePermission(t *testing.T) {
	rig := newRig()
	st := rig.clean()

	assertRejected(t, rig.val.Validate(adminUser, st, grant("moderator", tBeta)), "自己都不持有")

	if err := rig.val.Validate(adminUser, st, grant("moderator", tAlpha)); err != nil {
		t.Errorf("admin delegating a key it holds, downward, must pass: %v", err)
	}
}

func TestRule2IsLiftedByPermissionsManage(t *testing.T) {
	rig := newRig()
	st := rig.apply(rows{"admin": {tBeta}}, nil)

	if err := rig.val.Validate(renCaller, st, grant("moderator", tBeta)); err != nil {
		t.Errorf("permissions.manage must lift the delegation rule: %v", err)
	}
}

func TestGrantingAnAlreadyEffectiveKeyIsRefused(t *testing.T) {
	rig := newRig()
	st := rig.clean()
	assertRejected(t, rig.val.Validate(renCaller, st, grant("admin", tAlpha)), "无需重复授予")
}

func TestRevokingACodeFloorGrantPointsAtTheDenyOperation(t *testing.T) {
	rig := newRig()
	st := rig.clean()
	err := rig.val.Validate(renCaller, st, revoke("admin", tAlpha))
	assertRejected(t, err, "来自代码捆")
	assertRejected(t, err, "撤销(记 deny)")
}

func TestRevokingAnOverlayGrantIsAllowed(t *testing.T) {
	rig := newRig()
	st := rig.apply(rows{"admin": {tBeta}}, nil)
	if err := rig.val.Validate(renCaller, st, revoke("admin", tBeta)); err != nil {
		t.Errorf("revoking an overlay grant must pass: %v", err)
	}
}

func TestRule4GrantRejectedWhenAdminLacksTheKey(t *testing.T) {
	rig := newRig()
	st := rig.clean()
	err := rig.val.Validate(renCaller, st, grant("moderator", tBeta))
	assertRejected(t, err, "请先把")
	assertRejected(t, err, "授予 admin")
}

func TestRule4GrantRejectedWhenRenLacksTheKey(t *testing.T) {
	rig := newRig()
	st := rig.clean()
	assertRejected(t, rig.val.Validate(renCaller, st, grant("admin", tGamma)), "ren 的代码捆不含")
}

func TestRule4RevokeRejectedWhenModeratorStillHoldsTheKey(t *testing.T) {
	rig := newRig()
	st := rig.apply(rows{"admin": {tBeta}, "moderator": {tBeta}}, nil)
	err := rig.val.Validate(renCaller, st, revoke("admin", tBeta))
	assertRejected(t, err, "请先撤销 moderator")
}

func TestRule4AllowsTheOrderedSequence(t *testing.T) {
	rig := newRig()

	st := rig.clean()
	if err := rig.val.Validate(renCaller, st, grant("admin", tBeta)); err != nil {
		t.Fatalf("granting admin first must pass: %v", err)
	}

	st = rig.apply(rows{"admin": {tBeta}}, nil)
	if err := rig.val.Validate(renCaller, st, grant("moderator", tBeta)); err != nil {
		t.Fatalf("granting moderator after admin must pass: %v", err)
	}

	st = rig.apply(rows{"admin": {tBeta}, "moderator": {tBeta}}, nil)
	if err := rig.val.Validate(renCaller, st, revoke("moderator", tBeta)); err != nil {
		t.Fatalf("revoking moderator first must pass: %v", err)
	}
	st = rig.apply(rows{"admin": {tBeta}}, nil)
	if err := rig.val.Validate(renCaller, st, revoke("admin", tBeta)); err != nil {
		t.Fatalf("revoking admin after moderator must pass: %v", err)
	}
}

func TestDenyIsRefusedOnTheImmutableRows(t *testing.T) {
	rig := newRig()
	st := rig.clean()

	assertRejected(t, rig.val.Validate(renCaller, st, deny("ren", tAlpha)), "ren 行不可编辑")
	assertRejected(t, rig.val.Validate(renCaller, st, deny("user", tAlpha)), "user 行不可编辑")
}

func TestDenyRequiresALiveKey(t *testing.T) {
	rig := newRig()
	st := rig.clean()
	err := rig.val.Validate(renCaller, st, deny("admin", authz.Permission("demo.nonexistent")))
	assertRejected(t, err, "未知权限键")
}

func TestDenyTargetMustBeStrictlyBelowCaller(t *testing.T) {
	rig := newRig()
	st := rig.clean()

	assertRejected(t, rig.val.Validate(adminUser, st, deny("admin", tAlpha)), "严格低于")
	assertRejected(t, rig.val.Validate(modCaller, st, deny("moderator", tDelta)), "严格低于")

	if err := rig.val.Validate(renCaller, st, deny("admin", tAlpha)); err != nil {
		t.Errorf("ren must be able to deny the admin row: %v", err)
	}
}

func TestDenyOnTheCreatorRowIsRenOnly(t *testing.T) {
	rig := newRig()
	st := rig.clean()
	assertRejected(t, rig.val.Validate(adminUser, st, deny("creator", tAlpha)), "仅 ren 可编辑")
}

func TestAdminMayDenyDownward(t *testing.T) {
	rig := newRig()
	st := rig.clean()
	if err := rig.val.Validate(adminUser, st, deny("moderator", tDelta)); err != nil {
		t.Errorf("an admin must be able to deny a key it holds, downward: %v", err)
	}
}

func TestDenyIsRefusedWhenTheKeyIsNotEffective(t *testing.T) {
	rig := newRig()
	st := rig.clean()
	assertRejected(t, rig.val.Validate(renCaller, st, deny("moderator", tAlpha)), "没有可撤销的权限")
}

func TestDenyOnANonDelegableKeyIsRefused(t *testing.T) {
	rig := newRig()
	st := rig.clean()
	assertRejected(t, rig.val.Validate(renCaller, st, deny("admin", tLocked)), "没有可撤销的权限")
}

func TestDenyIsRefusedWhenTheKeyComesFromAGrantRow(t *testing.T) {
	rig := newRig()
	st := rig.apply(rows{"moderator": {tAlpha}}, nil)
	err := rig.val.Validate(renCaller, st, deny("moderator", tAlpha))
	assertRejected(t, err, "来自一条叠加授权")
	assertRejected(t, err, "删除该行")
}

func TestDenyingATwiceDeniedCellIsRefused(t *testing.T) {
	rig := newRig()
	st := rig.apply(nil, rows{"moderator": {tDelta}})
	assertRejected(t, rig.val.Validate(renCaller, st, deny("moderator", tDelta)), "已经是「已撤销」状态")
}

func TestGrantingOnTopOfADenyIsRefused(t *testing.T) {
	rig := newRig()
	st := rig.apply(nil, rows{"moderator": {tDelta}})
	assertRejected(t, rig.val.Validate(renCaller, st, grant("moderator", tDelta)), "请先恢复")
}

func TestRestoringWithoutADenyRowIsRefused(t *testing.T) {
	rig := newRig()
	st := rig.clean()
	assertRejected(t, rig.val.Validate(renCaller, st, restore("moderator", tDelta)), "没有可恢复的撤销记录")
}

func TestRestoringADeniedCellIsAllowed(t *testing.T) {
	rig := newRig()
	st := rig.apply(nil, rows{"moderator": {tDelta}})
	if err := rig.val.Validate(renCaller, st, restore("moderator", tDelta)); err != nil {
		t.Errorf("restoring a denied cell must pass: %v", err)
	}
}

func TestDenyRejectedWhenModeratorWouldOutrankAdmin(t *testing.T) {
	rig := newRig()
	st := rig.clean()
	err := rig.val.Validate(renCaller, st, deny("admin", tDelta))
	assertRejected(t, err, "moderator ⊆ admin")
	assertRejected(t, err, "请先撤销 moderator")
	assertRejected(t, err, string(tDelta))
}

func TestDenyAllowsTheOrderedSequence(t *testing.T) {
	rig := newRig()

	st := rig.clean()
	if err := rig.val.Validate(renCaller, st, deny("moderator", tDelta)); err != nil {
		t.Fatalf("denying moderator first must pass: %v", err)
	}

	st = rig.apply(nil, rows{"moderator": {tDelta}})
	if err := rig.val.Validate(renCaller, st, deny("admin", tDelta)); err != nil {
		t.Fatalf("denying admin after moderator must pass: %v", err)
	}

	st = rig.apply(nil, rows{"moderator": {tDelta}, "admin": {tDelta}})
	if err := rig.val.Validate(renCaller, st, restore("admin", tDelta)); err != nil {
		t.Fatalf("restoring admin first must pass: %v", err)
	}
	err := rig.val.Validate(renCaller, st, restore("moderator", tDelta))
	assertRejected(t, err, "moderator ⊆ admin")
	assertRejected(t, err, "请先恢复")
}

func TestRestoringRenIsUnreachableEvenWithARow(t *testing.T) {
	rig := newRig()
	st := rig.apply(nil, rows{"ren": {tAlpha}})

	if !rig.reg.Effective("ren", tAlpha) {
		t.Fatal("a deny row narrowed ren through Merge")
	}
	assertRejected(t, rig.val.Validate(renCaller, st, restore("ren", tAlpha)), "ren 行不可编辑")
}

func assertRejected(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a rejection mentioning %q, got nil", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Errorf("rejection %q does not mention %q", err.Error(), want)
	}
}
