# 2 · 镜像构建

所有镜像的**构建 context 都是各自仓库根**(前端要把 `packages/ui` 这个 Nuxt layer 一起带进去)。

## Infra(nextmoe-infra)

Infra 有 3 个 Dockerfile,因为它的二进制分两类:

| Dockerfile | 构建对象 | CGO | 基镜 |
|---|---|---|---|
| `docker/go.Dockerfile` | `catalog` 等纯 Go 服务 + 所有 `migrate-*`/worker | 0 | distroless |
| `docker/cgo.Dockerfile` | **`oauth` + `image`** | 1 | debian-slim + libwebp |
| `docker/nuxt.Dockerfile` | `web` + `wiki` | — | node-slim |

> **为什么 oauth 也要 cgo**:`oauth` 内嵌图床 admin 端点,其 `image/service` import 了 WebP `processor`(`kolesa-team/go-webp` → libwebp 的 cgo 绑定)。用 `go list -deps ./cmd/oauth | grep go-webp` 可验证。`catalog` 等纯 Go 服务与迁移工具不碰 processor,故纯 Go。

> infra 的「一键 `docker compose build`」本地栈已于 wiki 退役 W5 移除——镜像统一由 CI 构建推 GHCR([13-registry-ci](./13-registry-ci.md)),本地开发拉预构建镜像(`docs/dev-environment.md`)。需要手工构建单个镜像时用下面的参数化命令。

参数化单独构建(go/cgo 用 `CMD`,nuxt 用 `APP`):

```bash
docker build -f docker/go.Dockerfile  --build-arg CMD=catalog -t nextmoe-infra/catalog .
docker build -f docker/cgo.Dockerfile --build-arg CMD=oauth   -t nextmoe-infra/oauth .
docker build -f docker/nuxt.Dockerfile --build-arg APP=wiki   -t nextmoe-infra/wiki .
```

## moyu(kun-galgame-patch)

纯 Go(单 `server` 二进制 + 迁移/同步工具)+ Nuxt。无 cgo。

```bash
cd kun-galgame-patch
docker compose build          # api web(+ migrate job)
```

## kungal(kun-galgame-forum)

**kungal 的 `docker-compose.yml` 单独无法构建**——它在 `depends_on` 里引用了未定义的 `postgres`/`redis`,直接 `docker compose build` 会报 `invalid compose project`。必须叠加一个定义/外置了 pg+redis 的 compose:

```bash
cd kun-galgame-forum
docker compose build           # 与 moyu 一致;build 不依赖网络,infra 未起也能 build
```

## 基础镜像版本(2026-06 审计)

全部对齐到当前稳定版:

| 用途 | 镜像 | 说明 |
|---|---|---|
| Go 构建器 | `golang:1.25-trixie`(infra)/ `golang:1.26-trixie`(moyu/kungal) | 跟随各仓 go.mod;Debian 13 |
| 纯 Go runtime | `gcr.io/distroless/static-debian13:nonroot` | Debian 13 基线 |
| cgo runtime(oauth/image) | `debian:trixie-slim` + `libwebp7 libsharpyuv0` | Debian 13 的 libwebp 1.5 把 sharpyuv 拆成独立包,故需补 `libsharpyuv0`(bookworm 时是打进 libwebp7 的) |
| 前端 runtime/构建 | `node:24.22.1-trixie-slim` | Node 24.22.1，当前 Active LTS 的 2026-10-21 安全版本 |
| Postgres | `postgres:18-alpine` | **已升级 16→18**(2026-06,dump/restore;pg18 的 VOLUME 从 `/var/lib/postgresql/data` 改到 `/var/lib/postgresql`,挂载点已同步,见 [06-operations.md](./06-operations.md)) |
| Redis | `redis:8-alpine` | 已是最新大版本(`8-alpine` 自动取 8.8.x);向前兼容旧数据(本就是缓存/会话) |
| MinIO | `minio/minio:RELEASE.2025-09-07T16-13-09Z` | 已锁版本(原先是 `latest`)。注:MinIO 官方社区镜像已停更/归档(~2026-04),生产图床走 Cloudflare R2,影响小 |
| OpenSearch | `ghcr.io/next-moe/infra-opensearch:2.19.6` | catalog 搜索(icu/kuromoji/pinyin);索引派生自 Postgres,`reindex-catalog` 可重建 |

> Debian:bookworm(12)→**trixie(13,2025-08 起为 stable)**。bookworm 仍受支持到 2028,但 trixie 是当前稳定版,新部署用它更合适。换基镜后镜像需重建并替换无状态容器(有状态卷不受影响)。

## 前端 public 配置(构建期烘焙)

Nuxt 的 `runtimeConfig.public`(apiBase、OAuth client、image CDN)在 `nuxt.config.ts` 里读的是**自定义 `KUN_*` env 名**,只在 **build 期**生效。因此各仓 compose 在 `build.args` 里以 `PUBLIC_*` 传入并烘焙进镜像。例如 infra wiki:

```yaml
args:
  APP: wiki
  PUBLIC_API_BASE: http://localhost:15007/api
  PUBLIC_OAUTH_AUTHORIZE_BASE: http://localhost:15005/api/v1
  PUBLIC_OAUTH_CLIENT_ID: galgame-wiki-admin
  PUBLIC_OAUTH_REDIRECT_URI: http://localhost:15009/auth/callback
  PUBLIC_IMAGE_CDN_BASE: http://localhost:15002/kun-images
```

> 想「一次构建、运行时改配置」:不传 `PUBLIC_*`,改在容器运行时设 Nitro 约定名 `NUXT_PUBLIC_*`。但 `oauthClientID`/`oauthRedirectURI` 这类驼峰键到 env 名的映射有歧义,所以本部署默认走 build 期烘焙(各 web 的 host 地址固定,烘焙无碍)。详见 [05-configuration.md](./05-configuration.md)。

## 构建产物体积参考(实测)

```
distroless(galgame/migrate/kungal-api/moyu-api)   24–45 MB
cgo-slim   (oauth/image)                            ~180 MB
nuxt       (各 web)                                 ~390 MB
```

## 构建期会看到、可忽略的告警

- `requires buildx plugin to be installed` —— legacy builder 回落,正常。
- `@nuxt/image: sharp binaries included for linux-x64. Make sure you deploy to the same architecture.` —— build+run 都在 linux-x64 容器,匹配。
- `@tailwindcss/vite ... Sourcemap is likely to be incorrect` —— 无害。
