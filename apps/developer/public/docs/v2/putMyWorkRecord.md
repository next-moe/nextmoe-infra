# Replace my play state, playtime and rating on one work · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## PUT /v2/me/work-records/{work_id}

Replace my play state, playtime and rating on one work

One transaction for the three parts: either all are written or none is. PUT replaces the whole record, so a part that is absent or null is deleted: send every part the form shows. One exception: a third-party application's PUT without a rating leaves the rating as it is; DELETE /v2/me/ratings/{work_id} removes it. playtime is this app's own report, and the record answers the most minutes any app reported. A third-party application's new or changed short_summary is checked as on PUT /v2/me/ratings/{work_id}; a refused one is 422 at /rating/short_summary and no part is written. Requires a user access token; no scope is required.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `work_id` | path | 是 | string | Catalog work id. |

请求体

| 字段 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- |
| `playtime` | 否 | object |  |
| `playtime.minutes` | 是 | integer (int64) | Absolute cumulative minutes. |
| `rating` | 否 | object |  |
| `rating.aspect_scores` | 否 | object |  |
| `rating.aspect_scores.art` | 否 | integer (int64) | Absent or null when not rated. |
| `rating.aspect_scores.character` | 否 | integer (int64) | Absent or null when not rated. |
| `rating.aspect_scores.music` | 否 | integer (int64) | Absent or null when not rated. |
| `rating.aspect_scores.replay_value` | 否 | integer (int64) | Absent or null when not rated. |
| `rating.aspect_scores.route` | 否 | integer (int64) | Absent or null when not rated. |
| `rating.aspect_scores.story` | 否 | integer (int64) | Absent or null when not rated. |
| `rating.aspect_scores.system` | 否 | integer (int64) | Absent or null when not rated. |
| `rating.aspect_scores.voice` | 否 | integer (int64) | Absent or null when not rated. |
| `rating.overall` | 是 | integer (int64) | The overall score, a whole number from 1 to 10. |
| `rating.recommend` | 是 | string | How strongly the author recommends the work. 取值：strong_no \| no \| neutral \| yes \| strong_yes |
| `rating.short_summary` | 否 | string | Short review, plain text. Absent or empty means none. Free text; must not be used as a discriminant. |
| `rating.spoiler_level` | 否 | string | How much short_summary gives away. Absent means none. 取值：none \| portion \| serious |
| `work_state` | 否 | object |  |
| `work_state.completion` | 否 | string | How much is finished. Absent means unstated. Refused with wish. 取值：one_route \| main \| all |
| `work_state.state` | 是 | string | Closed vocabulary, as on /v2/me/work-states. 取值：wish \| doing \| done \| on_hold \| dropped |

```bash
curl -X PUT "https://api.nextmoe.dev/v2/me/work-records/1" \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"playtime":{"minutes":720},"rating":{"aspect_scores":{"art":8,"character":8,"music":9,"replay_value":8,"route":7,"story":8,"system":7,"voice":8},"overall":8,"recommend":"yes","short_summary":"画面和音乐都很出色，主线一周目通关后还想再走一遍。","spoiler_level":"none"},"work_state":{"completion":"all","state":"done"}}'
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/putMyWorkRecord
