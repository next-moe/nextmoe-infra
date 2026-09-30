# 01 · 线上契约

端点：`https://telemetry.nextmoe.dev`。客户端在它后面拼 `/v1/logs`、`/v1/symbols`，不带路径前缀。

## 1. 上报：`POST /v1/logs`

### 1.1 请求

- 标准 OTLP/HTTP，JSON 编码，即 opentelemetry-proto `ExportLogsServiceRequest` 的 JSON 映射：64 位整数为十进制字符串（也接受数字），枚举为整数（也接受枚举名），键为 lowerCamelCase。未知字段忽略。
- 请求头：
  - `Content-Type: application/json`（可带 `charset` 参数），其他类型回 415。
  - `Content-Encoding: gzip`；不带或 `identity` 也接受，其他编码回 415。
  - `Content-Length`：客户端应当带上；分块传输也接受。
  - `x-telemetry-key: <接入 key>`：每个 App 一个，打进安装包，**不是秘密**，只用于识别 App 与限流，不做任何授权。
- 大小：压缩后与解压后都以 2 MiB 为上限，超出回 413。客户端保证单批不超过 200 条记录、约 512 KB 未压缩 JSON。

### 1.2 资源属性（每个 `resourceLogs` 一份）

| 属性 | 说明 |
|---|---|
| `service.name` | 必须等于接入 key 所属 App 的服务名（如 `kungal-app`）；不等时该组全部记录被拒收，计入 `partialSuccess` |
| `service.version` | `<版本>+<构建号>`，与符号上传一致 |
| `deployment.environment.name` | `direct` 为正式渠道；`dev`、`check` 为本机。指标与告警只看 `direct` |
| `host.arch`、`os.name`、`os.version`、`android.os.api_level`、`device.model.identifier`、`device.manufacturer`、`telemetry.sdk.version` | 按原样入库，用于分布统计 |

**一个请求可以有多个 `resourceLogs`，每组的 `service.version` 可以不同。** 每条记录按它所在那一组的版本记账。组的版本是记录「所属进程」的版本：

- 本进程产生的记录用当前版本；
- 下一次启动补发的 `app.crash`、`app.exit` 用崩溃或被杀的那个进程的版本；
- 上个会话的 `session.end` 用该会话开始时的版本；
- 旧进程排队未发的记录各自带着入队时的版本；
- 进程在会话开始前就死了、没留下版本时，记到发送进程的版本。

资源的其余属性（如 `os.version`）是发送进程的。系统升级后补发的旧崩溃因此计入新系统版本，量很小，接受。

### 1.3 记录

- `eventName` 必填（OTLP 1.5 字段），为空的记录被拒收。未知事件名照常存储。
- `session.id`：32 位小写十六进制，每会话随机一个；格式不符时视为无会话，记录照常存储。
- `log.record.uid`：32 位小写十六进制，每条记录入队时生成一次，重发不变。**服务端按它去重**：同一 uid 只存一次。缺失或格式不符时服务端补一个随机值，这类记录不去重。
- 时间：取 `timeUnixNano`，为 0 时取 `observedTimeUnixNano`，都没有时取接收时间（截到分钟）。事件所属日期（UTC）：

| 事件时间 | 记账日期 | 处理 |
|---|---|---|
| 早于 2026-01-01（时钟被重置） | 接收日 | 保留 |
| 晚于接收时间 + 24 小时（未来） | 接收日 | 保留 |
| 早于接收时间 − 30 天（超出保留期） | — | 丢弃，计入 `partialSuccess`，仍回 2xx |
| 其余 | 事件时间的日期 | 保留 |

- 数值属性（`app.startup.ttid_ms`、`app.jank.frame_count`、`app.jank.frames`、`app.session.errors`、`android.exit.reason`、`http.response.status_code`）只接受 0 到 2147483647 的整数，`app.jank.threshold` 只接受非负有限数；不合规的值从记录里删去，记录本身保留。
- 所有字符串中的 NUL 字符被删去。`session.previous_id` 不入库。

事件与关键属性见客户端需求单；服务端依赖的是：`session.start`、`session.end`（`app.session.status`、`app.session.errors`）、`exception`（`exception.*`、`app.exception.handled`、`app.breadcrumbs`、`app.fault`、`http.request.method`、`url.path`、`http.response.status_code`）、`app.crash`（`app.exit.kind`、`exception.*`）、`app.startup`（`app.startup.ttid_ms`、`app.startup.type`）、`app.jank`（`app.jank.frame_count`、`app.jank.frames`）。

### 1.4 响应与重试

| 情形 | 状态码 | `Retry-After` |
|---|---|---|
| 成功 | 200，体为 `{}`；有记录被拒时为 `{"partialSuccess":{"rejectedLogRecords":"<n>","errorMessage":"…"}}` | — |
| 缺少或未知的接入 key | 401 | — |
| App 已停用 | 403 | — |
| 同一 App 同一来源 IP 超出限流（每秒 1 次、突发 60） | 429 | ≥ 5 秒 |
| App 总量超出限流（每秒 100 次、突发 1000） | 503 | 30 |
| 服务端写入繁忙、数据库故障、key 缓存尚未就绪 | 503 | 30 |
| 类型或编码不对 | 415 | — |
| 超过 2 MiB | 413 | — |
| gzip 损坏、JSON 无法解析、数据本身无法存储 | 400 | — |

客户端只重试 429、502、503、504（遵守 `Retry-After`，否则指数退避加全抖动），其余状态一律丢弃该批。服务端保证：

- 自身的临时故障一律回 503，不回 500；
- **重新部署期间不回 404**：容器重建的空窗由 Traefik 兜底路由回 503；
- 同一批重发不会重复计数（按 `log.record.uid` 去重）。

### 1.5 隐私约束（服务端承诺）

- 不存 IP，日志里也不记。限流在内存中按 `HMAC(进程随机密钥, App, IP)` 分桶，密钥不落盘。
- 入库时不保留「同批」关联：没有批次号、请求号；没有精确到日以下的接收时间；记录主键是客户端随机的 uid；会话表与崩溃队列没有任何写入时间戳。
- 原始事件、会话、崩溃明细保留 30 天，且不进入数据库备份；按天汇总的指标与问题统计保留 13 个月。

## 2. 符号上传：`POST /v1/symbols`

发版流水线在构建后调用，令牌设在 CI 的 `TELEMETRY_SYMBOLS_TOKEN`。

### 2.1 请求

- `Authorization: Bearer <64 位小写十六进制令牌>`。令牌由管理台「应用与密钥」生成，只显示一次；服务端只存它的 SHA-256。令牌在读取请求体之前校验。
- `multipart/form-data`，字段：
  - `service.name`：必须等于令牌所属 App；
  - `service.version`：`^[0-9A-Za-z][0-9A-Za-z.+_-]{0,127}$`，与上报的版本一致；
  - `flutter.engine_revision`（可选）：`flutter --version --machine` 的 `engineRevision`，40 位十六进制；
  - 若干 `file` 部分，文件名只能是下表之一，同一请求内不重复。

| 文件名 | 校验 | 用途 |
|---|---|---|
| `app.android-arm64.symbols` / `app.android-arm.symbols` / `app.android-x64.symbols` | ELF，机器类型与架构一致，带 GNU build-id | Dart 栈与 `libapp.so` 帧，按 build id 索引 |
| `obfuscation.map.json` | JSON 字符串数组，长度为偶数 | 还原混淆的类型名。格式是 `[原名, 混淆名, 原名, 混淆名, …]` |
| `mapping.txt` | 至少一行 `原名 -> 混淆名:` | R8 retrace，按版本取最新一份 |

- 单个文件不超过 64 MiB，整个请求不超过 100 MiB（Cloudflare 的上限也是 100 MB）。

### 2.2 响应

| 情形 | 状态码 |
|---|---|
| 成功（新上传或已存在） | 200，`{"upload_id":…,"created":true/false,"files":[…]}` |
| 令牌缺失、格式不对或未知 | 401 |
| App 已停用 | 403 |
| 不是 multipart | 415 |
| 字段或文件名不合规、服务名不符、文件校验失败 | 400，`message` 指出哪个文件、什么原因 |
| 超过大小上限 | 413 |
| 同时上传过多、存储或数据库故障 | 503，`Retry-After: 60` |

- 幂等：同一 `(App, 版本)` 下每个 `(文件名, 内容哈希)` 都已存在、且引擎版本已记录时，回 200 且 `created: false`，不写任何东西。
- 同一版本重新构建、内容不同，会成为一次新的上传，旧的保留：Dart 崩溃按 build id 找到它自己那次构建的符号。
- 带 `flutter.engine_revision` 时，服务端自行从 `https://storage.googleapis.com/flutter_infra_release/flutter/<revision>/<变体>/symbols.zip` 拉取 `android-arm64-release`、`android-arm-release` 两个变体的 `libflutter.so`，按其 BuildId 索引。debug 变体不拉。
- 符号文件在对应版本最后一次出现 365 天后删除。
