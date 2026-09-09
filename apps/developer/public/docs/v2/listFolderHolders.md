# Who holds this work in a folder · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/folders/holders

Who holds this work in a folder

The accounts that keep one work in a favorite folder, owner_uid-ascending, one page at a time. Folders of every visibility count: this face exists so a service can fan a notification out to the people who follow a work, and a private folder is still a person waiting to hear about it. It answers uids and nothing else — no folder ids, names, visibility or counts — so it cannot be walked into a "who favourited what" index. A work nobody holds is an empty list, not a 404. Requires an application key with the folder_holders:read scope on top of catalog:read; a user access token is refused. The scope is granted by an operator, not self-service, because the answer is somebody else's private collection.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer nmk_live_…
- scope：catalog:read + folder_holders:read

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `work_id` | query | 否 | string | Catalog work id. Required. |
| `cursor` | query | 否 | string | Opaque keyset cursor from a prior next_cursor. Must start with cur_. |
| `limit` | query | 否 | string | Page size 1-100, default 100. Values above 100 are 400 LIMIT_TOO_LARGE, not clamped. |

```bash
curl "https://api.nextmoe.dev/v2/folders/holders" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/listFolderHolders
