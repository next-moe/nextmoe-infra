# My progress through folders · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/me/folders/progress

My progress through folders

For each folder: how many of its live works the bearer keeps in another folder of their own (collected), and how many carry each of the bearer's play states, for a "you have played 12 of 50" line on someone else's list. Works themselves are not listed; GET /v2/me/works?work_ids= answers per work. Folders the bearer cannot read (private and not theirs, or gone) are in missing. Requires a user access token with folder:read (folder:write also grants reads).

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：folder:read 或 folder:write

| 参数 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `folder_ids` | query | 否 | string | Comma-separated folder ids, 1 to 50. |

```bash
curl "https://api.nextmoe.dev/v2/me/folders/progress" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/listMyFolderProgress
