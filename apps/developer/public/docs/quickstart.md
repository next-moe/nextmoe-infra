# 快速上手

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

从零到第一次成功调用，大约五分钟。不需要申请，不需要审核。

> [!NOTE]
> 匿名就能先试一把：`/v2/news`、`/v2/vocabularies`、`/v2/problems`、`/v2/catalog/stats` 与 `/v2/catalog/schemas/{object}` 不要任何凭据。想读目录数据再回来铸密钥。

## 1 · 创建应用

用生态账号（NextMoe / 鲲 Galgame）登录 [控制台](/dashboard)，不必另外注册开发者身份。每个账号最多 5 个应用，每个应用最多 5 把在用密钥。应用是配额、用量与 scope 的边界——一个产品一个应用，出事时可以单独吊销。

## 2 · 铸一把密钥

密钥形如 `nmk_live_…`，尾部带 CRC32 校验位，**只在铸造时显示一次**。它是机密：只放服务端，不要写进前端包、移动端二进制或公开仓库。开发联调可以铸 `nmk_test_` 前缀的测试密钥。分发出去的桌面客户端没有服务端可放，读目录数据请改用用户访问令牌——见[原生桌面应用接入](/docs/native-app)。

自助可勾选的 scope 有两个——`catalog:read`（读目录数据）与 `store:read`（商店联盟链接）。[`/v2/moyu/*`](/docs/moyu-patches) 与 [`/v2/sticker/*`](/docs/sticker-packs) 两个下游面不需要任何 scope，任意有效密钥都能调。`claim_events:read` 由运营方按需授予，不能自助勾选。

## 3 · 发出第一个请求

```bash
curl "https://api.nextmoe.dev/v2/catalog/works?limit=3" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

响应体**就是**那个集合，没有 `{code,message,data}` 外壳：

```json
{
  "object": "list",
  "items": [
    {
      "object": "work",
      "id": "207379",
      "medium": "galgame",
      "display_name": "…",
      "latin": "…",
      "localized": { "zh-Hans": { "value": "…", "is_machine": false } },
      "olang": "ja",
      "content_rating": "all_ages",
      "release_date": "2021-08-27",
      "release_date_precision": "day",
      "release_status": "released",
      "cover": {
        "url": "https://…",
        "hash": "…",
        "width": 560,
        "height": 420,
        "thumbhash": "…",
        "sexual": "safe",
        "violence": null,
        "source": "dlsite"
      },
      "banner": null,
      "claim": null,
      "created_at": "2025-11-02T09:14:33Z",
      "updated_at": "2026-08-29T02:51:07Z"
    }
  ],
  "next_cursor": "cur_…"
}
```

- `object` 是类型判别符，每个资源都带，值就是它的族名。
- id 是**十进制字符串**。它在库里是 int64，超出 JavaScript `Number` 的安全整数范围，发成 JSON number 会静默失真。
- 翻页只有 `next_cursor` 一种：把它原样回传即可。**末页直接不出现这个键**，没有 `has_more`，也不要用 `items.length === limit` 判断还有没有下一页。
- 默认瘦身：作品的标签、角色、评分、封面列表这些块都要在 `include=` 里点名才会出现。

## 4 · 按需取块

详情面同理——默认只有身份内核，`include=` 决定要哪些块，写错 token 是 `400 UNKNOWN_INCLUDE` 而不是静默少块：

```bash
curl "https://api.nextmoe.dev/v2/catalog/works/207379?include=tags,ratings,companies" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

每个族有哪些 `include` token、哪些 `sort` 键、哪些顶层字段，都能从发现面自己问出来，不必翻文档：

```bash
curl "https://api.nextmoe.dev/v2/catalog/schemas/work"
```

## 5 · 手里已经有外部 id？

不用先搜再猜。反查是集合上的一个参数，一次最多 100 个 `source:external_id`，没锚到的原样回在 `missing[]` 里，而不是让整个请求 404：

```bash
curl "https://api.nextmoe.dev/v2/catalog/works?refs=vndb:v19658,bangumi:302835" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

## 接下来

- [鉴权与凭据](/docs/authentication) — 应用密钥 vs 用户访问令牌，以及各自能开哪些面。
- [数据模型](/docs/concepts) — 六源如何对齐成一条记录，实体族之间怎么连。
- [全链走查](/docs/example) — 用两个真实系列走通搜索 → 详情 → 厂牌 → 反查。
- [端点参考](/docs/v2) — 113 个端点的参数、响应与 curl 示例。
- [下游站点面](/docs/moyu-patches) — 补丁与表情包两个联邦面：同一把密钥，不需要任何 scope。

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/quickstart
