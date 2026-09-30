# 03 · 部署与运维

## 1. 部署形态

| 服务 | 镜像 | 说明 |
|---|---|---|
| `telemetry` | `ghcr.io/next-moe/infra-telemetry` | 端口 9286；Traefik 只把 `telemetry.nextmoe.dev` 的 `/v1/logs`、`/v1/symbols` 路由进来，没有 http→https 跳转（`/v1/symbols` 带令牌，不能先走明文） |
| `telemetry-worker` | `ghcr.io/next-moe/infra-telemetry-worker` | 内置 `telemetry-decode`、R8 retrace（带校验和）、llvm-symbolizer；`docker/telemetry-worker.Dockerfile` |
| `migrate-telemetry` | `ghcr.io/next-moe/infra-migrate`，参数 `telemetry` | 每次部署运行；幂等 |

管理台通过 `NUXT_TELEMETRY_API_BASE_SSR=http://telemetry:9286/api/v1` 访问管理 API。

## 2. 环境变量

| 变量 | 必填 | 说明 |
|---|---|---|
| `KUN_TELEMETRY_PG_DATABASE` | 共享环境里已设为 `kun_telemetry` | 其余连接参数回落到 `KUN_PG_*` |
| `KUN_TELEMETRY_S3_ENDPOINT`、`KUN_TELEMETRY_S3_BUCKET`、`KUN_TELEMETRY_S3_ACCESS_KEY_ID`、`KUN_TELEMETRY_S3_SECRET_ACCESS_KEY` | 生产必填 | 符号文件的**私有**桶（Cloudflare R2）。未设置时部署直接失败：两个容器不共享磁盘，本地目录只适用于开发 |
| `KUN_TELEMETRY_S3_REGION`、`KUN_TELEMETRY_S3_FORCE_PATH_STYLE` | 否 | 默认 `auto`、`true` |
| `KUN_TELEMETRY_ADMIN_BASE_URL` | 否 | 告警邮件里的管理台链接，默认 `https://admin.nextmoe.dev` |
| `KUN_TELEMETRY_ENGINE_SYMBOLS_BASE_URL` | 否 | 引擎符号源，默认 Google 的 `flutter_infra_release` |
| 邮件 | 共享环境 | 复用 `KUN_VISUAL_NOVEL_EMAIL_*` |

符号文件包含源码路径与函数名，可以反推出混淆前的代码，桶必须是私有的，令牌只授权这一个桶。

## 3. 首次上线

1. 生产 Postgres 执行 `CREATE DATABASE kun_telemetry`（`initdb` 只在空数据目录时运行）。
2. 在 Dokploy 环境里填入上面四个 S3 变量。
3. DNS：`telemetry.nextmoe.dev` 开启 Cloudflare 代理，指向与 `api.nextmoe.dev` 相同的源站。
4. Traefik 文件配置加一条兜底路由：`Host(telemetry.nextmoe.dev)`、优先级 1、指向一个健康检查永远失败的后端，使容器重建期间回 503 而不是 404（客户端会重试 503，遇到 404 则丢弃）。
5. 合并部署后，更新服务器上的 `pg-backup/run.sh`（`scripts/prod-cron` 的脚本是手工安装的副本），否则新库不会按规则备份。
6. 在一次重新部署期间持续探测 `/v1/logs`，确认只出现 503（偶有 502），没有 404。

## 4. 接入一个 App

1. 管理台「应用与密钥」新建应用：服务名即客户端的 `service.name`。
2. 设置本 App 帧前缀，如 `package:kungal/`、`package:nextmoe_`、`dev.nextmoe.`。
3. 把接入 key 交给 App 团队，写进构建变量 `TELEMETRY_KEY`；端点 `TELEMETRY_ENDPOINT=https://telemetry.nextmoe.dev`。
4. 生成 CI 符号令牌（只显示一次），写进仓库机密 `TELEMETRY_SYMBOLS_TOKEN`。
5. 「告警」页添加收件邮箱并发送测试邮件。

轮换接入 key 会让所有已安装的版本立即停止上报，直到发出使用新 key 的新包，只在 key 被滥用时才做。
