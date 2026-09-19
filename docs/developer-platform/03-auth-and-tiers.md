# 认证、授权与分层

> 本文承载 §4 认证与授权(API Key / OAuth2 / scope / 校验路径)、§7 限流 + 配额 + 分层。设计与命名约定见 [01-design.md](./01-design.md);数据模型(`developer_api_keys` 等)见 [04 §5](./04-platform-internals.md)。

---

## 4. 认证与授权

两条腿,按风险分:

### 4.1 API Key —— 主入口,面向"读公开目录"

- **格式**:`nmk_live_…` / `nmk_test_…`,定长 37,尾部 6 位是 body 的 CRC32(base62),格式错的 key 不查库就拒(`devapi/key_v2.go`)。前缀区分环境,也便于密钥泄漏扫描器识别。更早的 `nm_live_` / `nm_test_` 是 `/v1` 那一代,`/v2` 一律拒收;`/v1` 已 410,存量旧 key 现在打不开任何面,轮换即换发 `nmk_`。
- **存储**(复用 `oauth_client.go` 的 `HashOAuthClientSecret` 模式):
  - 库里**只存 `sha256(key)` 的 hex**,带 `sha256:` 前缀;**明文仅创建时显示一次**,永不落库。
  - 另存 `key_prefix`(如 `nmk_live_a1b2`)与 `last4` 供门户识别。
  - 校验用 `crypto/subtle` 常量时间比较(同 `VerifySecret`)。
- **传递**:`Authorization: Bearer nmk_live_…`。`X-API-Key` 两处行为不同(2026-09-19 公网实测):catalog 进程自己的 `/v2`(catalog/news/store/me…,`apiv2/handler/identity.go` 的 `catalogAuth`)**只读 `Authorization`**,只带 `X-API-Key` 回 `401 MISSING_CREDENTIAL`;经网关 ForwardAuth 的下游面 `/v2/moyu`、`/v2/sticker`(`devapi/forwardauth.go` 借 `extractKey`)**两种都认**,门户 moyu/sticker 指南据此写了「等价写法」。对接方一律用 `Authorization`,两边都通。
- **一个应用可有多把 key**:支持**轮换**(签发新 key,旧 key 设未来 `expires_at`,宽限 24–72h,不瞬杀)与**吊销**(`revoked_at`,下次请求即拒)。
- **默认 scope** = `catalog:read`(只读公开);**NSFW 不是 scope**——曾是一道能力位,已于 2026-08-25 退役,见下方 §4.2 词表后的「NSFW 能力位(已退役)」条。**2026-08-18 更正**:原默认里的 `galgame:read` 已移除——`/v1/galgame` 面于 wave 146 整体退役为 `410 Gone`,该 scope 自那以后不被任何活路由消费,继续默认签发等于发一张对着空气的通行证。**已发出的旧 key 不动、不失效**(它们身上的这个 scope 同样什么都打不开);自助与 admin 两条铸 key 路径的空 scopes 默认现均为 `[catalog:read]`。
- API key 是**机密**:只能服务端使用;浏览器直连第三方用 OAuth2 public client + PKCE,**不发 key**。
- **一把 key 走遍所有面**:限流/配额计数是平台级(跨面合并计数),per-面权限用 scope 表达。

### 4.2 OAuth2 —— 写 / 代表用户的操作,扩展 IdP

- **应用 = `oauth_clients` 行**(第三方:`AutoConsent=false` → 必看同意页;SPA 用 `IsPublic=true` + PKCE)。
- 复用现有 grant 白名单(`Grants`)+ scope 白名单(`AllowedScopes` / `CheckScope`):
  - `client_credentials`:应用自身(app-only)读受限资源。
  - `authorization_code` + PKCE:**代表某用户**(如代为投稿)。
- **scope 词表**(按面命名,起步最小,可扩展):
  - `catalog:read`(公开读;未来 `manga:read` 等同构生长)
  - `store:read`(DLsite 分销链接面;出生于 2026-08-25 的授权制 scope,随整档退役于 2026-08-26 转为自助勾选——请求时仍须携带。它把守的路径自 2026-08-27 起是 `/v2/store/*`,`/v1/store` 已退役,见 [02 §3.9](./02-public-api.md))
  - ~~`news:read`~~(合作媒体资讯索引;2026-08-18 起是授权制 scope,2026-08-25 整档退役——`/v1/news` 当时只认「有一把有效 key」,任意 scope 均可;该面已于 2026-08-27 退役,继任者 `/v2/news` 匿名可调,见 [02 §3.9](./02-public-api.md)。常量仍在代码里,只为让存量 key 行里的这个字面量仍能被读懂)
  - `galgame:submit` `user:read`(Phase 3)
  - ~~`galgame:read`~~(随 `/v1/galgame` 面于 wave 146 退役;常量仍在代码里,只为让历史 key 行与旧 `allowed_scopes` 仍能被读懂)
  - ~~`galgame:nsfw`~~(同上;NSFW 从来不由这个 scope 执法,见下条)

- **NSFW 能力位(已退役,2026-08-25)**:r18 曾由 `developer_api_keys.nsfw_allowed` **AND** `oauth_clients.dev_nsfw_allowed` 两级布尔把关,须管理员授予;该能力位与它的 403(`NSFW_CAPABILITY_REQUIRED`)已整体退役,**任何持 key 的 app 都可以直接取 nsfw 内容,不再有审批**。

  退役的只有凭证检查,**`nsfw` 参数本身的语义一个字没改**:缺省 sfw,显式 `nsfw=true` 才含 r18,`content_rating=r18` 仍须配 `nsfw=true`。不带 `nsfw` 的请求**逐字节不受影响**——它在能力位时代就与是哪把 key 无关,现在依然如此。两列留在库里(默认值已恢复,见 `devapi.AddOAuthClientDevColumns` / `RestoreKeyNSFWDefault`),Go 侧字段与自助/管理面的授予入口均已移除。历史遗留的 `galgame:nsfw` scope 从来不由这道闸执法,现在同样什么都不执法。

### 4.3 校验路径(各面服务侧)

面服务**不直接读** `kun_galgame_infra`;凭证在各面边缘解析,**两个面共用同一套中间件实现**(落地为共享中间件包——`kungal-kit` 候选;或过渡期同构复制,收敛时机随 kit):

- **Bearer JWT**(OAuth2):本地验签(资源服务对一方 token 已这么做)→ 取 `client_id` + `scope`。
- **API Key**:调 IdP 的内部 introspection 端点 → **Redis 缓存**结果(短 TTL,如 60s),避免每请求打 IdP。

introspection 契约(IdP 新增,内部 s2s):
```
POST /oauth/apikey/introspect          (s2s, 仅内网/带 s2s 凭证)
  { "key": "nm_live_…" }
→ 200 { "active": true, "client_id": "...", "app_name": "...",
        "scopes": ["catalog:read"], "tier": "free",
        "key_id": 123, "rate_per_min": 60, "quota_daily": 50000 }
→ 200 { "active": false }   // 未知/已吊销/已过期
```
> 备选:若与 image/artifact 现有的 client 校验机制(site-key)一致地"共享 DB 读",可改为面服务直读 `oauth_clients`/`developer_api_keys`——**实现时与现有 image/artifact 的 client 校验路径对齐**,二选一,保持一致。

### 4.4 `/dev/*` 自助面的 client 栅栏(confused-deputy 硬前置)

开发者门户自助面(`/dev/*`,cmd/oauth)用**用户 JWT** 鉴权(不是 API key)。`middleware.Auth` 只验签名 + 用户在册未封,**不验这枚 access token 属于哪个 OAuth client**。若无额外栅栏,任何第三方 OAuth app 拿到的用户 token(哪怕只授了 `openid profile email`)都能替用户在 `/dev/*` 铸/轮换/吊销 API key——典型 confused-deputy。

因此 `middleware.Auth` 之后加一道 **`DevPortalFence`**(RFC 9068 的 `client_id` claim,pkg/utils/jwt.go 第一方为空):

- `client_id == ""`(第一方 `/auth/login` session token,无 OAuth client)→ **放行**;
- `client_id` ∈ 允许列表 → **放行**(即开发者门户自己的 confidential client);
- 其余 → **403** + `slog.Warn` 记被拒的 `client_id`(不记 token)。

**允许列表 = env `KUN_DEV_PORTAL_CLIENT_IDS`(CSV)**。**空 = fail-closed**:只放行第一方 token,任何 client token 一律 403(与 trust 侧 `KUN_TRUST_FORWARDER_CLIENT_IDS` 同形)。门户以 auth-code + PKCE 让用户登录时,token 带门户自己的 `client_id`,故门户 client 注册后须把它填进这个 env,否则门户用户会被自家栅栏 403。设在 **oauth 服务**上。

> **升级路径(第三方实际开放时)**:本栅栏是"只让门户自己进"的硬前置,不是细粒度授权。将来对任意第三方开放 `/dev/*` 时,新增 `dev:manage` scope + 同意页文案(第三方 app 须显式获用户同意才能管理其开发者资源),届时栅栏从"client 白名单"升级为"白名单 ∪ 持 `dev:manage` 的已同意 client"。本波不做。

### 4.5 `/dev/*` 上的三层门,各管一事(2026-08-18)

同一次自助调用最多穿过三道门,**互不替代**,顺序也不能换:

| 层 | 问的问题 | 判据 | 拒时 |
|---|---|---|---|
| **DevPortalFence**(§4.4) | 这枚用户 token 是**哪个 OAuth client** 签给谁的? | `client_id` claim ∈ 白名单(空=仅第一方) | 403,且**先于**一切业务逻辑 |
| **归属**(owner guard) | 这个应用是**调用者的**吗? | `oauth_clients.owner_user_id == uid` | 404(不泄露他人应用是否存在) |
| **平台策略矩阵**([02 §3.10](./02-public-api.md)) | 这件事**现在还开放**给开发者自助做吗? | `devapi_policy_overrides` / 代码默认 | 403(capability 关闭)或 409(应用未过审) |

**策略层永远排在归属之后**——否则「某功能已关闭」这句回答会告诉一个非 owner 目标应用确实存在。唯一例外是 `POST /dev/apps`:它没有归属可判,策略即第一道。(`POST /dev/scope-applications` 曾是第二个例外,随授权制于 2026-08-25 退役。)

策略层与 scope 判据([02 §3.9](./02-public-api.md))也不重叠:scope 决定**一把 key 能打开哪张面**,策略决定**开发者能不能自己铸这把 key**。前者写在凭据上,后者写在流程上。

---

## 7. 限流 + 配额 + 分层

**两件不同的事**:限流 = 短期防滥用(req/min);配额 = 业务上限(req/day)。**计数是平台级**(一把 key 在所有面共享同一份额度)。

| tier | rate/min | quota/day | 适用 |
|---|---|---|---|
| `free` | 60 | 50,000 | 默认,自助注册即得 |
| `trusted` | 600 | 1,000,000 | 邀请/审批的合作开发者(doc 19 D2:首批 = 友好 galgame 管理器项目) |
| `internal` | 不限 | 不限 | 一方应用(forum/moyu/letmoe;doc 19 W3 起 kungal/moyu 以此 tier 真实消费) |

> NSFW 曾是这张表的第四列(否 / 可申请 / 是),随能力位于 2026-08-25 一并退役:三个 tier 现在都能取 nsfw 内容。

> **tier 治理(D2 拍板)**:开发者身份与角色 = IdP 五全局角色(`docs/integration/oauth/11-roles.md`,冻结,不新增);tier / scope / 配额等**细粒度授权 = 开发者平台内部数据**(`oauth_clients.dev_*` + key 行),由平台管理面授予——与 permission-first 教义同构(角色只是权限捆的入口,代码只查权限)。

- Redis 实现:限流用滑动窗口(`ratelimit:{key}:{minute}`),配额用当日计数(`quota:{key}:{YYYY-MM-DD}`,TTL 到次日)。
- 响应头:`X-RateLimit-Limit/Remaining/Reset`、`Retry-After`、`X-Quota-Limit/Remaining`。门户实时显示剩余配额。
- 应用/key 上的 `DevRatePerMin`/`DevQuotaDaily` 为 0 时用 tier 默认值(同 `RefreshTokenTTL()` 的"0=用默认"范式)。
