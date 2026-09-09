# sticker 表情包面接入

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

`/v2/sticker` 是 [sticker.kungal.com](https://sticker.kungal.com) 联邦进平台的只读面。

**它不是第二份内容列表。** 站上每一张表情都打了 infra **catalog** 的作品 id 与角色 id，所以这个面回答的是：**这个 catalog 身份有哪些表情素材？** 手里已经握着 catalog id 的调用方可以直接 join——`/v2/sticker/characters/{character_id}/stickers` 与 `/v2/sticker/works/{work_id}/packs` 是主车道，其余端点都是围着它们的脚手架。

契约由表情包站自己的仓库拥有，本站只镜像它的 OpenAPI 原文；端点清单见[端点参考 · sticker 表情包面](/docs/sticker)。

## 数据边界

**只暴露已发布的表情包。** 草稿、隐藏、已删除的包，评论正文，审核状态，以及全部创作端点都不在这个面里，将来也不会有。

**名字是多语言映射，不是字符串。** 键为 `zh-cn`、`zh-tw`、`ja-jp`、`en-us`、`und`，每一个都是可选的，任何一个都可能缺席；`und` 放的是 catalog 里没有语言标记的显示名。

```json
{ "zh-cn": "夏日口袋", "ja-jp": "サマーポケッツ", "en-us": "Summer Pockets" }
```

回退链由你自己定——**这个面永远不替你挑一个**。`title`、`description`、`Work.name`、`Character.name`、`Tag.name` 全部是这个形状。

## 拿密钥

Base URL 是 `https://api.nextmoe.dev`，与 v2 同一个。凭据也是同一把：在[控制台](/dashboard)铸一把 `nmk_live_` 应用密钥即可，流程见[快速上手](/docs/quickstart)。

**不需要任何 scope。** 网关在请求到达表情包站之前跑 ForwardAuth，只查身份、速率与配额，不做授权——任意有效密钥都放行，永远不会 403。铸密钥时**不要去找 `sticker:read`**：这个 scope 字符串不存在，勾了会被拒。

```bash
curl "https://api.nextmoe.dev/v2/sticker/packs?limit=3" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

`X-API-Key: nmk_live_<YOUR_KEY>` 是等价写法。

> [!WARNING]
> **只能从你自己的服务端调用。** 网关对包括 `OPTIONS` 在内的每个方法都验密钥，而 CORS 预检不带认证头，所以浏览器直连必然 401——不是你的 CORS 配置写错了。详见[鉴权与凭据](/docs/authentication)。

## 九个端点

| 方法与路径                                           | 做什么                           |
| ---------------------------------------------------- | -------------------------------- |
| `GET /v2/sticker/packs`                              | 列出已发布的表情包               |
| `GET /v2/sticker/packs/{pack_id}`                    | 单个包，含其中的表情、作品与角色 |
| `GET /v2/sticker/stickers/{sticker_id}`              | 单张表情                         |
| `GET /v2/sticker/characters`                         | 该站有素材的 catalog 角色索引    |
| `GET /v2/sticker/characters/{character_id}`          | 单个 catalog 角色在该站的样子    |
| `GET /v2/sticker/characters/{character_id}/stickers` | **主车道**：某个角色的全部表情   |
| `GET /v2/sticker/works`                              | 该站有素材的 catalog 作品索引    |
| `GET /v2/sticker/works/{work_id}/packs`              | **主车道**：关于某部作品的表情包 |
| `GET /v2/sticker/tags`                               | 标签，用得最多的在前             |

> [!NOTE]
> 这个面的翻页与 catalog `/v2` **不同**：它用 `page`（1 起，最大 1000）加 `limit`（1–50，默认 20）的偏移翻页，列表信封是 `{object, items, total, page, limit}`——**没有 `next_cursor`**，`total` 一直都在。`limit` 超过 50 是 `400 LIMIT_TOO_LARGE`。

其余参数：`sort` 取 `new`（默认，按发布时间）或 `hot`（按下载数再按浏览数），两者都以 UUIDv7 的 id 收尾，所以是稳定的 tiebreaker；`q` 是不分大小写的子串匹配，**同时跨全部语言**——一个日文查询能命中只有日文标题匹配的包，超过 100 字符会被截断。

`nsfw`、`official`、`linked` 声明为**字符串枚举** `'true'` / `'false'`，不是 JSON 布尔；照字面写 `nsfw=true` 即可。

> [!IMPORTANT]
> `nsfw` 说的是**表情包自己的分级**，不是它所属游戏的。从一部 r18 游戏里剪出来的日常反应表情包是 `all_ages`，而它的 `work.content_rating` 仍然是 `r18`。这里关联的 95 部游戏里有 89 部是 r18，按游戏分级过滤会几乎清空结果。

### 例一：某个角色的全部表情

主车道。手里有 catalog 角色 id 就直接问：

```bash
curl "https://api.nextmoe.dev/v2/sticker/characters/12345/stickers?limit=2" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

```json
{
  "object": "list",
  "items": [
    {
      "object": "sticker",
      "id": "0193f2a1-…",
      "pack_id": "0193f29c-…",
      "position": 1,
      "image": {
        "hash": "3f7a…",
        "url": "https://…/full.webp",
        "thumb_url": "https://…/320.webp",
        "width": 512,
        "height": 512
      },
      "note": "…",
      "work": { "object": "work", "id": 61311, "name": { "zh-cn": "…" } },
      "character": {
        "object": "character",
        "id": 12345,
        "name": { "ja-jp": "…" }
      }
    }
  ],
  "total": 24,
  "page": 1,
  "limit": 2
}
```

- 最新在前，只含已发布的包。
- **空页与 404 含义不同**：角色没有素材时这里返回**空列表**，绝不会仅因为这个 id 就 404。会 404 的是 `GET /v2/sticker/characters/{character_id}`——那表示该站没有任何已发布的表情打了这个角色，它**不说明 catalog 认不认识这个 id**。
- `image.hash` 是 infra 图床服务的内容寻址，也是这里唯一稳定的标识符：同样的字节由生态内另一个站点存下来，hash 相同。表情包封面上没有这个字段，要稳定身份用 `cover_sticker_id`。
- `position` 从 1 开始，在同一个包内唯一。

### 例二：关于某部作品的表情包

```bash
curl "https://api.nextmoe.dev/v2/sticker/works/61311/packs?sort=hot&nsfw=true" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

```json
{
  "object": "list",
  "items": [
    {
      "object": "pack",
      "id": "0193f29c-…",
      "title": { "zh-cn": "…", "ja-jp": "…" },
      "description": { "zh-cn": "…" },
      "official": true,
      "content_rating": "all_ages",
      "sticker_count": 24,
      "view_count": 0,
      "download_count": 0,
      "cover": {
        "url": "https://…/full.webp",
        "thumb_url": "https://…/320.webp"
      },
      "cover_sticker_id": "0193f2a1-…",
      "work": {
        "object": "work",
        "id": 61311,
        "name": { "zh-cn": "…" },
        "content_rating": "r18"
      },
      "tags": [
        {
          "object": "tag",
          "slug": "…",
          "name": { "zh-cn": "…" },
          "pack_count": 0
        }
      ],
      "author": {
        "object": "author",
        "id": 1,
        "name": "…",
        "avatar_url": "https://…"
      },
      "created_at": "2026-08-29T02:51:07Z",
      "updated_at": "2026-08-29T02:51:07Z",
      "published_at": "2026-08-29T02:51:07Z"
    }
  ],
  "total": 3,
  "page": 1,
  "limit": 20
}
```

> [!NOTE]
> 「关于」是宽口径：一个包只要**声明**了某部游戏，**或含有该游戏的表情**，就算是关于它。站方预置的官方包一个游戏都不声明，却各自取材自几十部游戏——按窄口径读，它们什么都答不出来。`/v2/sticker/packs?work=61311` 用的是同一条关系。

`pack.work` 只在作者声明过时出现；混合包不声明任何游戏，但每一张表情上仍带各自的 `work` 与 `character`。`author.id` 是 NextMoe 账号 id，与生态内每个站点用的是同一个。

### 例三：单个包，含全部表情

```bash
curl "https://api.nextmoe.dev/v2/sticker/packs/0193f29c-…" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

详情比列表多三个数组：`stickers`（按展示顺序）、`works`（这些表情覆盖到的去重后游戏）、`characters`（去重后角色）。`works` 与 `characters` 就是这个包能 join 回 catalog 的全部身份，用它们做一次批量水合最省事。

`pack_id` 与 `sticker_id` 是 **UUIDv7 字符串**；`work.id`、`character.id`、`author.id` 是 **JSON 数字**（int64）。这与 catalog `/v2` 的「id 一律是字符串」不同，跨面拼接时记得转换。

## 和 catalog 拼起来

这个面给素材，catalog 给身份内容。两种方向：

```bash
# 从 catalog 往这边：我这部作品有没有表情素材
curl "https://api.nextmoe.dev/v2/sticker/works/61311/packs" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"

# 从这边往 catalog：拿角色 id 取 catalog 的权威资料
curl "https://api.nextmoe.dev/v2/catalog/characters/12345" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

`Work.id` 拿去 [`/v2/catalog/works/{id}`](/docs/v2/getCatalogWork) 解析，`Character.id` 拿去 [`/v2/catalog/characters/{id}`](/docs/v2/getCatalogCharacter) 解析——同一把密钥就能读。

要**同步一份索引**而不是逐个查，用 `GET /v2/sticker/characters`：它列出每个在已发布包中至少有一张表情的 catalog 角色 id，按数量排序。catalog 认识但该站没有素材的角色按设计不会出现在这里，所以这个索引就是「有素材的全集」。`GET /v2/sticker/works` 同理，并在作品索引上额外下发 `sticker_count`。

`Work.content_rating` 与 `Character.image_url` 都是 catalog 的值原样透传；catalog 标记角色立绘为限制级时 `image_url` 不下发。要严格的分级判定仍以 catalog 为准，见[镜像到自己的库](/docs/mirror)。

## 错误：两种方言

这个面上有**两套错误体**，按 HTTP status 分支，不要按 body 形状猜。

**网关写的（401、429）**——请求还没到表情包站就被拦下，body 是平台自己的信封：

```http
HTTP/1.1 401 Unauthorized
Content-Type: application/json

{"code":10001,"message":"未授权，请先登录"}
```

401 是缺密钥或密钥无效/已吊销；429 是这把密钥的速率或配额用尽，带 `Retry-After` 与 `X-RateLimit-*`。

**表情包站写的（其余全部）**——RFC 9457 `application/problem+json`，字段名与平台 `/v2` 的 problem 文档一致：

```http
HTTP/1.1 400 Bad Request
Content-Type: application/problem+json

{
  "type": "https://developer.nextmoe.dev/problems/platform/limit-too-large",
  "title": "Limit too large",
  "status": 400,
  "detail": "limit must be between 1 and 50",
  "instance": "/v2/sticker/packs?limit=500",
  "code": "LIMIT_TOO_LARGE"
}
```

`type` URI 解析到本站的[错误码注册表](/problems)。`code` 取自平台那份封闭注册表，原样照搬，因此一套客户端解码逻辑同时覆盖这个面与 catalog；这个面会出现的是 `INVALID_PARAMETER`、`LIMIT_TOO_LARGE`、`NOT_FOUND`、`INTERNAL_ERROR`、`SERVICE_UNAVAILABLE` 五个。`instance` 是失败的那条请求的路径与查询串。

未知 slug 是个例外：`tag=` 传一个不存在的 slug**匹配不到任何东西**，返回空列表，而不是报错。

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
  throw new Error(`${problem.code}: ${problem.detail ?? problem.title}`)
}
```

字段含义与分支顺序见[错误处理](/docs/errors)。

## 限流

限流在网关按**密钥所属应用**计数，与 v2 共池：free 档 60 次/分、50,000 次/日。超限是 `429` 加 `Retry-After`，并带 `X-RateLimit-*` 与 `X-Quota-*` 响应头。分档与退避写法见[限流与配额](/docs/rate-limits)。

同步索引时用满 `limit=50` 而不是默认 20，请求数直接降到四成之一。

## 缓存

> [!NOTE]
> 与 [moyu 补丁面](/docs/moyu-patches#caching)不同，这个面的契约里**没有声明 `ETag`、`304` 与 `Cache-Control`**。不要按条件请求去写客户端：`If-None-Match` 命中与否都不在承诺范围内。请按自己的 TTL 缓存响应体——素材是只增不改的，几分钟到几小时都安全。平台整体的条件请求约定见[缓存与条件请求](/docs/caching)。

真正该缓存的是图片：`image.url` 与 `image.thumb_url` 指向图床 CDN，直接引用即可，既不经过这个面也不消耗 API 配额。`thumb_url` 是 320px 变体，列表页用它。`image.hash` 是内容寻址，可以直接当本地缓存键。

## 接下来

- [端点参考 · sticker 表情包面](/docs/sticker) —— 九个端点的全部参数、响应 schema 与可直接运行的 curl 示例。
- [moyu 补丁面接入](/docs/moyu-patches) —— 另一个下游站点面，同一把密钥、同一套错误方言。
- [鉴权与凭据](/docs/authentication) —— 为什么这个面不能从浏览器直接调。

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/sticker-packs
