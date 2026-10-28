# List catalog works · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/catalog/works

List catalog works

Keyset-paginated work collection. q= switches to search (sort defaults to relevance). company_id=/tag_id=/series_id= filter the live registry when q= is absent. exclude_tag_id=/exclude_tag_tree= drop works by tag on every lane, search included. Anonymous calls are allowed and limited per IP address; an application key or a user access token with catalog:read gets its own, higher limit. view/include/fields/ids/refs/facets follow the v2 collection contract. include= takes titles, refs, intros, covers, companies, ratings, tags, credits, playtimes. view=full is titles, refs, intros, covers, companies, ratings, tags; credits, playtimes come only when include= names them. Every block fills on every lane, the ids=/refs= batch lane included. On a collection lane titles elects latin/localized and covers elects the two cover slots that grade the base cover — the full titles[] and covers[] arrays, and relations, releases, popularity, series, platforms, screenshots, characters, engines, links, are per-record blocks and live on /v2/catalog/works/{id} and its sub-resources; asking for one here is 400 UNKNOWN_INCLUDE. page= selects page mode (see the page parameter). sort=rating_bangumi, rating_vndb, rating_erogamescape, rating_dlsite, rating_nextmoe each rank by one source's rating, highest first, and read the search index, which is rebuilt nightly. Sources are never combined; each keeps its own scale. bangumi, erogamescape and dlsite rank by (10·m + n·score) / (10 + n), where n is that source's vote_count and m its mean score over the indexed works, which pulls a work with few votes toward m; vndb and nextmoe already publish a Bayesian score and rank by it as published. Works the source has not rated come last, and ties break by id ascending.

- 所属 API：Public API v2（/v2）
- 鉴权：可匿名调用
- scope：catalog:read（仅在带凭据时）

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
| `owner_uid` | query | 否 | string | The claiming site's own user id of the claim owner. Requires site=. Live registry filter; cannot be combined with q=, page= or search sorts. |
| `company_id` | query | 否 | string | Catalog company id. Live registry filter when q= is absent. |
| `company_rollup` | query | 否 | string | true expands company_id one hop down imprint/subsidiary. Only true or false. |
| `tag_id` | query | 否 | string | Comma-separated canonical tag ids, AND, max 10. |
| `exclude_tag_id` | query | 否 | string | Comma-separated canonical tag ids, max 100. Drops every work that carries any of them, from any source and at any spoiler level. ANDed with the other filters, tag_id included: a tag named in both leaves nothing. |
| `exclude_tag_tree` | query | 否 | string | Comma-separated canonical tag ids, max 100. As exclude_tag_id, and each id also drops the works carrying any tag below it in the tag hierarchy (Tag.descendant_count counts them). |
| `series_id` | query | 否 | string | Catalog series id. |
| `engine_id` | query | 否 | string | Catalog engine id. |
| `platform` | query | 否 | string | Open vocabulary platform token. Unknown matches nothing. |
| `released_after` | query | 否 | string | YYYY-MM-DD inclusive, on each work's release_date. |
| `released_before` | query | 否 | string | YYYY-MM-DD inclusive, on each work's release_date. |
| `olang` | query | 否 | string | Comma-separated BCP-47, or all. Open vocabulary; unknown values match nothing. Absent = no language gate. |
| `search_mode` | query | 否 | string | lexical (default) ranks q= by title and alias matching. hybrid also matches q= by meaning against each work's titles, tags and intro, so a description such as a half-remembered premise finds the work; it requires q=, ranks at most 100 hits, and cannot be combined with facets= or a sort other than relevance. The list's search_mode reports the mode that actually ranked the page: hybrid falls back to lexical when the semantic side is unavailable. 取值：lexical \| hybrid |
| `page` | query | 否 | string | 1-based page number. Selects page mode: the response carries total and total_relation and no next_cursor. page times limit may not exceed 10000; the last reachable page is min(ceil(total/limit), floor(10000/limit)). Cannot be combined with cursor, ids or refs. |

```bash
curl "https://api.nextmoe.dev/v2/catalog/works"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/listCatalogWorks
