// The roster itself is derived, never hand-kept: cmd/gen-v2-portal runs the
// same mcpface.ToolsFromSpec the MCP server registers with and writes
// app/generated/mcp-tools.mjs. This file adds only the Chinese one-liner, keyed
// by operationId, and refuses to load when the two sides disagree.
//
// The assertion is why it exists. The previous roster was a hand-written list
// that still named v1-era tools (catalog_search, news_list, …) months after the
// server had switched to v2 operationIds, and nothing caught it because nothing
// compared the two. Plain .mjs because both consumers must read the SAME list:
// docs/mcp/Container.vue renders it, and scripts/gen-llms.mjs (bare node, no TS
// loader) writes docs/mcp.md from it.

import { mcpTools } from '../app/generated/mcp-tools.mjs'

const DESCRIPTIONS = {
  searchCatalog:
    '按名字跨家族搜身份图谱：object= 选家族（works / characters / credit-names / companies …），命中行是 search_result，各带 target_object 说明它是什么。r18 需 nsfw=true；不接受 cursor= 与 ids=。',
  getCatalogWork:
    '按 id 取作品注册行，include=credits,relations 并取子块。被合并的 id 返回 404 ENTITY_MERGED 并在 Link rel=canonical 里给继任者；r18 不带 nsfw=true 同样是 404。',
  listCatalogWorks:
    '浏览 / 过滤作品注册表（评级 / 公司 / 标签 / 系列 / 引擎 / 平台 / 发售窗，keyset 分页，ids=/refs= 批量水合）。带 q= 时整条转为检索并按相关度排序——「查询 + 过滤」与「纯过滤」在 v2 是同一条路径。',
  listCatalogChanges:
    '增量同步变更流：近期更新过的作品，最旧优先。存下 next_cursor，下次轮询只拿变化的部分。',
  listCatalogClaimEvents:
    '认领生命周期事件流，默认最新在前；sort=recorded_asc 是镜像 / 发奖 cron 按 id 水位续读的那条泳道。事件带驳回理由与操作者 uid，所以除 catalog:read 外还要运营发放的 claim_events:read。',
  getCatalogCreditName:
    '按 id 取一个署名（credit name）——这是名字不是人；尚未挂到人物身份上时 person_id 为 null。',
  listCatalogCreditNames:
    'keyset 分页浏览署名注册表，q= 按名字过滤。ids=/refs= 是批量水合泳道，不分页。',
  getCatalogCreditNameCredits:
    '一个署名被记在哪些作品上，offset 游标分页——问「这个名字做过什么」用它。',
  getCatalogPerson:
    '按 id 取人物身份：它把同一个人用过的多个署名聚成一个人格。被合并的 id 返回 404 ENTITY_MERGED。',
  listCatalogPersons:
    'keyset 分页浏览人物身份注册表。ids=/refs= 是批量水合泳道，不分页。',
  getCatalogPersonCreditNames:
    '一个人物身份名下的全部署名。人物与署名是两层：身份在这里，某个署名各自的作品在 getCatalogCreditNameCredits。',
  getCatalogCharacter:
    '按 id 取角色；view=full 追加性别、生日、三围、血型与 instance_of_id。nsfw 同时控 r18 作品与 sexual 系 traits 的可见性。',
  listCatalogCharacters:
    'keyset 分页浏览角色注册表。ids=/refs= 是批量水合泳道，不分页。',
  getCatalogCharacterAppearances:
    '一个角色出演的全部作品，各带 roster_role、剧透等级与配音署名，offset 游标分页。',
  getCatalogCompany:
    '按 id 取公司 / 厂牌 / 社团注册行（v1 的 labels）。被合并的 id 返回 404 ENTITY_MERGED。',
  listCatalogCompanies:
    '浏览公司 / 厂牌词表本身（v1 的 labels）——用来发现 company id 再喂给 listCatalogWorks 的 company_id=。',
  getCatalogCompanyGraph:
    '一次拿到一家公司周围的整个会社家族（母公司 / 子品牌 / 文库 / 继承），nodes[] + edges[] 有向。getCatalogCompany 的关联只有一跳，问「某社旗下有哪些牌子」用这个；反向边按设计不下发。',
  getCatalogTag: '按 id 取正典标签——跨源标签词表的一行。',
  getCatalogTagExclusion:
    '解析一组排除 tag：给 exclude_tag_id / exclude_tag_tree，返回展开后的 tag id 与带有其中任一 tag 的全部作品 id，供站点在自建列表里剔除。',
  listCatalogTags:
    '浏览正典标签词表本身——用来发现 tag id 再喂给 listCatalogWorks 的 tag_id=。',
  getCatalogTrait: '按 id 取角色特征词表的一行。',
  listCatalogTraits:
    '浏览角色特征词表本身——用来发现 trait id。refs= 对它不解析：特征没有外部锚类型。',
  getCatalogEngine: '按 id 取引擎记录（名称与跨源 refs）。',
  listCatalogEngines:
    '浏览引擎词表本身——用来发现 engine id 再喂给 listCatalogWorks 的 engine_id=。',
  getCatalogRole: '按 id 取署名职务注册表的一行。未知 id 为 404。',
  listCatalogRoles:
    '浏览署名职务注册表本身（全表约 231 行）——key 与署名组的 role_key 相接。refs= 对它不解析：职务没有外部锚类型。',
  listCatalogNames:
    '一次解析多个族的显示名：keys=family:id,…（最多 500 个，如 tag:12,company:3），各族按自己的 ids= 批量通道取名，可见性与 nsfw 门完全一致；有中文名时另给 name_zh。只需要名字时用它代替逐族的 ids= 调用。',
  getCatalogSeries:
    '按 id 取系列（身份、源锚与简介）；成员作品用 listCatalogWorks 的 series_id= 取——回答「这个系列按什么顺序玩」。',
  listCatalogSeries:
    '浏览系列词表本身。系列不进搜索索引，这是发现 series id 的唯一入口；refs= 对它不解析：系列没有外部锚类型。',
  getCatalogRelease:
    '按 id 取单条发售行。被合并的 id 返回 404 ENTITY_MERGED；母作品是 r18 时，不带 nsfw=true 也是 404。',
  listCatalogReleases:
    '发售动态的 release 粒度：每一条发售行各自成项，移植版 / 复刻 / 中文化都看得见（月历只把作品放在最早发售月且只显示一次）。缺省按日期倒序。',
  listCatalogCalendar:
    '发售月历：month= / year= 选窗口，precision= 与 status= 在「已定档到月」「只知年」「已公布未定档」三个视图间切换——v1 的三条月历路径在 v2 是这一条。不接受 ids=。',
  getCatalogStats: '全库计数：各家族 LIVE 实体总量。无参数，无需凭据。',
  getCatalogWorkCovers:
    '作品的封面块（单块 cursor 分页，与 include=covers 同一批行）。只要这一块时优先于 getCatalogWork；CDN 渲染不了的行在分页前就被丢掉，所以短页是「块取完了」而不是「被过滤了」。',
  getCatalogWorkScreenshots:
    '作品的截图块（单块 cursor 分页，带尺寸与 thumbhash）。每行各自带 sexual / violence 等级——此面只报告不过滤，渲染门由调用方自己把。',
  getCatalogWorkTags:
    '作品的源标签块（单块 cursor 分页）。命中正典映射的行带 canonical id 与 tier / kind，未映射的行不带。',
  getCatalogWorkCharacters:
    '作品的角色花名册块（单块 cursor 分页，带 roster_role 与配音署名）。此面不设剧透上限——每行自带 spoiler 等级，由调用方分级。',
  getCatalogWorkCredits:
    '作品的职员署名块，按 role 分组。分页按署名行不按组：跨页的 role 会在两页各出现一次、各带该页的切片，拼页时要按 role 合并组。',
  getCatalogWorkReleases:
    '作品的发售行块（单块 cursor 分页，各带源锚与自己的发行公司——移植版 / 英文版各自的发行商在这里）。跨作品的发售时间线用 listCatalogReleases。',
  getCatalogWorkIntros:
    '作品的简介块（单块 cursor 分页，一语言一行）。源写的胜过机翻的，机翻行是打标不是隐藏——把它引用成「官方说法」前先看标。',
  getCatalogWorkRatings:
    '作品的分源评分块（单块 cursor 分页，带完整投票直方图与离散度）。分数留在各源原生标尺，永不混算成一个数。',
  getCatalogWorkRelations:
    '作品的关联作品块（单块 cursor 分页）。不带 nsfw 时 r18 关联端是整条丢弃而非置空，页里数的是幸存行。',
  getCatalogWorkSeries:
    '作品所属系列块（单块 cursor 分页）。member_count 是系列的全部成员数而非本页——顺着它用 listCatalogWorks 的 series_id= 或 getCatalogSeries。',
  getCatalogWorkLinks:
    '作品的非身份外链块（官网 / Steam / X 等，单块 cursor 分页）。这些是地址不是锚，身份锚在 getCatalogWork 的 refs[]；dlsite / dmm 这类无法由裸 code 还原商店 URL 的源按设计不在此面。',
  getCatalogWorkEngines:
    '作品的引擎块（单块 cursor 分页，与 include=engines 同一批行）。',
  listCatalogRevisions:
    '已合入的编辑修订流，缺省最新优先；sort=recorded_asc 按 id 从旧到新走同一个集合，这是镜像与贡献统计该用的姿态（配一条水位线）。object= + entity_id= 收敛到单个实体的历史。',
  getCatalogRevision:
    '按 id 取单条修订；include=diff 追加相对 diff_base（缺省为前一条）的字段级变更集。',
  listCatalogProposals:
    '已提交的编辑提案流，最新优先。proposer_uid= + state=merged + include_total=true 就是按贡献者的合入计数。此面不下发 patch，也不下发审核意见。',
  getCatalogProposal:
    '按 id 取单条提案的公开透明视图：提案人、状态、目标实体与时间戳；include=amendments 追加修订链。',
  listCatalogSplits:
    '拆分流:哪部作品把哪些发行拆成了哪部新作品,按变化时间升序。存下游标增量消费,就能把挂在这些发行上的本地数据跟到 new_work_id;撤销的拆分以 state=undone 重新出现在尾部,新作品经 redirects 指回原作品。',
  listCatalogDiscussion:
    '一条提案（或合并请求）下的讨论串，按时间正序：留言人、是否审核者、回复的上一条与正文。提案的讨论公开可读；合并请求的讨论只对请求人与合并审核者可见。',
  listCatalogRedirects:
    '合并去向流：被合并掉的 id 指向哪个继任者，最旧优先。存下游标增量消费，就能把本地副本里的死 id 换成活的。object= 收敛到单个家族；不接受 ids=。',
  getCatalogSchema:
    '一个实体家族的可编辑字段 schema 与 include 令牌全集（含 FULL_SET）。无需凭据，且不评估调用方权限——它描述的是形状，不是许可。',
  listNews:
    '合作媒体的 Galgame 资讯索引（keyset 分页，无需凭据）。只有标题、摘要与题图，正文永不下发——每条恒带来源与 source_url，读全文要回到媒体自己的站点。',
  listNewsSources:
    '资讯来源注册表：每家媒体的名称、主页、专栏入口，以及该渲染的归属文案。无参数，无需凭据。',
  getNewsItem:
    '按 id 取单条资讯。已撤回的、上游原文已消失的条目返回 404——这是契约不是查不到，别重试，也别拿缓存副本顶上。',
  listProblemTypes:
    '错误码注册表：/v2 全部顶层 code 的封闭清单，keyset 分页，无需凭据。',
  getProblemType:
    '按 code 取单条错误码定义。未知 code 返回 404 而不是 422——路径参数是查找键，不是封闭枚举。',
  listProblemReasons:
    '字段级 reason 的封闭清单，无需凭据。这里的取值永远不会作为顶层 code 出现。',
  listVocabularies:
    '已发布词表清单（封闭词表与 seed-open 词表），keyset 分页，无需凭据。',
  getVocabulary: '按 name 取一个词表的全部已发布取值。未知 name 返回 404。',
  listRatings:
    '一个人在一部作品上的评分，全 NextMoe 站点共用一条。work_id= 或 user_id= 至少给一个，两个都给就是这个账号对这部作品的那一条；按创建时间从新到旧。需要应用密钥，或带 catalog:read 的用户令牌。',
  getRating:
    '按评分 id 取这一条。一个人、一部作品、所有 NextMoe 站点只有一条评分；没有这条，包括已删除的，是 404。需要应用密钥，或带 catalog:read 的用户令牌。'
}

const undescribed = mcpTools
  .filter((t) => !DESCRIPTIONS[t.name])
  .map((t) => t.name)
const orphaned = Object.keys(DESCRIPTIONS).filter(
  (name) => !mcpTools.some((t) => t.name === name)
)
if (undescribed.length || orphaned.length) {
  throw new Error(
    'shared/mcp-tools.mjs is out of step with app/generated/mcp-tools.mjs — ' +
      `missing a description for [${undescribed.join(', ')}], ` +
      `and describing tools the server does not register: [${orphaned.join(', ')}]. ` +
      'Regenerate with `go run ./cmd/gen-v2-portal -o ../developer/app/generated` and edit DESCRIPTIONS.'
  )
}

export const MCP_TOOLS = mcpTools.map((t) => ({
  name: t.name,
  method: t.method,
  path: t.path,
  needsKey: t.needs_key,
  desc: DESCRIPTIONS[t.name]
}))
