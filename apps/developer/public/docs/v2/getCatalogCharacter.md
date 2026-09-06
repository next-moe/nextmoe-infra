# Get one character · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/catalog/characters/{id}

Get one character

Character detail. view=full adds gender, birthday, measurements, blood_type, instance_of_id. include=image,figure,traits,aliases,intros,refs adds art, trait, name, description and anchor blocks. spoiler=none|minor|major is the ceiling of the traits block and defaults to none. Merged ids are 404 ENTITY_MERGED. Requires an application key or a user access token with catalog:read.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer nmk_live_…
- scope：catalog:read

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | path | 是 | string | Decimal catalog id. |
| `nsfw` | query | 否 | string | true includes r18. false or absent hides r18. Only true or false. |
| `view` | query | 否 | string | basic (default) or full. Closed vocabulary. |
| `include` | query | 否 | string | Comma-separated blocks. Unknown token is 400 UNKNOWN_INCLUDE. |
| `fields` | query | 否 | string | Comma-separated top-level keys. Unknown token is 400 UNKNOWN_FIELD. object and id are always kept. |
| `spoiler` | query | 否 | string | Spoiler ceiling for the traits block: none (default), minor or major. Closed vocabulary; an unknown value is 400. Trait rows above the ceiling are not returned. Only the VNDB-derived trait vocabulary carries a spoiler level, and the default is the safe ceiling. |

```bash
curl "https://api.nextmoe.dev/v2/catalog/characters/value" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/getCatalogCharacter
