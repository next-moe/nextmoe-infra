# OAuth 服务 — GET API 清单

> 服务: **oauth**（`apps/api/cmd/oauth`） · Base URL: `/api/v1`
>
> 路由源: `apps/api/cmd/oauth/main.go`（含 `registerJobsAdmin` / `registerImageAdmin` 两个 helper）
>
> 配套: [image.get.md](./image.get.md) · [galgame.get.md](./galgame.get.md) · [moderation.get.md](./moderation.get.md) · [artifact.get.md](./artifact.get.md)
>
> **审计完成** —— 已修 / 已审计无问题（本轮字段对齐/越权/SQL注入/副作用扫描未发现可处理问题）。详见 [README 审计结果](./README.md#审计结果2026-05-29)。

## 图例 — 审计状态

- 已审计：对齐无问题
- 已修：已审计，发现问题并修复
- 保持：已审计，有意保持当前行为
- 待审计：inventory 默认

## 图例 — 鉴权

| 标记 | 中间件 | 含义 |
|---|---|---|
| 公开 | （无） | 完全公开 |
| OptionalJWT | `OptionalJWT` | 可选鉴权；带 token 附加内容，匿名只看公共部分 |
| 登录 | `Auth` | 必须登录。**oauth 的 `Auth` 会在每次请求查 DB user 状态**（封禁/匿名化即 403），非仅验签 |
| admin | `Auth` + `RequireRole("admin")` | 仅 admin |
| ClientAuth | `OAuthClientBasicAuth` | 服务到服务（client_id:secret），非终端用户 JWT |

## 统计

- 本服务 GET 端点：**20**
  - 运维 1 · 认证/身份 2 · OAuth 协议 3 · 用户（s2s + self）5 · 管理-用户 3 · 管理-站点/客户端 4 · 管理-任务 2
  - 新增 `GET /auth/me/moemoepoint/log`：本轮新增的用户自助流水端点（web /profile「萌萌点记录」）。
- 注：`/admin/image/*` 物理上也跑在本进程，但归到 [image.get.md](./image.get.md) 审计。

---

## 0. 运维 / 健康

| 路径 | 鉴权 | Handler | 状态 | 备注 |
|---|---|---|---|---|
| `GET /healthz` | 公开 | inline | 已审计 | 健康检查（root liveness，无 `/api/v1` 前缀；见 `cmd/oauth/main.go`） |

## 1. 认证 / 身份

| 路径 | 鉴权 | Handler | 状态 | 备注 |
|---|---|---|---|---|
| `GET /api/v1/auth/me` | 登录 | `authH.Me` | 已修 | 当前登录用户 profile；#01 返回 avatar_image_hash |
| `GET /api/v1/auth/me/moemoepoint/log` | 登录 | `moemoepointH.MyLog` | 新增 | 自助：用户查自己的萌萌点流水（**精简视图**，id 取 JWT 非路径参）。web /profile「萌萌点记录」用 |

## 2. OAuth 2.0 协议

| 路径 | 鉴权 | Handler | 状态 | 备注 |
|---|---|---|---|---|
| `GET /api/v1/oauth/authorize` | 公开 | `oauthH.Authorize` | 已审计 | 授权入口；内部校验 session |
| `GET /api/v1/oauth/client-info` | 公开 | `oauthH.GetClientPublic` | 已审计 | 客户端公开信息（前端决定是否显示同意页）|
| `GET /api/v1/oauth/userinfo` | 登录 | `oauthH.UserInfo` | 已修 | OIDC userinfo；按 scope 投影；#13 始终回 updated_at；#14 picture 用 avatar_image_hash 解析 CDN URL |

## 3. 用户（服务到服务 + 自助）

| 路径 | 鉴权 | Handler | 状态 | 备注 |
|---|---|---|---|---|
| `GET /api/v1/users/batch` | ClientAuth | `userBatchH.Get` | 已审计 | 按 id 批量拉公开资料；不含 email/moemoepoint；已注销账号带 `anonymized_at` |
| `GET /api/v1/users/deleted` | ClientAuth | `userBatchH.Deleted` | 已审计 | 已注销账号的增量流（按注销时间，游标分页），供下游清理本地数据 |
| `GET /api/v1/users/search` | ClientAuth | `userBatchH.Search` | 已修 | 用户名子串搜索；#30 q 长度按字符(rune)计，满长 CJK 名不再误拒 400 |
| `GET /api/v1/users/:id/moemoepoint` | ClientAuth | `moemoepointH.GetBalance` | 已审计 | 余额（统一货币）|
| `GET /api/v1/users/:id/moemoepoint/log` | ClientAuth | `moemoepointH.GetLog` | 已审计 | 流水**精简视图**（无 note/actor）|
| `GET /api/v1/users/:uuid` | 登录 | `authH.GetProfile` | 已修 | 按 uuid 取 profile；#12 去除 PII(email/status)；#29 预载 roles；#01 hash |

## 4. 管理 — 用户

| 路径 | 鉴权 | Handler | 状态 | 备注 |
|---|---|---|---|---|
| `GET /api/v1/admin/users` | admin | `adminH.ListUsers` | 已修 | 分页 + 搜索；#02 sort_by ORDER BY 注入(repo 白名单+handler 校验)；#15 avatar_image_hash |
| `GET /api/v1/admin/users/:uuid` | admin | `adminH.GetUser` | 已修 | 含 session 数等；#15 avatar_image_hash |
| `GET /api/v1/admin/users/:uuid/moemoepoint/log` | admin | `moemoepointH.AdminGetLog` | 已审计 | 流水**完整视图**（含 note/actor）|

## 5. 管理 — 站点 / OAuth 客户端

| 路径 | 鉴权 | Handler | 状态 | 备注 |
|---|---|---|---|---|
| `GET /api/v1/sites` | admin | `siteH.List` | 已审计 | |
| `GET /api/v1/sites/:id` | admin | `siteH.Get` | 已审计 | |
| `GET /api/v1/sites/:id/clients` | admin | `siteH.GetSiteClients` | 已审计 | 站点下的 OAuth 客户端 |
| `GET /api/v1/oauth/clients` | admin | `siteH.ListClients` | 已审计 | 全部客户端 |

## 6. 管理 — 任务（infra，跑在本进程）

| 路径 | 鉴权 | Handler | 状态 | 备注 |
|---|---|---|---|---|
| `GET /api/v1/admin/jobs` | admin | inline（`registerJobsAdmin`）| 已审计 | 注册的 job + 各自最近一次 run（路由实参为空字符串）|
| `GET /api/v1/admin/jobs/:name/runs` | admin | inline | 已审计 | 某 job 的运行历史（`?limit`）|
