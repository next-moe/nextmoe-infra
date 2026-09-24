# OAuth 服务 — POST API 清单

> 服务: **oauth**（`apps/api/cmd/oauth`） · Base URL: `/api/v1` · 路由源: `cmd/oauth/main.go`
>
> 鉴权/状态图例见 [README](./README.md)。配套: [oauth.get.md](./oauth.get.md) · [oauth.put.md](./oauth.put.md) · [oauth.delete.md](./oauth.delete.md) · [oauth.patch.md](./oauth.patch.md)
>
> **审计完成** —— 已修 / 已审计无问题（本轮字段对齐/越权/SQL注入/副作用扫描未发现可处理问题）。详见 [README 审计结果](./README.md#审计结果2026-05-29)。

## 统计

- 本服务 POST 端点：**21**
  - 认证（公开）6 · 认证（自助）3 · OAuth 协议 3 · 用户 1 · 管理-用户 5 · 管理-站点/客户端 2 · 管理-任务 1
- 注：`strict` = 严格限流中间件（非鉴权）。

---

## 1. 认证 — 公开（注册 / 登录 / 找回）

| 路径 | 鉴权 | Handler | 状态 | 备注 |
|---|---|---|---|---|
| `POST /api/v1/auth/register/send-code` | 公开 +strict | `authH.SendRegisterCode` | 已审计 | 注册邮箱验证码 |
| `POST /api/v1/auth/register` | 公开 +strict | `authH.Register` | 已审计 | 注册（需验证码）|
| `POST /api/v1/auth/login` | 公开 +strict | `authH.Login` | 已审计 | 登录（封禁用户拒）|
| `POST /api/v1/auth/refresh` | 公开 | `authH.Refresh` | 已修 | 用 httpOnly refresh cookie 续 access_token；#11 封禁用户拒发新 token 并撤销会话 |
| `POST /api/v1/auth/password/forgot` | 公开 +strict | `authH.ForgotPassword` | 已审计 | 发重置邮件 |
| `POST /api/v1/auth/password/reset` | 公开 +strict | `authH.ResetPassword` | 已修 | 用 token 重置；#27 基础设施错误→500(非误导性400)；#28 先原子消费 token，消除重放窗口 |

## 2. 认证 — 自助（登录态）

| 路径 | 鉴权 | Handler | 状态 | 备注 |
|---|---|---|---|---|
| `POST /api/v1/auth/logout` | 登录 | `authH.Logout` | 已审计 | |
| `POST /api/v1/auth/email/send-code` | 登录 | `authH.SendEmailChangeCode` | 已审计 | 改邮箱验证码 |
| `POST /api/v1/auth/me/deletion/send-code` | 登录（仅账号站会话，OAuth token 403） | `authH.SendDeletionCode` | 已审计 | 注销验证码，发往当前邮箱；admin/ren 拒绝 |
| `POST /api/v1/auth/me/deletion` | 登录（仅账号站会话） | `authH.RequestDeletion` | 已审计 | 凭验证码申请注销，7 天冷静期后执行（不可逆）|
| `POST /api/v1/auth/me/avatar` | 登录 | `avatarUploadH.UploadMine` | 已审计 | 仅 image client 配置时注册 |

## 3. OAuth 2.0 协议

| 路径 | 鉴权 | Handler | 状态 | 备注 |
|---|---|---|---|---|
| `POST /api/v1/oauth/token` | ClientAuth | `oauthH.Token` | 已审计 | token 端点（client 凭证 + 限流 `oauthTokenLimiter`）；含授权码兑换/refresh grant，已加 banned 检查 |
| `POST /api/v1/oauth/revoke` | 公开 | `oauthH.Revoke` | 已审计 | 吊销 token（凭 token 本身）|
| `POST /api/v1/oauth/authorize/consent` | 登录 | `oauthH.Consent` | 已审计 | 同意授权 → 下发 code |

## 4. 用户（服务到服务）

| 路径 | 鉴权 | Handler | 状态 | 备注 |
|---|---|---|---|---|
| `POST /api/v1/users/:id/moemoepoint` | ClientAuth | `moemoepointH.Adjust` | 已审计 | 发放/回收（幂等）；只接受 content_approved / content_removed / daily_checkin / liked |
| `POST /api/v1/users/:id/moemoepoint/charges` | ClientAuth | `moemoepointH.Charge` | 新增 | 扣费（幂等）；服务端校验余额，不足 400/16006 且不扣 |
| `POST /api/v1/users/:id/moemoepoint/reversals` | ClientAuth | `moemoepointH.Reverse` | 新增 | 按原幂等键撤销本 client 的一笔记录（每笔至多一次）|
| `POST /api/v1/shop/orders` | 登录（第一方）| `shopH.Purchase` | 新增 | 商店购买：一个事务内扣费 + 发放，幂等键按用户唯一 |

## 5. 管理 — 用户

| 路径 | 鉴权 | Handler | 状态 | 备注 |
|---|---|---|---|---|
| `POST /api/v1/admin/users/:uuid/ban` | admin | `adminH.BanUser` | 已审计 | 封禁 + 清会话 |
| `POST /api/v1/admin/users/:uuid/unban` | admin | `adminH.UnbanUser` | 已审计 | 解封（拒已匿名化）|
| `POST /api/v1/admin/users/:uuid/anonymize` | admin | `adminH.AnonymizeUser` | 已审计 | PII 清洗 + 封禁 + 头像 GC（不可逆）|
| `POST /api/v1/admin/users/:uuid/moemoepoint` | admin | `moemoepointH.AdminAdjust` | 已审计 | 管理员发放/扣除（reason 按 delta 正负派生）|
| `POST /api/v1/admin/users/:uuid/avatar` | admin | `avatarUploadH.Upload` | 已审计 | 仅 image client 配置时注册 |

## 6. 管理 — 站点 / OAuth 客户端

| 路径 | 鉴权 | Handler | 状态 | 备注 |
|---|---|---|---|---|
| `POST /api/v1/sites` | admin | `siteH.Create` | 已审计 | |
| `POST /api/v1/oauth/clients` | admin | `siteH.CreateClient` | 已修 | #32 grants 校验(非空+枚举子集) |

## 7. 管理 — 任务

| 路径 | 鉴权 | Handler | 状态 | 备注 |
|---|---|---|---|---|
| `POST /api/v1/admin/jobs/:name/run` | admin | inline（`registerJobsAdmin`）| 已审计 | 手动触发 job（后台运行）|
