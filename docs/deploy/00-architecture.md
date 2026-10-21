# 0 · 架构总览

## 三个仓库

| 仓库 | 代号 | 角色 | apps |
|---|---|---|---|
| `nextmoe-infra` | **infra / 枢纽** | 身份(OAuth)+ 图床 + galgame-wiki 的单一来源;**拥有共享基础设施** | api(多二进制)、web(admin)、wiki(galgame-wiki) |
| `kun-galgame-forum` | **kungal** | 论坛站(Fiber API + Nuxt SSR) | api、web |
| `kun-galgame-patch` | **moyu** | 补丁站(Fiber API + Nuxt SSR) | api、web |

三仓位于同一父目录 `website/` 下,平级。

## 服务拓扑

```
            ┌─────────────────── 共享基础设施(infra compose 定义一次)───────────────────┐
            │   postgres:18    redis:8    minio(S3)    opensearch                    │
            └──────────────────────────────────────────────────────────────────────────┘
                 ▲ 同一 docker 网络:kun-galgame-infra_default(所有容器都在上面)
   ┌─────────────┼───────────────────────────┬───────────────────────────┐
   │ infra         │                 moyu       │                 kungal     │
   │  oauth   ───┤(身份/账本/图床admin/jobs)  api ──(OAuth/图床/wiki)     api ──(OAuth/wiki/图床/搜索)
   │  image   ───┤(cgo+libwebp,图床)         web                          web
   │  galgame ───┤(galgame-wiki API + 搜索)
   │  web(admin)─┤
   │  wiki      ─┘
   └─ 长驻服务都无状态,可随意重建/扩缩 ─┘
```

下游(moyu/kungal)的 api 在运行时通过**服务名**调用枢纽:
- `http://oauth:9277/api/v1` —— OAuth 令牌、用户信息、moemoepoint 账本(s2s Basic Auth)
- `http://catalog:9281/api` —— galgame-wiki 数据(W3 起由 catalog 服务承载;独立 galgame 服务/9280 已退休)
- `http://image:9278` —— 图床上传 / 引用
- `postgres:5432` / `redis:6379` / `opensearch:9200` / `minio:9000`

## 端口表(host : 容器)

> host 端口统一 `1xxxx` 段,避免和本机 `air`(9277/9281/…)冲突。容器间互访用容器端口 + 服务名,与 host 映射无关。所有 Go HTTP 服务的健康端点已**统一为根路径 `/healthz`**(无鉴权、不过 CORS/限流),容器 HEALTHCHECK 用二进制自带的 `healthcheck` 子命令自探它。

| 服务 | 容器端口 | host 端口 | 健康端点 |
|---|---|---|---|
| infra oauth | 9277 | **15005** | `/healthz` |
| infra image | 9278 | **15006** | `/healthz` |
| infra catalog(含 galgame-wiki 面;独立 galgame/9280 已退休) | 9281 | **15281** | `/healthz` |
| infra web(admin) | 3000 | **15008** | `/`(302→`/auth/login`) |
| infra wiki(galgame-wiki) | 3000 | **15009** | `/` |
| infra postgres | 5432 | **15000** | `pg_isready` |
| infra redis | 6379 | **15001** | `redis-cli ping` |
| infra minio | 9000 / 9001 | **15002 / 15003** | 控制台 15003 |
| infra opensearch | 9200 | —(search 网络,不发布宿主端口) | `_cluster/health` |
| moyu api | 5214 | **15010** | `/healthz` |
| moyu web | 3000 | **15011** | `/` |
| kungal api | 2334 | **15012** | `/healthz` |
| kungal web | 7777 | **15013** | `/` |

## 数据库映射(同一 Postgres 实例,多库)

枢纽的一套 Postgres 承载全生态 5 个库:

| 库名 | 属主 | 由谁建 schema |
|---|---|---|
| `kun_galgame_infra` | infra oauth | `migrate`(infra) |
| `kun_galgame_wiki` → 生产已并入 `kun_catalog`(W1) | infra catalog(galgame 面) | `migrate-catalog`(infra,W5 单一入口) |
| `kun_images` | infra image | image 服务启动时 AutoMigrate |
| `kungalgame` | kungal | dump 恢复 + kungal `migrate` + 跨仓迁移 |
| `kungalgame_patch` | moyu | dump 恢复 + moyu `migrate` + 跨仓迁移 |

前 3 个库由 infra 的 `docker/initdb.d/01-create-databases.sh` 在 Postgres 首次初始化时一并 `CREATE DATABASE`(已含后两个下游库)。

## 镜像策略(每仓略有不同)

| 镜像 | 基镜 | 体积 | 说明 |
|---|---|---|---|
| infra galgame / 迁移工具 / kungal api / moyu api | `distroless/static-debian13` | 24–45MB | 纯 Go,`CGO_ENABLED=0` 静态二进制 |
| **infra oauth / infra image** | `debian:trixie-slim` | ~180MB | **cgo + libwebp**(`kolesa-team/go-webp`);oauth 因内嵌图床 admin 也被拉入 cgo |
| 所有 web(infra web/wiki、moyu web、kungal web) | `node:24.22.1-trixie-slim` | ~390MB | Nitro `node-server` + 自包含 `.output`(含 sharp,linux-x64) |

详见 [02-build.md](./02-build.md)。
