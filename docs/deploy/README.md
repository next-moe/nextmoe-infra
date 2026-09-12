# 鲲 Galgame 生态 — Docker 部署与运维文档

三仓一体的容器化部署指南。本目录按章节分文件编写,**建议按顺序阅读**。

> **拿到一台全新裸机**?先做 [SERVER-SETUP.md](./SERVER-SETUP.md) 服务器开荒(登录 / 系统更新 / 建用户 `kun` / SSH 加固禁 root+密码 / 防火墙 + fail2ban / 克隆仓库),再进 QUICKSTART 部署。
>
> **服务器已就绪、只想快速上线**?直接看 [QUICKSTART.md](./QUICKSTART.md) —— 到三站上线的精简步骤(**Dokploy**:内置 Traefik 反代 + 自动 HTTPS,无需 Caddy)。下面的分章文档是其展开与原理。
>
> **Dokploy 已装好、要照着一步步上线**?用 [17-go-live-checklist.md](./17-go-live-checklist.md) —— 带勾选框、每步标注**在哪配什么环境变量**的上线清单。
>
> **踩坑速查**:开荒/部署中实际翻过的车(SSH 权限 / Docker 绕过 ufw / Dokploy 面板 3000 / CF Tunnel 1033 / pg18 卷路径 / 按库迁移 …)集中在 [NOTES.md](./NOTES.md),遇到问题先翻这里。

| 章节 | 文件 |---|---|---|
| 0 | [00-architecture.md](./00-architecture.md) | 架构总览:三仓、服务拓扑、网络、端口、数据库映射 |
| 1 | [01-prerequisites.md](./01-prerequisites.md) | 前置条件:Docker、构建网络、buildx 现状、仓库布局 |
| 2 | [02-build.md](./02-build.md) | 镜像构建:infra 的 cgo/distroless 拆分、moyu/kungal、构建参数 |
| 3 | [03-bootstrap.md](./03-bootstrap.md) | **首次启动**:基础设施、建库、跨仓迁移顺序、OAuth 客户端注册 |
| 16 | [16-data-cutover.md](./16-data-cutover.md) | **带数据上线**:从 dump 起,服务器容器化逐条命令(`*-tools` 镜像)把老库数据迁进新 5 库;本地 `go run` ↔ 容器对照表、校验、回滚(03-bootstrap §B 的可抄命令版) |
| 17 | [17-go-live-checklist.md](./17-go-live-checklist.md) | **上线 Checklist**:Dokploy 装好后到三站上线的勾选清单,每步标注**在哪(面板/`docker/*.env`)配什么环境变量** + 密钥一致性铁律 + 验收/烟雾测试 |
| 18 | [18-config-center.md](./18-config-center.md) | **配置中心**:运行期可调项(开关/阈值/TTL/配额)的三层解析(代码默认 → 环境变量地板 → 数据库覆盖值)、平台策略与站点覆盖、后台任务的开关与时刻、下发给站点的读面、控制台 `/settings` 与 API、日志验证、从环境变量迁入的顺序、新增键的做法 |
| 4 | [04-run.md](./04-run.md) | 日常启停:增量启动 / 伞状编排、kungal 的 infra override |
| 5 | [05-configuration.md](./05-configuration.md) | 配置参考:各服务 env、前端 public 配置烘焙、密钥(上线速查) |
| 15 | [15-environment.md](./15-environment.md) | **环境变量大全**:三仓每个变量 + 构建参数 + CI secrets + 生产/Dokploy 注入 + Cloudflare/R2;分层模型、命名规律、跨服务一致性铁律、必改清单(配置层底层全集) |
| 6 | [06-operations.md](./06-operations.md) | 运维:健康/日志、升级、备份恢复、迁移 job、扩缩容 |
| 7 | [07-troubleshooting.md](./07-troubleshooting.md) | 故障排查:实跑中踩到的每一个坑 + 解法 |
| 12 | [12-dokploy.md](./12-dokploy.md) | **Dokploy 部署(线上推荐)**:单服务器自托管 PaaS,内置 Traefik 反代 + 自动 SSL + 编排;含真实域名映射与改造清单 |
| 13 | [13-registry-ci.md](./13-registry-ci.md) | **镜像 Registry + CI 构建**:GHCR + GitHub Actions 在 CI build → 推 GHCR → Dokploy 拉预构建镜像(生产机零构建);镜像清单 / workflow / tag 回滚 / prod compose 用 `image:` |
| 14 | [14-backup-restore.md](./14-backup-restore.md) | **数据库备份与还原**:Postgres(5 库)手动/自动/异地备份、各类还原场景与演练;Redis/MinIO/OpenSearch 取舍;PITR 进阶 |
| 9 | [09-edge-caddy.md](./09-edge-caddy.md) | 手动边缘反代 · Caddy(**不用 Dokploy 时**):自动 HTTPS、域名映射、§9.0 共同前提 |
| 10 | [10-edge-nginx.md](./10-edge-nginx.md) | 手动边缘反代 · Nginx:手动 TLS(certbot)、WS 升级头、容器名回源 |
| 11 | [11-edge-cloudflare-tunnel.md](./11-edge-cloudflare-tunnel.md) | 手动边缘反代 · Cloudflare Tunnel:纯出站、零入站端口(NAT/dae 后首选) |
| 附录 | [08-dae-dev-proxy.md](./08-dae-dev-proxy.md) | **仅开发机**:dae 透明代理下让容器走代理(生产纯净,勿叠加) |

> **线上采用单服务器 + Dokploy**(见 [12-dokploy.md](./12-dokploy.md)):它内置 Traefik 已是反代,**与 09-11 三选一互斥,勿叠加**。线上域名:kungal=`kungal.com`/`www.kungal.com`、moyu=`moyu.moe`/`www.moyu.moe`、OP=`account.nextmoe.com`、管理台=`admin.nextmoe.dev`、image=`image.kungal.iloveren.link`(`wiki.kungal.com` 已于开放 API Phase 2 · W5 退役)。

## 30 秒速览

- **三个仓库**:`nextmoe-infra`(枢纽 / infra)、`kun-galgame-forum`(kungal / 论坛)、`kun-galgame-patch`(moyu / 补丁站)。
- **枢纽拥有共享基础设施**:一套 Postgres(5 个库)、Redis、MinIO(S3)、OpenSearch。kungal/moyu 按服务名连过来。
- **每仓 = 无状态 api + web 容器**;Go 服务多阶段编译,Nuxt 出自包含 `.output`。
- **全部 host 端口在 `1xxxx` 段**,与本机 `air` 开发服务共存。
- 整套在测试机上**已实跑通过**:13 个容器全 healthy,跨仓服务名连通已验证。

## 一条命令看全局

```bash
docker ps --format '{{.Names}}\t{{.Status}}' | grep -E 'kun-galgame-infra-|moyu-|kungal-' | sort
```

> 文档里所有密钥、密码均为**测试值**(`191007` / `kun-docker-test-*` / `minioadmin`)。生产部署见 [05-configuration.md](./05-configuration.md) 的「密钥」一节,务必全部轮换。
