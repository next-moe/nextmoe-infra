# Edit or withdraw one of my news items · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## PATCH /v2/me/news/{id}

Edit or withdraw one of my news items

A pending or published item may be edited (title/summary/body/source_url/banner_hash/work_ids). A pending edit is scored again; a published edit takes the item off the feed and back to pending until a moderator publishes it again, unless its source is trusted. A published item may be withdrawn with {"status":"withdrawn"} and If-Match. rejected and withdrawn are terminal.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | path | 是 | string | News item id. |
| `If-Match` | header | 否 | string | Current ETag. Required to withdraw. |

请求体

| 字段 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- |
| `banner_hash` | 否 | string | Hash of the banner, as returned by POST /v2/me/news-images; a hash that face did not store is 422 VALIDATION_FAILED with reason UNKNOWN_REFERENCE. The empty string clears the banner. |
| `body` | 否 | string | At most 20000 runes. Only a community submission may carry one; the empty string clears it. Must not be used as a discriminant. |
| `source_url` | 否 | string | Canonical link to the original item, an absolute http(s) URL. The empty string clears it, which only a community submission may do. Must not be used as a discriminant. |
| `status` | 否 | string | Only withdrawn. Publishing and rejection happen in the moderation queue, never here. 取值：withdrawn |
| `summary` | 否 | string | At most 200 runes; longer is 422 VALIDATION_FAILED with reason TOO_LONG. Must not be used as a discriminant. |
| `title` | 否 | string | Must not be used as a discriminant. |
| `work_ids` | 否 | string[] | Replaces the whole linked-work set. |
| `work_ids[]` | 否 | string |  |

```bash
curl -X PATCH "https://api.nextmoe.dev/v2/me/news/1" \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"title":"中文版发售日确定"}'
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/patchMyNewsItem
