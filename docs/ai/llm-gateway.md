# NextMoe LLM 网关

> 读者：运维 LLM 供应商的人（ren），以及要接入大模型的服务或脚本的作者。
> 真相源：`internal/platform/llm`（网关核心、管理、种子）、`internal/platform/llm/proxy`（OpenAI 兼容端点）、`cmd/ai`（挂载）。管理面契约 = `docs/ai/admin-openapi.yaml` 里的 `ai-admin-llm` 标签。
> 状态基线：2026-10-28。

## 1. 一句话

所有大模型调用都经过 ai 服务里的同一个网关。供应商 key、模型、路由、场景全部存在 `kun_ai` 库里，只有 ren 能在管理端「LLM 网关配置」（`/ai/llm`）里查看和修改。**没有任何服务需要在环境变量里放供应商 key。**

```
agent（进程内）──┐
ai 审核（进程内）─┤                         ┌─► 供应商 A（如 Workers AI）
                 ├─► 网关：场景 → 模型链 → 路由 ─┼─► 供应商 B（如 DeepSeek 官方）
catalog ─────────┤   （负载均衡 + 故障切换）     └─► 供应商 C（如 OpenAI，审核）
批处理 CLI ──────┘
   ↑ 经 http://ai:9284/v1 + 访问密钥
```

## 2. 概念

| 概念 | 是什么 | 例子 |
| --- | --- | --- |
| 供应商 provider | 一个 OpenAI 兼容的端点 + key（加密存库）+ 可选额外请求头 + 价格倍率 | `cloudflare` → `https://api.cloudflare.com/client/v4/accounts/<id>/ai/v1` |
| 模型 model | 我们自己的稳定名字，带种类：`chat` / `embedding` / `moderation` | `deepseek-v4-flash` |
| 路由 route | 模型 × 供应商：这个供应商上该模型的上游 id、优先级、权重、价格 | `deepseek-v4-flash` @ cloudflare = `@cf/deepseek-ai/deepseek-v4-flash` |
| 场景 scene | 调用方要的东西：主模型 + 回退模型链 + 超时 | `agent.assistant` = deepseek-v4-flash → qwen3-30b-a3b |
| 访问密钥 access key | 内部调用方访问 `/v1/*` 的凭据，可限定可用的模型 / 场景 | `catalog` 只能用 `catalog.embed` |

只支持 OpenAI 兼容协议（Chat Completions、Embeddings、Moderations）。任何兼容的供应商都能直接加：Workers AI、DeepSeek 官方、xAI、OpenRouter、硅基流动、OpenAI 等。

## 3. 负载均衡与故障切换

一次调用的尝试顺序：

1. 先走场景的主模型，再依次走回退链里的模型；
2. 每个模型的路由按 **优先级** 从高到低分档，先用最高档；
3. 同一档内按 **权重** 做 rendezvous 哈希。键是会话 id（agent）或请求里的 `user`：同一个对话粘在同一个供应商上，有利于命中前缀缓存；不同对话按权重分散；
4. 只要还没向调用方吐出第一个字，任何可切换的失败（429、5xx、超时、首字超时、网络错误、鉴权失败）都会立刻换下一条路由，最多尝试 6 次。一旦开始输出就不再切换，避免半句话换一个模型；
5. 内容过滤、请求本身有错（400）、调用方取消不会切换。上下文超长会跳过同一模型的其他路由，直接换下一个模型。

熔断：

- 同一路由连续 3 次临时性失败后暂停一段时间，从 30 秒起倍增，最长 10 分钟；成功一次就恢复；
- 鉴权或额度失败按 1 分钟、2 分钟、4 分钟……退避，最长 30 分钟，连续 3 次时记一条 `auth_failing` 事件。**路由不会被自动停用**：Workers AI 曾在过载时返回 401，自动停用会把一阵过载变成需要人工恢复的事故；
- 所有路由都在熔断中时，挑最快恢复的那条硬试一次，不会直接报「无可用路由」。

每次尝试都记进 `llm_attempt`（耗时、首字时间、token、成本、错误种类），管理端「路由」页显示近 24 小时的调用数、错误率、平均耗时和成本。

## 4. 管理端（仅 ren）

权限 `ai.llm_manage` 只授予 ren，不可委派。读和写都需要它：普通 admin 连供应商列表都看不到。

- **供应商**：key 只写不读。保存后只显示「已配置 …末四位」。改 key 就是重新填一次；「清除 key」会让这个供应商失效。
- **路由**：「测试」按钮会用这条路由真实发一次很小的请求，不影响熔断状态和路由状态。「停用」立即生效；「启用」同时清掉这条路由的熔断。
- **场景**：主模型和回退链里的模型必须是同一种类。
- **访问密钥**：密钥只在创建时显示一次，库里只存哈希。停用或删除后立刻失效。
- 所有修改最多 30 秒内对 agent 进程生效（快照每 30 秒刷新）；对 ai 进程是立即生效。

## 5. 主密钥 `KUN_LLM_KEK`

供应商 key 用 AES-GCM 加密后存库，主密钥是 `KUN_LLM_KEK`（32 字节随机数的 base64，例如 `head -c 32 /dev/urandom | base64`）。这是唯一必须留在环境变量里的东西：

- ai 服务和 agent 项目必须配置**同一个值**；
- 没有它，管理端不能保存 key（状态接口返回 `kek_configured: false`，页面上会提示）；
- 丢了它，库里所有 key 都解不开，只能逐个重新填写。请和数据库备份分开保管。

## 6. 第一次启动（种子）

`llm_provider` 表为空时，ai 服务（以及 agent）启动时会把旧的环境变量一次性转成网关配置，key 直接加密入库：

- `KUN_AI_UPSTREAM_BASE_URL` / `_TOKEN` / `_MODEL` → 供应商 `cloudflare`，模型 `deepseek-v4-flash`、`qwen3-30b-a3b`、审核用的对话模型（默认 `glm-5.2`）、`qwen3-embedding-0.6b`，以及场景 `agent.assistant`、`agent.search`、`agent.title`、`ai.moderation.llm`、`catalog.embed`；
- `KUN_AI_OMNI_BASE_URL` / `_TOKEN` / `_MODEL` → 供应商 `openai`，模型 `omni-moderation-latest`，场景 `ai.moderation.omni`。

之后不会再读这些变量，可以从 Dokploy 里删掉。表不为空时什么都不做，所以在管理端删掉的东西不会被「种」回来。

## 7. 内部调用方：OpenAI 兼容端点

ai 服务对内网提供 `http://ai:9284/v1`（本地 `http://127.0.0.1:9284/v1`），任何 OpenAI SDK 或手写客户端都能直接用：

| 端点 | 说明 |
| --- | --- |
| `GET /v1/models` | 这个密钥能用的模型 |
| `POST /v1/chat/completions` | 支持流式（`stream: true`，含 `stream_options.include_usage`）、工具调用、`response_format`、多模态 content 等，未识别的字段原样透传给上游 |
| `POST /v1/embeddings` | 原样返回上游的结果 |
| `POST /v1/moderations` | 原样返回上游的结果 |

- 鉴权：`Authorization: Bearer <访问密钥>`。
- `model` 可以填场景 key（如 `catalog.embed`，走场景的回退链）、模型 key（如 `deepseek-v4-flash`），或某条路由的上游 id（如 `@cf/qwen/qwen3-embedding-0.6b`，兼容旧配置；只走该模型自己的路由）。
- 错误用 OpenAI 的格式：`{"error":{"message","type","code"}}`。上游的 4xx 带着原状态码和原文透传，所以依赖上游报错文本的逻辑不受影响，例如 catalog 的 embedding 在 `input too big` 时把批次减半。

服务和 CLI 统一读这两个环境变量（`pkg/config.LLMBaseURL()` / `LLMToken()`）：

```
KUN_LLM_GATEWAY_URL=http://ai:9284/v1
KUN_LLM_GATEWAY_KEY=nmk_…        # 管理端创建的访问密钥
```

设置了 `KUN_LLM_GATEWAY_KEY` 时优先走网关；没设置时仍回落到旧的 `KUN_AI_UPSTREAM_BASE_URL` / `_TOKEN` 直连上游，只为了切换期不中断，切换完成后应删除。

## 8. 场景一览

| 场景 | 调用方 | 种类 |
| --- | --- | --- |
| `agent.assistant` | AI 助手对话 | chat |
| `agent.search` | 搜索模式 | chat |
| `agent.title` | 对话标题生成 | chat |
| `ai.moderation.llm` | 内容审核 Tier2 | chat（JSON 输出） |
| `ai.moderation.omni` | 内容审核 Tier1 | moderation |
| `catalog.embed` | 预留给向量检索 | embedding |

新加调用方时先在管理端建场景，代码里只写场景 key，不写具体模型。

例外：catalog 的向量检索按上游 id `@cf/qwen/qwen3-embedding-0.6b` 调用，而不是 `catalog.embed`。模型名是每条向量文本哈希的一部分，改名会让全库向量被判定为过期，重新嵌入一遍（works 约 55 分钟）。真要换嵌入模型时本来就得全量重嵌，到时再一起改成场景名。
