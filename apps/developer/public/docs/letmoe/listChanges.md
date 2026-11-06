# Works whose resources changed · letmoe 下载资源链接面

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/letmoe/changes

Works whose resources changed

**The invalidation channel.** If you cache what this face answers for
a work — above all `GET /v2/letmoe/works/{id}/resources` — poll this
feed and drop your cached answers for every `work_id` it names. A
resource published, edited, taken down, moved to another work or given
a new mirror or merged download surfaces here within seconds, so a
long cache stays correct.

An entry carries no content: it says the work's answers may differ,
not how. Read them again when you need them. Adult works are named
too, whatever your `nsfw`, because a work can change sides.

**Call it without `cursor` once to start.** That answer is empty and
its `next_cursor` is the present; there is nothing to replay into a
cache that is empty. From then on pass the `next_cursor` of the
previous answer. It is always present: unchanged when nothing
happened, so store it after every call. Entries are oldest first, one
per work per page.

Entries surface 5 seconds after the change and are kept for 7 days.
A change inside a transaction open for longer than 5 seconds can be
skipped, and a consumer away for longer than 7 days loses the gap, so
keep a TTL on your cache as well; the feed is what lets it be long.
Download and view counts do not surface here.

Never cached: `Cache-Control: private, no-store`.


- 所属 API：letmoe 下载资源链接面（/v2/letmoe）
- 鉴权：Authorization: Bearer nmk_live_…
- scope：无需凭据

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `limit` | query | 否 | integer | 1 to 100. Over 100 is `LIMIT_TOO_LARGE`; the value is not clamped. |
| `cursor` | query | 否 | string | The `next_cursor` from a previous page of the same collection. |

```bash
curl "https://api.nextmoe.dev/v2/letmoe/changes" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/letmoe/listChanges
