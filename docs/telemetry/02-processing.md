# 02 · 处理、口径与告警

## 1. 会话

每个 `session.id` 一行（`telemetry_session`）：版本与环境取首次出现时的值；`started_at` 取 `session.start`；`session.end` 只认第一条（状态、错误数、结束时间）；收到 `app.exit.kind = anr` 的 `app.crash` 时标记 ANR。无论到达顺序如何，结果相同。会话按开始日期（UTC）记账，还没收到 `session.start` 时暂按最早事件的日期，收到后改正。

会话时长等于前台时长：客户端把「后台超时结束」的会话结束时间记为进入后台的时刻，前台死亡的记为死亡时间。

## 2. 按天指标（`telemetry_daily_metric`）

键为 App × 版本 × 环境 × 日期（UTC）。每 5 分钟重算最近 3 天，每小时重算最近 30 天，所以迟到几周的崩溃也会计入它所在的那一天。

| 字段 | 口径 |
|---|---|
| `sessions` | 收到了 `session.start` 的会话数（分母） |
| `crashed_sessions` / `unhandled_sessions` / `abnormal_sessions` | 上述会话中以 `crashed` / `unhandled` / `abnormal` 结束的个数 |
| `anr_sessions` | 上述会话中含 ANR 的个数 |
| `exceptions`、`crash_java`、`crash_native`、`crash_anr` | 事件数 |
| `ttid_p50_ms`、`ttid_p90_ms`、`startup_count` | `app.startup` 中 `app.startup.type` 为空或为 `cold` 的记录 |
| `jank_frames_over`、`jank_frames_total` | 两个计数的和；卡顿帧占比 = 前者 / 后者 |

注意：Google Play 的不良阈值按「日活用户」计算，这里没有设备 id，只能按会话算；同样的阈值在会话口径下偏松，所以告警另有「与上一版本相比变差」的规则。

## 3. 符号化（`telemetry-worker`）

每条 `exception`、`app.crash` 入库时进入 `telemetry_crash` 队列（与事件同一事务，按 `log.record.uid` 去重）。worker 逐条处理：

| 类型 | 还原方式 |
|---|---|
| Dart 异常（含 `contract`、`server`） | 栈头有 `build_id` 时按 build id 找 `.symbols`，用 `telemetry-decode`（package:native_stack_traces 0.7.0）还原；类型名与消息中引号内的名字用同一次上传的混淆映射还原（映射里私有名带 `@库哈希` 后缀，比对前去掉） |
| Java 崩溃、ANR | 按版本取 `mapping.txt`，用 R8 retrace 还原；ANR 取 `"main"` 线程的帧 |
| 原生崩溃 | `libapp.so` 帧按 BuildId 用 Dart 符号还原（把 pc 合成为 Dart 非符号化栈再解码）；`libflutter.so` 帧按 BuildId 用引擎符号经 llvm-symbolizer 还原；系统库帧保持原样 |

缺符号时记录为「等待符号」，并注明缺什么（`dart:<build id>`、`r8:<版本>`、`engine:<build id>`），先按未还原的帧归组；符号上传后每 5 分钟检查一次，自动重新还原、重新归组。工具失败 3 次记为「还原失败」，仍按原始帧归组。

Flutter 3.47.4 自带的 `flutter symbolize` 读不了 Dart 3.13 的栈，不要改用它。解码器要跟着构建使用的 Dart SDK 升级。

## 4. 指纹与问题

帧先规范化：Dart 路径 `…/<包>/lib/<路径>` 变成 `package:<包>/<路径>`（pub 缓存目录去掉版本号后缀），Java 帧取 `类.方法`，原生帧取模块名与函数名。**指纹不含行号、列号和地址**，同一问题在代码改动、重新构建后指纹不变。

「本 App 帧」由每个 App 的 `in_app_prefixes` 决定（管理台可改，如 `package:kungal/`、`package:nextmoe_`、`dev.nextmoe.`）。取前 5 个本 App 帧；没有就取前 5 个非系统帧；再没有就取前 5 帧。

| 类型 | 指纹组成 |
|---|---|
| `exception` | 类型名 + 帧 |
| `contract`（契约不一致） | 类型名 + 消息 + 帧（消息只写期望，不含具体值） |
| `server`（5xx） | 方法 + 路径 + 状态码 |
| `java` | 异常类 + 帧 |
| `anr` | 主线程帧 |
| `native` | 信号 + 帧 |

同指纹归为一个问题（`telemetry_issue`），每天的事件数与会话数从崩溃明细重新计数，不做累加。已解决的问题再次出现会重新打开并标记「回归」；已忽略的保持忽略。

## 5. 告警

每 5 分钟评估一次；同一规则同一对象只告警一次（去重键里带日期或小时的规则按周期重复）。收件人存在数据库里，在管理台「告警」页维护；目前支持邮件。指标规则只看 `direct` 环境，已停用的 App 不评估。

| 规则 | 时效 | 触发条件（阈值可在管理台按 App 调整，括号内为默认值） |
|---|---|---|
| 契约不一致 | 立即 | 出现新的 `contract` 问题 |
| 崩溃率超标 | 立即 | 某版本最近两天会话数 ≥ 最小样本（200），崩溃率 > 1.09% |
| ANR 率超标 | 立即 | 同上，ANR 率 > 0.47% |
| 崩溃率较上一版本变差 | 立即 | 两个版本都达到最小样本，且新版本崩溃率 > max(上一版本 × 2, 上一版本 + 0.5 个百分点) |
| 符号缺失 | 立即 | 正式渠道某版本有 ≥ 5 条「等待符号」且已超过一天（发版流水线没传符号） |
| App 无上报 | 立即 | 过去 7 天日均会话 ≥ 50，但最近 6 小时没有任何事件（与自身基线比，DNS 拦截等稳定缺失不会触发） |
| 引擎符号拉取失败 | 立即 | 某个引擎变体拉取失败 |
| 新问题、问题回归 | 15 分钟汇总 | 出现新的异常或崩溃问题；已解决的问题再次出现 |
| 服务端 5xx 过多 | 15 分钟汇总 | 最近一小时 `app.fault = server` 超过 50 条，附出现最多的 5 个接口 |

邮件中所有来自客户端的内容都做 HTML 转义。没有启用的收件人时告警记为「未发送」；发送失败最多重试 5 次。

## 6. 保留与备份

| 数据 | 保留 | 备份 |
|---|---|---|
| 原始事件（按天分区，整区删除）、会话、崩溃明细 | 30 天 | 只备份表结构，不备份数据 |
| 按天指标、问题与每日计数 | 13 个月 | 完整备份 |
| 告警记录 | 400 天 | 完整备份 |
| App、密钥哈希、收件人、符号元数据 | 长期 | 完整备份 |
| 符号文件（对象存储） | 版本最后出现后 365 天 | 不在数据库备份内 |
