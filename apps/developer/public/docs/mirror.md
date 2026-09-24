# 增量镜像目录

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

把目录同步进自己的库，然后靠一条游标信道保持新鲜。冷启动一次全量，之后只拉变化——不需要夜间重扫，也不需要「列出全部 id」那种面。

> [!NOTE]
> 先问自己需不需要镜像。只是按需查几条？直接读就好，配上 `ids=` 批量与 `ETag` 缓存足够了。**要在自己的 SQL 里过滤、排序、连表**，才值得镜像。

## 信道

`GET /v2/catalog/changes` 按 `(updated_at, id)` **升序**枚举目录人口。每一项是这样：

```json
{
  "object": "change",
  "target_object": "work",
  "id": "207379",
  "updated_at": "2026-08-29T02:51:07Z",
  "gone": true // 只在该 id 已离开公开人口时出现，从不发 false
}
```

它是一条普通的游标集合，`limit`（≤100）、`cursor`、`next_cursor` 与其它集合完全一样。

## 承诺了什么

凡是改动作品的 **claim 状态**、**展示轴**（`content_limit` 的输入：`display_nsfw`、`content_rating`，以及作品的封面是否全部被判 explicit）或**作品存在性**的写路径，都会 bump 这一行的 `updated_at`，因而必然在这条 feed 里现身。

其余字段（封面 / 标签 / 标题 / 简介 / 评分）是**尽力而为**——它们多数也会连带 touch 作品行，但只有上面三项是承诺。要对某个非承诺字段做强一致的镜像，请告诉我们，那说明它该被提升成承诺。

## 冷启动

1. **从空游标翻这条 feed。** 它按「最旧更新优先」枚举整个人口，所以第一次翻完就是一次全量清点——不需要另一个「列出全部 id」的面。
2. 每页拿到的 id 以 **≤100 一批**打 `/v2/catalog/works?ids=`，把需要的块用 `include=` 一次水合。
3. **两道闸全开**：传 `nsfw=true`，并且**不要传** `content_limit`。否则被闸掉的作品会在本地留成空洞，而你不会收到任何信号。
4. 存下最后一页的 `next_cursor`。

```http
GET /v2/catalog/changes?limit=100
GET /v2/catalog/works?ids=207379,207380,…&nsfw=true&include=covers,titles,companies
```

## 稳态

1. 带着存下的游标按自己的节奏轮询。游标是不透明串，原样回传。
2. 每页的 id 同样以 ≤100 一批水合，覆盖本地行。
3. 把新的 `next_cursor` 存回去。末页不带这个键，说明你已经追平了。

> [!WARNING]
> feed 有约 5 秒的在途事务水位：刚写入的行会晚一拍出现。**这不是丢失**，下一轮就会带上它。不要因为「刚改的没立刻出现」就去重置游标做全量重扫。

## gone 与合并

- `gone: true` 表示这个 id 已经离开公开人口——被合并掉，或不再被服务。删掉本地那一行。
- **被合并的 id 会同时出现在 `/v2/catalog/redirects`**，那里给出接替它的规范 id。有 redirect 就**重指**而不是删除，否则你会丢掉本地所有指向旧 id 的引用。

```http
GET /v2/catalog/redirects?limit=100

{
  "object": "list",
  "items": [
    { "object": "redirect", "target_object": "work",
      "old_id": "198221", "current_id": "207379",
      "merged_at": "2026-08-29T01:12:44Z" }
  ],
  "next_cursor": "cur_…"
}
```

`redirects` 也是游标 feed，同样可以增量订。`object=` 可以把它限定到某个族。

## 本地谓词怎么写

如果你把 `content_limit` 缓存成自己的一列用来过滤列表页，请把它写成**可空列**，`NULL` 表示「尚未同步」并且**放行**：

```sql
WHERE content_limit IS NULL OR content_limit = 'sfw'
```

这样冷启动期间未水合的行照常可见，而不是整站空白。真正的权威始终是水合时 catalog 自己的闸，本地这一列只是列表页的快筛。

判定不要自己算，直接读作品上的 `content_limit`：每部作品都带（2.26.0 起），认领与否都一样。服务端除了认领方的编辑判定和事实分级，还看封面——一部作品的封面全部被判 explicit 时，不论分级都是 `nsfw`。按分级自己推算会把这类作品当成 `sfw`。

```javascript
const contentLimit = work.content_limit // 'sfw' | 'nsfw'，不要用 content_rating 推算
```

## 整条回路

```javascript
let cursor = load('catalog_cursor') // 首次为空 = 冷启动

for (;;) {
  const page = await get('/v2/catalog/changes', { limit: 100, cursor })

  const gone = page.items.filter((c) => c.gone).map((c) => c.id)
  const live = page.items.filter((c) => !c.gone).map((c) => c.id)

  if (live.length) {
    const hydrated = await get('/v2/catalog/works', {
      ids: live.join(','),
      nsfw: 'true',
      include: 'covers,titles,companies'
    })
    await upsert(hydrated.items)
    // missing[] 里的 id 这一轮不可见，按 gone 处理或留待下轮
  }
  if (gone.length) await retireLocal(gone) // 先查 redirects，能重指就重指

  if (!page.next_cursor) break // 追平了
  cursor = page.next_cursor
  save('catalog_cursor', cursor)
}
```

- [字段裁剪与批量读](/docs/shaping) — `ids=` 的边界、`missing[]` 的语义、include 词表怎么问。
- [限流与配额](/docs/rate-limits) — 冷启动是这个 API 上最大的一次性用量，先算好预算。

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/mirror
