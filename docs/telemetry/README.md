# App 监测（telemetry）

NextMoe 生态 App 的崩溃、异常、会话、启动与卡顿监测后端。三个 App 共用一套服务，按资源属性 `service.name` 区分。

| 文档 | 读者 | 内容 |
|---|---|---|
| [01-contract.md](01-contract.md) | App 客户端、发版流水线 | `POST /v1/logs` 与 `POST /v1/symbols` 的线上契约：请求格式、状态码、重试、去重、时间与版本归属、隐私约束 |
| [02-processing.md](02-processing.md) | 维护者 | 会话与指标口径、符号化、分组指纹、问题、告警规则、保留期与备份 |
| [03-operations.md](03-operations.md) | 运维 | 部署形态、环境变量、首次上线步骤、接入一个新 App |
| [admin-openapi.yaml](admin-openapi.yaml) | 管理台 | 管理 API 规格，由 `go run ./cmd/gen-openapi -telemetry-admin` 从代码导出，CI 校验不漂移 |

本目录是这份契约的唯一来源。客户端仓库（kungal-apps）的需求单只指向这里，不再复述。

## 组成

- `telemetry`（`cmd/telemetry`，端口 9286，数据库 `kun_telemetry`）：公开接入 `telemetry.nextmoe.dev` 的 `/v1/logs` 与 `/v1/symbols`，内网提供 `/api/v1/admin/telemetry/*` 管理 API；负责会话合并、按天汇总、保留期、引擎符号拉取、告警评估与发送。
- `telemetry-worker`（`cmd/telemetry-worker`）：从队列取异常与崩溃，调用外部解码器还原栈，计算指纹，归并为问题。
- 管理台 `admin.nextmoe.dev` 的「应用监测」分组：概览、问题、应用与密钥、告警。只对 `ren` 可见。

服务端可观测性（infra 自身 Go 服务的调用链、指标、日志）不走这个公开接入，也不在本服务范围内。
