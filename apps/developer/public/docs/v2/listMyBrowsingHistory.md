# List my browsing history · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/me/history

List my browsing history

The works the bearer opened, newest first. Browsing history is kept per app: each app reads and deletes what it recorded, and entries older than 90 days or past the newest 500 per app are deleted. An account that switched browsing_history off on PATCH /v2/me/privacy has no history on the server. scope=all merges every app's entries per work and is 403 BROWSING_HISTORY_NOT_SHARED unless the account set browsing_history_sharing to shared, and 403 SCOPE_REQUIRED without history:read. Works that are no longer live in the catalog are left out. Items carry ids only; read the works themselves from /v2/catalog/works?ids=. Requires a user access token. With the default scope=app any app may call this and no scope is required.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `cursor` | query | 否 | string | Opaque keyset cursor from a prior next_cursor. Must start with cur_. |
| `limit` | query | 否 | string | Page size 1-100, default 20. Values above 100 are 400 LIMIT_TOO_LARGE, not clamped. |
| `view` | query | 否 | string | basic (default) or full. Closed vocabulary. |
| `include` | query | 否 | string | Comma-separated blocks. Unknown token is 400 UNKNOWN_INCLUDE. |
| `fields` | query | 否 | string | Comma-separated top-level keys after view/include. Unknown token is 400 UNKNOWN_FIELD. object and id are always kept. |
| `ids` | query | 否 | string | Comma-separated ids, max 100. Batch lane: no pagination. |
| `refs` | query | 否 | string | Comma-separated source:external_id, max 100. Batch lane: no pagination. |
| `include_total` | query | 否 | string | true to include total. Only true or false. |
| `facets` | query | 否 | string | Comma-separated facet names. Unknown token is 400 UNKNOWN_FACET. |
| `sort` | query | 否 | string | Closed per-collection sort key. |
| `nsfw` | query | 否 | string | true includes r18. false or absent hides r18. Only true or false. |
| `scope` | query | 否 | string | app (the default) acts on the history this app recorded. all acts on every app's history merged per work: listing it needs the history:read scope and the account's browsing_history_sharing set to shared; deleting it needs neither. |
| `content_limit` | query | 否 | string | Comma-separated closed editorial axis: sfw, nsfw, as on /v2/catalog/works. Absent applies no gate. A site that only shows sfw works passes sfw, so a work the account opened on an adult site never reaches its page. total counts after it. |

```bash
curl "https://api.nextmoe.dev/v2/me/history" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/listMyBrowsingHistory
