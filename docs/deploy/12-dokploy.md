# 12 · Dokploy 部署(单服务器 / 自托管 PaaS)

[Dokploy](https://dokploy.com/)([文档](https://docs.dokploy.com/docs/core/docker-compose)、[GitHub](https://github.com/Dokploy/dokploy))是开源自托管 PaaS。它**取代了手动边缘反代([09](./09-edge-caddy.md)/[10](./10-edge-nginx.md)/[11](./11-edge-cloudflare-tunnel.md)三选一)**:内置 **Traefik** 做反代 + **自动 Let's Encrypt SSL**,并额外提供 UI 编排、环境变量管理、实时日志/监控、备份。本套生态(三仓 compose + build + 服务名 s2s)与它的模型高度契合。

> 选了 Dokploy 就**不要再叠加** Caddy/nginx/Cloudflare Tunnel —— Traefik 已是它的反代。09-11 仅作"不用 Dokploy 时"的替代方案。

> **镜像从哪来**:生产**强烈建议用 CI 预构建镜像(GHCR)+ Dokploy 拉取,不要在生产机上 build**(本生态镜像重,会拖垮单服务器)—— 见 [13-registry-ci.md](./13-registry-ci.md)。本篇下文的"Git source + 在 Dokploy 上 build"是更简单的起步路径,跑通后建议切到 §13 的预构建流。

## 12.0 拓扑(单服务器)

```
                    Internet ──► :80/:443  ┌─────────── Traefik(Dokploy 内置)───────────┐
 DNS A 记录 → 服务器公网 IP                  │  按域名 + 路径自动路由(UI 配置,注入 labels) │
                                            └──────────────────────┬──────────────────────┘
                                          全部容器接入共享网络  dokploy-network
   ┌──────────────── infra compose app ─────────────────┐  ┌── kungal app ──┐  ┌── moyu app ──┐
   │ postgres redis minio opensearch(基础设施,仅内部) │  │ api  web       │  │ api  web      │
   │ oauth  image  catalog  account  admin  …          │  └────────────────┘  └───────────────┘
   └───────────────────────────────────────────────────┘   下游按服务名连枢纽:postgres / redis /
                                                            oauth:9277 / catalog:9281 / image:9278
```

- **3 个独立 Dokploy "Compose" 应用**(各对应一个 Git 仓库,Dokploy 克隆 + `build`):`nextmoe-infra`(infra)、`kun-galgame-forum`(kungal)、`kun-galgame-patch`(moyu)。
- **共享一个 `dokploy-network`**(external)。跨应用 s2s 只用枢纽的**唯一服务名**(`postgres`/`redis`/`oauth`/`catalog`/`image`)——这些名字全局唯一,在共享网络上可解析;各应用自己的 `api`/`web`/`migrate` 只在本应用内解析,不跨应用引用,因此**不存在名称冲突**(这点和手动反代文档里"web/api 别名跨仓冲突"是同一回事,Dokploy 用 Traefik router 区分对外路由,内部 s2s 只引用唯一名)。OpenSearch 不在这条共享网上:它只挂 project-scoped `search` 网络,仅 catalog 可达。
- **infra 仓额外挂一个独立 Compose 项目**承载 NextMoe 开发者门户(`developer.nextmoe.dev`):同一 Git 仓库、**两个** Dokploy Compose 应用——主栈 `docker-compose.prod.yml`(push→CI→**自动 redeploy webhook**)与门户 `docker-compose.developer.yml`(**手动部署,不挂 webhook**,发布节奏与主栈解耦)。门户是独立 Compose 项目,跨项目调 oauth 走主栈 oauth 服务上**专设的 compose 网络别名 `infra-oauth`**(主栈项目重建也不变;裸 `oauth` 同名别名跨项目会轮询,精确容器名则在项目重建时失效——别名两害皆免,见 [12.1](#121-域名--服务映射) 表下说明)。总计 Git 仓库 3 个、Dokploy Compose 应用 4 个。

> 为什么**不用伞状单 compose**:Dokploy 的 Compose 应用是"一个 Git 仓库 → 克隆并 build"。三仓是三个独立仓库;伞状 compose 要么需要 monorepo,要么走 Raw compose(那样必须预先 build+push 镜像到 registry)。**单服务器 + 三应用 + 共享网络**才是 Dokploy 的原生形态。

## 12.1 域名 → 服务映射

DNS 把下列域名的 A/AAAA 记录指向**服务器公网 IP**;Traefik 自动签发证书。**注意两种归属**:infra 主栈的 `account.nextmoe.com` / `admin.nextmoe.dev` 四条路由(下表前四行)由 **`docker-compose.prod.yml` 的 Traefik labels 所有**——**不要**在 Dokploy Domains 面板给这些服务添加域名(compose labels 会整体替换面板注入的 labels,面板条目静默失效——2026-07 oauth 404 教训);其余应用(developer 门户、kungal、moyu)照旧在各服务的 **Domains** 标签页添加(域名 + 路径 + 目标服务 + 容器内部端口),同一域名的 `/api*` 与 `/` 用两条记录(更具体的路径优先)。

| 公网域名 | 路径 | 所在 Dokploy 应用 | 目标服务:内部端口 |
|---|---|---|---|
| `account.nextmoe.com` | `/api/v1` | infra | `oauth:9277` |
| `account.nextmoe.com` | `/`(默认) | infra | `account:3000`(账户中心) |
| `admin.nextmoe.dev` | `/api/v1` | infra | `oauth:9277` |
| `admin.nextmoe.dev` | `/`(默认) | infra | `admin:3000`(管理台) |
| ~~`wiki.kungal.com`~~ | — | infra | **已退役(开放 API Phase 2 · W5,2026-07)**:两组 compose labels(`infra-wiki-api` / `infra-wiki-api-http`)已删、域 404,DNS 解析记录待用户删。galgame 富读改走 catalog internal 面(s2s,`nm_` key)。 |
| `developer.nextmoe.dev` | `/`(整站) | **infra-developer**(独立 Compose,手动部署,域名走本项目 **Domains 面板**) | `developer:3000` |
| `kungal.com` + `www.kungal.com` | `/api` | kungal | `kungal-api:2334` |
| `kungal.com` + `www.kungal.com` | `/`(默认) | kungal | `web:7777` |
| `moyu.moe` + `www.moyu.moe` | `/api/v1` | moyu | `moyu-api:5214` |
| `moyu.moe` + `www.moyu.moe` | `/`(默认) | moyu | `web:3000` |
| `image.kungal.iloveren.link` | `/` | —(见下) | Cloudflare **R2 自定义域**直供 / 或回源 `minio:9000` |

- **`kungal.com` / `moyu.moe` 顶级域 + `www` 子域**:两个都加同样的两条路径记录,指向同一对 `api`/`web`。需要 apex↔www 收敛时,可在 Dokploy/Traefik 加一条 301(否则两域并存即可)。
- **`image.kungal.iloveren.link`**:生产 `.env` 用的是 **Cloudflare R2**(`KUN_IMAGE_S3_ENDPOINT=...r2.cloudflarestorage.com`),所以这个域名是 **R2 的自定义域,由 Cloudflare 直接服务图片 blob,不经服务器/Traefik**。只有在"自托管 MinIO 存图"时才需要在 Dokploy 给它挂域名回源 `minio:9000`(重写到 `/kun-images` bucket)。
- **`image` 服务(`:9278`)是 s2s 内部服务**(下游 api 上传时调用),**不对外开域名**。
- **`developer.nextmoe.dev`(NextMoe 开发者门户)**:由 infra 仓的**第二个**、**独立**的 Dokploy Compose 项目承载(compose 路径 `docker-compose.developer.yml`,与主栈 `docker-compose.prod.yml` 分开),**手动部署**——**不挂** push→CI→自动 redeploy webhook,发布节奏与主栈解耦。它是**同源 Nuxt 壳**:浏览器只访问本域,Nitro 的 `/api/**` relay 在**服务端**转发到 oauth,**零 CORS**。其域名由**本项目的 Dokploy Domains 面板**管理——单服务整站项目的 Dokploy 原生形态(与 kungal/moyu 相同);这能成立的前提是该服务 compose 里**没有任何 labels 块**(compose labels 会整体替换面板注入——主栈 oauth 404 教训),**绝不能加回 labels**,否则下次部署域名静默失效。因是**独立 Compose 项目**,它调 oauth 走主栈 oauth 服务上专设的 compose 网络别名 **`http://infra-oauth:9277`**(`docker-compose.prod.yml` oauth 块 `networks 的 `dokploy-network` 键 aliases——别名必须挂在 Dokploy 部署时注入的同名键上,挂 `default` 键会被注入的无别名键顶掉`):主栈项目重建时容器名前缀会变而 compose 别名不变,且名字足够独特不会被其他项目的同名服务在共享网上轮询劫持。注意:**面板注入只在 Dokploy 的 Deploy 动作时生效**,服务器上裸跑 `docker compose up` 不会带域名 labels。

## 12.2 接入改造清单(从当前 host-port 部署 → Dokploy)

当前栈用 `ports: ["1xxxx:..."]` 暴露宿主端口 + 前端把浏览器 URL 烤成 `localhost:1xxxx`。Dokploy/Traefik 走容器内部端口路由,需做以下调整(都是**编排/配置层**,不动业务代码):

> **下面 A/B/C/D/E 列的值,三仓 `docker-compose.prod.yml` 已替你写死(非密钥/域名)**;真正要你填的只剩**各应用 Dokploy Environment 面板里的几个密钥**(`POSTGRES_PASSWORD`/`JWT_SECRET`/`OAUTH_CLIENT_SECRET`/S3 keys…)。逐个面板清单见 [15-environment §15.8](./15-environment.md) 与 [17-go-live-checklist.md](./17-go-live-checklist.md)。C/D/E 仅作"哪个值是什么"的参考。

**A. compose 网络**
- 各仓 compose 的网络从 `kun-galgame-infra_default`(external)改为 **`dokploy-network`**(external):
  ```yaml
  networks:
    default:
      name: dokploy-network
      external: true
  ```
- infra 的基础设施(`postgres`/`redis`/`minio`)与对外服务都要在此网络上(默认 `default` 即可)。OpenSearch 只挂 compose 的 `search` 网络,不进 `dokploy-network`。
- Dokploy 若开启"isolated network",要确保需要 s2s 的服务显式接入 `dokploy-network`,否则跨应用解析不到枢纽服务。

**B. `ports` → `expose`**
- 删除/改写所有 `ports: ["1xxxx:yyyy"]`,对外服务改用 `expose: ["yyyy"]`(只在容器网络内开放,Traefik 内部回源)。基础设施(pg/redis/minio/opensearch)连 `expose` 都不需要,纯内部即可。**生产不再有 1xxxx 宿主端口。**

**C. 前端浏览器侧 URL → 真实域名**(构建期 build args / 运行期 env;SSR 内部 base 维持服务名不变,见 [双 base 说明](#125-双-base-与-ssr))
- **infra account**(compose build args):`PUBLIC_API_BASE=https://account.nextmoe.com/api/v1`、`PUBLIC_IMAGE_CDN_BASE=https://image.kungal.iloveren.link`
- **infra admin**(compose build args):`PUBLIC_API_BASE=https://admin.nextmoe.dev/api/v1`、`PUBLIC_IMAGE_CDN_BASE=https://image.kungal.iloveren.link`
- ~~**infra wiki**(build args)~~ — **已退役(开放 API Phase 2 · W5,2026-07)**:wiki 前端(`apps/wiki`)与 `wiki.kungal.com` 域退役,`infra-wiki` 镜像不再构建。
- **kungal web**(`docker/web.env`):`NUXT_PUBLIC_API_BASE_URL=https://www.kungal.com`、`NUXT_PUBLIC_OAUTH_SERVER_URL=https://account.nextmoe.com/api/v1`、`NUXT_PUBLIC_OAUTH_FRONTEND_URL=https://account.nextmoe.com`、`NUXT_PUBLIC_OAUTH_REDIRECT_URI=https://www.kungal.com/auth/callback`、`NUXT_PUBLIC_KUN_GALGAME_URL=https://www.kungal.com`（死配置 `NUXT_PUBLIC_GALGAME_WIKI_URL` 已于 W5 删除）
- **moyu web**(`docker/web.env`):`NUXT_PUBLIC_API_BASE=https://www.moyu.moe/api/v1`、`NUXT_PUBLIC_OAUTH_SERVER_URL=https://account.nextmoe.com/api/v1`、`NUXT_PUBLIC_OAUTH_WEB_URL=https://account.nextmoe.com`、`NUXT_PUBLIC_OAUTH_REDIRECT_URI=https://www.moyu.moe/auth/callback`

**D. 后端 CORS 允许源 → 真实域名**
- infra `docker/oauth.env`、`docker/galgame.env`、`docker/image.env` 的 `KUN_FRONTEND_CORS_ORIGIN`:列出 `https://account.nextmoe.com,https://www.kungal.com,https://kungal.com,https://www.moyu.moe,https://moyu.moe`(`https://wiki.kungal.com` 已于 W5 从 CORS 白名单移除)
- kungal `docker/api.env` `CORS_ALLOW_ORIGINS=https://www.kungal.com,https://kungal.com`
- moyu `docker/api.env` `CORS_ALLOW_ORIGINS=https://www.moyu.moe,https://moyu.moe`

**E. 后端→后端 / 图床 base(env,用服务名,不变或确认)**
- 下游 api.env 的 `OAUTH_SERVER_URL=http://oauth:9277/api/v1`、`KUN_NEXTMOE_API_BASE=http://catalog:9281` + `KUN_NEXTMOE_API_KEY=<nm_ internal-tier key>`(galgame 富读走 catalog internal 面,客户端拼 `/internal`;旧名 `*_GALGAME_WIKI_BASE_URL` + legacy `/api` 读面已于 W5 退役,key 硬依赖)、`KUN_IMAGE_*CLIENT_BASE_URL=http://image:9278`(s2s 走服务名,**保持容器内部地址**)。
- 各服务的 `KUN_IMAGE_PUBLIC_BASE_URL=https://image.kungal.iloveren.link`(后端生成给前端的图片 URL,用公网 CDN 域)。

**F. 转发头**:Traefik 默认带 `X-Forwarded-Proto/Host`,SSR 绝对 URL、OAuth 跳转、`Secure` cookie 正确,无需手动配置。

## 12.3 OAuth client(数据库)

OAuth client 的 `redirect_uris` 存在枢纽 `kun_galgame_infra.oauth_clients` 表里(`id` 列即 client_id),上线必须改成 https 域名:

| client(name / id) | redirect_uris 加入 |
|---|---|
| 论坛 `4ed9bc99…` | `https://www.kungal.com/auth/callback`、`https://kungal.com/auth/callback` |
| 补丁 `df3ff60…` | `https://www.moyu.moe/auth/callback`、`https://moyu.moe/auth/callback` |
| ~~wiki 前端 PKCE client~~ | **已退役(W5)**:wiki 前端 + `wiki.kungal.com` 域退役,不再需要该 redirect_uri |

> **wiki 前端已退役(开放 API Phase 2 · W5)**,其 PKCE client(`53e9b5ea…`)已于 2026-07-22 从生产删除(U2 清账:零外键、零凭证使用、唯一陈年 session 一并删;image GC 纯由 refping 驱动,与 oauth client 行无关)。⚠️ **铁律:`galgame-wiki-admin`(图片上传身份)与其锚定的 sites 行 4(wiki.kungal.com)有意保留**——那是 ~16 万图字节的存储身份链,永不删。改完各站 redirect_uris 后,OAuth 的 `KUN_SITE_URL`/`KUN_FRONTEND_URL`(oauth.env)也改成 `https://account.nextmoe.com` / `https://account.nextmoe.com`。

## 12.4 部署步骤(Dokploy)

1. **装 Dokploy**(目标服务器):`curl -sSL https://dokploy.com/install.sh | sh`([安装文档](https://docs.dokploy.com/docs/core/manual-installation))。
2. **DNS**:把 12.1 所有域名 A 记录指向服务器公网 IP(`image.kungal.iloveren.link` 走 R2 则指 Cloudflare,不指本机)。
3. **建 3 个 Compose 应用**。两种来源:**(推荐·生产)** 指向各仓 `docker-compose.prod.yml`(用 `image:` 引用 GHCR 预构建镜像,见 [13-registry-ci.md](./13-registry-ci.md));**(起步)** 直接 Git source + 在 Dokploy 上 build(简单,但重镜像有拖垮单机风险)。**开发者门户额外建第 4 个、独立** Compose 应用(同 infra 仓,compose 路径 `docker-compose.developer.yml`,**手动部署、不挂 webhook**,见 [12.1](#121-域名--服务映射)),按需单独触发。
4. **填环境变量**:prod compose 已内联非密钥/域名;**只需在各应用 Dokploy Environment 面板填密钥**(逐个清单见 [15-environment §15.8](./15-environment.md) / [17-go-live-checklist.md](./17-go-live-checklist.md));**全部轮换测试值**(见 [05-configuration.md](./05-configuration.md))。
5. **部署顺序**:先部署 **infra**(等 `postgres`/`redis`/`minio`/`opensearch` healthy)→ 在 Dokploy **Terminal/Run** 跑首启迁移(见 12.6)→ 再部署 **kungal**、**moyu**。
6. **配域名**:每个应用的对外服务在 **Domains** 标签按 12.1 添加(含 `/api*` 与 `/` 两条),Dokploy 自动注入 Traefik labels + 签发证书。
7. **验证**:`curl -I https://account.nextmoe.com`(302→登录,有效证书)、`https://admin.nextmoe.dev`、`https://www.moyu.moe`、`https://www.kungal.com`。(`wiki.kungal.com` 已于 W5 退役,现返 404。)

## 12.5 双 base 与 SSR

前端已实现**双 base**(见 `docs`/各仓 nuxt.config):**SSR 在容器内用服务名**(`http://kungal-api:2334`、`http://oauth:9277/api/v1`、`http://catalog:9281/api`),**浏览器用 12.1 的公网域名**。Dokploy 下这套**正好需要**——没有宿主端口,SSR 必须走内部服务名,浏览器走 Traefik 域名。所以 12.2-C 只改"浏览器侧"URL,SSR 的 `NUXT_API_BASE_SSR` / `NUXT_AUTH_API_BASE_SSR` / kungal 的 `NUXT_API_BASE_URL` 维持容器内部地址不变。

## 12.6 数据迁移(首次切换)

跨仓迁移 pipeline(reset → 建库 → migrate-users → galgame-data → …)是一次性切换,在 Dokploy 的 **Terminal** 里对 infra 的 postgres / 各 `migrate` 容器执行,顺序见 [03-bootstrap.md](./03-bootstrap.md) 与 `docs/migration/`。注意 Dokploy 下连库用**容器网络服务名**(`postgres:5432`),不再是宿主 `localhost:1xxxx`。

## 12.7 注意 / 取舍

- **宿主端口随机问题**:Dokploy 下若仍用 `ports` 而不绑定固定值,重部署后宿主端口会变、打断外部连接([官方提示](https://docs.dokploy.com/docs/core/docker-compose));本套已全部走 Traefik 内部路由,**不要再发布 host 端口**。
- **数据库托管**:`postgres`/`redis` 可继续留在 infra compose,或迁到 **Dokploy 原生数据库**(带备份/UI);`minio`/`opensearch` 无原生支持,留 compose。
- **证书**:由 Traefik/Dokploy 管理与持久化,无需手动 certbot。
- **入站端口**:服务器需公网可达 80/443;若在 NAT/无法开入站,改回 [11-cloudflare-tunnel.md](./11-edge-cloudflare-tunnel.md) 方案(此时不用 Dokploy 的 Traefik 对外)。
- **dae**:生产服务器保持纯净,**不要叠加 dae**(见 [08-dae-dev-proxy.md](./08-dae-dev-proxy.md))。
