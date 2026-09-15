# 19 · 上游爬虫机(nextmoe-crawler)

> 你在这里:四个上游源(DLsite / ErogameScape / Getchu / HowLongToBeat)的 staging 库
> 要能每天自动刷新,而不是停在一次性快照上。本篇说明这些爬虫部署在哪、为什么在那里、
> 按什么顺序上线。catalog 侧怎么消费这些库,见 `scripts/prod-cron/vndb-refresh/run.sh`。

## 19.0 出口能力恰好互补,所以爬虫分两处

2026-09-15 两侧同一批 curl 实测:

| 源 | nextmoe-crawler(43.230.161.212,东京) | kungal-neo(194.238.79.49,德国 prod) |
|---|---|---|
| DLsite `info/ajax` + `product.json` | **200 JSON** | 302 → google.com |
| ErogameScape | **000**(ICMP 与 TCP 80/443 全丢) | **200**,0.76s |
| Getchu / HowLongToBeat | 301(跳 https)/ 200 | 301 / 200 |

DLsite 只有东京打得到;ErogameScape 只有 prod 打得到。后者不是 HTTP 层的地区判断,
是 IP 层丢包 —— 换 UA、加 Referer、改 scheme 都没用。**日本 IP 不是资格**:封的是机房/
滥用 IP 段,这台东京机的 PTR 是 `smtp6.motatijtt4.shop`,整段在名单上;
`kun-erogamescape-api/docs/05` 里「租一台日本 VPS 保底 100% 可行」这句实测为假。

所以 **ErogameScape 的爬虫跑在 prod,另外三个跑在东京**。两处都不需要中继。

### 为什么不是「四个都在东京 + prod 上架一个反向中继」

这是本篇的前一版设计,**在部署前撤回**。中继需要一个解析到 prod 的公开主机名,而
prod 的地址就是 `api.nextmoe.dev` 前面那层 Cloudflare 的全部依仗 —— 实测
`curl --resolve api.nextmoe.dev:443:194.238.79.49` 直接 **200**,源站不校验来源,
没有 CF IP 白名单也没有 Authenticated Origin Pulls。整个 zone 目前一条泄漏都没有
(`nextmoe.dev` / `www` / `mail` 无 A 记录、无 MX),这条会是第一条。

三个变体都查过,都不行:

- **随机主机名**(`eg-relay-<rand hex 32>.nextmoe.dev`):traefik 的
  `certresolver: letsencrypt` 发的是**单名证书**(实测 `dlsite-relay.nextmoe.dev`
  和 `api.nextmoe.dev` 的 SAN 各只有自己一个名字),而 Let's Encrypt 每张证书都必须
  提交公开的 Certificate Transparency 日志。crt.sh 查 `%.nextmoe.dev` 现成就能列出
  `beszel` / `openpanel` / `rybbit` / `website-screenshot` / `edit-ui` / `dm` 等一串
  从未对外宣传的名字。随机名在签发后几分钟就是公开的,而且它藏的是名字,要藏的是 IP。
- **改放 letmoe-forge**:它是 `194.238.79.88`,prod 是 `194.238.79.49` —— 同一个 /24,
  同一家 mycloudvps.de(PTR `shard-hill-c0r2ri` vs `stack-acorn-8x0ctp`),
  而且 `forge.letmoe.com` 同样是橙云、同样没暴露过。暴露它等于把找 prod 的搜索空间
  从整个 IPv4 缩到 256 个地址,配合上面那个直连 200,一遍扫描就命中。
- **WireGuard 私链**:能解决泄漏,但为一条每天一次的 HTTP 调用引入一条要维护的隧道,
  而且它断了 EG 爬虫会静默停。

**能留下来的规则**:跨机调用只能 **prod → 东京**,不能反向。东京机的地址本来就该公开
(它就是那台可以被上游封掉的一次性出口机),已在产的 `dlsite-relay.nextmoe.dev`
灰云指 43.230.161.212 就是这个形状。中继反了方向,所以搬的是爬虫,不是流量。

## 19.1 机器与应用

东京:`ssh nextmoe-crawler`,Debian 13,2 vCPU / 3.8 GB / 59 GB(实测剩 52 GB),
Dokploy 远程服务器,机上已有 `dokploy-traefik` 和 `dlsite-relay`。

| Dokploy Compose 应用 | 在哪台 | 仓库 / compose 路径 | 跑什么 |
|---|---|---|---|
| crawler-db | 东京 | nextmoe-infra `docker-compose.crawler-db.yml` | 一个 Postgres,三个库 |
| dlsite | 东京 | kun-dlsite-api `docker-compose.prod.yml` | `sync,fetch,refresh` 每 24h |
| getchu | 东京 | kun-getchu-api `docker-compose.prod.yml` | `discover,fetch` 每 24h |
| hltb | 东京 | kun-howlongtobeat-api `docker-compose.prod.yml` | `sync` 每 24h |
| erogamescape | **prod** | kun-erogamescape-api `docker-compose.prod.yml` | `refresh` 每 24h,直连 |

东京三条链的启动分别延后 10m / 1h / 2h(`--start-after`),这样它们不会在同一分钟
一起醒。prod 上那个是 10m,单纯为了别在部署窗口里开爬 —— `--start-after` 从容器启动
计时,瞄不准钟点。

**只跑爬虫,不跑 `serve`,不跑 Meilisearch**:消费方是 catalog 的 import 家族,
它直接用 DSN 读这些表做批量 join,从不调那个 HTTP 面。

## 19.2 调度在二进制里,不在面板

四个爬虫都支持 `--every D`(周期)和 `--start-after D`(首跑延后),所以调度跟着
compose 走 Git,不落在 Dokploy 面板里。三条行为是刻意的:

- 周期从一趟的**开始**计时,慢跑不会让下一趟越推越晚;一趟跑超周期就立刻再来。
- **失败的一趟只记日志不退出**。退出会更糟:容器被编排器重启,一次上游抖动就变成
  反复重启砸源站。一次性模式(不带 `--every`)仍然照旧退非零。
- SIGTERM 立即退 0,包括在 `--start-after` 等待期间。

命令位可以是逗号链(`sync,fetch,refresh`),按顺序跑、第一个失败就停并指名。
**必须是一个容器**:同一个爬虫的各 phase 共用同一把库级 advisory lock,拆成多个容器的话
先起的那个拿住锁,其余每一轮都失败,永远轮不到。

## 19.3 上线顺序

1. **东京机 `docker login ghcr.io`**。四个镜像全是 private,而东京机上
   `/root/.docker/config.json` **不存在**(prod 上有)。不补,Dokploy 拉不动镜像。
   本地 gh token 已带 `read:packages`,token 走 stdin 不进 argv:

   ```bash
   gh auth token | ssh nextmoe-crawler 'docker login ghcr.io -u KunMoe --password-stdin'
   ```

2. **东京机上部署 crawler-db**,面板填 `POSTGRES_PASSWORD`。
   三个库由 `docker/crawler-initdb.d` 建 —— 它**只在数据卷是空的那一次**跑;
   之后再加库要手工 `CREATE DATABASE`(prod 上 `kun_news` 就是这么补的)。

3. **灌种子**(见 19.4)。

4. **灌完各跑一次 `migrate`**(见 19.4 末尾)。dump 带的是取快照那天的表结构,
   而日常链里没有 `migrate`。

5. **部署三个东京爬虫应用**,各填一个 `DATABASE_URL` 指向 `crawler-postgres`。

6. **prod 上部署 erogamescape**,`DATABASE_URL` 指向 prod 自己的 Postgres。
   它读写的 `erogamescape` 库本来就在那儿,不用灌种子,也不进 19.5 的周搬运。

7. **各仓加 `DOKPLOY_WEBHOOK_*` secret**(`_DLSITE` / `_EROGAMESCAPE` / `_HOWLONGTOBEAT`
   / `_GETCHU`),CI 的 deploy 步在没有它时只记一行日志跳过,「提交自动部署」不会生效。
   验收不能停在「workflow 绿了」—— webhook 收 200 不等于部署成功,判据是箱上容器的
   `StartedAt` 变了。

其余非密钥项(周期、`--start-after`、`DLSITE_REFRESH_RECENT_DAYS`、`GETCHU_RPS`)
都已内联在各自 compose 里,**不要在面板重复设**。

## 19.4 先灌种子,否则第一跑是全量重爬

东京机上那三个库是**空的**,而 prod 上已经有 2026-06/07 的快照(实测
dlsite 13 GB、getchu 718 MB、howlongtobeat 28 MB;erogamescape 3795 MB 留在 prod 原地)。

不灌种子直接开跑,dlsite 的 `sync` 会拿空表算发现前沿,于是从零重新发现约 120 万个
workno —— 几天的活,而且全是本可以不发的请求。

灌种子要一条 prod → 东京 的路,**和 19.5 的 restage 是同一把 key**,所以先建它
(步骤在 `scripts/prod-cron/crawler-restage/run.sh` 头部),这样 14 GB 直接一跳,
不用从本地过两遍。

在 prod 上,**挂 tmux**(长跑,ssh 断了 SIGHUP 会杀掉),按从小到大的顺序 ——
让问题在 28 MB 那个身上暴露,而不是在 13 GB 跑完之后:

```bash
tmux new -s seed
mkdir -p /root/seed && cd /root/seed
ssh nextmoe-crawler 'mkdir -p /root/seed'
for db in howlongtobeat getchu dlsite; do
  docker exec kun-visual-novel-infra-vqvqbc-postgres-1 \
    pg_dump -U postgres -Fc -Z3 -d "$db" > "$db.dump"
  ls -lh "$db.dump"
  scp "$db.dump" nextmoe-crawler:/root/seed/
  rm -f "$db.dump"
done
```

东京机上恢复(两边都是 PG 18.4):

```bash
for db in howlongtobeat getchu dlsite; do
  docker run --rm --network dokploy-network \
    --env-file /root/crawler-db.env -v /root/seed:/seed:ro postgres:18-alpine \
    pg_restore -h crawler-postgres -U postgres -d "$db" \
               --no-owner --no-privileges "/seed/$db.dump"
done
rm -rf /root/seed
```

**然后各跑一次 `migrate`**。四个仓的 `schema.sql` 都是 `CREATE ... IF NOT EXISTS` /
`DROP COLUMN IF EXISTS`,幂等且只做加法,跑几次都安全;不跑的话,快照之后加过的列
就一直缺着,而日常链不会补:

```bash
umask 077
read -rsp 'crawler-db password: ' PW; echo
for pair in dlsite:kun-dlsite-api getchu:kun-getchu-api howlongtobeat:kun-howlongtobeat-api; do
  printf 'DATABASE_URL=postgres://postgres:%s@crawler-postgres:5432/%s?sslmode=disable\n' \
    "$PW" "${pair%%:*}" > /root/.mig.env
  docker run --rm --network dokploy-network --env-file /root/.mig.env \
    "ghcr.io/kunmoe/${pair##*:}:latest" migrate && echo "  ${pair%%:*} OK"
done
rm -f /root/.mig.env; unset PW
```

口令只进一个 0600 的临时文件,不进 argv —— 容器的 argv 在宿主 `ps` 里是可见的。

## 19.5 数据怎么回到生产机:prod 每周主动拉

catalog 的 import 家族读的是 **prod 本机**的那四个 staging 库(`--dlsite-dsn` /
`--eg-dsn` / `--hltb-dsn`,见 `scripts/prod-cron/vndb-refresh/run.sh`),不是 HTTP 面。
`erogamescape` 由 prod 上的爬虫直接写,天然是本地的、日级新鲜的;另外三个的桥是
`scripts/prod-cron/crawler-restage/run.sh`,周日 08:00 CST,在 vndb-refresh 之前。

**为什么爬是每天、传是每周**:两个消费方都是周级(bgm 周三 11:00、vndb 周日 17:30)。
每天运 14 GB 去喂一个一周才读一次的任务,买不到任何新鲜度,只是把同一件事付七遍钱。

**为什么是 prod 主动拉**:爬虫机是这套里最不可信的一台 —— 它存在的意义就是当那个可能
被上游封掉的东西。这条路上它不持有我们的任何凭据、也不为此开端口:私钥在 prod 这边,
它自己那侧要的库口令是一个 root-only 文件,两边都没有秘密进过命令行。

**三道闸**(本地用 ephemeral 库原样跑过,含阴性对照):

1. **体积下限**按**上一次成功的那份**算(70%),不是写死的数字 —— 这些库会长,唯一
   诚实的「够大」是「没比上周小多少」。首跑没有基线,只记录。
2. **行数下限**:恢复到 `<db>_next` 之后,拿它的主表行数和**正在服务的那份**比(70%)。
   体积闸过得去但内容被掏空的 dump 由它拦下。
3. **占用守卫**:`ALTER DATABASE ... RENAME` 要求库上没有别的会话。周级消费方只在
   跑的时候连,所以这里还有连接就说明有别的东西攥着它 —— 此时**不换**并报错退出,
   掐一个不知道是谁的连接不是这个任务该做的决定。

恢复永远落在 `<db>_next` 上再改名换过去,不是 `--clean` 盖在活库上 —— 后者会让活库在
一次 13 GB 恢复的全程处于空或半载状态,中途失败就一直那样躺着直到有人发现。
