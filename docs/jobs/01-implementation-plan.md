# 定时/可触发任务 —— 实施方案（方案 B，纯 docker-compose）

> 前置决策见 [README.md](./README.md)：方案 B（admin 服务内 job registry），
> 部署纯 docker-compose，且**先把能设计掉的 reset cron 设计掉**（已认可）。
> 本文锁定实现细节；落地代码以本文为准。

---

## 0. 范围与边界（先划清，防 scope creep）

### 本仓要写的代码（nextmoe-infra）

1. `internal/jobs`：Job 抽象 + 注册表 + 极简调度器 + advisory-lock 单飞 + panic 隔离 + `job_run` 落库。
2. `JobRun` model + 进 `cmd/migrate` 的 AutoMigrate（OAuth 核心库）。
3. 把 3 个真·周期 job 主体下沉 `internal/jobs/*`，`cmd/*` 退化薄壳。
4. 调度器在 `cmd/oauth`（admin API 服务）内启动；挂 `/admin/jobs*` 端点。

### 明确**不**在本仓做（边界）

- **不**改 kungal/moyu 仓库。它们的"重置签到/配额 cron"是下游自己的事。
- **不**新增任何"重置类" cron。OAuth 侧图片配额已是无 cron 自重置范本
  （`internal/platform/image/quota/quota.go`，窗口键 + 26h TTL）。
- `UserSiteData.DailyCheckIn` / `DailyImageCount` 是迁移自旧库的死列
  （OAuth 侧零引用）——本期**不动它**（动它要连带下游，另开任务）。
  本文 §7 给出下游应遵循的窗口化/日期派生**契约**，作为文档交付物，
  不作为本仓代码。
- 一次性 migrate-*/seed/setup、常驻 worker、HTTP 服务**不进**注册表。

### 进注册表的 job（全集，就这 3 个）

| job name | 来源 cmd | 默认调度 | 目标库 | 幂等 |
|---|---|---|---|---|
| `sync-vndb` | `cmd/sync-vndb` | 每日 03:00（增量） | GalgameDatabase | 是 |
| `galgame-image-refping` | `cmd/galgame-image-refping` | 每日 04:00 | GalgameDatabase | 是 |
| `image-gc` | `cmd/image-gc` | 每日 03:30 | ImagesDatabase | 是 |

`reindex-search` / `sync-vndb-relations` 暂**只**做成"可 admin 手动触发、
默认不自动调度"（注册但 schedule 为空）。理由见 README §1：按需/偶发。

---

## 1. 组件设计

### 1.1 Job 抽象（`internal/jobs/job.go`）

```go
type Summary map[string]any   // 落 job_run.summary (jsonb)

type RunFunc func(ctx context.Context, cfg *config.Config) (Summary, error)

type Job struct {
    Name     string    // 唯一键，= cmd 名
    Schedule Schedule  // 零值 = 不自动调度（仅手动）
    Run      RunFunc
}
```

- Job 不持有长连接：`Run` 内部按需自建 DB/client（与现有 `cmd/*` 行为一致），
  避免 oauth 进程长期占着 wiki/images 库连接。
- `RunFunc` 接 `cfg`，因为 3 个 job 连的是**不同**库（Galgame/Images），
  靠 cfg 自行构造。

### 1.2 Schedule（`internal/jobs/schedule.go`）—— 极简，无新依赖

只支持两种，足够覆盖现集合，**不引入 cron 库**（纯 compose 单进程，
3 个日任务，cron 表达式解析的复杂度不值得引依赖）：

```go
type Schedule struct {
    DailyAt  string        // "03:00"（本地时区），优先
    Every    time.Duration // 退化用：固定间隔；DailyAt 非空时忽略
}
func (s Schedule) zero() bool          // 都没设 → 仅手动
func (s Schedule) next(now time.Time) time.Time
```

- `DailyAt`：算出今天/明天该时刻；`next` 永远返回未来时刻。
- 时区：用进程本地时区（容器内设 `TZ`，compose 里显式配，文档 §6 提醒）。
- 不支持 cron 五段式——**有意为之**。将来真需要再引 `robfig/cron/v3`。

### 1.3 Registry（`internal/jobs/registry.go`）

```go
type Registry struct { /* name → Job, 有序 */ }
func NewRegistry() *Registry
func (r *Registry) Register(j Job)
func (r *Registry) Get(name string) (Job, bool)
func (r *Registry) List() []Job
```

进程启动时在一处集中 `Register`（`internal/jobs/all.go`），便于审计"有哪些 job"。

### 1.4 Runner（`internal/jobs/runner.go`）—— 执行一次的全部横切

`Run(ctx, job, trigger)` 串起：

1. **单飞**：OAuth 核心库 `SELECT pg_try_advisory_lock(key)`，
   `key = hashJobName(job.Name)`（int64，稳定哈希）。拿不到 → 记一条
   `skipped`(reason=locked) 的 job_run，直接返回（不报错）。
   多容器/多副本天然安全。`defer pg_advisory_unlock(key)`。
2. **建 job_run 行**：status=`running`, trigger, started_at。
3. **panic 隔离**：`defer recover()` → 标记 status=`failed`, error=panic 文本，
   不向上 panic（绝不带崩 oauth 进程）。
4. 调 `job.Run(ctx, cfg)`。
5. 收尾：成功 → status=`success`, summary(jsonb), finished_at；
   失败 → status=`failed`, error。
6. 全程结构化日志（job、trigger、run_id、耗时、结果）。

advisory lock 用**同一把连接**持有期：用 `db.Raw(...).Scan` 在一个
`*sql.Conn` / 事务上 lock+unlock，避免连接池换连接导致 unlock 失效
（实现细节：取一个 `sqlDB.Conn(ctx)`，lock/unlock/run 都在它上面跑；
job 自己的业务连接是另开的，互不影响）。

### 1.5 Scheduler（`internal/jobs/scheduler.go`）

```go
func StartScheduler(ctx context.Context, reg *Registry, cfg, runnerDeps)
```

- 每个有 schedule 的 job 一个 goroutine：`timer := time.NewTimer(next-now)`；
  到点 → `Runner.Run(ctx, job, "schedule")`（**串行**：上一次没跑完不叠下一次，
  靠 advisory lock + 计算 next 时以"现在"为基准）；`ctx.Done()` 退出。
- 镜像出厂即带调度表（schedule 写在 `all.go`，进 git，可 review/回滚）。
- 对齐现有 `app.StartCleanup` 范式（`internal/app/cleanup.go`），
  在 `cmd/oauth` 的 `setupRoutes` 里 `app.StartScheduler(cleanupCtx, ...)` 启动。

### 1.6 长任务异步（admin 手动触发）

`POST /admin/jobs/:name/run`：
- 校验 name 存在；
- `go runner.Run(context.Background()/*detached*/, job, "admin")`（**后台**，
  因为 sync-vndb 全量 15–20min，绝不阻塞 HTTP）；
- 立即 `202` 返回 `{ run_id }`（advisory lock 已保证不会和调度撞车——
  撞了就是一条 skipped:locked）。
- 进度/结果走 `GET /admin/jobs/:name/runs` 轮询。

---

## 2. `job_run` 表

OAuth 核心库（`cfg.Database`），AutoMigrate 经 `cmd/migrate`。

```go
type JobRun struct {
    ID         uint           `gorm:"primaryKey"`
    JobName    string         `gorm:"size:64;not null;index:idx_job_run_name_id,priority:1"`
    Trigger    string         `gorm:"size:16;not null"`  // schedule | admin
    Status     string         `gorm:"size:16;not null"`  // running | success | failed | skipped
    Summary    datatypes.JSON `gorm:"type:jsonb"`         // 成功时的 Summary
    Error      string         `gorm:"type:text;default:''"`
    StartedAt  time.Time      `gorm:"not null;index:idx_job_run_name_id,priority:2,sort:desc"`
    FinishedAt *time.Time
    CreatedAt  time.Time
}
func (JobRun) TableName() string { return "job_run" }
```

- 复合索引 `(job_name, started_at desc)`：列表/最近一次查询。
- `skipped`(reason=locked) 也落行——可见"被并发挡过"。reason 放 summary。
- 保留策略：本期不做自动清理；将来量大可加一个……进注册表的 job（自洽）。

---

## 3. Admin 端点（挂在 `cmd/oauth` 现有 `admin` 组）

现有：`admin := v1.Group("/admin", middleware.Auth(authSvc), middleware.RequireRole("admin"))`。新增：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/admin/jobs` | 列出注册表所有 job + 各自最近一次 run 摘要 |
| POST | `/api/v1/admin/jobs/:name/run` | 手动触发（后台），返回 `{run_id}` |
| GET | `/api/v1/admin/jobs/:name/runs?limit=20` | 该 job 运行历史（分页，按 started_at desc） |

- 统一走 `pkg/response` 扁平信封（与全仓一致）。
- 复用现有 admin 鉴权中间件，无需新角色。
- apps/admin：加一个"任务"页（`/jobs`）——列表 + 触发按钮 + 历史抽屉。
  用现有 KunUI（KunButton/KunBadge/KunModal/KunPagination）。
  **前端这一页作为最后一步，后端可独立验收。**

---

## 4. cmd 重构（最高风险，零行为变更目标）

原则：**单一真相源、cmd 行为不退化**。

每个 job：

- 新增 `internal/jobs/<name>.go`，导出 `func Run<Name>(ctx, cfg, opts <Name>Opts) (jobs.Summary, error)`，
  把现 `cmd/<name>/main.go` 的**主体逻辑原样搬入**（不改算法/SQL/批次）。
- `<Name>Opts` 承载原 flag（如 `sync-vndb` 的 `Full`、`image-gc` 的
  `SoftDays` 等），**默认值 = 原 flag 默认值**。
- `cmd/<name>/main.go` 退化为：`flag` 解析 → `config.Load` → `logger.Init`
  → 组装 opts → 调 `Run<Name>` → 打印 summary → 错误 `os.Exit(1)`。
  CLI / break-glass / 排障仍可直接 `go run ./cmd/<name> --flag`。
- 注册表里用**默认 opts** 包一层 `RunFunc`。

逐个做、逐个 `go build ./cmd/<name>` + 跑一次 `--dry-run`（refping/有 dry-run 的）
确认与重构前一致，再做下一个。顺序：`galgame-image-refping`（最小、刚写、
有 dry-run，先拿它练手）→ `image-gc`（无外部副作用 dry-run 友好）→
`sync-vndb`（最大，最后）。

---

## 5. 迁移/上线顺序

1. `JobRun` model + 注册进 `cmd/migrate` → 跑 `go run ./cmd/migrate`（OAuth 核心库 AutoMigrate 出 `job_run`）。
2. `internal/jobs` 骨架（job/schedule/registry/runner/scheduler）+ 单测。
3. 重构 3 个 cmd（§4 顺序），每步验证。
4. `all.go` 注册 3 个 + 调度表；`cmd/oauth` 启动 scheduler。
5. admin 端点 + 验收（curl）。
6. apps/admin 任务页。
7. docker-compose：确认 oauth 容器设 `TZ`，文档化（调度按本地时区）。

每步可独立 build/验收；任何一步失败不影响已上线部分（scheduler 未启用前纯新增）。

---

## 6. docker-compose 注意

- oauth 容器显式设 `TZ`（如 `Asia/Shanghai`）——`DailyAt` 按进程本地时区。
  不设则 UTC，03:00 会偏。compose service 加 `environment: [TZ=Asia/Shanghai]`
  或挂 `/etc/localtime`。
- oauth 容器若 `deploy.replicas > 1`：advisory lock 已保证全局单飞，安全；
  但 N 个副本都会各自起 scheduler goroutine、到点都尝试，靠 lock 去重
  （多打一次 `pg_try_advisory_lock` 的开销可忽略）。
- 无需任何宿主 cron / sidecar / 额外容器——这正是选 B 的理由。

---

## 7. 下游窗口化契约（文档交付物，非本仓代码）

给 kungal/moyu：以下"每日重置"应**删 cron、改设计**，OAuth 不代跑、不跨库写：

| 旧（reset cron） | 新（无 cron） | 参考实现 |
|---|---|---|
| 图片/上传日配额夜里清零 | 窗口计数器 `<biz>:quota:<scope>:<UTC日>` + ~26h TTL | `apps/api/internal/platform/image/quota/quota.go`（OAuth 侧已是范本，可直接照搬键设计） |
| 签到状态 `daily_check_in` 夜里清零 | 存 `last_check_in_date DATE` + `streak`；"今天签没签" = `last_check_in_date = CURRENT_DATE`；连签 = 日期差判定 | 无需 job |

OAuth 侧 `UserSiteData.DailyCheckIn/DailyImageCount` 死列：留待"下游签到
改造"统一收口时一起清，不在本期。

---

## 8. 验收清单

- [ ] `go run ./cmd/migrate` 后 OAuth 库有 `job_run`（列/索引）
- [ ] `go build ./...` 全绿；3 个 `cmd/*` 仍可独立 `--flag` 跑，输出与重构前一致
- [ ] `internal/jobs` 单测：schedule.next 边界、registry、runner（lock 命中/未命中、panic 隔离、summary/error 落库）
- [ ] `cmd/oauth` 启动日志显示 scheduler 注册了 3 个 job + 各自 next 时刻
- [ ] `POST /admin/jobs/galgame-image-refping/run` → 202 + run_id；`GET .../runs` 见结果
- [ ] 并发触发同 job → 第二条是 `skipped:locked`，不双跑
- [ ] kill -9 oauth 重启后 `running` 悬挂行有标注策略（启动时把超期 `running` 标 `failed:stale`，写进 runner 启动自检）
- [ ] apps/admin 任务页可列/触发/看历史

---

## 9. 关联

- 决策与权衡：[README.md](./README.md)
- 触发本线的 job：`docs/image_service/06-integration-guide.md` §七
- 配额无-cron 范本：`internal/platform/image/quota/quota.go`
