package perm

import "api/internal/platform/authz"

const View authz.Permission = "settings.view"

const Write authz.Permission = "settings.write"

const EnvWrite authz.Permission = "settings.env_write"

var adminPerms = []authz.Permission{View}

var renPerms = append(append([]authz.Permission{}, adminPerms...), Write, EnvWrite)

var Bundles = authz.Bundles{"admin": adminPerms, "ren": renPerms}

var Resolver = authz.NewHolder(Bundles)
