# Work split feed · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/catalog/splits

Work split feed

Executed and undone splits, oldest change first. Keyset-paginated. A split moves releases from original_id to a new work: the new id also appears on /v2/catalog/changes, and the moved releases keep their ids. A mirror that hangs its own data off releases or editions should follow them to new_work_id. An undo re-surfaces the split with state=undone: the releases are back on original_id and new_work_id redirects there (/v2/catalog/redirects). Anonymous calls are allowed and limited per IP address; an application key or a user access token with catalog:read gets its own, higher limit.

- 所属 API：Public API v2（/v2）
- 鉴权：可匿名调用
- scope：catalog:read（仅在带凭据时）

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `cursor` | query | 否 | string | Opaque keyset cursor from next_cursor. Omit to start from the first split. |
| `limit` | query | 否 | integer (int64) | Page size, 1-100. Default 50. |

```bash
curl "https://api.nextmoe.dev/v2/catalog/splits"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/listCatalogSplits
