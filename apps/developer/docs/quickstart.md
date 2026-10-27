---
title: 快速上手
eyebrow: 开始
description: 不注册、不带密钥，一分钟发出第一个 NextMoe API v2 请求：读懂响应、按需取块、用外部 id 反查；量大了再自助铸密钥。
---

# 快速上手

读目录不需要注册，也不需要密钥。整个 `/v2/catalog` 只读面（`claim-events` 除外）以及 `/v2/news`、`/v2/vocabularies`、`/v2/problems` 都可以什么凭据都不带，复制下面的命令就能跑。

不带凭据的请求按来源 IP 计一份额度，默认每分钟 30 次、每天 1,000 次，个人脚本、浏览器扩展和本地工具都够用。量上来了再[铸一把密钥](#key)，同样不需要申请。

## 1 · 发出第一个请求 {#first-call}

```bash
curl "https://api.nextmoe.dev/v2/catalog/works?limit=3"
```

响应体**就是**那个集合，没有 `{code,message,data}` 外壳：

```json
{
  "object": "list",
  "items": [
    {
      "object": "work",
      "id": "4",
      "medium": "galgame",
      "display_name": "Summer Pockets REFLECTION BLUE",
      "latin": null,
      "localized": {},
      "olang": "ja",
      "content_rating": "all_ages",
      "content_limit": "sfw",
      "release_date": "2018-04-25",
      "release_date_precision": "day",
      "release_status": "released",
      "cover": {
        "url": "https://…",
        "hash": "…",
        "width": null,
        "height": null,
        "thumbhash": null,
        "sexual": null,
        "violence": null,
        "source": "",
        "origin": "cover"
      },
      "banner": null,
      "claim": {
        "site": "kungal",
        "site_work_id": "4",
        "state": "live",
        "content_limit": "sfw"
      },
      "created_at": "2026-07-06T10:45:35Z",
      "updated_at": "2026-10-11T11:26:58Z"
    }
  ],
  "next_cursor": "cur_…"
}
```

- `object` 是类型判别符，每个资源都带，值就是它的族名。
- id 是**十进制字符串**。它在库里是 int64，超出 JavaScript `Number` 的安全整数范围，发成 JSON number 会静默失真。
- 翻页只有 `next_cursor` 一种：把它原样回传即可。**末页直接不出现这个键**，没有 `has_more`，也不要用 `items.length === limit` 判断还有没有下一页。
- 默认瘦身：集合里的 `localized{}` 要带 `include=titles` 才会填上；作品的标签、角色、评分、封面列表这些块也都要在 `include=` 里点名才会出现。

浏览器里同样能直接调：CORS 对所有来源开放，`fetch("https://api.nextmoe.dev/v2/catalog/works?limit=3")` 就行，不需要自己的后端代理。

## 2 · 按需取块 {#include}

详情面同理——默认只有身份内核，`include=` 决定要哪些块，写错 token 是 `400 UNKNOWN_INCLUDE` 而不是静默少块：

```bash
curl "https://api.nextmoe.dev/v2/catalog/works/12?include=tags,ratings,companies"
```

每个族有哪些 `include` token、哪些 `sort` 键、哪些顶层字段，都能从发现面自己问出来，不必翻文档：

```bash
curl "https://api.nextmoe.dev/v2/catalog/schemas/work"
```

## 3 · 手里已经有外部 id？ {#refs}

不用先搜再猜。反查是集合上的一个参数，一次最多 100 个 `source:external_id`，没锚到的原样回在 `missing[]` 里，而不是让整个请求 404：

```bash
curl "https://api.nextmoe.dev/v2/catalog/works?refs=steam:1230140,dlsite:VJ014002,bangumi:297264"
```

这三个 id 来自三家不同的站点，指的是同一部作品，所以只回一条记录。要知道每条结果是哪个 id 命中的，带上 `include=refs`；要连 R18 作品一起匹配，带上 `nsfw=true`——不带的话，R18 作品会和真正没收录的作品一起落进 `missing[]`。匿名请求同样可以带 `nsfw=true`。

## 4 · 量大了：铸一把密钥 {#key}

匿名额度刻意定得很小，它够一个人用，但撑不起一个产品。遇到下面任何一种情况，就该带凭据了：

- **从自己的服务端调用。** 服务器只有一个出口 IP，不带凭据时你的全部用户会挤进同一份匿名额度。
- **每天超过 1,000 次**，或者需要每分钟 30 次以上的突发。
- 要用 `/v2/store`、[`/v2/moyu/*`](/docs/moyu-patches)、[`/v2/sticker/*`](/docs/sticker-packs) 这几个只收密钥的面。

### 创建应用 {#app}

用生态账号（NextMoe / 鲲 Galgame）登录 [控制台](/dashboard)，不必另外注册开发者身份。每个账号最多 5 个应用，每个应用最多 5 把在用密钥。应用是配额、用量与 scope 的边界——一个产品一个应用，出事时可以单独吊销。

### 铸造并带上密钥 {#mint}

密钥形如 `nmk_live_…`，尾部带 CRC32 校验位，**只在铸造时显示一次**。它是机密：只放服务端，不要写进前端包、移动端二进制或公开仓库。开发联调可以铸 `nmk_test_` 前缀的测试密钥。自助铸的密钥是 `free` 档，每分钟 60 次、每天 50,000 次，见[限流与配额](/docs/rate-limits)。

```bash
curl "https://api.nextmoe.dev/v2/catalog/works?limit=3" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

返回的数据和匿名请求逐字节相同，只是额度记在你的应用上。

自助可勾选的 scope 有两个——`catalog:read`（读目录数据）与 `store:read`（[DLsite 分销链接](/docs/store)）。[`/v2/moyu/*`](/docs/moyu-patches)、[`/v2/sticker/*`](/docs/sticker-packs) 与 [`/v2/letmoe/*`](/docs/letmoe) 三个下游面不需要任何 scope，任意有效密钥都能调（两条 `/file` 下载路由除外，要运营发放的 `moyu:files` / `letmoe:files`）。`claim_events:read` 由运营方按需授予，不能自助勾选。

分发出去的桌面客户端没有服务端可放密钥：要么继续匿名（每个用户各自按自己的 IP 计额度），要么改用用户访问令牌——见[原生桌面应用接入](/docs/native-app)。

## 接下来 {#next}

- [鉴权与凭据](/docs/authentication) — 不带凭据、应用密钥、用户访问令牌，各自能开哪些面。
- [数据模型](/docs/concepts) — 六源如何对齐成一条记录，实体族之间怎么连。
- [全链走查](/docs/example) — 用两个真实系列走通搜索 → 详情 → 厂牌 → 反查。
- [端点参考](/docs/v2) — 每个端点的参数、响应与 curl 示例。
- [下游站点面](/docs/moyu-patches) — 补丁与表情包两个联邦面：任意有效密钥，不需要任何 scope。
