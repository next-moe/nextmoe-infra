# Catalog 生产 Cutover Runbook

> **这是一份照单执行的运维手册**,把第一期(catalog 01-15)+ 二期 A-01(16 工程化)累积的**所有未上线改动**一次性 cutover 到生产。**由你(用户)在生产主机执行**;每条命令可复制,占位符仅密钥/资源标识类。
>
> 背景与前置知识不在此重复,互链既有文档:部署总览 [00-architecture](./00-architecture.md)、日常运维 [06-operations](./06-operations.md)、Dokploy [12-dokploy](./12-dokploy.md)、镜像/CI [13-registry-ci](./13-registry-ci.md)、备份还原 [14-backup-restore](./14-backup-restore.md)、带数据上线范本 [16-data-cutover](./16-data-cutover.md)。契约见 [../catalog/README](../catalog/README.md)。
>
> **一句话形态**:push → CI 建镜像(含 `infra-catalog`/`infra-migrate-catalog`)+ 手动建 `infra-tools` → 生产建 `kun_catalog`/`erogamescape` 两库并灌 dump → Dokploy redeploy(**逐服务核 sha**)→ **显式核 schema**(不信自动链)→ 绑定 `catalog_site` → infra-tools 幂等重跑富集/对账/重建索引 → 验服务与消费面。

---

## 运维环境速览(每次连上先确认)

- 生产 = `ssh kungal-neo`(免密 `sudo`;docker 需 `sudo`)。若跳板被限流(`kex_exchange_identification: Connection closed`),直连:`ssh -o ProxyJump=none -i ~/.ssh/kungalgame_rsa -p 48542 kun@194.238.79.49`。
- 编排 = Dokploy compose 项目 `kun-visual-novel-infra-vqvqbc-*`,docker 网络 `dokploy-network`。**先发现真实前缀**(下同 `$PROJ`):
  ```bash
  ssh kungal-neo
  sudo -i
  PROJ=$(docker ps --format '{{.Names}}' | grep -oE 'kun-visual-novel-infra-vqvqbc-[a-z0-9]+' | head -1 | sed 's/-[a-z0-9]*$//')
  # 校验:应能列出 …-postgres-1 / …-oauth-1 / …-galgame-1 / …-web-1 等
  docker ps --format '{{.Names}}' | grep "$PROJ"
  PG="${PROJ}-postgres-1"
  ```
- **生产 SQL 一律走 trust-psql**(容器内本地 socket,无密码):`sudo docker exec "$PG" psql -U postgres -d <db> -c "…"`;含括号/引号的 SQL 用 `psql -i` 走 stdin 避免转义地狱。
- **一次性 cmd 走 infra-tools**(env-file 从服务容器导出,用完 shred);见 §6.0。

---

## 0 · 前置检查清单(在本地 + 上机前)

- [ ] **本地全量测试绿**:`cd apps/api && go build ./... && TEST_DATABASE_DSN=<local> go test -p 1 ./...`(应 40 包 ok / 0 FAIL)。
- [ ] **待推提交复核**:`git log --oneline origin/main..main` 应为 **20** 个(`1d1cc2c`…`3d69480` 的 catalog 全系列 01-17 + 随行的 artifact 尾 `1aa6b11`/`53f9b03`/`f21fd8b`)。逐条确认无杂项。
- [ ] **三个迁移欠账**明确(§3/§4 显式核验):
  1. 主库 `kun_galgame_infra`:`oauth_clients.catalog_site` 列(步骤 16,`cmd/migrate`);
  2. wiki 库 `kun_galgame_wiki`:`galgame_bangumi_meta` 表(步骤 10 欠账,`cmd/migrate-galgame`);
  3. **新库** `kun_catalog`:整套 Gold+src_bangumi+src_llm schema(`cmd/migrate catalog`)——**库需手建**(§2)。
- [ ] **dump 体积预估**(本地 `pg_dump -Fc` 实测,2026-07-06):`kun_catalog` ≈ **435 MB**(库 ~3.0 GB;dump ~26 s),`erogamescape` ≈ **358 MB**(库 ~4.2 GB;dump ~24 s)。scp 前留足带宽/磁盘。
- [ ] **富集/对账欠账**知晓:生产 wiki 的 bangumi 首次富集(~**6,093** 部 bid 锚游戏)+ 生产 wiki 相对本地快照的新增游戏认领(§6)。
- [ ] pg_dump 版本 ≥ 生产 PG(本地 18.4,生产 `postgres:18-alpine` → 兼容)。

---

## 1 · Push 与镜像构建

1. **推送(全流程第一个动作,由你执行)**——单仓 infra:
   ```bash
   cd /home/kun/Desktop/code/website/nextmoe-infra
   git push origin main
   ```
2. **盯 CI**:GitHub → Actions → `build-and-push` 本次 run 全绿。push 改了 `apps/api/**` → **go 组全建**,新镜像 `ghcr.io/next-moe/infra-catalog:latest` + `infra-migrate-catalog:latest` 首次出现,连同 `infra-migrate`/`infra-migrate-galgame`/`infra-galgame`/…一并重建。
3. **核 `infra-tools` 已随本次 push 建绿**(它现在与 go 组锁步,`apps/api/**` 或 `docker/tools.Dockerfile` 一变即建;要单独重建走 Actions → **`build-and-push`** → **Run workflow** → `scope=tools`)。`infra-tools` 由 `docker/tools.Dockerfile` 的 `-o /out/ ./cmd/...` 打包**每个** `cmd/*`,故本轮新增的 `reconcile-galgame-works`/`reindex-catalog`/`enrich-bangumi` 等**都随这次重建进镜像**——拉到旧镜像则生产 tools 里没有它们(stale → 静默失败)。

---

## 2 · 生产库准备(部署前)

> **顺序敏感**:2.0 的 wiki 备份是 §8 回滚的前提,**先于本节一切写操作**。本节仍是「上机 → `sudo -i` → 设好 `$PROJ`/`$PG`」的语境。

### 2.0 · wiki 库备份(回滚依赖,先做)

`enrich-bangumi`(§6)会写 `kun_galgame_wiki`,**不可简单回滚**(见 §8),故先冷备:
```bash
sudo docker exec "$PG" pg_dump -U postgres -Fc -d kun_galgame_wiki \
  > /root/pre-catalog-cutover_kun_galgame_wiki_$(date +%F).dump
ls -lh /root/pre-catalog-cutover_kun_galgame_wiki_*.dump   # 记录体积作证据
```
（可选,更稳)顺带备 `kun_galgame_infra`(`catalog_site` 是加列、天然可回滚,备份非必需但无害)。备份方式详见 [14-backup-restore §2.2](./14-backup-restore.md)。

### 2.1 · 建两个新库

**生产 Postgres 的 `initdb.d` 只在首次初始化生效**,故 `kun_catalog`/`erogamescape` 必须手建:
```bash
sudo docker exec "$PG" psql -U postgres -c "CREATE DATABASE kun_catalog;"
sudo docker exec "$PG" psql -U postgres -c "CREATE DATABASE erogamescape;"
sudo docker exec "$PG" psql -U postgres -c "\l" | grep -E 'kun_catalog|erogamescape'   # 确认存在
```

### 2.2 · 灌 dump（本地 → 生产）

大文件进生产的既有配方 = 本地 `pg_dump -Fc` → `scp` → 容器内 `pg_restore`(custom 格式走 stdin,无需 `docker cp`)。

**本地**(开发机)导两库:
```bash
cd /home/kun/Desktop/code/website/nextmoe-infra
PGPASSWORD=<本地pg密码> pg_dump -h localhost -U postgres -Fc -d kun_catalog   -f /tmp/kun_catalog.dump
PGPASSWORD=<本地pg密码> pg_dump -h localhost -U postgres -Fc -d erogamescape -f /tmp/erogamescape.dump
scp /tmp/kun_catalog.dump   kungal-neo:/root/
scp /tmp/erogamescape.dump kungal-neo:/root/
```
**生产**还原(容器内 `pg_restore` 读 stdin):
```bash
cat /root/kun_catalog.dump   | sudo docker exec -i "$PG" pg_restore -U postgres --no-owner --no-privileges -d kun_catalog
cat /root/erogamescape.dump | sudo docker exec -i "$PG" pg_restore -U postgres --no-owner --no-privileges -d erogamescape
```
> `kun_catalog` dump 含 **Gold + `src_bangumi` + `src_llm`** 全 schema+数据。`--no-owner --no-privileges` 抹掉本地 owner。`pg_restore` 对已存在对象可能刷少量 warning(权限/注释),非致命;若报大量 error 停下核对。

### 2.3 · 还原后行数抽验（trust-psql）

```bash
sudo docker exec "$PG" psql -U postgres -d kun_catalog -tAF' | ' -c \
 "SELECT (SELECT count(*) FROM catalog_work) works,(SELECT count(*) FROM catalog_credit) credits,(SELECT count(*) FROM catalog_credit_name) names,(SELECT count(*) FROM src_bangumi.subject) bgm_subject"
```
期望(2026-07-06 本地基线):**works 206,710 / credits 629,207 / 名义 59,769 / src_bangumi.subject 648,663**。数字不符停下排查。

---

## 3 · 部署与镜像 sha 核验

1. **触发 redeploy**:Dokploy 面板 redeploy(或 push 已触发的 webhook 自动 redeploy)。详见 [12-dokploy](./12-dokploy.md)。
2. **⚠️ no-pull 空档 —— 逐服务核 sha 为新**(CI webhook 的 redeploy 常不重拉 `:latest`):
   ```bash
   for s in catalog galgame oauth migrate-catalog; do
     echo "== $s =="; sudo docker inspect "${PROJ}-${s}-1" --format '{{.Image}}' 2>/dev/null || echo "(无此容器/job)"
   done
   # 对照 GHCR 最新 digest;若某服务镜像是旧的 → 手动拉 + 强制重建:
   #   sudo docker compose -p "$PROJ" pull <svc> && sudo docker compose -p "$PROJ" up -d --force-recreate <svc>
   ```
   (compose 文件路径见 Dokploy 应用目录;`docker compose -p "$PROJ"` 直接按项目名操作。)
3. **catalog / migrate-catalog 首次出现**:确认 `migrate-catalog` job **exited(0)**:
   ```bash
   sudo docker ps -a --format '{{.Names}} {{.Status}}' | grep -E 'migrate-catalog|catalog'
   ```

---

## 4 · 显式 schema 核验（不信「链跑过了」）

no-pull 空档下,自动迁移链**可能没用新镜像跑** → 三处 schema 全部显式核:
```bash
# 1) 主库:catalog_site 列(步骤 16)
sudo docker exec "$PG" psql -U postgres -d kun_galgame_infra -tAc \
 "SELECT data_type,character_maximum_length FROM information_schema.columns WHERE table_name='oauth_clients' AND column_name='catalog_site'"
#    期望:character varying | 64  （空 = migrate 没用新镜像跑 → §3.2 强制重建 migrate 再看）

# 2) wiki 库:galgame_bangumi_meta 表(步骤 10 欠账)
sudo docker exec "$PG" psql -U postgres -d kun_galgame_wiki -tAc \
 "SELECT to_regclass('public.galgame_bangumi_meta')"
#    期望:galgame_bangumi_meta（NULL = migrate-galgame 没跑 → 强制重建 migrate-galgame）

# 3) kun_catalog:表数 + 注册表种子计数
sudo docker exec "$PG" psql -U postgres -d kun_catalog -tAF' | ' -c \
 "SELECT (SELECT count(*) FROM information_schema.tables WHERE table_schema='public') tables,
         (SELECT count(*) FROM catalog_role) roles,(SELECT count(*) FROM catalog_source_role_map) maps,
         (SELECT count(*) FROM catalog_medium) media,(SELECT count(*) FROM catalog_source) sources,
         (SELECT count(*) FROM catalog_relation_type) reltypes"
#    期望:tables ≥ 27 | roles 228 | maps 259 | media 7 | sources 11 | reltypes 13
```

---

## 5 · 绑定赋值（`catalog_site`)

wiki 的机密 S2S client 绑到 `galgame_wiki`(`image_site_key='galgame_wiki'` 且非前端公开 client `galgame-wiki-admin`)。**先 SELECT 定位再 UPDATE**:
```bash
# 定位(生产 client id 是每环境随机,靠稳定的 image_site_key 认):
sudo docker exec "$PG" psql -U postgres -d kun_galgame_infra -c \
 "SELECT id,name,image_site_key,catalog_site FROM oauth_clients WHERE image_site_key='galgame_wiki'"
# 绑定(机密 S2S client;排除前端 admin client):
sudo docker exec "$PG" psql -U postgres -d kun_galgame_infra -c \
 "UPDATE oauth_clients SET catalog_site='galgame_wiki' WHERE image_site_key='galgame_wiki' AND id <> 'galgame-wiki-admin'"
# 核验:
sudo docker exec "$PG" psql -U postgres -d kun_galgame_infra -c \
 "SELECT id,name,catalog_site FROM oauth_clients WHERE catalog_site IS NOT NULL"
```
> 说明:这是**为 D 轨「live wiki→catalog claim」提供的绑定**。当前的作品认领由 §6c 的 `reconcile-galgame-works` **直写 catalog** 完成(不走 claim S2S 端点),故本步不影响本轮 cutover,只是把写路径的授权提前钉好。

---

## 6 · 幂等重跑序列（infra-tools + env-file 配方）

### 6.0 · tools 运行配方（每条 cmd 通用)

```bash
sudo docker pull ghcr.io/next-moe/infra-tools:latest   # §1.3 已建;这里确认拉到新的
# 把某服务的注入 env 导成 root-only 文件(含密码;绝不 echo 到日志/文件/commit):
dump_env(){ sudo docker inspect "$1" --format '{{range .Config.Env}}{{println .}}{{end}}' | sudo tee /tmp/x.env >/dev/null && sudo chmod 600 /tmp/x.env; }
run_tool(){ sudo docker run --rm --network dokploy-network --env-file /tmp/x.env ghcr.io/next-moe/infra-tools:latest "$@"; }
```
> 每条 cmd 先 **dry-run**(仓库铁律:不带 `--apply` = 只统计),核 config 行(如 `tagMap entries: N`)确认镜像是新的,再 `--apply`。跑完 **`sudo shred -u /tmp/x.env`**。
> `reconcile`/`reindex-catalog` 需 catalog 的 env(用 `${PROJ}-catalog-1`);`enrich-bangumi`/`reindex-search`/`audit-*` 需 galgame/wiki 的 env(用 `${PROJ}-galgame-1`)。

### 6.a · wiki bangumi 首次富集

`migrate-galgame` 已由部署链跑(§4 已核 `galgame_bangumi_meta`)。富集(dry → apply):
```bash
dump_env "${PROJ}-galgame-1"
run_tool enrich-bangumi                 # dry:统计应约 6,093 部待富集
run_tool enrich-bangumi --apply         # 写;统计输出留证据
```

### 6.b · 富集后重建 galgame 搜索索引

```bash
run_tool reindex-search --index=galgames
```

### 6.c · 认领生产 wiki 新增游戏 → catalog

`reconcile-galgame-works` 的 `--eg-dsn` **缺省即指 catalog PG 服务器上的 `erogamescape` 库**(= §2 灌入的同一 pg,同 user/pass)→ 无需手拼 DSN、无需碰密码:
```bash
dump_env "${PROJ}-catalog-1"
run_tool reconcile-galgame-works            # dry:统计将认领的新增数
run_tool reconcile-galgame-works --apply    # 认领生产 wiki 相对本地快照的新增游戏
```
> 读 wiki(`GalgameDatabase`)+ 写 catalog(`CatalogDatabase`)+ 读 erogamescape(缺省 DSN)——三库都在 `&infra-env` 覆盖内,故 `catalog-1` 的 env-file 足够。**未来 dlsite 波**(缓搬)到来时,按 §2 同法把 `dlsite` 库进生产,再补相应 import cmd。

### 6.d · bangumi id 审计基线

```bash
run_tool audit-bangumi-ids            # 基线统计留证据(不带 --apply)
```

### 6.e · catalog 实体搜索索引首建（生产 Meili)

```bash
dump_env "${PROJ}-catalog-1"
run_tool reindex-catalog              # 三索引:catalog_credit_names/characters/labels
sudo shred -u /tmp/x.env
```

---

## 7 · 服务与消费面验证

```bash
# catalog healthz(容器内自检;服务无公开域名,走 dokploy-network)
sudo docker run --rm --network dokploy-network curlimages/curl:latest -s http://catalog:9281/healthz   # {"status":"ok"}
# S2S 无凭据必 401
sudo docker run --rm --network dokploy-network curlimages/curl:latest -s -o /dev/null -w '%{http_code}\n' -X POST http://catalog:9281/api/v1/catalog/resolve   # 401
```
- **admin 三桶 UI**(生产 apps/web,oauth 前端域):以 admin 账号打开 catalog 审阅队列,三桶(candidates/proposals/probable-refs)可加载、计数合理(**确认桶 ~28,253 量级**)。
- **web catalog 基址**:确认 web 容器有 `NUXT_CATALOG_API_BASE_SSR=http://catalog:9281/api/v1`(`sudo docker inspect ${PROJ}-web-1 --format '{{range .Config.Env}}{{println .}}{{end}}' | grep NUXT_CATALOG`)。
- **Meili 三索引计数**:reindex-catalog 日志 vs `/indexes/*/stats`(约 59,769 / 45,012 / 10,096)。

---

## 8 · 回滚要点

| 对象 | 回滚 | 说明 |
|------|------|------|
| catalog 服务 / 镜像 | Dokploy 常规回退到上一 tag | 无状态,安全 |
| `kun_catalog` / `erogamescape` 库 | `DROP DATABASE` 重灌 §2 | cutover 期无消费者依赖,可反复重来 |
| `oauth_clients.catalog_site` 列 + `galgame_bangumi_meta` 表 | **留着无害** | 都是加法;回滚仅需把 `catalog_site` 置 NULL |
| **wiki 富集(§6a)** | **不可简单回滚** | `enrich-bangumi` 只填空语义(不覆盖已有),但要精确复原需**从 §2.0 的备份 `pre-catalog-cutover_kun_galgame_wiki_*.dump` 还原整库**——仅灾难场景用:`cat <dump> \| sudo docker exec -i "$PG" pg_restore -U postgres --clean --if-exists --no-owner -d kun_galgame_wiki` |

---

## 9 · 执行证据清单（逐项贴回,供「执行核验记录」)

- [ ] §0 本地测试 40 包绿 / `origin/main..main` = 20 提交
- [ ] §1 CI `build-and-push` 全绿(含 infra-catalog / infra-migrate-catalog / `infra-tools`)
- [ ] §2.0 wiki 备份体积(`ls -lh` 输出)
- [ ] §2.3 抽验:works 206,710 / credits 629,207 / 名义 59,769 / bgm_subject 648,663
- [ ] §3 逐服务镜像 sha 为新;`migrate-catalog` exited(0)
- [ ] §4 catalog_site=varchar(64) / galgame_bangumi_meta 存在 / kun_catalog 种子 228·259·7·11·13
- [ ] §5 绑定后 `SELECT … WHERE catalog_site IS NOT NULL` 命中 1 行(wiki 机密 client)
- [ ] §6a enrich-bangumi --apply 统计(~6,093)
- [ ] §6b/e reindex-search + reindex-catalog 计数(~59,769/45,012/10,096)
- [ ] §6c reconcile --apply 认领统计
- [ ] §6d audit-bangumi-ids 基线
- [ ] §7 healthz 200 / resolve 401 / admin 三桶可开(~28,253)/ web env 生效
