# NextMoe·未萌 账户改名与域拆分设计

> 状态:已裁定,执行中。裁定人:用户,2026-09-09。
> 关联:[05-federation-login-design.md](./05-federation-login-design.md)(联邦登录随本次切换一并上线)、
> [03-oidc-standardization-design.md](./03-oidc-standardization-design.md)(issuer 本次定终身,ES256 切换沿用其设计,不与本轨捆绑)。

## 1. 已锁定的裁定

| # | 裁定 | 内容 |
|---|------|------|
| D1 | 先改名,后联邦上线 | Google/GitHub OAuth 应用直接用最终回调域注册,一步到位 |
| D2 | 硬切换,接受全员登出 | 不做双 issuer / 双域名兼容代码;存量会话(含全部下游)在切换时全部作废 |
| D3 | `account.nextmoe.com` = OP + 账户中心 | issuer = `https://account.nextmoe.com`,**一次定终身**,ES256 切换后 JWKS 也在此域 |
| D4 | `admin.nextmoe.dev` = 管理台,独立应用 + 标准 OAuth RP | 管理台不再吃第一方 cookie,走授权码流程(confidential client + BFF),吃自己平台的狗粮 |
| D5 | 零 legacy 代码 | 旧域名只在边缘(Cloudflare 308 重定向,纯配置)存活一个宽限期;合并后代码中不存在任何 kungal 域名的**活引用**(记录历史事故的注释不在此列) |
| D6 | 品牌 = NextMoe·未萌 | 账户面「NextMoe·未萌 账号」,管理面「NextMoe·未萌 管理台」;下游产品(鲲 Galgame 论坛 / 鲲 Galgame 补丁等)保留各自产品品牌,只有平台账户层改名 |

## 2. 目标架构

### 2.1 域名与路由(Traefik,compose 所有)

| Host | 路径 | 后端 |
|------|------|------|
| `account.nextmoe.com` | `/api/v1/**` | oauth 服务(认证 API + 协议端点 `/api/v1/oauth/*` + 联邦回调) |
| `account.nextmoe.com` | `/oauth/jwks`、`/.well-known/*` | oauth 服务(OIDC 元数据,iss = 本域) |
| `account.nextmoe.com` | 其余 | **apps/account**(新 Nuxt 应用:登录/注册/找回/重置/登出、授权同意页、联邦补全页、个人资料) |
| `admin.nextmoe.dev` | `/api/v1/**` | oauth 服务(管理面 API,与今天 oauth.kungal.com 的路由形状一致) |
| `admin.nextmoe.dev` | 其余 | **apps/admin**(原 apps/web 更名:控制台全部页面;catalog/trust/ai 仍走 Nuxt server 代理到内网 DNS,不变) |
| `api.nextmoe.dev` | (不变) | 开放 API 公开面,与本轨无关 |

两个前端对浏览器都是**纯同源**——管理台拆到 .dev 不需要任何 CORS/第三方 cookie,
这是沿用现状(2026-09 时 oauth.kungal.com 一域双面已是同源模式)的直接结果。
`KUN_FRONTEND_CORS_ORIGIN` 仍只为下游站点浏览器直调服务(kungal.com / moyu.moe 家族)。

### 2.2 管理台 RP 模型(apps/admin)

- 新 OAuth client:`client_id = nextmoe-admin`,confidential,grants
  `authorization_code + refresh_token`,scopes `openid profile email`,
  redirect_uri `https://admin.nextmoe.dev/auth/callback`,auto_consent(第一方),
  绑定新 site 行(domain `admin.nextmoe.dev`)。**prod 的 client 行经现有
  oauth-clients 控制台在切换前创建**,secret 只进 admin 应用的 env,不进种子。
- BFF(Nuxt server routes,token 交换走内网 `http://oauth:9277/api/v1/oauth/token`):
  - 未登录 → 302 到 `{account}/oauth/authorize?...`(带 state + PKCE);
  - `/auth/callback` 页 → server route 交换 code,httpOnly refresh cookie 落
    admin.nextmoe.dev,access_token 交给客户端(沿用现有 cookie + Bearer 模式);
  - token 刷新改走本应用 server route(替换直调 `/api/v1/auth/token/refresh`);
  - 登出 = 清本域 cookie + `/api/v1/oauth/revoke`。
- 管理面 API 鉴权不需要任何后端改动:中间件只看 JWT 的 roles claim,OAuth 铸造的
  access token 天然携带(2026-09-09 验证:scope/client_id 只入 locals,无门)。
- admin/ren 的 step-up 语义不变:在 OP 密码登录;联邦对这两个角色仍然拒绝。

### 2.3 应用拆分(apps/web → apps/account + apps/admin)

- `git mv apps/web apps/admin`,再从中**抽出**账户面到新建 `apps/account`:
  pages `auth/**`、`oauth/authorize`、`profile`,及其组件树、`useAuth`、
  `plugins/auth.client.ts`、auth 布局。
- 两应用**各自持有**所需 composables/types 副本,不建共享包:两侧认证模型自此
  不同(第一方 cookie vs RP/BFF),共享反而要写分支。KunUI 仍是共同依赖。
- 品牌配置:`kungal.ts` 退役;account 配置名「NextMoe·未萌 账号」、
  admin 配置名「NextMoe·未萌 管理台」,canonical 指各自域名,GitHub 指 next-moe 组织;
  产品清单里的产品名(鲲 Galgame 论坛等)**保留**——那是产品品牌。
- admin 全站 noindex;account 正常 SEO。
- 镜像:`infra-web` 退役(GHCR 保留,注释记录),新 `infra-account` / `infra-admin`
  (`PUBLIC_API_BASE` 分别烘焙各自域名)。CI matrix、openapi-types 门、根 package.json
  脚本、`scripts/dev.sh`、dev 端口(account=9420 承袭,admin=9421)随之更新。

### 2.4 后端与数据

- Go 代码零架构改动:issuer/FrontendURL 语义不变,只有 env **值**在切换时翻
  (`KUN_SITE_URL = KUN_FRONTEND_URL = https://account.nextmoe.com`)。
- cookie 更名 `kg_*` → `nm_*`(`nm_fed_state`、`nm_browser`);全员登出使更名免费。
- 邮件发件人显示名默认值 → 「NextMoe·未萌」;发件域切到 nextmoe.com
  (SPF/DKIM/DMARC 在 Cloudflare 配,新域送达率需观察)。
- 数据迁移(cmd/migrate,原地 UPDATE 保 site_id):OP 的 sites 行
  `oauth.kungal.com` → `account.nextmoe.com` + 更名;新增 admin site 行;
  auto-consent 域名单加 `admin.nextmoe.dev`。种子同步(全新安装走新形状)。
- **会话作废不进迁移代码**(迁移会被重跑):切换 runbook 里一次性
  `DELETE FROM sessions` 手工执行。

## 3. 明确不改(裁定过的边界)

| 项 | 理由 |
|----|------|
| `KUN_` env 前缀、Go module `api`、数据库名 `kun_galgame_infra` | 内部标识符,无用户可见面;改名与改值同波双倍切换风险(面板漏一个 env 就是静默 localhost 默认值)。可作后续纯机械波,不欠品牌债 |
| 下游产品域名与品牌(kungal.com、moyu.moe…) | 产品自己的身份,平台账户层改名不波及 |
| 图床 CDN 域名 | 独立轨 |
| `aud`/`site_id` 语义 | 下游校验以 site_id 为准且行内 UPDATE 保 id,不受域名改动影响 |

## 4. 切换 runbook(合并 = 部署 = 切换时刻)

infra 栈是 push→CI→自动 redeploy,**合并本 PR 的那一刻就是切换**,因此:

1. **合并前预置**:Cloudflare 加 `account.nextmoe.com`、`admin.nextmoe.dev` DNS
   (提前指向 Traefik,404 无害);Dokploy 预改 env(`KUN_SITE_URL`/`KUN_FRONTEND_URL`
   → account 域、CORS 列表、`KUN_FEDERATION_*` 四个、admin 应用的
   `NUXT_OAUTH_CLIENT_SECRET`——compose 用 `:?` 声明,不预置则部署直接失败,
   这是故意的:面板变量不经 compose 列出根本进不了容器,空 secret 会让登录
   换码静默失败);在旧控制台创建 `nextmoe-admin` client 行(confidential,
   redirect URI = `https://admin.nextmoe.dev/auth/callback`,scope
   `openid profile email`);Google/GitHub 控制台按
   `https://account.nextmoe.com/api/v1/auth/federation/{provider}/callback` 建应用;
   nextmoe.com 邮件 DNS(SPF/DKIM)+ 在邮件服务商(MXroute)创建 `auth@nextmoe.com`
   邮箱(compose 已切到该发件账号,SMTP host 不变;邮箱不存在则注册/找回邮件全断)。
   **Traefik 路由注意**:旧 web 服务的 host catch-all 在 Dokploy 域名面板;新
   account/admin 服务的 catch-all 已改为 compose labels 所有。切换时把面板上
   oauth.kungal.com 的域名条目删掉,且**永远不要**在面板上给 account/admin 添加
   域名——compose labels 会整体顶掉面板注入的 labels(2026-07 oauth 404 事故同族)。
2. **合并 PR** → CI 构建 `infra-account`/`infra-admin` → 自动部署新路由与新应用
   (deploy job 在镜像推完后才触发 Dokploy,无论坛 09-08 那种 webhook 抢跑竞态)。
   重部后旧 `web` 容器可能成为孤儿继续空转(compose 已无此服务,`up -d` 不带
   `--remove-orphans` 不会清):面板域名条目删掉后它无路由,手动
   `docker rm -f` 收掉即可。
3. **立即执行**:`go run ./cmd/migrate`(sites 行 UPDATE + admin site + 联邦
   `oauth_accounts` 两个复合唯一索引——#180 的迁移仍未跑,一并落);
   `DELETE FROM sessions;`(全员登出,含下游)。
4. **边缘收尾**:Cloudflare 308:`oauth.kungal.com/*`、`oauth.kungal.org/*` →
   `account.nextmoe.com/$1`(宽限 6-12 个月,兼顾书签与在野 native app 的
   authorize 跳转;native app 的 token POST 需 308 保动词)。
5. **配置中心**(admin.nextmoe.dev 重新登录后):`auth.federation_providers`
   → `["google","github"]`,联邦随切换上线。
6. **下游波**(逐仓,各自会话/属主执行):论坛、补丁、AI、表情包更新 OP 基址 env
   与「鲲 Galgame 账号」登录文案;developer 门户(独立 Dokploy 项目)改 OP env;
   native app(YukiHub 等)通知升级,宽限期内靠边缘 308 存活。
7. **验证**(我):discovery 文档 iss、账户面登录/注册 E2E、管理台 RP 全流程、
   联邦 E2E、每个下游站登录回归。

回滚:revert 合并即恢复旧路由与镜像(旧 DNS 宽限期内仍在);会话清空不可逆(D2 已接受)。
回滚期间旧栈 seed 会重新插入 `oauth.kungal.com` 站点行;再次切回时 migrate 自愈——
存在 `account.nextmoe.com` 正典行时直接删除重播出来的旧域行(`cmd/migrate/rebrand.go`),无需手工清理。

## 5. 执行波次

| 波 | 执行者 | 范围(写路径域) |
|----|--------|-----------------|
| W1 后端+部署面 | grok | `apps/api/**`、`docker-compose*.yml`、`.github/workflows/**`、`scripts/dev*.sh`、根 `package.json` |
| W2a 账户应用 | grok | `apps/account/**`(读 apps/admin 抽取) |
| W2b 管理台改造 | grok | `apps/admin/**`(去账户面 + RP/BFF + 品牌) |
| W3 文档清扫 | grok + 编排者 | `docs/**`、`apps/developer/docs/**`(43 文件 139 处 kungal 域引用);之后编排者跑 kungal-docs sync/audit 与 developer 生成物再生 |
| W4 切换 | 用户 + 编排者 | 上节 runbook |

所有闸门(build/vet/gofmt/typecheck/lint/测试套件/E2E)由编排者执行;git 与跨仓操作不派发。
