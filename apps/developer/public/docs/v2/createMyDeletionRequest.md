# Request a deletion · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## POST /v2/me/deletion-requests

Request a deletion

Asks reviewers to delete a catalog entry; only works can be deleted today. Nothing is deleted on filing: a reviewer approves, the cooling-off runs, then the entry is soft-deleted. Its links to source entries stay as a tombstone so the importers do not create it again, and user records on it are kept but hidden until a reviewer restores it under the same id. A duplicate is refused 422: merge it with /v2/me/merge-requests instead. One open or approved request per entry; another is 409. Quotas are tiered: catalog.deletion_requests_per_day, _editor, _reviewer inside catalog.write_quota_window_hours; past it 429 QUOTA_EXCEEDED. Requires a user access token bound to a first-party catalog site. The token must carry the catalog:edit scope.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `Idempotency-Key` | header | 否 | string | Makes the request safe to retry. The same key with the same body within 24 hours replays the first response with Idempotency-Replayed: true; the same key with a different body is 409 IDEMPOTENCY_KEY_REUSED; a retry while the first request is still running is 409 IDEMPOTENCY_REQUEST_IN_PROGRESS. Scoped to the caller and the path. |

请求体

| 字段 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- |
| `note` | 是 | string | Evidence reviewers can check. Must not be used as a discriminant. |
| `object` | 是 | string | Family of the entry. Only works can be deleted today. 取值：work |
| `reason` | 是 | string | Why it should go. duplicate is refused 422: a duplicate is merged into the entry it duplicates through /v2/me/merge-requests. 取值：not_a_work \| out_of_scope \| spam_or_test \| other \| duplicate |
| `target_id` | 是 | string | The entry to delete. |

```bash
curl -X POST "https://api.nextmoe.dev/v2/me/deletion-requests" \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"note":"string","object":"work","reason":"not_a_work","target_id":"1"}'
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/createMyDeletionRequest
