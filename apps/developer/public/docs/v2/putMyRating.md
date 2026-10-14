# Rate a work, or replace my rating of it · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## PUT /v2/me/ratings/{work_id}

Rate a work, or replace my rating of it

Creates the rating or replaces every field of it; the id, client_id and site stay those of the first write. Sending the rating as it already stands writes nothing. The work's nextmoe score on /v2/catalog/works/{id}/ratings follows at once. Any app may write a rating; it is public on every site. A first-party site checks short_summary itself before sending it. For a third-party application's token the catalog checks a new or changed short_summary: a refused one is 422 VALIDATION_FAILED at /short_summary with reason NOT_ALLOWED_VALUE, and nothing is written. Requires a user access token; no scope is required.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `work_id` | path | 是 | string | Catalog work id. |

请求体

| 字段 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- |
| `aspect_scores` | 否 | object |  |
| `aspect_scores.art` | 否 | integer (int64) | Absent or null when not rated. |
| `aspect_scores.character` | 否 | integer (int64) | Absent or null when not rated. |
| `aspect_scores.music` | 否 | integer (int64) | Absent or null when not rated. |
| `aspect_scores.replay_value` | 否 | integer (int64) | Absent or null when not rated. |
| `aspect_scores.route` | 否 | integer (int64) | Absent or null when not rated. |
| `aspect_scores.story` | 否 | integer (int64) | Absent or null when not rated. |
| `aspect_scores.system` | 否 | integer (int64) | Absent or null when not rated. |
| `aspect_scores.voice` | 否 | integer (int64) | Absent or null when not rated. |
| `overall` | 是 | integer (int64) | The overall score, a whole number from 1 to 10. |
| `recommend` | 是 | string | How strongly the author recommends the work. 取值：strong_no \| no \| neutral \| yes \| strong_yes |
| `short_summary` | 否 | string | Short review, plain text. Absent or empty means none. Free text; must not be used as a discriminant. |
| `spoiler_level` | 否 | string | How much short_summary gives away. Absent means none. 取值：none \| portion \| serious |

```bash
curl -X PUT "https://api.nextmoe.dev/v2/me/ratings/1" \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"aspect_scores":{"art":8,"character":8,"music":9,"replay_value":8,"route":7,"story":8,"system":7,"voice":8},"overall":8,"recommend":"yes","short_summary":"画面和音乐都很出色，主线一周目通关后还想再走一遍。","spoiler_level":"none"}'
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/putMyRating
