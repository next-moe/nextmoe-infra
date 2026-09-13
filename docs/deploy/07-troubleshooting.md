# 7 · 故障排查

下面每一条都是**本次实跑中真实踩到的**,按「现象 → 原因 → 解法」整理。

## 构建期

### B1 · oauth/image 构建报 `build constraints exclude all Go files`
- **现象**:`CGO_ENABLED=0 go build ./cmd/oauth`(或 image)失败,提示 `kolesa-team/go-webp/encoder ... build constraints exclude all Go files`。
- **原因**:go-webp 是 cgo-only(无纯 Go 回退)。`image` 编码 WebP 直接用它;**`oauth` 经内嵌的图床 admin 间接 import**(`image/service` → `processor` → go-webp)。
- **解法**:这两个用 `docker/cgo.Dockerfile`(`CGO_ENABLED=1` + 构建期 `libwebp-dev` + 运行期 `libwebp7`),**不要**走 `go.Dockerfile`。`galgame` 和迁移工具纯 Go,继续用 `go.Dockerfile`。判定:`go list -deps ./cmd/X | grep go-webp`,非空即需 cgo。

### B2 · `the --mount option requires BuildKit`
- **原因**:本机无 `docker buildx`,`docker compose build` 用 legacy builder,不支持 `--mount=type=cache`。
- **解法**:三仓 Dockerfile 已移除 cache mount(仅普通层缓存)。装了 buildx 可自行加回。构建时的 `requires buildx plugin` 警告无害。

### B3 · cgo runtime 的 libwebp 包名随 Debian 版本变
- **现象**:运行阶段 `Unable to locate package libsharpyuv0`(bookworm),或缺 `libsharpyuv0` 导致 `libwebp.so` 加载失败(trixie)。
- **原因**:**libwebp 版本不同,sharpyuv 的打包方式不同**——bookworm(libwebp 1.2)把 sharpyuv 打进 `libwebp7`(无独立包);**trixie(libwebp 1.5)拆出独立 `libsharpyuv0`**。
- **解法**:跟随基镜的 Debian 版本装对应包。当前 `cgo.Dockerfile` 用 **trixie**,运行阶段装 `libwebp7 libsharpyuv0`(已配)。构建器与运行时的 Debian 版本必须一致(都 trixie),否则 `libwebp-dev`(build)与 `libwebp.so`(run)版本错配。

### B4 · Nuxt 构建报 `packages/ui/.nuxt/tsconfig.app.json ... no such file`
- **原因**:`@kun/ui` 是 Nuxt **layer**,需自己的 `.nuxt`;但 `pnpm install --ignore-scripts` 跳过了它的 `prepare`,且 `.dockerignore` 剥掉了主机的 `.nuxt`。
- **解法**:`nuxt.Dockerfile` 在 app 构建前先 `pnpm --filter @kun/ui run prepare`。(`--ignore-scripts` 本身是必须的——deps 阶段还没拷源码,apps 的 `postinstall: nuxt prepare` 会失败。)

## kungal 专属

### K1 · kungal/moyu `docker compose up` 报 `network kun-galgame-infra_default not found`
- **现象**:在 kungal 或 moyu 目录 `docker compose up` 报找不到外部网络(`build` 不受影响)。
- **原因**:两仓 base 都声明了 `networks.default.external: kun-galgame-infra_default`,该网络由 **infra** 创建 —— infra 没先起来,网络就不存在。
- **解法**:先 `cd nextmoe-infra && docker compose up -d`(它建网络 + 基础设施),再回下游 `docker compose up -d api web`。(注:kungal 已和 moyu 同构,**不再有** `docker-compose.infra.yml` / `standalone.yml`。)

### K2 · kungal api 启动即退出(无明显日志)
- **原因**:`OAUTH_CLIENT_ID` / `OAUTH_CLIENT_SECRET` 是 `requireEnv`,**空则 fail-fast**。kungal 仓自带的 `api.env` 这俩默认是空的。
- **解法**:填非空值(`kungal-web` / 注册时的 secret)。见 [05-configuration.md](./05-configuration.md)。

### K3 · kungal api 连不上库
- **原因**:kungal 自带 `api.env` 用的是仓库默认值——DB 密码 `kungal_dev_pw`(infra 是 `191007`)。
- **解法**:接 infra 时把密码改 `191007`、`JWT_SECRET` 填共享密钥。

## 跨仓 / 基础设施

### I4 · 下游报「数据库不存在」/ 服务起来但业务接口 500
- **原因 a**:`kungalgame` / `kungalgame_patch` 没建。initdb 脚本**只在数据卷首次初始化时跑一次**;复用旧卷不会补建。
  - **解法**:`docker exec ...postgres... psql -U postgres -c "CREATE DATABASE kungalgame" -c "CREATE DATABASE kungalgame_patch"`。
- **原因 b**:库是**空 schema**——各仓 `migrate` 是清理型,假设 dump 已恢复;空库上它打印「没有待执行的迁移」。健康端点 OK,但业务查询无表 → 报错。
  - **解法**:走完整数据 Bootstrap(恢复 dump + 跨仓迁移),见 [03-bootstrap.md](./03-bootstrap.md) B 节。

### I5 · OAuth 登录跳转后报错 / 拿不到令牌
- **原因**:对应 OAuth client 没注册到枢纽(client 不在任何 migrate 种子里)。
- **解法**:在 infra 管理端注册 client 或入 `oauth_clients` 表,secret 按 `sha256:` 哈希存,并让下游 `OAUTH_CLIENT_SECRET` 等于明文。见 [03-bootstrap.md](./03-bootstrap.md) A.5。

### I6 · 容器起来了但外部 curl 不通(连接拒绝)
- **原因**:服务绑了 `127.0.0.1`。
- **解法**:容器内必须绑 `0.0.0.0`(`KUN_FIBER_SERVER_HOST=0.0.0.0` / `KUN_IMAGE_SERVICE_HOST=0.0.0.0` / Nuxt `HOST=0.0.0.0`,均已在 env/Dockerfile 设好)。

### I7 · host 端口冲突
- **原因**:本机 `air` 开发服务占了 9277/9281 等。
- **解法**:整套 host 端口用 `1xxxx` 段(见 [00-architecture.md](./00-architecture.md) 端口表),与 dev 共存。

### I8 · 浏览器拿到的图 / API 地址是 `127.0.0.1:9277`(连不上)
- **原因**:前端 public 配置(`apiBase`/`imageCdnBase`)用了 in-config 默认值,没在 build 时烘焙正确的 host/公网地址。
- **解法**:构建时传 `PUBLIC_*` build args(或运行时 `NUXT_PUBLIC_*`),指向 host 端口 / 真实域名。见 [02-build.md](./02-build.md) + [05-configuration.md](./05-configuration.md)。

### I9 · 容器内 `go mod download` / 外呼超时,但宿主机能连(透明代理)
- **原因**:宿主机用了 dae 之类内核级透明代理,只代理本机流量,不代理 docker 网桥的**转发**流量。**仅开发机**问题。
- **解法**:见附录 [08-dae-dev-proxy.md](./08-dae-dev-proxy.md)(dae override + `lan_interface`)。**生产机不涉及**。

### I10 · 前端报 CORS:`No 'Access-Control-Allow-Origin' header`(preflight 失败)
- **现象**:`http://127.0.0.1:15008` 的页面 fetch `http://localhost:15005/api/v1/auth/refresh` 被 CORS 拦,预检无 ACAO。
- **原因**:**`127.0.0.1` 与 `localhost` 是不同 origin**(尽管同 IP)。API 的 CORS 是精确匹配白名单 + `AllowCredentials:true`,白名单里只有 `localhost:15008`,页面 origin 是 `127.0.0.1:15008` → 不匹配 → 预检被拒。前端 `apiBase` 烘焙成 `localhost`,而你用 `127.0.0.1` 打开页面就会触发。
- **本地解法(已配)**:让 CORS 白名单**同时含 `localhost` 和 `127.0.0.1`** 两套源。各 API 的 env:infra 用 `KUN_FRONTEND_CORS_ORIGIN`,moyu/kungal 用 `CORS_ALLOW_ORIGINS`,都已填两套。或简单点:**始终用 `localhost` 访问**(与烘焙的 apiBase 一致)。
- **线上解法**:生产每个站点的 **web 与它的 API 同源**(反代把 `account.nextmoe.com/api/v1/*`+`/oauth/jwks`+`/.well-known/*`→oauth api、其余→账户前端;`admin.nextmoe.dev/api/v1/*`→oauth api、其余→管理台;`www.moyu.moe/*`→web、`/api/v1/*`→api,见 [09-edge-caddy.md](./09-edge-caddy.md))。**同源请求根本不触发 CORS**,`/auth/refresh` 直接通。仅当前端跨域调别的子域 API 时,才把那个 API 的 CORS 白名单设成真实 https 域名(`https://www.kungal.com` 等);同时确保反代转发 `X-Forwarded-Proto`,使 `Secure` cookie 生效。

## 一条命令快速体检

```bash
docker ps --format '{{.Names}}\t{{.Status}}' | grep -E 'kun-galgame-infra-|moyu-|kungal-' | sort
# 期望:13 个容器,Go/Nuxt 服务均 (healthy)
```
