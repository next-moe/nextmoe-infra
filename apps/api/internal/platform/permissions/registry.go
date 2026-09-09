package permissions

import (
	aiPerm "api/internal/platform/ai/perm"
	artifactPerm "api/internal/platform/artifact/perm"
	"api/internal/platform/authz"
	catalogPerm "api/internal/platform/catalog/perm"
	devapiPerm "api/internal/platform/devapi/perm"
	newsPerm "api/internal/platform/news/perm"
	settingsPerm "api/internal/platform/settings/perm"
	sitePerm "api/internal/platform/site/perm"
	trustPerm "api/internal/platform/trust/perm"
)

const (
	RoleUser      = "user"
	RoleCreator   = "creator"
	RoleModerator = "moderator"
	RoleAdmin     = "admin"
	RoleRen       = "ren"
)

var MatrixRoles = []string{RoleUser, RoleCreator, RoleModerator, RoleAdmin, RoleRen}

var axisRank = map[string]int{
	RoleModerator: 1,
	RoleAdmin:     2,
	RoleRen:       3,
}

func HighestRank(roles []string) int {
	best := 0
	for _, r := range roles {
		if rank := axisRank[r]; rank > best {
			best = rank
		}
	}
	return best
}

type Key struct {
	Permission authz.Permission
	DescEN     string
	DescZH     string
}

type Domain struct {
	Name         string
	TitleZH      string
	Bundles      authz.Bundles
	Holder       *authz.Holder
	NonDelegable authz.NonDelegable
	Keys         []Key
}

type Registry struct {
	domains []Domain
	byKey   map[authz.Permission]int
}

func NewRegistry(domains ...Domain) *Registry {
	r := &Registry{domains: domains, byKey: make(map[authz.Permission]int)}
	for i, d := range domains {
		for _, k := range d.Keys {
			r.byKey[k.Permission] = i
		}
	}
	return r
}

func (r *Registry) Domains() []Domain { return r.domains }

func (r *Registry) Lookup(p authz.Permission) (Domain, bool) {
	i, ok := r.byKey[p]
	if !ok {
		return Domain{}, false
	}
	return r.domains[i], true
}

func (r *Registry) IsNonDelegable(p authz.Permission) bool {
	d, ok := r.Lookup(p)
	if !ok {
		return true
	}
	return d.NonDelegable.Has(p)
}

func (r *Registry) Effective(role string, p authz.Permission) bool {
	d, ok := r.Lookup(p)
	if !ok {
		return false
	}
	return d.Holder.Can([]string{role}, p)
}

func (r *Registry) InCodeBundle(role string, p authz.Permission) bool {
	d, ok := r.Lookup(p)
	if !ok {
		return false
	}
	for _, granted := range d.Bundles[role] {
		if granted == p {
			return true
		}
	}
	return false
}

var live = NewRegistry(
	Domain{
		Name:         "oauth",
		TitleZH:      "IdP 控制台",
		Bundles:      sitePerm.Bundles,
		Holder:       sitePerm.Resolver,
		NonDelegable: sitePerm.NonDelegable,
		Keys: []Key{
			{sitePerm.AdminAccess, "Reach the admin console at all (the page/list gate).", "进入管理控制台(页面与列表门)"},
			{sitePerm.UsersPIIView, "See user PII — email in the list, email and IP in the detail — and change a user's email.", "查看用户 PII(列表邮箱、详情邮箱与 IP),并修改用户邮箱"},
			{sitePerm.RolesGrantBasic, "Grant and revoke the below-admin roles (moderator, creator).", "授予/撤销 admin 以下角色(moderator、creator)"},
			{sitePerm.RolesGrantSite, "Grant and revoke site-scoped roles.", "授予/撤销站点作用域角色"},
			{sitePerm.RolesGrantAdmin, "Grant and revoke admin.", "授予/撤销 admin 角色"},
			{sitePerm.SitesCreate, "Create a site.", "创建站点"},
			{sitePerm.SitesUpdate, "Update a site.", "编辑站点"},
			{sitePerm.SitesDelete, "Delete a site.", "删除站点"},
			{sitePerm.SitesManageAll, "See and edit every site and client, not only self-created rows.", "跨创建者管理全部站点与客户端(而非仅自建行)"},
			{sitePerm.ClientsCreate, "Create an OAuth client.", "创建 OAuth 客户端"},
			{sitePerm.ClientsUpdate, "Update an OAuth client.", "编辑 OAuth 客户端"},
			{sitePerm.ClientsDelete, "Delete an OAuth client.", "删除 OAuth 客户端"},
			{sitePerm.ClientsStorageConfig, "Enable a client's object-storage capabilities (artifact / image).", "开启客户端存储能力(artifact / image)"},
			{sitePerm.ClientsPrivilegedConfig, "Set the sensitive client fields (ren-only scopes, auto_consent, display_order).", "设置敏感客户端字段(ren 专属 scope、auto_consent、display_order)"},
			{sitePerm.PermissionsManage, "Run the permission console — grant and revoke overlay rows.", "运行权限控制台(授予/撤销叠加行)"},
		},
	},
	Domain{
		Name:    "catalog",
		TitleZH: "目录注册表",
		Bundles: catalogPerm.Bundles,
		Holder:  catalogPerm.Resolver,
		Keys: []Key{
			{catalogPerm.Review, "Reach the catalog identity-registry curation surface (merge / unmerge / reconcile).", "目录注册表策展面(合并/拆分/对账)"},
			{catalogPerm.ClaimReview, "Decide a product site's submission — approve / decline / ban / unban.", "裁决产品站投稿认领(通过/驳回/封禁/解封)"},
			{catalogPerm.EditWork, "Propose an edit on catalog.work through the editing engine.", "对 catalog.work 提交编辑提案"},
			{catalogPerm.EditWorkReview, "Adjudicate a catalog.work proposal (amend / merge / decline / revert).", "裁决 catalog.work 提案(修订/合入/驳回/回滚)"},
			{catalogPerm.EditTaxonomy, "Propose an edit on the shared vocabulary (label / tag / engine / series).", "对共享词表提交编辑提案(label/tag/engine/series)"},
			{catalogPerm.EditTaxonomyReview, "Adjudicate a vocabulary proposal.", "裁决词表提案"},
			{catalogPerm.EditCharacter, "Propose an edit on catalog.character through the editing engine.", "对 catalog.character 提交编辑提案"},
			{catalogPerm.EditCharacterReview, "Adjudicate a catalog.character proposal (amend / merge / decline / revert).", "裁决 catalog.character 提案(修订/合入/驳回/回滚)"},
			{catalogPerm.EditRelease, "Propose an edit on catalog.release through the editing engine (edit existing rows, hide/unhide; no create).", "对 catalog.release 提交编辑提案(只编既有行、隐藏/解除隐藏,不可新建)"},
			{catalogPerm.EditReleaseReview, "Adjudicate a catalog.release proposal (amend / merge / decline / revert).", "裁决 catalog.release 提案(修订/合入/驳回/回滚)"},
			{catalogPerm.EditTrusted, "Write at the trusted tier through the editing engine (site ProposeTrusted lanes accept filings directly).", "以受信任层级走编辑引擎写入(站点 trusted 通道直接接受其提交)"},
			{catalogPerm.ClaimTrusted, "Mint a work submission straight to live, skipping claim review. Does not touch the editing engine.", "建档投稿直接上线、免认领审核(不影响编辑提案走不走审)"},
		},
	},
	Domain{
		Name:    "news",
		TitleZH: "情报 feed",
		Bundles: newsPerm.Bundles,
		Holder:  newsPerm.Resolver,
		Keys: []Key{
			{newsPerm.Review, "Decide a partner news item — publish / reject / withdraw. Publishing is a human act; the AI only advises.", "裁决合作方情报条目(发布/拒绝/撤回);发布只能由人做,AI 仅供参考"},
		},
	},
	Domain{
		Name:    "trust",
		TitleZH: "Trust & Safety",
		Bundles: trustPerm.Bundles,
		Holder:  trustPerm.Resolver,
		Keys: []Key{
			{trustPerm.QueueAccess, "Reach the T&S review inbox (list / claim / decide, registries, dead letters).", "进入 T&S 审核收件箱(队列/登记表/死信)"},
			{trustPerm.TermManage, "Create and deprecate Tier0 word-list terms.", "增改/退役 Tier0 词表词条"},
		},
	},
	Domain{
		Name:    "ai",
		TitleZH: "AI 网关",
		Bundles: aiPerm.Bundles,
		Holder:  aiPerm.Resolver,
		Keys: []Key{
			{aiPerm.UsageView, "Reach the AI-gateway usage / cost / budget dashboard.", "查看 AI 网关用量/成本/预算看板"},
		},
	},
	Domain{
		Name:         "devapi",
		TitleZH:      "开发者平台",
		Bundles:      devapiPerm.Bundles,
		Holder:       devapiPerm.Resolver,
		NonDelegable: devapiPerm.NonDelegable,
		Keys: []Key{
			{devapiPerm.Manage, "Manage open-API applications — tier / quota, and mint / rotate / revoke keys.", "管理开放 API 应用(tier/配额,铸造/轮换/吊销 key)"},
			{devapiPerm.PolicyManage, "Set the developer-platform policy matrix — whether app creation is self-service, needs approval, or is off, and whether owners may manage their own apps and keys.", "设定开发者平台策略矩阵(自助创建/需审批/关闭,以及自助管理应用与密钥);不可委派"},
		},
	},
	Domain{
		Name:    "artifact",
		TitleZH: "文件存储",
		Bundles: artifactPerm.Bundles,
		Holder:  artifactPerm.Resolver,
		Keys: []Key{
			{artifactPerm.FilesManage, "Browse, delete and reclaim stored artifact files.", "浏览/删除/回收 artifact 文件"},
		},
	},
	Domain{
		Name:    "settings",
		TitleZH: "配置中心",
		Bundles: settingsPerm.Bundles,
		Holder:  settingsPerm.Resolver,
		Keys: []Key{
			{settingsPerm.View, "See the configuration center — every key, its effective value and where it comes from.", "查看配置中心(全部键、生效值与来源)"},
			{settingsPerm.Write, "Set or reset a configuration override; every service picks it up within 30 seconds.", "设置/撤销配置覆盖值(30 秒内全服务生效)"},
		},
	},
)

func Live() *Registry { return live }
