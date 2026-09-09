# Which of my folders hold these works · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/me/folders/holdings

Which of my folders hold these works

Membership for up to 100 works in one request: for each work the bearer keeps in at least one folder, the ids of those folders. A work the bearer holds nowhere is left out rather than answered with an empty array, and an id that names no work is simply held nowhere. Folders of every visibility are searched — the bearer owns them all. work_ids is required; this is a batch read with no pagination. Requires a user access token with folder:read (folder:write also grants reads).

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：folder:read 或 folder:write

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `work_ids` | query | 否 | string | Comma-separated work ids, max 100. Batch read, no pagination. |

```bash
curl "https://api.nextmoe.dev/v2/me/folders/holdings" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/listMyFolderHoldings
