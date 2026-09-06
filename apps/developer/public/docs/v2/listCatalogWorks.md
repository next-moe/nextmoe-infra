# List catalog works · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/catalog/works

List catalog works

Keyset-paginated work collection. q= switches to search (sort defaults to relevance). company_id=/tag_id=/series_id= filter the live registry when q= is absent. Requires an application key or a user access token with catalog:read. view/include/fields/ids/refs/facets follow the v2 collection contract. include=titles,refs,intros,covers,companies,ratings,tags,credits fills on every lane; view=full is all of them except credits, which is an explicit ask. On a collection lane titles elects latin/localized and covers elects the two cover slots that grade the base cover — the full titles[] and covers[] arrays, and relations/releases/popularity/playtimes/series/platforms/screenshots/characters/engines/links, are per-record blocks and live on /v2/catalog/works/{id} and its sub-resources; asking for one here is 400 UNKNOWN_INCLUDE.

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
| `q` | query | 否 | string | Work title search. Switches this collection to the search index; sort defaults to relevance. Must not be used as a discriminant. |
| `content_rating` | query | 否 | string | Closed: all_ages, sensitive, r18. r18 requires nsfw=true. |
| `claimed` | query | 否 | string | true or false. Absent = no gate. |
| `claim_state` | query | 否 | string | Comma-separated closed states: none, live, draft. pending, declined and hidden are the per-site moderation queue: they need a key holding claim_events:read and a site= naming the caller's own site, and are otherwise not in the vocabulary. |
| `content_limit` | query | 否 | string | Comma-separated closed editorial axis: sfw, nsfw. |
| `site` | query | 否 | string | Claiming site key. Open vocabulary; unknown values match nothing. |
| `owner_uid` | query | 否 | string | The claiming site's own user id of the claim owner. Requires site=. Live registry filter; cannot be combined with q= or search sorts. |
| `company_id` | query | 否 | string | Catalog company id. Live registry filter when q= is absent. |
| `company_rollup` | query | 否 | string | true expands company_id one hop down imprint/subsidiary. Only true or false. |
| `tag_id` | query | 否 | string | Comma-separated canonical tag ids, AND, max 10. |
| `series_id` | query | 否 | string | Catalog series id. |
| `engine_id` | query | 否 | string | Catalog engine id. |
| `platform` | query | 否 | string | Open vocabulary platform token. Unknown matches nothing. |
| `released_after` | query | 否 | string | YYYY-MM-DD inclusive, earliest release per work. |
| `released_before` | query | 否 | string | YYYY-MM-DD inclusive, earliest release per work. |
| `olang` | query | 否 | string | Comma-separated BCP-47, or all. Open vocabulary; unknown values match nothing. Absent = no language gate. |

```bash
curl "https://api.nextmoe.dev/v2/catalog/works" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/listCatalogWorks
