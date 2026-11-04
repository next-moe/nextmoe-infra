# AI / MCP 接入

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

NextMoe 开放 API 同时以 MCP（Model Context Protocol）server 暴露：端点 https://mcp.nextmoe.dev/mcp，Streamable HTTP、stateless，带上同一把 API 密钥即可；不带密钥也能用，额度按你自己的 IP 计（默认每分钟 30 次、每天 1,000 次）。它是一层纯透传适配——每次工具调用就是一次对公开 /v2 GET 的请求，鉴权、限流、配额与用量与直连毫无区别。工具名就是 OpenAPI operationId，清单由 cmd/gen-v2-portal 从同一份 v2 spec 生成。

## 工具（67 个）

- `getCatalogCharacter` `GET /v2/catalog/characters/{id}`（无需密钥）：按 id 取角色；view=full 追加性别、生日、三围、血型与 instance_of_id。nsfw 同时控 r18 作品与 sexual 系 traits 的可见性。
- `getCatalogCharacterAppearances` `GET /v2/catalog/characters/{id}/appearances`（无需密钥）：一个角色出演的全部作品，各带 roster_role、剧透等级与配音署名，offset 游标分页。
- `getCatalogCompany` `GET /v2/catalog/companies/{id}`（无需密钥）：按 id 取公司 / 厂牌 / 社团注册行（v1 的 labels）。被合并的 id 返回 404 ENTITY_MERGED。
- `getCatalogCompanyGraph` `GET /v2/catalog/companies/{id}/graph`（无需密钥）：一次拿到一家公司周围的整个会社家族（母公司 / 子品牌 / 文库 / 继承），nodes[] + edges[] 有向。getCatalogCompany 的关联只有一跳，问「某社旗下有哪些牌子」用这个；反向边按设计不下发。
- `getCatalogCreditName` `GET /v2/catalog/credit-names/{id}`（无需密钥）：按 id 取一个署名（credit name）——这是名字不是人；尚未挂到人物身份上时 person_id 为 null。
- `getCatalogCreditNameCredits` `GET /v2/catalog/credit-names/{id}/credits`（无需密钥）：一个署名被记在哪些作品上，offset 游标分页——问「这个名字做过什么」用它。
- `getCatalogEngine` `GET /v2/catalog/engines/{id}`（无需密钥）：按 id 取引擎记录（名称与跨源 refs）。
- `getCatalogPerson` `GET /v2/catalog/persons/{id}`（无需密钥）：按 id 取人物身份：它把同一个人用过的多个署名聚成一个人格。被合并的 id 返回 404 ENTITY_MERGED。
- `getCatalogPersonCreditNames` `GET /v2/catalog/persons/{id}/credit-names`（无需密钥）：一个人物身份名下的全部署名。人物与署名是两层：身份在这里，某个署名各自的作品在 getCatalogCreditNameCredits。
- `getCatalogProposal` `GET /v2/catalog/proposals/{id}`（无需密钥）：按 id 取单条提案的公开透明视图：提案人、状态、目标实体与时间戳；include=amendments 追加修订链。
- `getCatalogRelease` `GET /v2/catalog/releases/{id}`（无需密钥）：按 id 取单条发售行。被合并的 id 返回 404 ENTITY_MERGED；母作品是 r18 时，不带 nsfw=true 也是 404。
- `getCatalogRevision` `GET /v2/catalog/revisions/{id}`（无需密钥）：按 id 取单条修订；include=diff 追加相对 diff_base（缺省为前一条）的字段级变更集。
- `getCatalogRole` `GET /v2/catalog/roles/{id}`（无需密钥）：按 id 取署名职务注册表的一行。未知 id 为 404。
- `getCatalogSchema` `GET /v2/catalog/schemas/{object}`（无需密钥）：一个实体家族的可编辑字段 schema 与 include 令牌全集（含 FULL_SET）。无需凭据，且不评估调用方权限——它描述的是形状，不是许可。
- `getCatalogSeries` `GET /v2/catalog/series/{id}`（无需密钥）：按 id 取系列（身份、源锚与简介）；成员作品用 listCatalogWorks 的 series_id= 取——回答「这个系列按什么顺序玩」。
- `getCatalogStats` `GET /v2/catalog/stats`（无需密钥）：全库计数：各家族 LIVE 实体总量。无参数，无需凭据。
- `getCatalogTag` `GET /v2/catalog/tags/{id}`（无需密钥）：按 id 取正典标签——跨源标签词表的一行。
- `getCatalogTagExclusion` `GET /v2/catalog/tag-exclusion`（无需密钥）：解析一组排除 tag：给 exclude_tag_id / exclude_tag_tree，返回展开后的 tag id 与带有其中任一 tag 的全部作品 id，供站点在自建列表里剔除。
- `getCatalogTrait` `GET /v2/catalog/traits/{id}`（无需密钥）：按 id 取角色特征词表的一行。
- `getCatalogWalkthrough` `GET /v2/catalog/walkthroughs/{id}`（无需密钥）：按 id 取一份攻略的选项图：入口、存档点、结局为节点，选项段为边，并标出每条路线与结局所属的角色。文本是原文（ja），localized 里是 zh-Hans 机翻；r18 作品需 nsfw=true。
- `getCatalogWork` `GET /v2/catalog/works/{id}`（无需密钥）：按 id 取作品注册行，include=credits,relations 并取子块。被合并的 id 返回 404 ENTITY_MERGED 并在 Link rel=canonical 里给继任者；r18 不带 nsfw=true 同样是 404。
- `getCatalogWorkCharacters` `GET /v2/catalog/works/{id}/characters`（无需密钥）：作品的角色花名册块（单块 cursor 分页，带 roster_role 与配音署名）。此面不设剧透上限——每行自带 spoiler 等级，由调用方分级。
- `getCatalogWorkCovers` `GET /v2/catalog/works/{id}/covers`（无需密钥）：作品的封面块（单块 cursor 分页，与 include=covers 同一批行）。只要这一块时优先于 getCatalogWork；CDN 渲染不了的行在分页前就被丢掉，所以短页是「块取完了」而不是「被过滤了」。
- `getCatalogWorkCredits` `GET /v2/catalog/works/{id}/credits`（无需密钥）：作品的职员署名块，按 role 分组。分页按署名行不按组：跨页的 role 会在两页各出现一次、各带该页的切片，拼页时要按 role 合并组。
- `getCatalogWorkEngines` `GET /v2/catalog/works/{id}/engines`（无需密钥）：作品的引擎块（单块 cursor 分页，与 include=engines 同一批行）。
- `getCatalogWorkIntros` `GET /v2/catalog/works/{id}/intros`（无需密钥）：作品的简介块（单块 cursor 分页，一语言一行）。源写的胜过机翻的，机翻行是打标不是隐藏——把它引用成「官方说法」前先看标。
- `getCatalogWorkLinks` `GET /v2/catalog/works/{id}/links`（无需密钥）：作品的非身份外链块（官网 / Steam / X 等，单块 cursor 分页）。这些是地址不是锚，身份锚在 getCatalogWork 的 refs[]；dlsite / dmm 这类无法由裸 code 还原商店 URL 的源按设计不在此面。
- `getCatalogWorkRatings` `GET /v2/catalog/works/{id}/ratings`（无需密钥）：作品的分源评分块（单块 cursor 分页，带完整投票直方图与离散度）。分数留在各源原生标尺，永不混算成一个数。
- `getCatalogWorkRelations` `GET /v2/catalog/works/{id}/relations`（无需密钥）：作品的关联作品块（单块 cursor 分页）。不带 nsfw 时 r18 关联端是整条丢弃而非置空，页里数的是幸存行。
- `getCatalogWorkReleases` `GET /v2/catalog/works/{id}/releases`（无需密钥）：作品的发售行块（单块 cursor 分页，各带源锚与自己的发行公司——移植版 / 英文版各自的发行商在这里）。跨作品的发售时间线用 listCatalogReleases。
- `getCatalogWorkScreenshots` `GET /v2/catalog/works/{id}/screenshots`（无需密钥）：作品的截图块（单块 cursor 分页，带尺寸与 thumbhash）。每行各自带 sexual / violence 等级——此面只报告不过滤，渲染门由调用方自己把。
- `getCatalogWorkSeries` `GET /v2/catalog/works/{id}/series`（无需密钥）：作品所属系列块（单块 cursor 分页）。member_count 是系列的全部成员数而非本页——顺着它用 listCatalogWorks 的 series_id= 或 getCatalogSeries。
- `getCatalogWorkTags` `GET /v2/catalog/works/{id}/tags`（无需密钥）：作品的源标签块（单块 cursor 分页）。命中正典映射的行带 canonical id 与 tier / kind，未映射的行不带。
- `getNewsItem` `GET /v2/news/{id}`（无需密钥）：按 id 取单条资讯。已撤回的、上游原文已消失的条目返回 404——这是契约不是查不到，别重试，也别拿缓存副本顶上。
- `getProblemType` `GET /v2/problems/{code}`（无需密钥）：按 code 取单条错误码定义。未知 code 返回 404 而不是 422——路径参数是查找键，不是封闭枚举。
- `getRating` `GET /v2/ratings/{id}`：按评分 id 取这一条。一个人、一部作品、所有 NextMoe 站点只有一条评分；没有这条，包括已删除的，是 404。需要应用密钥，或带 catalog:read 的用户令牌。
- `getVocabulary` `GET /v2/vocabularies/{name}`（无需密钥）：按 name 取一个词表的全部已发布取值。未知 name 返回 404。
- `listCatalogCalendar` `GET /v2/catalog/calendar`（无需密钥）：发售月历：month= / year= 选窗口，precision= 与 status= 在「已定档到月」「只知年」「已公布未定档」三个视图间切换——v1 的三条月历路径在 v2 是这一条。不接受 ids=。
- `listCatalogChanges` `GET /v2/catalog/changes`（无需密钥）：增量同步变更流：近期更新过的作品，最旧优先。存下 next_cursor，下次轮询只拿变化的部分。
- `listCatalogCharacters` `GET /v2/catalog/characters`（无需密钥）：keyset 分页浏览角色注册表。ids=/refs= 是批量水合泳道，不分页。
- `listCatalogClaimEvents` `GET /v2/catalog/claim-events`：认领生命周期事件流，默认最新在前；sort=recorded_asc 是镜像 / 发奖 cron 按 id 水位续读的那条泳道。事件带驳回理由与操作者 uid，所以除 catalog:read 外还要运营发放的 claim_events:read。
- `listCatalogCompanies` `GET /v2/catalog/companies`（无需密钥）：浏览公司 / 厂牌词表本身（v1 的 labels）——用来发现 company id 再喂给 listCatalogWorks 的 company_id=。
- `listCatalogCreditNames` `GET /v2/catalog/credit-names`（无需密钥）：keyset 分页浏览署名注册表，q= 按名字过滤。ids=/refs= 是批量水合泳道，不分页。
- `listCatalogDiscussion` `GET /v2/catalog/discussions/{object}/{id}`（无需密钥）：一条提案（或合并请求）下的讨论串，按时间正序：留言人、是否审核者、回复的上一条与正文。提案的讨论公开可读；合并请求的讨论只对请求人与合并审核者可见。
- `listCatalogEngines` `GET /v2/catalog/engines`（无需密钥）：浏览引擎词表本身——用来发现 engine id 再喂给 listCatalogWorks 的 engine_id=。
- `listCatalogNames` `GET /v2/catalog/names`（无需密钥）：一次解析多个族的显示名：keys=family:id,…（最多 500 个，如 tag:12,company:3），各族按自己的 ids= 批量通道取名，可见性与 nsfw 门完全一致；有中文名时另给 name_zh。只需要名字时用它代替逐族的 ids= 调用。
- `listCatalogPersons` `GET /v2/catalog/persons`（无需密钥）：keyset 分页浏览人物身份注册表。ids=/refs= 是批量水合泳道，不分页。
- `listCatalogProposals` `GET /v2/catalog/proposals`（无需密钥）：已提交的编辑提案流，最新优先。proposer_uid= + state=merged + include_total=true 就是按贡献者的合入计数。此面不下发 patch，也不下发审核意见。
- `listCatalogRankings` `GET /v2/catalog/rankings`（无需密钥）：评分排行榜：跨 VNDB / Bangumi / DLsite / 批评空间 / NextMoe / HowLongToBeat 的共识分（偏差值），或 by= 按单一来源排。条目带名次、排名值与各来源原分。默认只排视觉小说，scope=all 含普通游戏；listCatalogWorks 的过滤与 facets 这里都能用。模型尚未拟合时返回 503。
- `listCatalogRedirects` `GET /v2/catalog/redirects`（无需密钥）：合并去向流：被合并掉的 id 指向哪个继任者，最旧优先。存下游标增量消费，就能把本地副本里的死 id 换成活的。object= 收敛到单个家族；不接受 ids=。
- `listCatalogReleases` `GET /v2/catalog/releases`（无需密钥）：发售动态的 release 粒度：每一条发售行各自成项，移植版 / 复刻 / 中文化都看得见（月历只把作品放在最早发售月且只显示一次）。缺省按日期倒序。
- `listCatalogRevisions` `GET /v2/catalog/revisions`（无需密钥）：已合入的编辑修订流，缺省最新优先；sort=recorded_asc 按 id 从旧到新走同一个集合，这是镜像与贡献统计该用的姿态（配一条水位线）。object= + entity_id= 收敛到单个实体的历史。
- `listCatalogRoles` `GET /v2/catalog/roles`（无需密钥）：浏览署名职务注册表本身（全表约 231 行）——key 与署名组的 role_key 相接。refs= 对它不解析：职务没有外部锚类型。
- `listCatalogSeries` `GET /v2/catalog/series`（无需密钥）：浏览系列词表本身。系列不进搜索索引，这是发现 series id 的唯一入口；refs= 对它不解析：系列没有外部锚类型。
- `listCatalogSplits` `GET /v2/catalog/splits`（无需密钥）：拆分流:哪部作品把哪些发行拆成了哪部新作品,按变化时间升序。存下游标增量消费,就能把挂在这些发行上的本地数据跟到 new_work_id;撤销的拆分以 state=undone 重新出现在尾部,新作品经 redirects 指回原作品。
- `listCatalogTagCategories` `GET /v2/catalog/tag-categories`（无需密钥）：列出标签的浏览分组（题材类型、角色、恋爱与关系……），一页取全。分组 id 喂给 listCatalogTags 的 category=。
- `listCatalogTags` `GET /v2/catalog/tags`（无需密钥）：浏览正典标签词表本身——用来发现 tag id 再喂给 listCatalogWorks 的 tag_id=。category= 只看一个分组，sort=work_count 按作品数排。
- `listCatalogTraits` `GET /v2/catalog/traits`（无需密钥）：浏览角色特征词表本身——用来发现 trait id。refs= 对它不解析：特征没有外部锚类型。
- `listCatalogWalkthroughs` `GET /v2/catalog/walkthroughs`（无需密钥）：列出攻略（不含选项图）：work_id= 取一部作品的攻略，character_id= 取通向某角色的攻略，ids= 批量读。可见性与作品一致，r18 需 nsfw=true；content_limit= 按作品展示轴过滤（与 nsfw 独立，面向 sfw 读者传 nsfw=true&content_limit=sfw），条目带 work_content_limit。
- `listCatalogWorks` `GET /v2/catalog/works`（无需密钥）：浏览 / 过滤作品注册表（评级 / 公司 / 标签 / 系列 / 引擎 / 平台 / 发售窗，keyset 分页，ids=/refs= 批量水合）。带 q= 时整条转为检索并按相关度排序——「查询 + 过滤」与「纯过滤」在 v2 是同一条路径。
- `listNews` `GET /v2/news`（无需密钥）：合作媒体的 Galgame 资讯索引（keyset 分页，无需凭据）。只有标题、摘要与题图，正文永不下发——每条恒带来源与 source_url，读全文要回到媒体自己的站点。
- `listNewsSources` `GET /v2/news/sources`（无需密钥）：资讯来源注册表：每家媒体的名称、主页、专栏入口，以及该渲染的归属文案。无参数，无需凭据。
- `listProblemReasons` `GET /v2/problems/reasons`（无需密钥）：字段级 reason 的封闭清单，无需凭据。这里的取值永远不会作为顶层 code 出现。
- `listProblemTypes` `GET /v2/problems`（无需密钥）：错误码注册表：/v2 全部顶层 code 的封闭清单，keyset 分页，无需凭据。
- `listRatings` `GET /v2/ratings`：一个人在一部作品上的评分，全 NextMoe 站点共用一条。work_id= 或 user_id= 至少给一个，两个都给就是这个账号对这部作品的那一条；按创建时间从新到旧。需要应用密钥，或带 catalog:read 的用户令牌。
- `listVocabularies` `GET /v2/vocabularies`（无需密钥）：已发布词表清单（封闭词表与 seed-open 词表），keyset 分页，无需凭据。
- `searchCatalog` `GET /v2/catalog/search`（无需密钥）：按名字跨家族搜身份图谱：object= 选家族（works / characters / credit-names / companies …），命中行是 search_result，各带 target_object 说明它是什么。r18 需 nsfw=true；不接受 cursor= 与 ids=。

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/mcp
