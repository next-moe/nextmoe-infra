package perm

import "api/internal/platform/authz"

const UsageView authz.Permission = "ai.usage_view"

const BudgetManage authz.Permission = "ai.budget_manage"

var NonDelegable = authz.NonDelegable{
	BudgetManage: true,
}

var adminPerms = []authz.Permission{UsageView}

var renPerms = append(append([]authz.Permission{}, adminPerms...), BudgetManage)

var Bundles = authz.Bundles{
	"admin": adminPerms,
	"ren":   renPerms,
}

var Resolver = authz.NewHolder(Bundles)
