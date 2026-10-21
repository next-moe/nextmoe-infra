# List my deletion requests · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/me/deletion-requests

List my deletion requests

The bearer's own deletion requests, newest first; follow next_cursor for more. Requires a user access token bound to a first-party catalog site. The token must carry the catalog:edit scope.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `state` | query | 否 | string | Narrow to one state. 取值：open \| approved \| executed \| rejected \| withdrawn \| restored |
| `target_id` | query | 否 | string | Narrow to requests about one work. |
| `origin_site` | query | 否 | string | Narrow to requests filed from one first-party property. 取值：kungal \| moyu \| letmoe \| nextmoe \| editor |
| `cursor` | query | 否 | string | Opaque keyset cursor from next_cursor. |
| `limit` | query | 否 | integer (int64) | Page size, 1-100. Default 20. |

```bash
curl "https://api.nextmoe.dev/v2/me/deletion-requests" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/listMyDeletionRequests
