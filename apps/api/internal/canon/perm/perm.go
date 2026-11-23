// Package perm is canon's domain in the platform permission console: the keys
// the v3 review and curation routes check, their code bundles, and the holder
// the override poll swaps.
package perm

import (
	"api/internal/canon/registry"
	"api/internal/platform/authz"
)

const (
	TodoValue    authz.Permission = "canon.todo.value"
	TodoIdentity authz.Permission = "canon.todo.identity"
	Merge        authz.Permission = "canon.merge"
	Takedown     authz.Permission = "canon.takedown"
)

var All = []authz.Permission{TodoValue, TodoIdentity, Merge, Takedown}

var Bundles = authz.Bundles{"moderator": All, "admin": All, "ren": All}

var Holder = authz.NewHolder(Bundles)

var todoKeys = map[string]authz.Permission{
	registry.TodoIdentity:     TodoIdentity,
	registry.TodoAgentOnHuman: TodoValue,
}

// ForTodo is the key that decides a todo of this kind.
func ForTodo(kind string) (authz.Permission, bool) {
	p, ok := todoKeys[kind]
	return p, ok
}
