# Update a folder · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## PATCH /v2/me/folders/{id}

Update a folder

Partial update: name, description, visibility, is_default. A changed name or description first passes the platform's word-list check (refused: 422 at /name or /description, nothing written; check unavailable: written anyway). While the folder is public, its text goes to the platform's content scan after every change, and making it public scans its text and every item note. Requires a user access token with folder:write.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：folder:write

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | path | 是 | string | Folder id. |

请求体

| 字段 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- |
| `cover_work_id` | 否 | string | Pick a work this folder holds as its cover (422 otherwise); an empty string clears the pick. Removing that work from the folder clears it too. |
| `description` | 否 | string | New note; an empty string clears it. Must not be used as a discriminant. |
| `is_default` | 否 | boolean | Only true is accepted: it moves the default flag to this folder. Sending false is 422 — set the flag on another folder instead. |
| `name` | 否 | string | New display name. Must not be used as a discriminant. |
| `tags` | 否 | string[] | Replaces every tag; an empty array clears them. Same rules as on create. Must not be used as a discriminant. |
| `tags[]` | 否 | string |  |
| `visibility` | 否 | string | New visibility. 取值：private \| unlisted \| public |

```bash
curl -X PATCH "https://api.nextmoe.dev/v2/me/folders/1" \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"cover_work_id":"1","description":"全路线通关的作品","is_default":true,"name":"已通关","tags":["催泪"],"visibility":"public"}'
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/patchMyFolder
