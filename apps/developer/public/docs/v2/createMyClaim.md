# Submit a claim · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## POST /v2/me/claims

Submit a claim

Mint or claim a work. work_id claims an existing catalog work. refs= claims the work they resolve to, or mints one from display_name when none match. site_work_id with display_name and neither work_id nor refs mints a work anchored to the site's own id. field_values carries an editing-engine work field map onto any mint lane and may be sent alone, without work_id, refs or site_work_id; it is refused with work_id, and refs that already resolve to a work answer 409 instead of dropping it. released rides the same mint lanes under the same two refusals and becomes one curated catalog_release row on the minted work. A caller holding catalog.claim.trusted mints straight to live rather than pending. A mint whose display_name or catalog.work.titles match live works of the same medium is refused with 409 DUPLICATE_SUSPECTS naming them in suspects[] and nothing is written; re-send with confirm_duplicates=true to mint anyway. The claiming lanes — work_id, and refs that resolve — never hit this gate. A contributor who has used up catalog.claim_writes_per_day inside the sliding window is refused 429 QUOTA_EXCEEDED; holders of catalog.claim.trusted get the higher catalog.claim_writes_per_day_trusted. Requires a user access token bound to a catalog site. The token must carry the catalog:edit scope.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `Idempotency-Key` | header | 否 | string | Makes the request safe to retry. The same key with the same body within 24 hours replays the first response with Idempotency-Replayed: true; the same key with a different body is 409 IDEMPOTENCY_KEY_REUSED; a retry while the first request is still running is 409 IDEMPOTENCY_REQUEST_IN_PROGRESS. Scoped to the caller and the path. |

请求体

| 字段 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- |
| `confirm_duplicates` | 否 | boolean | Mint even when live works of the same medium share a submitted title. Without it such a mint is refused with 409 DUPLICATE_SUSPECTS and the works in suspects[]; nothing is written. Confirming still files the pairs for reconciliation. Only mint lanes read this. |
| `display_name` | 否 | string | Required to mint when refs do not match. Must not be used as a discriminant. |
| `field_values` | 否 | map<string, object> | Editing-engine field key to value for the minted work, e.g. catalog.work.titles — the same shape GET /v2/moderation/snapshots/{object}/{id} answers. Sent alone it mints from the map; sent with work_id it is 422. Top-level display_name wins over catalog.work.display_name. |
| `field_values.«key»` | 否 | object |  |
| `refs` | 否 | object[] | source:external_id anchors, at most 100. Used when work_id is absent. |
| `refs[].external_id` | 是 | string | Verbatim upstream id. Must not be used as a discriminant beyond exact match. |
| `refs[].source` | 是 | string | Open vocabulary source key such as vndb. Must not be used as a discriminant. |
| `released` | 否 | object |  |
| `released.d` | 否 | integer (int32) | Release day; 0 or absent means unknown. Requires m. |
| `released.m` | 否 | integer (int32) | Release month; 0 or absent means unknown. |
| `released.y` | 是 | integer (int32) | Release year. |
| `site_work_id` | 否 | string | The site's own work id. Sent alone with display_name — no work_id, no refs — it mints a work anchored to that id. |
| `work_id` | 否 | string | Existing catalog work id to claim. |

```bash
curl -X POST "https://api.nextmoe.dev/v2/me/claims" \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"work_id":"1"}'
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/createMyClaim
