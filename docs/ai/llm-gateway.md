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
| 模型 model | 我们自己的稳定名字，带种类：`chat` / `embedding` / `moderation` / `search` | `deepseek-v4-flash` |
| 路由 route | 模型 × 供应商：这个供应商上该模型的上游 id、优先级、权重、价格 | `deepseek-v4-flash` @ cloudflare = `@cf/deepseek-ai/deepseek-v4-flash` |
| 场景 scene | 调用方要的东西：主模型 + 回退模型链 + 超时 | `agent.assistant` = deepseek-v4-flash → qwen3-30b-a3b |
| 访问密钥 access key | 内部调用方访问 `/v1/*` 的凭据，可限定可用的模型 / 场景 | `catalog` 只能用 `catalog.embed` |

只支持 OpenAI 兼容协议：Chat Completions、Embeddings、Moderations、Responses（联网搜索）、Images（图片生成）。任何兼容的供应商都能直接加：Workers AI、DeepSeek 官方、xAI、OpenRouter、硅基流动、OpenAI 等。

## 3. 负载均衡与故障切换

一次调用的尝试顺序：

1. 先走场景的主模型，再依次走回退链里的模型；
2. 每个模型的路由按 **优先级** 从高到低分档，先用最高档；
3. 同一档内的顺序由场景的 **分流策略** 决定：
   - `weighted`（默认）：按权重做 rendezvous 哈希。键是会话 id（agent）或请求里的 `user`，同一个对话固定在同一个供应商上，有利于命中前缀缓存；不同对话按权重分散；
   - `least_busy`：本进程在途请求最少的路由优先；
   - `latency`：最近首字时间（EWMA）最低的路由优先；还没测过的路由排在最前，另有十分之一的请求随机探索；
   - `cost`：单价最低的路由优先，没配价格的排在最后；
4. 只要还没向调用方吐出第一个字，任何可切换的失败（429、5xx、超时、首字超时、网络错误、鉴权失败）都会立刻换下一条路由，最多尝试 6 次。一旦开始输出就不再切换，避免半句话换一个模型；
5. 请求本身有错（400）、调用方取消不会切换。
   - 上下文超长：跳过同一模型的其他路由，改走场景的「上下文超长回退链」，再继续原来的顺序；
   - 内容过滤：场景配了「内容过滤回退链」就改走它，没配就直接返回；
6. 路由的 **限流**：`max_in_flight`（并发）和 `rpm`（每分钟请求数）在 ai 和 agent 两个进程之间合计（经 Redis）。到了上限的路由直接跳过，不算失败、不计入 6 次；所有路由都满了时返回 `rate_limit`；
7. 单次请求类调用（向量、审核、联网搜索、图片）只有一条路由时，临时性失败会在 0.3–0.8 秒后再试一次。

熔断：

- 同一路由连续 3 次临时性失败后暂停一段时间，从 30 秒起倍增，最长 10 分钟；成功一次就恢复；
- 上游返回 429 或 503 并带了 `Retry-After`（或 `x-ratelimit-reset-*`）时，一次就冷却它要求的时长，限制在 1 秒到 10 分钟之间；
- 冷却状态经 Redis 在 ai 和 agent 之间共享，每 2 秒同步一次：一个进程踩到的坑，另一个进程不会再踩一遍。没有 Redis 时只在本进程内生效；
- 鉴权或额度失败按 1 分钟、2 分钟、4 分钟……退避，最长 30 分钟，连续 3 次时记一条 `auth_failing` 事件。**路由不会被自动停用**：Workers AI 曾在过载时返回 401，自动停用会把一阵过载变成需要人工恢复的事故；
- 所有路由都在熔断中时，挑最快恢复的那条硬试一次，不会直接报「无可用路由」。

每次尝试都记进 `llm_attempt`（耗时、首字时间、token、成本、错误种类）。
- 成本：上游在 usage 里报了实际费用（xAI 的 `cost_in_usd_ticks` / `cost_in_nano_usd`，1 美元 = 1e10 ticks）时以它为准；否则按路由的价格计算（token 单价，加上「每次调用」价格，图片按张计），两种都乘以供应商的价格倍率；
- 管理端「路由」页显示近 24 小时的调用数、错误率、平均耗时和成本；
- 「健康」页显示每条路由的状态（正常、冷却、限流、停用）、冷却到什么时候、在途数和本分钟用量、近 1 小时的 p50/p95 和最近一次错误，冷却中的路由可以手动「解除冷却」（所有进程同时生效）；
- 场景的「试运行」显示此刻一次调用会按什么顺序试哪些路由，以及哪些因为冷却被跳过。

### 思考等级

推理模型想多久由 **场景** 决定，怎么把它告诉上游由 **路由** 决定。

- 场景的 `reasoning`：`off`（默认，所有已有场景迁移后都是它）、`low`、`medium`、`high`、`default`（什么都不发，用模型自己的默认）。
  - `off`：上游能关思考的就关掉，不能关的就用它的最低档。
  - 按模型名直接调 `/v1` 的调用方不经过场景，保持模型默认。
  - 调用方自己在请求体里带了对应参数时，以调用方为准。
- 路由的 `thinking_control`：

| 值 | 发给上游的参数 | `off` 时 |
| --- | --- | --- |
| `effort` | `reasoning_effort`（Responses 协议是 `reasoning.effort`） | `low` |
| `effort_none` | 同上 | `none` |
| `template` | `chat_template_kwargs.enable_thinking` | `false` |
| `thinking` | `thinking.type` | `disabled` |
| `none` | 不发 | — |
| `auto`（默认） | 按上游 id 推断，路由列表里显示推断结果 | |

`auto` 的推断规则：
- Workers AI（`@cf/` 开头）上的 qwen3 / glm / deepseek → `effort_none`。官方文档写明 glm-5.2、deepseek-v4 接受 `none`，qwen3 实测也接受。
- grok、gpt-5、o3、o4 → `effort`。xAI 收到 `none` 会返回 400。
- 其他地方的 qwen3 → `template`。
- 其他地方的 deepseek、glm、kimi → `thinking`。
- 其余一律 `none`。

推断不对时在路由上改成明确的值，再用路由的「测试」确认上游接受。

## 4. 管理端（仅 ren）

权限 `ai.llm_manage` 只授予 ren，不可委派。读和写都需要它：普通 admin 连供应商列表都看不到。

- **供应商**：key 只写不读。保存后只显示「已配置 …末四位」。改 key 就是重新填一次；「清除 key」会让这个供应商失效。
- **路由**：「测试」按钮会用这条路由真实发一次很小的请求，不影响熔断状态和路由状态。「停用」立即生效；「启用」同时清掉这条路由的熔断。
- **场景**：主模型、回退链、上下文超长回退链、内容过滤回退链里的模型必须是同一种类。
- **可用模型**：每个供应商都有一份模型目录，读自它的 `/models`。
  - 刷新方式：「同步模型列表」随时手动刷新，ai 服务每天也会自动同步一次。
  - 种类判断：供应商给了元数据（OpenRouter 的 architecture、xAI 的 modalities）就照元数据；没给就按名字推断（embed → 向量，imagine/image → 图片，video → 视频……）；可以手动改，「测试」成功也会把种类记下来。
  - 「加入网关」：模型不存在时先建模型，再建路由。
  - 下架提醒：供应商不再列出某个模型时，用它的路由会收到 `upstream_delisted` 事件。
  - Cloudflare Workers AI 的 OpenAI 兼容地址没有模型列表，只能手动加路由。
  - 视频、语音合成、语音转写、重排序目前只能在目录里看到，网关还不能调用。
- **访问密钥**：密钥只在创建时显示一次，库里只存哈希。停用或删除后立刻失效。
- 所有修改最多 30 秒内对 agent 进程生效（快照每 30 秒刷新）；对 ai 进程是立即生效。

### 预算、用量与配置

- **预算**：场景和访问密钥都可以设「每日预算」（美元，按北京时间的自然日计算，0 表示不限）。
  - 花费以上游报的实际费用为准，没有就按价格计算；每次尝试都会累计，经 Redis 在 ai 和 agent 之间合计。
  - 当天用完后，这个场景或这把密钥的调用直接返回 `budget_exceeded`（/v1 上是 429），不会换路由重试。
  - 用到 80% 和 100% 时各记一条 `budget_80` / `budget_100` 事件，每天各一次。
  - 场景和密钥列表都显示今日已花多少。
- **用量**：
  - ai 服务每 10 分钟把调用记录按小时汇总进 `llm_usage_hourly`；原始的 `llm_attempt` 只保留 30 天，汇总表一直保留。
  - 「用量」页可以按供应商、路由、场景、访问密钥、模型分组，看今天、7 天、30 天的调用数、错误率、token 和费用，以及每日费用曲线。当前这一小时直接读原始记录，所以数据是实时的。
- **配置导出 / 导入**：
  - 导出：整套配置（供应商、模型、路由及价格和限流、场景、访问密钥的范围和预算）按名称导出为 JSON，**不含供应商 key 和访问密钥**。
  - 导入：先预览，列出每一项是新增、更新还是不变；确认后才写入。按名称合并，只新增或更新、从不删除，目标环境多出来的配置不受影响。
  - 导入时新建的供应商没有 key，要去「供应商」里补填；目标环境不存在的访问密钥不会自动创建（密钥只能当场生成），要手动新建。
  - 用途：备份，以及把预发环境调好的配置搬到生产。
- **审计**：所有修改都会记一条事件，并带上操作人。

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
| `POST /v1/images/generations` | 图片生成（`image` 种类的模型），原样返回上游的结果。xAI 返回的是**临时** URL，调用方要及时下载转存 |

定时任务要用不同种类的模型时（比如用 grok 生成图片），先在管理端为它建一个场景（如 `cron.cover_image` → `grok-imagine-image`，再配好回退），任务里只写场景 key。换模型、加供应商、限流都在管理端改，不用改任务代码。

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
| `agent.web_search` | AI 助手的联网搜索工具 | search |
| `agent.vision` | 带图片的助手对话（这一轮的上下文里有用户上传的图片时，代替 `agent.assistant`） | chat，模型要能看图 |
| `agent.summary` | 长对话的滚动摘要（没有可用路由时用 `agent.assistant`） | chat |
| `agent.memory` | 对话静止后整理用户的长期记忆，输出 JSON（没有可用路由时用 `agent.assistant`） | chat |
| `agent.embed` | 过去对话的语义索引和检索（没有可用路由时用 `catalog.embed`）。向量取前 256 维再归一化，换模型后旧向量要清空重建 | embedding |

新加调用方时先在管理端建场景，代码里只写场景 key，不写具体模型。

`agent.web_search` 和 `agent.vision` 不会自动生成，要由 ren 在管理端建好：

- **联网搜索（search 种类）**：网关用 OpenAI Responses 协议调用 `POST {base}/responses`，带服务端工具 `{"type":"web_search"}`，取回答正文和 `url_citation` 引用。供应商必须在 Responses API 里提供服务端 web_search（例如 xAI 的 grok-4.x）。在 chat/completions 里同一个模型不会联网。
  - 上游不认 `max_tool_calls`；agent 在 instructions 里把一次调用限制为最多两次检索，否则一次调用会检索十次、输入约 30 万 token。
  - 场景没有可用路由时，agent 不注册 `web_search` / `read_url` 工具，`/v2/agent/persona` 的 `web_search_available` 为 false，前端隐藏联网开关。
- **看图（agent.vision）**：场景没有可用路由时，上传菜单里不出现「图片」。

例外：catalog 的向量检索按上游 id `@cf/qwen/qwen3-embedding-0.6b` 调用，而不是 `catalog.embed`。模型名是每条向量文本哈希的一部分，改名会让全库向量被判定为过期，重新嵌入一遍（works 约 55 分钟）。真要换嵌入模型时本来就得全量重嵌，到时再一起改成场景名。
