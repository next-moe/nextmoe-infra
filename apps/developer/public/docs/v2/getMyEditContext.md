# What an editor sees behind one entity · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/me/edit-context/{object}/{id}

What an editor sees behind one entity

Per field: the value a proposal is written against, who wrote each scalar column (field provenance, newest first), and, for list fields whose importers keep their own rows, every stored row grouped by source with its identity key, whether it is hidden, whether it is the human lane, plus suppressions whose row is gone. An editor shows what each upstream says from this face and sends rows[].identity in the field's .suppressed companion to hide a row. Requires a user access token bound to a catalog site. The token must carry the catalog:edit scope.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `object` | path | 是 | string | Family of the entity. Unknown family is 404 NOT_FOUND. 取值：work \| company \| character \| release \| tag \| engine \| series \| person \| credit_name \| trait |
| `id` | path | 是 | string | Catalog id of the entity. |

```bash
curl "https://api.nextmoe.dev/v2/me/edit-context/work/1" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/getMyEditContext
