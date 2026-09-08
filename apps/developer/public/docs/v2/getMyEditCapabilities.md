# What I may edit on one family · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/me/edit-capabilities/{object}

What I may edit on one family

Per-field can_propose / can_review / would_automerge for the bearer, evaluated by the editing engine. The capability axis cannot ride on /v2/catalog/schemas/{object}: that face is credential-less and B34 forbids varying one URL's field set by credential, so the two are separate URLs and join on fields[].key. Pass entity_id= wherever the site grants the owner channel, or would_automerge answers the type-level question instead. Requires a user access token bound to a catalog site. The token must carry the catalog:edit scope.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `object` | path | 是 | string | Family these capabilities describe. Unknown family is 404 NOT_FOUND. 取值：work \| company \| character \| release \| tag \| engine \| series |
| `entity_id` | query | 否 | integer (int64) | Evaluate against one entity. Omit for the type-level answer, which cannot express the owner channel: a site that grants owner-review or owner-automerge answers would_automerge=false without it. |

```bash
curl "https://api.nextmoe.dev/v2/me/edit-capabilities/work" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/getMyEditCapabilities
