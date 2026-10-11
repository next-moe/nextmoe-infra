# Rate a work, or replace my rating of it · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## PUT /v2/me/ratings/{work_id}

Rate a work, or replace my rating of it

Creates the rating or replaces every field of it; the id, client_id and site stay those of the first write. Sending the rating as it already stands writes nothing. The work's nextmoe score on /v2/catalog/works/{id}/ratings follows at once. The rating is public on every site; run your own content check on short_summary before sending it. Refused 403 FIRST_PARTY_ONLY for a third-party application's token. Requires a user access token; no scope is required.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `work_id` | path | 是 | string | Catalog work id. |

```bash
curl -X PUT "https://api.nextmoe.dev/v2/me/ratings/value" \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"overall":0,"recommend":"strong_no"}'
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/putMyRating
