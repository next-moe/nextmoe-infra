# Preview a merge · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/me/merge-preview

Preview a merge

Computes what merging source_id into target_id would do, without writing: both sides' values for the resolvable fields, the fate of every external id, the releases of both works with source duplicates marked, title and intro outcomes, row counts, the site claim, and blockers and warnings. An entry already merged away or deleted comes back with a not_live blocker rather than an error. Requires a user access token bound to a catalog site. The token must carry the catalog:edit scope.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `object` | query | 是 | string | Family of the two entries. 取值：work \| company \| character \| person \| credit_name |
| `source_id` | query | 是 | string | The entry to fold away. |
| `target_id` | query | 是 | string | The entry that survives. |

```bash
curl "https://api.nextmoe.dev/v2/me/merge-preview" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/previewMyMerge
