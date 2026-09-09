# List or look up patch pages · moyu 补丁面

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/moyu/patches

List or look up patch pages

Without `ids` or `refs` this is the whole collection, paged and sorted.

With either, it is a **batch lookup**: up to 100 anchors in one request,
with every anchor that matched nothing echoed back in `missing`. This is
how you ask "which of these 100 games do you have patches for" in one
round trip.

A batch answers a set, not a page, so `cursor` and `sort` are refused
alongside it. Note that two pages can name one catalog work — moyu
dedupes on the VNDB string and a game that arrived under two spellings
has two pages — so `refs=catalog:<id>` may answer more than one item.
They are ordered so that the page a reader should land on comes first.


- 所属 API：moyu 补丁面（/v2/moyu）
- 鉴权：Authorization: Bearer nmk_live_…
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `limit` | query | 否 | integer | 1 to 100. Over 100 is `LIMIT_TOO_LARGE`; the value is not clamped. |
| `cursor` | query | 否 | string | The `next_cursor` from a previous page of the same collection. |
| `include_total` | query | 否 | boolean | Count the whole collection. Absent by default because it costs a count. |
| `nsfw` | query | 否 | boolean | Include pages catalog rates as adult. Off by default, matching catalog's own convention; a page catalog has not been rated yet is included either way and reports `content_limit: null`.  |
| `include` | query | 否 | string | Comma-separated relations to attach. 取值：resources \| publisher \| resources,publisher |
| `ids` | query | 否 | string | Up to 100 moyu patch ids. Mutually exclusive with `refs`.  |
| `refs` | query | 否 | string | Up to 100 foreign anchors as `source:external_id`. Sources are `vndb` (a VNDB id such as `v65869`) and `catalog` (a NextMoe catalog work id). Mutually exclusive with `ids`.  |
| `sort` | query | 否 | string | All descending. `updated` orders by when a resource was last added or changed, which is not the same as when the page was edited.  取值：updated \| created \| downloads \| views |
| `type` | query | 否 | string | Comma-separated patch kinds; any of them matches. |
| `language` | query | 否 | string | Comma-separated languages; any of them matches. |
| `platform` | query | 否 | string | Comma-separated platforms; any of them matches. |
| `has_resources` | query | 否 | boolean | Unset returns every page. A page can exist with no resources on it yet; `true` is the filter for "actually has something to download".  |

```bash
curl "https://api.nextmoe.dev/v2/moyu/patches" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/moyu/listPatches
