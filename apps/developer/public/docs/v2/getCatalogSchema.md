# Editable-field schema for one family · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/catalog/schemas/{object}

Editable-field schema for one family

Unauthenticated metadata: include tokens, FULL_SET, and editing-engine fields. include/full_set describe the family's DETAIL face and list_include/list_full_set its collection face, which is narrower on work. Actor capabilities are not evaluated. Unknown object is 404 NOT_FOUND. schemas/release sets creation_disabled.

- 所属 API：Public API v2（/v2）
- 鉴权：无需凭据
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `object` | path | 是 | string | Family this schema describes. Unknown family is 404 NOT_FOUND. 取值：work \| company \| character \| release \| tag \| engine \| series \| person \| credit_name \| trait |

```bash
curl "https://api.nextmoe.dev/v2/catalog/schemas/work"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/getCatalogSchema
