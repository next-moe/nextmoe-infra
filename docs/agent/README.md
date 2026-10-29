# NextMoe AI 助手 · 站点接入指南

> 读者：letmoe / kungal / moyu 等下游站点的后端集成者。
> 真相源：`cmd/agent` 与 `internal/platform/agent`；机读契约 = `docs/agent/openapi.yaml`（`go run ./cmd/gen-openapi -agent` 生成，CI 校验不漂移）。
> 状态基线：2026-10-29（编辑分支、重新生成、步骤落库、反馈、收藏、建议问题）。

## 1. 全景

agent 服务（:9287，`kun_agent` 库）是全站共用的 AI 助手：对话、流式回复、资料库检索、联网搜索。站点之间的区别只有看板娘（人设、头像、开场白），对话历史按站点隔离，萌萌点全站统一。

```
浏览器 ──► 你站后端（BFF） ──► agent :9287 ──► LLM 网关（kun_ai 路由表，多供应商故障转移）
                                   ├──► catalog /v2（混合检索：BM25 + 向量）
                                   ├──► 网关场景 agent.web_search（联网搜索，可选）
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
| GET | `/v2/agent/persona` | 看板娘：`name` / `avatar` / `greetings[]`，空对话的建议问题 `suggestions[]`，以及 `web_search_available` / `web_search_auto` |
| GET | `/v2/agent/quota` | 今日免费剩余、价格、是否可扣萌萌点、萌萌点余额 |
| GET | `/v2/agent/conversations?cursor=&limit=&starred=&q=` | 历史对话（按更新时间倒序，游标分页）；`starred=true` 只看收藏；`q` 搜索标题或任一条消息正文里含这段文字的对话（不区分大小写，最长 100 字） |
| POST | `/v2/agent/conversations` | 新建对话 `{"mode":"assistant"}` |
| GET | `/v2/agent/conversations/{id}?head=` | 对话 + 当前分支的消息；`head=<消息 id>` 看经过这条消息的那个分支 |
| PATCH | `/v2/agent/conversations/{id}` | 改标题 `title`、收藏 `starred`、记住当前分支 `head_message_id`，只改传了的字段 |
| DELETE | `/v2/agent/conversations/{id}` | 删除（204） |
| POST | `/v2/agent/conversations/{id}/messages` | 发消息，开始一轮回复（202） |
| POST | `/v2/agent/conversations/{id}/regenerate` | 对同一条用户消息重新回答（202），新回答和旧回答互为兄弟 |
| PUT | `/v2/agent/messages/{id}/feedback` | 给回答打分 `{"rating":"up"\|"down"\|"none"}`（204） |
| GET | `/v2/agent/runs/{id}/stream` | 这一轮的 SSE 流 |
| POST | `/v2/agent/runs/{id}/cancel` | 停止生成（204） |
| GET | `/v2/agent/pow/challenge` | 未登录搜索用的工作量证明题目 |
| POST | `/v2/agent/search` | 搜索模式 `{"q":"…","pow":"…"}` |

对话标题在第一轮开始时就并行生成，生成好后流里会推一帧 `data-title`（`{"title":"…"}`），前端直接改侧边栏即可。列表和对话详情里的 `active_run_id` 不为空，表示这个对话还在生成，可以在侧边栏转圈。

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

- `parent_id` 省略 = 接在当前分支末尾；`"0"` = 新开一个根分支；传某条 assistant 消息 = 在它后面续问。**前端正在看别的分支时，一定要显式传 `parent_id`（当前显示的最后一条消息）**，否则会接到服务端记住的分支上。
- 附件先传到 `POST /v2/agent/uploads`，再在 parts 里写 `{"type":"file","uploadId":"…"}`，见 §3.6。旧的写法（文本附件把内容直接放进 `text`）仍然可用。用户输入的文字、附件里的文字分别受 `agent.turn.max_input_chars` 限制（413）。
- 同一个 `Idempotency-Key` 换了请求体返回 422；同一对话已有回复在生成时返回 409。
- 免费次数用完且萌萌点不足返回 402。
- 联网搜索：
  - `web_search_auto` 为 true 时（设置 `agent.web.auto`，默认开），助手自己判断要不要联网，资料库外的内容和新闻、发售日这类时效问题会去搜。
  - 这时 `web_search: true` 表示「这一问先联网」。联网开关可以做成可选的强调，不必每次都开。
  - `web_search_auto` 为 false 时，只有带 `web_search: true` 的问题能联网。

### 3.2 编辑、重新生成与分支切换

消息是一棵树。每条消息带 `sibling_ids`：同一个父消息下的所有消息（包括自己），按时间从旧到新。自己在里面的位置就是 `‹ n/m ›` 里的 n，`m = sibling_ids.length`，大于 1 时显示切换器。

| 操作 | 请求 | 结果 |
| --- | --- | --- |
| 编辑用户消息后重发 | `POST …/messages`，`parent_id` = 被编辑消息的 `parent_id`（第一条消息是 `"0"`），`parts` = 新内容 | 新的用户消息和原消息互为兄弟，切换器出现在**用户消息**上 |
| 重新生成回答 | `POST …/regenerate` `{"message_id": 回答的 parent_id}` | 新回答和旧回答互为兄弟，切换器出现在**回答**上；不新增用户消息 |
| 重试某条用户消息 | `POST …/regenerate` `{"message_id": 这条用户消息的 id}` | 同上 |
| 切换到另一个版本 | `GET /conversations/{id}?head=<兄弟消息 id>` 渲染；再 `PATCH {"head_message_id": 同一个 id}` 记住选择 | 返回经过那条消息的整条分支，往下每一层都取用户上次在那里选过的版本（没选过就取最新的）；刷新后、或切回之前的问题时，都停在用户最后选的版本 |

重新生成和发消息一样需要 `Idempotency-Key`、一样计费、一样返回 `run_id`。

### 3.3 错误

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
| `NOT_REGENERABLE` | 400 | 重新生成的 `message_id` 不是本对话的用户消息 |
| `FILE_TOO_LARGE` / `UNREADABLE_FILE` | 413 / 422 | 文件超过本站上限 / 读不出来（文本不是 UTF-8、文档只有图片没有文字、文件损坏） |
| `BAD_UPLOAD` / `TOO_MANY_FILES` | 400 | `uploadId` 不是你本人在本对话可用的上传 / 一条消息的文件数超过上限 |
| `UNSUPPORTED_ATTACHMENT` / `INPUT_TOO_LONG` | 415 / 413 | 附件类型不支持 / 输入超长 |
| `RATE_LIMITED` / `ANON_DAILY_LIMIT` / `ANON_GLOBAL_LIMIT` | 429 | 每分钟限流 / 该 IP 今日搜索用完 / 全站未登录搜索今日用完 |
| `POW_REQUIRED` / `POW_REJECTED` / `POW_UNAVAILABLE` | 403 / 403 / 503 | 需要工作量证明 / 解无效、过期或已用过 / 服务端没配 PoW |
| `CATALOG_UNAVAILABLE` | 502 | 资料库检索失败 |
| `NOT_FOUND` / `INVALID_REQUEST` | 404 / 400、422 | 其余通用情况 |

### 3.4 流式回复

`GET /v2/agent/runs/{run_id}/stream` 返回 `text/event-stream`，带 `x-vercel-ai-ui-message-stream: v1` 头，帧格式就是 AI SDK 的 UI message stream 协议，前端直接用 `@ai-sdk/vue` 的 `Chat` + `DefaultChatTransport` 消费（`prepareSendMessagesRequest` 改成先 POST 发消息、再 GET 流）。

会出现的帧：`start`、`start-step`、`data-title`、`text-start` / `text-delta` / `text-end`、`reasoning-start` / `reasoning-delta` / `reasoning-end`、`tool-input-available`、`tool-output-available`、`source-url`、`data-entity-card`（实体卡片，格式见 §6.1）、`data-browse`（筛选链接，见下）、`data-quota`、`data-run`、`finish-step`、`finish`、`error`、`abort`，最后是 `[DONE]`。

- 卡片在回答正文结束后才下发，只包含回答里提到的实体，按提到的先后排列（同一实体只出一张）。工具查到但回答没提到的候选不会出现；回答一个都没提到时，只保留直接查看详情（`get_work` / `get_entity` 这类单条查询）得到的卡片。前端建议把卡片放在正文之后。
- `data-browse`：这一轮按标签筛选作品、或按特征筛选角色时，回答结束后（在卡片之后）推送，每种筛选条件一个，最多 3 个，落库为 `{type:"data-browse", id, data}`。站点按自己的资料库页面拼出链接，例如 letmoe 的 `/games?tag_id=6296`：

```json
{"target": "works", "ids": ["6296"], "names": ["催泪"], "sort": "popular", "sorts": ["popular", "classic"], "total": 22}
```

  - `target`：`works`（`ids` 是作品标签 id）或 `characters`（`ids` 是角色特征 id），所有 id 同时满足；
  - `sort`：回答优先用的排序；`sorts`：这一轮用过的全部排序。作品的取值是 `popular` / `classic` / `newest`，角色的取值是 `popular` / `newest`；
  - `total`：符合条件的总数。没有结果的筛选不会推送。
- 断线重连：带上 `Last-Event-ID`，从下一帧接着推。
- 这一轮已经结束、没有活着的流：204，前端改为重新拉对话（消息已经落库）。
- 落库的消息 `parts` 和流里的帧同形，就是 AI SDK 的 UIMessage parts：`text`、`file`、`step-start`（每一步开头一个）、`tool-<工具名>`（`{type, toolCallId, state:"output-available", input, output}`，用来展示「共运行 N 步，每步查了什么、查到几条」，格式见下）、`data-entity-card`（`{type, id, data}`）、`source-url`（`{type, sourceId, url, title}`）。推理过程不落库。
- 每一步的 `output`（流里的 `tool-output-available` 帧和落库的 `tool-*` part 相同）：
  - `cards` / `sources`：查到的实体数和网页数；
  - `total`：按标签筛选（`browse_works` / `browse_characters`）时，符合条件的总数，可写成「-> 8 部 / 共 123 部」；其他工具没有这个字段；
  - `meta`：给步骤文案用的补充信息。
    - `search_tags`：`{for: "works"|"characters", matched: ["催泪", "灰色（毛发）"]}`；
    - `browse_works`：`{sort: "popular"|"classic"|"newest", tags: ["催泪", "致郁"]}`；
    - `browse_characters`：`{sort: "popular"|"newest", traits: ["灰色", "傲娇"]}`。
    - 其他工具没有这个字段。
  - `entities`：这一步查到的实体，最多 12 个，每个和 §6.1 的卡片 `data` 同形，可以直接打开预览；
  - `links`：这一步读到的网页 `[{url, title}]`。
  工具一返回就推这一帧，所以模型还在继续查的时候，前端就能在步骤里展开看到已经查到了什么。注意：步骤里的实体是「查到的候选」，回答后的卡片才是「回答提到的」。
- 回答上的 `feedback` 是用户之前打的分（`up` / `down`，没打过是 `null`）。
- 你的 BFF 转发时要关掉缓冲（`X-Accel-Buffering: no`，不要 gzip 这个响应），否则字是一坨一坨出来的。

### 3.5 建议问题

`GET /persona` 的 `suggestions` 是分组的建议问题，用来做空对话时输入框下面的模式按钮：

```json
[{"label": "找作品", "icon": "search", "web_search": false, "prompts": ["推荐几部猫耳妹妹的催泪作", "…"]}]
```

- `icon` 是 lucide 图标名；每组的 `prompts` 比一屏多，前端一次显示三条，「换一组」轮换。
- `web_search: true` 的组，点出去的问题要带 `web_search: true` 发送。`web_search_available` 为 false 时服务端不会返回这类组，前端也应隐藏联网开关。
- 站点可以在管理端「AI 助手 · 人设」里配自己的组；为空时用内置的默认组。

### 3.7 人设

看板娘的名字、头像、人设提示词、开场白和建议问题都在管理端「AI 助手 · 人设」里按站点配置（需要 `ai.agent_manage`）。接口是 agent 服务的 `/api/v1/admin/agent/personas`。

- 每个站最多一个生效的人设，启用一个会自动停用该站的其他人设。
- 站点没有自己的人设时，用「默认」（`site_id` 0）的那个。生效的默认人设不能删除或停用，要先启用另一个默认人设。
- 人设提示词写的是「她是谁、怎么说话」。工具使用、引用来源、附件安全这些通用规则追加在人设后面，在设置中心「AI 助手」域的 `agent.prompt.*` 里改（可以按站点覆盖，见 §4）。
- 修改后下一问立即生效，不用重启。

### 3.6 上传

上传菜单的内容来自 `GET /persona` 的 `uploads`，全部由设置中心的 `agent.upload.*` 控制（可按站点覆盖）：

```json
{"max_files": 4, "options": [
  {"kind": "image", "label": "图片", "icon": "image", "accept": "image/png,image/jpeg,image/webp,image/gif", "max_bytes": 10485760},
  {"kind": "text", "label": "文本 / 代码", "icon": "file-text", "accept": ".txt,.md,…", "max_bytes": 524288},
  {"kind": "document", "label": "文档", "icon": "file", "accept": ".pdf,.docx", "max_bytes": 20971520}
]}
```

- 点上传按钮弹出菜单，每项一个选项，`accept` 直接给 `<input type="file">`。`max_files` 为 0 或 `options` 为空时不显示上传按钮。
- `POST /v2/agent/uploads`（`multipart/form-data`，字段名 `file`）→ `{id, kind, media_type, filename, size, chars, truncated, url}`。你的 BFF 原样转发 multipart 即可；前端可以先按 `max_bytes` 拦一次。
- 图片会缩到长边 1568 像素再交给模型；文档只提取文字（扫描件没有文字会返回 `UNREADABLE_FILE`）；文字超过单条消息上限会截断，`truncated` 为 true。
- 发消息时引用：`{"type":"file","uploadId":"<id>"}`。落库后这个 part 带 `mediaType`、`filename`、`url`；`GET /v2/agent/uploads/{id}` 用同样的鉴权取回缩略后的图片或模型读到的文字，前端显示附件时用它。
- 上传后一天内没有被消息引用的文件会被清理；被引用的文件随对话一起删除。
- 本轮上下文里有图片时，这一轮由网关场景 `agent.vision` 回答。

## 4. 萌萌点与次数

全部是后台动态配置（设置中心 `agent` 域，带站点作用域的可以按站覆盖），不写死：

| 设置项 | 含义 |
| --- | --- |
| `agent.enabled` | 站点开关（站点级） |
| `agent.free.units_per_day` | 每个用户每天免费单位数 |
| `agent.price.assistant` / `agent.price.web_surcharge` / `agent.price.search` | 一问的价格、开联网的加价、一次搜索的价格（整数） |
| `agent.paid.enabled` | 免费用完后是否允许扣萌萌点 |
| `agent.web.auto` | 助手是否自行判断联网（站点级，默认开） |
| `agent.prompt.rules` / `agent.prompt.web` / `agent.prompt.web_forced` / `agent.prompt.last_step` / `agent.prompt.title` | 追加在人设之后的通用规则、可联网时的规则、用户打开联网时的规则、最后一步的提示、起标题的提示词（站点级，多行文本，留空即不加） |
| `agent.prompt.web_search` | 交给联网搜索模型的指令（全局）。默认限制一次最多检索两次，放宽会直接抬高每次联网的成本 |
| `agent.user.rate_per_minute` | 已登录用户每分钟请求上限 |
| `agent.anon.enabled` / `agent.anon.verify_mode` / `agent.anon.pow_difficulty` | 未登录搜索开关、人机验证方式（`none` / `pow`）、难度 |
| `agent.anon.rate_per_minute` / `agent.anon.quota_per_day` / `agent.anon.global_quota_per_day` | 未登录每 IP 每分钟 / 每天、全站每天上限 |
| `agent.catalog_nsfw` | 该站助手能否看到 r18 作品（站点级） |
| `agent.turn.max_steps` / `agent.turn.max_output_tokens` / `agent.turn.max_input_chars` / `agent.context.max_messages` | 单轮工具调用步数、输出上限、输入上限、带入的历史条数 |
| `agent.upload.max_files`、`agent.upload.{image,text,document}.{enabled,max_bytes,types}` | 上传：每条消息的文件数，每类的开关、大小上限、允许的类型（图片按 MIME，文本和文档按扩展名），都可按站点覆盖 |

扣费流程：发消息时先预留免费次数，不够的部分只核对萌萌点余额；回复成功后才结算，萌萌点经 oauth 扣除（幂等键 `agent:run:<run_id>`），联网加价按「可能联网」预留，这一轮没真的搜索就不收。余额只够基本价时，这一问照常回答，只是不联网。失败、取消的轮次退回免费次数，也不扣萌萌点。界面上统一写「消耗 NextMoe 萌萌点」。

## 5. 未登录搜索与工作量证明

站点把 `agent.anon.verify_mode` 设成 `pow` 后，未登录搜索必须带解：

1. 你后端用 client 凭据 `GET /v2/agent/pow/challenge` → `{"token":"…"}`，交给浏览器。
2. 浏览器用 `@nextmoe/pow-widget`（仓库 `next-moe/nextmoe-pow`）解题：`await el.execute()`，解出的字符串放进表单字段。
3. 你后端把它作为 `pow` 和 `q` 一起 `POST /v2/agent/search`。

每个解只能用一次，题目 5 分钟过期，作用域绑定站点 + `agent.search`，别的站点或别的动作用不了。

## 6. 站点工具（MCP）与站内卡片

助手默认只能查 NextMoe 共享的资料库。站点独有的内容（比如 kungal 的话题）在各站自己的库里，由站点以 MCP 服务的形式借给助手。

### 6.1 卡片

`data-entity-card` 的 `kind`：

- 共享的 NextMoe 实体不带前缀，id 都是 catalog 的 id，各站通用：

| kind | id | 来自 | catalog 详情 |
| --- | --- | --- | --- |
| `work` | 作品 id | `search_catalog` / `browse_works` / `get_work` | `/v2/catalog/works/{id}` |
| `character` | 角色 id | `search_entities` / `browse_characters` / `get_entity` | `/v2/catalog/characters/{id}` |
| `person` | **署名（credit name）id**，不是 person id：署名是 catalog 对外的人物实体，person 只把同一人的多个署名连起来，很多署名还没有 person | 同上 | `/v2/catalog/credit-names/{id}` |
| `company` | 会社 / 社团 id | 同上 | `/v2/catalog/companies/{id}` |
| `series` | 系列 id | `search_entities` / `get_entity` | `/v2/catalog/series/{id}` |
| `tag` | 作品标签 id | `search_tags` | `/v2/catalog/tags/{id}` |

- 站点独有的实体带前缀：`<card_prefix>:<kind>`，如 `kungal:topic`。只有所属站点提供可点击的预览，其他站照普通卡片显示。
- 所有卡片都有这组预览字段，前端可以用同一个抽屉渲染：

```json
{
  "kind": "character", "id": "39099", "title": "朝武芳乃",
  "localized": {"ja": {"value": "Tomotake Yoshino"}},
  "subtitle": "女 · 《千恋＊万花》主角",
  "cover": "https://…", "summary": "…",
  "facts": [{"label": "声优", "value": "仙台惠理"}, {"label": "登场作品", "value": "《千恋＊万花》"}],
  "content_limit": "sfw"
}
```

  - `id` 是字符串。`subtitle` / `cover` / `summary` 可能为空；`facts` 是有序的「标签：值」，至少是空数组。
  - `cover` 是 https 地址。图片本身是成人内容（角色图、人物照、logo 也一样）时 `content_limit` 为 `nsfw`；站点没开成人内容时 `cover` 直接为空。
  - `work` 卡额外保留原来的字段：`release_date`、`content_rating`，以及对象形式的 `cover`（catalog 的封面对象，不是字符串）。另有结构化的 `tags`（最多 6 个，站点没开成人内容时不含性相关标签）、`companies`、`ratings`（`[{source: "bangumi"|"vndb", score, votes}]`），方便做更大的预览。
  - 站点卡片额外有 `url`（站内相对路径）。站点卡片也可以带 `facts`，最多 8 条。

```json
{"kind": "kungal:topic", "id": "88", "title": "猫耳作品讨论", "subtitle": "…", "cover": "https://…", "summary": "…", "facts": [{"label": "回复", "value": "42"}], "url": "/topic/88", "content_limit": "sfw"}
```

agent 只保留格式合格的卡片：
- `kind` 必须是本服务登记的前缀；
- `id`、`title` 不能为空；
- `url` 必须是站内相对路径（以 `/` 开头，不能是 `//`）；
- `cover` 只接受 https，不合格的封面会被去掉；
- 每次调用最多 12 张。

### 6.2 站点要提供的 MCP 服务

- 传输方式：MCP streamable HTTP（任一 MCP SDK 的标准 HTTP 服务端即可），地址要让 agent 在内网能访问到。
- 鉴权：
  - 登记为 `user_token` 时，每次调用都带 `Authorization: Bearer <提问用户的 access token>`，站点按自己的规则判断可见性和成人内容；
  - 登记为 `none` 时不带 token，只适合完全公开的数据。
- 工具：每个工具都要写清 `description` 和 `inputSchema`，模型靠它们决定什么时候调用。返回值：
  - `content`：给模型读的文字，简洁地写出结果要点；
  - `structuredContent.cards`：可选，格式见 §6.1。
  - 出错时返回 `isError: true` 并附文字说明，不要返回 HTTP 错误。
- 会修改数据的工具（「帮我办」类）暂不开放。以后会改为先返回待确认卡片，用户确认后才执行。

### 6.3 登记

在管理端「AI 助手 · 站点工具」里按站点登记（需要 `ai.agent_manage`）。接口在 agent 服务的 `/api/v1/admin/agent/site-tools`：

| 字段 | 含义 |
| --- | --- |
| `site_id` / `name` | 所属站点、服务名（模型看到的工具名是 `<name>__<工具名>`） |
| `url` | MCP 地址 |
| `auth` | `user_token` / `none` |
| `card_prefix` | 卡片 kind 的前缀 |
| `allowed` | 允许助手调用的工具名，空表示全部 |
| `timeout_ms` / `enabled` | 单次调用超时、开关 |

「测试」按钮会用你自己的 token 连一次服务，列出工具及其是否被允许。工具列表在 agent 里缓存 5 分钟；连接失败的服务 1 分钟后重试，期间这个站的对话照常进行，只是暂时没有这些工具。

## 7. 本地开发

```bash
go run ./cmd/migrate agent      # kun_agent（先 pnpm dev:db 建库）
go run ./cmd/migrate ai         # kun_ai 里的 llm_* 路由表
go run ./cmd/agent              # :9287
```

模型、供应商和 key 都在 LLM 网关里配置（管理端「LLM 网关配置」，仅 ren，见 `docs/ai/llm-gateway.md`）。本地网关为空时，`apps/api/.env` 里的 `KUN_AI_UPSTREAM_BASE_URL` / `KUN_AI_UPSTREAM_TOKEN` 会在第一次启动时被种成第一个供应商；网关里没有可用路由时，每一轮都会失败。`KUN_LLM_KEK` 要和 ai 服务用同一个值。其余可选：`KUN_AGENT_CLIENT_ID` / `KUN_AGENT_CLIENT_SECRET`（不填只有免费次数）、`KUN_AGENT_POW_KEYS`。联网搜索和看图不靠环境变量，由网关里的 `agent.web_search`、`agent.vision` 场景决定（见 `docs/ai/llm-gateway.md` §8）。

本地还要在设置中心给你的站点打开 `agent.enabled`。
