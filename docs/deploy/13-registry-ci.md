# 13 · 镜像 Registry + CI 构建(GHCR + GitHub Actions)

> 配合 [12-dokploy.md](./12-dokploy.md):**12 讲"怎么部署/路由"(Dokploy + Traefik + 域名),本篇讲"镜像从哪来"**。两者互补——Dokploy 负责拉镜像 + 反代 + 零宕机,GHCR + CI 负责在**别处**把镜像 build 好。

## 13.0 原则:不要在生产服务器上构建

Dokploy 官方明确:在部署服务器上 build "**可能导致服务器超时甚至冻结,所有应用宕机**"(Docker build 吃大量 RAM/CPU)。本生态镜像偏重——**oauth/image 是 cgo + libwebp**,每个前端是 **Nuxt 全量 build**,三仓合计 **16 个镜像**(含 3 个 `*-tools`)。在一台生产机上全 build = 单点风险。

**最佳实践(业界 + Dokploy 一致)**:
```
源码 push ─► CI(GitHub Actions)build 镜像 ─► 推送 registry(GHCR)
                                                   │ 触发 webhook / API
                                                   ▼
                            单服务器 Dokploy ─► 拉预构建镜像 ─► Traefik 零宕机滚动
```
生产机**零构建负载**,且天然带 **tag/回滚**。

## 13.1 选型结论

| 方案 | 何时用 | 评价 |
|---|---|---|
| **GHCR(GitHub Container Registry)**(首选) | 你们当前(已在 GitHub、单服务器) | 免费、原生集成 Actions(`GITHUB_TOKEN` 直接推)、**公开仓库镜像可设公开 → Dokploy 免凭证拉**、零额外基础设施 |
| 自托管 `distribution/registry:2` | 需私有 + 全自托管,且有**独立构建机/CI** | 轻量(单容器),但要自己加 **TLS + 认证 + GC**;若 build 仍在同一台生产机则**白搭**(没卸掉构建负载) |
| 自托管 **Harbor** | 多节点 / 需 RBAC、漏洞扫描、镜像签名、复制 | 功能全(CNCF 毕业),但**重**(core/db/redis/jobservice/registry/trivy 多容器),单台生产机不划算 |
| Dokploy **Build Server** | 不想用 GitHub Actions、想全自托管构建 | 独立 build VPS → 推 registry → 部署机拉(官方:"用 build server 时 registry 必需") |

**决定:现在上 GHCR + GitHub Actions;自托管 registry / Harbor 留到"多节点或需私有+扫描"时再说。**

## 13.2 镜像清单

CI 按各仓**现有 Dockerfile**(参数化)构建以下镜像并推到 `ghcr.io/next-moe/<name>`(GHCR 名必须小写):

| 镜像 `ghcr.io/next-moe/…` | 仓库 | Dockerfile | 关键 build-arg | 容器端口 |
|---|---|---|---|---|
| `infra-oauth` | infra | `docker/cgo.Dockerfile` | `CMD=oauth` | 9277 |
| `infra-image` | infra | `docker/cgo.Dockerfile` | `CMD=image` | 9278 |
| `infra-catalog` | infra | `docker/go.Dockerfile` | `CMD=catalog` | 9281(含 galgame-wiki 面;`infra-galgame`/9280 已随 wiki 退役 W3/W5 移除) |
| `infra-account` | infra | `docker/nuxt.Dockerfile` | `APP=account` | 3000 |
| `infra-admin` | infra | `docker/nuxt.Dockerfile` | `APP=admin` | 3000 |
| `infra-migrate` | infra | `docker/go.Dockerfile` | `CMD=migrate` | —(一次性;**所有库的唯一迁移镜像**,目标由参数给:裸跑=主库,`catalog`/`community`/`trust`/`ai`/`news`=对应库。原先每域一个的 `infra-migrate-catalog`/`-community`/`-trust`/`-ai`/`-news` 已合并进来,不再构建) |
| `infra-tools` | infra | `docker/tools.Dockerfile` | —(打包全部 `cmd/*`) | —(一次性) |
| `kungal-api` | nuxt4 | `docker/go.Dockerfile` | `CMD=server` | 2334 |
| `kungal-web` | nuxt4 | `docker/nuxt.Dockerfile` | —(单一 app,无 `APP`) | 7777 |
| `kungal-migrate` | nuxt4 | `docker/go.Dockerfile` | `CMD=migrate` | —(一次性) |
| `kungal-tools` | nuxt4 | `docker/tools.Dockerfile` | —(打包全部 `cmd/*`) | —(一次性) |
| `moyu-api` | patch-next | `docker/go.Dockerfile` | `CMD=server` | 5214 |
| `moyu-web` | patch-next | `docker/nuxt.Dockerfile` | `APP=web` | 3000 |
| `moyu-migrate` | patch-next | `docker/go.Dockerfile` | `CMD=migrate` | —(一次性) |
| `moyu-tools` | patch-next | `docker/tools.Dockerfile` | —(打包全部 `cmd/*`) | —(一次性) |

> 基础设施 `postgres`/`redis`/`minio` 用上游官方镜像,不进 CI。OpenSearch 用本仓 `docker/opensearch` 构建的 `infra-opensearch` 镜像。
>
> **`*-tools` 镜像**:把该仓 `apps/api/cmd/*` 的**每个**二进制打进一个镜像(infra 用 cgo+libwebp 编,
> 下游纯 Go)。`migrate`(及其 `catalog` 等目标)只够空库起服务;完整数据 cutover([03-bootstrap §B](./03-bootstrap.md))
> 需要 `migrate-users`/`migrate-galgame-data`/`migrate-moyu-galgame`/`dedup-galgame-alias`/`reindex-search` 等,
> 而一镜像只含一个 `CMD` 二进制,**不能** `--entrypoint` 复用。`tools` 已是各仓 prod compose 的
> jobs-profile 服务(environment 内联),按名跑一次性 job:
> ```bash
> docker compose -f docker-compose.prod.yml --profile jobs run --rm tools reindex-search -tagmap docs/tagMap.ts
> ```

## 13.3 Tag 与回滚约定

每次构建打**两个 tag**:
- **`:<git-sha>`** —— 不可变,精确回滚锚点。
- **`:latest`**(或 `:prod`)—— 移动标签,Dokploy 监听并拉取。

**回滚** = 把 Dokploy 的镜像引用从 `:latest` 临时改成某个已知良好的 `:<git-sha>` 再 redeploy(或在 prod compose 里 pin sha)。

## 13.4 GitHub Actions workflow

每仓放一个 `.github/workflows/build.yml`。下面是 **infra(最复杂,cgo + 2×Nuxt + Go)** 的完整示例;kungal/moyu **同构**,仅 `matrix` 列表不同。

> **省额度:infra 的实际 workflow 已改为「路径过滤 + 动态 matrix」**(下面这段是说明结构的简化示例,不是逐字现状)。GitHub 按 job 数×分钟计费且每 job 向上取整到 1 分钟,全量 matrix 即使全缓存每次 push 也要 ~10 分钟。现状:`changes` job 用 `dorny/paths-filter` 算出哪些组变了,只构建变更的镜像 —— `go`(oauth/image/artifact/catalog/community/trust/ai + 单一 `migrate`,与服务同 sha 锁步 ← `apps/api/**`)、`account`(← `apps/account/**`+根 manifest)、`admin`(← `apps/admin/**`+根 manifest)、`developer`(← `apps/developer/**`)、`tools`(`infra-tools`,与 go 组同源锁步 ← `apps/api/**` + `docker/tools.Dockerfile`)。docs-only 的 push 不构建任何镜像(~1 分钟)。`tools` 是唯一不触发 Dokploy redeploy 的组;要单独重建它,Actions → Run workflow → `scope=tools`。

```yaml
# nextmoe-infra/.github/workflows/build.yml
name: build-and-push
on:
  push:
    branches: [main]
permissions:
  contents: read
  packages: write          # 推 GHCR 必需
concurrency:                # 同分支新 push 取消旧构建
  group: build-${{ github.ref }}
  cancel-in-progress: true

jobs:
  build:
    runs-on: ubuntu-latest
    strategy:
      fail-fast: false
      matrix:
        include:
          - { name: infra-oauth,           file: docker/cgo.Dockerfile,  args: "CMD=oauth" }
          - { name: infra-image,           file: docker/cgo.Dockerfile,  args: "CMD=image" }
          - { name: infra-catalog,         file: docker/go.Dockerfile,   args: "CMD=catalog" }
          - { name: infra-migrate,         file: docker/go.Dockerfile,   args: "CMD=migrate" }
          - { name: infra-account,         file: docker/nuxt.Dockerfile, args: "APP=account" }
          - { name: infra-admin,           file: docker/nuxt.Dockerfile, args: "APP=admin" }
    steps:
      - uses: actions/checkout@v6
      - uses: docker/setup-buildx-action@v4
      - uses: docker/login-action@v4
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - uses: docker/build-push-action@v7
        with:
          context: .
          file: ${{ matrix.file }}
          build-args: ${{ matrix.args }}
          push: true
          tags: |
            ghcr.io/next-moe/${{ matrix.name }}:latest
            ghcr.io/next-moe/${{ matrix.name }}:${{ github.sha }}
          cache-from: type=gha,scope=${{ matrix.name }}
          cache-to: type=gha,mode=max,scope=${{ matrix.name }}

  deploy:                    # 全部镜像就绪后通知 Dokploy 拉取重部署
    needs: build
    runs-on: ubuntu-latest
    steps:
      - name: Trigger Dokploy redeploy
        env:                                               # secret 经 env 传入,run 里用引号包裹(防注入)
          WEBHOOK: ${{ secrets.DOKPLOY_WEBHOOK_INFRA }}    # 各仓用 _INFRA / _KUNGAL / _MOYU
        run: |
          [ -z "$WEBHOOK" ] && { echo "webhook 未设置,镜像已推 GHCR,跳过重部署"; exit 0; }
          curl -fsS -X POST "$WEBHOOK"
```

- **cgo 镜像**(oauth/image)在 `ubuntu-latest` 上正常 build —— cgo 发生在 build 容器内(`docker/cgo.Dockerfile` 的 debian-slim + libwebp),runner 无需特殊配置。
- **公开仓库 Actions 分钟免费**;`type=gha` 层缓存让二次构建快很多。
- kungal/moyu 的 workflow:`matrix` 换成 `kungal-api`/`kungal-web`/`kungal-migrate`(及 moyu 同理),`deploy` 步骤用各自的 `DOKPLOY_WEBHOOK_*`。
- **三仓 workflow 均已创建**:`<repo>/.github/workflows/build.yml`。触发分支:**infra=`main`,kungal/moyu=`master`**。注意 **kungal-web 无 `APP` build-arg**(单一 app);infra-account/admin 在此烤入真实域名(见 13.5);`deploy` 步骤已做 webhook 未设置时**优雅跳过**。

> **关键:让"构建完成"成为唯一的部署触发,否则永远部署上一次的镜像**
>
> 一次 `push` 会同时点燃**两个**部署触发,它们在赛跑:
> ```
> push ──┬─► GitHub Actions build-and-push    (几分钟:build + 推 GHCR)
>        └─► Dokploy autoDeploy(github 集成)  (几秒:compose pull + up)
>                                              ↑ 此刻 :latest 还是上一次的镜像
> ```
> Dokploy 的 **Auto Deploy 是 push 一到就部署,根本不等构建** → `pull :latest` 拉到的是上一版,
> 表现为"每次部署的都是上一次构建的镜像"。上面 workflow 里的 `deploy` job(`needs: build`,
> 构建全部完成后才 `curl` webhook)才是**正确**的晚触发,但它依赖下面的 secret;secret 没设时它会
> "优雅跳过",于是只剩错误的早触发在跑 —— 这正是这个坑的成因。
>
> **正确接法(两步,缺一不可):**
> 1. **设 secret**:在 Dokploy 每个 app 的设置里复制它的"部署 Webhook URL",填进对应仓的
>    Actions secret —— infra→`DOKPLOY_WEBHOOK_INFRA`、kungal(forum)→`DOKPLOY_WEBHOOK_KUNGAL`、
>    moyu(patch)→`DOKPLOY_WEBHOOK_MOYU`。
> 2. **关掉每个 app 的 Dokploy Auto Deploy**,去掉 push 早触发,消除赛跑。
>
> 接好后顺序才对:`push → Actions build + 推 GHCR → 构建完 curl webhook → Dokploy 拉新 :latest 部署`。
>
> **验证**:Actions 最近一次 run 的 `deploy` job 若打印"webhook 未设置…跳过",说明 secret 没设;
> Dokploy 库 `SELECT name, "autoDeploy" FROM compose;` 应全部为 `f`。
>
> **过渡期**(还没配 webhook):只做第 2 步关掉 Auto Deploy,然后每次等 Actions 构建跑完再去
> Dokploy 手动 Redeploy —— 麻烦但绝不会拉错镜像。

## 13.5 前端域名配置:两条路线(实测取舍)

Nuxt 的 public 配置有两种注入方式,直接影响"镜像是否环境无关":

- **运行时 `NUXT_PUBLIC_*` env(kungal / moyu 采用)** —— 二者的 web 读 `docker/web.env` 的 `NUXT_PUBLIC_*`(Nuxt 启动时读),**CI 构建通用镜像、不烤域名**,真实域名在 **Dokploy 环境变量 / web.env** 注入,一个镜像可用于任意环境(dev stack 即靠它切换域名,已实测)。
- **构建期 build-arg `PUBLIC_*`(infra account / admin 采用)** —— 域名在 **CI build 时**烤进镜像;`.github/workflows/build.yml` 里 infra-account / infra-admin 的 `build-args` 已写入真实 https 域名。

**为什么 infra 烤而非运行时**:约定源自已退役的 wiki 应用(其 `runtimeConfig.public.oauthClientID` / `oauthRedirectURI` 大写缩写使**运行时 `NUXT_PUBLIC_*` 反向映射别扭**,`docker/README.md` 有明确警告);account / admin 沿用 build 期烤入(`nuxt.config` 读 `KUN_*_NUXT_PUBLIC_*` 自定义名)。换域名需重跑 infra 的 workflow(改 `build-args`,或挪到仓库 Variables);kungal/moyu 无此缩写问题,故走运行时。

> **SSR 双 base 不变**:`NUXT_API_BASE_SSR` / `NUXT_AUTH_API_BASE_SSR` / kungal `NUXT_API_BASE_URL` 仍是**运行时**容器内服务名(见 [12-dokploy §12.5](./12-dokploy.md));registry 化只影响"镜像怎么来",不影响 SSR/浏览器 base 的划分。

## 13.6 生产 compose 改用 `image:`(不再 `build:`)

**已在三仓各加一份**只引用镜像的生产 compose `docker-compose.prod.yml`,Dokploy 指向它;CI 负责把这些 tag build+push 出来。下面是 infra 片段(完整见仓库文件):

```yaml
# nextmoe-infra/docker-compose.prod.yml(节选)
name: kun-galgame-infra
services:
  oauth:
    image: ghcr.io/next-moe/infra-oauth:latest   # ← 不再有 build:
    environment: { <<: *infra-env }            # ← 内联 environment(非密钥字面值 + ${VAR} 取密钥);无 env_file
    expose: ["9277"]                           # ← expose 而非 ports(见 12-dokploy §12.2-B)
    depends_on: { postgres: { condition: service_healthy }, redis: { condition: service_healthy } }
    healthcheck: { test: ["CMD", "/app/app", "healthcheck"], <<: *svc-health }
    restart: unless-stopped
  account:
    image: ghcr.io/next-moe/infra-account:latest
    environment:
      NUXT_API_BASE_SSR: http://oauth:9277/api/v1
      # 浏览器 public 域名已在 CI build 期烤入镜像(见 13.5),运行时只注 SSR base
    expose: ["3000"]
  admin:
    image: ghcr.io/next-moe/infra-admin:latest
    environment:
      NUXT_API_BASE_SSR: http://oauth:9277/api/v1
    expose: ["3000"]
  # admin / image / catalog 同理 …
  postgres: { image: postgres:18-alpine, ... }   # 基础设施仍用上游镜像
networks:
  default: { name: dokploy-network, external: true }
```
- Dokploy compose 见到 `image:` 即**拉取**(不在生产机 build);webhook 触发 → `docker compose pull && up` → 拉最新 `:latest` 滚动更新。
- 本地开发用 `docker-compose.dev.yml`(同样拉 GHCR 预构建镜像,见 `docs/dev-environment.md`);旧的本地 `build:` compose(仓根 `docker-compose.yml`)已于 wiki 退役 W5 移除。

## 13.7 Dokploy 侧配置

1. **加 Registry**(Settings → Registry):公开镜像可不加;私有则填 Name / Username=GitHub ID / Password=`read:packages` 的 **PAT** / URL=`https://ghcr.io`。
2. **应用指向 prod compose**(或用 Docker provider 填 `ghcr.io/next-moe/<svc>:latest`)。
3. **触发部署**:
   - **Webhook**:复制应用 Deployments 页的 Webhook URL → 放进 CI 的 `secrets.DOKPLOY_WEBHOOK_*`,`deploy` job `curl` 它。
   - 或 **API**:`POST /api/application.deploy`(带 API key)。
4. **公开 vs 私有镜像**:仓库公开 → 把对应 GHCR package 也设为 **public**,Dokploy 免凭证拉(最省事);私有则用 PAT。

## 13.8 Registry 维护

- **GHCR**:用 GitHub Packages 的**保留策略 / 手动删旧 tag** 控制体积(`:latest` 覆盖产生的旧 untagged 版本可定期清,可用 `actions/delete-package-versions` 之类)。
- **自托管(若将来用)**:registry **不会自动清**被覆盖 tag 的层 → 必须 `REGISTRY_STORAGE_DELETE_ENABLED=true` + 定期 `registry garbage-collect`;**TLS**(Let's Encrypt)+ **认证**(htpasswd / token)是底线;存储后端小规模用文件系统、规模化用 S3。

## 13.9 一次性迁移工具镜像(cutover)

跨仓迁移用到的 infra 一次性命令很多(`migrate-users`/`migrate-galgame-data`/`migrate-moyu-galgame`/`sync-vndb*`/`reindex-search` 等)。两种做法:
- 简单:cutover 时在 Dokploy Terminal 用 `infra-catalog`/`infra-migrate` 等已发布镜像 `docker run --entrypoint <bin>` 跑,或临时 `go run`(单镜像只含一个二进制,见上文,能跑的 job 极有限);
- 规整:加一个 `infra-tools` 镜像(一个 Dockerfile 编译所有 `cmd/*` 需要的二进制)推 GHCR,cutover 用它跑。
顺序见 [03-bootstrap.md](./03-bootstrap.md) 与 `docs/migration/`;连库用容器服务名 `postgres:5432`。

## 13.10 升级路径

- **多节点 / 想要漏洞扫描·RBAC·签名** → 上 **Harbor**(或 Dokploy Cluster + 其内置 registry 分发镜像)。
- **想全自托管构建**(不依赖 GitHub)→ **Dokploy Build Server**(独立 build VPS)+ 自托管 `distribution/registry:2`。
- 在那之前,**GHCR + Actions 足够**。

## 13.11 安全 checklist

- [ ] workflow `permissions: packages: write`,用 `GITHUB_TOKEN`(不要长期 PAT 推送)。
- [ ] Dokploy webhook URL / API key 放 **GitHub Secrets**,不入库。
- [ ] 私有镜像在 Dokploy 用最小权限 PAT(`read:packages`);能公开则公开免凭证。
- [ ] **密钥全部轮换**(见 [05-configuration.md](./05-configuration.md)),不烤进镜像——prod compose 用 `environment: + ${VAR}`,密钥填 **Dokploy 各应用 Environment 面板**(见 [15-environment §15.8](./15-environment.md))。
- [ ] 镜像 tag 用 `:<git-sha>` 保证可追溯/可回滚。
