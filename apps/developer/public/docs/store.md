# DLsite 分销面

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

- 路径前缀：`/v2/store`
- 凭据：Authorization: Bearer nmk_live_…（v2 应用密钥,铸造时勾选 store:read,自助、无需申请;商店价格免凭据）
- 端点数：4

## 使用须知

- 与目录同属 v2 公开面，单独成一份参考：给你的站点铸一条只属于你的应用的 DLsite 购买短链，读者经它购买，平台按点击把 DLsite 返还的优惠券分给你。
- /v2/store/purchase-links/{product_id} 按 DLsite 作品号（同人 RJ、商业 VJ）铸链接，同一个应用、同一个作品号重复调用拿回同一条。purchase_url 原样使用、不要改写或再包一层——结算读的就是它背后的点击计数。活动期间同时给 coupon_url 与 campaign，没有活动时两者为 null。
- 铸链接与读统计要带 store:read 的应用密钥。这个 scope 在控制台自助勾选、不审核，缺了是 403 SCOPE_REQUIRED。
- /v2/store/stats 是带来请求的这个应用所铸链接的每日点击，按 JST 日统计，一次最多 92 天，缺省是最近 30 天。uniques 是去重点击：同一条短链、同一个 JST 日、同一个访问指纹只算一次，自报为爬虫、链接预览或 HTTP 库的访问照常跳转但不计入。计数每小时同步一次，当天的数字始终不完整。
- 能铸链接不等于参与分成：每月的优惠券池是定额的，按去重点击的份额分，参与名单由运营维护。控制台「分销链接」页能看到你名下应用的点击与分到的优惠券。
- /v2/store/prices 是商店价格，不带凭据就能调：DLsite、Getchu、Steam 等商店按地区的报价，一次最多 100 部作品。报价是缓存的观测值，看 fetched_at / expires_at / stale；pending 表示正在抓取，稍后重试，批量车道从不等待冷启动。
- 错误体与 v2 其余部分相同，是 RFC 9457 application/problem+json。

## 端点

### 分销链接

- `GET /v2/store/purchase-links/{product_id}` — Get purchase and coupon links for one product [详情](https://developer.nextmoe.dev/docs/store/getStorePurchaseLinks.md)
- `GET /v2/store/stats` — Click statistics for my links [详情](https://developer.nextmoe.dev/docs/store/getStoreStats.md)

### 商店价格

- `GET /v2/store/prices` — Get storefront prices for many works [详情](https://developer.nextmoe.dev/docs/store/listStorePrices.md)
- `GET /v2/store/prices/{id}` — Get storefront prices for one work [详情](https://developer.nextmoe.dev/docs/store/getStoreWorkPrices.md)

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/store
