# OAuth 服务 — PATCH API 清单

> 服务: **oauth**（`apps/api/cmd/oauth`） · Base URL: `/api/v1` · 路由源: `cmd/oauth/main.go`
>
> 图例见 [README](./README.md)。配套: [oauth.get.md](./oauth.get.md) · [oauth.post.md](./oauth.post.md) · [oauth.put.md](./oauth.put.md) · [oauth.delete.md](./oauth.delete.md)
>
> **审计完成** —— 已修 / 已审计无问题（本轮字段对齐/越权/SQL注入/副作用扫描未发现可处理问题）。详见 [README 审计结果](./README.md#审计结果2026-05-29)。

## 统计

- 本服务 PATCH 端点：**2**（认证-自助 1 · 管理-用户 1）
- 注：`/api/v1/admin/image/:hash/review` 物理上也跑在本进程，归到 [image.patch.md](./image.patch.md)。

---

## 1. 认证 — 自助（登录态）

| 路径 | 鉴权 | Handler | 状态 | 备注 |
|---|---|---|---|---|
| `PATCH /api/v1/auth/me` | 登录 | `authH.UpdateProfile` | 已审计 | 改 name / avatar / bio（展示层）|

## 2. 管理 — 用户

| 路径 | 鉴权 | Handler | 状态 | 备注 |
|---|---|---|---|---|
| `PATCH /api/v1/admin/users/:uuid` | admin | `adminH.UpdateUser` | 已修 | 改 name/email/avatar/bio/status；#31 not-found→404；#15 avatar_image_hash；#11 转封禁时撤销会话；2026-09-08 补 F001 残留：目标为受保护账号(admin/**ren**)时非 `roles.grant_admin` 持有者一律 403，`email` 读写同归 `users.pii_view`(响应也脱敏)，写入前 `NormalizeEmail` |
