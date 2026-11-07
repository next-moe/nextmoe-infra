# Copy a folder into my folders · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## POST /v2/me/folders/copies

Copy a folder into my folders

Creates a private folder of the bearer's own holding every live work of the source in the source's order, with the source owner's notes, tags and cover pick. The description starts with 源自「<source name>」 followed by the source's own description, so the copier decides whether to keep the credit by editing it. 201 with the new folder. 404 when the source is private and not the bearer's; 422 at the folder cap (200). A name sent here first passes the platform's word-list check. Requires a user access token with folder:write.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：folder:write

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `Idempotency-Key` | header | 否 | string | Makes the request safe to retry. The same key with the same body within 24 hours replays the first response with Idempotency-Replayed: true; the same key with a different body is 409 IDEMPOTENCY_KEY_REUSED; a retry while the first request is still running is 409 IDEMPOTENCY_REQUEST_IN_PROGRESS. Scoped to the caller and the path. |

请求体

| 字段 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- |
| `folder_id` | 是 | string | The folder to copy: a public or unlisted one, or one of the bearer's own. |
| `name` | 否 | string | Name of the copy. Absent or blank: the source's name. Must not be used as a discriminant. |

```bash
curl -X POST "https://api.nextmoe.dev/v2/me/folders/copies" \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"folder_id":"1","name":"我的入坑清单"}'
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/copyFolder
