package perm

import "api/internal/platform/authz"

const (
	AdminAccess             authz.Permission = "oauth.admin_access"
	UsersPIIView            authz.Permission = "oauth.users.pii_view"
	RolesGrantBasic         authz.Permission = "oauth.roles.grant_basic"
	RolesGrantSite          authz.Permission = "oauth.roles.grant_site"
	RolesGrantAdmin         authz.Permission = "oauth.roles.grant_admin"
	ClientsStorageConfig    authz.Permission = "oauth.clients.storage_config"
	ClientsPrivilegedConfig authz.Permission = "oauth.clients.privileged_config"
	SitesManageAll          authz.Permission = "oauth.sites.manage_all"
	PermissionsManage       authz.Permission = "oauth.permissions.manage"
	MoemoepointAdjust       authz.Permission = "oauth.moemoepoint.adjust"
	UsersAnonymize          authz.Permission = "oauth.users.anonymize"
)

const (
	SitesCreate   authz.Permission = "oauth.sites.create"
	SitesUpdate   authz.Permission = "oauth.sites.update"
	SitesDelete   authz.Permission = "oauth.sites.delete"
	ClientsCreate authz.Permission = "oauth.clients.create"
	ClientsUpdate authz.Permission = "oauth.clients.update"
	ClientsDelete authz.Permission = "oauth.clients.delete"
)

var NonDelegable = authz.NonDelegable{
	RolesGrantAdmin:   true,
	PermissionsManage: true,
	SitesManageAll:    true,
	MoemoepointAdjust: true,
	UsersAnonymize:    true,
}

var adminPerms = []authz.Permission{
	AdminAccess,
	RolesGrantBasic,
	RolesGrantSite,
	SitesCreate,
	SitesUpdate,
	SitesDelete,
	ClientsCreate,
	ClientsUpdate,
	ClientsDelete,
}

var renPerms = append(append([]authz.Permission{}, adminPerms...),
	UsersPIIView,
	RolesGrantAdmin,
	ClientsStorageConfig,
	ClientsPrivilegedConfig,
	SitesManageAll,
	PermissionsManage,
	MoemoepointAdjust,
	UsersAnonymize,
)

var Bundles = authz.Bundles{
	"admin": adminPerms,
	"ren":   renPerms,
}

var Resolver = authz.NewHolder(Bundles)
