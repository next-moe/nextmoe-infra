# My folders, playtime and play state, per work · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/me/works

My folders, playtime and play state, per work

What the bearer has recorded about works: the folders holding each work, its playtime and its play state. With work_ids, up to 100 works in one request with no pagination: one item per distinct work id, in the order asked. A work the bearer has recorded nothing about still gets an item, with empty folder_ids and null playtime and work_state, and so does an id that names no work. Without work_ids, every work the bearer holds in a folder of their own, has a playtime on, or has a play state on, one item per work in ascending work id, paged with cursor and limit; include_total counts them. The values are the ones /v2/me/folders/holdings, /v2/me/playtimes and /v2/me/work-states answer; this face saves a client from asking all three. Cover votes are not included: they need catalog:edit, and /v2/me/cover-votes lists them. Requires a user access token with folder:read (folder:write also grants reads).

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：folder:read 或 folder:write

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
| `work_ids` | query | 否 | string | Comma-separated work ids, max 100. Batch read, no pagination. Absent walks every work the bearer has recorded anything about. |

```bash
curl "https://api.nextmoe.dev/v2/me/works" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/listMyWorks
