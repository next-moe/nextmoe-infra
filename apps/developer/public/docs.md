# API 文档

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## 3 个 API

- [Public API v2](https://developer.nextmoe.dev/docs/v2.md) — `/v2`，113 个端点，Authorization: Bearer nmk_live_…
- [moyu 补丁面](https://developer.nextmoe.dev/docs/moyu.md) — `/v2/moyu`，4 个端点，Authorization: Bearer nmk_live_…
- [sticker 表情包面](https://developer.nextmoe.dev/docs/sticker.md) — `/v2/sticker`，9 个端点，Authorization: Bearer nmk_live_…

## OpenAPI 原文（机器可读）

- Public API v2：https://api.nextmoe.dev/v2/catalog/openapi.json
- moyu 补丁面：https://developer.nextmoe.dev/specs/moyu-openapi.yaml
- sticker 表情包面：https://developer.nextmoe.dev/specs/sticker-openapi.yaml

游玩时长与编辑提案两个用户面不提供公开 spec 文件，以本站 Markdown 参考为准。

## 鉴权模型

- 应用密钥（`Authorization: Bearer nmk_live_…`）——在 https://developer.nextmoe.dev 控制台自助创建应用与密钥，无需申请；自助可勾选的 scope 有 catalog:read 与 store:read，/v2/moyu/* 与 /v2/sticker/* 两个下游面不需要 scope、任意有效密钥可调。/v2 只收 nmk_ 前缀的密钥。
- 用户访问令牌（`Authorization: Bearer <access token>`）——/v2/me 与 /v2/moderation 读写的是某个用户自己的东西，用该用户经 OAuth 授权码 + PKCE 授权后的令牌，不是应用密钥。
- v1 已于 2026-08-27 全面退役：/v1/catalog、/v1/news、/v1/store、/v1/playtime、/api/v1/catalog 与 /api/v1/user/catalog 一律返回 410，Link 指向 /v2。
- `/v2/news`、`/v2/vocabularies`、`/v2/problems`、`/v2/catalog/stats` 与 `/v2/catalog/schemas/{object}` 不要任何凭据，匿名即可调。

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs
