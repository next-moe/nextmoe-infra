# List or look up games with resources · letmoe 下载资源链接面

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/letmoe/works

List or look up games with resources

Without `ids` this is every game letmoe currently has a resource for,
paged and sorted.

With `ids` it is a **batch lookup** of up to 100 catalog work ids, with
every id that has no visible resource on letmoe echoed back in
`missing`. This is how you ask "which of these games can be downloaded
on letmoe" in one round trip. A batch answers a set, not a page, so
`cursor`, `sort`, `limit` and `include_total` alongside it are
`MUTUALLY_EXCLUSIVE_PARAMETERS`.


- 所属 API：letmoe 下载资源链接面（/v2/letmoe）
- 鉴权：Authorization: Bearer nmk_live_…
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `limit` | query | 否 | integer | 1 to 100. Over 100 is `LIMIT_TOO_LARGE`; the value is not clamped. |
| `cursor` | query | 否 | string | The `next_cursor` from a previous page of the same collection. |
| `include_total` | query | 否 | boolean | Count the whole collection. Absent by default because it costs a count. |
| `nsfw` | query | 否 | boolean | Include games whose display verdict is adult (`content_limit: nsfw`). Off by default, matching catalog's own convention.  |
| `ids` | query | 否 | string[] | Up to 100 catalog work ids. |
| `sort` | query | 否 | string | All descending. `updated` orders by when a resource of the game was last published or changed.  取值：updated \| downloads \| views |

```bash
curl "https://api.nextmoe.dev/v2/letmoe/works" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/letmoe/listWorks
