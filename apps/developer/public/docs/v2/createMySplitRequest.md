# Request a split · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## POST /v2/me/split-requests

Request a split

Asks reviewers to move some releases of a work into a work of their own, for an edition that deserves its own entry although VNDB lists it under the same VN. Nothing changes on filing: a reviewer approves (and may amend the selection), the cooling-off runs, then the new work is created. One open or approved request per work; another is 409. Quotas are tiered: catalog.split_requests_per_day, _editor, _reviewer inside catalog.write_quota_window_hours; past it 429 QUOTA_EXCEEDED. Requires a user access token bound to a first-party catalog site. The token must carry the catalog:edit scope.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `Idempotency-Key` | header | 否 | string | Makes the request safe to retry. The same key with the same body within 24 hours replays the first response with Idempotency-Replayed: true; the same key with a different body is 409 IDEMPOTENCY_KEY_REUSED; a retry while the first request is still running is 409 IDEMPOTENCY_REQUEST_IN_PROGRESS. Scoped to the caller and the path. |

请求体

| 字段 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- |
| `note` | 是 | string | Why these releases are a work of their own. Must not be used as a discriminant. |
| `object` | 是 | string | Family of the entry. Only works can be split today. 取值：work |
| `target_id` | 是 | string | The work to split. |

```bash
curl -X POST "https://api.nextmoe.dev/v2/me/split-requests" \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"note":"string","object":"work","target_id":"1"}'
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/createMySplitRequest
