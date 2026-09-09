# Permission-first 授权体系设计

> 2026-07-08 — 把站内授权从「五角色扁平 RBAC」演进为 **permission-first**:操作权限成为一等公民,
> 代码只检查权限、永不检查角色;角色降级为「权限捆」(数据)。**五角色全局契约(JWT `roles`
> claim)零变化**,下游 kungal/moyu/wiki 零影响——本演进不产生任何跨仓契约变更。
>
> 关联:五角色语义见 [integration/oauth/11-roles.md](../integration/oauth/11-roles.md)(Tier-A,权威);
> 本文是 infra **内部**设计与纪律沉淀(非 Tier-A,不 `docs:sync`)。落地编排见 `refs/plans/00-rbac/`。

## 1. 动机与模型

**痛点**:五角色扁平 RBAC 的所有 enforcement 都是散落在 ~30 个调用点的角色字符串匹配
(`RequireRole("admin","moderator")` / `hasRole(roles, …)`)。每出现一种新操作只能「塞给现有
角色」或「铸第 6 个全局角色」,两条路都不可扩展(RBAC 的业界天花板即 5–10 个角色)。

**定案模型(四层,身份全局、授权本地)**:

1. **五角色全局契约不动**(`11-roles.md`,Tier-A)。wire 格式(JWT `roles` claim)零变化,下游
   零影响。**今后不再新增全局角色。**
2. **站内 permission-first(本工程主体)**:操作权限成为一等公民的权限字符串常量;**所有代码只
   检查权限、永不检查角色**。角色降级为「权限捆」(bundle)= 数据。通用引擎
   (`internal/platform/authz`,平台域)与各域自己的权限词汇(如 `internal/platform/galgame/perm`)
   分离——引擎不含任何产品词汇,满足「平台不得 import galgame」不变量。
3. **信任轴**(nextmoe-draft doc 11 的 TL0-4)未来接**同一** resolver——本工程不建,但引擎形态
   不阻碍它(权限解析 = 各来源捆的并集)。
4. **scope 域限定 / 字段级权限 / 外部引擎(OpenFGA 等)**:全按 §6 触发器,现在不建。

**行为不变基线**:整个迁移是纯重构。每个调用点迁移后,授予该权限的角色集合与迁移前一致(唯二
语义修正见 §4)。

## 2. 引擎与词汇

### 2.1 引擎 `internal/platform/authz`(平台域,零产品词汇)

```go
type Permission string                     // 一个操作能力,如 "galgame.publish_direct"
type Bundles map[string][]Permission       // 角色名(精确的 roles claim 串) → 该角色授予的权限集
type Resolver struct{ /* role -> set */ }  // 构造后不可变
func NewResolver(b Bundles) *Resolver
func (r *Resolver) Can(roles []string, p Permission) bool

type Checker interface{ Can(roles []string, p Permission) bool }  // 执行点只需要它
type Holder struct{ /* atomic.Pointer[Resolver] */ }              // 可整体热替换
func NewHolder(b Bundles) *Holder
func (h *Holder) Swap(r *Resolver)
type NonDelegable map[Permission]bool      // 叠加层永不可授予的键(§7.3)
```

- `Can` 语义:调用者的**任一**角色的捆内含该权限即放行;对 nil/空 roles、未知角色、未知权限一律
  **fail-closed**(false)。
- 隐式 `user`(登录本身)**不进** authz;登录判定归 `JWTAuth`/`Auth` 中间件,authz 只管提权。
- **不做通配符、不做层级**——层级作为**数据**编码在各域的捆里(`ren 捆 ⊇ admin 捆 ⊇ moderator 捆`),
  由属性测试钉死。
- 路由门:`middleware.RequirePermission(res authz.Checker, p authz.Permission)`,与旧
  `RequireRole` 同形(读 `c.Locals("user_roles")`,fail-closed 403)。in-handler 判定:
  `perm.Resolver.Can(roles, perm.Xxx)`。
- **执行点持 Holder,不持 Resolver**(2026-08-04 §7 起):各 perm 包的 `Resolver` 变量类型是
  `*authz.Holder`。Resolver 本身仍不可变——刷新是**整体新建 + 原子换指针**,读者永不看到半应用
  的授权表,热路径零加锁。若执行点捕获了 `*Resolver`,叠加层刷新会静默失效到重启为止,故门参数
  收为 `Checker` 接口。

### 2.2 七个词汇包(各域自持,import 引擎)

| 包 | 面向 | 依赖方向 |
|---|---|---|
| `internal/platform/galgame/perm` | galgame 产品域 | 产品 import 平台(authz),合法 |
| `internal/platform/catalog/perm` | catalog 内部审核面 | 平台域,import authz |
| `internal/platform/trust/perm` | T&S 统一审核收件箱队列 | 平台域,import authz |
| `internal/platform/site/perm` | IdP 控制台(`oauth.*`,横跨 auth/site,单叶子包承载) | 只 import authz;auth handler import 它不扰动既有 auth→site 方向 |
| `internal/platform/artifact/perm` | artifact 文件运维 | 平台域,import authz |
| `internal/platform/devapi/perm` | 开发者平台(NextMoe 开放 API)管理面 | 平台域,import authz |
| `internal/platform/ai/perm` | AI 网关用量看板(运营面) | 平台域,import authz |

每包 = 权限常量 + `var Bundles authz.Bundles`(role→捆,层级由 `moderatorPerms ⊆ adminPerms ⊆ renPerms`
组合保证)+ `var Resolver = authz.NewHolder(Bundles)`(包级单例;2026-08-04 起是 Holder,起点即
代码捆,叠加层刷新时整体换掉。退役的 galgame 包仍是固定 `Resolver`——它没有需要保鲜的执行点)。

**跨域聚合在 `internal/platform/permissions`**:各 perm 包互不认识(这正是分包的意义),而控制台
需要同时看见全部词汇表,故聚合是控制台的关注点,放在一个只被自己引用的叶子包里——不可能成环。
退役的 `galgame/perm` **刻意不在其中**:列出零执行点的键,只会诱使运维授出一把没有任何代码会检查
的权限。

### 2.3 全部权限的当前 golden 表

> 唯一权威在代码里的 `*_test.go` golden 映射;下表是它的人读快照(**2026-08-04**,
> 逐行核对七个 `perm_test.go` 的 `goldenGrants` 取得)。
>
> **本表是"代码捆"= 默认值,不是地板。** 2026-08-04 起叠加层(§7)可在运行时把某个键**加**给
> 或**从** creator/moderator/admin **减**去,活线上的实际授予 = 本表 ∪ grant 行 − deny 行。
> **唯一的例外是 `ren`:任何 deny 行对它都是死的**(§7.1),故 ren 那一列——且只有 ren 那一列
> ——仍是下界。线上当前实际值看权限矩阵控制台(`/permission`)。

| 权限 | 授予角色捆 | 语义 |
|---|---|---|
| `galgame.publish_direct` | creator, moderator, admin, ren | 直接发布(跳审核队列 + 跳配额) |
| `galgame.review` | moderator, admin, ren | 审核队列查看未发布提交 |
| `galgame.edit_any` | moderator, admin, ren | 编辑任意 galgame(内容审核编辑权) |
| `galgame.create` | moderator, admin, ren | POST /galgame 直发路由门 |
| `galgame.admin_access` | moderator, admin, ren | /admin/galgame/* 管理面 |
| `galgame.taxonomy.edit_any` | moderator, admin, ren | tag/engine/official/series 增改删 |
| `galgame.taxonomy.review` | moderator, admin, ren | taxonomy 修订回滚 |
| `galgame.search.all_states` | moderator, admin, ren | 越过公开态钳制、跨全部状态搜索 |
| `galgame.owner_override` | admin, ren | 越权处置(owner-or-admin 的 admin 支;**不含 moderator**) |
| `edit.galgame.game.review` | admin, ren | 引擎面裁决 `galgame.game` 提案(merge/decline/amend/revert);跟随 owner_override 轴(**不含 moderator**) |
| `edit.galgame.game.status` | moderator, admin, ren | 直改 `galgame.game.status` 管理字段(approve/decline/ban/unban 落为引擎直编);跟随 admin_access 轴 |
| `edit.galgame.game.vndb_id` | moderator, admin, ren | 改 `galgame.game.vndb_id`(占坑教训 → 已发布条目 staff-only;投稿人改自己草稿由 adapter 情境授予,非角色) |
| `catalog.review` | ren | catalog 内部审核/浏览面(注册表策展:merge/unmerge、对账队列) |
| `catalog.claim.review` | moderator, admin, ren | 裁决产品站投稿认领(approve/decline/ban/unban);与 `catalog.review` **刻意分键**——投稿审核是常规内容审核,收窄到 ren 即产品权利回归 |
| `edit.catalog.work` | admin, ren | 引擎面对 `catalog.work` 提提案(全局角色授予只给策展 staff;租户用户经信任层级/站点叠加取得) |
| `edit.catalog.work.review` | admin, ren | 引擎面裁决 `catalog.work` 提案(amend/merge/decline/revert) |
| `edit.catalog.taxonomy` | admin, ren | 引擎面对 `catalog.{label,tag,engine,series}` 提提案(四族**一把键**=同一权威:注册表共享词表) |
| `edit.catalog.taxonomy.review` | admin, ren | 引擎面裁决词表提案;**建/删/合并词表条目不在此键**,属注册表策展,仍在 `catalog.review` 之后 |
| `edit.catalog.character` | admin, ren | 引擎面对 `catalog.character` 提提案(标量属性 + curated 别名车道 + 简介;**建/删/合并角色不在此键**,属注册表策展) |
| `edit.catalog.character.review` | admin, ren | 引擎面裁决 `catalog.character` 提案(amend/merge/decline/revert);kungal overlay 与词表同档=`automerge=never`,一个角色被多部作品共享 |
| `catalog.edit.trusted` | admin, ren | 以受信任层级(`TrustTier=2`)走编辑引擎写入:站点 `ProposeTrusted` 通道直接接受其提交(letmoe work overlay)。**与审核轴刻意正交,故不含 moderator**;产品站把它授给自己的角色(如 letmoe `creator`)走 §7 叠加层。2026-09-09 前它还兼管建档快车道,现已拆给 `catalog.claim.trusted`,本键**只**管编辑引擎 |
| `catalog.claim.trusted` | admin, ren | 建档投稿(`POST /v2/me/claims` 铸造车道)直接落 `live`、免认领审核;**不触编辑引擎**——从 `catalog.edit.trusted` 拆出(2026-09-09),因为单键下「投稿免审、编辑照审」无法表达(论坛要给 moderator 的正是这个组合,走权限台叠加层授予)。两键互不蕴含;第三方应用封顶同样适用 |
| `trust.queue_access` | moderator, admin, ren | T&S 统一审核收件箱队列 |
| `trust.term_manage` | admin, ren | Tier0 词表增改/退役(站域封禁权,比 queue_access 敏感;**不含 moderator**) |
| `ai.usage_view` | admin, ren | AI 网关用量/成本/预算看板(**不含 moderator**——运营面) |
| `oauth.admin_access` | admin, ren | 控制台四组门(/admin、/sites、/oauth/clients、/admin/artifact) |
| `oauth.users.pii_view` | ren | 看用户 PII(邮箱/IP);**也是改邮箱的键**——`PATCH /admin/users/:uuid` 的 `email` 字段只对持键者开放(不许写自己读不到的字段),响应里的 email 同样按此键脱敏 |
| `oauth.roles.grant_basic` | admin, ren | 授予/撤销 moderator、creator |
| `oauth.roles.grant_site` | admin, ren | 授予/撤销站点作用域角色(契约 12-site-roles;站点角色恒低于全局 moderator,故 admin 可授) |
| `oauth.roles.grant_admin` | ren | 授予/撤销 admin(及隐式 user 基座);**不可委派** |
| `oauth.sites.create` | admin, ren | 创建站点(2026-08-04 从 admin_access 拆出) |
| `oauth.sites.update` | admin, ren | 编辑站点(同上) |
| `oauth.sites.delete` | admin, ren | 删除站点(同上) |
| `oauth.clients.create` | admin, ren | 创建 OAuth 客户端(同上) |
| `oauth.clients.update` | admin, ren | 编辑 OAuth 客户端(同上;`/storage` 子资源另需 storage_config) |
| `oauth.clients.delete` | admin, ren | 删除 OAuth 客户端(同上) |
| `oauth.permissions.manage` | ren | 权限控制台:对任意角色增删叠加授权;**不可委派** |
| `oauth.clients.storage_config` | ren | 开客户端存储能力(artifact/image) |
| `oauth.clients.privileged_config` | ren | 敏感客户端字段(ren-only scope / auto_consent / display_order) |
| `oauth.sites.manage_all` | ren | 跨创建者管理站点与 OAuth 客户端;**没有此键的 admin 只看得见、只改得动自己创建的行**(`sites.created_by_user_id` / `oauth_clients.created_by_user_id`;NULL 归属者=历史行与开发者门户应用,仅 ren 可及);**不可委派** |
| `artifact.files.manage` | ren | artifact 文件浏览/删除/回收 |
| `devapi.manage` | admin, ren | 开发者平台管理面(启用应用 / tier / 铸·轮换·吊销 key / 审 scope 申请与应用申请) |
| `devapi.policy_manage` | ren | 改开发者平台策略矩阵(自助创建/需审批/关闭,以及自助管理应用与密钥);矩阵对 admin 只读可见;**不可委派** |

> **`edit.*` 命名段说明**:编辑引擎的字段策略键以 `edit.<entity 全名>` 起头
> (`edit.galgame.game.*` / `edit.catalog.work*` / `edit.catalog.taxonomy*`),
> 与 §2.4 的 `<domain>.<object>.<verb>` 并存——引擎按实体名解析策略,故 domain
> 段落在 `edit.` 之后。键仍分别由所属域的 perm 包(galgame / catalog)持有。

### 2.4 命名约定

`<domain>.<verb>` 或 `<domain>.<object>.<verb>`,全小写,段内 snake_case
(例:`galgame.publish_direct`、`oauth.clients.storage_config`)。

### 2.5 「如何新增一个权限」操作指南(零新角色)

1. 在对应域的 `perm` 包加权限常量(段式命名)。
2. 把它加进应授予的角色捆(用 `moderatorPerms/adminPerms/renPerms` 组合以自动保持包含性)。
3. 在该包 `perm_test.go` 的 golden 表加一行(角色集)。
4. 调用点用 `perm.Resolver.Can(roles, perm.Xxx)`(in-handler)或
   `middleware.RequirePermission(perm.Resolver, perm.Xxx)`(路由门)。
5. **在 `internal/platform/permissions/registry.go` 的对应域 `Keys` 里补一行**(键 + 中英
   描述)。漏了会被 `TestRegistryDescribesExactlyTheBundledKeys` 直接判红:没有注册表条目的键
   在控制台既看不见也授不出去,反过来有条目却不在任何捆里的键则谁都授不到——两种漂移都在合并前
   拦下。

**页面门 vs 操作门**(2026-08-04 起):`oauth.admin_access` 只管"进不进得来 / 看不看得见列表",
每个写操作另有自己的 CRUD 键。二者叠加,归属规则(`mayManage`)再叠在最上层——持有
`oauth.sites.update` 只意味着"能改自己有权的站点",不改变归属范围。

**永远不要为一个新操作铸一个新全局角色**——这正是本演进要根除的反模式。

## 3. 测试纪律(每个 perm 包四组标配)

1. **golden 映射表**:对每个权限遍历五角色,逐一断言授予与否,与 golden 列完全一致。
2. **非捆角色恒假**:任何不在捆里的角色(隐式 `user`、空串、退役别名占位)对每个权限恒 false
   ——这是 fail-closed 默认,也是「退役 `super_admin` 现只是个未知角色」的安全性证明。
3. **管理轴包含性**(属性测试):`moderator 捆 ⊆ admin 捆 ⊆ ren 捆`。
4. **creator 正交**(属性测试):galgame 面 creator 捆 = `{publish_direct}`;所有平台面 creator 零授予。

## 4. 历史修正存档(唯二允许的语义修正)

迁移是纯重构,唯二例外(逐条记于 01/02 执行报告):

### 4.1 `+ren`(潜伏契约不合规修复)

契约 §4 规则 2 要求管理轴逐级包含(`ren ⊇ admin ⊇ moderator`),但 infra 原有的角色检查普遍
只写 `admin`/`admin,moderator` 而**漏了 `ren`**。借迁移把 `ren` 补进**所有含 `admin` 的授权集**。
因运维不变量「ren 必持 admin」,线上行为实际零变化。受影响权限:上表所有含 `ren` 的行,其
`ren` 均为本次补入。owner_override 特别注意:原仅 `admin` → 现 `{admin, ren}`(**不含 moderator**,
保持 owner-or-admin 语义)。

### 4.2 `−super_admin`(IdP 从不签发)

历史别名 `super_admin` 不是有效角色(契约 §1)。原 galgame 三处调用点
(`submission_service.go` publish_direct、`galgame_service.go` review/edit_any)误含它,一律剔除。
剔除后 `super_admin` 只是个未知角色,由 §3 测试 2 证明其零授予。

## 5. 刻意保留的角色字符串(target 侧语义,**不是**待迁移欠账)

permission-first 只管「**调用者能不能做某操作**」。以下角色字符串是**别的语义**,正确地保留:

| 位置 | 语义 | 为何不迁 |
|---|---|---|
| `admin_handler.go` `manageableRoles` | 哪些角色可经 API 授予(`ren` 永不可) | target 白名单,非 caller 能力 |
| `admin_handler.go` `callerCanManageRole` 内 `role=="admin"\|\|"user"` | 按**目标**角色选所需权限(grant_admin/grant_basic) | 分类 target,非门 |
| `admin_service.go` `adminProtected` | **目标用户**是否 admin(封禁/匿名化保护) | 保护规则,非 caller 能力 |
| `auth_service.go` 账号切换 step-up | **目标账号**是否 admin/ren → 要求 step-up | target 属性判定,非门 |
| `creator_application_service.go` `CreatorRoleName` | 审批通过时**被授予**的角色名 | 授予对象,非门 |
| `taxonomy_revision_handler.go` `roleLevel` | 把角色映射成存进 `taxonomy_revision` 的**数值审计快照**(记录「哪一级改的」) | 审计留位字段(galgame_wiki 契约留位),非 enforcement |

## 6. 非目标与触发器

- **JWT legacy `Role int` / `SiteID` claim**(`pkg/utils/jwt.go`):wire 兼容字段,归 OIDC 标准化轨,
  本工程不动。
- ~~**DB 自定义权限捆** + admin API + web 管理窗口~~:**触发条件已达成,2026-08-04 已实现**——
  见 §7。落地形态与当年的设想不同:没有建 `site_roles`/`site_role_permissions` 三表(那是把
  RBAC 再造一遍),而是一张**双极性叠加表** `role_permission_overrides` 盖在代码捆之上
  (2026-08-04 落地时只增不减,同日经裁定加入 deny 半边)。
- **scope 域限定(轻量 ReBAC)**:某分区版主/某系列编辑。**触发** = 第一个「仅限某资源子集」需求。
- **字段级权限**:**触发** = doc 09 P1-1 编辑引擎泛化动工。
- **外部授权引擎(OpenFGA/SpiceDB,AuthZEN)**:**触发** = 跨站共享图谱 / 「按权限过滤列表」成为热
  路径。
- **下游 forum/moyu 迁移**:两站已是命名角色 + 能力函数形(`CanModerate`/`CanAdminister`/`IsCreator`),
  收益低不急;**触发** = kungal-kit 共享库抽出时。

本工程完成后,授权体系里不再有任何「看起来存在但从不生效」的结构:僵尸 `permissions`/
`role_permissions` 表与 `user_site_data.role` 死列已删(refs step 03)。

## 7. 运行时叠加层与权限矩阵控制台(2026-08-04)

§6 的「DB 自定义权限捆」触发器达成。落地的**不是**再造一套 RBAC 表,而是一层薄叠加。

### 7.1 模型:代码是默认值,ren 是保险

`role_permission_overrides(role, permission, effect, granted_by_user_id, created_at)`,
`(role, permission)` 唯一,`effect ∈ {grant, deny}`。一行 = 该角色**额外持有**(grant)或
**不持有**(deny)该权限;删这一行 = 该角色回到代码捆。同一格 grant 与 deny 自相矛盾,唯一索引
保证二者不可能同时存在。

**首版是只增不减的,同日(2026-08-04)经裁定改成现在这样。** 原设计用「一次点击不可能把任何人锁
死」换掉了一个真实操作——「先把这把键从 moderator 收走,周一再说」本来只能靠改代码捆 + 部署。裁
定把那条保证换成一条更窄、但守住了要害的:

> **只有 `ren` 保留不可动摇的代码地板。** creator / moderator / admin 都可以被叠加层减到代码捆
> 以下。

`ren` 是锁死之后的**恢复保险**:无论表里写了什么,它都必须还能进控制台把事情改回来。所以这条豁
免**硬编码在 `Merge` 里**,而不是只交给 validator——它得挡住不是本代码写出来的行:手工 INSERT、
恢复的 dump、将来某个忘了规则的新端点。validator 里那条「ren 行不可编辑」只是把同一件事讲给操作
者听。(`user` 行同理不可编辑,但原因不同:它根本不会生效。)

`permission_audit_logs(actor_user_id, action, role, permission, created_at)` 的动作词汇随之从两个
变成四个:`grant` / `revoke`(删 grant 行)/ `deny` / `revoke_deny`(删 deny 行)。**前两个拼写没
有改动**——改名会把每一条历史行的含义一起改掉。删 grant 行与删 deny 行让角色朝相反方向移动,故审
计里必须是两件事。

`effect` 列**故意不带 GORM `default:` 标签**:该标签会让 GORM 在字段为零值时把它从 INSERT 里省
掉,于是 `""` 会静默变成库默认值——一次「看起来是授予」的权限变更。没有它,空 effect 是插入时的
NOT NULL 报错。存量表加这一列因此走 `AddOverrideEffectColumn` 的裸 SQL(带 DEFAULT 加列回填,再
DROP DEFAULT),与 `devapi.AddOAuthClientDevColumns` 同形;加完 AutoMigrate 才跑。

审计表只增不改,与 override 的增删**同事务**写入:没有审计的权限变更正是这套东西存在的理由,两
者不能拆开。被撤销的授予在 override 表里消失,在这里仍在——否则「上周二是谁给 moderator 那把键
的」永远无法回答。

### 7.2 四个操作

「撤销」在有了 deny 行之后是有歧义的,所以写路径讲四个操作而不是两个:

| 操作 | 做什么 | 适用格 |
|---|---|---|
| `grant` | 写一条 grant 行 | 未持有(`none`) |
| `revoke_grant` | 删那条 grant 行 → 回代码捆 | 由叠加授权持有(`grant`) |
| `deny` | 写一条 deny 行 | 由代码捆持有(`code`) |
| `revoke_deny` | 删那条 deny 行 → 回代码捆 | 已撤销(`deny`) |

**每一种格状态恰好只允许其中一个操作**,所以矩阵给前端的是一个判决而不是一份菜单,前端也就不必
在两个按钮之间做选择。写端点相应地是:`POST /overrides` 带 `effect`,`DELETE /overrides` **不带**
——那一格上有哪条行,删的就是哪条。让客户端自己声明要删哪一种,等于给「以为在撤销授予、实际删掉
了 deny」留了口子。

### 7.3 写入规则(全部服务端强制)

1. **键必须活**。对 `internal/platform/permissions` 注册表校验;打错的、退役 galgame 词汇表里的
   键一律拒。
2. **行必须可编辑**:`creator` / `moderator` / `admin` 三行,**grant 与 deny 同一套**。`user` 排
   除是因为它**根本不会生效**(普通用户 JWT 的 `roles` 是空数组,永不进 `Can`);`ren` 排除有两个
   理由——它是包含性不变量的**上界**,也是 §7.1 那条恢复保险。
3. **不可委派键任何人都授不出去**:`oauth.roles.grant_admin` / `oauth.permissions.manage` /
   `oauth.sites.manage_all`。这三把键的持有者本可借此绕开控制台自身的护栏(铸管理员 / 改写授权表
   本身 / 逃出归属作用域),故只能改代码捆并部署。持 `oauth.permissions.manage` 也照拒。
   **这条只管 grant**:「不肯把键发出去」是一条关于提权的规则,而 deny 是反方向。deny 也不需要单
   独一条——三把键没有任何可编辑角色持有,规则 5 的「没有可撤销的权限」自然就挡住了。
4. **委派规则**(不持 `oauth.permissions.manage` 的调用者):目标角色须**严格低于**自己在管理轴
   上的层级,且自己**确实持有**该权限,`creator` 列仅 ren 可编辑(creator 不在管理轴上,层级比较
   对它无意义)。**grant 与 deny 完全同规则**:从同侪手里收走一把键,并不比递给他一把更无害。
   注意这条已经隐含了两件事——没人能动自己那一层,所以 **admin 行只有 ren 能 deny,自我 deny 不可
   能**。
5. **状态前提**(每一条都指出「那你其实想做的是哪个操作」):
   - `grant`:该键已生效 → 拒(无需重复授予);该格上有 deny 行 → 拒,请先**恢复**。
   - `revoke_grant`:没有 grant 行但键已生效 → 拒,因为它来自代码捆,要收回请用 **deny**。
   - `deny`:该键**未生效** → 拒(没有可撤销的权限);该键**只**由一条 grant 行带来 → 拒,正确操作
     是**删掉那条叠加授权**,而不是再叠一条 deny;已经有 deny 行 → 拒。
   - `revoke_deny`:没有 deny 行 → 拒(没有可恢复的撤销记录)。
6. **包含性**:写入后该键仍须满足 `moderator ⊆ admin ⊆ ren`,**两个方向都查**。授 moderator 一把
   admin 没有的键 → 拒并提示「请先授予 admin」;把 admin 的某键 deny 掉而 moderator 还持有 → 拒并
   提示「请先撤销 moderator 的该键」。提示语还会看另一端那格是什么行,以便说清该用哪个操作(该
   「删掉那条叠加授权」还是「记 deny」,该「授予 admin」还是「恢复 admin」)。

矩阵的「哪些格子你能改」由**同一个 validator 逐格跑一遍**得出,所以 UI 不可能与写路径对同一件事
给出不同答案。前端不复刻任何授权判断。

### 7.4 分发与失效

**事实先行**:所有带 perm 包的服务(oauth / catalog / trust / ai)都已经通过 `app.New` 持有主库
`kun_galgame_infra` 连接,**没有哪个进程需要别人把副本送过来**。于是:

- **真源 = 主库那张表**,每个进程自己读、自己换 Resolver(`Holder.Swap`,整体新建 + 原子换指针)。
- **Redis 只承载「有变更,去看一眼」的提示**(`authz:overrides:changed`),仅在手头已有 Redis 客户端
  的进程使用(oauth)。频道上不跑任何权威数据,故消息丢失/重复/乱序至多推迟一次刷新。
- **轮询 30s 是地板**,所有进程都跑。`cmd/trust` 与 `cmd/ai` 根本没有 Redis(`NeedCache:false`),
  没有这条地板它们会永远停在代码捆——那正是「授予了却不生效」的静默失败。写入方本地**同步**刷新,
  故 oauth 自己的下一个请求立即看到新表。
- **失效安全**:读不到库 → 保持上一份并告警;叠加行指向已下线的键、或 effect 不认识 → 忽略并告警。
  **执行不会因为基础设施故障而放宽。**

#### 启动窗口(deny 带来的唯一一处不对称)

有了 deny 之后,「保持上一份」在**进程刚起来、一次都没加载成功**时不再是纯粹的失效安全:此时在跑
的是代码捆,而代码捆里**还有操作者已经 deny 掉的那些键**。它不会朝另一个方向出错——读不到表永远
凭空变不出一条 grant——但它确实是这套东西唯一「错」而不只是「旧」的时刻。

所以 `Distributor.Start` 的首次加载是**同步且带重试的**(3 次,退避 1s/2s/4s,最坏约 3s)。全败之
后进程**照常提供服务**——把权限控制台的故障升级成全站故障是更坏的交易——但日志走 `slog.Error` 而
不是一条会被划过去的 warning,并明说:本进程只在执行代码捆,**没有任何 deny 生效**,直到下一次轮
询成功。

### 7.5 面

`GET /api/v1/admin/permissions/matrix`(门:`oauth.admin_access`)导出全部域 × 五角色的生效矩阵,
每格标注来源(`code` / `grant` / `deny` / `none`)与本调用者可做什么(`editable` / `can_deny` /
`can_restore`,三者至多一真;都为假时 `reason` 说明为什么);`POST /overrides`(body,带 `effect`)
写入、`DELETE /overrides?role=&permission=` 删除该格上的那条行(不指定种类,见 §7.2);`GET /audit`
读最近变更。写端点**刻意不在路由上收 `oauth.permissions.manage`**——普通 admin 向下委派是合法路
径,把路由收到 ren 会让规则 4 永不可达;能改哪些格子是**逐格**判定的。前端在 `/permission`。
