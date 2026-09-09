---
title: moyu 补丁面接入
eyebrow: 下游站点面
description: 接入 /v2/moyu 只读面：一部游戏在 鲲 Galgame 补丁（www.moyu.moe）上有哪些补丁资源。任意有效应用密钥即可调用，无需 scope；refs= 批量反查、catalog_work_id 回填游戏信息、两种错误方言与缓存约定。
---

# moyu 补丁面接入

`/v2/moyu` 是 **鲲 Galgame 补丁**（[www.moyu.moe](https://www.moyu.moe)）联邦进平台的只读面。它只回答一个问题：**这部游戏在 moyu 上有哪些补丁资源？**

游戏由你手里已有的锚指名——一个 VNDB 号，或一个 NextMoe catalog 作品 id——答案是站上对应的那个页，以及挂在这个页下面的资源。

契约由补丁站自己的仓库拥有，本站只镜像它的 OpenAPI 原文；端点清单见[端点参考 · moyu 补丁面](/docs/moyu)。

## 它不给什么 {#not}

两处删减都是契约里写死的取舍，不是遗漏。先读完再动手，能省掉一整轮返工。

**没有下载直链、提取码与解压密码。** 在 moyu 上「显示链接」是一次单独的、按资源限速的请求，它存在的全部意义就是链接不能被批量抓走。这个面每一行都带 `web_url`——把读者送到 www.moyu.moe 的那个页面上去下载，这是唯一的路径。

**没有游戏名、封面、标签、角色与制作人员。** 那些归 catalog，moyu 一份副本都不存。每一行都带 `catalog_work_id`，拿它去 [`/v2/catalog/works/{id}`](/docs/v2/getCatalogWork) 解析——同一把密钥就能读，一次请求问的是权威，而不是从我们这里拿一份更旧的答案。

> [!IMPORTANT]
> `patch.id`、`vndb_id` 与 `catalog_work_id` 是同一部游戏的**三个互不相等、互不可替换**的 id 空间。把 `catalog_work_id` 当补丁 id 去打 `/v2/moyu/patches/{id}` 有时也能返回 200——那是另一个页，不是你要的那个。

## 拿密钥 {#auth}

Base URL 是 `https://api.nextmoe.dev`，与 v2 同一个。凭据也是同一把：在[控制台](/dashboard)铸一把 `nmk_live_` 应用密钥即可，流程见[快速上手](/docs/quickstart)。

**不需要任何 scope。** 这个面是免费只读面，网关只做身份、计量与限流，不做授权——任意有效密钥都放行，永远不会 403。铸密钥时不要去找 `moyu:read`，这个 scope 字符串不存在，勾了会被拒。

```bash
curl "https://api.nextmoe.dev/v2/moyu/patches?limit=3" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

`X-API-Key: nmk_live_<YOUR_KEY>` 是等价写法，两种任选其一。

> [!WARNING]
> **只能从你自己的服务端调用。** 网关对包括 `OPTIONS` 在内的每个方法都验密钥，而 CORS 预检不带认证头，所以浏览器直连必然 401——这不是你的 CORS 配置写错了。`nmk_` 密钥本来也不该出现在浏览器里，详见[鉴权与凭据](/docs/authentication)。

## 四个端点 {#endpoints}

| 方法与路径                            | 做什么                                     |
| ------------------------------------- | ------------------------------------------ |
| `GET /v2/moyu/patches`                | 列出补丁页，或按 `ids=` / `refs=` 批量反查 |
| `GET /v2/moyu/patches/{id}`           | 单个补丁页                                 |
| `GET /v2/moyu/patches/{id}/resources` | 该页上的资源，翻页                         |
| `GET /v2/moyu/resources/{id}`         | 单个资源                                   |

约定与 catalog `/v2` 共享，一套客户端同时覆盖两边：id 一律是**字符串**；集合按不透明 `cursor` 翻页，`total` 要花一次 count 所以默认不算，`include_total=true` 才给；关系按 `include=` 点名附加；时间是 RFC 3339 UTC，日期是 `YYYY-MM-DD`。

`limit` 取 1 到 100，超过 100 是 `400 LIMIT_TOO_LARGE`——**不会被夹到 100**。`nsfw` 缺省为 `false`，与 catalog 同一个约定；catalog 还没有给出分级的页两种情况下都会出现，并报 `content_limit: null`。

### 例一：列一页，并带上资源 {#example-list}

```bash
curl "https://api.nextmoe.dev/v2/moyu/patches?limit=2&sort=updated&has_resources=true&include=resources" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

```json
{
  "object": "list",
  "items": [
    {
      "object": "patch",
      "id": "11617",
      "vndb_id": "v4145",
      "catalog_work_id": "61311",
      "content_limit": "nsfw",
      "release_date": "2007-09-28",
      "type": ["manual"],
      "language": ["zh-Hans"],
      "platform": ["windows"],
      "resource_count": 3,
      "download_count": 0,
      "view_count": 0,
      "favorite_count": 0,
      "comment_count": 0,
      "web_url": "https://www.moyu.moe/…",
      "created_at": "2025-11-02T09:14:33Z",
      "updated_at": "2026-08-29T02:51:07Z",
      "resource_updated_at": "2026-08-29T02:51:07Z",
      "resources": [
        {
          "object": "patch_resource",
          "id": "10463",
          "patch_id": "11617",
          "name": "…",
          "storage": "s3",
          "size": "0.571 MB",
          "hash": "…",
          "model_name": "",
          "localization_group_name": "…",
          "note": "…",
          "type": ["manual"],
          "language": ["zh-Hans"],
          "platform": ["windows"],
          "download_count": 0,
          "like_count": 0,
          "web_url": "https://www.moyu.moe/…",
          "created_at": "2025-11-02T09:14:33Z",
          "updated_at": "2026-08-29T02:51:07Z"
        }
      ]
    }
  ],
  "next_cursor": "cur_…",
  "total": null
}
```

几处值得先知道：

- `sort` 一律降序，可取 `updated`（默认）、`created`、`downloads`、`views`。`updated` 排的是**资源**最近一次新增或改动的时间（也就是 `resource_updated_at`），这与补丁页本身被编辑的时间不是一回事。
- `has_resources` 不传时返回全部页。一个页可以还没有任何资源，`true` 就是「确实有东西可下」的那道过滤。
- `type`、`language`、`platform` 都是逗号分隔的多值，命中其中任意一个即可（例如 `type=ai,manual`）。
- `include=` 只认 `resources`、`publisher`、`resources,publisher` 三种写法；写别的是 `400 UNKNOWN_INCLUDE`。
- 单页详情 `GET /v2/moyu/patches/{id}` **默认什么都不附加**，资源也不例外：先看 `resource_count` 有没有东西可要，数量多时用 `/v2/moyu/patches/{id}/resources` 翻页。

### 例二：refs= 批量反查 {#example-refs}

这是这个面最值钱的一条车道：**「我手上这 100 部作品，你们有哪些的补丁？」一次往返问完。**

```bash
curl "https://api.nextmoe.dev/v2/moyu/patches?refs=vndb:v65869,catalog:61311&include=resources" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

```json
{
  "object": "list",
  "items": [
    {
      "object": "patch",
      "id": "11617",
      "vndb_id": "v4145",
      "catalog_work_id": "61311",
      "…": "…"
    }
  ],
  "next_cursor": null,
  "total": null,
  "missing": ["vndb:v65869"]
}
```

- 一次最多 100 个锚，source 只有 `vndb`（如 `v65869`）与 `catalog`（NextMoe catalog 作品 id）两种。超过 100 是 `400 TOO_MANY_IDS`。
- **没有命中的锚不会让整个请求 404**，它们按你发来的写法原样回在 `missing[]` 里。这个数组只在 `ids=` / `refs=` 车道出现。
- `ids=` 是同一条车道的另一种写法，收的是 moyu 自己的补丁 id（如 `223309,11617`）。`ids` 与 `refs` **互斥**。
- 批量回答的是一个集合而不是一页，所以此时 `cursor` 与 `sort` 会被拒。
- **一个 `catalog:<id>` 可能回不止一项**：moyu 按 VNDB 字符串去重，一部以两种写法进来的游戏就有两个页。它们的顺序保证读者该落地的那个页排在最前——要选一个就取 `items[0]`。

### 例三：单个资源 {#example-resource}

```bash
curl "https://api.nextmoe.dev/v2/moyu/resources/10463?include=publisher" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

资源行里几个容易读错的字段：

- `storage` 为 `s3` 表示文件在 moyu 自己的对象存储里，`user` 表示是发布者放在别处的链接。两种都不给直链。
- `size` 是**给人看的字符串**（如 `"0.571 MB"`），不是字节数。
- `hash` 是文件的 BLAKE3，是字节本身唯一稳定的身份；在开始记录它之前上传的行为空字符串。
- `model_name` 只对 AI 翻译补丁有意义，由发布者手填——自由文本，不是词表，不要拿它做枚举。
- `note` 是 Markdown 源码，其中的图片 token 已经解析成绝对 URL。
- 只有仍然存活的资源会被列出：被发布者停用或被审核隐藏的资源不出现在列表里，在它自己的 URL 上也是 `404`，与站上表现一致。

## 和 catalog 拼起来 {#catalog}

这个面给身份，catalog 给内容。一次完整的展示流程是两步：

```bash
# 1 · 我关心的这几部作品，哪些有补丁
curl "https://api.nextmoe.dev/v2/moyu/patches?refs=catalog:61311,catalog:207379" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"

# 2 · 用回来的 catalog_work_id 批量取游戏名与封面
curl "https://api.nextmoe.dev/v2/catalog/works?ids=61311&include=covers" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

第二步的 `ids=` 一次同样收最多 100 个，所以第一步的一页正好喂给第二步的一次调用。反过来，如果你手里只有 VNDB 号，`refs=vndb:v65869` 直接问 moyu 就行，不必先去 catalog 换 id。

`catalog_work_id` 在占位页上是 `null`（页建得比游戏进 catalog 还早）。这种行只能显示 moyu 那边的信息，把它当成「有补丁但还没对上作品」处理，不要丢弃。

`content_limit` 是 catalog 展示轴判定的镜像，`null` 表示 moyu 还没镜像过来。**无论如何以 catalog 为准**——要严格的分级判定，请按[镜像到自己的库](/docs/mirror)里的写法从 catalog 取。

## 错误：两种方言 {#errors}

这个面上有**两套错误体**，按 HTTP status 分支，不要按 body 形状猜。

**网关写的（401、429）**——请求还没到补丁站就被拦下，body 是平台自己的信封：

```http
HTTP/1.1 401 Unauthorized
Content-Type: application/json

{"code":10001,"message":"未授权，请先登录"}
```

401 是缺密钥或密钥无效/已吊销；429 是这把密钥的速率或配额用尽，带 `Retry-After` 与 `X-RateLimit-*`。

**补丁站写的（其余全部）**——RFC 9457 `application/problem+json`，与 catalog `/v2` 同一个形状：

```http
HTTP/1.1 400 Bad Request
Content-Type: application/problem+json

{
  "type": "https://developer.nextmoe.dev/problems/platform/limit-too-large",
  "title": "Limit too large",
  "status": 400,
  "detail": "limit must be between 1 and 100",
  "instance": "/v2/moyu/patches?limit=500",
  "code": "LIMIT_TOO_LARGE",
  "request_id": "req_01JBQ7X4M2K9P3W5T8ZVN6HRDC",
  "errors": [{ "parameter": "limit", "reason": "OUT_OF_RANGE", "detail": "…" }]
}
```

`type` URI 解析到本站的[错误码注册表](/problems)，`code` 取自平台那份封闭注册表，因此一套解码逻辑同时覆盖这个面与 catalog。这个面会出现的 `code`：`INVALID_PARAMETER`、`UNKNOWN_ENUM_VALUE`、`UNKNOWN_SORT`、`UNKNOWN_INCLUDE`、`INVALID_CURSOR`、`LIMIT_TOO_LARGE`、`TOO_MANY_IDS`、`NOT_FOUND`、`METHOD_NOT_ALLOWED`、`INTERNAL_ERROR`、`SERVICE_UNAVAILABLE`。`400` 时 `errors[0]` 指出是哪个参数。

一个能同时吃下两种方言的分支写法：

```js
const res = await fetch(url, { headers: { Authorization: `Bearer ${key}` } })
if (res.status === 401) throw new Error('key missing or revoked')
if (res.status === 429) {
  await sleep(Number(res.headers.get('Retry-After') ?? 60) * 1000)
  return retry()
}
if (!res.ok) {
  const problem = await res.json() // application/problem+json
  throw new Error(`${problem.code}: ${problem.detail}`)
}
```

字段含义与字段级 `reason` 的完整两层注册表见[错误处理](/docs/errors)。

## 限流 {#rate-limits}

限流在网关按**密钥所属应用**计数，与 v2 共池：free 档 60 次/分、50,000 次/日。超限是 `429` 加 `Retry-After`，并带 `X-RateLimit-*` 与 `X-Quota-*` 响应头。分档、计数身份与退避写法见[限流与配额](/docs/rate-limits)。

批量反查在这里同时是省钱手段：100 部作品一次请求，比 100 次单查省两个数量级的配额。

## 缓存 {#caching}

每个 200 的 GET 都带 `ETag`。把它原样放进下一次请求的 `If-None-Match`，没变就是 `304`，不计入响应体传输：

```bash
curl -i "https://api.nextmoe.dev/v2/moyu/patches/11617" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>" \
  -H 'If-None-Match: "9f2a1c…"'
```

这个面声明的是 `Cache-Control: public, max-age=300, s-maxage=1800, stale-while-revalidate=3600`——**可共享缓存**，与 v2 大多数按凭据能力位变化、只能 `private, no-store` 的面不同。放一层自己的 CDN 或反向代理是安全的。完整契约见[缓存与条件请求](/docs/caching)。

## 接下来 {#next}

- [端点参考 · moyu 补丁面](/docs/moyu) —— 四个端点的全部参数、响应 schema 与可直接运行的 curl 示例。
- [sticker 表情包面接入](/docs/sticker-packs) —— 另一个下游站点面，同一把密钥、同一套错误方言。
- [鉴权与凭据](/docs/authentication) —— 为什么这个面不能从浏览器直接调。
