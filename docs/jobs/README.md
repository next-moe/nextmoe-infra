# 定时/可触发任务架构 —— 方案对比（决策文档）

> 状态：**决策文档，未实施**。本文只做三方案权衡，供拍板；选定后再单独写实施方案。
> 触发点：补 `galgame-image-refping` 每日 cron 时发现，本仓 `cmd/` 下"需要被周
> 期/按需运行"的任务不少，但调度散落在代码库外、无法被 admin 手动触发、无统一
> 可见性。需要定一个长期形态。
>
> 以代码为准（2026-05 核对）；本文引用的分类基于 `apps/api/cmd/` 实际内容。

---

## 1. 现状盘点

把 `apps/api/cmd/*` 全量按"该不该被调度器自动跑"分类：

| 类别 | 成员 | 自动调度？ |
|---|---|---|
| **真·周期任务** | `sync-vndb`(日增量)、`galgame-image-refping`(日)、`image-gc`(日 TTL GC) | 是（本文核心对象） |
| **按需/偶发**（适合 admin 手动触发，不自动） | `reindex-search`、`sync-vndb-relations` | 否（手动为主） |
| **常驻 worker**（不是 cron，是服务） | `image-moderation-worker`、`worker` | 否（独立常驻进程） |
| **一次性运维/迁移**（绝不能自动跑） | `migrate`、`migrate-galgame`、`migrate-galgame-data`、`migrate-moyu-galgame`、`migrate-galgame-banners-to-image-service`、`seed`、`image-setup`、`cleanup-bogus-vndb-id`、`dedup-galgame-alias` | 否（永远手动） |
| **HTTP 服务** | `galgame`、`image`、`oauth`、`artifact`、`moderation` | 否 |

**关键事实**：真正"要定时 + 想可手动"的核心集合只有 **3 个日任务 + 2 个按需任务**，规模小且稳定。

### 现在是怎么调度的

- 周期任务：靠**项目外**的 OS crontab / k8s CronJob 触发 `cmd/*` 二进制（`cmd/image-gc` 头注释就写"Run it as a cron"）。调度表不在 git 里。
- 仅两处**进程内** ticker：`internal/app/cleanup.go`（1h）、`internal/platform/image/service/moderation_worker.go`（常驻 poll）。
- **没有**：cron 库依赖、job 注册表、`job_run` 历史表、admin 触发入口。
- **已具备**（任何方案都可复用）：admin 鉴权 `middleware.Auth + RequireRole("admin")`、`/admin/*` 路由组、apps/admin 管理 UI、结构化日志、job 本身已按幂等设计（sync/ping/gc 重跑安全）。

---

## 2. 需求

1. 周期任务**默认自动执行**（不依赖人记得配外部 cron）。
2. 也能由 **admin 手动触发**（重跑、补数据、排障）。
3. 统一**可见性**：有哪些 job、上次何时跑、成功/失败、摘要。
4. 不为小规模任务集引入过度工程；不重复业务代码（monorepo 共享 `internal/`/`pkg/`）。

---

## 3. 三方案

### 方案 A —— 独立 cron 项目/模块

**形态**：把所有周期任务收拢到一个单独部署单元（独立 repo，或 monorepo 内独立 module + 独立部署）。

| | |
|---|---|
| 优点 | failure domain 隔离（跑挂不波及 API）；调度集中在一处 |
| 缺点 | 独立 repo → 必须重复或反向依赖共享 `internal/`（models/DB/config/imageclient/search），强耦合；独立 module + 独立部署 → **其实就是现在的 `cmd/` 本身**，并没解决"admin 可触发 + 统一可见"；多一套部署/发布/监控 |
| 工作量 | 中（搭项目骨架）~ 高（拆依赖） |
| 适用 | 任务量大、团队规模大、需独立 SLA 时。**本仓 5 个任务不到这个量级** |

### 方案 B —— admin 服务内的 job registry（推荐，详见 §5）

**形态**：周期任务下沉为可调用函数，admin/API 服务内置轻量调度器默认自动跑，并暴露 admin 触发接口 + UI + 运行历史。

| | |
|---|---|
| 优点 | 充分复用已有 admin 鉴权/路由/UI/日志；直接满足"默认自动 + admin 触发 + 可见"；单部署单元、零业务代码重复；调度表入 git（即代码） |
| 缺点 | job 与 admin 服务同生命周期；需自己处理：长任务异步化、多副本单飞、panic 隔离（见 §4） |
| 工作量 | 中（下沉 N 个 job + 调度器 + `job_run` 表 + 锁 + 一个 UI 面板） |
| 适用 | 任务集小而稳定、已有 admin 体系——**正是本仓现状** |

### 方案 C —— 纯外部调度 + 最小 admin 触发

**形态**：保持 OS/k8s 调度 `cmd/*` 二进制不变，仅加一个 admin 端点能"按需 dispatch 一次"。

| | |
|---|---|
| 优点 | 改动最小；保留 failure 隔离（独立进程） |
| 缺点 | 跨进程触发别扭（需 exec 二进制 / 投队列 / k8s Job API）；调度表仍在代码库外、易漂移；"统一可见性"难做（运行结果在另一个进程的日志里）；只算打补丁，没解决根因 |
| 工作量 | 低 |
| 适用 | 只想最小止血、不追求统一管理时 |

---

## 4. 通用工程约束（任何方案都要满足）

1. **边界要硬**：只有"真·周期 + 按需"那 5 个进调度体系；一次性迁移/常驻 worker/HTTP 服务**不进**。把 migrate-* 暴露成"一键点"是危险的。
2. **长任务异步**：`sync-vndb` 全量 15–20 分钟。触发必须后台 goroutine + `context` 取消，立即返回 run_id，异步查状态——绝不阻塞 HTTP 线程。
3. **多副本单飞**：admin/调度进程若多实例，需全局保证同一 job 只有一个实例在跑。推荐 **Postgres advisory lock**（或 `job_run` 唯一约束）。job 本身已幂等，这层只防并发浪费/打架。
4. **panic 隔离**：每个 job 包 `recover`，单个 job 崩不能带崩宿主进程。
5. **调度表即代码**：cron 表达式进 git/config，可 review、可回滚，杜绝"线上 crontab 漂移、没人知道为什么停了"。
6. **幂等是前提**（已满足）：sync-vndb / reference-ping / image-gc 当前都按重跑安全设计，是任何"可重试/可手动重跑"方案的基础，勿在后续改动中破坏。

---

## 5. 追加考量 A：kungal/moyu 的"重置类" job（配额/签到）

> 用户提出：kungal/moyu 那边有"重置上传配额、图片配额、签到状态"等 cron，是否在 OAuth 这边统一跑？

核查代码后，**这个问题的前提需要先纠偏**：这类"每天清零"的 job **大多根本不该是 cron**。真正要决策的不是"在哪跑 cron"，而是"用时间窗/日期派生状态把 cron 设计掉"。

### 证据 1：图片配额已经是"无 cron 自重置"的正确范本

`internal/platform/image/quota/quota.go`：配额是 Redis 窗口键
`image:quota:count:<site>:<UTC日>` / `:bytes:`，`keyTTL = 26h` 跨日自动过期，
`ResetAt = nextDay(now)`。**零 cron，按 key 设计天然每日自重置。**
→ kungal/moyu 若还有"重置图片配额"的 cron，是**冗余/错误**，应删除，而不是搬到 OAuth 继续跑。

### 证据 2：`DailyCheckIn` / `DailyImageCount` 是旧单体 reset-cron 的遗留

`auth/model.UserSiteData` 有 `DailyCheckIn int default 0`、`DailyImageCount int default 0`，但 OAuth 侧 handler/service **零引用**——现只出现在 model 定义（其唯一历史写入方 `cmd/migrate-users` 已随 permission-first 卫生波退役，见 git history）。这俩 int 计数列正是旧单体"夜里 cron 清零"那套模式的产物，属下一批可清理的死列。

### 正确设计（把 cron 设计掉）

| 旧（reset cron） | 新（无 cron） |
|---|---|
| 上传/图片配额 `DailyImageCount` 每夜清零 | 窗口计数器（image_service 已是范本：`<key>:<UTC日>` + TTL），自重置 |
| 签到 `DailyCheckIn` 每夜清零 | 存 `last_check_in_date`(DATE) + streak；按"今天日期 vs last_check_in_date"派生"今天签没签"，无需清零 |

### 服务边界（硬约束）

OAuth 只拥有它该拥有的：身份、image 配额（已做对）。**OAuth 绝不能跑 cron 去写 kungal/moyu 的本地库**——跨服务边界违例、强耦合、故障放大。可以"统一"的是**模式/框架**（窗口计数器、date-derived 状态的写法），不是跨库写操作。

**结论**：迁移到 OAuth 中心化是**删掉这些 reset cron** 的契机，不是把它们集中起来继续跑。这进一步缩小了"真·周期"集合（仍是 §1 那 3 个日任务），也进一步反对"为它们专门建独立项目（方案 A）"。

---

## 6. 追加考量 B：Docker 部署对三方案的影响

Docker 下"进程 = 容器"，宿主 crontab 脆弱（容器易失、扩缩容、镜像重建丢宿主 cron）。把这层叠加到三方案：

| 方案 | Docker 形态 | 评价 |
|---|---|---|
| A 独立 cron 项目 | 额外镜像 + 额外常驻容器（或自带一堆 k8s CronJob） | 容器最多、运维最重 |
| C 外部调度 | 专设 cron-sidecar 容器 / k8s CronJob（每 job 一份 manifest）/ 宿主 cron 耦合 | 每 job 一个调度产物，调度表仍在代码库外 |
| **B admin 内调度器** | 调度器寄生在**本就必须存在的长生命周期容器**里 | **零额外容器、不耦合宿主 cron、调度表随镜像出厂（不可变、与代码同版本）** |

- B 的唯一 Docker 注意点：水平扩 admin 容器 → 多调度实例 → **必须** PG advisory lock 单飞（§4 已列为硬约束）。Docker/k8s 扩缩容正是这条非可选的根本原因。
- Docker 时代一个值得点名的替代：**k8s `CronJob` 跑现有 `cmd/*` 薄壳** —— 这其实是"外部调度"的云原生形态，有 per-job 隔离 + 原生重试/历史（`kubectl get jobs`），但失去 in-app admin 触发/统一 UI，且**要求 k8s**（纯 docker-compose 没有此原语）。

**部署形态已确认 = 纯 docker-compose**（无 k8s）：没有 k8s CronJob 原语，外部调度只能靠脆弱的宿主 cron 或 sidecar 容器（调度表仍在镜像外）→ **B 是唯一干净解，且不做 B+CronJob 混合**。上面"k8s 替代"一段仅作存档，本项目不适用。

---

## 7. 推荐（最终由你拍板）

综合两项追加考量，推荐分两步：

1. **先把能设计掉的 reset cron 设计掉**（窗口计数器 / date-derived 状态），不要把它们"统一到 OAuth 继续当 cron 跑"。这一步与方案选择无关，是纯收益，且让真·周期集合稳定在 ~3 个日任务。OAuth 不跨写 kungal/moyu 本地库；能共享的是 job 框架范式。
2. **剩余真·周期任务用方案 B**：核心集小而稳定，项目已有 admin 鉴权/`/admin/*`/UI/共享 `internal/`，B 复用这些直接拿到"默认自动 + admin 可触发 + 统一可见"；叠加 Docker 后，纯 docker-compose 下 B 近乎唯一干净解，k8s 下可 B 或 B+CronJob 混合。

推荐的落地骨架（实施方案另文细化）：

- `internal/jobs/<name>`：每个 job 一个 `Run(ctx) (Summary, error)`；现有 `cmd/sync-vndb` 等 `main()` 主体下沉至此。
- `cmd/*` 保留为 3 行薄壳（运维/break-glass/k8s CronJob 直接 CLI 跑，**单一真相源、零重复**）。
- 进程内轻量调度器：注册各 job 的 cron 表达式，默认自动跑。
- `job_run` 表 + Postgres advisory lock：历史可见性 + 多副本/多容器单飞。
- `GET /admin/jobs`、`POST /admin/jobs/:name/run`、`GET /admin/jobs/:name/runs` + apps/admin 一个面板。

**反对 A**：monorepo 下为小任务集拆独立 repo = 代码强耦合/重复 + 多一套运维，Docker 下容器更多，过度工程。
**反对停在 C**：根因是"调度在代码库外、不可见、不可手动"，C 不解决；Docker 下还要么耦合宿主 cron、要么 sidecar/CronJob 一 job 一产物。

**决策记录**：部署形态已确认 = **纯 docker-compose**（无 k8s）。因此：
- 不存在 k8s CronJob 原语，外部调度只剩"脆弱宿主 cron / sidecar 容器"两条劣解；
- **方案 B 为最终方向，不做 B+CronJob 混合**（无 k8s 可混）；
- §6 表格中 A/C 在纯 compose 下劣势进一步放大（A 多镜像多容器；C 只能宿主 cron 或 sidecar，调度表仍在镜像外）。

**唯一剩余开放项**：是否认可"先删 reset cron、改窗口/日期派生"这步（强烈建议认可，纯收益、与方案选择无关）。

> 认可后我写《实施方案》（含 `job_run` schema、调度器选型——轻量 cron 库 vs 手写 ticker、advisory lock 键设计、reset 字段的窗口化迁移、compose 下单容器内调度器的形态、迁移顺序）。

---

## 8. 关联文档

- 触发本讨论的具体 job：`docs/image_service/06-integration-guide.md` §七（`galgame-image-refping`）
- VNDB 同步现状：`docs/sync/vndb/README.md`
- image TTL 生命周期（为何 reference-ping 必须周期跑）：`docs/image_service/04-migration-plan.md`、`internal/platform/image/service/gc.go`
- 配额"无 cron 自重置"范本：`internal/platform/image/quota/quota.go`
- reset-cron 遗留字段：`internal/platform/auth/model/user_site_data.go`（`DailyCheckIn`/`DailyImageCount`，OAuth 侧零引用）
