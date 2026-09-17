# nextmoe-infra

> NextMoe / 鲲 Galgame 生态的**枢纽(hub)** —— 统一身份(OAuth)、图床(image service)、跨媒介 catalog,并**拥有全生态共享的基础设施**。
>
> 原名 `kun-oauth-admin` → `kun-galgame-infra`(2026-06-01) → `nextmoe-infra`(2026-09-01, GitHub `next-moe/nextmoe-infra`)。

## 这是什么

本仓是鲲 Galgame 生态**三仓**中的**枢纽**,对内对外提供:

- **身份中心** —— 自建 **OAuth 2.0 授权服务器**(授权码 + PKCE),生态内所有站点的统一登录与用户/积分(moemoepoint)体系。
- **图床(image service)** —— 图片上传、处理(WebP)、分发。
- **galgame-wiki** —— galgame 元数据的权威来源(VNDB 同步、标签、厂商等)。
- **共享基础设施的拥有者** —— 一套 **Postgres(5 库)/ Redis / MinIO(S3)/ OpenSearch**;下游站点按服务名连过来,不各自再起一套。

## 生态

| 仓库 | 代号 | 角色 |
|---|---|---|
| **nextmoe-infra**(本仓) | infra / 枢纽 | 身份 + 图床 + catalog + 共享基础设施 |
| [kun-galgame-forum](https://github.com/KunMoe/kun-galgame-forum) | kungal | 论坛主站 |
| [kun-galgame-patch](https://github.com/KunMoe/kun-galgame-patch) | moyu | 补丁站 |

下游(kungal / moyu)在运行时通过容器**服务名**调用枢纽:`oauth:9277`、`catalog:9281`(galgame-wiki 面自 W3 起由 catalog 服务承载)、`image:9278`,并共用枢纽的 Postgres / Redis / MinIO / OpenSearch。

## 架构

**可部署服务**(均无状态;Go 多阶段编译,Nuxt 出自包含 `.output`):

| 服务 | 容器端口 | 说明 |
|---|---|---|
| `oauth` | 9277 | OAuth 授权服务器 + 用户 / moemoepoint(cgo:内嵌图床 admin) |
| `image` | 9278 | 图床服务(cgo + libwebp) |
| `artifact` | 9279 | 大文件(补丁)服务(纯 Go) |
| `catalog` | 9281 | 跨媒介目录 + **galgame-wiki API**(纯 Go;:9280 独立 galgame 服务已于 wiki 退役 W3/W5 退休) |
| `community` | 9282 | 社区原语服务(纯 Go) |
| `trust` | 9283 | Trust & Safety 平台(纯 Go) |
| `ai` | 9284 | AI 网关语义层(纯 Go) |
| `web` | 3000 | 管理端前端(Nuxt 4) |
| `wiki` | 3000 | galgame-wiki 前端(Nuxt 4) |
| `developer` | 3000 | NextMoe 开发者门户(Nuxt 4) |

**共享基础设施**(本仓 compose 定义一次):Postgres、Redis、MinIO、OpenSearch。

**一套 Postgres 承载全生态 5 个库**:`kun_galgame_infra`(oauth/用户)、`kun_catalog`(catalog+galgame 两族)、`kun_images`、`kungalgame`(下游论坛)、`kungalgame_patch`(下游补丁)。

## 技术栈

- **后端**:Go 1.27 + [Fiber](https://gofiber.io/),**单模块多二进制**(`cmd/*`)。`oauth`/`image` 因 `go-webp` 走 cgo(debian-slim + libwebp),其余纯 Go(distroless)。
- **前端**:Nuxt 4 SSR(Nitro `node-server`)+ TypeScript,两个应用共享 **`@kun/ui`** Nuxt layer。
- **数据**:PostgreSQL · Redis · MinIO(S3 兼容)· OpenSearch(全文搜索)。
- **工程**:pnpm 10 workspace(monorepo)· Docker Compose · GitHub Actions(CI→GHCR)。

## 仓库结构

```
apps/
  api/              Go Fiber 后端(多二进制)
    cmd/            入口与工具:oauth / image / catalog / artifact / trust … + migrate-* / sync-vndb* / reindex-search …
    internal/       app(装配)· platform(领域)· infrastructure(db/redis/s3 客户端)· jobs · middleware
  web/              管理端前端(Nuxt 4,extends @kun/ui)
  wiki/             galgame-wiki 前端(Nuxt 4,extends @kun/ui)
packages/
  ui/               @kun/ui —— 共享 Nuxt UI layer(组件 / 色系 / 样式)
  image-client/     @kun/image-client —— 图床客户端
docker/             Dockerfile(cgo / go / nuxt)+ 各服务 env + initdb.d(建库清单,12 个)
docs/               文档(deploy / migration / api / galgame_wiki / image_service …)
scripts/            运维脚本(reset_all.sh)+ 源库 dump
```

## 快速开始

### 本地开发(一条命令)

前置:一台你自己的 Postgres(**不在 compose 里**)。坐标默认 `127.0.0.1:5432` / `postgres`,
不一致就在仓库根写一个 `.env`(已 gitignore)覆盖 `KUN_PG_HOST/_PORT/_USER/_PASSWORD`。

```bash
pnpm install
pnpm dev:db     # 建齐 12 个库(幂等;initdb.d 只在 Postgres 首次初始化时跑,这里永远不会自动跑)
pnpm dev        # 平台底座(docker-compose.dev.yml:redis/minio/opensearch/mailpit/迁移)
                # + air 热重载五个常改 Go 服务(oauth/catalog/image/artifact/trust)+ Nuxt 前端
```

`pnpm dev` 会先跑 `pnpm dev:doctor` 体检(约 2 秒,只读):shell 是不是 Git Bash、docker
能不能连、Postgres 通不通、**host 网络的容器能不能真的够到它**、12 个库齐不齐、GHCR 有没有权限
—— 每条失败都直接给修法。迁移容器「启动几秒就 exit 1」基本只有两个原因(密码不对 / 库不存在),
体检直接说是哪个。

> **Windows**:整套栈依赖 `network_mode: host`,这是 Linux 特性 —— Docker Desktop 里的
> "host" 是那台 Linux 虚拟机,够不到装在 Windows 上的 Postgres。受支持的路径是
> **Postgres、仓库、shell 全在同一个 WSL2 发行版里**(仓库克隆到 `~` 而非 `/mnt/c`)。
> 完整步骤见 [docs/dev-environment.md](./docs/dev-environment.md) → Windows setup。

完整模型(镜像拉取、Replace 模式、数据脱敏快照)见 [docs/dev-environment.md](./docs/dev-environment.md);
community / ai 不在默认栈里(没人拨它们),需要时 `pnpm dev:full` = 全平台纯镜像(开发下游产品仓时用),
`pnpm dev:down` 停底座。

### 生产

生产用 `docker-compose.prod.yml`(Dokploy + GHCR 预构建镜像;迁移 job 随每次部署自动跑)。

## 部署

- **快速上线**:[docs/deploy/QUICKSTART.md](./docs/deploy/QUICKSTART.md) —— 全新 Debian 服务器到三站上线的精简步骤(**Dokploy**:内置 Traefik 反代 + 自动 HTTPS)。
- **线上方案**:单服务器 + Dokploy,镜像由 **CI 构建推 GHCR**、生产机零构建。完整分章(架构 / 构建 / 首启 / 运维 / 排错 / Dokploy / Registry-CI / 备份还原 / 源站 IP 防泄漏)见 [docs/deploy/README.md](./docs/deploy/README.md)。
- **备份与还原**:[docs/deploy/14-backup-restore.md](./docs/deploy/14-backup-restore.md)。

## 开发规范

前端约定(UI 组件、页面/组件拆分、常量与类型位置、自定义色系、箭头函数等)见 [CLAUDE.md](./CLAUDE.md)。
