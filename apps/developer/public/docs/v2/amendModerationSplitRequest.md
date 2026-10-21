# Amend a split request · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## PATCH /v2/moderation/split-requests/{id}

Amend a split request

Replaces the chosen releases, the new work's name, language and titles, and the entries to promote, while the request is open or approved. Validated like a filing; records who amended. Requires the catalog.merge.review permission. The token must carry the catalog:edit scope.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | path | 是 | string | Split request id. |

请求体

| 字段 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- |
| `display_name` | 是 | string | The new work's display name. Must not be used as a discriminant. |
| `olang` | 是 | string (bcp47) | The new work's original language, BCP-47. |
| `promote_refs` | 否 | object[] | Bangumi or ErogameScape entries linked to the work as related or probable that become the new work's identity. |
| `promote_refs[].external_id` | 是 | string | The entry's id in that registry. Must not be used as a discriminant beyond exact match. |
| `promote_refs[].source` | 是 | string | A registry that lists editions as separate entries. 取值：bangumi \| erogamescape |
| `release_ids` | 是 | string[] | Releases of the work that move to the new work. At least one release has to stay. |
| `release_ids[]` | 否 | string |  |
| `titles` | 否 | object[] | Titles the new work starts with. |
| `titles[].lang` | 是 | string (bcp47) | BCP-47, or empty for a title with no language. |
| `titles[].title` | 是 | string | Must not be used as a discriminant. |
| `titles[].title_kind` | 是 | string | How the new work holds the title. 取值：official \| alias \| abbreviation |

```bash
curl -X PATCH "https://api.nextmoe.dev/v2/moderation/split-requests/1" \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"display_name":"string","olang":"string","release_ids":["string"]}'
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/amendModerationSplitRequest
