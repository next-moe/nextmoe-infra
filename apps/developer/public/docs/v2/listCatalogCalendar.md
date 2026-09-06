# Release calendar · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/catalog/calendar

Release calendar

One collection. month=/year= pick a window; precision= and status= select among the dated month, year-only, and undated views that were three v1 routes. content_limit= gates on the editorial display axis and olang= on the original language (absent = ja plus zh). meta carries today plus, on the dated month window, min_month/max_month/has_prev/has_next for month navigation. Requires an application key or a user access token with catalog:read. ids= is not accepted. include=titles,refs,intros,covers,companies,ratings,tags,credits fills on this lane; view=full is all of them except credits, which is an explicit ask. On a collection lane titles elects latin/localized and covers elects the two cover slots that grade the base cover — the full titles[] and covers[] arrays, and relations/releases/popularity/playtimes/series/platforms/screenshots/characters/engines/links, are per-record blocks and live on /v2/catalog/works/{id} and its sub-resources; asking for one here is 400 UNKNOWN_INCLUDE.

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
| `month` | query | 否 | string | Dated month window YYYY-MM. Default: current month in Asia/Tokyo. |
| `year` | query | 否 | string | Year-only window YYYY (v1 pending). Default with precision=year: current year in Asia/Tokyo. |
| `precision` | query | 否 | string | day, month, or year. year selects the year-only window. day and month use the dated month window. |
| `status` | query | 否 | string | released, dated, announced, cancelled, unknown. announced and unknown select the undated window. cancelled is empty until the catalog records cancellations. |
| `content_limit` | query | 否 | string | Comma-separated closed editorial axis: sfw, nsfw. |
| `olang` | query | 否 | string | Comma-separated BCP-47, or all. Open vocabulary; unknown values match nothing. Absent = the calendar's home population, ja plus zh. |

```bash
curl "https://api.nextmoe.dev/v2/catalog/calendar" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/listCatalogCalendar
