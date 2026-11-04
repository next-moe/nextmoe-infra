# Click statistics for my links · DLsite 分销面

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/store/stats

Click statistics for my links

Daily clicks on the links this application minted, over a JST-day range of at most 92 days. The bearer application is the subject — this replaces v1's /v1/store/me/stats. Requires an application key with the store:read scope.

- 所属 API：DLsite 分销面（/v2/store）
- 鉴权：Authorization: Bearer nmk_live_…
- scope：store:read

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `from` | query | 否 | string | First JST day to cover, YYYY-MM-DD. Optional; defaults to 29 days before to. |
| `to` | query | 否 | string | Last JST day to cover, YYYY-MM-DD. Optional; defaults to today. from and to must be at most 92 days apart. |

```bash
curl "https://api.nextmoe.dev/v2/store/stats" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/store/getStoreStats
