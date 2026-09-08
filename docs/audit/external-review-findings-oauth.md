# 外部审计发现 · nextmoe-infra(已逐条对代码核实)

> 来源:codex(`kungal-docs/gpt/`)+ claude(`kungal-docs/claude/`)对三仓的双重审计,合并裁决见 `kungal-docs/gpt/04-claude-comparison-final.md`。
>
> 本文件**只收录与本仓(nextmoe-infra)相关**的发现,并由本仓维护者**逐条对当前工作树代码核实**(2026-05-30)。
>
> **核实前提**:当前工作树为**原始上线代码**(例:`revision_service.go:448` `MergePR` 仍用 `now := galgame.Updated`)。两位外部审计审的就是这份代码,故下列发现对当前代码成立。
>
> 核实标记:确认 = 直读代码确认 · 大致确认 = 与两审一致 + 代码模式吻合(未逐行复跑)· 注意 = 确认但有修正/属文档化取舍 · 待确认 = 未能在本仓定位(需团队确认)

---

## 修复状态(2026-05-30)

> `go build ./... && go vet ./... && go test -p 1 ./...` 全绿;新增 `oauth_client_secret_test.go` 验证 H2 双路径。

**已修(代码):**
- **HIGH**:H1(`F001` admin 对 admin 目标的 ban/anonymize/force-logout 加 `adminProtected` 门 + handler 映射 403)、H2(`F002/F015` client secret 改 `sha256:` 哈希存储 + `VerifySecret` 双路径兼容旧明文 + 两处验证点 + image ClientAuth + create 落哈希 + 新增 `cmd/migrate-hash-client-secrets` 回填工具)。
- **MEDIUM**:M1(`F007` `/auth/refresh` 拒绝 `ClientID!=""` 的 oauth-flow session)、M2(`F009` ban/anonymize 的会话撤销改 best-effort + log)、M3(`F010`+`GPT-M01` 幂等全字段比对 + `OnConflict DoNothing` 收敛并发,消除一次性 500)、M4(`F011` reference-ping 按调用方 site 过滤,新增 `ExistingHashesForSite`)、M5(`F013` GC 软删 UPDATE 重查 `last_referenced_at` 谓词)、M6(`F014` series Create 加 staff 门)、M7(`F040` `GET /image/:hash` 的 `sites` 仅返回调用方自己的 site)。
- **LOW**:`F035`(stats 用 PG 会话时区算日界)、`F037`(reset 拒绝 banned)、`F038`(改邮箱验证码改 `subtle.ConstantTimeCompare`)、`F039`(admin 搜索 ILIKE 转义 `%/_`)、`F041`(`DecodeFromBytes` 先 `DecodeConfig` 验尺寸再 decode)、`F042`(GC 阶段失败向 runner 返回 error)、`F043`(MergePR `completed_time=NOW()`)、`F044`/`F082`(contributor Count/Create 错误传播,共 3 处)、`F047`(message 列表 Count 错误传播,2 处)、`F048`(BanGalgamesByUser 返回 failed_ids)、`F051`(`VerifyMoyuPassword` 改 `crypto/subtle`)。

**已缓解 / 记录(不改代码):**
- `F046`:`GalgameRevision.action` CHECK 约束的 ALTER 已由 `cmd/migrate-galgame`(DROP+ADD CONSTRAINT)处理,模型注释也已警示——**已缓解**,无需改。
- `F087`:admin web 的 access_token 为非 httpOnly cookie 是 **SPA 读取令牌的设计取舍**(refresh_token 已 httpOnly);改 httpOnly 会改动整套前端鉴权模型,**暂按取舍记录**,缓解靠 CSP / 防 XSS。
- `F076`:migrate-users 缺 post-remap 三库 id 对账断言——迁移已执行,价值有限;建议作为**独立校验脚本**补,**未改复杂的一次性迁移工具**。

**部署后需跑一次(可先 `-dry-run`):**
- `go run ./cmd/migrate-hash-client-secrets` —— 把现存明文 client secret 哈希(幂等、非破坏:下游沿用现有明文仍可验证)。

**二轮核查补修(2026-05-30,据 `kungal-docs/audit/claude.1.md` 指出的残留):**
- **F001 残留**:`AdminService.UpdateUser` 之前用 `FindByUUID` 且直接写 `req.Status`,admin 可借这个通用编辑接口给同级 admin 设 `Status=1`,**绕过** Ban/Anonymize/DeleteSessions 上的 `adminProtected` 闸(可逆封禁)。已改:载入角色(`FindByUUIDWithRoles`)+ 当 `req.Status==1 && 目标为 admin && 原本未封` 时返回 `ErrForbidden`,handler 映射 403(与 BanUser 一致);非封禁编辑与解封不受影响。
- **F037 残留**:`ResetPassword` 已挡 banned(安全边界在),但 `ForgotPassword` 仍会给 banned/匿名账号签发重置 token 并发邮件。已改:`ForgotPassword` 对 `IsBanned()` 静默返回(邮件无意义 + 不泄露"该账号存在但被封"的 oracle)。

**三轮补修(2026-09-08,管理台补上「编辑用户资料」入口时把这条路径重新读了一遍):**

- **F001 二次残留**:上一轮只给 `UpdateUser` 的 `Status` 加了门,`Name`/`Email`/`Avatar`/`Bio` 仍无 actor↔target 等级比较;且 `adminProtected` 按角色名匹配 `"admin"`,而 `ren` 只有 `ren` 角色行(12-site-roles:ren 仅可直接配 DB 授予)→ **admin 之上的账号在 ban/anonymize/force-logout 里也从未被保护过**。已改:`adminProtected` 改问权限包(`Can(roles, oauth.admin_access)`,覆盖 admin 与 ren),`UpdateUser` 对受保护目标要求 `oauth.roles.grant_admin`。
- **PII 键的写侧缺口**:`GET /admin/users/:uuid` 按 `oauth.users.pii_view` 把 email 脱敏成空串,但 `PATCH` 既不校验该键就允许改 email,**响应还回明文 email**——普通 admin 可把任意账号(含 ren)的邮箱改成自己的,再走「忘记密码」接管;不改也能拿 PATCH 当 PII 读取器。已改:改 email 需持该键,PATCH 响应同 GET 一样脱敏,写入前 `NormalizeEmail`(与注册/自助改邮箱一致)。
- 仍按取舍/缓解保留(均 LOW):**F045**(MergePR/Revert 重放快照 tag/official/engine/series id 未校验存在性,NO_CLAIM——与"已删除 #ID"渲染、无 FK 的现有设计一致,id 来自先前已校验快照)、**F046 残留**(运行期 PR/revision status 无对账)、**F076**、**F087**。

> 下方逐条保留原始核实记录与最小修法;每条的修复状态见上方汇总(按 finding ID 对照)。

---

## HIGH(2)

### H1 · 任意 admin 可封禁/匿名化任意账号(含同级/超管),无 actor↔target 角色等级校验
- **来源/契约**:`F001` · C1 · 直读 `admin_service.go` + `admin_handler.go` + `role.go` 确认
- **位置**:`internal/platform/auth/service/admin_service.go`(`BanUser` 155-168、`AnonymizeUser` 195-250、`UnbanUser`、`DeleteUserSessions`);`cmd/oauth/main.go`(admin 组仅 `RequireRole("admin")`);`internal/middleware/role.go`
- **核实**:admin 组只校验 caller 含 `"admin"` 角色;四个变更方法只按 path `uuid` 取目标,**全程无 target.role 与 actor.role 的等级比较**;`AnonymizeUser` 不可逆(`UnbanUser` 对已匿名账户直接拒绝)。一个被盗/作恶的 admin 可永久匿名化(抹 PII + 全站强制下线)任意同级 admin 甚至平台所有者,且 id 被 galgame/image 等跨服务 FK 引用 → 跨站爆炸半径。
- **最小修法**:在四个方法里加载 target 角色,当 `target.rank >= actor.rank` 时拒绝(actor 角色/uuid 从 handler 经 `c.Locals` 下传);匿名化/封禁 admin+ 仅限超管。

### H2 · OAuth client secret 明文存储,Basic/token 校验直接对 DB 列做恒等比较
- **来源/契约**:`F002`(+`F015` 同根因)· C3 · 直读 `oauth_client_basic_auth.go:43` + `oauth_client.go:17` 确认
- **位置**:`internal/middleware/oauth_client_basic_auth.go:43` `subtle.ConstantTimeCompare([]byte(client.Secret), []byte(secret))`;`internal/platform/site/model/oauth_client.go:17` `Secret string gorm:"size:255;not null"`
- **核实**:对明文列做恒等比较——bcrypt/argon2 哈希永不可能等于明文,**代码本身证明 secret 明文存储**。该 client_secret 同时是 moemoepoint 账本写入(`POST /users/:id/moemoepoint`)与 `/oauth/token` 签发凭据。任一 DB 读权限泄露(SQLi/备份/副本/运维)即拿到可直接使用的 s2s 凭据 → 账本任意写 / 跨站冒充。HIGH 而非 CRITICAL,因需先有 DB 读泄露。
- **最小修法**:创建时哈希存储,中间件与 token 签发改用哈希校验器;若某流程必须可逆,隔离出主库。

---

## MEDIUM(7)

### M1 · 旧 `/auth/refresh` 接受 body 传入的 OAuth-flow refresh token,绕过 client 绑定、丢失 scope/site_id
- **来源/契约**:`F007` · C2 · 大致确认(两审一致;auth 模型需团队再追 session-vs-oauth refresh token 是否同源)
- **位置**:`internal/platform/auth/service/auth_service.go:407` `RefreshToken` → `FindByRefreshTokenOrPrev`
- **核实**:`/auth/refresh` 从 cookie 或 body 取 refresh token,经 `FindByRefreshTokenOrPrev` 找 session 续签,不做 client 绑定 / grant-type 校验。若 session refresh token 与 oauth-flow refresh token 共表,则可走此路径绕过 `RefreshWithClient` 的 client 绑定与 scope/site_id。建议团队确认 token 模型后定级。
- **最小修法**:区分 session-cookie refresh 与 oauth client refresh;`/auth/refresh` 只接受前者,拒绝 oauth-flow token。

### M2 · `BanUser` 半成功:status 已存、撤销会话失败时接口报错(掩盖"已封禁但未下线")
- **来源/契约**:`F009` · C2 · 注意:确认核心,**但标题"404"有误——实际是 500**
- **位置**:`internal/platform/auth/service/admin_service.go:155-168`(`user.Status=1; Update; return sessionRepo.DeleteByUserID(...)`)
- **核实**:status 更新先提交,再 `DeleteByUserID`;若后者失败返回的是裸 gorm error(非 `*AppError`)→ handler 兜底走 `InternalError`(**500,非报告所称 404**)。结果:用户已封禁(status 存了)但接口报 500,admin 误以为失败、会话可能仍活。重试可补(再封 + 再删会话)。
- **最小修法**:封禁放一个事务(或 status 更新与会话撤销都成功才算成功);失败时返回明确错误且说明"已封禁,会话撤销失败,请重试"。

### M3 · moemoepoint 幂等:先查后插(并发同 key 撞唯一索引→500)+ 命中只比 3 字段,不比 ref/source/actor/note
- **来源/契约**:`F010` + `GPT-M01` · C3 · 直读 `moemoepoint_service.go` 确认 · 注意:并发 500 属**已文档化的接受取舍**,3 字段比较是**真契约缺口**
- **位置**:`internal/platform/auth/service/moemoepoint_service.go:74`(`First` by key)、`:77`(只比 `UserID/Delta/Reason`)、`:93`(后插)
- **核实**:① 幂等是事务内 `SELECT … First` 再 `Create`,并发同 key 第二个撞 `uniqueIndex(idempotency_key)` → 一次性 500——`06-moemoepoint.md §8` 已声明"唯一索引兜底,极少数并发同键得一次性 500,重试即 `applied:false`",属**接受的取舍**,非新 bug。② 命中旧 key 时只比 `user_id/delta/reason`,**不比** `ref/source_app/actor_user_id/note`;而契约(`§3.1` 错误码 `16004`)规定"幂等键已存在但**请求体不一致**→ 16004"。故"同 key 不同 ref"会静默返回旧结果而非 16004 → **真契约缺口**。
- **最小修法**:幂等命中时比对全部业务字段(至少含 `ref/source_app`),不一致返 `16004`;并发竞态可用 `INSERT … ON CONFLICT DO NOTHING` + 命中回读,把 500 变成 `applied:false`。

### M4 · `reference-ping` 实际不按站点过滤(docstring 谎称已过滤)
- **来源/契约**:`F011` · C4 · 直读 `handler.go Ping` + `service.go ReferencePing` 确认
- **位置**:`internal/platform/image/handler/handler.go`(`Ping` 不取调用方 site)、`service/service.go:~408`(`ReferencePing` 只 `FindExistingHashes` + `TouchReferenced`,无 site 过滤);docstring 却写"filtered to those the caller's site has actually used"
- **核实**:Ping 处理器把 hash 直接喂给 `ReferencePing`,服务层对**任意已存在 hash** 刷新 `last_referenced_at`,不校验该 hash 是否被调用方 site 用过。任一 image client 可保活任意已知 hash(阻止其被 GC)。impact 偏低,但**文档与代码不符**且跨站。
- **最小修法**:让 `ReferencePing` 接收调用方 site,仅 `TouchReferenced` 该 site 在 `image_site_usage` 里实际用过的 hash;或修正 docstring 明确"全局保活"是有意。

### M5 · image GC 软删 TOCTOU:按谓词选行,再按 id 更新 `deleted_at`,不重查 `last_referenced_at`
- **来源/契约**:`F013` · C4 · 直读 `gc.go` 确认
- **位置**:`internal/platform/image/service/gc.go`(`softDelete`:`SELECT … WHERE deleted_at IS NULL AND last_referenced_at < threshold` 收 ids → `UPDATE … WHERE id IN ?` SET deleted_at,**UPDATE 不带 `last_referenced_at` 谓词**)
- **核实**:SELECT 与 UPDATE 之间若某 hash 被 `reference-ping` 刷新,它仍会被软删(UPDATE 未重查谓词)。窗口小(GC 低频 + 365d 阈值)但真实。
- **最小修法**:UPDATE 带上同一谓词:`UPDATE … WHERE id IN ? AND last_referenced_at < ?`(用同一 threshold),把刚保活的行排除。

### M6 · galgame series Create 未做 staff 门:任意登录用户可建系列并把任意 galgame 归入
- **来源/契约**:`F014` · 直读 `series_handler.go Create` 确认
- **位置**:`internal/platform/galgame/handler/series_handler.go`(`Create` 只检 `userID==0`,读 `roles` 仅用于 `roleLevel(roles)` 审计,**无 `hasRole(roles,"admin","moderator")` 门**)
- **核实**:与同模块 tag/official/engine Update 及 series Delete/Revert 的 staff 门不一致——series Create 是唯一漏的。`CreateSeries` 会把 `req.GalgameIDs` 里任意 galgame 的 `series_id` 改掉并写 galgame_revision。任意持有效令牌的用户可篡改归属。
- **最小修法**:`Create` 读 roles 后加 `if !hasRole(roles,"admin","moderator"){ return Forbidden }`(与 Delete/Revert 一致)。

### M7 · `GET /image/:hash` 向任一 image client 返回跨站 `sites` 列表与审核标签
- **来源/契约**:`F040`(两审从 LOW 上调 MEDIUM)· C4 · 直读 `handler.go Meta` 确认 · 注意:代码注释已自承"V2 再限制"
- **位置**:`internal/platform/image/handler/handler.go`(`Meta` 返回 `"sites": sites` 与 `"review_labels"`)
- **核实**:Meta 把该 hash 的**跨站使用列表**(哪些 site 用过)与审核标签返回给任意通过 Basic Auth 的 image client。一个 site 可借此探知其它 site 用了哪些图 → 跨站元数据泄露。模型注释已写"available to any authenticated caller; consider restricting in V2",属已知但未处理。
- **最小修法**:`sites` 仅返回调用方自己的 site(或对非 admin 隐藏);审核标签同理收敛。

---

## LOW(15)

> 多为静默吞错 / 时区 / 一致性瑕疵。「确认」为本次直读确认,「大致确认」为与两审一致 + 代码模式吻合。

| ID | 标题 | 位置 | 核实 |
|---|---|---|---|
| `F035` | daily-stats 用进程本地 TZ(`now.Location()`)定边界,与 PG `date_trunc('day')` 的会话 TZ 不一致 → 跨 TZ 部署少/多算一天 | `internal/platform/galgame/repository/admin_repository.go:62,73,119` | 确认 |
| `F037` | 密码重置(Forgot/Reset)不拒绝 banned/匿名化用户(与其它路径不一致;实际影响小,封禁者本就无法登录) | `internal/platform/auth/service/auth_service.go:482,524` | 确认 |
| `F038` | 改邮箱验证码用 `data.Code != code` 普通比较(非常量时间),与注册/邮箱验证路径的 `subtle.ConstantTimeCompare` 不一致 | `internal/platform/auth/service/auth_service.go:765`(对比 `:199`) | 确认 |
| `F039` | admin 用户搜索 `FindAllPaginated` 的 ILIKE 不转义 `% / _`(与 `SearchByName` 的 `escapeLikePattern` 不一致) | `internal/platform/auth/repository/user_repository.go:~290`(对比 `:80-107`) | 确认 |
| `F040`→见 M7 | (已上调 MEDIUM) | — | — |
| `F041` | 解压炸弹像素上限 `MaxDecodedPixels` 在**完整 `image.Decode` 之后**才校验,小文件可先分配巨图再被拒 | `internal/platform/image/processor/processor.go:36-43,57` | 大致确认(应改用 `DecodeConfig` 先验尺寸) |
| `F042` | GC 某 phase 失败被计数但 runner 仍返回成功 | `internal/platform/image/service/gc.go` | 大致确认 |
| `F043` | `MergePR` 的 `completed_time` 用事务前的 stale `galgame.Updated`(应为合并时刻) | `internal/platform/galgame/service/revision_service.go:448,451` | 确认(直读确认,对比 `DeclinePR:492` 用 `NOW()`) |
| `F044` | `MergePR` 内 contributor upsert 的 `Count`/`Create` 错误被吞 | `internal/platform/galgame/service/revision_service.go:~453` | 确认 |
| `F045` | `MergePR`/`Revert` 重放 snapshot 的 tag/official/engine/series id 时不重新校验存在性 | `internal/platform/galgame/service/revision_service.go` | 大致确认 |
| `F046` | `GalgameRevision.action` / `GalgamePR.status` 的 CHECK 约束仅 inline gorm tag,AutoMigrate 无法 ALTER 既有约束(部署隐患) | `internal/platform/galgame/model/pr.go:12,45`(注释已自承) | 确认 |
| `F047` | galgame message 列表 `Count(&total)` 不检查 `.Error` → 可能 false zero | `internal/platform/galgame/...message...` | 大致确认 |
| `F048` | `BanGalgamesByUser` 吞掉单项失败,接口报全成功无失败 id 列表 | `internal/platform/galgame/service/admin_service.go:179` | 大致确认 |
| `F051` | `VerifyMoyuPassword` 手写"constant-time"字节循环,应用 `crypto/subtle` | (本次未在 `internal/platform/auth` 定位到该函数) | 待确认:需团队确认位置 |
| `F076` | `migrate-users` 重映射 oauth user id 后无 post-remap 对账断言(三库 id 集合一致性未校验) | `cmd/migrate-users/main.go` | 大致确认 |
| `F082` | 两处 galgame 写路径在条件 `Create` 前的 contributor-existence `Count` 忽略 `.Error`(false-zero) | `internal/platform/galgame/...`(同 F044 模式) | 大致确认 |
| `F087` | admin web 把 access_token 存在 JS 可读(非 httpOnly)cookie;最高权应用上的 XSS 可直接窃取 | `apps/web/app/composables/{useApiFetch,useAuth,useApi}.ts`(注释已自承非 httpOnly) | 确认(直读确认);注意:属 SPA 读取令牌的设计取舍 |

---

## 跨仓但定位在本仓代码(migrate-users)

- **`F031`**(C1,两审定 LOW):kungal `system_message_read_state` 的 per-user 游标回填依赖运维**先跑 migrate-users 再跑 kungal 012**;`migrate-users` 的 FK 重映射清单**不含**该表(该表由 012 后建)。若顺序颠倒,游标指向 remap 前的旧 id。大致确认,修法二选一:把该表加进 `migrate-users` 的重映射清单,或让 kungal 012 的 id 源显式 + 幂等。

## 与本仓相关但**非本仓侧缺陷**(仅记录,不计入本仓修复项)

- **`F032`/`F024`**(C2):下游(kungal/moyu)封禁/降权最长滞后 ≈ access-token TTL,因下游**只验签不逐请求回查 oauth 状态**。oauth 侧已正确(封禁删 session + refresh 时回查 `IsBanned`),**缺陷在下游**,本仓无需改;若要求秒级生效,需下游缩短 TTL 或敏感操作回查。
- **`F077`**(C2):`invalid-grant`/`invalid-client-secret` 信封 code 在 kungal/moyu 分类不一致——code 是 oauth 定义的,但**分类问题在下游**。

---

## 小结(本仓需处理)

- **优先**:H1(admin rank gate)、H2(client secret 哈希/轮换)。
- **其次 MEDIUM**:M3 幂等全字段比对、M4/M5/M7 image 跨站作用域与 GC TOCTOU、M6 series staff 门、M1 `/auth/refresh` token 绑定、M2 ban 半成功。
- **成批 LOW**:`.Error` 吞错(F042/F044/F047/F048/F082)、TZ(F035)、常量时间比较(F038)、ILIKE 转义(F039)、CHECK 约束迁移(F046)、completed_time(F043)。
- **待确认**:F051 定位、F007 token 模型、F031 迁移顺序责任归属。
