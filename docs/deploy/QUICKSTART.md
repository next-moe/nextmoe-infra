# 全新 Debian 服务器 · 一键部署三站(精简版 · Dokploy)

> 从一台空 Debian 到 **kungal / moyu / NextMoe 平台栈** 全部上线的步骤,每步带一句说明。
> 用 **Dokploy**(自托管 PaaS):它**内置 Traefik 反代 + 自动 Let's Encrypt SSL + 编排**,所以**不需要 Caddy/Nginx**,也别再叠加。
> 深入原理见 [12-dokploy.md](./12-dokploy.md)(部署/路由)+ [13-registry-ci.md](./13-registry-ci.md)(镜像从哪来)。
>
> **服务器还是全新裸机**?先做 [SERVER-SETUP.md](./SERVER-SETUP.md)(登录 / 系统更新 / 建用户 `kun` / SSH 加固 / 防火墙 / 克隆仓库),再回这里。**本篇假设你已用 `kun` 登录、仓库已 clone 到 `~/app`**。

**线上域名**:`kungal.com`/`www.kungal.com`、`moyu.moe`/`www.moyu.moe`、`account.nextmoe.com`、`admin.nextmoe.dev`、`image.kungal.iloveren.link`(走 Cloudflare R2,**不经本机**)。(`wiki.kungal.com` 已于开放 API Phase 2 · W5 退役。)

---

## 1. DNS

把下列域名的 **A 记录指向服务器公网 IP**(`image.*` 指向 Cloudflare,不指本机):
`kungal.com`、`www.kungal.com`、`moyu.moe`、`www.moyu.moe`、`account.nextmoe.com`、`admin.nextmoe.dev`

> **要隐藏源站 IP**(强烈建议)见 [§10](#10-隐藏源站-ip--防泄漏强烈建议):**本项目**给这些记录开 **Proxy(橙云 / CDN)** 并把防火墙锁到 Cloudflare 段(方案 A);仅当服务器无公网 IP / 在 NAT 后才考虑 Tunnel(方案 B,高并发易 1033)。下面 §1–§9 是"公网直连"基线,§10 在其上加固。

## 2. 装 Dokploy(自动装 Docker)

```bash
sudo apt update && sudo apt -y install ufw            # 防火墙
sudo ufw allow OpenSSH && sudo ufw allow 80 && sudo ufw allow 443 && sudo ufw --force enable
#   80/443 = Traefik 对外(§10 隐藏源站后改为"只放 Cloudflare 段")
curl -sSL https://dokploy.com/install.sh | sudo sh        # sudo 加在 sh 上(不是 curl):管道后半段才是 root 跑脚本
```
> `sudo curl … | sh` 是**错的** —— `sudo` 只管 `curl`,`sh` 仍以普通用户跑 → 脚本报 `This script must be run as root`。更稳:`curl -fsSL …/install.sh -o /tmp/dokploy.sh && sudo sh /tmp/dokploy.sh`(还能先看一眼脚本)。

装完浏览器开 `http://<服务器IP>:3000` 注册管理员(**立刻设强密码**)。

> **装前自查(避坑)**:① 内存 ≥ 2G、磁盘 ≥ 30G(本项目走 GHCR 预构建已大减构建负载);② **80/443/3000 必须空闲**,别先装别的 web 服务;③ 必须 **root 直接装、不要在 LXC 容器内**;④ 脚本用 `curl ifconfig.me` 取公网 IP 来 `docker swarm init` —— 该服务不可达会装失败,确认能出站访问它,或装后手动 `docker swarm init --advertise-addr <你的IP>`;⑤ 磁盘写满会让 Dokploy 内置 DB 进 recovery、面板打不开 → 定期 `docker system prune`。
> Dokploy 把应用跑成 **Docker Swarm 服务**(不是普通 `docker compose`);且**面板 3000 默认对全网开放、ufw 还管不住它**(Docker 绕过 ufw)→ 安全收口见 [§9](#9-收尾),务必做。

## 3. 镜像:CI 构建 → GHCR(不在生产机 build)

本生态 13 个重镜像(cgo + 4×Nuxt),**别在生产机构建**(会拖垮单机)。各仓已带 `.github/workflows/build.yml`:

```bash
# 本机:三仓各自 push 到默认分支(infra=main,kungal/moyu=master)
git push        # → GitHub Actions 自动 build 并推 ghcr.io/next-moe/*(:latest + :<sha>)
```
- 到 GitHub 各仓 **Packages**,把镜像设为 **public** → Dokploy 免凭证拉;私有则在 Dokploy 配 `read:packages` 的 PAT。
- *起步捷径*:不想配 CI → 第 4 步应用直接选 **Git source** 让 Dokploy 在服务器上 build(简单,但重镜像有拖垮单机风险)。

## 4. Dokploy 建 3 个 Compose 应用

面板 → **Create → Compose**,各对应一个 Git 仓库,Compose 文件指向各仓 **`docker-compose.prod.yml`**(已用 `image: ghcr.io/next-moe/*` + `expose` + `dokploy-network`):

| 应用 | 仓库 | Compose 文件 |
|---|---|---|
| `nextmoe-infra` | infra | `docker-compose.prod.yml` |
| `kun-galgame-forum` | kungal | `docker-compose.prod.yml` |
| `kun-galgame-patch` | moyu | `docker-compose.prod.yml` |

三应用共享 Dokploy 提供的 `dokploy-network`(external),跨应用按枢纽**唯一服务名**(`postgres`/`redis`/`oauth`/`galgame`/`image`)互通。

## 5. 配置环境变量(只填密钥,域名已写死在 prod compose)

三仓 `docker-compose.prod.yml` 已把**非密钥/域名写死在 `environment:`**,**密钥用 `${VAR}` 从各应用的 Dokploy Environment 面板取**——所以**不用放任何 `docker/*.env`**,每个应用面板只填这几个密钥(**务必轮换所有测试值**):

- **infra 面板**:`POSTGRES_PASSWORD`、`JWT_SECRET`、`MINIO_ROOT_USER`、`MINIO_ROOT_PASSWORD`、`KUN_IMAGE_S3_ENDPOINT`/`_ACCESS_KEY`/`_SECRET_KEY`(R2)、(可选 `KUN_VISUAL_NOVEL_EMAIL_PASSWORD`)
- **kungal 面板**:`POSTGRES_PASSWORD`(=infra)、`OAUTH_CLIENT_SECRET`(§6 拿到后填)、`JWT_SECRET`(kungal 自己的)、(可选 B2/MAIL/image client)
- **moyu 面板**:`POSTGRES_PASSWORD`(=infra)、`OAUTH_CLIENT_SECRET`、`KUN_VISUAL_NOVEL_S3_STORAGE_ACCESS_KEY_ID`/`_SECRET_ACCESS_KEY`(B2)、(可选 MAIL)

> 真实 https 域名、OAuth client_id、服务名 base、CDN 域、CORS 都已是 prod compose 里的字面值;改前端域名改对应 web 服务的 `NUXT_PUBLIC_*`。infra 前端域名烤在 CI(build.yml)。逐项见 [15-environment §15.8](./15-environment.md) 与 [17-go-live-checklist.md](./17-go-live-checklist.md)。

## 6. 部署顺序 + 建库 + 注册 OAuth client

1. **先部署 infra**,等 `postgres`/`redis`/`minio`/`opensearch` healthy。
2. 在 infra 应用的 **Terminal** 跑首启迁移:
   ```bash
   docker compose -f docker-compose.prod.yml run --rm migrate           # kun_galgame_infra:表 + 站点/角色种子
   docker compose -f docker-compose.prod.yml run --rm migrate-catalog   # wiki 两族 + catalog:表 + 约束(W5 单一入口;部署也自动跑)
   ```
3. **注册 OAuth client**(否则前端登录走不通,不在任何 migrate 种子里):登录 infra 管理端建 **论坛 / 补丁 / wiki** 三个 client(`redirect_uri` 填各自 https 回调),把生成的 **secret 写回** kungal/moyu 应用的 **Environment 面板**(`OAUTH_CLIENT_SECRET`)。见 [03-bootstrap §A.5](./03-bootstrap.md) + [12-dokploy §12.3](./12-dokploy.md)。
4. **再部署 kungal、moyu**;各自 Terminal 跑 `docker compose -f docker-compose.prod.yml --profile jobs run --rm migrate`(清理型迁移,空库打印「无迁移」即正常)。

> 要导入旧 Node 站点真实数据 → [03-bootstrap §B](./03-bootstrap.md) + `docs/migration`(`migrate-users` 是分水岭,**严格按序**)。

## 7. 挂域名(Traefik 自动签 SSL)

每个对外服务在应用的 **Domains** 标签加记录(域名 + 路径 + 目标服务 + 容器内端口);同域 `/api*` 与 `/` 各一条(更具体的路径优先)。Dokploy 自动注入 Traefik labels 并签 Let's Encrypt 证书:

> Traefik 的 LE 用 HTTP 验证,要求该域名签发时是 **DNS-only(灰云)**;若按 [§10](#10-隐藏源站-ip--防泄漏强烈建议) 开了橙云/隧道,改用 **DNS-01** 验证或**关掉 LE 由 Cloudflare 出证**(别一边橙云一边等 HTTP 验证,会签不出)。

| 公网域名 | 路径 | 应用 | 目标服务:端口 |
|---|---|---|---|
| `account.nextmoe.com` | `/api/v1` | infra | `oauth:9277` |
| `account.nextmoe.com` | `/` | infra | `account:3000`(账户中心) |
| `admin.nextmoe.dev` | `/api/v1` | infra | `oauth:9277` |
| `admin.nextmoe.dev` | `/` | infra | `admin:3000`(管理台) |
| ~~`wiki.kungal.com`~~ | — | infra | **已退役(W5,2026-07)**:compose labels 已删、域 404,DNS 待删;galgame 富读走 catalog internal 面(s2s) |
| `kungal.com` + `www.kungal.com` | `/api` | kungal | `kungal-api:2334` |
| `kungal.com` + `www.kungal.com` | `/` | kungal | `web:7777` |
| `moyu.moe` + `www.moyu.moe` | `/api/v1` | moyu | `moyu-api:5214` |
| `moyu.moe` + `www.moyu.moe` | `/` | moyu | `web:3000` |

> `image.kungal.iloveren.link` 走 Cloudflare R2(由 CF 直供 blob),**不在 Dokploy 挂域名**;`image` 服务(`:9278`)是 s2s 内部,不对外。

## 8. 验证上线

```bash
curl -I https://account.nextmoe.com     # 302→登录 + 有效证书
curl -I https://www.kungal.com
curl -I https://www.moyu.moe
```
Dokploy 各应用看 **Logs / 健康状态**。**回滚** = 镜像引用从 `:latest` 临时改某个 `:<git-sha>` 再 redeploy。

## 9. 收尾

- **收口面板 3000(重要)**:Dokploy 用 Docker 发布 3000,**Docker 会绕过 ufw**(自己写 iptables),`ufw deny 3000` 挡不住、面板默认对全网开放。三选一**真正**收口:
  1. **挂域名 + HTTPS**(推荐,顺带把 `http://IP:3000` 变成 https):Dokploy → **Settings → Server** → 填 **Web Server Domain** = `panel.kungal.com` + 管理员 Email + 开 **HTTPS(Let's Encrypt)** → Save。前提:A 记录 `panel.kungal.com → 服务器IP`,且签发时 80/443 对 LE 可达(走 CF 橙云则签发期临时设 DNS-only,或用 DNS-01)。之后用 `https://panel.kungal.com`(面板自带登录),再用下面任一方式封掉直连 3000;
  2. **SSH 隧道访问**:`ssh -L 3000:127.0.0.1:3000 kungal-neo` 后开 `http://localhost:3000`,并用**云厂商防火墙**(在 Docker iptables 之前生效)封公网 3000;
  3. 装 [`ufw-docker`](https://github.com/chaifeng/ufw-docker) 让 ufw 真正管住 Docker 端口,再拒绝 3000。
  > 你的 Cloudflare 只代理 80/443 的域名,**3000 不在其保护内**,务必单独收口。
- **用户 / 注册控制**:**首个注册的账号 = Owner(最高管理员)**;此后 Dokploy 是**邀请制** —— `/register` 不再开放自助注册(建好 Owner 后访问注册页会跳登录),新成员由 Owner 在 **Settings → Users** 邀请并分配角色。免费版角色 **Owner / Admin / Member**(Member 可按权限点精细授权;自定义角色是 Enterprise)。所以**无需额外"禁止注册"**——默认就是关的;稳妥起见开隐身窗口访问 `/register` 确认它跳登录,并务必把面板按上一条收口(域名 / SSH 隧道),让登录/注册页根本不对公网暴露。
- **移除 Dokploy 时**:Docker 是独立系统包(`docker-ce`),**不会变孤儿、也不会被连带卸掉**;但 Dokploy 会留下 Swarm 模式 +`dokploy`/`dokploy-traefik`/`dokploy-postgres`/`dokploy-redis` 服务 +`dokploy-network`+ 卷,按[官方卸载](https://docs.dokploy.com/docs/core/uninstall)清理(`docker service rm …` → `docker system prune --all --volumes` → `docker swarm leave --force`)。注意你的应用是 Swarm 服务,迁出 Dokploy 需改回普通 compose。
- **图床**:走 Cloudflare R2(`image.env` 配 R2 凭证,Cloudflare 直供,不经服务器);自托管 MinIO 才需在 Dokploy 给 `image.*` 挂域名回源 `minio:9000`。
- **持续更新**:push 代码 → CI 重 build 推 GHCR → CI 的 `deploy` job(Dokploy webhook,放 GitHub Secret 的 `DOKPLOY_WEBHOOK_*`)触发拉新镜像滚动更新(见 [13-registry-ci.md](./13-registry-ci.md))。
- **备份**:用 Dokploy 自带备份,或 Terminal 跑 `pg_dumpall`,见 [06-operations.md](./06-operations.md)。

## 10. 隐藏源站 IP / 防泄漏(强烈建议)

§1–§9 默认在公网 80/443 上跑 Traefik + LE,**A 记录直指服务器 → 源站 IP 暴露**(LE 证书也进 CT 日志)。若 IP 泄漏,攻击者可绕过 Cloudflare 直打源站(DDoS/扫描)。**本项目采用方案 A**(Dokploy + Cloudflare 代理 / CDN);方案 B(Tunnel)仅作「无公网 IP / NAT 后」的备选。

### 方案 A · Cloudflare 代理(橙云 / CDN)+ 防火墙锁 CF 段(推荐 · 本项目采用)

保留 §1–§9 的公网 80/443 ——**Dokploy 的 Traefik 在源站直接扛并发,不经隧道单点**。① 所有公网记录开 **Proxy(橙云)**:即获得 CDN/缓存 + DDoS 防护 + 隐藏源站;② 防火墙**只放 Cloudflare 段**访问 80/443(即便 IP 泄漏,非 CF 来源也直连不进):
```bash
for ip in $(curl -s https://www.cloudflare.com/ips-v4) $(curl -s https://www.cloudflare.com/ips-v6); do
  sudo ufw allow from "$ip" to any port 80,443 proto tcp comment 'cloudflare'
done
sudo ufw delete allow 80 && sudo ufw delete allow 443     # 删掉对所有人开放的 80/443(SSH 必须保留!)
```
> CF 段会变,用 [ufw-cf](https://github.com/Malith-Rukshan/ufw-cf) 或 [cloudflare-ufw-updater](https://github.com/jakejarvis/cloudflare-ufw-updater) 定时同步。
> **SSL**:橙云会拦截 LE 的 HTTP 验证 → Traefik 改用 **DNS-01**(填 Cloudflare API token)或挂 **Cloudflare Origin CA 证书**,CF 的 SSL 模式设 **Full (strict)**。

### 方案 B · Cloudflare Tunnel(备选:无公网 IP / NAT 后;高并发慎用)

源站**只出不进、零入站端口**,IP 永不进 DNS。但**全部流量经单个 `cloudflared` 进程汇聚**,它是吞吐 / 连接瓶颈(每隧道默认 100 连接上限;CF 官方建议高流量跑 **≥2 个 4C4G replica**),**并发一高就容易 [Error 1033](https://developers.cloudflare.com/support/troubleshooting/http-status-codes/cloudflare-1xxx-errors/error-1033/)(找不到健康连接器)**。所以**本项目不用它**,仅当「服务器无公网 IP / 在 NAT 后、80/443 无法对外」时才考虑:
```bash
docker run -d --name cloudflared --restart unless-stopped --network dokploy-network \
  cloudflare/cloudflared:latest tunnel --no-autoupdate run --token <TUNNEL_TOKEN>
# CF 隧道里 hostname → http://dokploy-traefik:80;Dokploy 域名关掉 LE(CF 边缘出证);ufw 关 80/443 只留 SSH
```
DNS 此时是 CNAME 到 `<id>.cfargotunnel.com`。详见 [11-edge-cloudflare-tunnel.md](./11-edge-cloudflare-tunnel.md)。

### 防泄漏清单(无论哪种方案)

- **DNS 审计**:所有 `A/AAAA/MX/TXT/SPF/`旧子域都不能含源站 IP;别留 `dev.`/`staging.` 等直指源站的记录(会被 DNS 数据集 / CT 日志搜出)。
- **邮件**:本项目走**外部 SMTP 中继**(mxroute `tuesday.mxrouting.net:587`)**仅出站发信**,源站**不收信、不跑邮件服务**,因此 **ufw 无需为邮件开任何入站端口**(MX 指向 mxroute 而非源站);只要别用源站直发到「不存在地址」触发退信暴露 IP 即可。
- **历史 IP**:若该 IP 曾公开过(DNS 历史 / SecurityTrails / Shodan),上 CF 后**换一个新服务器 IP**——旧记录是公开档案,改不掉。
- **证书**:方案 A(Origin CA)/ 方案 B 源站不对外暴露公网 LE 证书 → 不进 CT;方案 A(DNS-01)证书只暴露域名(本就公开),IP 仍被防火墙挡住。
- **持续监控**:隐藏源站是长期事,定期用 [Cloudflare 官方指南](https://developers.cloudflare.com/fundamentals/security/protect-your-origin-server/) 自查。
