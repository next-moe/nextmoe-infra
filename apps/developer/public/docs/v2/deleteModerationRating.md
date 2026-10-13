# Delete any rating · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## DELETE /v2/moderation/ratings/{id}

Delete any rating

Removes the rating from every site at once; the deletion reaches /v2/ratings/changes as a gone entry and the work's nextmoe score follows. 204 with no body. Requires a user access token whose holder has catalog.rating.moderate through a global role: the moderator, admin and ren roles have it, and the permission console can grant it to another role. A role a site grants on its own tokens does not count here, since a site moderator moderates that site and a deleted rating leaves every site. Reviewing catalog edits or claims does not include it.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | path | 是 | string | Rating id. |

```bash
curl -X DELETE "https://api.nextmoe.dev/v2/moderation/ratings/1" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/deleteModerationRating
