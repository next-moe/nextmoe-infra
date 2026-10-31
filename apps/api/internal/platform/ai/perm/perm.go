package perm

import "api/internal/platform/authz"

const UsageView authz.Permission = "ai.usage_view"

const BudgetManage authz.Permission = "ai.budget_manage"

const LLMManage authz.Permission = "ai.llm_manage"

const AgentManage authz.Permission = "ai.agent_manage"

var NonDelegable = authz.NonDelegable{
	BudgetManage: true,
	LLMManage:    true,
}

var adminPerms = []authz.Permission{UsageView, AgentManage}

var renPerms = append(append([]authz.Permission{}, adminPerms...), BudgetManage, LLMManage)

var Bundles = authz.Bundles{
	"admin": adminPerms,
	"ren":   renPerms,
}

var Resolver = authz.NewHolder(Bundles)
