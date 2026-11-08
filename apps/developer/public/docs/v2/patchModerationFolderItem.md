# Moderate a folder item's note · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## PATCH /v2/moderation/folders/{id}/items/{work_id}

Moderate a folder item's note

Rewrite or clear the note an owner wrote on one work in any folder; the membership itself stays. The change moves the item's updated_at, so it reaches the owner's incremental sync. 404 when the work is not in the folder. Requires a user access token whose holder has catalog.folder.moderate: the moderator, admin and ren roles have it, and the permission console can grant it to another role. Reviewing catalog edits or claims does not include it.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | path | 是 | string | Folder id. |
| `work_id` | path | 是 | string | Catalog work id. |

请求体

| 字段 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- |
| `note` | 是 | string | The note's replacement; an empty string clears it, which is the usual action. Must not be used as a discriminant. |

```bash
curl -X PATCH "https://api.nextmoe.dev/v2/moderation/folders/1/items/1" \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"note":"string"}'
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/patchModerationFolderItem
