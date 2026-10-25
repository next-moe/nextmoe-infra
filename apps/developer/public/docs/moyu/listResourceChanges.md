# Resource change feed · moyu 补丁面

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/moyu/changes

Resource change feed

What happened to each resource, oldest first, so a mirror can follow
moyu instead of re-reading every page. The log starts on 2026-10-14
with no backfill: take one full scan through the collections first,
then follow this from an empty cursor. Replaying a page is safe.

`next_cursor` is always set. Keep it and ask again; a page shorter
than `limit` means you are caught up. Entries appear once every
transaction that started before them has finished, so a long write
can delay the feed for its duration but never makes it skip one.

`kind` says which way to react:
- `created`, `file_replaced`, `updated`, `restored`: read the
  resource again (`GET /v2/moyu/resources/{id}`).
- `hidden`, `deleted`: it is no longer public. `hidden` may come back
  as `restored`.
- `normalized`: the file was swapped for the passwordless zip letmoe's
  forge built from it. The content is the same; the size and password
  changed, and `/file` now answers `normalized: true`.

Counter changes (downloads, likes) are not events, and nor are edits
made while a resource is not public.

Never cached: `Cache-Control: no-store`.


- 所属 API：moyu 补丁面（/v2/moyu）
- 鉴权：Authorization: Bearer nmk_live_…
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `limit` | query | 否 | integer | 1 to 100. Over 100 is `LIMIT_TOO_LARGE`; the value is not clamped. |
| `cursor` | query | 否 | string | The `next_cursor` from a previous page of the same collection. |

```bash
curl "https://api.nextmoe.dev/v2/moyu/changes" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/moyu/listResourceChanges
