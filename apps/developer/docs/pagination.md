---
title: 集合与分页
eyebrow: API 基础
description: NextMoe API v2 的集合契约：list 信封、cur_ 游标、limit 上限、include_total、sort 与 facets，以及正确的翻页循环写法。
---

# 集合与分页

整个 API 的默认分页是 keyset 游标。所有集合共用同一个信封、同一套参数、同一条「翻到头了」的判据。唯一的例外是两个搜索集合另有[页码模式](#page-mode)。

## list 信封 {#envelope}

```json
{
  "object": "list",
  "items": [ … ],
  "next_cursor": "cur_…",     // 末页省略
  "total": 12345,             // 只在 include_total=true 时出现
  "facets": { … },            // 只在 facets= 请求时出现
  "missing": ["…"]            // 只在 ids= / refs= 批量车道出现
}
```

`object` 和 `items` 恒在。`items` 是空数组时就是 `[]`，永远不会是 `null`。

## 游标 {#cursor}

- `next_cursor` 是**不透明串**，以 `cur_` 开头。原样回传即可，不要解析、不要构造、不要基于它做算术。
- **末页直接不出现这个键。** 没有 `next_cursor: null`，也没有 `has_more`——两个真值来源必然会不同步。
- 不要用 `items.length === limit` 判断还有没有下一页：满页末页必然说谎。
- 游标是 keyset 而不是 offset，所以深翻页不会越翻越慢，也不会因为中途有新行插入而重复或漏行。

正确的翻页循环长这样：

```javascript
let cursor
do {
  const url = new URL('https://api.nextmoe.dev/v2/catalog/works')
  url.searchParams.set('limit', '100')
  if (cursor) url.searchParams.set('cursor', cursor)

  const page = await fetch(url, {
    headers: { Authorization: `Bearer ${key}` }
  }).then((r) => r.json())

  for (const work of page.items) handle(work)
  cursor = page.next_cursor // 末页是 undefined，循环自然结束
} while (cursor)
```

## limit {#limit}

- 范围 1–100，默认 20。
- `limit=101` 是 `400 LIMIT_TOO_LARGE`，**不会**被截断成 100。静默截断会让你以为自己拿到了全部。

## total 默认不发 {#total}

要精确总数就传 `include_total=true`。它默认关闭是有代价考量的：带过滤条件的精确 `COUNT` 是对同一批数据的第二次全扫，最先在压力下超时，而且它和你刚拿到的那一页天然不一致（两次查询之间数据会变）。

做「共 N 页」的分页器请三思——游标集合的页码本来就没有稳定含义，做「加载更多」会更贴合。确实需要「跳到第 N 页」的浏览界面，用下面的页码模式。

## 页码模式 {#page-mode}

`GET /v2/catalog/works` 与 `GET /v2/catalog/search` 另收 `page=`。这两个集合由搜索索引驱动，索引本来就按页取、总数精确，所以它们能兑现页码；其余集合没有 `page=`。

```json
{
  "object": "list",
  "items": [ … ],
  "total": 12345,             // 页码模式下恒在，与 include_total 无关
  "total_relation": "eq"      // eq：total 精确；gte：total 是下界
}
```

- `page` 从 1 开始，与 `cursor`、`ids`、`refs` 互斥。
- 响应**不带** `next_cursor`，也不带 `page_count`：用 `total` 与 `limit` 自己算。
- **深度上限**：`page × limit ≤ 10000`。越界是 `400 INVALID_PARAMETER`，`errors[0]` 为 `{"parameter": "page", "reason": "OUT_OF_RANGE", "params": {"minimum": 1, "maximum": <最大页>}}`。最后一页可达页码是 `min(ceil(total / limit), floor(10000 / limit))`。
- 在 `/v2/catalog/works` 上，`page=` 与 `q=`、`facets=`、搜索排序一样会切到搜索车道，所以同样不能与 `owner_uid=`、`site=`、`platform=` 同用。
- 不要去解析游标里装的是什么，也不要自己拼游标来「模拟页码」：游标的内容随时可能改变，页码模式才是契约。

## sort {#sort}

每个集合声明自己的一套封闭 `sort` 键，未知值是 `400 UNKNOWN_SORT`。排序一律带 tie-breaker——平局顺序不会由存储内部决定，所以重建索引不会打乱翻页。

以作品集合为例，它接受 `id`（默认）、`updated`、`relevance`、`released_desc`、`released_asc`、`popularity`。带 `q=` 做标题搜索时会切到搜索索引，此时只有 `relevance`、`released_desc`、`released_asc`、`popularity` 有意义。

## facets {#facets}

`facets=` 请求分面计数，结果放在 `facets` 里，未知的分面名是 `400 UNKNOWN_FACET`。作品集合支持 `tag_id`、`company_id`、`olang`、`content_rating`、`medium`、`platform`。

```http
GET /v2/catalog/works?facets=content_rating,medium&limit=1
```

## 批量车道 {#batch}

`ids=` / `refs=` 是**另一条车道**：一次最多 100 个，没有分页，`next_cursor` 不会出现。请求了但不可见的 id 原样回在 `missing[]` 里，而不是让整个请求 404。详见 [字段裁剪与批量读](/docs/shaping)。

> [!NOTE]
> 需要把整个目录同步下来时，不要靠翻 `/v2/catalog/works` 硬扫——用 [`/v2/catalog/changes` 镜像信道](/docs/mirror)，它按更新时间升序枚举整个人口，冷启动一次翻完，之后只要增量。
