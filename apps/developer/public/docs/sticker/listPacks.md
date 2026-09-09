# List published sticker packs · sticker 表情包面

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/sticker/packs

List published sticker packs

- 所属 API：sticker 表情包面（/v2/sticker）
- 鉴权：Authorization: Bearer nmk_live_…
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `limit` | query | 否 | integer | Page size, 1-50, default 20. Above 50 is 400 LIMIT_TOO_LARGE. |
| `page` | query | 否 | integer | 1-based page number, 1-1000. |
| `sort` | query | 否 | string | `new` (default) orders by publication date, `hot` by downloads then views. Both end on the id, which is a UUIDv7 and therefore a stable tiebreaker. 取值：new \| hot |
| `q` | query | 否 | string | Substring match, case-insensitive, across every language at once -- a Japanese query finds a pack whose Japanese title matches even when nothing else does. Truncated at 100 characters. |
| `nsfw` | query | 否 | string | `true` includes r18 packs; absent or `false` hides them. Same meaning as catalog's nsfw parameter. Note this is the *pack's* rating: a pack of ordinary reaction faces cut from an r18 game is `all_ages`, and its work's `content_rating` still says `r18`. 取值：true \| false |
| `tag` | query | 否 | string | Tag slug. Unknown slugs match nothing rather than erroring. |
| `work` | query | 否 | integer (int64) | Catalog work id. Keeps packs that declare this game or hold a sticker of it -- the same relation `/v2/sticker/works/{work_id}/packs` uses. |
| `official` | query | 否 | string | true keeps only packs published by the site itself. 取值：true \| false |
| `linked` | query | 否 | string | true keeps only packs that declare a catalog work. 取值：true \| false |

```bash
curl "https://api.nextmoe.dev/v2/sticker/packs" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/sticker/listPacks
