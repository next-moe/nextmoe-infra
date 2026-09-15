# 19 · 上游爬虫机(nextmoe-crawler)

> 你在这里:四个上游源(DLsite / ErogameScape / Getchu / HowLongToBeat)的 staging 库
> 要能每天自动刷新,而不是停在一次性快照上。本篇说明这些爬虫部署在哪、为什么在那里、
> 按什么顺序上线。catalog 侧怎么消费这些库,见 `scripts/prod-cron/vndb-refresh/run.sh`。

## 19.0 为什么不是一台机器

两台机器的出口能力**恰好互补**,2026-09-15 两侧同一批 curl 实测:

| 源 | nextmoe-crawler(43.230.161.212,东京) | kungal-neo(194.238.79.49,德国 prod) |
|---|---|---|
| DLsite `info/ajax` + `product.json` | **200 JSON** | 302 → google.com |
| ErogameScape | **000**(ICMP 与 TCP 80/443 全丢) | **200** |
| Getchu / HowLongToBeat | 301(跳 https)/ 200 | 301 / 200 |

DLsite 只有东京打得到;ErogameScape 只有 prod 打得到。后者不是 HTTP 层的地区判断,
是 IP 层丢包 —— 换 UA、加 Referer、改 scheme 都没用。**日本 IP 不是资格**:封的是机房/
滥用 IP 段,这台东京机的 PTR 是 `smtp6.motatijtt4.shop`,整段在名单上;
`kun-erogamescape-api/docs/05` 里「租一台日本 VPS 保底 100% 可行」这句实测为假。

所以四个爬虫都跑在东京,ErogameScape 那一路的出站经 prod 上的**反向中继**绕回去 ——
正是 prod 用东京的 `dlsite-relay` 打 DLsite 的镜像对称做法。

## 19.1 机器与应用

机器:`ssh nextmoe-crawler`,Debian 13,2 vCPU / 3.8 GB / 59 GB,Dokploy 远程服务器,
机上已有 `dokploy-traefik` 和 `dlsite-relay`。**盘要和 `/opt/mengzhan`(2dfan 补丁镜像)分**,
它自称 52 GB 可能不够。

| Dokploy Compose 应用 | 仓库 / compose 路径 | 跑什么 |
|---|---|---|
| crawler-db | nextmoe-infra `docker-compose.crawler-db.yml` | 一个 Postgres,四个库 |
| dlsite | kun-dlsite-api `docker-compose.prod.yml` | `sync,fetch,refresh` 每 24h |
| getchu | kun-getchu-api `docker-compose.prod.yml` | `discover,fetch` 每 24h |
| hltb | kun-howlongtobeat-api `docker-compose.prod.yml` | `sync` 每 24h |
| erogamescape | kun-erogamescape-api `docker-compose.prod.yml` | `refresh` 每 24h |
| eg-relay(**在 prod 上**) | nextmoe-infra `docker-compose.eg-relay.yml` | EG 反向中继 |

四条链的启动分别延后 10m / 1h / 2h / 3h(`--start-after`),这样它们不会在同一分钟一起醒。

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

1. **DNS**:`eg-relay.nextmoe.dev` A 记录指向 prod(194.238.79.49)。
2. **prod 上部署 eg-relay**,然后从东京机验:
   `curl -sS -o /dev/null -w '%{http_code}' -X POST 'https://eg-relay.nextmoe.dev/~ap2/ero/toukei_kaiseki/sql_for_erogamer_form.php'`
   应当不是 403(403 = traefik 的 ipallowlist 没放行这台机)。
3. **东京机上部署 crawler-db**,面板填 `POSTGRES_PASSWORD`。
   四个库由 `docker/crawler-initdb.d` 建 —— 它**只在数据卷是空的那一次**跑;
   之后再加库要手工 `CREATE DATABASE`(prod 上 `kun_news` 就是这么补的)。
4. **灌种子**(见 19.4),再部署四个爬虫应用。
5. **各仓加 `DOKPLOY_WEBHOOK_*` secret**(`_DLSITE` / `_EROGAMESCAPE` / `_HOWLONGTOBEAT`
   / `_GETCHU`),CI 的 deploy 步在没有它时只记一行日志跳过,「提交自动部署」不会生效。

各应用面板要填的:`DATABASE_URL`(指向 `crawler-postgres`,四个库各一个)。其余非密钥
项已内联在 compose 里。

## 19.4 先灌种子,否则第一跑是全量重爬

这四个库在东京机上是**空的**,而 prod 上已经有 2026-06/07 的快照(dlsite 13 GB、
erogamescape 3,795 MB、getchu 718 MB、howlongtobeat 28 MB)。

不灌种子直接开跑,dlsite 的 `sync` 会拿空表算发现前沿,于是从零重新发现约 120 万个
workno —— 几天的活,而且全是本可以不发的请求。EG 的 `refresh` 同理:它只重拉可变表,
空库上等于什么都刷不出来。

从 prod 取 dump 灌进去(prod 侧只读):

```bash
ssh kungal-neo "sudo docker exec kun-visual-novel-infra-vqvqbc-postgres-1 \
  pg_dump -U postgres -Fc -d dlsite" > /tmp/dlsite.dump
# 东京机上 pg_restore --no-owner --no-privileges -d dlsite
```

## 19.5 数据怎么回到生产机:prod 每周主动拉

catalog 的 import 家族读的是 **prod 本机**的那四个 staging 库(`--dlsite-dsn` /
`--eg-dsn` / `--hltb-dsn`,见 `scripts/prod-cron/vndb-refresh/run.sh`),不是 HTTP 面。
桥是 `scripts/prod-cron/crawler-restage/run.sh`,周日 08:00 CST,在 vndb-refresh 之前。

**为什么爬是每天、传是每周**:两个消费方都是周级(bgm 周三 11:00、vndb 周日 17:30)。
每天运 17 GB 去喂一个一周才读一次的任务,买不到任何新鲜度,只是把同一件事付七遍钱。

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
