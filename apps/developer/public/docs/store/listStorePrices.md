# Get storefront prices for many works · DLsite 分销面

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/store/prices

Get storefront prices for many works

Cached storefront price quotes for up to 100 catalog works. Unauthenticated. Quotes are cached observations; read fetched_at / expires_at / stale. pending means a fetch is in flight, retry shortly. The batch lane never waits on a cold miss.

- 所属 API：DLsite 分销面（/v2/store）
- 鉴权：无需凭据
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `ids` | query | 是 | string | Comma-separated decimal catalog work ids, at most 100. Duplicates are collapsed. Unknown ids are listed in missing[]. |

```bash
curl "https://api.nextmoe.dev/v2/store/prices"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/store/listStorePrices
