# NextMoe 开放 API 与开发者平台 文档

> 一句话定位:把 **NextMoe 开放 API**(生态作品数据的只读能力,按媒介/域分「面」)安全、自助地开放给**第三方开发者**;配套 **NextMoe 开发者平台**(开发者门户)负责注册应用、领取凭证、查看用量、阅读文档。**不引入重型 API 网关**,而是复用已有的 OAuth2 IdP + Fiber + Redis + Cloudflare + Nuxt,把「开发者平台」那薄薄一层做进现有体系。

本目录是该设计的单一来源。原单文件设计稿(v2.1)在 2026-07-22 按主题拆成下列编号文档;**章节号(§1–§15)是跨文档稳定锚点**——代码注释按 `§N` 引用本设计,拆分只改文件名、不改章节号。拆分后的新增拍板以 08+ 续编,章节号自 **§16** 起续用同一锚点空间。

## 文档索引

| # | 文件 | 内容(原 §) | 状态 |
|---|------|-----------|------|
| 01 | [design.md](./01-design.md) | 定位 / 命名约定 · 背景与目标(§1)· 域名与部署拓扑(§2)· 跨面互链与面挂载(§3.3/§3.4)· 分期(§13)· 关键决策/待定(§15) | 已完成 |
| 02 | [public-api.md](./02-public-api.md) | 公开 API 面与端点:原则(§3.1)· v1 端点清单(§3.2)· 稳定性承诺 + 演进条款(§3.5)· OpenAPI 策略(§10) | 已完成 |
| 03 | [auth-and-tiers.md](./03-auth-and-tiers.md) | 认证与授权(API Key / OAuth2 / scope / 校验路径,§4)· 限流 + 配额 + 分层(§7) | 已完成 |
| 04 | [platform-internals.md](./04-platform-internals.md) | 数据模型(§5)· 请求生命周期 / 中间件链(§6)· 缓存(§8)· 可观测 / 计量(§12) | 已完成 |
| 05 | [developer-portal.md](./05-developer-portal.md) | 开发者门户 `developer.nextmoe.dev`(§9) | 已完成 |
| 06 | [security-compliance.md](./06-security-compliance.md) | 安全 / 滥用 / 合规:NSFW 默认(能力闸已退役)、来源投影(D1 再分发)、CORS、ToS、审计(§11) | 已完成 |
| 07 | [migration-and-ops.md](./07-migration-and-ops.md) | 迁移与运维提醒:主库迁移、新域名 / CF、Redis 键空间、契约登记、面服务中间件(§14) | 已完成 |
| 08 | [downstream-faces-and-sdk.md](./08-downstream-faces-and-sdk.md) | 下游开放 API = 面联邦(§16)· OpenAPI 契约与客户端 SDK / Flutter(§17) | 拍板 2026-07-23,触发式执行 |
| 09 | [mcp-server.md](./09-mcp-server.md) | MCP server:公开 /v1 只读面的纯透传协议适配(架构裁决 · M1 七工具面 · 认证/计量复用 · 部署) | 拍板 2026-07-23,M1 执行中 |
| 10 | [native-app-integration.md](./10-native-app-integration.md) | 原生桌面应用(Tauri / Wails)以用户访问令牌读 `/v2/catalog`:双凭证读面 · 环回回调 + PKCE · 钥匙串 · 按用户配额(§18) | 拍板 2026-09-06,已实现 |

> 各 Phase 的实施进度见 [01 §13 分期](./01-design.md)。战略上位(开放 API 计划与 galgame-wiki 退役,W0–W5 波次)见 `refs/docs/nextmoe-draft/19`;工程任务书见 `refs/plans/05-open-api`(Phase 1)与 `refs/plans/09-open-api-phase2`(Phase 2)。

## 命名约定(全文固定)

- **NextMoe 开放 API** = 对外开放的只读 HTTP API 总称(`api.nextmoe.dev`),内部按媒介/域分**面**(face):**catalog 面**(跨媒介身份/图谱)、**galgame 面**(galgame 内容,v1 唯一的内容面)、(未来)manga / novel / anime 面。
- **NextMoe 开发者平台** = 开发者门户 + 凭证/配额/用量管理(`developer.nextmoe.dev`)。
- 开发者账号 = 鲲 Galgame 账户(IdP;后续随品牌升级更名「NextMoe 账户」——同一账号体系,改名不阻塞本设计)。

## 关键决策速查(详见 [01 §15](./01-design.md))

- **域名 = `nextmoe.dev` 族**(`api.` / `developer.`);`nextmoe.com` 留给本体揭幕。base URL 是对第三方最贵的契约,品牌迁移必须在第一个外部消费者出现前完成。
- **公开投影 = 聚合记录(D1,2026-07-14 拍板)**——逐字段多源归并 + `attribution` 块,**不做逐源原始字段的批量再分发**;评分 = 逐源数值 + 归源链接。详见 [06 §11](./06-security-compliance.md)。
- **加法优先、永不改语义**——五条演进条款约束「什么是加性、什么必须升版本」;破坏性变更走 `/v2` 并行 + ≥12 月迁移窗口。详见 [02 §3.5](./02-public-api.md)。
- **一份契约三类消费者**——一方站点(forum / moyu / letmoe)以 `internal` tier 真实消费同一开放 API;门户展示的 API = 全部真实 API。
- **不上网关**——各面服务本地鉴权中间件(JWKS 验签 / introspection + Redis 缓存)+ Traefik 路径分面;没有集中网关单点。
- **公开读可被 Cloudflare 边缘缓存**——鉴权与响应内容解耦,缓存键不含 key;这是「开放 API 代价可控」的承重墙。详见 [04 §8](./04-platform-internals.md)。
- **下游开放 API = 面联邦,不建第二平台**——kungal / moyu / letmoe 未来的公开小面并入本平台作新 face(一个凭证/门户/计量平面),接入分进程内中间件与网关 ForwardAuth 两档。详见 [08 §16](./08-downstream-faces-and-sdk.md)。

## 内部设计文档,非跨仓契约镜像

本目录是 NextMoe 开放 API + 开发者平台的**内部设计文档**,不经 kungal-docs `docs:sync` 下发下游镜像(与 `docs/artifact` / `docs/image_service` 等 Tier-A「整目录即契约」不同)。对外契约的机器可读来源是**公开 OpenAPI spec ×2**(catalog 面 / galgame 面),纳入 `docs:verify` + oasdiff 破坏性门(见 [02 §10](./02-public-api.md) / [07 §14](./07-migration-and-ops.md))。
