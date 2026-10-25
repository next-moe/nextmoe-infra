# Turn a text walkthrough into a draft chart · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## POST /v2/me/walkthrough-drafts

Turn a text walkthrough into a draft chart

A language model reads the text and returns a choice chart in the shape catalog.walkthrough.chart takes, with the import gate's verdict on it. Nothing is written to the catalog and the text is not stored; the caller edits the draft and files it as a proposal. Each caller has 20 drafts per rolling 24 hours; past it 429 QUOTA_EXCEEDED. 503 when no model is configured. Requires a user access token bound to a catalog site. The token must carry the catalog:edit scope.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `Idempotency-Key` | header | 否 | string | Makes the request safe to retry. The same key with the same body within 24 hours replays the first response with Idempotency-Replayed: true; the same key with a different body is 409 IDEMPOTENCY_KEY_REUSED; a retry while the first request is still running is 409 IDEMPOTENCY_REQUEST_IN_PROGRESS. Scoped to the caller and the path. |

请求体

| 字段 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- |
| `text` | 是 | string | A walkthrough as plain text, such as choices written one per line under route headings. It is sent to a language model and not stored. Must not be used as a discriminant. |

```bash
curl -X POST "https://api.nextmoe.dev/v2/me/walkthrough-drafts" \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"text":"string"}'
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/createMyWalkthroughDraft
