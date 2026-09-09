---
title: 接入用户数据
eyebrow: 集成指南
description: 用 OAuth 用户访问令牌接入 NextMoe 的用户面：游玩时长、认领、编辑提案、封面投票、资讯投稿，以及审核队列。
---

# 接入用户数据

`/v2/me` 与 `/v2/moderation` 读写的是**某个用户自己的东西**。应用密钥在这两个前缀下一律无效——它证明不了「哪个用户」。

## 先拿到用户令牌 {#token}

标准 OAuth 2.0 授权码 + PKCE，端点在 `https://account.nextmoe.com/api/v1`。完整步骤见 [鉴权与凭据](/docs/authentication#user-token)。拿到 `access_token` 后照常放进 `Authorization: Bearer`——和应用密钥同一个位置，但**不要同时带两个**。

> [!WARNING]
> 用户面的限流按**用户**计数，不是按 IP。所以一定要带用户令牌调用；用别的方式代理会让你的全体用户挤进同一个桶。

## 六类用户数据 {#surfaces}

### 游玩时长

```http
GET    /v2/me/playtimes              # 我的全部记录
GET    /v2/me/playtimes/{work_id}    # 单部作品
PUT    /v2/me/playtimes/{work_id}    # 覆盖写
POST   /v2/me/playtimes              # 批量写
DELETE /v2/me/playtimes/{work_id}
```

这一组**只要用户令牌**，不需要任何额外 scope——任何已开通用户登录的应用都可以调用。批量写用 `POST /v2/me/playtimes`，导入历史记录时别用 `PUT` 逐条打。

### 游玩状态

```http
GET    /v2/me/work-states             # 我的全部状态
GET    /v2/me/work-states/{work_id}   # 单部作品
PUT    /v2/me/work-states/{work_id}   # 覆盖写
POST   /v2/me/work-states             # 批量写
DELETE /v2/me/work-states/{work_id}
```

状态是两个轴：`state` 是封闭词表 `wish / doing / done / on_hold / dropped`，与 Bangumi 收藏类型一一对应；`completion` 表示通了多少（`one_route / main / all`），可以不填，`wish` 不允许携带它。`PUT` 是整体替换——不带 `completion` 就等于清掉它。这一组和游玩时长一样**只要用户令牌**，不需要任何额外 scope。

### 认领

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

### 编辑提案

```http
GET    /v2/me/proposals
POST   /v2/me/proposals              # 提一个修改
GET    /v2/me/proposals/{id}
PATCH  /v2/me/proposals/{id}         # 修改或撤回，需要 If-Match
POST   /v2/me/proposals/{id}/amendments   # 追加一条修正，需要 If-Match
POST   /v2/me/edit-images            # 提案里要用的图先传这里
```

提案是「我想把这个字段改成那个值」。可改哪些字段由 `GET /v2/catalog/schemas/{object}` 的 `fields` 给出。提案与修订历史是**公开只读**的（`/v2/catalog/proposals`、`/v2/catalog/revisions`）——目录的每一次改动都留痕。

### 封面投票

```http
GET    /v2/me/cover-votes
PUT    /v2/me/cover-votes/{cover_id}
DELETE /v2/me/cover-votes/{cover_id}
```

封面项的 `id` 在 `GET /v2/catalog/works/{id}/covers` 里——每个能被投票的东西都在读面上有地址，这是硬性设计约束的一个具体例子。

### 资讯投稿

```http
GET    /v2/me/news
POST   /v2/me/news                   # 投稿，总是落 pending
GET    /v2/me/news/{id}
PATCH  /v2/me/news/{id}              # 编辑或撤回
```

投稿总是落在 `pending`，由人工过审后才出现在公开的 `/v2/news`。授权模型是「有源行即有资格」：你在资讯源表里有一行，就能替那个源投稿，没有单独的申请流程。

## 审核面 {#moderation}

`/v2/moderation/*` 是给**有审核权的用户**用的，同样是用户令牌，能不能看见由那个用户在对应站点上的权限决定。

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

所有 `POST` 都支持 `Idempotency-Key`。同一身份、同一路径、同一个 key 的重复请求会重放首次结果（保留 24 小时）。key 请用 UUID 之类的一次性值，并且**同一个 key 只配同一个 body**——body 不同是 `409 IDEMPOTENCY_KEY_REUSED`。

- [拿用户令牌](/docs/authentication) — 授权码 + PKCE 的四步，以及刷新令牌的注意事项。
- [错误处理](/docs/errors) — 412 / 428 / 409 分别意味着什么，该怎么恢复。
