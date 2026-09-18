# Revert to a revision · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## POST /v2/moderation/reverts

Revert to a revision

Body names revision_id. Requires a user access token bound to a catalog site. The token must carry the catalog:edit scope.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `Idempotency-Key` | header | 否 | string | Makes the request safe to retry. The same key with the same body within 24 hours replays the first response with Idempotency-Replayed: true; the same key with a different body is 409 IDEMPOTENCY_KEY_REUSED; a retry while the first request is still running is 409 IDEMPOTENCY_REQUEST_IN_PROGRESS. Scoped to the caller and the path. |

```bash
curl -X POST "https://api.nextmoe.dev/v2/moderation/reverts" \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"revision_id":"string"}'
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/revertModeration
