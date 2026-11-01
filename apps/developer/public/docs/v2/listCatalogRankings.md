# Rank works by their ratings · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/catalog/rankings

Rank works by their ratings

Ranks works by their ratings on every source, or on one. Every source keeps its own published score (per_source[].score); next to it each entry carries what a fitted model makes of it. Per source, mean is the work's own mean pulled toward what works with as many votes usually score, so a work with three votes cannot outrank one with three thousand on luck, with a 90% interval and the work's percentile in that source. The consensus puts the sources on one scale: each source's estimate becomes a normal score within that source, a one-factor model fitted on the works several sources share corrects for each source's audience, and the sources are combined weighted by how much each one knows about the work. It is read as a deviation score (偏差値): 50 is the typical work rated by two or more sources and every 10 points is one standard deviation. The model is refitted from the data nightly and the ranking reads the search index, rebuilt nightly; model describes the fit in use. Every filter of GET /v2/catalog/works that the search index carries applies here, plus year=, circulation=, platform=, playtime_min/max=, vote floors and score bounds. page= selects page mode. Answers 503 until the first model is fitted. Anonymous calls are allowed and limited per IP address; an application key or a user access token with catalog:read gets its own, higher limit.

- 所属 API：Public API v2（/v2）
- 鉴权：可匿名调用
- scope：catalog:read（仅在带凭据时）

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `cursor` | query | 否 | string | Opaque cursor from a prior next_cursor. Must start with cur_. |
| `limit` | query | 否 | string | Page size 1-100, default 20. Values above 100 are 400 LIMIT_TOO_LARGE, not clamped. |
| `view` | query | 否 | string | basic (default) or full, for the work inside each entry. |
| `include` | query | 否 | string | Comma-separated blocks of the work inside each entry, as on GET /v2/catalog/works. Unknown token is 400 UNKNOWN_INCLUDE. |
| `include_total` | query | 否 | string | true to include total. Only true or false. |
| `facets` | query | 否 | string | Comma-separated facet names over the ranked population: tag_id, company_id, olang, content_rating, platform, circulation. |
| `nsfw` | query | 否 | string | true includes r18. false or absent hides r18. Only true or false. |
| `by` | query | 否 | string | consensus (default) ranks by every source combined; a source key (vndb, bangumi, dlsite, erogamescape, nextmoe, howlongtobeat) ranks by that source alone. |
| `metric` | query | 否 | string | The statistic to rank by. With by=consensus: lower, upper, mean, percentile, divergence, votes, sources. With a source: lower, upper, mean, median, native, percentile, spread, votes. The default is lower for order=desc and upper for order=asc: the bottom of the 90% interval for a best-of list, so a handful of enthusiastic votes cannot outrank an established work, and its top for a worst-of list, so a work is listed as bad only when it is bad with confidence. mean ranks by the best estimate; native by the source's own published score; divergence by how much the sources disagree. |
| `order` | query | 否 | string | desc (default, best first) or asc (worst first). |
| `sources` | query | 否 | string | by=consensus only: comma-separated source keys to build the consensus from, for example erogamescape,bangumi. Absent = every source. Every number in consensus then covers only these sources. |
| `min_sources` | query | 否 | string | by=consensus only: keep works at least this many of the consensus sources rated, 1-6. |
| `min_votes` | query | 否 | string | Vote floors, comma-separated. A bare number applies to by= (the total over the consensus sources for by=consensus); source:number applies to that source, for example erogamescape:50,bangumi:100. Every floor must hold. |
| `max_votes` | query | 否 | string | Vote ceilings, in the same form as min_votes, for lists of lesser-known works: min_votes=5&max_votes=50 keeps works between five and fifty votes. |
| `score_min` | query | 否 | string | Keep works whose value (in the list's unit) is at least this. |
| `score_max` | query | 否 | string | Keep works whose value (in the list's unit) is at most this. |
| `year` | query | 否 | string | Release year, or an inclusive range such as 2015-2020. Cannot be combined with released_after or released_before. |
| `released_after` | query | 否 | string | YYYY-MM-DD inclusive, on each work's release_date. |
| `released_before` | query | 否 | string | YYYY-MM-DD inclusive, on each work's release_date. |
| `circulation` | query | 否 | string | commercial: a game brand made or sold it. doujin: only a doujin circle did. Works with neither credit match neither. |
| `platform` | query | 否 | string | Comma-separated platform tokens (win, ps4, swi, …), any of. Open vocabulary; unknown values match nothing. |
| `playtime_min` | query | 否 | string | Minutes. Keep works whose playtime is at least this; playtime is the median of the sources' estimates. |
| `playtime_max` | query | 否 | string | Minutes. Keep works whose playtime is at most this. |
| `content_rating` | query | 否 | string | Closed: all_ages, sensitive, r18. r18 requires nsfw=true. |
| `content_limit` | query | 否 | string | Comma-separated closed editorial axis: sfw, nsfw. |
| `claimed` | query | 否 | string | true or false. Absent = no gate. |
| `company_id` | query | 否 | string | Catalog company id. |
| `tag_id` | query | 否 | string | Comma-separated canonical tag ids, AND, max 10. |
| `exclude_tag_id` | query | 否 | string | Comma-separated canonical tag ids, max 100. Drops every work that carries any of them. |
| `exclude_tag_tree` | query | 否 | string | Comma-separated canonical tag ids, max 100. As exclude_tag_id, and each id also drops the works carrying any tag below it. |
| `series_id` | query | 否 | string | Catalog series id. |
| `engine_id` | query | 否 | string | Catalog engine id. |
| `olang` | query | 否 | string | Comma-separated BCP-47, or all. Open vocabulary; unknown values match nothing. |
| `page` | query | 否 | string | 1-based page number. Selects page mode: the response carries total and total_relation and no next_cursor. page times limit may not exceed 10000; the last reachable page is min(ceil(total/limit), floor(10000/limit)). Cannot be combined with cursor, ids or refs. |

```bash
curl "https://api.nextmoe.dev/v2/catalog/rankings"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/listCatalogRankings
