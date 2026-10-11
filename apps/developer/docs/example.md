---
title: 全链走查
eyebrow: 集成指南
description: 用两个真实系列走通 NextMoe 公开 API v2 全链：标题搜索 → 详情按 include 取块 → 系列/厂牌深链 → 外部 id 反查。
---

# 全链走查

以 **SEQUEL**（同人 · リーフジオメトリ）与 **いろセカ**（商业 · FAVORITE）两个真实系列演示 搜索 → 详情 → 系列/厂牌深链 → 外部 id 反查，全部走公开 API v2。

两系列均为 R18——正好演示调用方自控的 `nsfw` 参数：缺省隐藏，显式 `nsfw=true` 才可见；只认 `true` / `false`，写别的是 `400` 而不是按默认值处理。

## 1 · 标题搜索 {#search}

```bash
curl "https://api.nextmoe.dev/v2/catalog/search?object=work&q=SEQUEL&nsfw=true" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

```json
{
  "object": "list",
  "items": [
    {
      "object": "search_result",
      "target_object": "work",
      "id": "207379",
      "display_name": "この素晴らしい世界に祝福を！このゲーム性に嫉妬を！",
      "latin": "…",
      "localized": { "zh-Hans": { "value": "…", "is_machine": false } },
      "sources": ["dlsite", "bangumi"],
      "content_rating": "r18"
    }
  ],
  "total": 5
}
```

没有信封：响应体**就是**那个集合。id 一律是字符串，翻页用 `next_cursor`（末页直接不出现这个键，没有 `has_more`）。

`search` 一次搜所有实体，命中行只有名字、来源和分级。只找作品、又想在同一次请求里拿到封面、开发商、简介这些块时，用 `GET /v2/catalog/works?q=SEQUEL&nsfw=true&include=companies,intros`：同一个索引，结果直接是作品对象。

## 2 · 详情：默认瘦，按 include 取块 {#detail}

```bash
curl "https://api.nextmoe.dev/v2/catalog/works/207379?nsfw=true\
&include=series,companies,popularity,titles,relations,credits" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

```json
{
  "object": "work",
  "id": "207379",
  "medium": "galgame",
  "display_name": "…",
  "content_rating": "r18",
  "release_status": "released",
  "series": [
    {
      "object": "series",
      "id": "320",
      "display_name": "SEQUEL",
      "member_count": 5,
      "source": "dlsite"
    }
  ],
  "companies": [
    {
      "object": "company",
      "id": "11310",
      "display_name": "リーフジオメトリ",
      "company_kind": "doujin_circle",
      "attribution_role": "circle"
    }
  ],
  "popularity": [
    { "source": "bangumi", "metric": "bgm_wish", "value": 33 },
    { "source": "bangumi", "metric": "bgm_collect", "value": 29 },
    { "source": "dlsite", "metric": "downloads", "value": 17710 },
    { "source": "dlsite", "metric": "wishlist", "value": 17114 }
  ],
  "titles": [
    {
      "lang": "ja",
      "title": "…",
      "latin": "…",
      "title_kind": "official",
      "is_machine": false
    }
  ],
  "relations": [
    /* include=relations：关系边 + 对端 work（view=basic） */
  ],
  "credits": [
    /* include=credits：按 role_key 分组；CV 那条带 character_id */
  ]
}
```

还有 `refs` / `intros` / `tags` / `covers` / `screenshots` / `releases` / `ratings` / `playtimes` / `characters` / `engines` / `links` / `platforms`——v2 默认瘦身，每一块都要在 `include=` 里点名，写错 token 是 `400 UNKNOWN_INCLUDE` 而不是静默少块。

多源数据在同一响应里并列出（按 `source` 区分，刻度保持源原生、绝不归一）：

```json
// いろセカ家族（w426 紅い瞳）的多源评分与时长（include=ratings,playtimes）
"ratings": [
  { "source": "vndb",         "score": 7.87, "vote_count": 375 },
  { "source": "bangumi",      "score": 6.9,  "vote_count": 1065, "rank": 4184 },
  { "source": "erogamescape", "score": 78,   "vote_count": 446 }
],
"playtimes": [
  { "source": "vndb",         "minutes": 578, "vote_count": 38 },
  { "source": "erogamescape", "minutes": 600 }
]
```

`rank` / `vote_count` 这类「没记录」的位置恒发 `null` 或 `0`，不会整键消失。

## 3 · 厂牌 / 社团档案 {#company}

```bash
curl "https://api.nextmoe.dev/v2/catalog/companies/11310" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

```json
{
  "object": "company",
  "id": "11310",
  "display_name": "リーフジオメトリ",
  "latin": "…",
  "localized": {},
  "company_kind": "doujin_circle"
}
```

`work_count`（同一 nsfw 闸下可见的作品数）恒在。名下作品**不内嵌**在这个实体里——反查是作品集合上的一个过滤器，这样它自带分页与全套过滤：

```bash
curl "https://api.nextmoe.dev/v2/catalog/works?company_id=11310&nsfw=true" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

## 4 · 外部 id 反查 {#refs}

手握 VNDB / Bangumi / DLsite / ErogameScape id 时的首选路径：

```bash
curl "https://api.nextmoe.dev/v2/catalog/works?refs=vndb:v19658&nsfw=true" \
  -H "Authorization: Bearer nmk_live_<YOUR_KEY>"
```

批量就把最多 100 个 `source:external_id` 用逗号连起来一次问完——v2 没有 `/lookup` 这样的动词路径，反查是集合上的 `refs=` 参数。没锚到的会原样回在 `missing[]` 里，而不是让整个请求 404。

## 接下来 {#next}

- [端点参考](/docs/v2) — 全部 88 个端点的参数、响应与 curl。
- [字段裁剪与批量读](/docs/shaping) — `include` / `fields` / `ids` / `refs` 的完整规则。
- [数据浏览](/explore) — 先不写代码，在浏览器里点着试。

机器可读 spec 在 `https://api.nextmoe.dev/v2/catalog/openapi.json`（免密钥）。
