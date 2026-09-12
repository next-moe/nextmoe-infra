# 11 · 边缘反代:Cloudflare Tunnel

> **生产高并发不建议**:全部流量经**单个 `cloudflared` 进程**汇聚,它是吞吐 / 连接瓶颈(每隧道默认 100 连接上限;CF 官方建议高流量跑 ≥2 个 4C4G replica),**并发一高易 [Error 1033](https://developers.cloudflare.com/support/troubleshooting/http-status-codes/cloudflare-1xxx-errors/error-1033/)(找不到健康连接器)**。本生态线上改用 **Dokploy + Cloudflare 代理(橙云 / CDN)+ 防火墙锁 CF 段**(见 [QUICKSTART §10 方案 A](./QUICKSTART.md))。**本篇仅适用**:服务器无公网 IP / 在 NAT·CGNAT·dae 之后、80/443 无法对外的场景。

`cloudflared` 与 Cloudflare 边缘建立**纯出站**长连接,把公网流量隧道回本机——**无需开放任何入站端口、无需公网 IP、不用动路由器/防火墙**([cloudflared 镜像](https://infra.docker.com/r/cloudflare/cloudflared)、[2026 实践](https://dev.to/mihailtd/secure-self-hosting-with-cloudflare-tunnels-and-docker-zero-trust-security-5bbn))。TLS 由 Cloudflare 边缘终止,DDoS/缓存/WAF 顺带白嫖。

**这是本机最合适的方案**:你这台在 dae 之后、入站不一定可达,而 Tunnel 全程出站,**dae 那套 `lan_interface` 对它也无所谓**(cloudflared 自己出站,被 dae 代理也能连 CF)。

## 11.0 何时选它
- 服务器在 NAT / CGNAT / dae 之后,开不了 80/443 入站 → Caddy/Nginx 的 ACME 走不通,选它。
- 想要 Cloudflare 的边缘 TLS / 缓存 / 防护,且域名已托管在 Cloudflare。

## 11.1 前提
- 同 [09-edge-caddy.md §9.0](./09-edge-caddy.md):**前端仍要用真实 https 域名重建**(浏览器打的是 CF 边缘的 https),OAuth client 的 redirect_uri 改 https 域名,反代按**容器名**回源。
- 一个 Cloudflare 账号,且 `kungal.com` / `moyu.moe` 的 DNS 托管在 Cloudflare。
- **回源可以是明文 http**(cloudflared → 容器在内网),所以下游服务无需自带证书。

## 11.2 建隧道,拿凭据
仪表盘 **Zero Trust → Networks → Tunnels → Create**,或 CLI:
```bash
docker run -it --rm -v "$PWD/cf:/home/nonroot/.cloudflared" cloudflare/cloudflared tunnel login
docker run -it --rm -v "$PWD/cf:/home/nonroot/.cloudflared" cloudflare/cloudflared tunnel create kungal-eco
# → 生成 cf/<TUNNEL_ID>.json 凭据 + cf/cert.pem
```

## 11.3 配置文件模式(多服务更可复现)
`cf/config.yml`(ingress 按 **hostname + path** 匹配,**顺序优先、首条命中**):
```yaml
tunnel: kungal-eco
credentials-file: /etc/cloudflared/<TUNNEL_ID>.json

ingress:
  # —— 枢纽 account(OP + 账户中心)——
  - hostname: account.nextmoe.com
    path: ^/(api/v1/|oauth/jwks|.well-known/)
    service: http://kun-galgame-infra-oauth-1:9277
  - hostname: account.nextmoe.com
    service: http://kun-galgame-infra-account-1:3000
  # —— 枢纽 admin(管理台)——
  - hostname: admin.nextmoe.dev
    path: ^/api/v1/
    service: http://kun-galgame-infra-oauth-1:9277
  - hostname: admin.nextmoe.dev
    service: http://kun-galgame-infra-admin-1:3000

  # —— 枢纽 galgame-wiki:已退役(开放 API Phase 2 · W5,2026-07)——
  #   wiki.kungal.com 域 + 独立 galgame(:9280)+ wiki 前端均退役;galgame 富读
  #   改走 catalog internal 面(s2s,nm_ key)。此 ingress 段不再需要。

  # —— kungal 论坛(/api + 将来的 /socket.io 自动支持 WS)——
  - hostname: www.kungal.com
    path: ^/(api|socket\.io)/
    service: http://kungal-api-1:2334
  - hostname: www.kungal.com
    service: http://kungal-web-1:7777

  # —— moyu 补丁站 ——
  - hostname: www.moyu.moe
    path: ^/api/v1/
    service: http://moyu-api-1:5214
  - hostname: www.moyu.moe
    service: http://moyu-web-1:3000

  # —— 图床(自托管回源 MinIO bucket)——
  - hostname: image.kungal.com
    service: http://kun-galgame-infra-minio-1:9000
    originRequest:
      httpHostHeader: image.kungal.com   # 按需,路径前缀可在 MinIO 侧用 bucket 策略处理

  # 兜底:其余一律 404
  - service: http_status:404
```
> `path` 是正则。WebSocket 由 cloudflared **自动支持**,无需额外配置。`X-Forwarded-*` 由 Cloudflare 边缘注入。

## 11.4 部署(cloudflared 容器,加入生态网络,零端口)
`edge-cf/docker-compose.yml`:
```yaml
name: edge-cf
services:
  cloudflared:
    image: cloudflare/cloudflared:latest
    restart: unless-stopped
    command: tunnel --no-autoupdate run
    volumes:
      - ../cf:/etc/cloudflared:ro     # config.yml + <TUNNEL_ID>.json
    # 注意:无 ports!纯出站
networks:
  default: { name: kun-galgame-infra_default, external: true }
```
> 也可用 **token 模式**(仪表盘管理 ingress,容器只需 `TUNNEL_TOKEN` 环境变量、连 config.yml 都省了):`command: tunnel --no-autoupdate run --token ${TUNNEL_TOKEN}`。多服务自建更推荐上面的配置文件模式([对比](https://docker.recipes/devops/cloudflared-tunnel))。

```bash
cd edge-cf && docker compose up -d
docker compose logs -f cloudflared | grep -iE "registered tunnel connection|ERR"
```

## 11.5 DNS 路由
让每个 hostname 指向隧道(给每个域名建一条 CNAME 到 `<TUNNEL_ID>.cfargotunnel.com`):
```bash
for d in account.nextmoe.com admin.nextmoe.dev www.kungal.com www.moyu.moe image.kungal.com; do   # wiki.kungal.com 已于 W5 退役
  docker run --rm -v "$PWD/cf:/etc/cloudflared" cloudflare/cloudflared tunnel route dns kungal-eco "$d"
done
```
(或仪表盘里给每个 Public Hostname 自动建记录。)Cloudflare 的橙云(代理)开启即获边缘 TLS。

## 11.6 验证
```bash
curl -I https://account.nextmoe.com             # 200/302,证书是 Cloudflare 签的
curl -I https://www.moyu.moe                # 首页经隧道(/healthz 仅容器内部探活)
# 隧道健康
docker compose -f edge-cf/docker-compose.yml logs cloudflared | grep -i "Registered tunnel connection"
```

## 11.7 注意
- **完全不开入站端口**,所以可以(也应该)把各仓 compose 的 `1xxxx` host 端口全删/绑回环——外部只经 Cloudflare 进来。
- 回源用容器名,cloudflared 与目标必须**同在 `kun-galgame-infra_default` 网络**。
- 大文件/上传:Cloudflare 免费版有响应体大小与超时限制(图床大图、补丁包建议走 R2/B2 直链,不经隧道)。
- 凭据文件 `cf/<TUNNEL_ID>.json` 是**机密**,勿入镜像/git。
- 这是三种里**唯一不依赖入站可达**的;dae 机器、家宽、CGNAT 都能用。
