# 17 · 上线 Checklist(三仓生态 → 三站上线)

> 你在这里:Dokploy 已装好、`counter.kungal.com` 测试应用正常 → 平台层(Dokploy/Traefik/DNS/SSL/`dokploy-network`)已验证。
> 本篇是**边做边打勾**的上线清单,标注**每一步在哪配什么环境变量**。原理见
> [12-dokploy](./12-dokploy.md) / [13-registry-ci](./13-registry-ci.md) / [15-environment](./15-environment.md) / [16-data-cutover](./16-data-cutover.md)。
>
> **只有一个配置落点**:三仓 `docker-compose.prod.yml` 已把**非密钥/域名写死在 `environment:`**,
> **密钥用 `${VAR}` 从各应用的 Dokploy Environment 面板取**。所以**不用在服务器放任何 `docker/*.env`**——
> 每个应用面板里只填下面列的那几个密钥即可。(`docker/*.env` 只剩**本地 dev** 用。)

---

## 0 · 一次性生成密钥(后面复用,先备齐)

> **所有面板密钥都只用「字母数字(+ `-` `_`)」,不要含 `$ # % : / @ ? & 空格 引号`。**
> 原因:面板值经 Dokploy 写成 `.env` → docker compose 解析 `${VAR}`,`$` 会被当变量替换、`#` 会被当注释截断 →
> **密钥被悄悄改短/改坏**(典型:密钥里带 `$#` → compose `${VAR}` 解析后只剩几字节)。
> 最省心:**`openssl rand -hex 32`**(64 位十六进制,纯 0-9a-f,绝不会被吃,且远超 16 字节;`POSTGRES_PASSWORD` 用它也天然 URL-safe)。

- [ ] `POSTGRES_PASSWORD` = `openssl rand -hex 32`(三仓面板填**同一个值**;务必字母数字,它还要被拼进 `KUN_DATABASE_URL`)
- [ ] `JWT_SECRET`(infra)= 强随机(infra 面板;oauth/image/galgame 共用)
- [ ] `JWT_SECRET`(kungal)= 强随机(kungal 面板;**与 infra 不同**,只是同名变量)
- [ ] `MINIO_ROOT_USER` / `MINIO_ROOT_PASSWORD`(用 R2 则填占位即可)
- [ ] **Cloudflare R2**:endpoint、Access Key、Secret(、bucket)
- [ ] **B2**:moyu 补丁文件一套 key(**必填**,补丁站核心;moyu prod compose 已 fail-fast);kungal 工具集一套 key(**可选**,不填则 toolset 上传端点 500)
- [ ] **SMTP** 密码(可选)
- [ ] OAuth client secret ×3 —— **Phase 2 注册时生成**,先留空

> **一致性铁律**(配错→能起但 401/403/连不上,见 [15-environment §15.3](./15-environment.md)):
> - 三仓面板的 `POSTGRES_PASSWORD` 填**同一个值**(infra postgres 密码 = 下游 DSN 密码)
> - 每个 `OAUTH_CLIENT_SECRET` = 注册该 client 时枢纽生成的明文
> - infra 内部 oauth/image/galgame 读同一 `${JWT_SECRET}`(YAML anchor),自动一致;kungal 的 `JWT_SECRET` 是它自己的,无需匹配 infra;moyu **没有** JWT_SECRET
> - 图床 CDN 域 `https://image.kungal.iloveren.link` 已写死在三仓 prod compose,天然一致

---

## Phase 0 · 前置(动 Dokploy 应用之前)

- [ ] **镜像上 GHCR**:三仓 push 到 main → CI build+push `ghcr.io/next-moe/*`(含 `*-tools`)。→ [13-registry-ci](./13-registry-ci.md)
- [ ] **GHCR 包设 Public**(或给 Dokploy 配 registry 凭证)。
- [ ] **(可选)GitHub repo Secrets**:`DOKPLOY_WEBHOOK_INFRA` / `_KUNGAL` / `_MOYU`。
- [ ] **DNS A 记录 → 服务器公网 IP**:`account.nextmoe.com`、`admin.nextmoe.dev`、`kungal.com`+`www`、`moyu.moe`+`www`。(`wiki.kungal.com` 已于开放 API Phase 2 · W5 退役,解析记录待删。)
- [ ] **DNS**:`image.kungal.iloveren.link` → **Cloudflare R2 自定义域**(不指服务器)。
- [ ] **定方向**:空库验证(Phase 2A)/ 带生产数据(Phase 2B)。**建议先空库跑通,再做数据 cutover**。

---

## Phase 1 · 起枢纽 infra(地基)

- [ ] Dokploy 建 **Compose 应用 `infra`** → 指向 `nextmoe-infra/docker-compose.prod.yml`,网络 `dokploy-network`。
- [ ] **infra 应用 Environment 面板**填(其余非密钥/域名 prod compose 已写死):
```env
# ── 必填(留空 → docker compose 直接报错,不启动)──
POSTGRES_PASSWORD=<强随机>              # PG 超级用户密码;= 下游 KUN_DATABASE_URL 里的密码。必须 URL-safe(字母数字)
JWT_SECRET=<强随机>                     # oauth 签发 / image·galgame 验签 access_token,三者共用同一个
MINIO_ROOT_USER=<自定义>                # MinIO 管理员名(用 R2 也要填,可填占位如 minioadmin)
MINIO_ROOT_PASSWORD=<强随机>            # MinIO 管理员密码
KUN_IMAGE_S3_ENDPOINT=<R2 端点>         # 图床后端;R2=https://<acct>.r2.cloudflarestorage.com,自托管 MinIO=http://minio:9000
KUN_IMAGE_S3_ACCESS_KEY=<R2 access key> # 图床 S3 访问密钥(空 → image 服务启动即退出)
KUN_IMAGE_S3_SECRET_KEY=<R2 secret key> # 图床 S3 私钥

# ── 可选(有默认值,按需覆盖)──
KUN_IMAGE_S3_REGION=auto               # 默认 auto(R2)。AWS/MinIO 填区域,如 us-east-1
KUN_IMAGE_S3_BUCKET=kun-images         # 默认 kun-images。改成你的桶名
KUN_IMAGE_S3_FORCE_PATH_STYLE=false    # 默认 false(R2)。自托管 MinIO 必须改 true
KUN_VISUAL_NOVEL_EMAIL_PASSWORD=<SMTP 密码>   # 默认空。不填则注册验证码/找回密码邮件发不出
POSTGRES_USER=postgres                 # 默认 postgres,一般不改
```
> infra web/wiki 前端域名是 **CI 构建期烤进镜像**的(build.yml),**部署时无需配**;要改改 build.yml 重构。
- [ ] **部署 infra**,确认 `postgres` / `redis` / `minio` / `opensearch` 全部 **`(healthy)`**(都已配 healthcheck)。oauth/image/galgame/web/wiki 此时可能因空库/未注册 client 未完全就绪,正常。
  > **怎么看 healthy?** Dokploy 应用页能看到每个服务的容器状态和日志;最直接是在 Dokploy 应用的 **Terminal**(或 SSH 到服务器)跑:
  > ```bash
  > docker compose -f docker-compose.prod.yml ps   # STATUS 列:(healthy) / (unhealthy) / (health: starting)
  > ```

---

## Phase 2 · 数据 / Schema + OAuth client

> 一次性 job 已内联 environment,无需 env 文件(`tools` 在 jobs profile;`migrate*` 部署时自动跑,也可手动 run):
> `docker compose -f docker-compose.prod.yml [--profile jobs] run --rm <migrate|migrate-catalog|tools> …`

### 2A · 空库(先验证管线)
- [ ] `docker compose -f docker-compose.prod.yml run --rm migrate`(infra schema + 种子)
- [ ] `docker compose -f docker-compose.prod.yml run --rm migrate-catalog`(wiki 两族 + catalog schema,W5 单一入口)→ [03-bootstrap §A](./03-bootstrap.md)

### 2B · 带生产数据(正式上线)—— 实际放到 Phase 3/3′ 之后做
> 16 的流水线**交错跑 infra + kungal + moyu 三家工具**,需要 kungal/moyu **已部署**(`$FORUM`/`$PATCH` 目录 + `.env` 存在)才能跑 `$KUNGAL`/`$MOYU` 那几步。
> 所以顺序是:**先 2A → 2.1 → Phase 3/3′ 把三站(空库)跑通,再回这里做 cutover**。
> 注意 cutover 的 16.4 会 `DROP` `kun_galgame_infra` → **抹掉 2.1 注册的 OAuth client**;cutover 后需**重新注册这 3 个 client**(client_id 固定不变)并把新 secret 更新进下游面板重部署。
> (想只注册一次:Phase 3/3′ 部署 kungal/moyu 时先填**占位** `OAUTH_CLIENT_SECRET` 把目录/`.env` 建出来 → 跑完 cutover → 再注册真 client、回填 secret 重部署。)
- [ ] `scp` 两个 dump → 还原 → 整条迁移流水线(`--profile jobs run --rm tools <job>`,严格按序、含 dry-run/校验)。→ [16-data-cutover](./16-data-cutover.md)

### 2.1 注册 3 个 OAuth client(**不做登录全废**)→ [12-dokploy §12.3](./12-dokploy.md) / [03-bootstrap §A.5](./03-bootstrap.md)
- [ ] 论坛 client `4ed9bc99ec0a789a4796b83e22bd84c5` → redirect_uris 加 `https://www.kungal.com/auth/callback`、`https://kungal.com/auth/callback`
- [ ] 补丁 client `df3ff6008d740bfacbe46aa8cf483cf2` → redirect_uris 加 `https://www.moyu.moe/auth/callback`、`https://moyu.moe/auth/callback`
- [ ] ~~wiki client redirect_uri~~ — **已退役(W5)**:wiki 前端 + `wiki.kungal.com` 域退役,无需此 redirect_uri(⚠️ 铁律:承载图片上传身份的 client 保留;两同名「鲲 Galgame Wiki」client 的存废清理属 C4,待用户裁)
- [ ] **记下每个生成的明文 secret** → 填到下游应用面板的 `OAUTH_CLIENT_SECRET`(Phase 3)。

---

## Phase 3 · 起 forum(kungal)

- [ ] Dokploy 建 **Compose 应用 `kungal`** → `kun-galgame-forum/docker-compose.prod.yml`,网络 `dokploy-network`。
- [ ] **kungal 应用 Environment 面板**填:
```env
POSTGRES_PASSWORD=<= infra 同名值>
OAUTH_CLIENT_SECRET=<注册论坛 client 的明文>
JWT_SECRET=<强随机>                 # kungal 自己的会话密钥(不必=infra)
# 可选:KUN_IMAGE_CLIENT_ID / KUN_IMAGE_CLIENT_SECRET(直传封面)
# 可选:FILE_STORAGE_*(B2 工具集)、MAIL_*(发信)、S3_*(内联图床)
```
> 域名、OAuth client_id、服务名 base、CDN 域均已写死在 prod compose;web 前端 `NUXT_PUBLIC_*` 也在 compose 里(改域名改那里重部署)。
- [ ] 部署 kungal,等 `kungal-api`(healthy)→ `web`(healthy)。

---

## Phase 3′ · 起 moyu(patch)

- [ ] Dokploy 建 **Compose 应用 `moyu`** → `kun-galgame-patch/docker-compose.prod.yml`,网络 `dokploy-network`。
- [ ] **moyu 应用 Environment 面板**填:
```env
POSTGRES_PASSWORD=<= infra 同名值>
OAUTH_CLIENT_SECRET=<注册补丁 client 的明文>
KUN_VISUAL_NOVEL_S3_STORAGE_ACCESS_KEY_ID=<B2 key>        # 必填(补丁站核心;空则 compose 报错不启动)
KUN_VISUAL_NOVEL_S3_STORAGE_SECRET_ACCESS_KEY=<B2 secret> # 必填
# 可选:KUN_VISUAL_NOVEL_EMAIL_PASSWORD、KUN_IMAGE_OAUTH_CLIENT_ID/SECRET
```
> moyu **没有 JWT_SECRET**(不透明会话)。B2 endpoint/region/bucket/url、CDN 域、域名、client_id 均已写死在 prod compose;前端图床域是 `moyu-moe.ts` 常量(已对齐)。
- [ ] 部署 moyu,等 `moyu-api`(healthy)→ `web`(healthy)。

---

## Phase 4 · 域名 + Cloudflare + 验收

### 4.1 Dokploy Domains(每个应用对外服务加「域名+路径→服务:端口」,`/api*` 与 `/` 各一条)→ [12-dokploy §12.1](./12-dokploy.md)
- [ ] infra:`account.nextmoe.com` `/api/v1`→`oauth:9277`、`/`→`account:3000`；`admin.nextmoe.dev` `/api/v1`→`oauth:9277`、`/`→`admin:3000`
- [ ] ~~infra:`wiki.kungal.com`~~ — **已退役(W5)**:两组 compose labels 已删、域 404,DNS 待删;galgame 富读走 catalog internal 面(s2s,`nm_` key)
- [ ] kungal:`kungal.com`+`www` `/api`→`kungal-api:2334`、`/`→`web:7777`
- [ ] moyu:`moyu.moe`+`www` `/api/v1`→`moyu-api:5214`、`/`→`web:3000`
> 服务名是 **`kungal-api` / `moyu-api`**(不是 `api`)——Dokploy 不应用 compose 的 `networks.aliases`,只注册服务名;两仓都叫 `api` 会 DNS 冲突,导致 SSR(刷新页面)拉不到数据。所以服务名直接用唯一名。

### 4.2 Cloudflare → [15-environment §15.9](./15-environment.md) + [NOTES.md](./NOTES.md)
- [ ] 各域名开**橙云代理(Proxied)**
- [ ] R2:bucket + 自定义域 `image.kungal.iloveren.link`(对应 infra 面板的 `KUN_IMAGE_S3_*`)
- [ ] **源站锁定**:ufw 只放行 Cloudflare IP 段 + SSH
- [ ] **不开 Cloudflare Tunnel**(高并发 Error 1033)

### 4.3 验收
- [ ] `curl -I https://account.nextmoe.com https://admin.nextmoe.dev https://www.kungal.com https://www.moyu.moe`(有效证书 + 200/302;`wiki.kungal.com` 已退役,现返 404)
- [ ] 烟雾测试:注册/登录(OAuth 跳转回各站)、发帖/评论、传图(进 R2)、galgame 搜索出结果(forum/moyu)、补丁下载
- [ ] `docker ps` 看各应用容器全 healthy

---

## 上线后
- [ ] 配 Postgres 定时备份(`pg_dumpall` cron + 异地)→ [14-backup-restore](./14-backup-restore.md)
- [ ] VNDB 增量同步做成 cron(`--profile jobs run --rm tools sync-vndb -tagmap docs/tagMap.ts`)
- [ ] 复核没有遗留测试密钥(`191007` / `kun-docker-test-*` / `minioadmin`)→ [15-environment §15.10](./15-environment.md)

---

**相关**:[QUICKSTART.md](./QUICKSTART.md) · [12-dokploy](./12-dokploy.md) · [13-registry-ci](./13-registry-ci.md) · [15-environment](./15-environment.md)(变量全集) · [16-data-cutover](./16-data-cutover.md) · [NOTES.md](./NOTES.md)
