# MCP Server(公开只读面的 AI-agent 协议适配层)

> 拍板 2026-07-23(D4 提级 Phase 2,见 [01 §13](./01-design.md);派发-验收模式同 wave 08/09)。
> 🪦 **2026-08-27(wave R3)**:v1 整面退役,MCP 工具全部改打 `/v2`。下文凡以 `/v1/...` 路径描述某条工具的后端均为历史记录——那些路径现在返回 410,工具名单以 `apps/developer/app/generated/mcp-tools.mjs` 为准。
> 一句话定位:把**已有的公开 /v1 只读面**同时暴露为 **MCP(Model Context
> Protocol)server**,让 AI 助手 / agent 用自然的工具调用直接查生态目录——
> **不新造任何数据面、权限面或计量面**。

## 1. 核心架构裁决:纯透传适配器(thin pass-through adapter)

MCP server 是公开只读契约前面的一层**协议适配**,不是第二个 API:

- 每个 MCP tool 调用 = 一次对公开面(`api.nextmoe.dev`)的 HTTP 请求,
  **原样转发调用方的 API key**;响应 JSON 即 tool result。**上游自 `409a7a80` 起是
  `/v2`**(本文余下各处写 `/v1` 的地方是 M1 的历史记述)。
- 因此鉴权、tier、NSFW 可见性、限流、日配额、用量计量**全部天然复用**:
  流量落在同一个面、记在同一把 key 上(`/dev/usage` 里与直连流量无别)。
  MCP 层自身**零 authz 逻辑、零计量逻辑**——它连数据库都不碰。
- 面的版本化留在上游;tool 名不带版本(名字就是该版本 spec 的 `operationId`)。
  上游 expand→contract 纪律(02 §3.5)自动覆盖 MCP 消费者。

## 2. 形态与宿主

- 新 Go 服务 **`cmd/mcp`**(端口 **9285**),用 **官方 MCP Go SDK**
  (`github.com/modelcontextprotocol/go-sdk`;执行时核最新 tag,若官方 SDK
  能力缺口再评估 `mark3labs/mcp-go`,以官方优先)。
- Transport:**Streamable HTTP、stateless 模式**(无会话粘性,水平扩展与
  现有 Fiber 服务同治)。不做 stdio 分发(自托管用户 M2 再议)。
- 域名:**`mcp.nextmoe.dev`**(独立子域,Dokploy panel-domain 姿态,与
  developer-portal 同款单服务项目;`nextmoe.dev` 族命名约定见 README)。
- 上游 base 走 env(`KUN_MCP_UPSTREAM_BASE`,prod = api.nextmoe.dev 的
  内网服务地址),超时/重试保守(单次 30s,不重试非幂等——M1 全只读,
  幂等 GET 允许一次重试)。

## 3. 认证(M1)

- 调用方在 MCP endpoint 上带 `Authorization: Bearer nmk_live_…`(各 MCP
  客户端的标准 header 配置即可;**v1 的 `nm_` key 不再受理**,上游是 v2)。MCP 层只做
  **形态检查**(缺失 / 非 `nmk_` 前缀 / 长度不对 → 立即 MCP error,提示去
  developer.nextmoe.dev 领 key);**真正的
  鉴权仍在上游面**(key 无效/超限时把面的 401/403/429 错误体转成带
  说明的 tool error 返回)。
- MCP 规范的 OAuth 2.1 授权流 = M2(第三方实际开放后,与 `dev:manage`
  同期评估);M1 的静态 key 模式对 agent 场景已充分。

## 4. 工具面(由 v2 spec 派生)

**工具清单不是手写的,从 2026-08-25 起也不再写在这里。** `mcpface.ToolsFromSpec` 读 v2 OpenAPI 文档,把符合准入前缀的每一条 GET 变成一个工具:工具名 = `operationId`,参数集合 = 该 op 的 query + path 参数(含 `view=` / `fields=`),描述 = 该 op 的 spec description。`NewServer` 启动时调它注册,`cmd/gen-v2-portal` 调同一个函数生成门户的 `app/generated/mcp-tools.mjs`,门户页与 `docs/mcp.md` 消费那份生成物 + 一张按 `operationId` 索引的中文描述表(双向完整性断言,缺一条或多一条都在构建期硬失败)。

准入与凭据两条规则,都在 `internal/platform/mcpface/spec.go`:

| 规则 | 内容 |
|---|---|
| `mcpToolPrefixes` | 进面的路径前缀:`/v2/catalog` `/v2/news` `/v2/problems` `/v2/vocabularies`。`/v2/me` 与 `/v2/moderation` 不进面(用户令牌写面,理由同下文 playtime 条) |
| `takesAppKey` | 哪些工具在缺 key 时**本地**就报错而不去打上游:读该 op 在 spec 里声明的 `security`,含 `applicationKey` 即需要。**不是手写路径表**——2026-09-18 之前这里是一份按前缀手写的 `httpNeedsKey`,与 apiv2 的 `v2Security` 是同一规则的两份副本;现在 `v2Security` 写进 spec、MCP 从 spec 读,只剩一个真源 |

2026-09-18 为 **57 个工具**(`/v2/catalog` 49 + `/v2/news` 3 + `/v2/problems` 3 + `/v2/vocabularies` 2)。每个参数的描述与闭词表(`enum`)也取自 spec;v2 的 query/path 参数在 spec 里一律声明为 `type: string`、由服务端解析,所以工具入参全是 string 不丢类型信息。这个数随 v2 spec 增长自动变,**不需要改本文**;`CheckG10`(`apiv2/handler/gates_mcp.go`)保证 spec 里每条该进面的 GET 都有对应工具且参数不缺。

> **为什么删掉原来的 37 行表**:那张表列的是 M1 时代手写的 v1 工具名(`catalog_search` / `news_list` / …)。`409a7a80` 把 `NewServer` 切到 `registerSpecTools` 之后,server 注册的是 v2 `operationId`,而表和门户页都没跟上——**表上每一个名字都不再存在**,而没有任何东西比对过两边。第二份手写清单必然漂移,所以这一波把它换成派生物,并把「派生物 vs 描述表」的比对做成构建期断言。

### 4.1 历史裁定(逐条收面的理由,仍然成立)

下面这些是 v1 时代逐波把端点收进工具面的判据。v2 的准入改成按前缀,但**为什么这些东西值得给 LLM**的理由不随之作废,故保留备查。

> **2026-08-25**:news 面的授权制凭据前提**整体退役**。此前三条 news 工具的描述里各自写明「需 `news:read`(授权制)」,`instructions` 串也讲怎么去门户申请;现在 `/v2/news` 匿名即可,`/v1/news` 只要一把有效 key(任意 scope),两处文案均已删除。留着的话模型会以为自己缺权限,把一次正常结果读成「被拒绝」。

> **2026-08-18**:news 面三条**全部进面**。它们与 catalog 工具**不共用凭据前提**——`news:read` 不在 devapi 自助 scope 集内,合作方只授权了索引;故三条工具的描述里各自写明授权制,不然模型只会稳定地拿到 403 而不知道为什么。**同日改动**:授予路径从"平台人工签发"改为**门户申请 + 平台审批**,`NewServer` 的 `instructions` 串同步改口。(两条都随上面的 2026-08-25 退役。)

> **wave 146(2026-07-30)**:`galgame_search` / `galgame_get` **随其上游 `/v1/galgame` 面一同退役**——该面现返回 `410 Gone`,继续注册这两个工具只会稳定地喂给调用方一个错误。后继:实体搜索接自然语言搜索,按 id 取详情接详情道。

- **v1 时代的 catalog 覆盖面(34/37)**:公开 v1 catalog 面共 37 op,手写工具表覆盖 34。
  这个分数随 v2 的前缀准入作废(v2 的覆盖由 `CheckG10` 保证,不再是一个人工维护的比值),
  下面保留的是各条**为什么值得收面**的裁定理由。上一波记为「待裁定」的**作品子资源十二条**
  (`works/{id}/covers` 等,见 [02 §3.2](./02-public-api.md))已由 owner 裁定
  **全部收进工具面**(wave 213 波 2,2026-08-21)。裁定理由:它们分页的确实是
  `catalog_work_get` 已经整块返回的东西,但那正是收面的价值——一个数据丰富的作品
  带 `include=relations,credits` 是 50 KB,身份核心不到十分之一,而模型多数时候
  只要其中一块,十二条让它按块取而不必为一个封面吞下整个作品;十二条全是 GET,
  参数与母面逐字一致(`limit`/`offset`/`nsfw`),合乎纯透传红线。**逐参对齐时踩到的一处**:
  `characters` **没有** `spoilers` 参数(花名册每行自带 spoiler 等级由调用方分级),
  十二条里只有 `tags` 带 `spoilers`——按「母面有的子面都有」想当然会造出一个上游
  根本不认的参数。上一波记为「待裁定」的 `stats` 与 `series` 已由 owner
  裁定收进工具面(wave 189,2026-08-07),且 `series` 实收**两条**而非一条——
  系列不进任何搜索索引,只有 `series/{id}` 详情道的话调用方**永远拿不到 id**,
  浏览道是它的唯一发现入口,两条必须成对进面。上一波记为「待裁定」的
  `GET /v1/catalog/labels/{id}/relation-graph` 与 `GET /v1/catalog/releases` 已由
  owner 裁定收进工具面(wave 196,2026-08-08)。两条的收面理由都是**实测**而非直觉:
  relation-graph 服务端封顶 depth 4 / 60 节点且不分页,全库最大连通分量只触及 32 条边,
  载荷有硬上界,而它答的「某社旗下有哪些牌子 / 这个牌子归谁」是多跳问题——单跳的
  `catalog_label_get.relations[]` 只能让模型自己猜下一步走谁。`releases` 起初被判为
  「大基数列表面、收益低」,该判断经实测**推翻**:它有 13 个过滤器(日期区间 / platform /
  lang / olang / kind / official / content_limit / cursor / limit / include),载荷由
  `limit` 而非语料决定(31.9 万 release,一个「2024 年 Windows 日语」这样的现实问法
  收敛到 1,843 条,默认页量 20),且它答的平台 / 语言 / 版本类型 / 官方性是 calendar
  三桶按构造答不了的——calendar 把作品放在**最早**发售月且只显示一次。上一波记为「待裁定」的 A2 八条(calendar 三桶 / taxonomy 列表三条 /
  `engines/{id}` / `works/search`)已由 owner 裁定收进工具面(wave 7,2026-07-30),
  八条全是 GET,合乎纯透传红线。**有意留白**仍是三条:`POST /v1/catalog/lookup/batch`
  (批量外部 id 水合)、`GET /v1/catalog/redirects`(合并事件 keyset 流,供镜像清理
  存量 id)、`POST /v1/catalog/resolve`(旧 id→正典 id 批量扁平化)——它们服务的是
  **镜像维护 / 批量同步**型消费者,应直连 HTTP 面:单轮 LLM tool call 没有批量、
  也没有存量 id 维护语义;小批量水合已由 `catalog_works_list` 的 `ids=` 覆盖,单个
  外部 id 由外部 id 反查覆盖;且 `lookup/batch` 与 `resolve` 是
  POST,而 mcpface 传输是 GET 纯透传。(v2 的按前缀准入沿用同一条红线:非 GET 一律不进面。)

- **playtime 面:已裁定不进 MCP(wave 207,非「还没做」)**。平台的第二个公开面
  `/v1/playtime`(5 op,见 [02 §3.8](./02-public-api.md))**整族出界**,理由是它与
  §1 的红线正面冲突:MCP 层是**纯透传**适配器,把调用方那把 `X-API-Key` 原样转发给
  上游、自己零 authz 零计量**连数据库都不碰**;而 playtime 是**用户 Bearer 令牌**认证的
  **写**面,写的是某个具体人的个人记录。把一枚用户令牌穿过 LLM 的 tool 循环去写他的
  个人数据,不在 MCP 的授权范围内——这需要的是 MCP 规范的 OAuth 2.1 流(§3 里的
  M2),而不是把 `Authorization` 头换一种内容继续透传。触发条件与 M2 同期:真出现
  「让 agent 帮我同步游玩库」的外部需求时,连同写面工具一起评估,而不是先把面开了。
  在那之前,**覆盖率分数不把这 5 条算进分母**。

- **r18 姿态 = 调用方自控**(104 波所定;wave 213 波 2 曾改为能力位门控,该能力位已于
  2026-08-25 退役,姿态回到 104 波原样):catalog 系工具是 `nsfw=true` 显式开、默认
  全部隐藏——LLM 消费者不显式要就永远看不到 r18,而**要了就一定拿得到**,不再有
  凭证维度的 403。每个工具的 `nsfw` 参数描述与 `instructions` 串里的
  「requires an API key with the NSFW capability」已一并删除:留着的话模型会以为
  自己缺权限,把一次空结果读成「被拒绝」而反复换问法重试。(旧 galgame 系工具的
  `content_limit`+`galgame:nsfw` scope 姿态已随 /v1/galgame 摘牌一并退役;
  `catalog_works_search` 的 `content_limit` 是编辑展示轴,与 r18 轴无关。)

- tool description 用英文、面向 LLM 写清「何时用哪个」(lookup vs search
  的分工是重点:有外部 id 用 lookup,自然语言用 search)。
- 输入 schema 逐参对齐上游 query 参数(分页参数透传,默认页量保守)。
- **不做**的(明确出界):redirects/resolve/lookup/batch(镜像维护面,理由见
  上面的覆盖说明)、**playtime 面整族**(wave 207,理由见上)、resources/prompts(M2)、
  任何写面(Phase 3 submit 开放后随 OAuth 一起评估)。`changes`(canonical-W1)与 calendar 三桶(wave 7,收的是
  **catalog 面**的月历;当年出界的是已退役的 galgame 面月历)原属此列,现均已
  进面(见上表)。

## 5. 运维与部署

- 独立 Dokploy 单服务项目(照抄 developer-portal 的 panel-domain +手动
  Deploy 姿态,`docker-compose.mcp.yml`);镜像走现有 CI 矩阵。
- healthz 照平台惯例;结构化日志记 tool 名 + 上游状态码 + 时延,
  **永不记 key 明文**(fingerprint 前 8 hex)。
- 冒烟:MCP `initialize` + `tools/list` + 一次 `searchCatalog` 真调用。(2026-08-25 起 `tools/list` 应回 **54** 工具;这个数由 v2 spec 决定,加面就会变,**冒烟不要拿它当断言**——要断言就断言 `CheckG10` 已经在 CI 里断言过的那件事:每条该进面的 GET 都有工具。冒烟调用走 `searchCatalog`——早先写的 `galgame_search` 随 `/v1/galgame` 面于 wave 146 退役,`catalog_search` 随 `409a7a80` 的 spec 派生改名。news 工具现在无需凭据,拿它冒烟也不会再撞上一个会被误读成部署失败的 403。)

## 6. 阶段

- **M1(本波)**:§1-§5 全部;门户 docs 页加「AI/MCP 接入」一节(端点、
  key 配置示例:Claude Code / Claude Desktop / 通用 MCP 客户端片段)。
- **M2(触发式)**:MCP resources(work 页面作为资源)、prompts、OAuth
  2.1、stdio 自托管包、写面工具。触发条件 = 真实外部消费者出现。
