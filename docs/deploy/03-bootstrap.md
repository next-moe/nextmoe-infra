# 3 · 首次启动(Bootstrap)

首次部署分两种目标,**命令不同**:

- **A. 仅把服务跑起来**(空库,验证容器化 / 搭新环境)——本文档实跑验证的路径。
- **B. 带生产数据上线**(从 dump 恢复 + 跨仓迁移)——见本章末「完整数据 Bootstrap」。

---

## A. 仅服务跑起来(空库)

### A.1 启动枢纽基础设施 + 建库

```bash
cd nextmoe-infra
# 首次:从模板生成运行时 env(docker/*.env 不入仓,见 15-environment §15.8)
for f in oauth image galgame; do cp -n docker/$f.env.example docker/$f.env; done
docker compose build
docker compose up -d postgres redis minio opensearch
```

> `docker/*.env` 已 `.gitignore`,fresh clone 里只有 `*.env.example`。生产则按 [15-environment §15.8](./15-environment.md) 用 Dokploy 注入,**别提交真实 env**。

Postgres **首次**初始化时,`docker/initdb.d/01-create-databases.sh` 会一次性建好全部 5 个库
(`kun_galgame_infra` 由 `POSTGRES_DB` 建,脚本再建 `kun_galgame_wiki`、`kun_images`、`kungalgame`、`kungalgame_patch`)。

> initdb 脚本**只在数据卷为空时运行一次**。若卷已存在(如本机重启后复用),需手动补建缺的库:
> ```bash
> docker exec kun-galgame-infra-postgres-1 psql -U postgres \
>   -c "CREATE DATABASE kungalgame" -c "CREATE DATABASE kungalgame_patch"
> ```

确认:
```bash
docker exec kun-galgame-infra-postgres-1 psql -U postgres -tAc \
  "SELECT datname FROM pg_database WHERE datname NOT IN ('postgres','template0','template1') ORDER BY 1"
```

### A.2 建 schema(各仓 migrate job)

```bash
cd nextmoe-infra
docker compose -f docker-compose.prod.yml run --rm migrate            # kun_galgame_infra:表 + 站点/角色种子
docker compose -f docker-compose.prod.yml run --rm migrate-catalog   # wiki 两族 + catalog:表 + 约束(W5 单一入口)
```

- `image` 服务启动时会对 `kun_images` 做 **AutoMigrate**,无需单独 job。
- moyu / kungal 的 `migrate` 是**清理型迁移**,假设 `kungalgame(_patch)` 已由 dump 恢复;空库上它会打印「没有待执行的迁移」并退出 0——**这是正常的**,服务仍能启动(健康端点不依赖业务表)。

### A.3 启动枢纽服务

```bash
docker compose up -d oauth image galgame web wiki
```

健康自检:
```bash
curl -s localhost:15005/healthz   # {"status":"ok"}
curl -s localhost:15006/healthz
curl -s localhost:15007/healthz
```

### A.4 启动下游(moyu / kungal)

见 [04-run.md](./04-run.md)。简版:

```bash
# moyu(其 compose 自带 external 网络声明)
cd kun-galgame-patch && docker compose build && \
  docker compose run --rm migrate && docker compose up -d moyu-api web

# kungal(与 moyu 一致,base 自带 external 网络)
cd kun-galgame-forum
docker compose build && docker compose run --rm migrate && docker compose up -d kungal-api web
```

### A.5 OAuth 客户端注册(否则登录走不通)

各前端用一个 OAuth client 走授权码流程,但**客户端不在任何 migrate 里种子**,需手动注册到枢纽:

| 客户端 | client_id | 用途 |
|---|---|---|
| galgame-wiki-admin | `galgame-wiki-admin` | infra wiki 前端 |
| moyu | `df3ff6008d740bfacbe46aa8cf483cf2` | moyu web |
| kungal-web | `kungal-web` | kungal web |

> kungal 的 api **强制要求** `OAUTH_CLIENT_ID`/`OAUTH_CLIENT_SECRET` 非空(`requireEnv`),否则启动即退出;但「非空」只让进程起来,真正的令牌交换仍需该 client 存在于枢纽库。

注册方式二选一:
1. **管理端 UI**:登录 infra web(`localhost:15008`,需先有 admin 账号)→ OAuth 客户端 → 新建,填 redirect_uri / scope / grants,**记下生成的 secret** 写进对应仓的 `*.env`。
2. **直接入库**:向 `kun_galgame_infra.oauth_clients` 插入行(secret 要按 `sha256:<hex>` 哈希存,见 infra `cmd/migrate-hash-client-secrets` 与 `OAuthClient.HashOAuthClientSecret`)。

下游 `*.env` 里的 `OAUTH_CLIENT_SECRET` 必须等于注册时的明文 secret。

### A.6 搜索回填(可选)

OpenSearch 索引由 catalog 启动时 `EnsureIndexes` 创建,但**空**。要让搜索出结果,需把 Postgres 数据灌进去:
```bash
cd nextmoe-infra/apps/api && go run ./cmd/reindex-catalog
```

---

## B. 完整数据 Bootstrap(生产)

> **要可直接抄的服务器命令**(从 dump 起、`docker run *-tools`、含 dry-run/校验/回滚)看 [16-data-cutover.md](./16-data-cutover.md)。本节只给顺序与原理。

带数据上线是一条**有严格顺序的跨仓流水线**(不是单个 job)。Docker 不简化顺序,但每一步都能容器化成 `compose run` job。总体顺序(详见各仓 `docs/migration/`):

1. **恢复源库 dump**:`kungalgame`、`kungalgame_patch` 从生产 dump 恢复;清空 infra 的 3 个库。
   (infra `scripts/reset_all.sh` 是本地版的 drop+restore。)
2. **kungal 预处理**:`check-dup-email` → kungal `migrate`(001–004、008–009)。
3. **moyu OAuth 对齐**:moyu `migrate-oauth-prep`。
4. **infra 身份/内容迁移**(顺序敏感):
   `migrate` → `migrate-users`(对齐三库 user.id)→ `migrate-galgame` → `migrate-galgame-data` → `migrate-moyu-galgame` → `dedup-galgame-alias`。
5. **kungal 收尾迁移**(**必须在 `migrate-users` 之后**,否则游标指向旧 id):
   kungal `migrate --only=005/006/007/015/012` + `backfill-provider-names`。
6. **moyu 收尾迁移**:moyu `migrate`(001 then 全量)+ `remap-patch-ids`。
7. **VNDB / 发布日期 / 萌萌点回填**:infra `sync-vndb*`、各仓 `backfill-release-date`、infra `migrate-moemoepoint`(2026-09-23 已删除:萌萌点改记复式账本,旧流水由 `cmd/migrate` 与 `cmd/oauth` 启动时自动导入,见 `docs/integration/oauth/06-moemoepoint.md` §6)。

> 每步对应一个 `docker compose run --rm <job>`(job 名 = `cmd/` 目录名,用 `--build-arg CMD=` 出镜像)。**严格按上面的序**;`migrate-users` 是分水岭,跨它的步骤不能乱序。原始逐条命令见仓库根 `docs/migration/` 与团队 runbook。

---

完成后,继续 [04-run.md](./04-run.md) 做日常启停,或直接 [06-operations.md](./06-operations.md) 看运维。
