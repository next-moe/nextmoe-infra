# 平台内部实现

> 本文承载 §5 数据模型、§6 请求生命周期(中间件链)、§8 缓存、§12 可观测 / 计量。设计与命名约定见 [01-design.md](./01-design.md);认证/分层见 [03](./03-auth-and-tiers.md);迁移与运维见 [07](./07-migration-and-ops.md)。

---

## 5. 数据模型(均在主库 `kun_galgame_infra`)

### 5.1 扩展 `oauth_clients`(沿用 Image*/Artifact* 扩展字段范式)

应用(无论 API-key-only 还是 OAuth2)都是一行 `oauth_clients`,新增。**字段是平台级**(tier/配额对整个开放 API 生效,不按媒介拆——per-面权限走 scope):

```go
// --- Developer platform / NextMoe open API extension fields ---
// OwnerUserID: 第三方开发者应用的拥有者(生态账号)。一方站点 client 为 NULL
// (它们靠 SiteID 归属)。门户的 "我的应用" 按此过滤,也用于管理鉴权。
OwnerUserID    *uint  `gorm:"index" json:"owner_user_id,omitempty"`

DevEnabled     bool   `gorm:"not null;default:false" json:"dev_enabled"`      // 准入 NextMoe 开放 API
DevTier        string `gorm:"size:20;not null;default:'free'" json:"dev_tier"` // free|trusted|internal(D2:tier 授予由平台内部完成;身份/角色沿 IdP 五全局角色,不铸新全局角色)
// 限流/配额(0 = 用 tier 默认值,见 03-auth-and-tiers.md §7)
DevRatePerMin  int    `gorm:"not null;default:0" json:"dev_rate_per_min"`
DevQuotaDaily  int    `gorm:"not null;default:0" json:"dev_quota_daily"`

// --- 应用审批流(2026-08-18,见 02 §3.10)---
// DevReviewStatus: approved | pending | declined。存量行迁移时全量回填
// approved(它们都是在"创建无条件自助"年代建的);OAuth 控制台建的一方
// client 不认识这两列,写进去是空串——空串**刻意 fail-open**,判据一律写
// 成 status ∈ {pending, declined},绝不写 status != 'approved'。
DevReviewStatus string `gorm:"size:20;not null" json:"dev_review_status"`
// 拒绝理由,原样回执给申请人;rune 计数 ≤2000(= maxScopeAppMessageLen)。
// 用 text 而非 varchar(n):2000 个汉字装不进按字符计的短 varchar 的直觉里
// 反复出错,text 让长度只由服务端那一个 rune 判据说了算。
DevReviewNote   string `gorm:"type:text" json:"dev_review_note,omitempty"`

// --- 应用生命周期 + 结算名册(2026-08-29,见 02 §3.11)---
// DevArchivedAt: owner 在门户删除应用的时刻。**不是软删的 deleted_at**——行、
// client_id 与它的每一条引用(session / 授权码 / 短链 / 计量历史)全部留下,变
// 的只是它离开 owner 的世界:三个 owner 域查询一律带 dev_archived_at IS NULL,
// 于是它从列表消失、per-app 路由 404,**并且不再计入 5-app 上限**——这正是本
// 波的理由,此前一个被拒的申请永久占着一格且自助面交不回来。
DevArchivedAt *time.Time `json:"dev_archived_at,omitempty"`

// StoreSettlementEligible: 这个应用的点击是否计入其 owner 在每月 DLsite
// 优惠券池里的份额。2026-08-29 落列时存量行全部回填 false;2026-09-19 起
// 新建应用默认 true(运营裁定),运营与归档照常能把它关掉。
StoreSettlementEligible bool `gorm:"not null;default:true" json:"store_settlement_eligible"`
```
> scope 直接复用既有 `AllowedScopes` + `CheckScope`,不另起字段。
> 以上各列由 `devapi.AddOAuthClientDevColumns` 的 raw SQL 加(`ADD COLUMN … NOT NULL DEFAULT 'approved'` 完成回填后 `DROP DEFAULT`),与既有 `dev_*` 列同一模式、同一函数,**必须在 AutoMigrate 之前跑**。
> **`store_settlement_eligible` 是这条模式的例外,DEFAULT 刻意留着**:字段带 `default:` 标签,GORM 在它取零值 `false` 时**把这一列从 INSERT 里省掉**,`DROP DEFAULT` 之后 NOT NULL 会直接拒掉整行。2026-09-19 起 DEFAULT 是 `true`:省掉的那一列落成 `true`,所以新应用一律进名册。(同样保留 DEFAULT 的还有 `dev_nsfw_allowed`,理由不同——Go 侧的字段已经没了。)`dev_archived_at` 可空,存量行回填 `NULL`。
> 模型上的 `gorm:"default:true"` 标签与那条保留的 SQL DEFAULT **必须成对**:带 default 标签的零值字段正是 GORM 从 INSERT 里省掉的那一种,省掉之后接住这一列的就只剩库里的 DEFAULT。§5.4「不用 GORM `default:` 标签」管的是另一类列——那里的列需要写得进零值。运营改这一位走 `UpdateAppFields` 的 map,map 里的 `false` 照常写得进去。

### 5.2 新表 `developer_api_keys`

```go
type DeveloperAPIKey struct {
    ID          uint       `gorm:"primaryKey" json:"id"`
    ClientID    string     `gorm:"size:50;not null;index" json:"client_id"` // FK oauth_clients.id(= 应用)
    Name        string     `gorm:"size:100;not null" json:"name"`           // 开发者起的标签
    KeyHash     string     `gorm:"size:80;not null;uniqueIndex" json:"-"`   // "sha256:<hex>"
    KeyPrefix   string     `gorm:"size:24;not null;index" json:"key_prefix"`// nm_live_a1b2
    Last4       string     `gorm:"size:4;not null" json:"last4"`
    Scopes      datatypes.JSON `gorm:"type:jsonb" json:"scopes"`            // ⊆ 应用 AllowedScopes
    ExpiresAt   *time.Time `json:"expires_at,omitempty"`  // 轮换宽限/有效期;NULL=不过期
    RevokedAt   *time.Time `json:"revoked_at,omitempty"`  // 吊销即拒
    LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
    CreatedByUserID uint   `gorm:"not null" json:"created_by_user_id"`
    CreatedAt   time.Time  `json:"created_at"`
}
func (DeveloperAPIKey) TableName() string { return "developer_api_keys" }
```
有效性:`active = RevokedAt IS NULL AND (ExpiresAt IS NULL OR ExpiresAt > now())`。

> **删行(2026-08-29)**:这张表的行现在删得掉,但只删**已吊销且从未服务过任何请求**的(`RevokedAt` 非空、`LastUsedAt` 为空、且 `developer_api_usage` 里没有它的 `key_id`),否则 **409**;两个条件对 owner 与运营**逐字相同**。`developer_api_usage.key_id` 背后**没有外键**,删掉一把用过的钥匙就把计量历史变成指向虚空的行。`LastUsedAt` 是这道判据里**耐久的那一半**——usage 行会被 [§5.3](#53-新表-developer_api_usage用量落盘按日聚合) 的留存作业修剪掉,那个时间戳不会。**owner 删除应用时同一道判据会扫一遍这张表**:没有用量的钥匙行随应用一起删,有用量的留下(归档后的应用对自助面不可见,留一把 owner 再也够不到的没用过的钥匙等于造一个只有运营清得掉的孤儿)。语义与端点见 [02 §3.11](./02-public-api.md)。

### 5.3 新表 `developer_api_usage`(用量落盘,按日聚合)

```go
type DeveloperAPIUsage struct {
    ID        uint      `gorm:"primaryKey"`
    ClientID  string    `gorm:"size:50;not null;uniqueIndex:idx_usage_day,priority:1"`
    KeyID     uint      `gorm:"not null;uniqueIndex:idx_usage_day,priority:2"` // 0=应用级汇总哨兵
    Face      string    `gorm:"size:40;not null;uniqueIndex:idx_usage_day,priority:3"` // catalog/galgame/galgame_internal[_write|_propose]
    Day       string    `gorm:"size:10;not null;uniqueIndex:idx_usage_day,priority:4"` // YYYY-MM-DD(UTC)
    Count     int64     `gorm:"not null"`
    Status4xx int64     `gorm:"column:status_4xx;not null"`
    Status5xx int64     `gorm:"column:status_5xx;not null"`
    UpdatedAt time.Time
}
```
> 实时计数在 **Redis**(限流/配额计数器),周期 flush 到此表供门户出历史图;`last_used_at` 同理异步回写(每 key 每分钟至多一次)。
> `face` 已是**一等列**(粒度 = (client, key, face, day)),门户能出"按面"曲线;`key_id 0` 是应用级汇总哨兵(不用可空 key_id,避开唯一索引的 NULLs-distinct 语义)。`status_4xx/5xx` 显式列名——GORM 命名策略把 `Status4xx` 蛇形化成 `status4xx`(数字前不加下划线),读写两侧都用 `status_4xx`。

> **留存**:本表只增不减,`prune-developer-usage` 每日 job 删除 `day < 今天−400 天` 的行(400 为拍板值,常量 `DeveloperUsageRetentionDays`)。跨副本单飞由 jobs runner 的按 job 名 advisory lock 提供。

### 5.4 新表 `devapi_policy_overrides`(平台策略矩阵,2026-08-18)

```go
type PolicyOverride struct {
    ID          uint      `gorm:"primaryKey"`
    Capability  string    `gorm:"size:64;not null;uniqueIndex"` // app.create | app.manage | key.mint | scope.apply
    Mode        string    `gorm:"size:20;not null"`             // self_service | approval | disabled
    SetByUserID uint      `gorm:"not null"`
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
func (PolicyOverride) TableName() string { return "devapi_policy_overrides" }
```

- **没有行 = 代码默认**(`devapi.capabilities` 注册表里每个 capability 自带 `Default`,全部为 `self_service`);删行 = 回到默认。语义镜像 `role_permission_overrides`。
- **不建独立 audit 表**:`SetByUserID` + `UpdatedAt` 就是最后写者记录,策略只有四行、只有一个写者角色(`ren`),再加一张表是记账开销大于信息量。
- **不用 GORM `default:` 标签**(零值陷阱)。
- **读路径不上缓存**:门户与管理台的策略读是低 QPS 的人机操作,service 方法内直接一行 DB 查即可;上 Distributor/Redis 那套会给一个每天变零次的值加一层失效面。
- 语义与端点见 [02 §3.10](./02-public-api.md);写路由的权限是 ren-only 的 `devapi.policy_manage`(不可委派,见 `docs/auth/04` §2.3)。

> **迁移**:以上列 + 表都在 `kun_galgame_infra` → `go run ./cmd/migrate`(部署不自动跑,见 [07 §14](./07-migration-and-ops.md))。

---

## 6. 请求生命周期(API 中间件链)

```
Cloudflare(TLS,可能命中边缘缓存→直接返回)
  → Traefik(按 /v1/<face>/ 路由到面服务)
    → 面服务(catalog 或 galgame,同一套中间件):
       1. resolveCredential:  Bearer key/JWT → app + scopes + tier
                              (JWT 本地验;API key 经 introspection + Redis 缓存)
                              失败 → 401
       2. recordUsage:包住其后的整条链,响应回来时按(面, 路由模板, 状态码)记一次
                      + 异步 last_used_at;周期 flush 落 developer_api_usage
       3. rateLimit(Redis,滑动窗口,key=app/key,跨面共享计数) → 超 → 429 + Retry-After + X-RateLimit-*
       4. quota(Redis 当日计数器,跨面共享) → 超 → 429 + X-Quota-*
       5. requireScope(端点所需 scope ⊆ 凭证 scope) → 缺 → 403
       6. ETag(If-None-Match 命中 → 304)
       7. handler(查询 + 设缓存头)
```

> **这条链上曾有第 6 步「nsfw 能力闸」**(wave 213 波 3 加,凭证无 `nsfw_allowed` 带 `nsfw=true` → 403,刻意不降级为 sfw),**已于 2026-08-25 随能力位整体退役**:`nsfw=true` 现在对任何 `catalog:read` 凭证直接生效,与该闸存在之前的形状一致。参数本身没变——不带 `nsfw` 仍是 sfw 投影,由 handler 而非中间件决定。`/v1/catalog/stats`(挂在 group 之上)与 `/v1/news` 从来不在这条链上。

伪代码(中间件):
```go
func OpenAPIAuth(c fiber.Ctx) error {
    cred, err := resolveCredential(c)            // API key(introspect+cache) 或 JWT
    if err != nil { return resp401(c) }
    if !allowRate(cred) { return resp429(c, retryAfter) }   // Redis 滑窗
    if !allowQuota(cred) { return resp429Quota(c) }         // Redis 日计数
    c.Locals("cred", cred)
    return c.Next()
}
// group 上:requireScope("catalog:read")   // NSFW 能力闸已于 2026-08-25 退役
```

---

## 8. 缓存(公开读的承重墙)

**关键设计:把"鉴权"与"响应内容"解耦,让公开读对 Cloudflare 可缓存。**

- 同一 `content_limit` + 同一版本下,公开目录读的**响应内容对所有调用者相同**(与是哪把 key 无关)。鉴权只用于**限流/计量**,不改变响应体。
- 因此缓存键 = `(path, query, content_limit, /v1)`,**不含 key**;响应可带 `Cache-Control: public, s-maxage=…`,被 Cloudflare 边缘共享缓存。
- 把 calendar 已验证的模式铺到两个面的热路径(galgame list/detail/batch/官方成员;catalog works/persons/labels 详情):
  - 弱 **ETag**(嵌 `max(updated)` 或资源指纹)→ `If-None-Match` 命中回 304。
  - `Cache-Control`:历史/稳定数据 `s-maxage` 长(如 1 天),易变的短(如 5 分钟);`max-age=0` 让浏览器每次回源校验。
  - `Cache-Tag`(Cloudflare Cache Rules / 按内容键)便于精准失效。
- 鉴权失败 / 配额头等**不可缓存**部分,仍在回源层处理(CF 仅缓存 2xx 公开读)。
- catalog 面的 **301 redirect 响应同样可缓存**(旧 ID → canonical 是永久事实)。
- **备注(吸收自 API 设计 skill,用在对的地方)**:`GET /v1/galgame` 列表在 6 万→10 万+ 目录上,offset 深翻页有性能悬崖 → 公开列表改 **游标分页**(`cursor`/`next_cursor`),既稳又对缓存友好。(该面已于 2026-07-30 摘牌,但结论**原样适用**于接棒的 `GET /v1/catalog/works`,后者本就是 keyset 分页。)

> 这一节是"开放 API 代价可控"的核心:做好缓存 + CF,绝大多数公开读在边缘命中,回源服务不被打爆。

---

## 12. 可观测 / 计量

- Redis 实时计数(限流/配额)→ 周期 flush `developer_api_usage` → 门户曲线 + 配额执行 + 告警。
- 每请求:`last_used_at` 异步回写;按 (client, key, face, day) 聚合 count/4xx/5xx。
- **账户级 `GET /dev/usage?days=N`**(user-JWT,owner-guarded;`OwnerUsageSummary`)一次返回:
  - `daily[]`——窗口内稠密日序列(缺口补 0,老→新),供柱状图;`total_count/4xx/5xx` 为窗口合计。
  - `by_app[]`——每应用合计(按量降序);`by_face[]`——每 face 合计 `{ face, count, status_4xx, status_5xx }`(按量降序)。以上皆读 `developer_api_usage` rollup。
  - `live[]`——**实时剩余**(章程 05 §9 的账户级兑现):owner 每把 active key 一行 `{ app_name, key_id, rate_limit, quota_limit, quota_used, quota_remaining, quota_reset }`。**直接读 Redis 执法计数器**(`quota:{key_id}:{UTC日}`,与限流/配额同源,不从 rollup 估算);`quota_reset` = 下个 UTC 零点的 epoch 秒。Redis 不可达时 `live` 为空数组并加 `live_unavailable: true`(读面降级,绝不 5xx)。
- **留存**:`prune-developer-usage` 每日 job 修剪 `developer_api_usage`(见 [§5.3](#53-新表-developer_api_usage用量落盘按日聚合))。
