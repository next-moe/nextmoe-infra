# 开发者门户

> 本文承载 §9 开发者门户(`developer.nextmoe.dev`)。设计与命名约定见 [01-design.md](./01-design.md);门户展示的公开 spec 与 OpenAPI 策略见 [02 §10](./02-public-api.md)。

---

## 9. 开发者门户(`developer.nextmoe.dev`)

- **定名与定位(2026-07-28,用户裁定)**:名字朴素——**NextMoe 开发者平台**;定位语 = **「ACGN 数据,以此为准」**(当各源各执一词,NextMoe 逐字段裁定唯一标准答案;首发 Galgame 面,同构扩展至全部 ACGN 媒介)。API 面名不变(spec title 属冻结契约不碰)。
- **账号复用**:用生态账号经 IdP 登录即开发者账号,**不另造身份**(品牌显示随「NextMoe 账户」改名同步,机制零变)。
- **核心功能**:
  1. 创建应用(= 一行 `oauth_clients`,`owner_user_id=当前用户`,`dev_enabled=true`)→ 拿 `client_id`(OAuth);删除应用见 [§9.5](#95-应用生命周期删除取代停用2026-08-29)。
  2. 管理 **API Keys**:创建(**show-once** 明文)、看 `prefix+last4+last_used`、轮换(带宽限)、吊销;**删除**仅限「已吊销且从未用过」的钥匙,见 §9.5。
  3. **用量/配额**(`/usage` 页,读 `GET /dev/usage?days=N`,窗口 7/14/30 天):
     - **每日调用量**柱状图 + 窗口合计(总请求 / 错误率 / 4xx / 5xx)——读 `developer_api_usage` rollup 的稠密日序列。
     - **按应用** / **按面** 两张分解表(各按量降序)。
     - **实时配额剩余**:每把 active key 一张卡,显示今日剩余 / 每日配额 + 用量条 + 速率上限——**直接读 Redis 执法计数器**(与限流同源,非 rollup 估算)。计数后端不可达时该区降级为「暂不可用」提示,页面其余照常(`live_unavailable`)。
  4. **OpenAPI 文档**:用 **Scalar** 渲染(MIT、Try-It 最强、支持 OAuth flow、可嵌 Nuxt);两份公开 spec(catalog 面 / galgame 面)分 tab 呈现,未来媒介面同构加 tab。
  5. 申请更高 tier(走审批)。
- **技术**:门户前端 Nuxt(`apps/` 下新增或并入现有);平台后端扩展 account/IdP 侧的 API(应用/key/用量 CRUD,鉴权用现有 JWT + `owner_user_id` 归属校验)。

### 9.1 登录升级为 OP 跳转 SSO(拍板 2026-07-23 · 生产部署收官 2026-07-26)

门户登录已从本地密码表单升级为 **OAuth Authorization Code + PKCE(S256)跳转登录**(下游站点那套「已在 hub 登录即一键进入」)。门户即 IdP 的一个**第一方 confidential client**;OAuth token 落进**现有** access_token / refresh cookie 约定,`/dev/*` 与 `/auth/me` 靠同一 signer 的 access_token 直接消费(后端**零代码改动**)。

**实现(apps/developer,全部 client/Nitro 侧)**:

- `app/utils/oauth-pkce.ts` — PKCE code_verifier / code_challenge(S256)/ state 生成;
- `app/composables/useOAuthLogin.ts` — `startLogin` / `startRegister`:存 verifier+state+redirect 进 sessionStorage → 顶层跳 `{authorizeBase}/oauth/authorize`(register 先经 OP `/auth/register?redirect=`);
- `app/pages/auth/callback.vue` — 校验 state → POST `/auth/exchange` → 播种 access_token + 拉 user → 跳 redirect/`/dashboard`;
- `server/routes/auth/{exchange,refresh,logout}.post.ts` + `server/utils/oauth-session.ts` — 服务端换码/刷新/吊销 + 落 cookie(access_token JS 可读 Path=/、refresh_token httpOnly Path=/auth、auth_mode 标记);
- 登录 modal(`components/login/Modal.vue`)= SSO 主按钮 + 密码表单回退 + SSO 注册引导。

**关键契约发现(修正原「落进现有 refresh 约定」的假设)**:第一方 `/api/v1/auth/refresh` **拒绝 client-bound(OAuth)session**(`auth_service.go:611`)——OAuth session **只能**经 `/oauth/token` `grant_type=refresh_token` 刷新(轮换)。故门户用 Nitro `/auth/refresh` 包 `/oauth/token`;`auth_mode` cookie 选择刷新/登出路径(`oauth`→Nitro 路由、`password`→第一方 relay),密码回退路径完全不变。access_token 仍是同一 signer,`/dev/*`、`/auth/me` 零改动消费。

**部署配置(✅ 已于 2026-07-26 全部落地:client 注册 + 双 env + oauth/portal 部署;SSO 全链与 /dev/* 栅栏放行均经生产实测)**:

1. **注册 OAuth client**(admin,`POST /api/v1/oauth/clients`,或管理台):`redirect_uris=["https://developer.nextmoe.dev/auth/callback"]`、`grants=["authorization_code","refresh_token"]`、`is_public=false`(confidential,门户有 Nitro 服务端)、`auto_consent=true`(第一方跳过同意页)、`allowed_scopes=[]`(默认 openid/profile/email)。响应给出 `client_id` + 一次性明文 `client_secret`。
2. **配置门户环境变量**(生产):`NUXT_PUBLIC_OAUTH_CLIENT_ID`、`NUXT_OAUTH_CLIENT_SECRET`(服务端)、`NUXT_PUBLIC_OAUTH_AUTHORIZE_BASE=https://oauth.kungal.com/api/v1`、`NUXT_PUBLIC_OAUTH_WEB_BASE=https://oauth.kungal.com`、`NUXT_PUBLIC_OAUTH_REDIRECT_URI=https://developer.nextmoe.dev/auth/callback`。`redirect_uri` **完全串匹配**,勿有尾斜杠漂移。**注意:Dokploy Environment 面板的值只做 compose 变量替换——变量必须同时在 `docker-compose.developer.yml` 的 `environment:` 块里声明转发才会进入容器**(缺声明会静默回落到镜像构建期的 localhost 默认值,2026-07-23 首次部署实爆);五个 SSO 变量已全部声明,新增 runtime-config 键时须同步补这里。
3. **本地 dev**:向本地 `kun_galgame_infra` 播一条等效 client 行(redirect_uri 用 `http://127.0.0.1:9430/auth/callback`),并设对应 `NUXT_PUBLIC_OAUTH_*` / `NUXT_OAUTH_CLIENT_SECRET`;authorize/web base 指向本地 OP(API :9277 / 前端 :9420)。注意 `refresh-dev-db` 会抹掉一切手播的 dev-only client 行——刷新后需重播,或改用快照自带的 prod client + `dev-secret-<client_id>` 契约。**已固化配方(2026-07-26 首播)**:client 行 = `devportal-dev` / secret = `sha256:` + hex(sha256(`dev-secret-devportal-dev`))(公开 dev 凭证契约)/ confidential / `auto_consent=true` / grants `["authorization_code","refresh_token"]` / scopes `["openid","profile","email"]` / redirect 精确 `http://127.0.0.1:9430/auth/callback`,SQL 模板沿用 `docs/dev-environment.md` 的 letmoe-dev upsert(替换 VALUES 即可);门户侧 env 落 `apps/developer/.env`(gitignored)= 五个 SSO 变量 + **`NUXT_OAUTH_API_BASE=http://127.0.0.1:9277`**(nuxt.config 的 dev 默认是 `:19277`,本地 oauth 实际监听 `:9277`,不覆写则 Nitro 换码/刷新打不通)。**栅栏联动**:oauth 进程须带 `KUN_DEV_PORTAL_CLIENT_IDS=devportal-dev`,否则 SSO 登录成功但 `/dev/*` 被 DevPortalFence 403(fail-closed;密码回退不受影响)。**本地已固化为默认(2026-07-26)**:`apps/api/.env`(godotenv——air 热组与手起二进制皆读)、`apps/api/.env.example`、`docker-compose.dev.yml` oauth 块三处均为 `devportal-dev`;oauth 重启后协议级 E2E 全链绿(login → consent 签码 → 门户换码 → client-bound token → `/dev/*` 200 → refresh 轮换 → 再 200)。注意:`GET /oauth/authorize` 设计上恒 303 到 OP 前端页,授权码由 OP 前端 auto_consent 后打的 `POST /oauth/authorize/consent` 签发——脚本化 E2E 必须走 consent 腿。

> `/dev/*` 的 owner 判定按 uid、与 token 的 client 归属无关(已核:OAuth access_token 在 `/auth/me` 与 devapi 链上等价于直登 token)。

**验收后记(2026-07-23 双维度评审后修正)**:

- **token 读取器**(`server/utils/oauth-session.ts` `tokenWirePayload/tokenWireError`):`/oauth/token` 是 OAuth 协议端点,只有 RFC 6749 裸 shape(成功 `{access_token,...}` / 失败 `{error,error_description}`)。exchange/refresh 路由以 **access_token 存在性**判成败,不看任何状态字段——2026-07-25 线格式切换那天,只看 `code` 的读取器会静默全断,这条判据是唯一没被咬到的原因。
- **登出双模全清**(`useAuth.logout`):密码与 SSO 两种 session 的 refresh_token 同名不同 Path(`/api/v1/auth` vs `/auth`),登出无条件两路都打——只清当前 auth_mode 会让另一模式的存活 cookie 在下次导航时把用户「静默复活」登录(跨账号时更是错账号复活)。
- **瞬时刷新失败不清会话**(`useTokenRefresh` 返回 `REFRESH_TRANSIENT`):网络抖动 / IdP 5xx / Nitro 刷新路由的蓄意 503 不再被判成 session 死亡强制登出;仅 4xx(无 cookie / 过期 / 吊销)才清会话跳登录。**重试 UI(2026-07-26 补齐)**:transient 态现有全局呈现——`useRefreshTransient`(useState,记账收敛在单飞 promise 上)驱动 `layout/RefreshBanner` 固定横幅(说明会话仍有效 + 一键重试/忽略;以 `auth_mode` cookie 为「确有会话」门,匿名访客不见横幅);重试成功即落 token、拉 user、`refreshNuxtData()` 重取降级页面的数据,重试发现 session 已死才清态跳 /login。同波修正 `middleware/auth.ts`:原先把 transient 布尔坍缩成「未刷新→弹 /login」,违反本条契约;现改为三态——成功落 token 放行、transient 放行(页面降级渲染 + 横幅接手)、确死才弹登录。
- **已接受的偏差(有意为之,评审记录在案)**:access_token 为 JS 可读 cookie(沿袭 apps/web 约定;refresh_token httpOnly 兜底持久层);PKCE verifier/state 存 sessionStorage(confidential client 下 PKCE 是纵深防御,主认证在服务端 client_secret)。
- **client 栅栏(已拍板并实现,2026-07-23 wave 08)**:上面「owner 判定与 token 的 client 归属无关」原是隐患——`middleware.Auth` 只验 signer + uid、不查 token 属哪个 OAuth client,任何第三方 app 的用户 token(仅授 `openid profile email`)都能替用户铸/轮换/吊销 API key(confused-deputy)。已加 **`DevPortalFence`**(`middleware.Auth` 之后):第一方 `/auth/login` session token(`client_id==""`)与 env `KUN_DEV_PORTAL_CLIENT_IDS` 白名单内的 client 放行,其余 403;**空白名单 = fail-closed(仅放行第一方)**。因此门户专属 client 注册后,须把它的 `client_id` 填进 **oauth 服务**的 `KUN_DEV_PORTAL_CLIENT_IDS`,否则门户自己的 SSO 用户会被栅栏 403(密码回退不受影响)。完整契约与 `dev:manage` 升级路径见 [03 §4.4](./03-auth-and-tiers.md)。

### 9.2 应用自助登录能力(`user_login`)

到本节前,自助注册的 app 是**纯 API key 身份**:`grants: []`、`redirect_uris: []`,fail-closed,根本不可能签出用户令牌。当每一张开放 API 面都是匿名读时那是对的默认;当某张面必须知道**是哪个用户**时它就是错的 —— 游戏时长是第一个。替代方案是继续在 OAuth 控制台手工建 client,那等于让人永远卡在每一个第三方集成的中间。

`POST /api/v1/dev/apps` 与 `PATCH /api/v1/dev/apps/:client_id` 接受可选的 `user_login`:

```json
{ "name": "Kurumi", "user_login": {
    "redirect_uris": ["http://127.0.0.1:53682/callback"],
    "scopes": ["openid", "profile"] } }
```

给出它 → app 置 `is_public=true`、`grants=["authorization_code","refresh_token"]`,scope 并入 `allowed_scopes`(`openid` 自动补)。**不给 → 完全保持原样**,本字段出现前注册的每个 app 行为逐字节不变。`user_login` 是**整体替换**而非 patch:否则删掉一个回调将永远做不到,而废弃的回调正是最该能删的东西。**但整块关不掉**——至少要一个回调 URI,没有「取消登录能力」这条路;自 2026-08-29 起这一位还决定了这个应用**永远删不掉、只能归档**,见 [§9.5](#95-应用生命周期删除取代停用2026-08-29)。

**门户表面(2026-09-09 补齐)**:建应用弹窗仍只收 `name` / `description`,`user_login` 的开启与编辑在**应用详情页的「用户登录」卡片**上,提交的就是本节这个 `PATCH /dev/apps/:client_id`(闸同 §9 的编辑/删除,即 `app.manage`);既然整块关不掉,卡片上也就没有「关闭」这个动作。在这之前本节只有 API、门户没有任何入口,而 [10 §18.2](./10-native-app-integration.md) 写着「在门户建一个带 `user_login` 的应用」——2026-09 一个下游据此判定回调地址只能由运营代注册,补这张表单就是为了关掉这条缝。

**四道护栏**(开放自助注册的代价,一个都不能省):

1. **回调白名单**:只收 `https://`(且不是裸 IP)与 `http://` 到 `127.0.0.1` / `[::1]` 环回。拒绝通配、fragment(隐式流的令牌通道)、userinfo(`https://example.com@evil.com/cb` 在人眼里是前者)、以及到任何非环回主机的明文 http —— 授权码就走在这个 URL 里。**`localhost` 也拒**:它过主机名解析,可以被指向别处,`127.0.0.1` 不能。
2. **强制 PKCE**:桌面应用把二进制发给用户,里面没有秘密。标 `is_public` 即让 OAuth 服务在无 `code_challenge` 时**拒绝**它的授权码。环回回调按 **RFC 8252 §7.3 端口无关**匹配(端口是运行时才选的),scheme/host/path/query 仍精确匹配 —— 非环回 URI 永远走不到这个分支。
3. **保留名**:同意页把应用名显示在用户账号旁边,「NextMoe 官方助手」就是我们自己托管的钓鱼页。含 nextmoe / 未萌 / kungal / 官方 / official / admin 等片段一律拒。这是地板不是滤网(存心的冒充者会用同形字),配套的是同意页上不靠猜意图的**第三方标记**(`owner_user_id` 非空)。
4. **同意 scope 白名单**(`selfServiceUserScopes`):`openid` / `profile` / `email` / `playtime:read` / `playtime:write` / **`catalog:edit`(wave R3,2026-08-17 起)** / **`catalog:read`(2026-09-06 起,见 [10 §18](./10-native-app-integration.md))**。**`playtime:read` / `playtime:write` 仍可申请,但调 `/v2/me/playtimes` 不再需要它们**(`/v1/playtime` 已于 2026-08-27 退役)——任何已开通用户登录的应用都能读写该用户自己的时长。这两个词留在白名单里只是为了让旧授权 URL 不 400。**注意仍不在其中的**:`image:upload`、`artifact:upload` —— 自助注册不能向人索取花我们存储的权限。往这张表里加一项是**政策决定**,不是改配置;`catalog:edit` 就是这样一次政策决定,其代价已在面上收讫:第三方令牌永远 `ModerationCapped`(只能提案、不能裁决),且每用户未决提案帽 20(429)。写共享语料因此始终隔着一道人审。

它与 API key 的 scope 白名单(`selfServiceScopes` = `catalog:read` / `store:read`)**仍然是两张表**:一个说机器 key 匿名能干什么,另一个说应用**能向人要什么**。合并就等于让只读 key 的白名单去决定同意页的政策。

2026-09-06 起 `catalog:read` 这个**词**同时出现在两张表上,那不是合并——`/v2/catalog` 只读面从此接受应用密钥**或**带该 scope 的用户令牌(仍是一条请求一个凭证),所以「向人要读目录的权限」第一次成为一件说得通的事。两张表各自的判据没有变,只是恰好都收了这一项。**注意仍不在同意表里的**:`store:read`、`claim_events:read`、`folder_holders:read` —— 第一个是结算口径,后两个是运营授予,都没有可向人索取的形态。`folder_holders:read`(2026-09-09 起)尤其不能自助:它答的是「谁把这部作品放进了收藏夹」,含私密夹,那是别人的收藏而不是目录里的行。

一处实现细节值得记下来,因为它看起来像 bug:`appAllowedScopes` 给每个自助应用无条件注入 `catalog:read`,所以 `allowed_scopes` 分不清「应用申请了它」与「注入的」。`toUserLoginView` 因此照旧把它滤出同意视图。没有功能损失——注入本身就意味着任何自助应用都可以在 `/oauth/authorize` 请求它。

### 9.3 授权制 scope 的申请通道(2026-08-18 建,2026-08-25 退役)

这条通道存在过一周。它把 `news:read` 的"联系平台"变成门户里的一次申请:自助面两条端点(`POST` / `GET /api/v1/dev/scope-applications`)、管理侧三条(`/api/v1/admin/devapi/scope-applications*`)、铸密钥对话框里的授权制条目(`components/keys/ScopeApplyModal.vue`)、管理台的「Scope 申请审核」面板(`components/devapi/ScopeApplications.vue`),以及承载它们的 `devapi_scope_applications` 表。

**整条通道于 2026-08-25 删除**,连同它唯一的申请对象:`/v1/news` 不再检查 scope,一把有效的机器 key 就够了,而同一批内容在 `/v2/news` 上匿名可读已有一段时间(`/v1/news` 本身已于 2026-08-27 退役)。审批队列因此没有任何东西可决——留着它只是让人排队等一个必然的"是"。裁定与备查清单见 [02 §3.9](./02-public-api.md)。

铸密钥对话框现在只剩自助复选框一组(`components/keys/MintModal.vue`);管理台 `/devapi` 页少了审核面板那一节。

### 9.4 平台策略矩阵 + 应用审批流(2026-08-18)

到本节前,门户的自助面是**无级别的**:注册即建应用、建完即启用、启用即能铸 key。这在只有我们自己用的时候是对的默认;一旦第三方真的进来,平台就需要一个能在**不改代码、不改部署**的前提下收紧或临时关停某一步的旋钮。本节把这个旋钮做成一张矩阵。

**矩阵**(能力 × 允许的 mode,判据表见 [02 §3.10](./02-public-api.md)):`app.create`(自助 / 需审批 / 关闭)、`app.manage`、`key.mint`(后两者只有自助 / 关闭)。本波起手是四能力,第四行 `scope.apply` 随 §9.3 于 2026-08-25 退役。默认全开 —— **平台出厂是开放的,策略只做收紧**,所以任何一行缺 override 都等于今天的行为,升级这一波对现有开发者是零可见变化。

**吊销永远不入闸**。关掉 `key.mint` 的场景是「先别再发新钥匙了」,不是「谁也别想止损」;把 revoke 一起关掉会让一次泄漏在策略打开之前无法收敛。

**为什么是 ren-only**:改矩阵不是日常运营动作(那是 `devapi.manage` 管的:调 tier、配额、审应用),而是**改平台对外承诺**——「现在还能不能自助注册」这句话对所有第三方同时生效。故新增 `devapi.policy_manage`,**只进 ren 捆**且标 `non_delegable`(先例:`oauth.permissions.manage`)。管理台的矩阵对 admin **可见但只读**,并明示「仅 ren 可改」——看得见才知道当下是什么政策,看不见只会让人反复去问。

**审批流**只加了三个状态、没有第四个:`approved` / `pending` / `declined`。`withdraw`(申请人撤回)**故意不做**——待审的申请撤回等价于停用一个从未启用的应用,而 `declined` → resubmit 已经覆盖了「想改了再来」这条真实路径;多一个状态就多一组迁移与四处 UI 分支,换不到任何新能力。

**门户表现**(`apps/developer`):dashboard 拉 `GET /dev/policies` → `approval` 时创建对话框顶部挂提示、提交后回执「已提交,等待平台审核」;`disabled` 时创建按钮禁用并说明原因。应用卡片与详情页对 `pending` / `declined` 挂状态 chip;`declined` 展示拒绝理由 + 「重新提交」按钮(→ resubmit 端点),~~两态都**隐藏「停用」**~~(**2026-08-29 翻案,见 [§9.5](#95-应用生命周期删除取代停用2026-08-29)**:这两态恰恰是最需要删除的两态)并在密钥区写「审核通过后可铸造密钥」。`key.mint=disabled` → 铸造 / 轮换禁用(吊销照常);`app.manage=disabled` → 编辑 / 删除禁用。

**管理台表现**(`apps/web`,归档/删除/结算三项由 §9.5 补入):`/devapi` 页顶部为策略矩阵卡(`components/devapi/PolicyMatrix.vue`,改动走确认弹窗;选中「默认」那格即 `DELETE` 掉 override 行),应用列表加状态过滤(`enabled` / `pending` / `declined` / `disabled` / `all`,缺省 `enabled` 兼容现状)并在卡片上显示 review 状态,`components/devapi/PendingApps.vue` 是待审应用面板(通过 / 拒绝,拒绝须填理由)。新页 `/devapi/keys`(`components/devapi/Keys.vue`)是**跨全部应用的密钥清单**:按状态与应用过滤、分页,只展示前缀与后四位等元数据,行动作 rotate / revoke 直接复用既有 per-app 端点 —— 这一页刻意**不新增编辑端点**,「编辑 token」在这个平台上从来就只有轮换与吊销两个动作。(2026-08-29 加了第三个,但它不是编辑:删掉一把**已吊销且从没用过**的钥匙,见 §9.5。)

### 9.5 应用生命周期:删除取代停用(2026-08-29)

门户此前没有「删掉一个应用」这个动作:`DELETE /dev/apps/:client_id` 叫停用,做的是 `dev_enabled=false`,而 5-app 上限从来不看 `dev_enabled` 也不看审核状态。于是一个被停用的、被拒的、还在排队的应用**永久占着五格之一**,门户里没有任何交回它的办法——**五次被拒,这个账号就再也进不来平台了**,而它一次也没做错什么。本节把停用换成删除。判据、状态机与两面端点表见 [02 §3.11](./02-public-api.md);这里只记门户侧看得见的部分。

**删除做什么**:吊销该应用全部活钥匙、连带删掉其中**从未服务过任何请求**的钥匙行(否则 owner 再也够不到它们——归档后的应用对自助面不可见,那把没用过的钥匙会变成只有运营清得掉的孤儿),再把应用移出 owner 的世界——列表里消失、每一条 per-app 路由 404、**不再计入五格**。行本身按引用分两臂:**从未被任何东西引用过的壳直接删掉,有过引用的归档留痕**(有用量的钥匙一律留下,并且把应用的行一起留下)。两臂共用运营硬删的同一道守卫,门户这条路上消失的行恰好是运营本来就被允许删的那一行。

**开过用户登录的应用只归档,永远不真删**:判据是「这个 client 有没有登录配置」(`is_public` / `grants` / `redirect_uris`),不是「现在还有没有人登着」——过期 session 与授权码每小时被清理作业**真删**,数它们只答得出此刻的状态。真正的原因在另一个库:用户时长按 `(user, work, client)` 落在 `kun_catalog`,平台侧的守卫够不到那里,所以凡是能签用户进来的 client 一律留行。**门户默认建出来的是纯 API 应用,不受影响**(§9.2:不传 `user_login` 就是 `grants: []` / `redirect_uris: []`);代价是**开了 `user_login` 的应用哪怕建完立刻删,也会留下一行归档**——这是不制造孤儿时长行的价钱。

**它是单向的,文案必须这么写**:门户没有解归档,复活只有平台运营做得到(`PATCH dev_enabled=true`)。删除对话框要说清楚的是「短链、登录会话与用量历史都留着,消失的是这个应用在你这里的位置」——而不是「数据已清除」。

**`pending` / `declined` 现在也能删**,而且它们恰恰是最需要删的两态(§9.4 那条「两态隐藏停用」因此翻案)。原先要防的「等待审核悄悄变成没有了」,由「删除单向、复活只有运营」承接。

**密钥区多了删除,但不是第二个吊销**:只有**已吊销且从未服务过任何请求**的钥匙删得掉(否则 409:未吊销 / 已有用量)。一把用过的钥匙永远只到吊销为止——`developer_api_usage` 背后没有外键,删掉它就把用量页的历史变成指向虚空的行。所以密钥行上的两个动作要分开呈现:吊销是止损(任何策略都关不掉),删除只是收拾一把从没用上的钥匙。

**吊销换到 `POST …/keys/:id/revoke`**(`DELETE …/keys/:id` 现在真的是删)。**前后端不必同时上线,顺序也随意**:还在用旧路由的门户把 DELETE 发过来,打到的是一把没吊销过的活钥匙 → 409,而不是静默地把一份活凭证连同它的记录一起删掉。这道 409 就是「换路由不必卡着部署窗口」的全部理由。

**管理台侧新增三件**(端点见 [02 §3.10](./02-public-api.md) 表):`status=archived` 过滤(其余过滤器一律排除已归档行)、每个应用的归档与硬删动作(硬删须先归档、零引用、且这个 client 从来不具备用户登录能力,否则 409——所以管理台上「删除」对绝大多数登录类 client 是永远灰的,文案要说清是**不能**而不是**没权限**)、以及结算名册开关 `store_settlement_eligible`。**名册开关不要和 `store:read` 画等号**:铸店铺短链是自助的,而每月优惠券池是定额的、每多一个参与者都稀释其余人,所以「分不分钱」是运营在这一列上写下的名册。存量行(含已持有 `store:read` 的九个应用)全部为 `false`。
