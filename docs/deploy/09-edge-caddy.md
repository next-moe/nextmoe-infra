# 9 · 边缘反代:Caddy(推荐)

Caddy 是这套自托管栈最优雅的默认选择([Caddy 官方](https://caddyserver.com/docs/quick-starts/reverse-proxy)、[2026 实践](https://nerdleveltech.com/caddy-reverse-proxy-docker-compose-production-https-tutorial)):**自动 HTTPS**(自动签发 + 续期 Let's Encrypt 证书)、WebSocket 零配置透传、HTTP/3、配置文件极简。

## 9.0 共同前提(三种反代都适用)

1. **前端必须用真实域名重新构建**。当前镜像把 public 配置烘焙成了 `http://localhost:1xxxx`(见 [02-build.md](./02-build.md))——浏览器会去连 localhost。上线前用真实 https 域名重建各 web:
   ```bash
   # 例:infra account 前端
   docker build -f docker/nuxt.Dockerfile --build-arg APP=account \
     --build-arg PUBLIC_API_BASE=https://account.nextmoe.com/api/v1 \
     --build-arg PUBLIC_IMAGE_CDN_BASE=https://image.kungal.iloveren.link \
     -t nextmoe-infra/account .
   # (infra wiki 前端 + wiki.kungal.com 域已于开放 API Phase 2 · W5 退役,不再构建)
   ```
   moyu/kungal 同理(`PUBLIC_*` / `OAUTH_*` 改真实域名)。**OAuth client 的 redirect_uri 也要在枢纽里改成 https 域名**(见 [03-bootstrap.md](./03-bootstrap.md) A.5)。
2. **拓扑**:反代作为**容器**加入 `kun-galgame-infra_default` 网络,按**唯一容器名**回源(生态里 `web`/`api` 这两个服务别名在三个项目间冲突,必须用容器名)。上线后可**不再发布** 1xxxx host 端口,仅反代暴露 80/443。
3. **转发协议头**:务必转发 `X-Forwarded-Proto`/`Host`,否则下游 SSR 生成的绝对 URL、OAuth 跳转、`Secure` cookie 会错(Caddy 默认就带,Nginx 需手动)。
4. **WebSocket**:当前聊天已改 HTTP(`useSocketIO` 已停用),无活跃 WS;Caddy 即便将来重启 Socket.IO 也**自动透传**,无需配置。

## 9.1 域名 → 后端映射

| 公网域名 | 路径 | 后端容器:端口 |
|---|---|---|
| `account.nextmoe.com` | `/api/v1/*`、`/oauth/jwks`、`/.well-known/*` | `kun-galgame-infra-oauth-1:9277` |
| `account.nextmoe.com` | 其余 | `kun-galgame-infra-account-1:3000`(账户中心) |
| `admin.nextmoe.dev` | `/api/v1/*` | `kun-galgame-infra-oauth-1:9277` |
| `admin.nextmoe.dev` | 其余 | `kun-galgame-infra-admin-1:3000`(管理台) |
| ~~`wiki.kungal.com`~~ | — | **已退役(开放 API Phase 2 · W5)**:域 + 独立 galgame(:9280)+ wiki 前端退役;galgame 富读走 catalog internal 面(s2s) |
| `www.kungal.com` | `/api/*`(+ `/socket.io/*` 若启用) | `kungal-api-1:2334` |
| `www.kungal.com` | 其余 | `kungal-web-1:7777` |
| `www.moyu.moe` | `/api/v1/*` | `moyu-api-1:5214` |
| `www.moyu.moe` | 其余 | `moyu-web-1:3000` |
| `image.kungal.iloveren.link` | `/*` | 对象存储(自托管 → `kun-galgame-infra-minio-1:9000`/bucket;生产建议 R2/B2 + CDN) |

## 9.2 Caddyfile

`edge/Caddyfile`:
```caddyfile
# 全局:用真实邮箱接 Let's Encrypt 到期通知
{
	email admin@kungal.com
}

# —— 枢纽 account(OP + 账户中心) ——
account.nextmoe.com {
	handle /api/v1/* {
		reverse_proxy kun-galgame-infra-oauth-1:9277
	}
	handle /oauth/jwks {
		reverse_proxy kun-galgame-infra-oauth-1:9277
	}
	handle /.well-known/* {
		reverse_proxy kun-galgame-infra-oauth-1:9277
	}
	reverse_proxy kun-galgame-infra-account-1:3000
}

# —— 枢纽 admin(管理台) ——
admin.nextmoe.dev {
	handle /api/v1/* {
		reverse_proxy kun-galgame-infra-oauth-1:9277
	}
	reverse_proxy kun-galgame-infra-admin-1:3000
}

# —— 枢纽:galgame-wiki:已退役(开放 API Phase 2 · W5,2026-07)——
#   wiki.kungal.com 域 + 独立 galgame(:9280)+ wiki 前端均退役;galgame 富读改走
#   catalog internal 面(s2s,nm_ key)。此站点块不再需要。

# —— kungal 论坛 ——
www.kungal.com {
	handle /api/* {
		reverse_proxy kungal-api-1:2334
	}
	# 若将来重启 Socket.IO:Caddy 自动透传 WebSocket,无需额外配置
	reverse_proxy kungal-web-1:7777
}

# —— moyu 补丁站 ——
www.moyu.moe {
	handle /api/v1/* {
		reverse_proxy moyu-api-1:5214
	}
	reverse_proxy moyu-web-1:3000
}

# —— 图床(自托管:回源 MinIO 的 bucket)——
image.kungal.iloveren.link {
	rewrite * /kun-images{uri}
	reverse_proxy kun-galgame-infra-minio-1:9000
}
```
> Caddy 默认就会带 `X-Forwarded-For/Proto/Host`、自动签发并强制 HTTPS、HTTP→HTTPS 跳转。`reverse_proxy` 对 WebSocket 自动透传。多副本可加 `lb_policy least_conn`(长连接更优,见 [Caddy 实践](https://oneuptime.com/blog/post/2026-01-16-docker-caddy-automatic-https/view))。

## 9.3 部署(容器,加入生态网络)

`edge/docker-compose.yml`:
```yaml
name: edge
services:
  caddy:
    image: caddy:2-alpine
    restart: unless-stopped
    ports: ["80:80", "443:443", "443:443/udp"]   # udp=HTTP/3
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy_data:/data        # 证书持久化,否则重启会重签触发 LE 限额
      - caddy_config:/config
networks:
  default:
    name: kun-galgame-infra_default
    external: true
volumes:
  caddy_data:
  caddy_config:
```
```bash
cd edge && docker compose up -d
```

> **证书卷必须持久化**(`caddy_data:/data`)——否则每次重启都重新签发,会撞 Let's Encrypt 限额([见实践](https://nerdleveltech.com/caddy-reverse-proxy-docker-compose-production-https-tutorial))。

## 9.4 收尾:关闭直接暴露的 host 端口
反代就绪后,把各仓 compose 里 `ports: ["1xxxx:..."]` 改成只绑本机回环(`127.0.0.1:1xxxx:...`)或整段删掉,只让 Caddy 的 80/443 对外。防火墙只放行 80/443。

## 9.5 验证
```bash
curl -I https://account.nextmoe.com            # 200/302 + 有效证书
curl -I https://www.moyu.moe                # 首页经反代(/healthz 是容器内部探活,不公开路由)
docker compose -f edge/docker-compose.yml logs caddy | grep -i "certificate obtained"
```

## 9.6 注意
- DNS 要先把这些域名 A/AAAA 指到本机公网 IP,Caddy 才能完成 ACME HTTP/TLS 验证(80/443 必须公网可达;若在 NAT/dae 后无法开放入站,改用 [11-cloudflare-tunnel.md](./11-cloudflare-tunnel.md))。
- 容器名 `kun-galgame-infra-account-1` 等带 `-1` 副本序号;若给服务 `--scale` 或改了项目名会变,届时更新 Caddyfile(或给各 web 加唯一网络别名后用别名)。
- `image.kungal.iloveren.link` 生产更推荐直接用 R2/B2 + Cloudflare CDN,不经本机反代回源 MinIO。
