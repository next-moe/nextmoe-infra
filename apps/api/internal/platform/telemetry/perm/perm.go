package perm

import "api/internal/platform/authz"

const View authz.Permission = "telemetry.view"

const Manage authz.Permission = "telemetry.manage"

var NonDelegable = authz.NonDelegable{
	Manage: true,
}

var renPerms = []authz.Permission{View, Manage}

var Bundles = authz.Bundles{
	"ren": renPerms,
}

var Resolver = authz.NewHolder(Bundles)
