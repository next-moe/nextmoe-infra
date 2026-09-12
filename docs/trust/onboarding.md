# NextMoe 审核平台 · 站点接入指南(onboarding)

> 读者:forum / moyu / letmoe 等下游仓的集成者。目标:读完本文即可独立把自己站的 UGC 接进统一审核,不需要再来问 infra。
> 真相源:本文描述的一切以 `cmd/trust`、`cmd/ai`、`internal/platform/{trust,ai,community}` 代码为准;机读契约 = `docs/trust/openapi.yaml`(S2S 面)与 `docs/trust/admin-openapi.yaml`(管理面)。
> 状态基线:2026-07-16。

---

## 1. 全景:平台长什么样、哪些已经在产

审核平台 = **trust 服务(:9283,kun_trust 库)+ AI 网关(cmd/ai,:9284,kun_ai 库)+ 统一审核收件箱(apps/admin 管理端)**。设计正典是 doc 14/18/20(nextmoe-draft)。核心哲学一句话:**AI 与词表是建议者不是闸门;同步路径只跑确定性规则且 fail-open;LLM 永不阻塞发布**。

```
用户写内容(你的站)
   │
   ├─(发布前,可选)──► POST /trust/check      同步词表闸:banned 直拒 / suspect 放行入队 / <10ms / fail-open
   │
   ├─(发布后,异步)──► POST /trust/scan       影子扫描:落 trust_scan_result(pending)
   │                        │
   │                        └─► scan worker(60s tick)──► Tier0 词表记录(tier0_matched)
   │                                                   └─► AI 网关 moderate-text 路由
   │                                                          ├─ Tier1: OpenAI omni-moderation(在产,免费)
   │                                                          └─ Tier2: LLM 精判(级联升级;渠道待接,零代码升维)
   │
   ├─(用户举报)────► POST /trust/reports      去重/限速/信誉加权 → 超阈建统一收件箱条目
   │
   └─(你站本地审核队列的信号)─► POST /trust/forward  → 统一收件箱(仅限中继型调用方,见 §3)
                                                       ↑
                                     人工:apps/admin 统一收件箱(领取/裁决/处置/执法回调)
```

**在产状态(2026-07-16)**:
- ✅ Tier0 词表档:46,434 条活跃全局 suspect 词(Sensitive-lexicon 除色情类目导入)+ Aho-Corasick 匹配器;**banned 恒为空是纪律**——每一条 banned 都要人工从影子数据里提升,不从外部词库直灌。词表管理 UI:管理端 `/trust/terms`。
- ✅ 影子扫描全链:community 原语的发帖/编辑已自动喂 scan,worker 经 AI 网关用 omni 打分,真实生产流量在流。
- ✅ 统一收件箱 + forward/resolve 闭环 + 执法回调(HMAC 签名,注册于 subject_kind.callback_url)。
- ✅ 举报面(reports)+ 注册表(subject kinds / report reasons)+ 管理端(队列/注册表/词表/AI 用量看板)。
- ⏳ 影子期:2026-07-16 起 ≥4 周。期间 scan 打分**零执法零入队**,纯攒校准数据;四周后拿分布定阈值,先开「超阈入队」,最后才谈「自动隐藏 top ≤1%」。
- ⏳ Tier2 LLM 渠道:owner 择定中;接入后 = 填 `KUN_AI_UPSTREAM_*` 三个 env,级联自动升维,**下游零感知零改动**。

## 2. 概念词汇表(读懂 API 的前提)

| 概念 | 含义 |
|---|---|
| **site(租户)** | 每个产品站一个字符串 key(`kungal` / `moyu` / `letmoe` …)。trust 所有数据按 site 分域。 |
| **S2S client** | `oauth_clients` 表的一行,HTTP Basic(client_id:secret)认证。**你的 site 从 client 的 `catalog_site` 绑定派生,普通调用方绝不在请求体里传 site**(传了会被拒)。 |
| **subject_kind 注册表** | `(site, kind)` 白名单,**fail-loud**:向未注册的 kind 提交 report/scan 会得 422。这是防打错站/打错类型的保险丝,不是权限系统。kind 举例:`forum_topic` / `forum_reply` / `community_post` / `galgame_comment` / `user`。 |
| **中继型调用方(forwarder)** | 唯一例外:像 community 服务这种"一个进程服务多个站"的中继,允许在 body 带 `site`,由 trust 侧 `KUN_TRUST_FORWARDER_CLIENT_IDS` allowlist 反制。**产品站直连不需要也不该申请这个**。 |
| **影子模式(shadow)** | scan 打分只落库不执法。表里 `mode=0` 写死;别指望 scan 会替你拦内容——拦截只发生在 check 面的 banned 词。 |
| **fail-open** | check/scan 的调用方纪律:trust 不可用/超时 → **放行 + 打 warn 日志**,发布可用性永远优先于拦截完备性。 |

## 3. 三个 S2S 面:契约与接法

所有端点:`http://trust:9283`(dokploy-network 内)/ Basic 认证 / 响应一律 house envelope `{code, message, data}`。

### 3.1 `POST /api/v1/trust/check` — 同步词表闸(发布前,可选但推荐)

```
Request:  { "text": "<用户原文 raw>", "author_id": 123 }        # author_id 可选;site 从凭证派生
Response: { "decision": "allow" | "deny" | "hold", "matched": ["<命中的归一化词>", ...] }
```

- 语义:`deny` = banned 词命中 → **你应拒绝这次写入**(建议 422 + 明确文案);`hold` = 仅 suspect 命中 → **正常发布**,同时把它送进你站的审核队列(community 原语的做法:发布 + 建 review item,即"入队不拦");`allow` = 干净。
- 无状态、不落库、p99 <10ms(内存 AC 自动机)。**不查注册表**(check 只针对文本,不针对 subject),所以接 check 不需要预注册 kind。
- **调用方纪律(硬性)**:①在你的 DB 事务**之前**调用,HTTP 永不进事务;②客户端超时 ≤500ms;③任何错误/超时按 `allow` 处理 + warn 日志。参考实现:infra 仓 `internal/platform/community/service/check.go`(逐行照抄即可)。
- 检查文本构成惯例:回帖/编辑 = raw 原文;带标题的首发内容 = `title + "\n\n" + raw`。
- 词表现实:全局词(site=NULL)对所有站生效,per-site 词只对该站。**banned 现为空**,所以接上 check 当下零拦截风险——它的价值随影子数据逐周长出来。

### 3.2 `POST /api/v1/trust/scan` — 异步影子扫描(发布/编辑后,推荐全量接)

```
Request:  { "subject_kind": "forum_topic", "subject_id": "8841", "text": "<raw>", "author_id": 123 }
Response: { "scan_id": 456, "truncated": false }
```

- **前置:`(你的 site, kind)` 必须已在注册表**(否则 422 fail-loud)。注册方式见 §5。
- 受理即返(accept-type):行落 `trust_scan_result(status=pending)`,worker 后台打分。文本超 8000 rune 会被截断并回报 `truncated:true`——你不需要预截。
- **刻意不去重**:每次编辑就是一次新 scan(重复受理是特性,worker 幂等)。
- **调用方纪律**:发布事务提交**之后**、off-request(goroutine / 队列)发送;纯 best-effort,失败打 warn 即可不必重试(漏一条无害,下次编辑自愈)。参考实现:`internal/platform/community/service/scan.go` + `ScanningSink`。
- 你会得到什么:影子期内什么都不会"发生"(零回调零入队),但管理端能看到你站内容的打分分布、词表命中(`tier0_matched`)——这是四周后给你站定策略的数据地基。

### 3.3 `POST /api/v1/trust/reports` — 用户举报(有举报 UI 就接)

```
Request:  { "subject_kind": "...", "subject_id": "...", "reason_key": "...", "note": "...",
            "snapshot": "<举报时点内容快照>", "subject_url": "https://...", "reporter_id": 123 }
Response: { "report_id": ..., "review_item_id": ... }        # review_item_id 非零 = 已聚合进收件箱
```

- 去重(同人同 subject 一票)/ 限速 / 举报人信誉加权(新号 ×0.5、staff 单票即入队)在 trust 侧,你只管转发。
- 可用 reason 列表:`GET /api/v1/trust/report-reasons`(全局基底 + 你站扩展);kind 列表:`GET /api/v1/trust/subject-kinds`。
- `snapshot` 请务必带(内容可能事后被编辑,收件箱审的是举报时点)。

### 3.4 `POST /api/v1/trust/forward` + `/forward/resolve` — 本地审核队列 → 统一收件箱

仅当你的站**自己有**本地审核队列且想汇入统一收件箱时才需要(community 原语已内建此链,原语上的站不用管)。需要进 forwarder allowlist,找 infra 开通。执法回调(裁决 → 你站执行删除/隐藏)经 `subject_kind.callback_url` + HMAC 签名投递,契约见 openapi.yaml 的 callback 部分。

## 4. 各站现状(接入前先看自己那行)

| 站 | 现状 | 差什么 |
|---|---|---|
| **kungal 评论区**(galgame/rating/website/toolset,community 原语) | ✅ 全链在产:check 闸 + scan 影子 + 举报 + forward 自动具备 | 无 |
| **kungal 主论坛**(topic/reply,forum 仓自有表) | ❌ 未接 | kinds `forum_topic`/`forum_reply` **已预注册**;forum 仓接 §3.1+3.2(接入波编排中) |
| **kungal 资源发布 / bio 等** | ❌ 未接 | 同上配方;新 kind 需注册 |
| **moyu** | ❌ 未接 | 全套:client + kinds 注册 + §3 三面 |
| **letmoe**(community 原语) | ⚠️ 半接:**check 闸已生效**(check 不查注册表,全局词全租户适用);scan 事件在发但受理面 422 丢弃 | 注册表加 `(letmoe, community_post)` 一行即完整(建议随 letmoe 上线 runbook 做) |

## 4.1 站点策略:尺度归你,不归平台

审核尺度**按站可调**,记在 `trust_site_policy`(管理端 `/trust/site-policies`)。每一项都可以留空,**留空 = 继承平台默认**——所以没建行的站行为和过去完全一致。

| 项 | 含义 | 建议新站起手 |
|---|---|---|
| `scan_mode` | `shadow` 只记录 / `live` 开审核单 | **shadow**,先看自己的分数分布 |
| `auto_hide_enabled` | live 时是否允许自动隐藏 | **关**——先只让人看见,别让分类器直接下架 |
| `sample_rate` | 清白判定的抽检比例(唯一能测漏判的手段) | 先 0,通电后再开 |
| `flag_threshold` | 本站认定"命中"的分数线;留空=采用网关自己的判定 | 留空 |
| `aggregate_threshold` | 多少举报权重开单 | 留空,按平台默认 |

**「留空」和「填成跟默认一样的值」不是一回事**:以后平台默认变了,留空的站跟着变,填死的站不变。所以只在你真的有主张时才填。

新站的正常路径是 **shadow 一段时间 → 看分布 → live 但 auto-hide 关 → 有把握再开自动隐藏**。跳过任何一步都等于拿真实用户当校准样本。

## 5. 接入 checklist(新站/新内容类型,照做即可)

1. **S2S client**:找 infra 在 `oauth_clients` 铸一行(id + sha256 secret + `catalog_site=<你的 site>`)。秘钥经 Dokploy 面板 env 注入你的服务,永不进 git。**你不需要 forwarder allowlist**(那是中继专用)。
2. **注册 subject kinds(声明式 ensure,首选)**:kind 清单本就是你站的属性——把它写成你仓里的一个常量数组,**在服务启动路径(或一次性脚本)里调一次 ensure**,trust 逐 kind 收敛即止,幂等可重复跑。kind 命名用内容类型的稳定蛇形名(`forum_topic`)。

   ```
   POST /api/v1/trust/subject-kinds/ensure          # Basic 认证;site 从凭证派生,只作用于自站
   Request:  { "kinds": [
                 { "key": "forum_topic" },
                 { "key": "forum_reply",
                   "callback_url": "http://forum:9200/internal/trust/callback",
                   "callback_secret": "<HMAC 密钥>",
                   "notify_on_dismiss": false }
             ] }                                     # ≤50 个;超限 422
   Response: { "results": [
                 { "key": "forum_topic", "result": "created" },
                 { "key": "forum_reply", "result": "unchanged" }
             ] }                                     # result ∈ created | updated | unchanged | deprecated_skipped
   ```

   - **收敛语义(逐 kind)**:不存在 → `created`;已存在且活跃 → 你提供的字段有变更则 `updated`、全同则 `unchanged`;**已弃用 → `deprecated_skipped`**(见 §6 红线)。以相同数组二次跑必然全 `unchanged`,所以放进启动路径完全安全。
   - **字段是稀疏的**:省略某个回调字段 = 不声明它,已有值保持不动(不会被清空)。要接执法回调就带上 `callback_url`(+ `callback_secret`);将来改配置 = 改数组重跑。
   - **建议 best-effort 调用**(失败打 warn 不阻塞启动)。这样"注册 kinds"从"找 infra 跑 SQL / 点 UI"变成"改你仓里的常量数组"。
   - **兜底:管理端批量添加**。没有 ensure 接线、或临时补一批时,管理端 `/trust` 注册表页「批量添加」= 一行一个 key + 共享回调/驳回字段,走同一套收敛逻辑(需显式选站)。单条注册 / 改配置 / 弃用仍在同页。
3. **接 check(可选)**:写路径事务前 + 500ms 超时 + fail-open;deny 拒绝、hold 发布并进你的本地审核视野。
4. **接 scan(推荐全量)**:发布/编辑成功后 off-request 发送;别进事务、别重试。
5. **接举报(有 UI 就接)**:reason 列表动态拉取,带 snapshot。
6. **env 纪律**:两个功能各自独立开关、默认关(参考 community 的 `KUN_TRUST_SCAN_ENABLED` / `KUN_TRUST_CHECK_ENABLED` 形态),compose 显式写 `"false"` 行方便通电 grep;**不要**借"client 配没配"当开关。
7. **验证**:开开关后发一条测试内容 → trust 库 `trust_scan_result` 应出现你的 site/kind 行并在 ~60s 内变 `status=1`(scored);check 面可 curl 冒烟(带一个已知 suspect 词应得 `hold`)。
8. **策略行**:通电前找 infra 在 `/trust/site-policies` 给你的站写一行 `scan_mode=shadow`(见 §4.1)。不写这行 = 直接继承平台默认姿态,对新站通常太狠。
9. **存量内容**:不要往 scan 面灌历史数据(worker 是为增量设计的)。存量走离线批扫工具(`cmd/scan-backlog`,infra 仓),产出高危 worklist 后按需入收件箱。

## 6. 红线(违反=打回)

1. **LLM/网关永不进同步路径**——你的发布路径只允许碰 check(确定性词表)。
2. **check/scan 双 fail-open**:trust 挂了,你的站必须照常发布。
3. **影子期零执法**:不要根据 scan 结果在你的站做任何自动处置;执法只走收件箱人工裁决 + 回调。
4. **site 不上线缆**(普通调用方);**secret 不进 git**;**注册表 fail-loud 不要绕**(422 说明你打错了站或忘了注册,不是让你重试)。
5. 文本发 **raw 原文**(不是渲染后 HTML)。
6. **ensure 不复活退役 kind**:声明式 ensure / 批量添加对 `is_deprecated=true` 的 kind 一律 `deprecated_skipped`——绝不复活、也不改它的配置。复活退役 kind 是管理员的明确决定(管理端单条「启用」),不是接入方数组能触发的副作用。

## 7. FAQ

- **check 和 scan 都接吗?** scan 建议全量(纯收益零风险);check 看内容类型——高滥用面(评论/回帖/资源说明)接,低频高信任面(如管理员公告)可不接。
- **会拖慢发帖吗?** check p99 <10ms + 500ms 超时兜底;scan 完全离线。community 原语在产实测无感。
- **suspect 词命中会怎样?** check 返 `hold`(你发布+入队);scan 侧记进 `tier0_matched`。都不拦人。
- **误杀了怎么办?** 词是数据不是代码:管理端 `/trust/terms` 把噪词 deprecate,60s 内全生态生效。
- **我能看到自己站的打分数据吗?** 现阶段经 infra 管理端;per-site 数据面板是触发式后续。
