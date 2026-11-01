---
title: 评分排行榜
eyebrow: 集成指南
description: 用 /v2/catalog/rankings 做游戏排行：跨 VNDB、Bangumi、DLsite、批评空间、NextMoe、HowLongToBeat 的共识分，或任一来源单独排名；每个数字怎么读、常见榜单的现成查询、全部筛选项。
---

# 评分排行榜

`GET /v2/catalog/rankings` 把作品按评分排好序，一次给出名次、用来排名的值、每个来源的原始分与模型估计。它读的是搜索索引，所以 `/v2/catalog/works` 上能用的筛选（标签、会社、系列、引擎、语言、发售日、分级……）这里都能用。

```bash
curl "https://api.nextmoe.dev/v2/catalog/rankings?limit=20"
```

不带凭据就能调用，按 IP 限流；带应用密钥或带 `catalog:read` 的用户令牌走各自更高的额度。

## 每个数字怎么读 {#reading}

一个条目长这样（节选）：

```json
{
  "object": "ranking_entry",
  "rank": 1,
  "value": 87.52,
  "work": { "id": "60", "display_name": "命运石之门", "…": "…" },
  "consensus": {
    "score": 91.65,
    "lower": 87.52,
    "upper": 95.78,
    "percentile": 99.99,
    "source_count": 6,
    "vote_count": 27455,
    "divergence": 0.44
  },
  "per_source": [
    {
      "source": "erogamescape",
      "score": 93,
      "vote_count": 504,
      "mean": 90.94,
      "lower": 89.94,
      "upper": 91.93,
      "median": 92.93,
      "spread": 11,
      "percentile": 99.97
    }
  ]
}
```

| 字段                    | 含义                                                                                                                                       |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| `per_source[].score`    | 来源自己发布的分数，原样不动。VNDB 是它的贝叶斯分、Bangumi 与 DLsite 是均值、批评空间是中央值。                                            |
| `per_source[].mean`     | 在该来源刻度上的**真实水平估计**：作品自身的均值，向「同等票数的作品通常得到的分数」收缩。票越少收缩越多，三票满分排不到三千票的名作前面。 |
| `lower` / `upper`       | 90% 区间的两端。区间越窄，说明这个分数越可信。                                                                                             |
| `median`                | 同样收缩后的中位票，批评空间式的「中央值」口径。                                                                                           |
| `spread`                | 投票的标准差，越大说明评价越两极。                                                                                                         |
| `percentile`            | 在该来源评过的作品里超过了多少比例。                                                                                                       |
| `consensus.score`       | **共识分**，按偏差值读：50 是被两个以上来源评过的典型作品，每 10 分是一个标准差；60 以上就是明显好于平均。                                 |
| `consensus.lower`       | 共识分 90% 区间的下端。默认就按它排名。                                                                                                    |
| `consensus.divergence`  | 来源之间的分歧，以百分位点计：一个来源排在前 5%、另一个排在中游，就是几十点的分歧。                                                        |
| `value` 与列表的 `unit` | 用来排名的那个值；`unit` 告诉你它的单位（`deviation` 偏差值、`native` 来源刻度、`percentile`、`votes` 等）。                               |

> [!TIP]
> 展示时可以直接把 `consensus.score` 当作「综合评分」，把 `lower`–`upper` 画成误差条，再把 `per_source` 列成各站原分。原分永远在，用户可以自己核对。

### 共识分怎么来的 {#method}

1. 每个来源单独估计一次真实水平：经验贝叶斯，先验的均值随票数变化（票少的作品整体偏低，这是数据里看到的规律），先验的强度从数据里估计。`model.sources[].prior_votes` 就是这个强度：票数等于它时，均值被拉回一半。
2. 每个来源的估计在该来源内部转成正态分数，这样 5 星制和 100 分制可以比较，DLsite 普遍偏高也不再是问题。
3. 在被多个来源共同评过的作品上拟合一个单因子模型，校正各来源受众的系统差异；再按每个来源对这部作品「了解多少」（票数、该来源与共识的吻合度）加权合并。

模型每夜重新拟合，`model` 块告诉你当前用的是哪一次拟合。拟合时会做回测：把一个来源藏起来，用其余来源去预测它，共识分在全部六个来源上都比简单平均各站百分位更准。

## 常见榜单 {#recipes}

| 想要的榜单                     | 查询                                                    |
| ------------------------------ | ------------------------------------------------------- |
| 综合神作榜                     | `?nsfw=true`                                            |
| 全年龄综合榜                   | 不带 `nsfw`（默认隐藏 R18）                             |
| 批评空间中央值榜（50 票以上）  | `?by=erogamescape&metric=native&min_votes=50&nsfw=true` |
| 批评空间收缩中位数榜           | `?by=erogamescape&metric=median&nsfw=true`              |
| 某年度榜                       | `?year=2020&nsfw=true`，或区间 `?year=2015-2020`        |
| 同人 / 商业分开排              | `?circulation=doujin` / `?circulation=commercial`       |
| DLsite 同人榜                  | `?by=dlsite&circulation=doujin&nsfw=true`               |
| 只信批评空间和 Bangumi         | `?sources=erogamescape,bangumi&min_sources=2`           |
| 至少三个站都评过               | `?min_sources=3`                                        |
| 冷门佳作（票少但好）           | `?metric=mean&min_votes=5&max_votes=50`                 |
| 争议作（各站分歧最大）         | `?metric=divergence`                                    |
| 两极分化（同一站内评价分裂）   | `?by=bangumi&metric=spread&min_votes=100`               |
| 地雷榜                         | `?order=asc&min_votes=50`                               |
| 短篇佳作（10 小时以内）        | `?playtime_max=600`                                     |
| Switch 平台榜                  | `?platform=swi`                                         |
| 某会社 / 某系列 / 某标签内排名 | `?company_id=…` / `?series_id=…` / `?tag_id=…`          |
| 偏差值 60 到 70 之间           | `?score_min=60&score_max=70`                            |

> [!NOTE]
> 默认排序用区间的**下端**（`metric=lower`）：只有确定地好才会排在前面。`order=asc` 时默认换成区间的**上端**（`metric=upper`），只有确定地差才进地雷榜。想按最佳估计排，用 `metric=mean`。

## 全部参数 {#parameters}

| 参数                            | 说明                                                                                                                                                                    |
| ------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `by`                            | `consensus`（默认）或来源键：`vndb`、`bangumi`、`dlsite`、`erogamescape`、`nextmoe`、`howlongtobeat`                                                                    |
| `metric`                        | 共识：`lower`、`upper`、`mean`、`percentile`、`divergence`、`votes`、`sources`。单一来源：`lower`、`upper`、`mean`、`median`、`native`、`percentile`、`spread`、`votes` |
| `order`                         | `desc`（默认）或 `asc`                                                                                                                                                  |
| `sources`、`min_sources`        | 只用这些来源构成共识；至少被几个来源评过。仅 `by=consensus`                                                                                                             |
| `min_votes`、`max_votes`        | `100` 作用于 `by`（共识时是总票数）；`erogamescape:50,bangumi:100` 按来源分别设下限 / 上限                                                                              |
| `score_min`、`score_max`        | 按列表的 `unit` 过滤 `value`                                                                                                                                            |
| `year`、`released_after/before` | 年份或闭区间；二者不能同用                                                                                                                                              |
| `circulation`                   | `commercial` 或 `doujin`                                                                                                                                                |
| `platform`                      | 逗号分隔，任一匹配                                                                                                                                                      |
| `playtime_min`、`playtime_max`  | 分钟；取各来源时长估计的中位数                                                                                                                                          |
| 作品筛选                        | `content_rating`、`content_limit`、`claimed`、`company_id`、`tag_id`、`exclude_tag_id`、`exclude_tag_tree`、`series_id`、`engine_id`、`olang`                           |
| `facets`                        | `tag_id`、`company_id`、`olang`、`content_rating`、`platform`、`circulation`，统计范围是参与排名的作品                                                                  |
| `view`、`include`               | 塑形条目里的 `work`，与 `/v2/catalog/works` 相同                                                                                                                        |
| `limit`、`cursor`、`page`       | 见 [分页](/docs/pagination)；`page=` 模式直接给出 `total`                                                                                                               |

## 注意 {#caveats}

- 排名读每夜重建的索引，新评分最晚第二天进榜；条目里的 `per_source` 是实时的。
- 首次部署、模型还没拟合时返回 `503`。
- 名次是全榜位置，同值时按热度、id 排序，所以每个名次都唯一。
- 一部作品只被一个来源评过时也有共识分，但区间更宽；按默认的 `lower` 排名时会自然靠后。
