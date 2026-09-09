# The resources on one patch page · moyu 补丁面

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/moyu/patches/{id}/resources

The resources on one patch page

Newest change first. Only live resources are ever listed: one its
publisher disabled or moderation hid is absent here and `404` at its own
URL, exactly as on the site.


- 所属 API：moyu 补丁面（/v2/moyu）
- 鉴权：Authorization: Bearer nmk_live_…
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | path | 是 | string | moyu patch id. Not a catalog work id and not a VNDB number. |
| `limit` | query | 否 | integer | 1 to 100. Over 100 is `LIMIT_TOO_LARGE`; the value is not clamped. |
| `cursor` | query | 否 | string | The `next_cursor` from a previous page of the same collection. |
| `include_total` | query | 否 | boolean | Count the whole collection. Absent by default because it costs a count. |
| `include` | query | 否 | string |  取值：publisher |

```bash
curl "https://api.nextmoe.dev/v2/moyu/patches/value/resources" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/moyu/listPatchResources
