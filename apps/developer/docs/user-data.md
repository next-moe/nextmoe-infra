---
title: 接入用户数据
eyebrow: 集成指南
description: 用 OAuth 用户访问令牌接入 /v2/me。游玩记录与收藏夹各有专页；本页是其余用户面的入口，并写认领、编辑提案、封面投票、资讯投稿与审核队列。
---

# 接入用户数据

`/v2/me` 与 `/v2/moderation` 读写的是**某个用户自己的东西**。应用密钥在这两个前缀下一律无效——它证明不了「哪个用户」。

## 先拿到用户令牌 {#token}

标准 OAuth 2.0 授权码 + PKCE，端点在 `https://account.nextmoe.com/api/v1`。完整步骤见 [鉴权与凭据](/docs/authentication#user-token)。拿到 `access_token` 后照常放进 `Authorization: Bearer`——和应用密钥同一个位置，但**不要同时带两个**。

> [!WARNING]
> 用户面的限流按**用户**计数，不是按 IP。所以一定要带用户令牌调用；用别的方式代理会让你的全体用户挤进同一个桶。

## 各面在哪 {#surfaces}

游玩记录与收藏夹有专页。下表是 `/v2/me` 的每一族：凭据，以及语义写在哪一页。

| 一族                           | 路径                                                                              | 凭据                                                   | 写在哪                                       |
| ------------------------------ | --------------------------------------------------------------------------------- | ------------------------------------------------------ | -------------------------------------------- |
| 游玩状态、时长、评分、一次写完 | `/v2/me/work-states`、`/v2/me/playtimes`、`/v2/me/ratings`、`/v2/me/work-records` | 用户令牌，无 scope。第三方应用写的短评由中央做内容检查 | [评分与游玩记录](/docs/play-records)         |
| 隐私开关                       | `/v2/me/privacy`                                                                  | 用户令牌，无 scope                                     | [评分与游玩记录](/docs/play-records#privacy) |
| 收藏夹                         | `/v2/me/folders`                                                                  | `folder:read` / `folder:write`                         | [收藏夹](/docs/folders)                      |
| 按作品汇总                     | `/v2/me/works`                                                                    | `folder:read`（`folder:write` 也满足读）               | [本页](#works)                               |
| 关注的会社                     | `/v2/me/followed-companies`                                                       | 用户令牌，无 scope                                     | [本页](#followed-companies)                  |
| 屏蔽的标签                     | `/v2/me/blocked-tags`                                                             | 用户令牌，无 scope                                     | [本页](#blocked-tags)                        |
| 浏览记录                       | `/v2/me/history`                                                                  | 用户令牌，无 scope；`scope=all` 读要 `history:read`    | [本页](#history)                             |
| 我的发售月历                   | `/v2/me/calendar`                                                                 | 用户令牌，无 scope                                     | [本页](#calendar)                            |
| 编辑能力                       | `/v2/me/edit-capabilities`                                                        | `catalog:edit`                                         | [本页](#edit-capabilities)                   |
| 编辑配额                       | `/v2/me/edit-quota`                                                               | `catalog:edit`                                         | [本页](#edit-quota)                          |
| 认领                           | `/v2/me/claims`                                                                   | `catalog:edit`                                         | [本页](#claims)                              |
| 编辑提案与配图                 | `/v2/me/proposals`、`/v2/me/edit-images`                                          | `catalog:edit`                                         | [本页](#proposals)                           |
| 封面投票                       | `/v2/me/cover-votes`                                                              | `catalog:edit`                                         | [本页](#cover-votes)                         |
| 资讯投稿                       | `/v2/me/news`、`/v2/me/news-images`                                               | 用户令牌，无 scope                                     | [本页](#news)                                |

缺了表里点名的 scope 是 `403 SCOPE_REQUIRED`，响应里写出缺的是哪一个。

## 按作品汇总 {#works}

```http
GET /v2/me/works
GET /v2/me/works?work_ids=
```

一次读回这个人对作品记下的东西：每部作品在哪些收藏夹里、游玩时长、游玩状态、评分。数值与 `/v2/me/folders/holdings`、`/v2/me/playtimes`、`/v2/me/work-states`、`/v2/me/ratings` 各自的回答相同，免得一个客户端打四次。

带 `work_ids=`（最多 100 个）是批量读，不分页：每个不重复的 id 一项，顺序与请求一致。这个人什么都没记过的作品，以及并不存在的 id，也各有一项，`folder_ids` 为空，时长、状态、评分都是 null。

不带 `work_ids` 时，列出这个人收藏过、报过时长、记过状态或评过分的每一部作品，按作品 id 升序，用 `cursor` / `limit` 翻页，`include_total` 给总数。

封面投票不在这里：它要 `catalog:edit`，清单在 `/v2/me/cover-votes`。

这一面要 `folder:read`。只申请了 `folder:write` 的应用也读得回来。

## 关注的会社 {#followed-companies}

```http
GET    /v2/me/followed-companies
GET    /v2/me/followed-companies/{company_id}
PUT    /v2/me/followed-companies/{company_id}
DELETE /v2/me/followed-companies/{company_id}
```

只要用户令牌，不需要 scope。列表是这个人关注的会社，最新的关注在前。每一项只带 id，用 `/v2/catalog/companies?ids=` 水合。`company_ids=` 是批量读。这份清单只对主人可见；公开的只有计数，在 `/v2/catalog/companies/{id}` 的 `follower_count`。

`PUT` 没有 body。已经关注过的再 PUT 一次，返回存着的那一行，不再写。不存在、已删除，或这个 id 已经被合并走，是 404（合并走的是合并实体那一种 problem）。每人最多 **1000** 家，超出是 422。

`DELETE` 是 204，没有 body，本来就没关注也是 204。单条 GET 在没关注时是 404。

## 屏蔽的标签 {#blocked-tags}

```http
GET    /v2/me/blocked-tags
GET    /v2/me/blocked-tags/{tag_id}
PUT    /v2/me/blocked-tags/{tag_id}
DELETE /v2/me/blocked-tags/{tag_id}
```

只要用户令牌，不需要 scope。列表按最新屏蔽在前排。每一项只带 id，用 `/v2/catalog/tags?ids=` 水合。`tag_ids=` 是批量读。这份清单只对主人可见，目录自己不会拿它去过滤：客户端把 `tree` 模式的 id 放进 `/v2/catalog/works` 和两个月历的 `exclude_tag_tree`，把 `exact` 模式的 id 放进 `exclude_tag_id`。最多 **100** 条，所以整份清单放得进这两个参数。

`PUT` 的 body 可选，`mode` 是 `tree`（缺省）或 `exact`。已经屏蔽过的再 PUT，只改模式，除此之外不再写。未知标签是 404。超出 100 条是 422。

`DELETE` 是 204，没有 body，本来就没屏蔽也是 204。单条 GET 在没屏蔽时是 404。

## 浏览记录 {#history}

```http
GET    /v2/me/history
PUT    /v2/me/history/works/{work_id}
DELETE /v2/me/history/works/{work_id}
DELETE /v2/me/history
```

只要用户令牌，不需要 scope。浏览记录**按应用分开**：每个应用只读、只删自己记下的。用户在作品页真正停留之后再 `PUT`，不要在预取或服务端渲染时发；一分钟内重复的访问只算一次。`PUT` 和两个 `DELETE` 都是 204，没有 body。作品不在上线状态时 `PUT` 是 404。

列表按最近一次访问排序，最新的在前，每一项只带 id，用 `/v2/catalog/works?ids=` 获取作品信息。全年龄站点要带 `content_limit=sfw`，这样用户在成人站看过的作品不会出现在它的页面上。记录保留 **90 天**，每个应用最多 **500** 条，超出的旧记录会被删掉。

`scope=all` 按作品合并所有应用的记录。读要同时满足两件事：令牌带 `history:read`，并且用户在 `/v2/me/privacy` 把 `browsing_history_sharing` 设成了 `shared`。缺前者是 `403 SCOPE_REQUIRED`，缺后者是 `403 BROWSING_HISTORY_NOT_SHARED`。删除带 `scope=all` 时两者都不需要。

用户在 `/v2/me/privacy` 把 `browsing_history` 设成 `off`，会立刻删掉所有应用的浏览记录；之后 `PUT` 仍然是 204，但服务器上什么都不存。站点可以读 `/v2/me/privacy` 来决定要不要显示「最近浏览」。第三方应用只能把这两个开关往更私密的方向改（关闭记录、取消共享），反过来改是 `422`，reason 为 `NOT_PERMITTED`。

## 我的发售月历 {#calendar}

```http
GET /v2/me/calendar
```

这个人关注的会社名下的作品，窗口、参数和 `meta` 与 `/v2/catalog/calendar` 相同。不传 `olang` 表示全部语言，不传 `exclude_company_kind` 表示什么都不排除：人口已经是这个人自己选的，公开月历的缺省（日语、只要商业）会把关注的同人社团或中国会社藏掉。一个人一家都没关注，得到一个空窗口。

只要用户令牌，不需要 scope。不接受 `ids=`。`include=` 可以是 `titles,refs,intros,covers,companies,ratings,tags,credits`；`view=full` 是其中除 `credits` 以外的全部，`credits` 要点名才有。在集合面上，`titles` 选出 latin / localized，`covers` 选出给基础封面定级的两个封面位。完整的 `titles[]`、`covers[]`，以及 relations / releases / popularity / playtimes / series / platforms / screenshots / characters / engines / links，是单条记录的块，在 `/v2/catalog/works/{id}` 和它的子资源上；在这里要其中一块是 `400 UNKNOWN_INCLUDE`。

## 编辑能力 {#edit-capabilities}

```http
GET /v2/me/edit-capabilities/{object}?entity_id=
```

编辑引擎对这个人算出的、每个字段的 `can_propose` / `can_review` / `would_automerge`。`object` 是 `work / company / character / release / tag / engine / series`，未知家族是 404。

这组答案不能挂在 `/v2/catalog/schemas/{object}` 上：那个面不看凭据，同一个 URL 不能按凭据变字段集。两个 URL 分开，用 `fields[].key` 对齐。作品与发行开了主人通道，要带 `entity_id=`；不带的话 `would_automerge` 回答的是类型级的问题，结果是 false。

编辑策略对所有第一方站点是同一套：任何人都能提案；作品与发行由审核者或 `catalog.edit.trusted` 持有者（编辑者）提交时立即生效，作品主人可审核自己作品上的提案；角色与共享词表（会社、标签、引擎、系列）的提案一律等审核。策略可在运行时调整，这里的答案总是当下生效的那一份。

答案已计入配额：`quota_tier` 是调用者的配额档位，`quota_exhausted` 为 true 时所有 `can_propose` 都是 false；编辑者用完当日免审直编额度后 `would_automerge` 变为 false，提交仍可，只是转为待审。`central` 为 true 表示令牌来自中央编辑站，它能查看、裁决、回滚任何站点上的提案。

要用户令牌，而且令牌必须绑在一个目录站点上，并带 `catalog:edit`。

## 编辑配额 {#edit-quota}

```http
GET /v2/me/edit-quota
```

调用者所在的配额档位（`user` / `editor` / `reviewer`），以及三个计数项各自的 `limit` / `used` / `remaining`：

- `proposals`：滑动窗口内提交的提案数，立即生效的也算。用满后提交是 `429 QUOTA_EXCEEDED`。
- `direct_edits`：窗口内免审生效的编辑数。普通用户为 0；编辑者用满后提交照常，只是转为待审；审核者不设上限（`limit` 为 null）。
- `open_proposals`：仍待裁决的提案数，不按窗口。达到上限后提交是 `429 QUOTA_EXCEEDED`，直到有提案被裁决或撤回。

按人计，跨所有目录站点。上限是运行时配置，以这里返回的为准。要用户令牌，令牌须绑在目录站点上并带 `catalog:edit`。

## 认领 {#claims}

```http
GET    /v2/me/claims
POST   /v2/me/claims                 # 投稿一个认领
GET    /v2/me/claims/{id}
PATCH  /v2/me/claims/{id}            # 需要 If-Match
DELETE /v2/me/claims/{id}            # 只能删草稿
```

认领是把目录里的一条记录和你自己站点的条目绑起来。提交时可以带 `refs`（外部锚）或 `field_values`（向导里用户填的字段图）。

> [!NOTE]
> 带 `refs` 的提交如果命中了已存在的作品，会返回 `409` 而不是静默新建一条——这是防止你在目录里造出重复条目。先按 `refs=` 查一次，命中就走已有的那条。

## 编辑提案 {#proposals}

```http
GET    /v2/me/proposals
POST   /v2/me/proposals              # 提一个修改
GET    /v2/me/proposals/{id}
PATCH  /v2/me/proposals/{id}         # 修改或撤回，需要 If-Match
POST   /v2/me/proposals/{id}/amendments   # 追加一条修正，需要 If-Match
POST   /v2/me/edit-images            # 提案里要用的图先传这里
```

提案是「我想把这个字段改成那个值」。可改哪些字段由 `GET /v2/catalog/schemas/{object}` 的 `fields` 给出。列表带 `include=patch`（2.27.0 起）时每一行都有 `patch` 与 `effective_patch`，与详情面一致，列表页不必逐条再读详情。提案与修订历史是**公开只读**的（`/v2/catalog/proposals`、`/v2/catalog/revisions`）——目录的每一次改动都留痕。

## 封面投票 {#cover-votes}

```http
GET    /v2/me/cover-votes
PUT    /v2/me/cover-votes/{cover_id}
DELETE /v2/me/cover-votes/{cover_id}
```

封面项的 `id` 在 `GET /v2/catalog/works/{id}/covers` 里——每个能被投票的东西都在读面上有地址，这是硬性设计约束的一个具体例子。

## 资讯投稿 {#news}

```http
GET    /v2/me/news
POST   /v2/me/news                   # 投稿
GET    /v2/me/news/{id}
PATCH  /v2/me/news/{id}              # 编辑或撤回
POST   /v2/me/news-images            # 上传横幅，返回 banner_hash 用的 hash
```

横幅（封面图）只能来自 `POST /v2/me/news-images`（multipart 的 `file`，JPEG / PNG / WebP）：把它返回的 `hash` 填进 `banner_hash`。用别处上传得到的 hash 会被拒为 `422`（`/banner_hash`，`UNKNOWN_REFERENCE`）——资讯横幅由资讯站点自己的图床身份续期，挂在别的站点名下的图大约一年后会被回收。

不指定源时投到社区源 `community`：任何登录用户都能投，稿件落在 `pending`，版主发布后才出现在公开的 `/v2/news`；滑动窗口内用完 `catalog.news_submissions_per_day` 的账号是 `429 QUOTA_EXCEEDED`。替合作源投稿时，这个源必须绑定在你的账号上并且处于启用状态；稿件落在 `pending`，受信任的源直接 `published`。

## 审核面 {#moderation}

评分的删除与收藏夹的审核不在这一节：[评分与游玩记录 · 版主删除](/docs/play-records#moderation)、[收藏夹 · 审核](/docs/folders#moderation)。

`/v2/moderation/*` 是给**有审核权的用户**用的，同样是用户令牌，能不能看见由那个用户在对应站点上的权限决定。下面这一组还要 `catalog:edit`。

```http
GET  /v2/moderation/claims                     # 认领队列
POST /v2/moderation/claims/{id}/decisions      # 通过 / 拒绝，需要 If-Match
GET  /v2/moderation/proposals                  # 提案队列
POST /v2/moderation/proposals/{id}/decisions   # 需要 If-Match
POST /v2/moderation/reverts                    # 回退到某个修订
GET  /v2/moderation/snapshots/{object}/{id}    # 当前编辑快照
```

每个租户只能看见和决定自己站点的东西，跨租户操作是 `403 TENANT_MISMATCH`。

## 写操作的两个必修课 {#write-hygiene}

### If-Match：不是可选的礼貌

`PATCH /v2/me/claims/{id}`、`PATCH /v2/me/proposals/{id}`、`POST /v2/me/proposals/{id}/amendments` 与两个 `moderation` 决策面都**要求** `If-Match`。流程固定是「GET 拿 `ETag` → 带着它写」：

```javascript
const res = await fetch(url, { headers: auth })
const etag = res.headers.get('ETag')
const current = await res.json()

const write = await fetch(url, {
  method: 'PATCH',
  headers: { ...auth, 'If-Match': etag, 'Content-Type': 'application/json' },
  body: JSON.stringify(patch)
})
// 412 = 有人在你读之后改过：重新 GET，合并，再写一次
```

### Idempotency-Key：网络超时不该写两条

所有 `POST` 都支持 `Idempotency-Key`。同一身份、同一路径、同一个 key 的重复请求会重放首次结果（保留 24 小时）。key 请用 UUID 之类的一次性值，并且**同一个 key 只配同一个 body**——body 不同是 `409 IDEMPOTENCY_KEY_REUSED`；首个请求还没处理完就重试是 `409 IDEMPOTENCY_REQUEST_IN_PROGRESS`，写操作不会被执行两遍，稍等用同一个 key 重试即可。

- [拿用户令牌](/docs/authentication) — 授权码 + PKCE 的四步，以及刷新令牌的注意事项。
- [错误处理](/docs/errors) — 412 / 428 / 409 分别意味着什么，该怎么恢复。
