# List traits · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/catalog/traits

List traits

Keyset-paginated character traits. Requires an application key or a user access token with catalog:read. ids= is a batch lane. refs= is not resolved: traits have no catalog_external_ref entity_type. parent_id= lists direct children; group_id= lists traits in that root group (the root excluded); root=true|false keeps only roots or only non-roots. Filters are conjunctive. Without nsfw=true, sexual-family traits are excluded from the list and land in missing[] on the ids= batch; naming one as parent_id or group_id is 400. include=aliases,description (and view=full) add those blocks. include=character_count is the nightly index total that GET /v2/catalog/characters?trait_id=<this id>&page=1 answers under this request's nsfw; the engine failing is 503. It is an explicit ask: view=full does not add it. is_sexual reports the sexual-family flag.

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
| `parent_id` | query | 否 | string | Catalog trait id. Direct children of this trait only. Naming a sexual-family trait without nsfw=true is 400. |
| `group_id` | query | 否 | string | Catalog trait id of a root group. Traits in that group, excluding the root itself. Naming a sexual-family trait without nsfw=true is 400. |
| `root` | query | 否 | string | true: only root traits. false: only non-root traits. Only true or false. |

```bash
curl "https://api.nextmoe.dev/v2/catalog/traits" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/listCatalogTraits
