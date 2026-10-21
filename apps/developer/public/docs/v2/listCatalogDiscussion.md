# Discussion under a proposal or request · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/catalog/discussions/{object}/{id}

Discussion under a proposal or request

Posts oldest first; follow next_cursor for newer ones. A proposal's thread is public like the proposal; a merge request's is readable by its requester and by catalog.merge.review holders. With a user token the page carries viewer: whether the caller may post or moderate here, and whether the thread reaches their notices. Anonymous calls are allowed and limited per IP address; they read public threads only.

- 所属 API：Public API v2（/v2）
- 鉴权：可匿名调用
- scope：catalog:read（仅在带凭据时）

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `object` | path | 是 | string | Object discriminant of the anchor: proposal, merge_request, or one a later request type registers. |
| `id` | path | 是 | string | Id of the proposal or request. |
| `cursor` | query | 否 | string | Opaque cursor from next_cursor. |
| `limit` | query | 否 | integer (int64) | Page size, 1-100. Default 50. |

```bash
curl "https://api.nextmoe.dev/v2/catalog/discussions/example/1"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/listCatalogDiscussion
