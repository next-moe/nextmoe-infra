# List tag categories · Public API v2

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## GET /v2/catalog/tag-categories

List tag categories

The browsing groups canonical tags are filed under, in display order, all on one page. A tag carries its group as tag_category; GET /v2/catalog/tags?category={id} lists a group's tags, and category=none the tags in no group. Ids are stable; display names and order may change. Anonymous calls are allowed and limited per IP address; an application key or a user access token with catalog:read gets its own, higher limit.

- 所属 API：Public API v2（/v2）
- 鉴权：可匿名调用
- scope：catalog:read（仅在带凭据时）

无参数。

```bash
curl "https://api.nextmoe.dev/v2/catalog/tag-categories"
```

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/v2/listCatalogTagCategories
