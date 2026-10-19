# File a proposal · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## POST /v2/me/proposals

File a proposal

Quotas are tiered (user, editor, reviewer; GET /v2/me/edit-quota answers the bearer's): a contributor who has used up their tier's proposal limit inside the sliding window (catalog.proposals_per_day, catalog.proposals_per_day_editor, catalog.proposals_per_day_reviewer), or who already has their tier's most proposals awaiting a decision (catalog.open_proposals_max_*), is refused 429 QUOTA_EXCEEDED. An editor past catalog.direct_edits_per_day_editor still files, but the proposal queues for review instead of applying. Requires a user access token bound to a catalog site. The token must carry the catalog:edit scope.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `Idempotency-Key` | header | 否 | string | Makes the request safe to retry. The same key with the same body within 24 hours replays the first response with Idempotency-Replayed: true; the same key with a different body is 409 IDEMPOTENCY_KEY_REUSED; a retry while the first request is still running is 409 IDEMPOTENCY_REQUEST_IN_PROGRESS. Scoped to the caller and the path. |

请求体

| 字段 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- |
| `entity_id` | 是 | string | Target catalog id. |
| `entity_type` | 是 | string | Editing-engine type, e.g. catalog.work. Must not be used as a discriminant. |
| `note` | 否 | string | Must not be used as a discriminant. |
| `patch` | 是 | map<string, object> | Field-key to new value. |
| `patch.«key»` | 否 | object |  |

```bash
curl -X POST "https://api.nextmoe.dev/v2/me/proposals" \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"entity_id":"1","entity_type":"string","patch":{"key":"string"}}'
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/createMyProposal
