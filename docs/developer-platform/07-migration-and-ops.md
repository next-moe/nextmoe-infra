# 迁移与运维提醒

> 本文承载 §14 迁移与运维提醒。设计与命名约定见 [01-design.md](./01-design.md);数据模型见 [04 §5](./04-platform-internals.md);认证/分层见 [03](./03-auth-and-tiers.md)。

---

## 14. 迁移与运维提醒

- **数据库迁移(主库 `kun_galgame_infra`)**:新增 `oauth_clients` 列(`owner_user_id` + `dev_*`)+ 新表 `developer_api_keys` / `developer_api_usage` → **`go run ./cmd/migrate`**。⚠️ 部署**不自动**跑迁移,漏跑 = GORM 读不存在的列 → 静默失败(参见仓库历史教训)。
- **2026-08-18 策略矩阵 + 应用审批(同一条命令)**:`oauth_clients` 再加两列(`dev_review_status` / `dev_review_note`,前者带临时 DEFAULT 把存量行全量回填成 `approved` 后 DROP DEFAULT)+ 新表 `devapi_policy_overrides` → **`go run ./cmd/migrate`**,库 `kun_galgame_infra`。**必须先迁移再上新代码**:漏跑时 GORM 的 `SELECT *` 读不到 `dev_review_status`,`appAwaitsReview` 看到的永远是零值——闸门会静默失效(fail-open,不报错)。新表为空即全平台走代码默认(= 迁移前的行为),故迁移本身对现有开发者零可见变化。
- **2026-08-29 应用生命周期 + 结算名册(同一条命令)**:`oauth_clients` 再加两列(`dev_archived_at` 可空、存量回填 `NULL`;`store_settlement_eligible NOT NULL DEFAULT false`,存量全部 `false`)→ **`go run ./cmd/migrate`**,库 `kun_galgame_infra`,同一个 `devapi.AddOAuthClientDevColumns`。**必须先迁移再上新代码**:漏跑时 GORM 的 `SELECT *` 读不到 `dev_archived_at`,而三个 owner 域查询的 `WHERE dev_archived_at IS NULL` 会直接**报错**(列不存在)——门户「我的应用」整页 500,是响的失败不是静默的。**`store_settlement_eligible` 的 DEFAULT 刻意不 DROP**,与其余 `dev_*` 列不同:`false` 是 Go 零值,GORM 每条 INSERT 都省掉这一列,没有 default 的 NOT NULL 会拒掉整行(见 [04 §5.1](./04-platform-internals.md))。迁移本身对现有开发者零可见变化(没有行是已归档的,也没有行进结算名册)。
- **2026-09-19 奖励券改按用户分 + 名册默认进(同一条命令)**:`store_coupons` 去掉 `client_id`、加 `user_id`;`store_coupon_shares` 主键从 `(batch_id, client_id)` 改成 `(batch_id, user_id)`(AutoMigrate 不改主键,所以由 `storeModel.RekeyCouponsByOwner` 在 AutoMigrate 之前整表重建;表里有按应用分过的券或份额时拒绝执行而不是丢数据——当日生产三张券表都是 0 行);`oauth_clients.store_settlement_eligible` 的 DEFAULT 改成 `true`,存量行不动。→ **`go run ./cmd/migrate`**,库 `kun_galgame_infra`(生产部署时 migrate 容器先于 oauth 自动跑)。迁移到新代码上线之间,旧 oauth 读券表会因 `client_id` 不存在报错,只影响那几十秒里打开券页面的人。
- **新域名**:`api.nextmoe.dev`、`developer.nextmoe.dev` → DNS + Cloudflare(含公开读的 Cache Rules)+ Traefik 路由(按 `/v1/<face>/` 分发到 catalog/galgame 服务)+ 各后端 CORS allowlist。
- **Redis**:新增 `ratelimit:*` / `quota:*` / `apikey:*`(introspection 缓存)键空间。
- **契约**:公开 spec ×2 纳入 `docs:verify` + oasdiff,在 kungal-docs 登记为对外 Tier-A 契约。
- **面服务中间件**:catalog/galgame 两面共用鉴权/限流/配额中间件——首选提取为共享包(`kungal-kit` 候选);过渡期同构复制时,两处必须同步演进(写进各自 README owner 声明)。
