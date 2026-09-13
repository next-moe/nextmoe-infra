# 18 · 配置中心(运行期可调项)

> 你在这里:某个开关或阈值要改(比如关闭上传、把 trust 从 shadow 切到 live),不想改面板再重部署。
> 本篇说明配置中心管什么、值从哪来、怎么改、改了怎么验证。环境变量全集仍在
> [15-environment](./15-environment.md);配置中心是它之上的**运行期一层**,不是替代。

## 18.0 一句话模型

每个可调项是代码里声明的一个**键**(`internal/platform/settings/keys`),有类型、默认值、范围和说明。
进程内的生效值按三层解析,后者压前者:

| 层 | 载体 | 何时生效 | 谁能改 |
|---|---|---|---|
| 1. 代码默认 | `keys.go` 里的声明 | 编译期 | 改代码 + 部署 |
| 2. 环境变量地板 | 键声明的 `EnvVar`(和 15-environment 里的同名变量) | 进程启动时读一次 | 面板 / compose + 重部署 |
| 3. 数据库覆盖值 | 主库 `kun_galgame_infra` 的 `setting_overrides` | 写入后 30 秒内所有服务生效(oauth / image / artifact 经 Redis 推送为毫秒级) | 控制台 `/settings`,需 `settings.write` |

第 3 层有两种行:**平台行**(所有服务读的那一份)和**站点行**(`scope=site:<sites.id>`,只对声明了
「可按站点覆盖」的键存在)。自 W3-c 起站点行也进程内生效:`keys.X.Get()` 仍只看平台行,
`keys.X.ForSite(siteID)` 有站点行时返回站点值、否则退回平台生效值。

读取是进程内原子指针,请求路径零网络;每个服务每 30 秒向主库拉一次平台行与站点行(各几十行),
两批都取到才一起应用——站点查询失败时整份沿用上一份快照。
刷新失败沿用上一份;启动时连续失败则只带 1+2 两层运行并打 `settings: initial load failed` 的
ERROR 日志。数据库里的一行若类型不符、越界或枚举外,会被忽略并退回 2/1 层,日志有 WARN。

**边界**:配置中心只收运行期可调项(开关、阈值、TTL、配额)。密钥、host/port/dsn/bucket、S3
path style、PG 连接池、OIDC 签名算法、aff URL 模板等启动期接线永远留在环境变量里。

## 18.1 现有键(86 个)

| 域 | 键数 | 来源 |
|---|---|---|
| `platform` | 2 | W2,站点契约 |
| `auth` | 7 | W1 1 + W3-a 6 |
| `image` | 5 | W1 1 + W3-b 4(GC) |
| `artifact` | 9 | W1 8 + W3-b 1(GC) |
| `trust` | 11 | W1 4 + W3-a 7 |
| `ai` | 6 | W1 3 + W3-a 1 + W3-b 2(模型名) |
| `store` | 7 | W1 1 + W3-b 6(价格面) |
| `apiv2` / `catalog` / `community` / `developer` | 4 / 2 / 7 / 2 | W3-a 新域 |
| `jobs` | 24 | W2,12 任务 × 2 |

全目录以控制台 `/settings` 为准——**注册表即界面**,每个键的类型、范围与中英文说明都在
`internal/platform/settings/keys` 的声明里(`keys.go` = platform + W1,`policy.go` = W3-a,
`boot.go` = W3-b,`jobs.go` = 任务表)。下面只展开 platform 与 W1 的详表,W3 两批列到键名。

### 平台策略(`platform`,W2)

没有环境变量地板;没有仓内消费者——它们是下发给各站点的契约,读面见 18.7。

| 键 | 类型 | 默认 | 公开 | 可按站点覆盖 | 站点必须做什么 |
|---|---|---|---|---|---|
| `platform.read_only` | bool | false | 是 | 是 | 拒绝一切写操作并展示提示;为整机搬迁的只读窗口准备 |
| `platform.notice` | string(≤ 500,单行) | 空 | 是 | 是 | 页面顶部展示这行公告 |

### 运行期可调项(W1,18 个)

`image.upload_enabled` 与 `artifact.upload_enabled` 同时是**公开键**(随读面下发,站点据此隐藏上传入口),
自 W3-c 起也**可按站点覆盖**并按调用站执法:image 上传门与 artifact 三个上传入口按 client 绑定的
站点读 `ForSite`,未绑定站点的 client 用平台值。image 的上传门排在鉴权之后——未认证请求在上传
关闭时回的是 401,不再是 503(认证过的仍是 503)。

| 键 | 类型 | 默认 | 环境变量地板 | 生效位置 |
|---|---|---|---|---|
| `auth.verification_code_ttl_minutes` | int 1..1440 | 15 | `KUN_AUTH_VERIFICATION_CODE_TTL_MINUTES` | oauth 注册/改邮箱验证码 |
| `image.upload_enabled` | bool | false | `KUN_IMAGE_UPLOAD_ENABLED` | image `POST /image/upload`(关闭时 503) |
| `artifact.upload_enabled` | bool | false | `KUN_ARTIFACT_UPLOAD_ENABLED` | artifact 上传/续传/完成三个入口 |
| `artifact.multipart_threshold_bytes` | int | 52428800 | `KUN_ARTIFACT_MULTIPART_THRESHOLD` | 超过此大小走分片 |
| `artifact.part_size_bytes` | int ≥ 5 MiB | 16777216 | `KUN_ARTIFACT_PART_SIZE` | 分片大小 |
| `artifact.presign_upload_ttl_seconds` | int | 3600 | `KUN_ARTIFACT_PRESIGN_UPLOAD_TTL_SECONDS` | 上传预签名有效期 |
| `artifact.presign_download_ttl_seconds` | int | 86400 | `KUN_ARTIFACT_PRESIGN_DOWNLOAD_TTL_SECONDS` | 下载预签名有效期 |
| `artifact.orphan_ttl_hours` | int | 24 | `KUN_ARTIFACT_ORPHAN_TTL_HOURS` | `artifact-gc` 孤儿上传回收 |
| `artifact.softdelete_ttl_hours` | int | 168 | `KUN_ARTIFACT_SOFTDELETE_TTL_HOURS` | `artifact-gc` 软删物理回收 |
| `artifact.reclaim_min_idle_seconds` | int | 3600 | `KUN_ARTIFACT_RECLAIM_MIN_IDLE_SECONDS` | 控制台回收文件的最小空闲时长 |
| `trust.scan_enabled` | bool | false | `KUN_TRUST_SCAN_ENABLED` | community 异步扫描是否发往 trust |
| `trust.check_enabled` | bool | false | `KUN_TRUST_CHECK_ENABLED` | community 同步词表门 |
| `trust.scan_mode` | enum shadow / live | shadow | `KUN_TRUST_SCAN_MODE` | trust 平台默认执法姿态(站点策略可再覆盖) |
| `trust.scan_sample_rate` | float 0..0.05 | 0 | `KUN_TRUST_SCAN_SAMPLE_RATE` | trust 平台默认抽检比例 |
| `ai.escalate_threshold` | float 0..1 | 0.4 | `KUN_AI_ESCALATE_THRESHOLD` | Tier1 → Tier2 升级阈值 |
| `ai.negative_sample_rate` | float 0..1 | 0.05 | `KUN_AI_NEGATIVE_SAMPLE_RATE` | 阴性抽样比例 |
| `ai.force_escalate` | 列表,每项 `site:kind` | 空 | `KUN_AI_FORCE_ESCALATE`(逗号分隔) | 强制升级的 (站点, 内容类型) 对 |
| `store.link_quota_per_client` | int | 5000 | `KUN_STORE_LINK_QUOTA_PER_CLIENT` | 每个调用站可铸链的商品数上限 |

单位写在键名里,环境变量的写法与迁入前完全一致。

### 请求路径策略(W3-a,29 个)

原代码常量收编,无环境变量地板、无公开/站点覆盖;改了 30 秒内在请求路径生效:

- `apiv2.*`:`default_rate_per_minute` / `default_quota_per_day` / `auth_fail_per_minute` / `auth_fail_block_seconds`
- `auth.*`:`ip_rate_per_minute` / `token_endpoint_rate_per_minute` / `strict_rate_per_minute` / `allowed_email_domains` / `verification_resend_cooldown_seconds` / `register_gift_points`
- `trust.*`:`report_rate_window_minutes` / `report_rate_max_per_window` / `aggregate_threshold` / `new_account_age_days` / `new_account_reporter_weight` / `policy_cache_ttl_seconds` / `term_cache_ttl_seconds`
- `ai.moderate_max_tokens`
- `community.*`:`sandbox_max_links` / `sandbox_max_images` / `sandbox_max_mentions` / `sandbox_max_topics_per_day` / `sandbox_max_replies_per_day` / `sandbox_window_hours` / `flag_hide_threshold`
- `catalog.*`:`totals_cache_ttl_seconds` / `merge_cooling_off_hours`
- `developer.*`:`credential_cache_ttl_seconds` / `credential_cache_negative_ttl_seconds`

### 启动期旋钮收编(W3-b,13 个)

原 `pkg/config` 字段迁入,六个键保留原环境变量作地板
(`KUN_STORE_PRICE_ENABLED` / `_USER_AGENT` / `_STEAM_REGIONS` / `_DLSITE_CURRENCIES`、
`KUN_AI_UPSTREAM_MODEL` / `KUN_AI_OMNI_MODEL`),其余无地板:

- `image.gc_*`:`cold_after_days` / `softdelete_after_days` / `harddelete_after_days` / `max_per_run`(image-gc 任务开跑时读)
- `artifact.gc_max_per_run`(artifact-gc 任务开跑时读)
- `store.price_*`:`enabled`(总开关,关闭时报价请求 503、后台刷新跳过)/ `user_agent` / `steam_regions` / `dlsite_currencies` / `wait_on_miss_ms` / `fresh_for_hours`
- `ai.upstream_model` / `ai.omni_model`

**例外:`store.price_steam_regions` 与 `store.price_dlsite_currencies` 在 catalog 启动时读一次**,
改了要重启 catalog 才生效(fetcher 构造期定型);其余 84 个键都是使用时读。

### 后台任务(`jobs`,W2,12 × 2)

oauth 上的调度器(`internal/jobs`)每个任务两把键,没有环境变量地板,默认值就是迁入前 `all.go`
里写死的时刻;详见 18.6。

| 任务 | `jobs.<n>.enabled` 默认 | `jobs.<n>.schedule` 默认 |
|---|---|---|
| `image-gc` | true | `daily@03:30` |
| `galgame-image-refping` | true | `daily@04:00` |
| `catalog-image-refping` | true | `daily@04:15` |
| `news-image-refping` | true | `daily@04:20` |
| `user-avatar-refping` | true | `daily@04:30` |
| `image-ref-audit` | true | `daily@04:45` |
| `artifact-gc` | true | `daily@05:30` |
| `prune-developer-usage` | true | `daily@06:00` |
| `ymgal-news-poll` | true | `every:10m` |
| `ymgal-news-sweep` | true | `daily@04:05` |
| `store-stats-sync` | true | `every:1h` |
| `news-moderate` | true | `every:5m` |

键名里的 `<n>` 是任务名把 `-` 换成 `_`(`jobs.image_gc.schedule`)。

## 18.2 怎么改

控制台 `admin.nextmoe.dev` → 侧栏「配置中心」(`/settings`)。页面按域列出全部键:生效值、来源
(`已覆盖` = 有数据库行;`默认` = 无行)、环境变量名、备注与最后修改人。持有 `settings.write`
(默认只有 ren;可在权限矩阵委派给 admin)的人可「编辑」或「撤销覆盖」,每次变更记审计
(旧值 → 新值 + 备注),页面底部可见。

页面顶部的「作用域」默认是「平台(全局)」;选中某个站点后只列出可按站点覆盖的键,来源多一种
`站点覆盖`,没有站点行的键显示它继承的平台生效值。站点行自 W3-c 起与平台行同样进程内生效
(上传两开关按调用站执法)并同样触发推送/轮询刷新,也继续喂 18.7 的读面。

页面显示的「来源」不含环境变量:oauth 进程看不见其他容器的环境。所以当某键显示「默认」而所属
服务的 compose 里仍设着同名变量时,该服务实际用的是变量值。真值看 18.3。

API(供脚本):
```
GET    /api/v1/admin/settings                总览(domains[].keys[]:key/kind/default/effective/source/override/site_scoped/public)
GET    /api/v1/admin/settings?site=<id>      某站点的总览(只含可按站点覆盖的键;source 可为 site;inherited = 平台生效值)
GET    /api/v1/admin/settings/audit?limit=   最近变更(含 scope_kind / scope_id)
PUT    /api/v1/admin/settings/{key}[?site=]  {"value": <json>, "note": "...", "version": <上次看到的 version,可选>}
DELETE /api/v1/admin/settings/{key}?note=[&site=]   撤销覆盖
```
`version` 不符返回 409;类型/范围/枚举不符返回 400 并带原因;对不可按站点覆盖的键带 `site` 写入返回
400「该配置不支持按站点覆盖」;`site` 不存在返回 404。

## 18.3 怎么验证真的生效了

每个服务启动时打一行 `settings: loaded keys=86 overrides=N env_floor=M site_overrides=K`,之后每当
某键的值或来源变化打一行 `settings: applied key=... value=... source=db|env|default`;站点行的增删改
打 `settings: site override applied|removed key=... site=...`。改完值后 30 秒内在目标服务的
容器日志里应看到对应的 `applied` 行;没看到 = 该服务没连上主库或没启动分发器,先查它的启动日志。

```bash
docker logs --since 2m <container> 2>&1 | rg 'settings: (loaded|applied|initial load failed)'
```

## 18.4 首次上线顺序(从环境变量迁过来)

1. 主库先建表:`go run ./cmd/migrate`(`setting_overrides` / `setting_audit_logs`;部署不会自动跑)。
2. 部署。此时所有键仍由 compose 里的环境变量地板决定,行为不变。
3. 在控制台把 compose 里现设的值写成数据库行(至少 `image.upload_enabled` /
   `artifact.upload_enabled` / `trust.scan_enabled` / `trust.check_enabled` = true,以及面板上
   设过的 `trust.scan_mode` / `trust.scan_sample_rate` / `ai.*`)。
4. 确认各服务日志出现 `applied ... source=db` 后,再从 compose 拔掉这些环境变量行。**先建行再拔
   env**,反过来会有一段上传被关闭的窗口。

生产已于 2026-09-03 走完这四步(W3-e):控制台建了 7 行(上传两开关 / trust 三键 /
`ai.force_escalate` / `store.link_quota_per_client`),`docker-compose.prod.yml` 里这 7 键的
env 行已拔。此后删掉某行 DB 值,回落的是**代码默认**(不再有 env 地板)。

## 18.5 怎么新增一个键

1. 在 `internal/platform/settings/keys` 里对应域的文件声明(`settings.Bool/Int/Float/String/Enum/StringList`),
   给默认值、范围/枚举、中英说明;要保留环境变量地板就填 `EnvVar`;要随读面下发给站点就标 `Public`,
   允许站点单独覆盖就标 `SiteScoped`(两者都是契约,加了就不能撤)。新的定时任务不在这里声明,
   而是在 `keys/jobs.go` 的表里加一行(见 18.6)。
2. 把它加进该域的 `Keys` 列表;`TestLiveRegistryIsWellFormed` 的 golden 表补一行。
3. 消费处在**使用时**读 `keys.X.Get()`(可按站点覆盖的键在知道调用站时读
   `keys.X.ForSite(siteID)`),不要在构造时读进结构体字段(那正是迁走的东西)。
4. 测试里用 `settings.Override(t, keys.X, v)` 注入,cleanup 自动还原。
5. 不需要迁移、不需要改控制台:注册表即界面。

不进配置中心的:密钥与接线(见 18.0 边界)、一次性 cmd 工具专用的 `KUN_*`(它们仍直接读 env)。

## 18.6 后台任务的开关与时刻

调度器只跑在 oauth 上。每个任务的 `jobs.<n>.enabled` / `jobs.<n>.schedule` 由调度循环每 30 秒重读:
改了 `schedule`,下一次触发时刻按新值重算;`enabled=false` 到点时跳过并记一行
`jobs: skipped, disabled`,再前进到下一个时刻;控制台「立即运行」不看 `enabled`。

`schedule` 只有两种写法:`daily@HH:MM`(进程本地时间,生产容器是 UTC)与 `every:<N>m` / `every:<N>h`
(N ≥ 1 的整数)。控制台按这个正则拦截其它写法;直接改库写了坏值,该任务空转并打
`jobs: bad schedule` 的 ERROR,改回来即恢复。

新增一个定时任务:`internal/jobs/all.go` 里 `Register`,并在 `internal/platform/settings/keys/jobs.go`
的表里加 `{"<name>", "<默认 schedule>"}` 一行。漏了后者,进程启动即 panic,单元测试也会指名。

## 18.7 下发给站点的读面

`GET /api/v1/settings`(OAuth Client Basic Auth)返回调用方站点应遵守的全部公开键,带强 `ETag`,
`If-None-Match` 相同回 304。站点每 30–60 秒轮询,内存快照,失败沿用上一份。契约与参考客户端见
[docs/integration/oauth/14-settings.md](../integration/oauth/14-settings.md)。

读面报告的是 oauth 进程看到的值(数据库行,否则代码默认),看不见其它服务的环境变量地板——这也是
18.4「先建行再拔 env」的另一个理由。

