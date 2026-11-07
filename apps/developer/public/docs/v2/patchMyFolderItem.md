# Update a folder item · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## PATCH /v2/me/folders/{id}/items/{work_id}

Update a folder item

Partial update of one membership: note, and its place in the display order (after_work_id or before_work_id). Both move updated_at, so the change reaches incremental sync; an unchanged note writes nothing. On a public folder a changed note goes to the platform's content scan. 404 when the work is not in the folder. Requires a user access token with folder:write.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：folder:write

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | path | 是 | string | Folder id. |
| `work_id` | path | 是 | string | Catalog work id. |

请求体

| 字段 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- |
| `after_work_id` | 否 | string | Move this work to directly after that one in the folder's display order (sort=position). |
| `before_work_id` | 否 | string | Move this work to directly before that one. Send after_work_id or before_work_id, not both: to move to the top name the current first work, to the end the current last. |
| `note` | 否 | string | New note on this work; an empty string clears it. Trimmed. Must not be used as a discriminant. |

```bash
curl -X PATCH "https://api.nextmoe.dev/v2/me/folders/1/items/1" \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"note":"入坑首选，共通线就很好哭"}'
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/patchMyFolderItem
