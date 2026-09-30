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

```bash
curl -X PATCH "https://api.nextmoe.dev/v2/me/news/value" \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{}'
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/patchMyNewsItem
