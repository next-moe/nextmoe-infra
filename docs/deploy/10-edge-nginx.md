# 10 · 边缘反代:Nginx

经典、可控、生态成熟([2026 实践](https://oneuptime.com/blog/post/2026-01-16-docker-nginx-reverse-proxy/view))。代价是 TLS 与 WebSocket 都要**手动配**(Caddy 自动)。选它通常是因为团队已有 Nginx 运维经验或需要精细的缓存/限流。

> **先读 [09-edge-caddy.md](./09-edge-caddy.md) 的 §9.0 共同前提**(前端用真实域名重建、按容器名回源、转发 `X-Forwarded-Proto`、关闭 1xxxx 直连)与 §9.1 域名映射表——三种反代完全一致,这里不重复。

## 10.1 公共片段

`edge-nginx/conf.d/00-common.conf`(http 级):
```nginx
# WebSocket 升级所需(当前聊天走 HTTP,无活跃 WS;此为前瞻/无害)
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

# 反代统一头(SSR 取协议/host、Secure cookie、真实 IP 全靠它)
proxy_http_version 1.1;
proxy_set_header Host              $host;
proxy_set_header X-Real-IP         $remote_addr;
proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
proxy_set_header X-Forwarded-Proto $scheme;
proxy_set_header X-Forwarded-Host  $host;
proxy_set_header Upgrade           $http_upgrade;     # WS
proxy_set_header Connection        $connection_upgrade;
proxy_read_timeout 86400s;                            # WS 长连接不被 60s 掐断

gzip on;
gzip_types text/plain text/css application/json application/javascript application/xml image/svg+xml;
```

## 10.2 站点(以两个为例,其余照此模式)

`edge-nginx/conf.d/account.conf`:
```nginx
# HTTP → HTTPS(并放行 ACME 验证)
server {
    listen 80;
    server_name account.nextmoe.com;
    location /.well-known/acme-challenge/ { root /var/www/certbot; }
    location / { return 301 https://$host$request_uri; }
}
server {
    listen 443 ssl;
    http2 on;
    server_name account.nextmoe.com;
    ssl_certificate     /etc/letsencrypt/live/account.nextmoe.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/account.nextmoe.com/privkey.pem;

    location /api/v1/ { proxy_pass http://kun-galgame-infra-oauth-1:9277; }
    location /oauth/jwks { proxy_pass http://kun-galgame-infra-oauth-1:9277; }
    location /.well-known/ { proxy_pass http://kun-galgame-infra-oauth-1:9277; }
    location /       { proxy_pass http://kun-galgame-infra-account-1:3000; }
}
```
`edge-nginx/conf.d/admin.conf`:
```nginx
server {
    listen 80;
    server_name admin.nextmoe.dev;
    location /.well-known/acme-challenge/ { root /var/www/certbot; }
    location / { return 301 https://$host$request_uri; }
}
server {
    listen 443 ssl;
    http2 on;
    server_name admin.nextmoe.dev;
    ssl_certificate     /etc/letsencrypt/live/admin.nextmoe.dev/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/admin.nextmoe.dev/privkey.pem;

    location /api/v1/ { proxy_pass http://kun-galgame-infra-oauth-1:9277; }
    location /       { proxy_pass http://kun-galgame-infra-admin-1:3000; }
}
```
`edge-nginx/conf.d/moyu.conf`:
```nginx
server {
    listen 80; server_name www.moyu.moe;
    location /.well-known/acme-challenge/ { root /var/www/certbot; }
    location / { return 301 https://$host$request_uri; }
}
server {
    listen 443 ssl; http2 on; server_name www.moyu.moe;
    ssl_certificate     /etc/letsencrypt/live/www.moyu.moe/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/www.moyu.moe/privkey.pem;
    location /api/v1/ { proxy_pass http://moyu-api-1:5214; }
    location /       { proxy_pass http://moyu-web-1:3000; }
}
```
其余域名(`www.kungal.com`→kungal、`image.kungal.com`→MinIO)照 [§9.1](./09-edge-caddy.md) 映射复制(`wiki.kungal.com` 已于开放 API Phase 2 · W5 退役,galgame 富读走 catalog internal 面 s2s)。`www.kungal.com` 若重启 Socket.IO,给 `location /socket.io/ { proxy_pass http://kungal-api-1:2334; }` 即可(公共片段里的 Upgrade 头会生效)。

> Docker DNS 是动态的,`proxy_pass` 直接写容器名有时需用变量 + resolver 才能在目标重启后自动重解析:
> ```nginx
> resolver 127.0.0.11 valid=30s;
> set $up http://kun-galgame-infra-account-1:3000; proxy_pass $up;
> ```

## 10.3 部署(Nginx + certbot 自动续期)

`edge-nginx/docker-compose.yml`:
```yaml
name: edge
services:
  nginx:
    image: nginx:1.27-alpine
    restart: unless-stopped
    ports: ["80:80", "443:443"]
    volumes:
      - ./conf.d:/etc/nginx/conf.d:ro
      - ./certbot/www:/var/www/certbot:ro
      - ./certbot/conf:/etc/letsencrypt:ro
  certbot:
    image: certbot/certbot
    restart: unless-stopped
    volumes:
      - ./certbot/www:/var/www/certbot:rw
      - ./certbot/conf:/etc/letsencrypt:rw
    # 每 12h 尝试续期
    entrypoint: sh -c 'trap exit TERM; while :; do certbot renew --webroot -w /var/www/certbot; sleep 12h & wait $${!}; done'
networks:
  default: { name: kun-galgame-infra_default, external: true }
```

首次签发(webroot 验证,需 80 端口公网可达):
```bash
cd edge-nginx
docker compose up -d nginx
for d in account.nextmoe.com admin.nextmoe.dev www.kungal.com www.moyu.moe image.kungal.com; do   # wiki.kungal.com 已于 W5 退役
  docker compose run --rm certbot certonly --webroot -w /var/www/certbot \
    -d "$d" --email admin@kungal.com --agree-tos --no-eff-email
done
docker compose up -d
docker compose exec nginx nginx -s reload
```

## 10.4 验证
```bash
docker compose exec nginx nginx -t           # 配置语法
curl -I https://account.nextmoe.com             # 200/302 + 证书有效
curl -I https://www.moyu.moe                 # 首页经反代(/healthz 仅容器内部探活)
```

## 10.5 注意
- **TLS、续期、WS 升级头都要手动**——这是 Nginx 相对 Caddy 的主要成本。
- 用容器名回源时配 `resolver 127.0.0.11`(Docker 内置 DNS),否则容器重启换 IP 后 Nginx 仍指旧 IP。
- 偏好「label 驱动、自动 TLS」的 Nginx 方案可用 [`nginx-proxy` + `acme-companion`](https://github.com/nginx-proxy/nginx-proxy)(给各容器打 `VIRTUAL_HOST`/`LETSENCRYPT_HOST` label,自动生成 vhost + 证书),但那样就和 Caddy 的简洁度趋同了——若图省事直接选 [Caddy](./09-edge-caddy.md)。
- 80/443 须公网可达做 ACME;在 NAT/dae 后无法开入站则用 [Cloudflare Tunnel](./11-cloudflare-tunnel.md)。
