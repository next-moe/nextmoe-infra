# Edit proposal history · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/catalog/proposals

Edit proposal history

Filed proposals, newest first. proposer_uid=+state=merged with include_total=true is the per-contributor tally. This face publishes no patch and no decision note. Requires an application key or a user access token with catalog:read.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer nmk_live_…
- scope：catalog:read

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `cursor` | query | 否 | string | Opaque keyset cursor from a prior next_cursor. Must start with cur_. |
| `limit` | query | 否 | string | Page size 1-100, default 20. Values above 100 are 400 LIMIT_TOO_LARGE, not clamped. |
| `view` | query | 否 | string | basic (default) or full. Closed vocabulary. |
| `include` | query | 否 | string | Comma-separated blocks. amendments is the only token on this face. Unknown token is 400 UNKNOWN_INCLUDE. |
| `fields` | query | 否 | string | Comma-separated top-level keys after view/include. Unknown token is 400 UNKNOWN_FIELD. object and id are always kept. |
| `ids` | query | 否 | string | Comma-separated proposal ids, max 100. Batch lane: no pagination. |
| `include_total` | query | 否 | string | true to include total. Only true or false. |
| `sort` | query | 否 | string | filed_desc is the only key. Closed. |
| `object` | query | 否 | string | Closed family filter: work, company, character, release, tag, engine, series. |
| `entity_id` | query | 否 | string | Catalog id of one entity. Requires object=. |
| `site` | query | 否 | string | Tenant key. Open vocabulary; unknown values match nothing. |
| `proposer_uid` | query | 否 | string | The claiming site's own user id of the proposer. Not a catalog id. |
| `state` | query | 否 | string | Closed: open, merged, declined, withdrawn. |

```bash
curl "https://api.nextmoe.dev/v2/catalog/proposals" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/listCatalogProposals
