# One revision · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/catalog/revisions/{id}

One revision

include=diff adds the field-level change set against diff_base, or against the preceding revision when diff_base is absent. This id is what POST /v2/moderation/reverts takes. Requires an application key or a user access token with catalog:read.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer nmk_live_…
- scope：catalog:read

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | path | 是 | string | Revision id. |
| `include` | query | 否 | string | Comma-separated blocks. diff is the only token. Unknown token is 400 UNKNOWN_INCLUDE. |
| `view` | query | 否 | string | basic (default) or full. full adds diff. |
| `fields` | query | 否 | string | Comma-separated top-level keys. Unknown token is 400 UNKNOWN_FIELD. |
| `diff_base` | query | 否 | string | Revision id to diff against. Requires include=diff. Absent means the preceding revision of the same entity. |

```bash
curl "https://api.nextmoe.dev/v2/catalog/revisions/value" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/getCatalogRevision
