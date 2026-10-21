# Change a merge request's choices · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## PATCH /v2/moderation/merge-requests/{id}

Change a merge request's choices

Replaces field_resolution and hide_release_ids on an open or approved request; amended_by and amended_at record who. Requires the catalog.merge.review permission. The token must carry the catalog:edit scope.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | path | 是 | string | Merge request id. |

请求体

| 字段 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- |
| `field_resolution` | 否 | map<string, string> | Optional. Field → source\|target for the fields GET /v2/me/merge-preview lists under fields. A field not named keeps that preview's default. 422 for a field the family cannot resolve. |
| `field_resolution.«key»` | 否 | string |  |
| `hide_release_ids` | 否 | string[] | Optional, works only. Source releases to hide once they land on the target — normally the ones the preview marks duplicate_of. 422 for a release that is not the source's own. |
| `hide_release_ids[]` | 否 | string |  |

```bash
curl -X PATCH "https://api.nextmoe.dev/v2/moderation/merge-requests/1" \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"field_resolution":{"key":"string"},"hide_release_ids":["string"]}'
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/amendModerationMergeRequest
