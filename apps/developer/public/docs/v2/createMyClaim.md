# Submit a claim · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## POST /v2/me/claims

Submit a claim

Mint or claim a work. work_id claims an existing catalog work. refs= claims the work they resolve to, or mints one from display_name when none match. site_work_id with display_name and neither work_id nor refs mints a work anchored to the site's own id. field_values carries an editing-engine work field map onto any mint lane and may be sent alone, without work_id, refs or site_work_id; it is refused with work_id, and refs that already resolve to a work answer 409 instead of dropping it. A caller holding catalog.edit.trusted mints straight to live rather than pending. A mint whose display_name or catalog.work.titles match live works of the same medium is refused with 409 DUPLICATE_SUSPECTS naming them in suspects[] and nothing is written; re-send with confirm_duplicates=true to mint anyway. The claiming lanes — work_id, and refs that resolve — never hit this gate. Requires a user access token bound to a catalog site. The token must carry the catalog:edit scope.

- 所属 API：Public API v2（/v2）
- 鉴权：Authorization: Bearer <用户访问令牌>
- scope：无需凭据

无参数。

```bash
curl -X POST "https://api.nextmoe.dev/v2/me/claims" \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{}'
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/createMyClaim
