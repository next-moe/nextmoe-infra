package perm

import "api/internal/platform/authz"

const Manage authz.Permission = "shop.manage"

const Publish authz.Permission = "shop.publish"

const Grant authz.Permission = "shop.grant"

// The code pool and a user's orders show coupon codes in plaintext, sold ones
// included. These keys sat in the admin bundle until 2026-09, when prod had 48
// global admins and three of them outside ren read the pool.
var NonDelegable = authz.NonDelegable{
	Manage:  true,
	Publish: true,
	Grant:   true,
}

var renPerms = []authz.Permission{Manage, Publish, Grant}

var Bundles = authz.Bundles{"ren": renPerms}

var Resolver = authz.NewHolder(Bundles)
