# List an account's play records · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/work-records

List an account's play records

The works one account has a play state on — wish, doing, done, on_hold or dropped — each with its playtime and rating, the shape /v2/me/work-records/{work_id} answers its owner. This is the face a profile page's game library reads. Newest first by the play state's updated_at; a work with only a playtime or only a rating is not listed. Works that are no longer live in the catalog are left out. facets=state counts the works per state after the same filters as the items, state= included, as every v2 facet does: all five states are present, 0 when empty. Ask for it on a request without state= to fill a row of tabs. include_total counts the items. An account that keeps its play records private (PATCH /v2/me/privacy) is 403 PLAY_RECORDS_PRIVATE, except to its own user token; the account reads its own list, private or not, on /v2/me/work-records, which needs no scope. Play records are public by default. Requires an application key or a user access token with catalog:read.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer nmk_live_…
- scope：catalog:read

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
| `state` | query | 否 | string | Comma-separated closed vocabulary: wish, doing, done, on_hold, dropped. Absent lists every state. |
| `content_limit` | query | 否 | string | Comma-separated closed editorial axis: sfw, nsfw, as on /v2/catalog/works. Absent applies no gate. total and the state facet count after it, so a reader who hides nsfw works gets counts that match what it shows. |
| `user_id` | query | 是 | string | The account whose play records to list — the central sign-in user id, the same one a rating reports as user_id. |

```bash
curl "https://api.nextmoe.dev/v2/work-records" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/listWorkRecords
