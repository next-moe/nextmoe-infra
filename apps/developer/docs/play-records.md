---
title: 评分与游玩记录
eyebrow: 集成指南
description: 游玩状态、游玩时长、评分，以及把三者一次写完的 work-records：谁能写、替换语义、公开读取与镜像入口。
---

# 评分与游玩记录

一个人在一部作品上的游玩数据分成四份资源。三份各自能读能写，第四份把它们放进同一个请求。

## 四份资源怎么选 {#which}

| 资源     | 路径                            | 什么时候用                     |
| -------- | ------------------------------- | ------------------------------ |
| 游玩状态 | `/v2/me/work-states`            | 只改「想玩 / 在玩 / 通了」     |
| 游玩时长 | `/v2/me/playtimes`              | 只上报本应用累计的分钟数       |
| 评分     | `/v2/me/ratings`                | 只改这个人给这部作品的评分     |
| 一次写完 | `/v2/me/work-records/{work_id}` | 一个表单同时交状态、时长和评分 |

四份都只要用户访问令牌，不需要 scope。公开读取别人的评分不走 `/v2/me`，见 [公开读取](#public)。

```http
GET    /v2/me/work-states
GET    /v2/me/work-states/{work_id}
PUT    /v2/me/work-states/{work_id}
POST   /v2/me/work-states
DELETE /v2/me/work-states/{work_id}

GET    /v2/me/playtimes
GET    /v2/me/playtimes/{work_id}
PUT    /v2/me/playtimes/{work_id}
POST   /v2/me/playtimes
DELETE /v2/me/playtimes/{work_id}

GET    /v2/me/ratings
GET    /v2/me/ratings/{work_id}
PUT    /v2/me/ratings/{work_id}
DELETE /v2/me/ratings/{work_id}

GET    /v2/me/work-records/{work_id}
PUT    /v2/me/work-records/{work_id}
```

## 一次写完 {#work-records}

`GET /v2/me/work-records/{work_id}` 返回表单要展示的三块，形状与三条单资源的 GET 一致。什么都没记过也是 200，对应的块为 null。

`PUT /v2/me/work-records/{work_id}` 在**一个事务**里写这三块：要么都写上，要么都不写。它**替换整条记录**。某一块缺了，或者是 null，这块就被删掉。客户端必须把表单上显示的每一块都原样送回去。

```json
{
  "work_state": { "state": "done", "completion": "all" },
  "playtime": { "minutes": 720 },
  "rating": {
    "overall": 8,
    "recommend": "yes",
    "spoiler_level": "none",
    "short_summary": "画面和音乐都很出色，主线一周目通关后还想再走一遍。"
  }
}
```

`playtime` 写的是**本应用自己的**上报。读回来的分钟数是所有应用里报过的最大值。

> [!WARNING]
> 复制一个空 body 再 PUT，会删掉这个人的游玩状态和本应用的时长。第一方应用还会连评分一起删掉。第三方应用不送 `rating` 时，评分保持原样。

## 给这部作品打分 {#rate}

表单的顺序是固定的：先读，再整份送回。

1. `GET /v2/me/work-records/{work_id}`，把返回的三块填进表单。null 的块显示为空。
2. 用户改完后，`PUT` 同一路径，body 里带上表单上的**每一块**，包括用户没改的那些。
3. 第三方应用不能写评分：表单里把评分只读显示，PUT 时不送 `rating`，状态和时长照常写入，评分留在原地。送了 `rating` 是 `403 FIRST_PARTY_ONLY`，整次写入都不生效。

第一方应用不要靠「省略 rating」来表示「没改评分」——省略就是删除。没改的评分要原样送回去。

## 评分 {#ratings}

一条评分是**一个人、一部作品、所有 NextMoe 站点共用的一条**。`id`、`client_id` 和 `site` 永远是第一次写入时的值，后来换一个站点改内容也不会换主人。

`PUT /v2/me/ratings/{work_id}` 创建或整份替换。`overall` 是 1 到 10 的整数，`recommend` 是 `strong_no / no / neutral / yes / strong_yes`。`aspect_scores` 的每一项也是 1 到 10，不评的项不送。`spoiler_level` 不送等于 `none`。`short_summary` 是纯文本，最长 2000 个字符，不送或空串等于没有短评。和已经存着的内容完全一样时，这次写入不落库。作品的 nextmoe 评分（`/v2/catalog/works/{id}/ratings`）随即更新。

> [!IMPORTANT]
> `PUT /v2/me/ratings/{work_id}`，以及 work-records 里的 `rating` 块，**只有第一方应用能写**。第三方应用的令牌得到 `403 FIRST_PARTY_ONLY`。中央服务不检查 `short_summary` 的内容，每个第一方站点在写入之前自己做内容检查。

读是放开的。`GET /v2/me/ratings` 与 `GET /v2/me/ratings/{work_id}` 任何拿着这个用户令牌的应用都能调，不需要 scope。列表按目录最后一次改动这条评分的时间从旧到新排，`next_cursor` 是增量水位；`work_ids=` 是最多 100 个 id 的批量读，不分页。单条不存在是 404。

`DELETE /v2/me/ratings/{work_id}` 任何应用都能调，不需要 scope。没有这条评分也是 204。删除会在 `/v2/ratings/changes` 里变成一条 `gone`。

## 游玩时长 {#playtimes}

每个应用各写自己的一行，单位是累计分钟，0 到 60000。`PUT /v2/me/playtimes/{work_id}` 覆盖本应用的这一行，重复送同一个数不会多写一行。

`GET /v2/me/playtimes/{work_id}` 回答的是**各应用上报里最大的分钟数**。从没人报过是 404。`GET /v2/me/playtimes` 按行枚举，`work_ids=` 是批量读。

`DELETE /v2/me/playtimes/{work_id}` 只删**本应用**的那一行。别的应用报过的还在，之后的 GET 继续从它们里面取最大。

批量写用 `POST /v2/me/playtimes`，一次最多 100 条，响应是 `207 Multi-Status`：`items[]` 与请求逐位对应，每一项要么是一条 playtime，要么是一个 problem。导入历史时用这一条，不要逐条 PUT。

## 游玩状态 {#work-states}

`state` 是封闭词表 `wish / doing / done / on_hold / dropped`。`completion` 表示通了多少，`one_route / main / all`，可以不填。`wish` 不允许带 `completion`。

`PUT /v2/me/work-states/{work_id}` 整体替换。body 里没有 `completion`，原来的完成度就被清掉。重复送同一份内容不会多写。`POST /v2/me/work-states` 是批量，同样 207，一次最多 100 条。`work_ids=` 是列表上的批量读。

## 公开读取 {#public}

别人写下的评分在 `/v2/ratings`，不在 `/v2/me`。需要应用密钥，或带 `catalog:read` 的用户令牌。

```http
GET /v2/ratings?work_id={work_id}
GET /v2/ratings?user_id={user_id}
GET /v2/ratings?work_id={work_id}&user_id={user_id}
GET /v2/ratings/{id}
```

`work_id=` 与 `user_id=` 至少要有一个。两个都给，就是这个账号对这部作品的那一条。列表按**创建时间**从新到旧，后来的修改不会把一条评分挪到前面。这个面没有内容闸：调用方要藏 R18 作品，在水合作品时自己滤。

`GET /v2/ratings/{id}` 按评分 id 取一条。没有这条，包括已经删掉的，都是 404。

## 镜像 {#mirror}

要在自己的库里留一份评分副本，走 `/v2/ratings/changes`，不要翻 `/v2/ratings`。冷启动、水位和 `gone` 的做法见 [增量镜像目录 · 评分镜像](/docs/mirror#ratings)。

## 版主删除 {#moderation}

`DELETE /v2/moderation/ratings/{id}` 把这条评分从所有站点删掉，feed 里出现 `gone`，作品的 nextmoe 评分随后更新。204，没有 body。

门槛是 `catalog.rating.moderate`，而且**只认全局角色**：`moderator`、`admin`、`ren` 默认有，权限控制台也可以授给别的角色。令牌上由某个站点自己授予的角色不算。审核编辑或认领的权限也不包括这一条。没有这个权限是 403。
