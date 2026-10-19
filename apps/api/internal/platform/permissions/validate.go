package permissions

import (
	"fmt"

	"api/internal/platform/authz"
	sitePerm "api/internal/platform/site/perm"
)

var EditableRoles = []string{RoleEditor, RoleCreator, RoleModerator, RoleAdmin}

func isEditableRole(role string) bool {
	for _, r := range EditableRoles {
		if r == role {
			return true
		}
	}
	return false
}

type OverlayState map[string]string

func stateKey(role string, p authz.Permission) string { return role + "\x00" + string(p) }

func (o OverlayState) Effect(role string, p authz.Permission) string { return o[stateKey(role, p)] }

func (o OverlayState) HasGrant(role string, p authz.Permission) bool {
	return o.Effect(role, p) == EffectGrant
}

func (o OverlayState) HasDeny(role string, p authz.Permission) bool {
	return o.Effect(role, p) == EffectDeny
}

func (o OverlayState) Set(role string, p authz.Permission, effect string) {
	o[stateKey(role, p)] = effect
}

type Caller struct {
	UserID uint
	Roles  []string
}

type Op string

const (
	OpGrant       Op = "grant"
	OpRevokeGrant Op = "revoke_grant"
	OpDeny        Op = "deny"
	OpRevokeDeny  Op = "revoke_deny"
)

type Action struct {
	Op         Op
	Role       string
	Permission authz.Permission
}

func (a Action) writesRow() bool { return a.Op == OpGrant || a.Op == OpDeny }

func (a Action) effect() string {
	if a.Op == OpDeny || a.Op == OpRevokeDeny {
		return EffectDeny
	}
	return EffectGrant
}

type RejectionError struct{ Msg string }

func (e *RejectionError) Error() string { return e.Msg }

func reject(format string, args ...any) error {
	return &RejectionError{Msg: fmt.Sprintf(format, args...)}
}

type Validator struct{ reg *Registry }

func NewValidator(reg *Registry) *Validator { return &Validator{reg: reg} }

func ManagesPermissions(roles []string) bool {
	return sitePerm.Resolver.Can(roles, sitePerm.PermissionsManage)
}

func (v *Validator) Validate(caller Caller, state OverlayState, act Action) error {
	if !isEditableRole(act.Role) {
		if act.Role == RoleUser {
			return reject("user 行不可编辑:普通用户的 JWT roles 为空数组,授予或撤销它的权限都不会生效")
		}
		if act.Role == RoleRen {
			return reject("ren 行不可编辑:ren 既是包含性不变量的上界,也是锁死后的恢复保险,只能改代码捆")
		}
		return reject("未知角色 %q,只能编辑 editor / creator / moderator / admin", act.Role)
	}

	if _, ok := v.reg.Lookup(act.Permission); !ok {
		return reject("未知权限键 %q", act.Permission)
	}

	if act.Op == OpGrant && v.reg.IsNonDelegable(act.Permission) {
		return reject("%q 不可委派:此类权限只能通过改代码捆并部署来变更", act.Permission)
	}

	if !ManagesPermissions(caller.Roles) {
		if act.Role == RoleCreator {
			return reject("creator 行仅 ren 可编辑")
		}
		if HighestRank([]string{act.Role}) >= HighestRank(caller.Roles) {
			return reject("只能编辑严格低于自己管理层级的角色(你的层级不高于 %q)", act.Role)
		}
		if !v.reg.holderCan(caller.Roles, act.Permission) {
			if act.Op == OpDeny {
				return reject("不能撤销自己都不持有的权限 %q", act.Permission)
			}
			return reject("不能授予自己都不持有的权限 %q", act.Permission)
		}
	}

	if err := v.checkState(state, act); err != nil {
		return err
	}

	return v.checkContainment(state, act)
}

func (v *Validator) checkState(state OverlayState, act Action) error {
	effective := v.reg.Effective(act.Role, act.Permission)
	hasGrant := state.HasGrant(act.Role, act.Permission)
	hasDeny := state.HasDeny(act.Role, act.Permission)

	switch act.Op {
	case OpGrant:
		if hasDeny {
			return reject("%q 的 %q 目前是「已撤销」状态:请先恢复这条撤销记录,而不是新增授予",
				act.Role, act.Permission)
		}
		if effective {
			return reject("%q 已持有 %q,无需重复授予", act.Role, act.Permission)
		}

	case OpRevokeGrant:
		if !hasGrant {
			if effective {
				return reject("%q 的 %q 来自代码捆,没有叠加授权可删:要收回它请使用「撤销(记 deny)」",
					act.Role, act.Permission)
			}
			return reject("%q 并未持有 %q,没有可撤销的叠加授权", act.Role, act.Permission)
		}

	case OpDeny:
		if hasDeny {
			return reject("%q 的 %q 已经是「已撤销」状态", act.Role, act.Permission)
		}
		if hasGrant {
			return reject("%q 的 %q 来自一条叠加授权:请直接撤销这条叠加授权(删除该行),而不是再记一条 deny",
				act.Role, act.Permission)
		}
		if !effective {
			return reject("%q 并未持有 %q,没有可撤销的权限", act.Role, act.Permission)
		}

	case OpRevokeDeny:
		if !hasDeny {
			return reject("%q 的 %q 没有可恢复的撤销记录", act.Role, act.Permission)
		}

	default:
		return reject("未知操作 %q", act.Op)
	}
	return nil
}

func (v *Validator) effectiveAfter(act Action) bool {
	switch act.Op {
	case OpGrant:
		return true
	case OpDeny:
		return false
	default:
		return v.reg.InCodeBundle(act.Role, act.Permission)
	}
}

func (v *Validator) checkContainment(state OverlayState, act Action) error {
	after := func(role string) bool {
		if role == act.Role {
			return v.effectiveAfter(act)
		}
		return v.reg.Effective(role, act.Permission)
	}
	mod, admin, ren := after(RoleModerator), after(RoleAdmin), after(RoleRen)

	if mod && !admin {
		if act.Role == RoleAdmin {
			how := "记 deny"
			if state.HasGrant(RoleModerator, act.Permission) {
				how = "删除那条叠加授权"
			}
			return reject("此操作会破坏 moderator ⊆ admin:请先撤销 moderator 的 %q(%s)",
				act.Permission, how)
		}
		if state.HasDeny(RoleAdmin, act.Permission) {
			return reject("此操作会破坏 moderator ⊆ admin:admin 的 %q 目前处于已撤销状态,请先恢复它",
				act.Permission)
		}
		return reject("此操作会破坏 moderator ⊆ admin:请先把 %q 授予 admin", act.Permission)
	}
	if admin && !ren {
		return reject("授予会破坏 admin ⊆ ren:ren 的代码捆不含 %q", act.Permission)
	}
	return nil
}

func (r *Registry) holderCan(roles []string, p authz.Permission) bool {
	d, ok := r.Lookup(p)
	if !ok {
		return false
	}
	return d.Holder.Can(roles, p)
}
