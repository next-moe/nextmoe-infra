# NextMoe AI 助手 · 站点接入指南

> 读者：letmoe / kungal / moyu 等下游站点的后端集成者。
> 真相源：`cmd/agent` 与 `internal/platform/agent`；机读契约 = `docs/agent/openapi.yaml`（`go run ./cmd/gen-openapi -agent` 生成，CI 校验不漂移）。
> 状态基线：2026-10-28。

## 1. 全景

agent 服务（:9287，`kun_agent` 库）是全站共用的 AI 助手：对话、流式回复、资料库检索、联网搜索。站点之间的区别只有看板娘（人设、头像、开场白），对话历史按站点隔离，萌萌点全站统一。

```
浏览器 ──► 你站后端（BFF） ──► agent :9287 ──► LLM 网关（kun_ai 路由表，多供应商故障转移）
                                   ├──► catalog /v2（混合检索：BM25 + 向量）
                                   ├──► Brave（联网搜索，可选）
                                   └──► oauth（萌萌点扣费）
```

**agent 不对浏览器开放。** 生产上它只在 dokploy-network 上（`http://infra-agent:9287`），由你站后端代调。浏览器拿到的流就是你后端原样转发的 SSE。

两种模式：

| 模式 | 谁能用 | 端点 | 计费 |
| --- | --- | --- | --- |
| assistant（对话） | 已登录用户 | `/v2/agent/conversations/**` | 每天免费次数，用完按题扣萌萌点 |
| search（搜索） | 已登录 + 未登录 | `POST /v2/agent/search` | 未登录按 IP 限流，可要求工作量证明 |

## 2. 鉴权

| 场景 | `Authorization` | 说明 |
| --- | --- | --- |
| 用户请求 | `Bearer <用户 access token>` | 站点由 token 的 `client_id` 反查（`oauth_clients.site_id`），所以 token 必须是你站 client 签发的 |
| 未登录搜索 | `Basic base64(client_id:client_secret)` | 只对 search 与 pow/challenge 有效；对话端点要求用户 token |

- 未绑定站点的 client 一律 403；站点没开 `agent.enabled` 也是 403。
- 你后端要把访客真实 IP 放进 `CF-Connecting-IP`。agent 只在请求来自私网 / 回环地址时才信这个头，所以只能由同网的后端转发。

## 3. 端点

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| GET | `/v2/agent/persona` | 看板娘：`name` / `avatar` / `greetings[]` |
| GET | `/v2/agent/quota` | 今日免费剩余、价格、是否可扣萌萌点、萌萌点余额 |
| GET | `/v2/agent/conversations?cursor=&limit=` | 历史对话（按更新时间倒序，游标分页） |
| POST | `/v2/agent/conversations` | 新建对话 `{"mode":"assistant"}` |
| GET | `/v2/agent/conversations/{id}` | 对话 + 当前分支的消息 |
| PATCH | `/v2/agent/conversations/{id}` | 重命名 `{"title":"…"}` |
| DELETE | `/v2/agent/conversations/{id}` | 删除（204） |
| POST | `/v2/agent/conversations/{id}/messages` | 发消息，开始一轮回复（202） |
| GET | `/v2/agent/runs/{id}/stream` | 这一轮的 SSE 流 |
| POST | `/v2/agent/runs/{id}/cancel` | 停止生成（204） |
| GET | `/v2/agent/pow/challenge` | 未登录搜索用的工作量证明题目 |
| POST | `/v2/agent/search` | 搜索模式 `{"q":"…","pow":"…"}` |

对话标题在第一轮回复结束后自动生成，前端重新拉一次对话列表即可看到。

### 3.1 发消息

```http
POST /v2/agent/conversations/42/messages
Authorization: Bearer …
Idempotency-Key: 3b8f…（每次点击发送生成一个，重试时复用）

{
  "parts": [
    {"type": "text", "text": "推荐几部猫耳妹妹的催泪作"},
    {"type": "file", "mediaType": "text/plain", "filename": "notes.txt", "text": "…文件内容…"}
  ],
  "parent_id": "117",
  "web_search": false
}
```

```json
{"run_id": "01JB…", "user_message_id": "118", "assistant_message_id": "119"}
```

- `parent_id` 省略 = 接在当前分支末尾；`"0"` = 新开一个根分支；传某条 assistant 消息 = 在它后面续问；传某条 user 消息的父消息 = 重新生成 / 编辑后重发。消息上的 `siblings` > 1 时可以在前端显示分支切换。
- 附件目前只收文本（`text/*`、`application/json`、`application/x-yaml`），由你后端读出内容放进 `text`；图片返回 415。文字和附件总长受 `agent.turn.max_input_chars` 限制（413）。
- 同一个 `Idempotency-Key` 换了请求体返回 422；同一对话已有回复在生成时返回 409。
- 免费次数用完且萌萌点不足返回 402。

### 3.2 错误

错误体是 `application/problem+json`：`{status, title, detail, code}`。**按 `code` 分支，不要按 `detail` 或状态码猜。**

| code | 状态 | 含义 |
| --- | --- | --- |
| `LOGIN_REQUIRED` | 401 | 需要用户 token（或站点不允许未登录搜索） |
| `INVALID_TOKEN` / `INVALID_CLIENT` | 401 | token 无效或过期 / client 凭据错误 |
| `CLIENT_NOT_BOUND` / `ASSISTANT_DISABLED` | 403 | client 没绑定站点 / 站点没开助手 |
| `FREE_UNITS_EXHAUSTED` | 402 | 今日免费次数用完，且站点没开萌萌点付费 |
| `INSUFFICIENT_MOEMOEPOINTS` | 402 | 免费次数用完，萌萌点也不够 |
| `RUN_ACTIVE` | 409 | 这个对话还有回复在生成 |
| `IDEMPOTENCY_KEY_REUSED` | 422 | 同一个 Idempotency-Key 换了请求体 |
| `BAD_PARENT` / `EMPTY_MESSAGE` | 400 | `parent_id` 不属于该对话 / 消息为空 |
| `UNSUPPORTED_ATTACHMENT` / `INPUT_TOO_LONG` | 415 / 413 | 附件类型不支持 / 输入超长 |
| `RATE_LIMITED` / `ANON_DAILY_LIMIT` / `ANON_GLOBAL_LIMIT` | 429 | 每分钟限流 / 该 IP 今日搜索用完 / 全站未登录搜索今日用完 |
| `POW_REQUIRED` / `POW_REJECTED` / `POW_UNAVAILABLE` | 403 / 403 / 503 | 需要工作量证明 / 解无效、过期或已用过 / 服务端没配 PoW |
| `CATALOG_UNAVAILABLE` | 502 | 资料库检索失败 |
| `NOT_FOUND` / `INVALID_REQUEST` | 404 / 400、422 | 其余通用情况 |

### 3.3 流式回复

`GET /v2/agent/runs/{run_id}/stream` 返回 `text/event-stream`，带 `x-vercel-ai-ui-message-stream: v1` 头，帧格式就是 AI SDK 的 UI message stream 协议，前端直接用 `@ai-sdk/vue` 的 `Chat` + `DefaultChatTransport` 消费（`prepareSendMessagesRequest` 改成先 POST 发消息、再 GET 流）。

会出现的帧：`start`、`start-step`、`text-start` / `text-delta` / `text-end`、`reasoning-start` / `reasoning-delta` / `reasoning-end`、`tool-input-available`、`tool-output-available`、`source-url`、`data-entity-card`（作品卡片：`kind` / `id` / `title` / `localized` / `release_date` / `content_rating` / `content_limit` / `cover`；对没开成人内容的读者展示封面与否，按 `content_limit` 判断）、`data-quota`、`data-run`、`finish-step`、`finish`、`error`、`abort`，最后是 `[DONE]`。

- 断线重连：带上 `Last-Event-ID`，从下一帧接着推。
- 这一轮已经结束、没有活着的流：204，前端改为重新拉对话（消息已经落库）。
- 落库的消息 `parts` 和流里的帧同形，就是 AI SDK 的 UIMessage parts：`text`、`file`、`data-entity-card`（`{type, id, data}`）、`source-url`（`{type, sourceId, url, title}`）。推理过程和工具调用不落库。
- 你的 BFF 转发时要关掉缓冲（`X-Accel-Buffering: no`，不要 gzip 这个响应），否则字是一坨一坨出来的。

## 4. 萌萌点与次数

全部是后台动态配置（设置中心 `agent` 域，带站点作用域的可以按站覆盖），不写死：

| 设置项 | 含义 |
| --- | --- |
| `agent.enabled` | 站点开关（站点级） |
| `agent.free.units_per_day` | 每个用户每天免费单位数 |
| `agent.price.assistant` / `agent.price.web_surcharge` / `agent.price.search` | 一问的价格、开联网的加价、一次搜索的价格（整数） |
| `agent.paid.enabled` | 免费用完后是否允许扣萌萌点 |
| `agent.user.rate_per_minute` | 已登录用户每分钟请求上限 |
| `agent.anon.enabled` / `agent.anon.verify_mode` / `agent.anon.pow_difficulty` | 未登录搜索开关、人机验证方式（`none` / `pow`）、难度 |
| `agent.anon.rate_per_minute` / `agent.anon.quota_per_day` / `agent.anon.global_quota_per_day` | 未登录每 IP 每分钟 / 每天、全站每天上限 |
| `agent.catalog_nsfw` | 该站助手能否看到 r18 作品（站点级） |
| `agent.turn.max_steps` / `agent.turn.max_output_tokens` / `agent.turn.max_input_chars` / `agent.context.max_messages` | 单轮工具调用步数、输出上限、输入上限、带入的历史条数 |

扣费流程：发消息时先预留免费次数，不够的部分只核对萌萌点余额；回复成功后才结算，萌萌点经 oauth 扣除（幂等键 `agent:run:<run_id>`），开了联网但这一轮没用上的不收加价。失败、取消的轮次退回免费次数，也不扣萌萌点。界面上统一写「消耗 NextMoe 萌萌点」。

## 5. 未登录搜索与工作量证明

站点把 `agent.anon.verify_mode` 设成 `pow` 后，未登录搜索必须带解：

1. 你后端用 client 凭据 `GET /v2/agent/pow/challenge` → `{"token":"…"}`，交给浏览器。
2. 浏览器用 `@nextmoe/pow-widget`（仓库 `next-moe/nextmoe-pow`）解题：`await el.execute()`，解出的字符串放进表单字段。
3. 你后端把它作为 `pow` 和 `q` 一起 `POST /v2/agent/search`。

每个解只能用一次，题目 5 分钟过期，作用域绑定站点 + `agent.search`，别的站点或别的动作用不了。

## 6. 本地开发

```bash
go run ./cmd/migrate agent      # kun_agent（先 pnpm dev:db 建库）
go run ./cmd/migrate ai         # kun_ai 里的 llm_* 路由表
go run ./cmd/agent              # :9287
```

模型、供应商和 key 都在 LLM 网关里配置（管理端「LLM 网关配置」，仅 ren，见 `docs/ai/llm-gateway.md`）。本地网关为空时，`apps/api/.env` 里的 `KUN_AI_UPSTREAM_BASE_URL` / `KUN_AI_UPSTREAM_TOKEN` 会在第一次启动时被种成第一个供应商；网关里没有可用路由时，每一轮都会失败。`KUN_LLM_KEK` 要和 ai 服务用同一个值。其余可选：`KUN_AGENT_CLIENT_ID` / `KUN_AGENT_CLIENT_SECRET`（不填只有免费次数）、`KUN_AGENT_POW_KEYS`、`KUN_BRAVE_API_KEY`（不填就没有联网搜索）。

本地还要在设置中心给你的站点打开 `agent.enabled`。
