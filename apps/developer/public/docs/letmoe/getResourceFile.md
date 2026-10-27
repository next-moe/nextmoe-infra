# The download options of a resource · letmoe 下载资源链接面

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/letmoe/resources/{id}/file

The download options of a resource

The links `Resource` withholds. Needs the operator-granted
`letmoe:files` scope.

A `hosted` mirror's `url` is a direct link to letmoe's own file,
supporting `Range`. It is looked up for each answer and not promised
to last: a replaced or taken-down file stops downloading, so do not
store it. An `external_link` mirror is a
link its publisher hosts elsewhere, with the share code and archive
password it needs. A `composite` is the game and a moyu patch spliced
into one download at the edge.

Never cached: every answer, refusals included, is
`Cache-Control: private, no-store`. Each successful answer counts one
download on the resource, so ask when a reader asks to download.


- 所属 API：letmoe 下载资源链接面（/v2/letmoe）
- 鉴权：Authorization: Bearer nmk_live_…
- scope：letmoe:files

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | path | 是 | string | letmoe resource id. |
| `nsfw` | query | 否 | boolean | Include games whose display verdict is adult (`content_limit: nsfw`). Off by default, matching catalog's own convention.  |

```bash
curl "https://api.nextmoe.dev/v2/letmoe/resources/1024/file" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/letmoe/getResourceFile
