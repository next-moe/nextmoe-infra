# List my proposals · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/me/proposals

List my proposals

The bearer's own proposals filed on the token's catalog site; on the central editor (catalog site editor), on every site. state= is a closed vocabulary and an unknown value is 400. object= or entity_type= narrows to one family, entity_id= to one entity — on this lane entity_id= is accepted without a family because every row already belongs to the caller. include=patch (or view=full) adds each row's patch, effective_patch, changed_fields and (open rows) conflict_fields, the same block GET /v2/me/proposals/{id}?include=patch answers, so a list page needs no read per row. field=, has_conflict= and updated_since= filter; sort=filed_desc (default), filed_asc, updated_desc or updated_asc. Requires a user access token. The token must carry the catalog:edit scope.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：无需凭据

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
| `state` | query | 否 | string | Closed: open, pending, merged, declined, withdrawn. Unknown value is 400 UNKNOWN_ENUM_VALUE. |
| `object` | query | 否 | string | Closed family filter: work, company, character, release, tag, engine, series, person, credit_name, trait. |
| `entity_type` | query | 否 | string | Editing-engine type, e.g. catalog.work. The same spelling POST /v2/me/proposals takes in its body. Names the same filter as object=; sending both with different families is 400. |
| `entity_id` | query | 否 | string | Catalog id of one entity. Accepted alone on this lane, which is already fenced to the bearer's own proposals; pair it with object= or entity_type= when ids collide across families. |
| `site` | query | 否 | string | Tenant key. Only the central editor (catalog site editor) may name a site other than its own; elsewhere it must equal the token's site or the call is 403 TENANT_MISMATCH. Open vocabulary; unknown values match nothing. |
| `origin_site` | query | 否 | string | Closed: kungal, moyu, letmoe, nextmoe, editor. Keeps proposals filed from that property. Unknown value is 400 UNKNOWN_ENUM_VALUE. |
| `field` | query | 否 | string | A field key such as catalog.work.titles. Keeps proposals whose patch or any amendment names it. |
| `has_conflict` | query | 否 | string | true keeps open proposals that a revision recorded after their base_revision_seq overlaps on a field; false keeps the rest. Only true or false. |
| `updated_since` | query | 否 | string | RFC 3339 UTC ending in Z. Keeps proposals whose updated_at is later. |

```bash
curl "https://api.nextmoe.dev/v2/me/proposals" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/listMyProposals
