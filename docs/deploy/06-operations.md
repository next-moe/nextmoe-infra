# 6 · 运维(Day-2)

## 健康与可见性

```bash
# 全生态状态
docker ps --format '{{.Names}}\t{{.Status}}' | grep -E 'kun-galgame-infra-|moyu-|kungal-' | sort

# 端点探活
for u in 15005/healthz 15006/healthz 15007/healthz \
         15010/healthz 15012/healthz; do
  printf "%-24s " "$u"; curl -s -o /dev/null -w "%{http_code}\n" "http://localhost:$u"
done

# 日志(跟随)
docker compose logs -f --since 10m oauth
```

- 三个 Go HTTP 服务的容器 `HEALTHCHECK` 用**二进制自带的 `healthcheck` 子命令**(distroless 无 shell):`/app healthcheck`(infra cgo 镜像是 `/app/app healthcheck`)自探**根路径 `/healthz`**(全服务已统一)。前端用 Node TCP 探活。
- `docker inspect --format '{{.State.Health.Status}}' <container>` 看健康判定。

## 升级 / 重新部署

无状态服务,滚动重建即可:
```bash
# 改了代码后,重建并替换(数据卷不动)
docker compose build oauth && docker compose up -d oauth
# 下游同理(kungal 记得带 -f override)
```
- 升级 Go/Nuxt 服务**不需要**动数据库。换基础镜像(trixie / node24 等)也只是重建无状态容器,数据卷不受影响。
- 升级**有状态**镜像(Postgres 大版本)**不能**直接改 tag 重建——数据卷格式不兼容会导致新版起不来。

### Postgres 大版本升级(需迁移,勿直接换 tag)
当前在 `postgres:18-alpine`(已于 **2026-06 由 16 升级**)。大版本升级实测步骤(dump/restore,最稳):
```bash
# 1. 全量备份(角色 globals + 所有库)
docker exec kun-galgame-infra-postgres-1 pg_dumpall -U postgres > all.sql
# 2. 停栈、删旧卷(备份在手才删)
docker compose down && docker volume rm kun-galgame-infra_pg
# 3. pg18 把 VOLUME 从 /var/lib/postgresql/data 改到 /var/lib/postgresql(PGDATA=/var/lib/postgresql/18/docker)
#    —— compose 的卷挂载点要同步改成 /var/lib/postgresql(本仓已改)。
#    用临时 pg18 容器恢复:不挂 initdb.d、不设 POSTGRES_DB,避免和 dumpall 的 CREATE DATABASE 冲突。
docker run -d --name pg18r -e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=<pw> \
  -v kun-galgame-infra_pg:/var/lib/postgresql postgres:18-alpine
cat all.sql | docker exec -i pg18r psql -U postgres     # 等就绪后;唯一无害报错:role "postgres" already exists
docker stop pg18r && docker rm pg18r                    # 卷已填好(临时容器仅用于恢复)
# 4. 正常起栈(卷已初始化 → initdb.d / POSTGRES_DB 不再触发),核对计数
docker compose up -d postgres   # 等 healthy
# 备选:pgautoupgrade 镜像原地升级(进阶,先备份)
```
Redis(`redis:8-alpine`)/MinIO(已锁 RELEASE)向前兼容旧数据,直接换 tag 重建即可。OpenSearch 索引是派生数据,换引擎镜像后 `go run ./cmd/reindex-catalog` 重建即可。

## 备份 / 恢复

> **完整的备份/还原详解**(手动/自动/异地、各类还原场景、PITR)见 [14-backup-restore.md](./14-backup-restore.md)。下面是速记。

数据全在 infra 的命名卷:`kun-galgame-infra_{pg,redis,minio,opensearch}`。

```bash
# Postgres 逻辑备份(所有库)
docker exec kun-galgame-infra-postgres-1 pg_dumpall -U postgres > all-$(date +%F).sql
# 单库
docker exec kun-galgame-infra-postgres-1 pg_dump -U postgres kungalgame > kungalgame-$(date +%F).sql

# 卷级备份(停服更安全)
docker run --rm -v kun-galgame-infra_pg:/data -v "$PWD:/b" alpine \
  tar czf /b/pg-vol-$(date +%F).tgz -C /data .

# MinIO 对象:挂卷 tar,或用 mc mirror 到异地
docker run --rm -v kun-galgame-infra_minio:/data -v "$PWD:/b" alpine \
  tar czf /b/minio-$(date +%F).tgz -C /data .
```

恢复:`psql < dump.sql` 进对应库;卷级则 `tar x` 回空卷后再起服务。
- Redis 是缓存/会话,**可不备份**(丢了重新登录即可)。
- OpenSearch 索引可由 `cmd/reindex-catalog` 从 Postgres 重建,**不必备份**。

## 迁移 / 一次性任务

`migrate*` 随每次 `up`/部署自动跑并把服务 gate 在其后;也可手动 run(`tools` 仍是 jobs profile):
```bash
docker compose -f docker-compose.prod.yml run --rm migrate              # infra oauth schema
docker compose -f docker-compose.prod.yml run --rm migrate-catalog      # wiki 两族 + catalog schema(W5 单一入口)
# 其它一次性工具走全量 tools 镜像(见 13-registry-ci):
docker compose -f docker-compose.prod.yml --profile jobs run --rm tools sync-vndb -tagmap docs/tagMap.ts
```
跨仓数据迁移的**顺序**见 [03-bootstrap.md](./03-bootstrap.md) B 节——`migrate-users` 是分水岭。

## 定时任务

infra `oauth` 进程内置 job 调度器(启动日志可见 `jobs: scheduler started`),自动跑 `image-gc` / `sync-vndb` / `*-refping` 等,**无需** crontab。可在 admin 端 `/api/v1/admin/jobs/*` 手动触发 / 看历史。

## 扩缩容

- 无状态 api/web 可水平扩:`docker compose up -d --scale galgame=2`(前面要有反代做负载均衡;infra 的 job 调度用 PG advisory lock 做单飞,多副本安全)。
- 有状态(pg/redis/minio/opensearch)单实例;要高可用需各自的集群方案,超出本机范围。

## 资源 / 清理

```bash
docker images --format '{{.Repository}}:{{.Tag}}\t{{.Size}}' | grep -E 'nextmoe-infra/|moyu/|kungal/'
docker system df            # 看占用
docker image prune          # 清悬空镜像
```
- 5 个 Node SSR 进程内存占大头(各 ~150–300MB)。内存紧张时给 web 容器加 `mem_limit`。

## 反向代理(生产)

各 web/api 前面套 Caddy/Traefik,按域名分流并终止 TLS。示例(Caddy):
```
account.nextmoe.com → infra account:3000 ;  /api/v1/* + /oauth/jwks + /.well-known/* → oauth:9277
admin.nextmoe.dev    → infra admin:3000   ;  /api/v1/* → oauth:9277
www.kungal.com     → kungal web:7777
www.moyu.moe       → moyu web:3000    ;  /api/* → moyu moyu-api:5214
image.kungal.com   → minio:9000/kun-images   (或 CDN 回源 MinIO)
```
注意把对应 web 镜像的 `PUBLIC_*`/`NUXT_PUBLIC_*` 改成真实公网域名(见 [05-configuration.md](./05-configuration.md))。
