# One patch page · moyu 补丁面

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/moyu/patches/{id}

One patch page

- 所属 API：moyu 补丁面（/v2/moyu）
- 鉴权：Authorization: Bearer nmk_live_…
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | path | 是 | string | moyu patch id. Not a catalog work id and not a VNDB number. |
| `include` | query | 否 | string | Comma-separated relations to attach. Nothing is attached by default, including the resources — `resource_count` tells you whether there is anything to ask for, and `/patches/{id}/resources` pages them when there are many.  取值：resources \| publisher \| resources,publisher |

```bash
curl "https://api.nextmoe.dev/v2/moyu/patches/value" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/moyu/getPatch
