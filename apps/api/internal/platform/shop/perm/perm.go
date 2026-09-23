package perm

import "api/internal/platform/authz"

const Manage authz.Permission = "shop.manage"

const Publish authz.Permission = "shop.publish"

const Grant authz.Permission = "shop.grant"

var adminPerms = []authz.Permission{Manage, Publish, Grant}

var Bundles = authz.Bundles{"admin": adminPerms, "ren": adminPerms}

var Resolver = authz.NewHolder(Bundles)
