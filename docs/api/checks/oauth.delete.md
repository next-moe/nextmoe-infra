# OAuth 服务 — DELETE API 清单

> 服务: **oauth**（`apps/api/cmd/oauth`） · Base URL: `/api/v1` · 路由源: `cmd/oauth/main.go`
>
> 图例见 [README](./README.md)。配套: [oauth.get.md](./oauth.get.md) · [oauth.post.md](./oauth.post.md) · [oauth.put.md](./oauth.put.md) · [oauth.patch.md](./oauth.patch.md)
>
> **审计完成** —— 已修 / 已审计无问题（本轮字段对齐/越权/SQL注入/副作用扫描未发现可处理问题）。详见 [README 审计结果](./README.md#审计结果2026-05-29)。

## 统计

- 本服务 DELETE 端点：**4**（当前用户 1 · 管理-用户 1 · 管理-站点/客户端 2）
- 注：`/api/v1/admin/image/:hash` 物理上也跑在本进程，归到 [image.delete.md](./image.delete.md)。

---

## 0. 当前用户

| 路径 | 鉴权 | Handler | 状态 | 备注 |
|---|---|---|---|---|
| `DELETE /api/v1/auth/me/deletion` | 登录（仅账号站会话） | `authH.CancelDeletion` | 已审计 | 冷静期内撤销注销；幂等 |

## 1. 管理 — 用户

| 路径 | 鉴权 | Handler | 状态 | 备注 |
|---|---|---|---|---|
| `DELETE /api/v1/admin/users/:uuid/sessions` | admin | `adminH.DeleteUserSessions` | 已审计 | 强制下线（清所有会话）|

## 2. 管理 — 站点 / OAuth 客户端

| 路径 | 鉴权 | Handler | 状态 | 备注 |
|---|---|---|---|---|
| `DELETE /api/v1/sites/:id` | admin | `siteH.Delete` | 已修 | #16 站点下有客户端时返回可读 400(预检) 而非 FK 500 |
| `DELETE /api/v1/oauth/clients/:id` | admin | `siteH.DeleteClient` | 已修 | 2026-08-29:此前**无任何守卫**地物理删行,绕过开发者平台那七个条件;现与 `DELETE /admin/devapi/apps/:client_id` 共用 `devapi.Repository.EnsureDeletable`,被引用/能签用户/未归档的开发者应用一律 409。见 [开发者平台 02 §3.11](../../developer-platform/02-public-api.md) |
