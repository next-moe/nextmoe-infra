# 01 — 服务定位与契约

> catalog 是**跨媒介身份/图谱注册层**:把多来源的作品/发行/人物/署名/厂牌收敛成一套带来源锚与分级信任的规范身份。人审经 admin 队列治理;写路径按 per-client site 绑定授权。本篇是对外契约,数据结构以生成的 [v2-openapi.yaml](./v2-openapi.yaml) / [admin-openapi.yaml](./admin-openapi.yaml) 为准。
>
> 🪦 **v1 已于 2026-08-27(wave R3)整面退役。** 本篇的 **§2(S2S `/api/v1/catalog`)、§4(用户令牌面 `/api/v1/user/catalog`)以及凡以 `/v1/catalog`、`/v1/playtime` 描述公开面的段落,均为历史记录**——那些路径现在一律返回 `410 Gone`,`Link` 指向 `/v2`,服务它们的 handler 与 `openapi.yaml` / `public-openapi.yaml` 两份 spec 已删除。在产的继任面是 **`/v2`**([v2-openapi.yaml](./v2-openapi.yaml),札记见 [v2-implementation-notes.md](./v2-implementation-notes.md));**领域语义(实体模型、来源锚、分级信任、展示轴、花名册与归属边)不随路由退役,下文这部分仍然有效**。仍在产的第二个面是 admin 队列 `/api/v1/admin/catalog`。

## 1. 服务定位:registry 层 vs body 层

catalog **只管身份、关系、来源锚**:

- **实体**:`work`(作品)/`release`(发行/SKU)/`credit_name`(署名名义,孤儿合法)/`person`(人)/`character`(角色)/`label`(厂牌/社团/公司)。实体类型常量 `0=person 1=credit_name 3=label 4=character 5=work 6=release 7=tag 8=engine`。**`2` 是已退役的空位**(wave 195,2026-08-08):它曾留给 `org`(厂牌背后的法人),但那张表**一行数据都没存在过**、也没有任何 label 带过 `org_id`——「这个牌子归哪家公司」由 `catalog_label_relation` 回答,而一个可空外键在结构上表达不了八种任意深度的关系(`spawned`/`formerly` 无处安放),更会造出两套公司实体两个 id 空间(VISUAL ARTS 这种自己也出作品的,答案是「都是」)。**公司就是 label**,归属是一条关系边。该值**永不重新分配**(持久化判别值),表与 `catalog_label.org_id` 已 DROP。**7/8 是纯数据位**(A2-0 wiki 注册表抢救波):它们只作为 `catalog_external_ref.entity_type` 出现,承载 wiki 的 tid/eid 地址簿;**无公开读面**——公开 lookup / resolve / redirects 词表不含之,维持既有四/七族。
- **来源锚** `catalog_external_ref`:把实体锚到外部来源的 id,按 `link_kind` 分级 `exact(0)` / `probable(1)` / `related(2)`;exact 有唯一约束(一个来源的一个外部 id 只精确锚一个同类实体)。
- **关系**:credit(**署名边**:work ↔ credit_name/label,"谁演了什么角色/担任什么职务")、**work_label 归属边**(work ↔ label,"哪个社团/发行方对作品负责";`kind`:0=circle/1=publisher/2=developer/3=brand)、**work_character 花名册边**(work ↔ character,"哪个角色出现在作品里";`kind`:0=unknown/1=main/2=secondary/3=appears,**0=unknown 是有意义值**——EG 无主配分型、Bangumi 低频尾型归此;`spoiler`:0=none/1=minor/2=major,VNDB 源、Bangumi/EG 恒 0)、redirect(合并后的旧→新 id)、alias(名义别名)。**署名 ≠ 归属 ≠ 出演**:credit 是个人署名,work_label 是组织责任,work_character 是出演事实(有配音的角色会同时在 credit 与 work_character 两表,语义不同);读面(§2.4/§2.10)负责合并展示,三者并存不互斥。**label_relation 企业结构边**(wave 186,label ↔ label,`catalog_label_relation`)与上述三者正交:它描述的是**厂牌之间**的公司关系,不碰任何作品。一行读作「`other_label_id` 是 `label_id` 的 `<relation>`」,词表是**四对互逆码**:`1=parent`/`2=subsidiary`、`3=imprint`/`4=imprint_of`、`5=spawned`/`6=origin`、`7=succeeded_by`/`8=formerly`(**无 0 值**——未定的关系不是事实)。图**镜像存**(每条事实按两个方向各存一行、码互逆,VNDB `producers_relations` 自身的形状),故**读面永不反转**:`WHERE label_id = ?` 取到什么就渲染什么。主键 `(label_id, other_label_id, relation, source_id)`,`source_id` 在键内故多源可并存;写入是**按源整体重建**(单事务 DELETE + 批插,`cmd/import-label-relations`),两端都必须有该源的 **exact** 厂牌锚,否则计入 `skipped_unanchored` 等下一轮。

catalog **不存**产品展示体:简介、封面/截图字节、评分、点赞、收藏、NSFW 过滤——这些是**产品站(body 层)**各自持有的。产品站保留自己的富行,只把「这是**哪一个**作品/人物」的身份问题委托给 catalog。这条分界是硬约束:catalog 加展示字段 = 越界。(**一处经拍板的例外**:媒体聚合波把封面/截图/简介/评分等**来源事实**收进 catalog 后,wave 175 的最佳封面票也落在这里——票只是封面行的**参考数据**,不改排序、不写编辑钉子、也不带任何展示逻辑,展示与否仍是产品站的事。见 §2.13。)

来源注册表(节选):`source` `2=vndb 3=bangumi 4=dlsite 5=erogamescape 19=nextmoe 20=howlongtobeat 1=user`;`medium` `1=galgame 5=asmr`;`content_rating` `0=all_ages 1=sensitive 2=r18`。完整注册表由 `cmd/migrate catalog` 的 seed 落库。

⚠️ **`content_rating` 是年龄轴,不是展示轴**(A2-R5,doc 106 §38 事故)。这一列答的是「**游戏本体**是什么分级」;「我要**渲染的素材**(封面/截图/简介)能不能摆上公开页」是**另一个问题**,由**编辑展示轴**回答——公开面 `claimed_by.content_limit`(词表 `sfw|nsfw`),认领作品读 **`catalog_work.display_nsfw`**(人工编辑判定;**W1-pre 本体化**前是读时桥接 wiki 正文的 `galgame.content_limit`,refs/proj/140 §5b 把该判定物化成 registry 自己的列,wiki 表族退役后由 catalog 编辑面持有),未认领行按年龄轴回落(该列对未认领行恒 false 且从不被读)。生产实测两轴在 5,568 部作品上不一致(r18 游戏 × 编辑判定 sfw),**互不是对方的放宽或收紧**;把年龄轴当展示门正是那次 SEO 塌缩的根因。语义源 = `model.DisplayLimitKey`,契约与闸参见 [developer-platform/02 §3.2.8](../developer-platform/02-public-api.md#328-编辑展示轴-content_limit闸a2-r5)。**把这条判定缓存到本地列的消费站,同步信道见本篇 §8(`/v2/catalog/changes` 是镜像信道)。**

**release 粒度自 wave 174 起有了公开读面**:`GET /v1/catalog/releases`(发售动态时间线)把**每一行带日期的 release** 当作一个条目按其**自身日期**排序,认领与未认领同规;人口 = LIVE galgame 作品的 release 且日期**至少精确到月**(只到年与无日期者不入本面,它们仍归日历的 pending / tba 两桶)。它是**日历的下一粒度**:日历按作品的**最早**发行日安放作品、一部作品只出现一次,故移植版/复刻版/本地化版在那里**构造上不可见**;本面的 `is_first`(该行是否为该作品最早的带日期 release)正是把二者分开的那一位。`kind` 缺省**排除 trial / patch**(发售动态问的是「东西出了」),`lang` 按 `COALESCE(release.lang, work.olang)` 匹配(dlsite/getchu 泳道的店铺 SKU 不记语言,构造上即作品原语),`official` 视**缺键为 official**(只有 VNDB 泳道写这个旗,写 `false` 即民间汉化/非官方版)。契约见 [developer-platform/02 §3.2](../developer-platform/02-public-api.md)。

**公开面的名字原语自 wave 209 起终局化**:`/v1/catalog` 上**凡出现实体名的投影**——detail、list、花名册 `characters`、`voices`、`credits`、`siblings`、label `relations`、`relation-graph` 节点(08-19 补齐,209 当波漏掉的唯一 label 投影)、`via_label`、实体 search hit——一律是同一组三字段 `display_name` + `latin`(有则)+ `localized{}`(按 canonical BCP-47 tag 取名的 map,无则 `{}`),渲染规则恒为 `localized[locale] ?? display_name ?? latin`。此前一个名字按投影不同有三种形状(detail 用 `display_name`、花名册/credits/relations/search 用 `name`、别名是裸字符串数组),消费端得为每种投影各写一条渲染路径。三处随之改形(**均为 breaking,逐条声明在 `public-openapi-breaking-ignore.txt`**):① search hit / 花名册角色与其 `voices` / `credits` 条目 / label `relations` / `via_label` 的 **`name` 更名为 `display_name`**——同一字符串在同一响应里换键,没有值失联;② **`aliases[]` 从扁平字符串数组升级为 `{value, lang, kind, machine}` 对象数组**(按 value+lang 去重,display name 那一行除外),裸串答不了「这个拼法是哪门语言」和「谁写的」,于是既筛不出 locale、也挡不住机翻文本冒充源数据;③ 五个各自为政的 intro DTO 合并为一个 **`PublicIntro{lang, intro, source, machine}`**(work/character/name/label/tag 同形;tag 的 `machine` **恒 false**——`catalog_tag_intro` 没有 provenance 列)。一项是纯加法:`localized{}` 补齐到此前缺它的每个投影。

**作品标题跟上同一根轴(wave 210,2026-08-19)**:`catalog_work_title` 加 `provenance`(0=source / 1=machine)+ `src_hash` / `mt_model`,与三张别名表、四张 intro 表同义。读面随之动:详情面 `titles[]` 每行、S2S `WorkTitle` 每行补 `machine`(仅为真时出现),`localized{}` 的每个 locale 条目同带该旗。选举定序改为 **provenance → kind → id**:**源标题恒胜,机翻只能占源没填的空 locale**,与上一段实体名的规矩逐字相同。供给侧同波补齐两条**源**泳道(bangumi `name_cn`、VNDB 非机翻中文 release 标题,均 fill-missing、provenance=0),机翻只覆盖两条源都供不出的残量;`src_hash` 是日文原名的 sha256,原名一改该行即到期重译。

**works 名字原语终局化,四槽 `names` 退役(wave 212,2026-08-19;A 波双发 + B 波删除,零 schema 零迁移)**:wave 209 给公开面每个带名实体统一了 `display_name` + `latin` + `localized{}`,唯独漏了 **works**——作品此前只有专为论坛留的四槽 `names{ja-jp, zh-cn, zh-tw, en-us}`。四槽是有损的:`lang` 为 ko/ru/vi 或未标注的标题**没有槽位可去,构造上不可达**(生产实测 `catalog_work_title.lang` 分布 ja / en / zh-Hans / zh-Hant / zh / 空,四槽已在丢 `zh` 与空 lang 的信息,新语种一进来即整批不可见)。**A 波**给四个作品投影补上同一组原语并与四槽双发;**B 波**在论坛与 moyu 两家消费端迁完并上线后把兼容层整层删掉——公开面自此**不存在任何 `ja-jp` / `zh-cn` / `zh-tw` / `en-us` 键**:

| 投影 | 名字原语 | 时机 |
|---|---|---|
| `works/{id}` 详情 `PublicCatalogWork` | `latin` + `localized{}` | **恒带**,无本地化名发 `{}` |
| `works` 列表行 `PublicWorkListItem` | `latin` + `localized{}` | **仅 `include=names`**,未点名时**整键缺席而非发 `{}`**——「没请求」与「没有」是两个断言 |
| 作品 brief `PublicWorkBrief`(relations / series_siblings / lookup / tag·label·name·character 的 works 子列表)| `latin` + `localized{}` | 与标题同一次查询,`localized` 恒带、空为 `{}` |
| 实体 search 的 works 类 hit | `localized{}`(`latin` 本已有,来自搜索索引文档)| ≤20 命中一次批查 |

- **选举沿用四槽那一套**:同一批 `catalog_work_title` 行、同一个 `(provenance, kind, id)` 定序、同一个 first-row-wins 扫描,**源标题恒胜机翻、official 胜 alias**,机翻占空 locale 时带 `machine: true`。差别只在键:四槽是四个固定产品键,`localized{}` 是**任意 canonical BCP-47 tag**(`zh-hant` 归一为 `zh-Hant`,**裸 `zh` 保留为 `zh` 不猜字形**),`lang` 为空或不成其为标签的行**一律不入**(它们仍在 `titles[]` 里可列可搜)。
- **`include=` 的六个 token 拼写不随块退役而改名**:`names,intros,labels,ratings,covers,refs` 逐字照旧,`include=names` 现在闸的是 `latin` + `localized{}`。moyu 全站标题都走 `include=names`,把这个 token 改名会让它全站渲染空标题、而两侧都不报任何错——有测试钉住这六个拼写。
- **`kind` 是两套不相交的词表**:实体名(角色/厂牌/名义)说 `translation|spelling_variant`(取自三张别名表),**作品标题说 `official|alias|abbreviation`**(取自 `catalog_work_title`)。按实体类型读,别拿一套词表校验另一套。
- **`latin` = display 行的罗马字**:顺序扫标题行,取第一条 `title == display_name` 的行的 `latin`;无此行或该行无 latin 则整键缺席。
- **详情面的 `search_hint` 陷阱**:详情走 `withHints=true` 载入(比列表多一类行、`ORDER BY` 多一层 `lang`),`kind=search_hint` 的行**在认领 locale 槽之前**即被剔除——先认槽再剔除会把它背后的真标题一起丢掉,两条路径的选举结果因此逐字相等(有测试钉住)。

同波另有两处加法件(均恒出、空值分别为 `[]` / `{}`;relation-graph 节点的 `localized{}` **连同 `name`→`display_name` 改名**已由同日独立波先行完成,不在本波):

- **`labels` 列表行补 `aliases[]`**(`{value, lang, kind, machine}`,与 `labels/{id}` 同语义、同去重),与该行的 `localized{}` 共用同一次别名批查,不增查询。
- **角色 `traits[]` 补 `localized{}` + `group_localized{}`**:值取自 `catalog_character_trait.name_zh`(组名经 `group_tid` 自连接解析),键恒为 **`zh-Hans`**——该列是简体(3,327 条已填行中 738 条含简体专用字、**0 条含繁体专用字**),`kind` 为 `translation`,`machine` 如实映射 `name_zh_provenance`(0=策展/1=机翻,该列此前不上读面,生产分布 3,098 / 229)。
- **文档注记**:`tags/{id}` 的 `intros[].machine` **恒 false**——`catalog_tag_intro` 没有 provenance 列,该旗在这一面记的是「未知」而不是「人写的」,已写进字段 doc,免得下游当 bug 报。

B 波删掉的四件(**均为 breaking,逐条声明在 `public-openapi-breaking-ignore.txt`**):

- **四个作品投影的 `names` 块整体删除**(列表行、search hit、brief、日历行);渲染改用上表的 `localized[locale] ?? display_name ?? latin`。
- **works 列表的 `intros` 块从四槽对象改成 `[{lang, intro, source, machine}]` 数组**——即详情面自 wave 209 起发的那个 `PublicIntro` 数组:一语言一元素,同一批 `catalog_work_intro` 行、同一套选举,两面对同一部作品给出逐字相同的数组(有测试钉住)。
- **详情面的简介键 `intro` 更名为 `intros`**(数组、元素、次序都不变,只换键),与列表块以及 label / tag / character / name 各面对齐。**S2S 读面的 `intro` 是另一张面,不动。**
- **角色 `traits[]` 的扁平 `name_zh` / `group_zh` 删除**,由 A 波补上的 `localized["zh-Hans"]` / `group_localized["zh-Hans"]` 接管——同一个串,另带 provenance。**S2S 面的 `name_zh` / `group_name_zh` 不动。**

**`localized{}` 的机器填缺(2026-08-18 裁决,推翻旧规)**:机翻别名行(`provenance=1`)现在**可以占一个 locale 槽,但仅当该 locale 没有任何 source 行**,占位时带 `machine: true`;source 行**恒胜**,机翻行只能补空、永不遮蔽(与 §2.4 intro 块的 shadow-never-delete 同规)。旧规「`localized{}` 只收权威名」把机翻行整个挡在外面,代价是中文读者对着日文 `display_name`,而可用的中文写法就躺在别名表里够不着。要挡机翻的消费端按 `machine` 过滤;不挑的消费端从此**只需 `localized[locale] ?? display_name` 两级**——`?? latin` 是名字在该 locale 完全无着落时的最后一格,机器填缺之后极少走到,而按投影分支的那套渲染逻辑已随本波作废。公开面全文契约见 [developer-platform/02 §3.7](../developer-platform/02-public-api.md)。

## 2. S2S 端点(Basic client 认证,前缀 `/api/v1/catalog`)

写/运维面:resolve(2.1)· redirects feed(2.2)· claim(2.3,带 site 绑定)——**人类写端点已在 wave 181/185 全部退役,见 2.12 / 2.13 / 2.15**。读面(D-01,2.4-2.6):by-anchor · credits · entity search。内部浏览器(D-02,2.7):stats · works/{id} · labels/{id}/works。产品建游面(2.8):works/search。实体读面(2.9-2.11):names/{id}/works · characters/{id}/works · characters/{id}。

### 2.1 `POST /catalog/resolve` — 批量 id 规范化(只读)

一次解析同一 `entity_type` 下最多 1000 个 id 到其规范 id(跟随 redirect)。

- 请求:`{ "entity_type": 5, "ids": [12, 34, …] }`
- 响应:`{ "mappings": {"12": 12, "34": 99}, "redirected": [34] }`——`mappings` 是「旧 id(字符串键)→ 规范 id」,未被 redirect 的 id 映射到自身;`redirected` 是其中发生过跳转的子集。
- 用途:产品站在展示/入库前把手里的 catalog id 归一到当前规范 id。**不受 site 绑定限制。**

### 2.2 `GET /catalog/redirects` — redirect keyset feed(只读)

按 `merged_at` 升序的 keyset 分页,吐出所有「旧 id → 新 id」的合并边,供产品站的清理 cron 增量消费(把本地存的旧 catalog id 批量改写到新 id)。

- 响应:`{ "items": [{entity_type, old_id, current_id, merged_at}], "next_cursor": "…" }`。
- 客户端持久化 `next_cursor`,下次轮询回传;`next_cursor` 为空表示当前页未满(已追平)。**不受 site 绑定限制。**

### 2.3 `POST /catalog/works/claim` — 作品认领/注册(写)

把一个产品侧作品行认领到一个 catalog work 身份。

- 请求:`{ medium_id, site, product_work_id, display_name, olang?, content_rating?, anchors?[{source_id, external_id, level?}] }`。`level ∈ {work, release}`,缺省 `work`。
- **锚的层级(R3/R5)**:SKU 性质的外部 id(**DLsite workno**、VNDB release)锚 **release**,作品性质的(Bangumi subject、VNDB vn)锚 **work**。同人站以 dlsite_id 认领时须传 `level:"release"`。
- 语义:锚查找**跨 work/release 两级**——release 级锚经 `catalog_release.work_id` 回溯其 owning work。三分支:
  1. **锚命中未认领 work**(site=NULL)→ 认领成功(填 site/product_work_id、stub→live),该 work 既有身份资产(EG ref / credits / labels / releases)**全数继承**;
  2. **锚命中已被他站认领的 work** → **409 冲突**,响应带**结构化归属**(见下),调用方只记 link 不抢占;
  3. **锚无命中** → 铸新 work;其中 `level:"release"` 的锚落在**新建的 release** 上(绝不落 work),从而与后续按 release 锚去重的导入**天然同一身份**,不产生分裂。`work` 级锚落 work。
- 响应 `{ work_id, created }`,`created=true` 表示新铸。
- **幂等**:同一 (site / product_work_id) 重复 claim 收敛到同一 `work_id`。
- **冲突 409**:house 信封 `{code, message, data}`,`data = { work_id, owning_site, owning_product_work_id? }`——`work_id` 是锚已解析到的 catalog work,`owning_site`/`owning_product_work_id` 是占坑方。绝不抢占他站身份。
- ⚠️ **site 绑定要求(写端点独有)**:见 §4。

### 2.4 `GET /catalog/works/by-anchor?source=&external_id=` — 锚反查读穿(只读)

产品站拿一个外源 id 读穿到 catalog 作品。`source` 对 `catalog_source` 注册表校验(即白名单:dlsite/vndb/bangumi/erogamescape/…);命中 **work 级或 release 级锚**均可(release 锚回溯其 work;`exact` 优先、work 级优先破平);未命中 **404**。

- 响应 `data`:`work`(id/medium/display_name/olang/content_rating/status/**site 认领态**)+ `titles`(official/alias/abbreviation/search_hint;**被编辑面压制的标题行不出任何读面**——`edit_suppressed_row` 负知识按内容派生键排除,行本身仍留在表里由 importer 所有,标题搜索与搜索索引同规排除)+ `releases`(每个含 kind/模糊日期/**主平台码 `platform` + 全平台数组 `platforms`(step 96,空则省略)**/各自 `anchors`)+ **`labels`(经 work_label 归属边,含 label 自身 kind + 归属 kind)**+ **`refs`**+ **`characters`(花名册投影)**+ **`intro`(多语言简介,媒体聚合读面)**+ **`covers`(封面集,含竖版,媒体聚合读面)**+ **`screenshots`(截图集,媒体聚合读面)**+ **`ratings`(评分集,源原生刻度,媒体聚合读面)**+ **`tags`(内容标签集,原样 folksonomy,媒体聚合读面)**+ **`popularity`(热度计数集,源原样计数,媒体聚合读面)**+ **`playtimes`(时长估计集,分钟归一,媒体聚合读面)**+ **`series`(系列归属,step 94)**+ **`platforms`(作品级平台集,step 96)**。
- **`refs` 块(消费面)**:把本作品**全部 exact 锚**(work 级 + release 级)拍平成一张表,每条 `{ source, external_id, level(work|release), release_id? }`(`release_id` 仅 release 级)。用途:渲染 DLsite/EG 外链、展示跨源身份链。**只出 exact 档**——`probable` 是审核泳道内部态、`related` 是非身份链接,均不入 `refs`。**`relations` 块(wave 104)**:work↔work 关系边(sequel_of/fandisc_of/remake_of/collects/shares_character…)按本作品视角单行双向渲染(对向短语按 is_symmetric 解析),对端身份字段随行(id/display_name/medium/content_rating/site);S2S 详情**恒带**(r18 端原样,内部面不设门),公开面 include=relations 且 **r18 端由 `nsfw` 参数门控**(wave 104 起由调用方传参决定;wave 213 波 3 曾在此加一道 NSFW 能力位闸,该能力位已于 2026-08-25 退役,`nsfw=true` 对任何持 key 的调用方直接生效。**不传 `nsfw` 的请求逐字节不变**,这条契约从始至终没动过)。
- **`characters` 块(花名册,step 46;spoiler step 47)**:`work_character` 出演边 **∪** VA credit(`catalog_credit.character_id`)的**并集**——每条 `{ character_id, display_name, latin, gender, kind, spoiler, image_hash, figure_hash, va[{credit_name_id, name}] }`。合并语义:有出演边者 `kind`/`spoiler` 从边取;**credit-only 角色**(仅有 VA credit、无出演边,如 VNDB 路 credits)也出,`kind=0`/`spoiler=0`;`va` 列出为该角色配音的**全部**名义(同名义多 credit 去重)。**`spoiler`**(0=none/1=minor/2=major)= 该角色在此作出场的剧透档;排序:主角优先(kind 有序 main→secondary→appears→unknown)后按 display_name;`va` 内按 name。**`kind` / `spoiler` 是共识值 + 人工覆盖(wave 10/R2c-2)**:`uq_catalog_work_character` 不含来源列,一对 (work, character) 全库只有一行,三个 importer 只 `ON CONFLICT DO NOTHING`(先写者定调),其后每次合并的 survivorship 又把另一侧并进来(`kind=0` 时提升、`spoiler` 取大)。**因此旧契约那句「VNDB `chars_vns.spoil`;Bangumi/EG 边恒 0」是假的**——生产 7,617 条 EG 行带非零 kind、29 条 bangumi 行带非零 spoiler,而这两个 importer 结构上写不出这些值;`matched_by` 记的是谁 INSERT 了这一行,不是谁写了它现在的值。两列**现已是可编辑内容字段**(编辑字段键 `catalog.work.roster`,元素 `{character_id, kind, spoiler}`,**只改既有出演边、不可新建也不可删除**):人工写入在该行上盖 `field_provenance` 戳,此后合并的 survivorship **不再顶掉它**(把 spoiler 调低到 0、把 kind 改回 unknown 都会活下来)。**被压制的出演边不出任何读面**(伴生字段 `catalog.work.roster.suppressed`,负知识按行身份键 `roster:<character_id>` 排除,行本身仍留在表里由 importer 所有):被压掉的角色若还有 VA credit,仍以 credit-only 形态留在并集里(`kind=0`/`spoiler=0`/**不带 `identity`**);没有 credit 就整个消失。**`identity`**(`omitempty`)= 该出演边的行身份键,回显即可、**不要自己拼**;它的**在与不在本身是一条读面语义**——带 `identity` ⇔ 该角色因出演边出现、可被压制;不带 ⇔ credit-only 角色,要动它去 `GET /catalog/works/{id}/credits`。这同时把过去「`kind=0` 到底是 credit-only 还是有人设成 unknown」这个不可分的问题分开了。**链接可见性铁则**:`va` 只暴露 credit_name 名义本身(名义永远可见),**不做任何 person 展开**(person 聚合才受政策辖制)。**压制的连带代价(wave 08/R2c-1)**:并集的 credit 半边同样排除被压制的 credit 行,于是压掉一条 VA credit 会让该角色的 `va[]` 少一条;若该角色是 **credit-only**(只有 VA credit、无出演边),**该角色整个从花名册消失**——这是「读路径统一排除被压制行」的正确后果,不是并集的例外。花名册面**不提供压制入口**(`va[]` 是 `SELECT DISTINCT`,一行对应 1..N 条 credit,故不下发 `identity`);要压一条 VA 请去 `GET /catalog/works/{id}/credits`。空花名册序列化 `[]`;角色 **`image_hash`=胸像**(step 48 VNDB 波 + 167 §10 Getchu 波),**`figure_hash`=全身立绘**(167 §11 Getchu 波)——**两者是不同资产、互不回落**:`image_hash` 按 256×360 `cover` 渲染,`figure_hash` 必须按自身比例渲染(preset `character_figure`,`inside`),裁进竖框即毁。两列均可缺省。
- **`intro` 块(多语言简介,媒体聚合读面 step 52;治理契约 refs/proj/51,**读面于 W1-pre 本体化** refs/proj/140)**:统一形状 `[{ lang, intro, source_id, machine? }]`,一语言一元素。**一条原生泳道,认领与无体作品同规**:恒读 `catalog_work_intro`,一语言多源时**先按 `provenance` 升序(源行 0 优先于机翻行 1)再按 `source_id` 升序**取一(user > vndb > …),元素按 lang 排序。**桥接已退役**:认领作品的简介曾在读时从 galgame body 的四语言列 pivot 出来(bridge-not-copy),W1-pre 的镜面步 `wikirescue` **步 q** 把其中 **ja/en 两语言逐字物化**进 `catalog_work_intro`(`source_id`=galgame_wiki(12)=复刻桥归因、`provenance=0`;空白判定按 trim、存的是**原始列值**),读时桥随即删除——线上字节不动(全认领人口 SQL 对拍 0 差异)。**`zh_cn`/`zh_tw` 不物化**(2026-07-29 用户拍板①:wiki 的 zh 正文本身是无上游的译文,终档 dump 封存,后续 intromt 波以 ja 原文直翻回填)——认领作品的中文简介因此**在翻转时消失,这是有意变更**(实测 21,303 行 / 19,700 部作品),非缺陷。镜面步每日跟随 wiki 侧编辑直至 W1 表族退役,其后 catalog 编辑面持有该列。`source_id` 引用 `catalog_source` 注册表(归因,§8.C)。批量:一批 `catalog_work_intro`,非逐 work。空序列化 `[]`。
  - **`machine` 标记(机翻 step 75,refs/proj/75)**:`catalog_work_intro` 的机翻行(`provenance=1`)在读面元素上带 `machine: true`,源/桥接行**省略**该字段——机翻文本**永不冒充源数据**,消费端可渲染「机翻」徽标。机翻行**填缺语言**落地(ja→zh-Hans):仅当该 work **无任何 `zh-Hans/zh-Hant` 源行**(`provenance=0`)时写;它沿用源 ja 行的 `source_id`(归因=译自该源),另记 `src_hash=sha256(源 ja 文本)`(源文变则重译)与 `mt_model`(问责)。新源/人工 zh 落地后,上面的 provenance 升序令读面**自动优先源行**、机翻行退居遮蔽(shadow-never-delete)。**两条人口泳道**(`cmd/intro-mt --population`):`bodyless`(catalog 原生作品,step 75 试点,top-5k 已饱和)与 `claimed`(wiki 面作品——拍板①弃 wiki zh 后,以步 q 物化的 ja 行(source_id=galgame_wiki)直翻回填,机翻行同键归因 galgame_wiki);零 ja 行的作品(正文只在 wiki zh 列者)不在候选内,待假名归位波补 ja 后由同一 fill-missing 幂等自然收编。源料:step 52 VNDB `vn.description`(en)、step 55 DLsite、step 57 Bangumi summary(原样存,不清洗)、步 q wiki ja 物化行。
- **`covers` 块(封面集,含竖版,媒体聚合读面 step 53;治理契约 refs/proj/51)**:统一形状 `[{ id, image_hash, kind, portrait_pinned, sort_order, sexual, violence, source_id, vote_count }]`,一封面一元素,claimed/bodyless 归并后消费者无感来源;按 `(sort_order, image_hash)` 排序。**竖版**由 `portrait_pinned=true` 挑出——kungal/moyu 重构读此值即得竖版封面。**桥接归并**:**claimed 作品**(`site='galgame_wiki'`)封面**读时桥接**自 `galgame_cover`(kind/portrait_pinned/sexual/violence 原样),`source_id` 由 `galgame_cover.source` 文本映射到 `catalog_source`:`''`→**galgame_wiki**(12,wiki 用户上传=首方 body)、`vndb`→vndb(2)、`bangumi`→bangumi(3)、`upscale`→**upscale**(13,本波新增的首方 AI 放大衍生源);**绝不复制**进 catalog 原生行,桥接读**不重传字节**(galgame 封面永住 galgame_wiki image scope)。**bodyless 作品**(`site=''`)读 `catalog_work_cover` 原生行(字节住 catalog image scope)。**严格 XOR**:claimed 只读桥接;galgame 封面空 = `covers` 空 `[]`,**不回落**原生行(即便存在被遮蔽的原生行也不读——shadow-never-delete §8.B)。批量:claimed 一批 `galgame_cover` + bodyless 一批 `catalog_work_cover`,非逐 work。空序列化 `[]`。**字节纪律 §4**:catalog scope 图(bodyless 封面+立绘)由 `catalog-image-refping` 保活,其全集含 `catalog_work_cover` **全部行(含被遮蔽行)**,漏计遮蔽行 = GC 吃活图(66k 同类);galgame scope 图(claimed 桥接封面)由 `galgame-image-refping` 独立保活。**bodyless 封面数据**:EG(bodyless 主锚 5,844)无任何封面字节(纯统计站,零图列),DLsite(6,102 release 锚)`product_json` 虽带封面 URL 但需从外部 CDN 抓 ~6k 图另立字节波——本波只交付 schema+读面+refping 管线,bodyless 封面数据留待「确认更好 bodyless 封面源」后续波(诚实,不硬凑)。 **桥接余额(W1-pre 收官后)**:封面与截图是**最后两条读时桥**——标题/简介/tag+安全轴/评分/热度/展示轴六面已于 refs/proj/140 全部本体化,这两条按 W2 的既定交接机制留给 W1:原生行 W2 已铺好,桥退即翻转。 **`id` 与投票投影(wave 175)**:`id` 即 `catalog_work_cover` 行 id——用户面的投票端点(§4.1)寻址的就是它;`vote_count` 是该封面的**最佳封面票数**。**本读面不带 `voted`,也不再收 `?uid=`(wave 181)**:那是一个被断言的观看者,而票数是公共的、`voted` 是私人的——「我投没投」只在用户面(§4.4)对着已验令牌回答。**两者纯属参考数据**:票**不改排序、不写 `sort_order`/`portrait_pinned`**,编辑的钉子恒压过票数,是否拿票数做文章由各消费面自定;NSFW 仍由既有 `sexual`/`violence` 在读面裁剪,与票无关。票数按整页封面 id **一次 GROUP BY** 聚合(非逐封面)。
- **`screenshots` 块(截图集,媒体聚合读面 step 54 + 125;治理契约 refs/proj/51 + refs/proj/125)**:统一形状 `[{ image_hash, caption, sort_order, sexual, violence, source_id }]`,一截图一元素;**排序(wave 188 起)= `(source_id, sort_order, image_hash)`**,即**同源行连成一块、块与块不混排**——`sort_order` 只在**单源内**有意义(每个回填器都从 0 自编号),跨源按它排等于把两条互不相干的序列交织起来、伪造一个没人写过的顺序。消费者**按 `source` 分组**渲染画廊(每个 `source` 一段),这也正是这个序给出的形状。与 `covers` 同构,差异:截图带 `caption`、无 `kind`/`portrait_pinned`。**双泳道归并((facet, source) XOR,125 修正,取代旧的整 facet XOR;与 tags/popularity 同规)**:对 screenshot facet,**每 (work, source) 只有一条泳道**——**wiki 桥接泳道**(`galgame_wiki`=用户上传、`vndb`=sync)恒**读时桥接**、绝不物化;**catalog 原生泳道**(`dlsite`)恒读 `catalog_work_screenshot` 原生行;两泳道各带 `source_id` 归因,claimed 读面 = 桥接 ∪ 原生。**wiki 桥接泳道(仅 claimed)**:**claimed 作品**(`site='galgame_wiki'`)截图读时桥接自 `galgame_screenshot`(caption/sexual/violence/sort_order 原样),`source_id` 由 `galgame_screenshot.source` 文本映射到 `catalog_source`:`''`→**galgame_wiki**(12,wiki 用户上传=首方 body)、`vndb`→vndb(2)(截图源域仅此两值;未知值兜底 galgame_wiki);**绝不复制**进 catalog 原生行,桥接读**不重传字节**(galgame 截图永住 galgame_wiki image scope)。**catalog 原生泳道(所有作品,claimed 与 bodyless 一视同仁)**:读 `catalog_work_screenshot` 原生行(UNIQUE (work_id, image_hash);字节住 catalog image scope;55 回填 bodyless + **125 回填 claimed**:DLsite exact **release** 锚 × 本地镜像 `image_samples[]`)。**125 放开 claimed**——dlsite 是 catalog 原生源,claimed 的 DLsite 商店样图不再因认领态隐没。**写侧靶向(wave 188 用户 2026-08-07 拍板,推翻 125 的「兜底不补充」)**:**逐源补缺(per-source fill-missing)**——对每条源泳道,一个作品是候选**当且仅当**它有该源的 staged 图、且 `catalog_work_screenshot` 上**没有该源的行**(`NOT EXISTS ... AND source_id = <该源>`);claimed 与 bodyless **一视同仁**,`source=dlsite` 的限制随之作废(`getchu` 泳道同规,`internal/jobs/getchumedia`)。**理由**:vndb 行是**游戏截图**、dlsite/getchu 行是**官方样例 CG**——语义不同的两类图,且读面按源分块展示,故第二条泳道是**补充**而非对策展画廊的稀释。旧规「只写桥接空 + 原生空的作品/样图是『什么都没有』的兜底,不是真截图的补充」**整条作废**。跨源**同字节不重复入库**:`(work_id, image_hash)` 唯一键在写时挡下,先写者保留其 `source_id` 归因。intro/cover facet 不随之放开——intro 仍是**跨源整语言**补缺(一个读面不出现两条同语言简介),cover 对 claimed **照旧整 facet XOR 拒写**(读时桥接)。旧「claimed 只读桥接、不回落原生」**整条作废**:claimed 的原生行是读面一等公民。**两泳道按 `(work_id, image_hash)` 去重,桥接行领先并获胜**(128 裁定,取代 125 的「不去重」):**绝不按 source 过滤**——**wiki 退役抢救**(`internal/jobs/wikirescue`)把 `galgame_screenshot` 物化进 `catalog_work_screenshot`(`source_id=galgame_wiki`)以熬过 galgame 表族折叠,把 wiki 源从原生泳道滤掉等于抹掉抢救成果;去重消掉的是**同一张图两现**——桥接与抢救行并存的窗口内同一 image_hash 会两泳道各有一行,而那是**一张截图**,桥接行(现役 body)获胜,桥接退役后原生行即全部真相、原样浮出。另两个写手不产生重复:回填器(dlsite/getchu)wave 188 后按**逐源补缺**取候选,但写侧仍受 `(work_id, image_hash)` 唯一键约束,与既有行**同 hash 即不入库**;bodyless 作品带着 step-54 原生截图**被后来的认领就地收编**(§8.B shadow-never-delete)时,那些是**不同的图(不同 hash)**,与 wiki 体截图同列正是 125 所求。批量:claimed 一批 `galgame_screenshot` + 全部作品一批 `catalog_work_screenshot`,非逐 work。空序列化 `[]`。**字节纪律 §4**:catalog scope 图(**全部** `catalog_work_screenshot` 行 = bodyless + claimed 原生截图,加封面+立绘)由 `catalog-image-refping` 保活,其全集**不设认领态过滤**,含**被遮蔽行**,漏计 = GC 吃活图(66k 同类);galgame scope 图(claimed **桥接**截图)由 `galgame-image-refping` 独立保活——两把 refping 按**字节所在 scope** 分工,不按认领态分工。**W2 图字节退役后**(数据层退役轨,`internal/jobs/wikirescue` 步骤 m/n/o):wiki 的封面/截图行整表投影进 catalog 原生表,且这些哈希在 `image_site_usage` 上**增记一条 `site='catalog'` 归属行**(只增不改、绝不动 `galgame_wiki` 行),于是 `catalog-image-refping` 自然接管保活;字节本身一张图全站一行、归属才分站,故「加一个 owner」不搬字节、不重传、不碰 galgame_wiki image key。 **桥接余额(W1-pre 收官后)**:封面与截图是**最后两条读时桥**——标题/简介/tag+安全轴/评分/热度/展示轴六面已于 refs/proj/140 全部本体化,这两条按 W2 的既定交接机制留给 W1:原生行 W2 已铺好,桥退即翻转。
  - **`sexual` / `violence`(封面与截图共用的安全轴)**:`sexual` 是**逐图有效分级** 0/1/2(0=干净 / 1=内衣·泳装 / 2=裸露或性行为),**不是作品分级**——同一部作品的两张图取不同值是正常的。**三条来源**:**vndb 行** = VNDB 社区**逐图**投票(原样桥接/物化);**curated / user 行** = 运营与用户手填;**其余每一个源**(bangumi/dlsite/getchu/upscale/steam…**以及将来新增的任何源**)= 机器摄入行,值由图床的视觉分级阶梯映射而来——`images.review_labels->'grade'.level`(0=干净 / 1=内衣 / 2=裸露 / 3=性行为,闭合是非题阶梯,覆盖全库 99.9997%)按 **`0→0, 1→1, 2→2, 3→2`** 落到本轴(catalog 轴不区分「裸露」与「性行为」,两者同为最强档)。**作品级临时值**:各 importer 插入新行时按**作品级**分级盖一个保守临时戳(DLsite age / Getchu content_rating / bangumi 恒 0),写路径一律 OnConflict DoNothing,故该戳**只对全新行生效、永不覆盖已有行**;`cmd/sync-image-grades` 每晚(cron `image-grade-nightly`:先增量分级新图,再传播)把**机器源行**刷成逐图分级,人工三源(vndb/curated/user)**永不改写**。一张新图因此最多保持一个夜晚的作品级临时值。**教训**:把这个临时戳当成逐图终值来读,曾把约 2.35 万张干净图当色情糊在论坛上。`violence` 则**只有 vndb 行有意义**(同为社区逐图投票)——**不存在任何自动暴力分级**,机器源行恒 0,而本语料的真实基率约 0.03%,所以 0 的意思是「本源没有这个轴」,不是「已判定为无暴力」。**封面读面的裁剪线 = `sexual >= 2`**(`model.SexualExplicit`,与 repincovers 钉图 job 的 `nsfwSexualFloor` 同一条线;门 = `请求 nsfw && 作品展示轴`,展示轴按 `model.DisplayLimitKey` 分类——认领读 `display_nsfw`、未认领回落年龄轴):sexual=1(内衣·泳装级)对所有读者可见。曾经读面藏 `>=1` 而钉图 job 钉 `=1`、且未认领行被裸读恒 false 的 `display_nsfw` 挡死,两处不一致合起来让热门列表约四分之一的作品 `cover: null`(2026-08 论坛报障)。
  - **实体图的 `*_meta`(公开面加法)**:实体面的图此前只发一个 hash / URL,尺寸、thumbhash 与分级一概没有——消费端渲染角色胸像/立绘、人物照片、厂牌 logo 时既留不出宽高比也读不到分级。现每个实体图槽旁并置一个可选对象 `{width?, height?, thumbhash?, sexual?, violence?}`:`characters/{id}` 与作品花名册行的 `image_meta` / `figure_meta`、`names/{id}` 的 `photo_meta`、`labels/{id}` 与 `labels` 列表行与 `labels/{id}/relation-graph` 节点的 `logo_meta`。**既有字符串键一字未动**(`image` / `figure` / `photo_hash` / `logo_hash` 照常),对象纯属加法。值取自图床 `POST /image/meta-batch`(每个响应一次批量查询,与封面/截图共用同一次);**查不到即整个对象缺席**,读面照常作答——元信息是增益,不是可用性依赖。
    - **`sexual`** = 该图的机器分级,与上面作品媒体轴**同一把尺**(`images.review_labels->'grade'->>'level'` 按 `0→0, 1→1, 2→2, 3→2` 折算)。**缺席 ≠ 0**:缺席 = 尚未评级(刚上传、夜间 grader 还没跑到),`0` = 已评级且判为安全。
    - **`violence`** = 该图的暴力档,**本波恒缺席**:逐图暴力数据只有 VNDB 社区投票一处,而那批图走的是作品媒体面(`covers` / `screenshots` 的 `violence` 列 = 有记录的社区/人工判定,`0` = 该源没有这个轴、不是「已判定为无暴力」);自动暴力分级实测不可用,实体图上因此没有任何可断言的值。字段先立,语义钉死为「**缺席 = 无已知判定**」。
- **`ratings` 块(评分集,媒体聚合读面 step 58a;治理契约 refs/proj/58 Facet A,**读面于 W1-pre 本体化** refs/proj/140)**:统一形状 `[{ source_id, score, vote_count, rank? }]`,**一源至多一元素**,按 `source_id` 升序(vndb=2 → bangumi=3 → dlsite=4 → erogamescape=5 → nextmoe=19 → howlongtobeat=20)。**score 恒为源原生刻度,绝不归一**(58 拍板):`vndb` = 1-10 **均值**、`bangumi` = 0-10 **均值**、`dlsite` = 0-5 **星均值**(两位小数)、`erogamescape` = 0-100 **中央值**、`nextmoe` = 1-10 **均值**(两位小数,论坛 galgame_rating 每夜聚合,≥3 人才发布,跌破即删行)、`howlongtobeat` = 0-100 **评分整数**(站方 review_score)——语义不同,消费端按 `source_id` 分源渲染。`rank` = 源内排名,源无此概念(VNDB meta/DLsite/EG)或该作未入榜(Bangumi rank 0)时缺省。**一条原生泳道,认领与无体作品同规**:恒读 `catalog_work_rating`(UNIQUE (work_id, source_id))。**四张 meta 表的读时桥已退役**:认领作品的评分曾在读时从 `galgame_{vndb,bangumi,dlsite,eg}_meta` 桥出,W1-pre 把四条泳道逐字物化——**vndb 归镜面步 q/s 家族的步 s**(`score = rating/10`:meta 存的是 kana 线上刻度 10-100,÷10 得 VNDB 自身展示的 1-10 刻度,**同源解码非跨源归一**;以 float64 计算后按最短往返存,读回即桥的同一个 double),**bangumi/dlsite/eg 归一次性收编步 t**,此后由 `workratings` importer 持久保鲜(本波拆掉了它的 claim 闸,认领与无体作品在其眼中平权);**vndb 泳道自 wave 206 起也归同一个 importer**——此前只做过步 s 那一次物化,`src_vndb` 每周重新入库后**没有任何东西回刷 vndb 分数**,20,892 行自 2026-07-31 起冻结;新泳道 `score` = `src_vndb.vn.c_rating / 100`,即 **VNDB 站上那个头条数字**(贝叶斯平滑后;与冻结行逐行对拍,20,890 行中 17,400 行完全相同,其余为期间漂移,平均偏差 0.005),未平滑的算术均值 `c_average` **不进 `score`**,而是作为 `stats.average` 并置(见下);`vote_count` = `c_votecount`;**步 t 不进每日链、W1 终扫也不跑**(importer 的值更新鲜,回写 meta 旧值=倒退)。**「无分」恒为缺席、绝不假 0**——物化时逐字复刻桥的过滤:vndb `rating IS NULL`(wave 206 起同时排除 `c_votecount = 0`——一票没有的「评分」不是分)、bangumi `score<=0`(未评分)、dlsite `rate_average_star IS NULL` 或 `rate_count<=0`(不足 ~5 票不公开,星数与票数恒同现同缺)、EG `median IS NULL`(EG 的 0 是真实低分,NULL 才是无分)——一条都不出行。全认领人口 SQL 对拍 0 差异。批量:一批 `catalog_work_rating`,非逐 work。空序列化 `[]`。纯 DB 零字节——无 refping 事。
  - **评分详情(wave 205 建面,wave 206 补齐四源;详情面独有)**:`ratings` 的元素在 **work 详情响应**里可再带两个可选字段,供消费端点开单源做可视化;**列表面的 `?include=ratings` 块永不携带**(一页 50 条只为一个不一定打开的弹窗付费)。① **`distribution`** = 投票直方图 `[{ score, count }]`,`score` 为**该源自己的刻度**(与 `score` 同规,绝不归一)、**按 score 升序**、**稀疏**(某档无票即无元素)。**排序由服务端负责**:库里按 `{"<档>": 票数}` jsonb 存,而 `"10"` 作为文本排在 `"9"` 之前,消费端自己遍历对象键会把 1-10 刻度的最高档摆到中间。② **`stats`** = `{ average?, stdev?, min?, max? }`,与 `score` 同一投票总体的离散度。两个字段都**缺席即无**,`0` 是真值(EG 的最低分就是 0)。

    **逐源能力矩阵(wave 206 起四源全有 `distribution`;2026-08-28 起 howlongtobeat 为第五源)**:

    | source | `distribution` 档位键域 | 直方图口径 | `stats` |
    |---|---|---|---|
    | `bangumi`(3) | `"1"`…`"10"` | dump 的 `score_details`,**与 `vote_count` 同源**——后者本就是它逐桶加出来的 | 无 |
    | `dlsite`(4) | `"1"`…`"5"` | `rate_count_detail`,**与 `vote_count` 同源** | 无 |
    | `erogamescape`(5) | `"0"`,`"10"`,`"20"`,…,`"90"`,`"100"`(十分位**下取整**:0-9→`"0"`、90-99→`"90"`、恰好 100→`"100"`;**`"0"` 是真档**,EG 的最低分就是 0) | 镜像 `reviews` 表**快照**(`tokuten IS NULL` 的纯留言行不计) | `average`/`stdev`/`min`/`max`,取自 `games` 同一行的 `average2`/`stdev`/`min2`/`max2` |
    | `vndb`(2) | `"1"`…`"10"` | VNDB **另发的每日投票 dump**(`vndb-votes-latest.gz`),站内 10-100 刻度每十分并一档 | `average`,即 `c_average`(未平滑算术均值,与 `score` 所取的贝叶斯 `c_rating` 并置) |
    | `howlongtobeat`(20) | `"5"`,`"10"`,…,`"100"`(五分位档,稀疏) | 游戏页 `userReviews` 直方图,**与 `vote_count`(review_count)同源** | 无 |
    | `nextmoe`(19) | `"1"`…`"10"` | 论坛 overall 逐档计数,与 `vote_count` 恒等 | `average`(同 score)+`stdev` |

    **两条口径警告(契约事实,不是待修的 bug)**:**EG** 的柱子来自 `reviews` 镜像快照,而 `score`/`vote_count`/`stats` 来自 `games` 行——**两张表各走各的同步游标**,故各柱之和**可能不等于** `vote_count`(两个方向都可能);**VNDB** 的柱子来自公开投票 dump,该 dump **排除私密列表用户的票**,故各柱之和**恒 ≤** `vote_count`(实测 v97:22,415 / 23,678 ≈ 94.7%)。**消费端一律把「各柱之和」当作这张直方图自己的分母**——不要用 `vote_count` 去归一化柱高,也不要把两者的差额当成缺失数据补一根柱子上去:那是两个总体,不是同一总体的两次测量。VNDB 侧还有一条:某作品**有 `score` 但没有 `distribution`** 是正常态(投票 dump 只覆盖有公开票的 vn),缺席即无。
- **`tags` 块(内容标签集,媒体聚合读面 step 58b + T2;治理契约 refs/proj/58 Facet B + refs/proj/70 §3/§8,**读面于 W1-pre 本体化** refs/proj/140)**:统一形状 `[{ name, count?, source_id, spoiler, sexual, canonical_id?, tier?, kind? }]`,一 tag 名一源一元素,按 **`(count DESC, name ASC, source_id ASC)`** 排序(高票在前;`name` 为**字节序**——该序原由 Go 排序产生,下沉进 SQL 时显式 `COLLATE "C"` 才不被库 collation 悄悄改掉;`source_id` 破 (count, name) 平)。**name 恒为源原文,原生层不做词表映射/规范化**(58 拍板;仅 trim 首尾空白)——规范层(step 74)在其上加**加法覆盖**(见下),原生 name/count/source_id 永不改写。`count` = 该 tag 在源上的票数,源无票数概念时**缺省**(整个 wiki tag 层无票数;bangumi moderated meta_tags 亦 count=0)。**内容标签与 `labels` 严格分离**:`labels` 是归属词汇(组织责任,brand/publisher/circle/group),**tags 绝不入 `catalog_label`**(词汇红线)。**一条原生泳道,认领与无体作品同规**:恒读 `catalog_work_tag`(UNIQUE (work_id, name, source_id))。**wiki 桥接泳道已退役**:认领作品的 tag 曾在读时从 `galgame_tag_relation ⋈ galgame_tag` 桥出,W1-pre 的镜面步 **步 r** 把 92 万条边逐字物化——`name`=`galgame_tag.name`(**wiki 策展的展示名**,VNDB sync 经 tagMap 英→中本地化,未映射者保留英文原名,用户自建者原样:这批 zh 策展名只存在于此,是这条泳道值得物化而非从 VNDB 重推的原因)、`source_id` 按边自身 `source` 映射(`''`→**galgame_wiki**(12,用户自建)、`vndb`→vndb(2,sync;未知值兜底 galgame_wiki)、`count=0`;镜面步每日跟随 wiki 编辑直至 W1。bgm/dlsite 原生行是 **step 88 / T1 两波 importer 的财产**,镜面步的属权范围把它们整体排除——**一行一个持久写手**是本波红线。**安全轴(A2-1e/R8,W1-pre 落成真列)**:`catalog_work_tag.spoiler`(**逐边**:0=无 / 1=轻 / 2=重,物化时按 `LEAST(GREATEST(spoiler_level,0),2)` clamp——wiki 列是无 CHECK 的 bigint,公开契约只承诺三值词表)与 `catalog_work_tag.sexual`(**逐 tag**:`galgame_tag.category='sexual'`);两列 **NOT NULL 无 default**,folksonomy 行(bangumi/dlsite)由其 importer 显式写 **0/false**——那是「**此源不发布安全轴**」的诚实取值,不是数据库默认值(覆盖不对称是真实的:安全轴只存在于 VNDB 派生词表)。**spoiler 天花板下沉进原生查询**:调用方给上限(`spoiler <= ?`),**S2S 面恒 0**(= 前 A2-1e 时代逐字节同的行为)、公开面由 `?spoilers=0|1|2` 控,只有显式要才看得见剧透边。批量:一批 `catalog_work_tag`,非逐 work。空序列化 `[]`。纯 DB 零字节——无 refping 事。
  - **规范层加法覆盖(step 74,加法零破坏;治理契约 refs/proj/70+74)**:每元素可选带 `canonical_id?`(`catalog_tag.id`)/ `tier?`(`0`=core 核心 / `1`=longtail 长尾 / `2`=hidden 隐藏)/ `kind?`(`0`=content 内容 / `1`=meta 平台属性)。命中规则:按 `(source_id, name)` 批量查 `catalog_tag_source_map`——**桥接泳道与原生泳道皆覆盖**,claimed 的 vndb 桥接 tag(`source_id=vndb`、`name=galgame_tag.name`)与 bodyless 的 vndb tag 命中同一 map 键;命中则带三字段,**未映射则三字段全省**(原样照旧展示)。`source_name` 键:vndb=`galgame_tag.name`、bangumi/dlsite=`catalog_work_tag.name`。**消费端展示策略**(非删数据):`tier` 分层折叠(核心默展 / 长尾折叠 / 隐藏不展)、`kind=meta` 移出内容标签云进属性过滤 facet。本波(70a 确定性切片)只建**跨源(≥2 源)NFKC 精确同名组**(一律 `tier=core`,手钉 meta 名单 `kind=meta`);单源 canonical 名 + LLM 跨源配对 + tier 终标留待 70b,故单源 tag 本波恒未映射。
- **`popularity` 块(热度计数集,媒体聚合读面 step 62;治理契约 refs/proj/62,**读面于 W1-pre 本体化** refs/proj/140)**:统一形状 `[{ source_id, metric, value }]`,**一 (source, metric) 至多一元素**,按 `(source_id, metric)` 升序。**`metric` 词表(可扩,加常量即扩)**:`0`=downloads(购买/下载数,DLsite `dl_count`)、`1`=wishlist(收藏数,`wishlist_count`)、`2`=reviews(书面评论数,`review_count`);`10`=bgm_wish、`11`=bgm_collect(dump 键 "done")、`12`=bgm_doing、`13`=bgm_on_hold、`14`=bgm_dropped(Bangumi favorite 五桶);`20`=favorites(**本站收藏数**,source=`nextmoe`);未来源追加新常量,**绝不改号**。**`value` 恒为源原样计数,绝不跨源相加**(DLsite 一笔销售与 Bangumi 一次收藏是不同单位)——消费端按 `(source_id, metric)` 分源渲染,与评分刻度规则同构。**空值语义**:源未公开的计数**无元素**(DLsite 商业作不公开 `dl_count`——缺席 ≠ 0),源公开的 0 是真实值、**有元素**(刷新回路保持其现势)。**`nextmoe`/`favorites` 是这条语义的唯一例外**:它恒有元素,0 就是「无人收藏」而非「未知」,因为这个数我们永远算得出来——见下。**一条原生泳道,认领与无体作品同规**:恒读 `catalog_work_popularity`(UNIQUE (work_id, source_id, metric))。**`nextmoe`/`favorites` 不进这张表**:它由读路径现算——`SELECT count(DISTINCT owner_uid) FROM catalog_user_folder_item WHERE work_id IN (…) GROUP BY work_id`——因为 `catalog_user_folder_item` 本身就是收藏的正典存储,再上卷一份只会多一个会分叉的答案(对比 playtime:中位数不是计数,所以它需要 `jobs/userplaytime` 上卷)。**私密收藏夹计入**:这是对人去重的聚合,不指名任何人,排除私密夹会把回填进来的绝大多数收藏抹掉,而被它取代的论坛/moyu 两张源表本来也没有逐条收藏的可见性。**dlsite 桥接泳道已退役**:认领作品的 dlsite 三计数曾在读时从 `galgame_dlsite_meta` 的三列 pivot 出来(NULL 列无行、公开的 0 出行),W1-pre 的一次性收编 **步 t** 按同一语义物化,此后由 `workratings` importer 持久保鲜(claim 闸已拆);bgm 五桶自 T2b/102 起本就认领无体同走原生。**易变值刷新**:写路径均为值变才更的 `ON CONFLICT DO UPDATE`——镜像刷新后重跑即刷新。批量:一批 `catalog_work_popularity`,非逐 work。空序列化 `[]`。纯 DB 零字节——无 refping 事。
- **`playtimes` 块(时长估计集,媒体聚合读面 step 91;治理契约 refs/proj/91)**:统一形状 `[{ source_id, minutes, vote_count }]`,**一源至多一元素**,按 `source_id` 升序。**`minutes` 为分钟归一**(摄取侧单位换算:EG 小时×60、vndb `c_length` 原生即分钟、HLTB 秒÷60)——**估计语义仍源原生**(vndb=带票中位、erogamescape=社区中位、howlongtobeat=**Main Story** 中位 `comp_main_med`,缺则均值 `comp_main`),消费端按 `source_id` 分源渲染,不跨源平均。`vote_count`=支撑估计的用户报告数(vndb `c_lengthnum`、HLTB `comp_main_count`;EG 不公布按作品票数→0,真实零)。**本 facet 无 claimed 桥接泳道**(wiki galgame 家族无时长字段)——**所有作品(claimed 与 bodyless 一视同仁)读 `catalog_work_playtime` 原生行**,是 (facet,source) XOR 的退化情形(桥接集恒空,仅原生泳道)。写路径=`cmd/backfill-work-playtime`(EG exact work 锚 × EG 镜像 / vndb exact work 锚 × `src_vndb.vn` / hltb work 锚(exact 或 probable,`rule:hltb-steam`) × HLTB 镜像),值变才更 `ON CONFLICT DO UPDATE`(镜像/dump 刷新→重跑即刷新);理智上限 1,000 小时,超限拒写(镜像存在 10,000h 级脏值)。批量:全部作品一批 `catalog_work_playtime`,非逐 work。空序列化 `[]`。纯 DB 零字节——无 refping 事。
- **`series` 块(系列归属,step 94;治理契约 refs/proj/94 选项 B)**:统一形状 `[{ id, name, source_id, member_count }]`,按 series id 升序。**系列是一等实体**(`catalog_series` + `catalog_series_member`,非两两边——O(n) 成员、系列名可寻址):dlsite 泳道首发(`series_id`/`series_name` 原样,UNIQUE (source_id, external_id)),未来 vndb/bgm 系列泳道同表加源。**物化门 = ≥2 个锚定 galgame 作品**(单员系列无读面价值,留镜像);`member_count`=系列内锚定作品总数。**catalog 原生 facet,无 claimed 桥接**(wiki 家族的 galgame_series 词表在其自有读面,永不桥入)。写路径=`cmd/import-work-series`(镜像即真相:系列改名就地更/成员 insert-if-absent+stale delete/跌破门槛整系列删)。空序列化 `[]`。纯 DB 零字节——无 refping 事。
  - **curated 泳道(wave 180)**:同表另有 `source_id=curated`(12)的**人工系列**,`external_id` = `wiki:<原 wiki series id>`——退役的 wiki `galgame_series` 分组经一次性种子 `cmd/seed-curated-series` 复原(冻结产物 `refs/proj/180-artifacts`;成员集被单一 dlsite 系列全覆盖者跳过,不建重复分组)。种完之后**长期唯一写者是人工编辑面**(`series_ids` 字段整替换成员、curated 系列可改名),故种子 insert-only、二遍零写,**永不重跑**;dlsite 系列永不挂人工成员(其导入器 reap 外来成员)。
  - **derived 泳道(wave 184)**:`source_id=derived`(18,trust_tier 1)= **机器推断泳道**——上游没发布、人也没写,由 catalog 自有事实算出来的分组。写者只有 `cmd/build-derived-series`:在 `catalog_work_relation` 的**系列性边子集** `{2 sequel_of, 3 side_story_of, 4 fandisc_of, 7 same_series}` 上取连通分量,每个分量物化一个系列,`external_id` = `comp:<分量内最小 live work id>`。三条**不重开的边界**:① 只为与 dlsite/curated 现有系列成员**零交集**的分量建系列,有交集的落 worklist 留人工/curated 富化——机器永不改写人的分组;② 巨型分量(≥30)先用强边 `{2,7}` 重聚,仍 ≥30 的不建、落 worklist;③ reaper 语义可重跑(成员 insert-absent/delete-stale、跌破 2 员整删、`display_name` 每轮由 builder 重算——derived 无人工改名)。**人工恒胜**:编辑面的 curatedOnly 守卫天然拒绝人碰 derived 系列,想要别的分组就在 12 号泳道自己策展。
  - **成员顺序与角色(wave 184,三泳道通用)**:`catalog_series_member` 增两列。`position` smallint NOT NULL DEFAULT 0 = **1 起的阅读序**(按成员最早 release 日期升序、无日期者垫底、work_id 破平),**0 是「尚未定序」哨兵**(184 前的存量值),读面把 0 排**最后**而不是最前。`kind` smallint NOT NULL DEFAULT 0 = 成员在这条线里的角色:0 unknown / 1 main / 2 fandisc / 3 side_story / 4 collection,由**分量内**(两端都是本系列成员)的关系边判:`a fandisc_of b` ⇒ a=2,`a side_story_of b` ⇒ a=3,有边证据但非上述 ⇒ 1;**无边证据时 derived 泳道给 1、dlsite/curated 泳道给 0 不硬猜**。排序算法是三处共用的同一实现(`internal/jobs/seriesorder`),供 dlsite reaper 每轮维护、derived builder 物化时赋值、`cmd/backfill-series-order` 回填存量(dry 默认/`--apply`/二遍零写零 touch)。两列是纯函数产物,故日期订正后重跑即可修序;**任何真变更都 touch 相关 work**(117/118/119 口径),二遍必须零写。公开投影 `GET /v1/catalog/series/{id}?include=works` 加法新增 **`members[]`**,与 `works[]` **平行**(同页同序同 r18 过滤,`members[i]` 描述 `works[i]`),元素 `{ work_id, position, kind }`,`kind` 出**字符串键**(`unknown|main|fandisc|side_story|collection`,公开面永不出数字枚举);`works[]` 本身改按 position 排序。2026-08-26 起 series 详情面加法新增 **`work_count`**(同 nsfw 闸下可见的成员数,与列表面同一口径;v2 series 基本形状同步携带)。work 面的 `series` 块与 `series_siblings` 形状不变。
- **`platforms` 块(作品级平台集,step 96;治理契约 refs/proj/96)**:统一形状 `[{ platform, source_id }]`,按 platform 升序。**码 = `catalog_platform` registry 键(vndb 码表 48 枚:win/and/ios/…)**,registry 提供显示名与弃用审计;列本身存 text 码(同 lang 列约定,不 FK)。**平台事实存在两个粒度**:vndb/dlsite 面在 **release 级**(`releases[].platform` 主码 + `releases[].platforms` 全数组——移植是 release 级事实);bgm 面在 **work 级**(Bangumi 把平台标在 subject 上,其无体作品多数无 release 行,1,875/13,293)——本块只出 work 级显式行,消费端按需并集两粒度。**catalog 原生 facet,无 claimed 桥接**。写路径=`cmd/import-work-platforms`(dlsite 泳道:镜像 `product_json.platform` pc→win/android→and/ios→ios,**smartphone/play 为观看器标志非 OS 移植,跳过**,仅 galgame 媒介存根;bgm 泳道:infobox `平台` 经映射表规范化,未映射值计数上报不猜)。幂等:dlsite 写守 platform 仍空,bgm 写 ON CONFLICT DO NOTHING。空序列化 `[]`。纯 DB 零字节——无 refping 事。
- **`refs` 与 `releases[].anchors` 的分工**(两视图并存,面向不同消费者):`refs` = **消费级摘要**(exact-only,产品站直接渲染,无需理解档位);`releases[].anchors` = **质检全景**(逐 release 的全部锚,**显式携带 `link_kind` 与 `matched_by`**,供内部数据浏览器等按档自筛)。产品站消费一律用 `refs`;`anchors` 中出现非 exact 档位时消费端**必须**按 `link_kind` 自筛,不得当身份使用。
- 用途:letmoe 音声读穿页(镜像其 wiki 读穿),社团归属由此获得。`GET /catalog/works/{id}`(§2.7)返回**同一 bundle**(含 `refs`)。

### 2.5 `GET /catalog/works/{id}/credits` — 作品署名(只读)

按 role 分组的署名列表。每组 = role(id/key/name)+ 条目;条目 = 名义(id + lang 分桶名 + latin)+ 可选 character(id+名,VA 用)+ note + source key + **`identity`**。**孤儿名义原样出**(person 层未建,如实);**排序 = role_id 权重 + 人工车道优先(`source_id=12`)+ 源 key + 名义 id**——人工行此前按 `curated` 的字典序夹在 bangumi 与 dlsite 之间。

- **`identity`(wave 08/R2c-1)**:该 credit 行的**行身份键**,即 `catalog.work.credits.suppressed` 的元素。**客户端只回显、不自拼**——键格式是实现细节,组合规则可能变,而 `.suppressed` 的写面会用同一道 `KeyCheck` 拒掉任何不是本字段自己生成的串(422)。同字段下发于公开面 `GET /v1/catalog/works/{id}?include=credits`、S2S/公开的 `names/{id}` 反查;**凡下发它的面,该字符串恰好回指一行 `catalog_credit`**。
- **压制**:被 `catalog.work.credits.suppressed` 记为负知识的上游行**不出任何读面**(行本身留在表里由 importer 所有,与标题压制同规),分页总数与其列表同谓词。
- **人工车道**:`source_id=12`(`curated`)的行由编辑面增删改,上游行只读+可压制;人工新建的 credit 若与上游行撞 `(role_id, credit_name_id, character_id)`,写面返回 **422 并指向 `.suppressed`**(唯一索引不含 source,不加宽——加宽等于同一条 credit 在读面出现两次)。

### 2.6 `GET /catalog/search/entities?q=&type=&locale=&limit=` — 实体搜索(只读)

- `type` ∈ `names|characters|labels`(单选;非法 → Huma enum 校验 422);
- **`locale` ∈ `zh|ja|en` → 服务端决定引擎侧含义**(OpenSearch 是唯一搜索引擎,自 2026-09-01;Meilisearch 于 2026-09-02 退役。语言处理在索引期按字段分析器完成,不落任何每查询参数;`LocalesForUI` 仍把粗粒度 UI locale 收进查询合同,引擎丢弃它。不变量 2 不变:消费者只传粗粒度 UI locale,**绝不透传任意引擎参数**);
- `limit` cap 20;空 `q` → 按 popularity 返回热门。
- 响应条目:id(前缀 n/c/b)· entity_type · name(分桶取非空)· latin · sources · popularity · kind(label)· person_id(名义,缺省=孤儿)· **`logo_hash`(仅 label,wave 170:厂牌 logo 在图床的内容哈希,与作品封面 `image_hash` 同币种,消费端据此拼 CDN URL;缺省=该 label 无 logo)**。**该哈希不在搜索索引里**——命中页的 label id 回 Postgres 单查一次补水(索引设置与文档一概不动)。

### 2.7 内部浏览器三端点(D-02,同 Basic S2S 读面)

供**内部数据浏览器**(wiki 前端 staff 专用,经 galgame 后端代理)用;仍是 Basic S2S 读面。

- `GET /catalog/stats`:仪表盘全部计数**单端点单往返**——works 矩阵(medium × 认领态 × status)、实体计数(**孤儿名义单列**,person=0 如实)、credits 按 source、归属边 by kind、**refs source × tier 交叉表**(身份质量一张表)、队列水位(candidates/proposals by status、probable refs、rejections)、**src_llm bid 判定**(same/different/unsure/deterministic;src_llm 缺表则该段空)、**新鲜度 = 各 source 锚 max(created_at)**(诚实近似,不加簿记)。⚠️ **本端点是内部遥测,不上公开面**:队列水位/LLM 判定/锚交叉表/新鲜度/孤儿与 claim 态矩阵描述的是「注册表如何被治理」。产品面要的「目录有多大」由 **公开面 `GET /v1/catalog/stats`(149b)** 单独回答——**另一套 DTO、另一组 SQL**(LIVE works 按 medium + 身份族存量,r18 计入),见 [developer-platform/02 §3.2](../developer-platform/02-public-api.md)。
- `GET /catalog/works/{id}`:与 2.4 by-anchor 同 bundle,入口换 catalog id;404 同义。
- `GET /catalog/labels/{id}/works`:厂牌反查(经归属边),返回 label 自身信息(`label`:id/名/kind/**`logo_hash`**——wave 170:厂牌 logo 在图床的内容哈希,与作品封面 `image_hash` 同币种,空串=无 logo)+ offset 分页作品列表(cap 50)+ total,页面直达即自足。**被合并掉的 label(软删除 + 留下 catalog_redirect)与不存在的 id 同义 → 404**;旧 id 的去向走 §2.1 resolve / §2.2 redirects。公开面 `GET /v1/catalog/labels/{id}` 在同一情形下更进一步:**301 + `Location` + 信封 `code=12` 且 `data.current_id` 给出幸存者 id**(绝不在旧 id 下 200 出幸存者内容)。
  - **wave 186 加法——公开面 `relations[]`**:`GET /v1/catalog/labels/{id}` 的基础记录(非 include 门控,与 `links`/`intros` 同列)新增 **`relations`**,元素 `{ id, display_name, localized{}, relation }`(wave 209 前该投影用短键 `name`),读作「**`display_name` 是本厂牌的 `<relation>`**」。`relation` 出**字符串键**(公开面永不出数字枚举),词表八值 = §1 的四对互逆码:`parent | subsidiary | imprint | imprint_of | spawned | origin | succeeded_by | formerly`。因图是**镜像存**的,本面只做 `WHERE label_id = ?`,**不反转任何关系**;另一端软删(被合并掉)的行不出,排序 `(relation, name, id)`,空序列化 `[]`。同波顺带修好一处**静默丢数据**:label 的 `links[]` 此前只认 official_site / twitter / cien 三种模板,生产上已有的 steam / pixiv / web 行被整条丢弃——三个读面(work / label / person)现在共用**同一张模板表**,故这三种在 label 上也照常出;`dlsite`/`dmm` 仍**故意缺席**(店铺 URL 分区,注册表只存裸码,任何单一模板都会对一部分人口 404)。
  - **wave 188 加法——公开面 `GET /v1/catalog/labels/{id}/relation-graph`**:上一条的 `relations[]` 只有**一跳**,站在 Key 上看不见母公司旗下的兄弟品牌;本端点一次给出**整个连通企业家族**。鉴权、信封、缓存与 `labels/{id}` 同(同一 `/v1/catalog` 中间件链、`catalog:read`),唯一参数 `nsfw`。
    - 响应 `data = { nodes: [{ id, display_name, localized{}, logo_hash, work_count }], edges: [{ from, to, relation }] }`,两个键**恒出**(无边序列化 `[]`)。节点名即 §1 的名字原语(**08-19 跟进**:209 当波漏掉了本投影,节点曾是短键 `name` 且无 `localized{}`,下游因此出现「详情页猫猫社、关系图ねこねこソフト」的分裂;改名为 breaking,声明在 `public-openapi-breaking-ignore.txt`)。`nodes[0]` 恒为种子;`logo_hash` 投影同 `PublicLabel.logo_hash`(空串=无 logo,不省略);`work_count` 复用 browse lane 的同一 nsfw 感知聚合,故与 `labels/{id}.work_count`、`labels` 列表行**逐字一致**。
    - 遍历 = 自种子沿 `catalog_label_relation` 的**广度优先**walk + visited 集(镜像存储本身即成环,无 visited 不停机),**depth ≤ 4、nodes ≤ 60**;上限**按广度生效**,故被截断时留下的是离种子**最近**的一圈。JOIN `catalog_label` 排除软删(被合并掉的 label 在任何一跳都不出),渲染的边恒在节点集**内部**(不出悬空引用)。**不分页**——切片后的图不是图。
    - 边语义:`{from, to, relation}` 读作「**`to` 是 `from` 的 `relation`**」,与 `relations[].relation` 同一读法(那里 `from` 即被查看的厂牌)。因图**镜像存**,本面每个事实**只出一次**:仅渲染互逆对的**正向**四值 `parent | imprint | spawned | succeeded_by`,四个反向值(`subsidiary | imprint_of | origin | formerly`)由**反读同一条边**得到(要「X 的子公司」= 取 `to` 为 X 且 relation 为 `parent` 的边)。多源断言同一 `(from,to,relation)` 折叠为一条,`source` 不出面。
    - 种子不存在 → 404;种子被合并掉 → 与 `labels/{id}` 同样的 **301 + `current_id`**;种子无边 → **单节点零边图**(不是 404)。`relations[]` 一跳面**保持不动**。
  - **wave 199 加法——imprint 上卷**:186 给了箭头、188 给了整张图,但**所有读面仍只问「归属到本行的作品有哪些」**,于是 **311 个会社页答以空列表,而 10,668 部作品就挂在它下面一跳**(VISUAL ARTS 自有 19、75 个 imprint 下 553;NEXTON 自有 38、下挂 339)。上卷是**显式开关 + 强制归因**两件事:
    - `GET /v1/catalog/labels/{id}` 恒出 **`imprint_work_count`**:沿 `imprint`/`subsidiary` **向下一跳**多摸到的作品数,与 `work_count` 同一 nsfw 感知 + live 认领聚合。**永不并入 `work_count`** —— 并进去就抹掉了 186 建图正是为了留住的那支箭头。两个人口**不相交**(母子共同署名的作品只算在母公司一侧)。
    - `GET /v1/catalog/works?label_id=<id>&label_rollup=1` 是跟着那个数走的那一页,人口恰为 `work_count + imprint_work_count`——**「chip 上的数 = 点进去拿到的数」这条全 taxonomy 道赖以成立的不变式,对第二个数同样成立**。经子厂牌进来的行带 **`via_label{id, display_name, localized{}}`**(wave 209 前是 `{id,name}`),会社自有的行不带。
    - **不跟** `spawned` 与 `succeeded_by`:分家出去的公司目录是它自己的(fairys 之于 sprite),公司承继是**两实体一箭头**(wave 198 复核员已裁)。跟了它们就等于把别家的作品挂在本会社名下,而归因存在的意义正是防这件事。软删(被合并掉)的子厂牌在两处都不出。
  - **wave 200 更正——一家公司只占一格,版次事实下沉到 release**:`labels[]` 的存储粒度是 `(work, label, kind)`,于是「既开发又发行」的公司**占两行**,**56,438 部作品**因此把同一家公司印了两遍。同时 vndb 本就按 release 记录发行方,而 `orglabels` 的 W(O) 无任何语言闸,把移植商 / 本地化商 / 汉化补丁组一并压平到作品级——あまいろショコラータ 因此列出 5 家「会社」,且压得并不一致(英文版发行商进来了、俄文版的没有)。
    - `labels[]` **一家公司一条**,其担任的全部身份进 **`kinds[]`**(排序,恒非空);单数 `kind` 保留,含义收紧为**最具识别力的那一个**(brand → circle → developer → publisher 优先级)。消费端渲染角色请读 `kinds[]`。
    - 作品级归属的定义收窄为「**原语言 · 非补丁** release 的公司」。`orglabels` 内部把**证据集**(用于共现判定,保持宽)与**可归属集**(用于铸边,收窄)拆开;判据是 `src_vndb.releases.olang`,而非旧闸读的 `releases_titles`——后者答的是「有没有该语言的标题」,双语本地化版照样通过。**必须先改规则再谈清理**:删掉的边下一次 mint 会照原规则推回来。
    - 新表 **`catalog_release_label`**(release ↔ label,`kind` 复用 `WorkLabelKind*`):被收窄挤出去的事实落在它真正成立的那一层,而不是被丢弃。`cmd/import-release-labels` 从 vndb 回填,**无语言闸**——release 自己就说明了版次。
    - **读面**:`releases[]` 每行加 **`labels[]`**,形状与作品级 `labels[]` 逐字同(一家公司一条 + `kinds[]` + `work_count`,共用同一折叠与同一聚合,故两个粒度的数不可能各说各话)。**恒在**,该版次无已知公司时为 `[]`——与同结构里的 `refs[]` 同规:缺键与消费端解析失败无从区分,而「不知道谁发行了这一版」是常见且真实的答案(上游发行方尚无 exact 锚时即如此)。`GET /v1/catalog/releases` 的条目同样恒带此键(时间线上一行提出的问题正是「这个移植是谁在出」,而作品块答不了它)。没有它,表写了没人读,作品页仍是一堆压平的公司。
  - **wave 201——tag 的 `work_count` 改读上卷**:`labels` / `engines` / `series` 三条边靠外键直达作品,现算是一次索引区间扫描;`tag` 是唯一经映射表(`catalog_tag_source_map ⋈ catalog_work_tag`,约 120 万行)找作品的边,没有一种排序能同时服务两个方向,postgres 的最优计划要走遍全部过闸作品、展开每部约 39 条 tag 行(产出约 43 万行只留 6.9 万)。**生产实测 200–400ms,每次 `GET /works/{id}` 一遍,占本服务慢查询日志的 90%**。
    - 这不是执行计划问题:强制全部 join order、加 `(name, source_id) INCLUDE (work_id)` 覆盖索引、把 canonical tag id 反规范化进边表,三条路各自实测只有 ~1.5×,因为这道题本身就要算这么多。
    - 新表 **`catalog_tag_work_count`**(`tag_id` 主键 + `n_all` / `n_sfw` / `n_nsfw` + `computed_at`)。三列即读面的**全部参数空间**:chip 的数只随调用方 `nsfw` 变,展示轴那一支根本不随调用方变,其余闸门(未软删 / LIVE / galgame 媒介 / `claim_state=live`)由契约钉死。
    - **上卷由读路径自己的聚合函数产出**(`workCountsLive`,传 nil id 列表即「全部键」),不是第二份 SQL。这是它可信的唯一理由:上卷**只可能过期,不可能算的是另一道题**——一份由另一个实现填的上卷,一旦任一侧改动就会静默分叉,而两边都会继续自信地作答。测试 `TestTagRollupMatchesTheLiveAggregate` 常驻守卫这条等式。
    - **代价说白**:tag chip 的数从「构造上精确」降为「**截至 `computed_at` 精确**」。这是刻意的:tag 计数只在批量导入后才会动,而一个慢几分钟的浏览辅助数字,比一个每开一次作品页就要花掉三分之一秒的精确数字更值。**只对 tag 让**——另外三条边现算又快,让它们过期换不来任何东西。
    - **引导回退**:上卷表**一行都没有**时读面自动回落到现算,所以读半边可以先于写半边上线,不会把全站 tag chip 一次性打成 0;表里一有行,缺席即 0 且被采信(回退是引导,不是常驻第二意见)。
    - 刷新:`go run ./cmd/refresh-tag-counts --dsn …`,全量重算 + 单事务换页,重复跑是幂等的。

### 2.8 `GET /catalog/works/search?q=&medium_id=&limit=` — 作品标题搜索(只读)

产品站「上游优先建游」的选择器面(letmoe step 18):staff 在建游首屏一个输入框里搜上游是否已有此作品,搜到即一键读穿建行,无需填全量信息。

- `q`:标题子串(**NFKC 折叠**——复用 `catalog_work_title.title_norm` 生成列 `lower(normalize(title, NFKC))`,与导入期折叠字节一致;查询侧同法折叠后 `LIKE '%…%'`;空 `q` → Huma minLength 422)。
- `medium_id`:可选 medium 过滤(**-1 = 全部**,Huma 无法表达可空标量故用哨兵;letmoe 建游默认传 `1`=galgame 收窄结果);`limit` cap 50(默认 20)。
- **v1 无 trgm 索引**:纯 `ILIKE` 扫 ~19 万 title 行,staff 低频可接受;调用量升再对 `title_norm` 加 `pg_trgm` GIN 索引(记录,量升触发)。
- 响应 `items[]` = 轻 brief:`work_id` · `display_name` · `medium_id` · `content_rating` · `status` · **`site`(认领态,空=未认领)** · **`dlsite_id`(首个 DLsite workno 锚,无则缺省)**。`merged`(status=2)墓碑不出面。选中后产品站按需再走 §2.4 by-anchor / §2.7 works/{id} 取全量 bundle。

### 2.9 `GET /catalog/names/{id}/works` — 名义反查(只读)

名义(署名)反查:这个名义参与了哪些作品。用途 = letmoe 实体页(人物/名义页,step 20)的数据源之一——「这个制作方/声优/脚本还做了哪些」硬需求①,页面直达即自足。

- **名义自述**(`name`):`id` + **lang 分桶名**(`ja`/`zh`/`other` 三桶,名义只落其一——search 不变量 1)+ `latin` + `person_id` + **`siblings`(同一 person 的其他名义,各 `id`/分桶名/latin)**。`person_id` 与 `siblings` 一并给,消费方的人物页免二次查其余名义。
  - ⚠️ **分桶名只在本 S2S 面存续**。公开面 `GET /v1/catalog/names/{id}` 的同名 `name{}` 自 wave 191 起**弃用**——三桶看起来像「一名三语」,实为「一个名字,按它自己的语言归档」,于是无 `zh` 键被普遍误读为「无中文名」。公开面的替代物 = `display_name` + `lang` + `localized{}`(按 locale 取名的 map,供给同为三张别名表);wave 209 起这组字段是**全公开面统一的名字原语**、`localized{}` 并接受机翻填缺(§1 末),见 `docs/developer-platform/02-public-api.md` §3.7。
  - ⚠️ **link-visibility 铁则**(`model.LinkVisibility`,search/doc.go 把「人物页装配时过滤」明确指向此端点):credit_name→person 的**隐藏链接从不进入「同一人」聚合**。故 ①被查名义自身链接为隐藏时,`person_id` 与 `siblings` 一律不出(该名义呈现为独立身份);②`siblings` 恒只含**公开链接**的兄弟名义。
  - **wave 172 加法——人物块**:`photo_hash`(人物照片在图床的内容哈希,与作品封面 `image_hash` 同币种,消费端据此拼 CDN URL;**恒出**,空串=无照片)+ `gender` + `birth_y`/`birth_m`/`birth_d`(模糊生日三列,精度自表达;未记录则该键缺席)。五键**与 `person_id` 同一道闸**:隐藏链接下一律不出(`photo_hash=""`、其余缺席)——照片与生日是**人物事实**,在被刻意隐藏的链接下公开它们,等于泄露那条链接本身。人物列经 `LEFT JOIN catalog_person`(`deleted_at IS NULL`)取,孤儿名义照常返回自身行。
  - **wave 186 加法——公开面人物 `links[]`**:公开面 `GET /v1/catalog/names/{id}` 的人物块新增 **`links`**,元素 `{ source, url }`(与 label 的 `links` 同形),投影自**人物**(`entity_type=0`)的锚,**两条互不相干的铸造车道**:`link_kind=related` 走三面共用的那张模板表——官网 / twitter / pixiv / ci-en 等**非身份**网址;`link_kind=exact` 只对**两个源**铸造,即那些人物粒度的 id 恰好就是一个稳定公开页的源——**vndb**(s 前缀 staff id → `https://vndb.org/{sxxx}`)与 **bangumi**(person id → `https://bgm.tv/person/{id}`),id 形状不符的行整条跳过而**不猜**。`probable` **永不**铸造;`dlsite`(creater id 没有公开页)与 `erogamescape`(creater.php 未经核实)的 exact 锚同样**仍然排除**。exact 车道**只在人物面**:work / label 的 `links` 不受影响,那张共用模板表一字未改。**身份锚不等于页面地址**——铸造只能从**人物级**锚取,credit_name 上的 vndb ref 是 staff **别名** id(生产上是裸数字 `1` / `10` / `100`),照它拼 URL 会落到毫不相干的 VN 页,论坛就这样出过一次张冠李戴事故。它**与 `person_id` 同一道闸**:隐藏链接下不出——主页是**人物事实**,在被刻意隐藏的链接下公开它,等于泄露那条链接本身。孤儿名义与隐藏链接一律 `[]`(**恒出,不为 null**),两条车道**合并后**统一排序 `(source_id, external_id)`。
- **作品列表**(`items`,offset 分页,cap 50):每条 = `work`(轻 brief:`work_id`/`display_name`/`medium_id`/`content_rating`/`status`/`site` 认领态)+ **`roles`**(该名义在此作担任的**全部** role,每条 `role_id`/`role_key`/`role_name` + `identity`(§2.5)+ 若配音则 `character_id`/`character` 具名)。`total` = 该名义参与的**去重作品数**。**被压制的 credit 行不出**,`total` 与 `items` 同谓词——一个作品的全部 credit 都被压制时它整条离开列表。
- 反查走 `idx_catalog_credit_credit_name_id`(既有索引,无需新建)。缺失 id → 404(与 by-anchor/works/{id} 同义)。

### 2.10 `GET /catalog/characters/{id}/works` — 角色反查(只读)

角色反查:这个角色出现在哪些作品、由谁配音。用途同 2.9(letmoe 角色页,step 20)。step 46 起为**并集**:出演边(`catalog_work_character`)∪ 配音(`catalog_credit`)。

- **角色自述**(`character`):`id` + **lang 分桶名** + `latin`。
- **作品列表**(`items`,offset 分页,cap 50):每条 = `work`(同 2.9 轻 brief)+ **`kind`**(出演边强度:0=unknown/1=main/2=secondary/3=appears;**仅经 credit 命中而无出演边时为 0**)+ **`spoiler`**(出演剧透档:0=none/1=minor/2=major;仅经 credit 命中而无出演边时为 0,step 47)+ **`identity`**(`omitempty`,该出演边在**本作**上的行身份键;仅 credit 命中时缺席——判别语义同 §2.4)+ **`voiced`**(bool,该角色在此作是否有 VA credit——即使 `kind=0` 也能区分「纯出演」与「有配音」)+ **`voices`**(经 credits 的 `character_id` 关联,为该角色配音的名义,各 `credit_name_id`/`name`/`lang`/`latin`;同名义同作多条 credit 去重为一;纯出演作序列化 `[]`;**多对一折叠,故不下发 `identity`**,压制入口在 §2.5)。`total` = 该角色出现的**去重作品数(并集)**。**压制的连带代价(wave 08/R2c-1)**:并集的 credit 半边排除被压制行,故压掉某作的全部 VA credit 后,`voiced` 由 true 翻 false;若该角色在该作**没有出演边**,该作整个离开 `items` 与 `total`(两者共用同一个 `UNION`)。**并集的出演边半边同规(wave 10/R2c-2)**:被压制的出演边(`catalog.work.roster.suppressed`)同样排除,`kind`/`spoiler`/`identity` 与这一半用**同一个谓词**——压掉某作的出演边后,该作若还有 VA credit 就退回 credit-only 形态(`kind`/`spoiler` 归 0、无 `identity`),否则同样整个离开 `items` 与 `total`。`kind`/`spoiler` 的共识值 + 人工覆盖语义见 §2.4。
- 反查走 `uq_catalog_work_character`(character_id 成员)+ `idx_catalog_credit_character_id`(既有索引,无需新建)。缺失 id → 404。

### 2.11 `GET /catalog/characters/{id}` — 角色详情(只读,step 46)

角色实体自述,letmoe/kungal 角色页直达即自足(§2.4 的 `characters` 投影给的是 work 内花名册摘要,本端点给的是角色本体)。

- 响应 `data`:`id` · `display_name` · `latin` · `lang` · **`gender`**(1=male/2=female/3=other;缺省=unknown,无 0 值)· `description` · **`instance_of`**(跨宇宙变体的基底角色 id,VNDB instance_of;非变体则缺省)· **`image_hash`**(**胸像**内容哈希,step 47 VNDB 波前恒缺省)· **`figure_hash`**(**全身立绘**内容哈希,167 §11 Getchu 波;与 `image_hash` 是不同资产,不是它的回落。**字节已抠除白底**(wave 204,ToonOut):带 alpha 的 webp、并按主体包围盒裁去了留白,因此可以直接渲染在任意底色上,深色模式不再出现白框;抠图前的白底原件留在 `catalog_character.figure_source_hash`,**不上读面**)· **典型集物理属性**(step 81 字段 PR C2,真列平铺,均缺省=unknown、无有意义 0 值):`birthday_month`/`birthday_day`(月/日;年份不上列——见 `extra`)· `blood_type`(1=A/2=B/3=AB/4=O)· `height_cm` · `weight_kg` · `bust_cm`/`waist_cm`/`hip_cm` · `cup`(逐字大写罩杯 token)· **`extra`**(受治理长尾,源命名空间 `{"bgm": {…}}`:未晋升为真列的 Bangumi infobox 字段——星座/年龄/属性/趣味…,以及被剔出真列的生日年份/越界测量值原样保留;值为字符串或字符串数组;无长尾则缺省)· **`attr_sources`**(真列属性的来源归因 `{列名 → source key}`,如 `{"gender":"vndb","blood_type":"bangumi"}`,取自各属性列 field_provenance 的最新 writer;无填充属性则缺省)· **`aliases`**(书写变体,各 `id`/`name`/`latin`/`lang`/`kind`(0=translation/1=spelling_variant/2=search_hint)/`is_primary_for_locale`;无别名序列化 `[]`)· **`intros`**(多语言角色简介,step 65 字段 PR C1:统一形状 `[{ lang, intro, source_id, machine? }]`,与 §2.4 work `intro` 块同形;一语言一元素,同语言多源**先按 `provenance` 升序(源行 0 优先于机翻行 1),机器行内部 `derived`(18)的提取行优先于翻译行,再按 `source_id` 升序**取一(derived 例外=wave 178 panel 裁定:「质量对比好者留」的胜者以 `derived` 行落库、由该例外浮出,panel 判既有行更好时**什么都不写**、选举自然维持原状;两处读面同规:公开面与 S2S 详情面;源文行仍**永不被任何机器行遮蔽**);`machine: true` 仅出现在被浮出的机翻行上(实体简介机翻波 refs/proj/172:同 work 的 step-75 契约——填缺 zh-Hans、沿用源行 `source_id`、`src_hash`/`mt_model` 落库、**永不遮蔽源行**);**角色是 catalog 原生实体,无 claimed/bodyless 之分、无桥接**——恒读 `catalog_character_intro` 原生行;源料 = Bangumi 角色 summary(假名启发式 ja/zh-Hans)+ VNDB 角色 description(en,`[spoiler]` 段**整段剔除**后落库)+ Getchu ja(refs/proj/167)+ 机翻 zh-Hans;无简介序列化 `[]`)· **`traits`**(VNDB trait 集,step 93:统一形状 `[{ id, name, name_zh?, group_tid, group_name?, group_name_zh?, sexual?, spoiler_level, lie? }]`,按 `(group_tid, gorder, name)` 排序令组内连续;`?spoilers=0|1|2` 封顶 spoiler_level(**默认 0——不显式要就永不泄剧透**);`name`/`group_name` 为 VNDB 英文原样;**`name_zh`/`group_name_zh` 是词表的简体中文名(wave 176)**,取自 `catalog_character_trait.name_zh`(组名同经 group_tid 自连接解析),**无中文名时整个字段缺省**——消费端回落英文原名,而不是渲染空串;来源记在 `name_zh_provenance`(0=策展词表/人工,1=机翻,仅 `name_zh` 非空时有意义,策展永不被机翻覆盖),该列**在本 S2S 面仍不上读面**(公开面自 wave 212 波 A 起以 `traits[].localized["zh-Hans"].machine` 出它);`sexual` 标志随行——catalog 是 R18 面,消费端自行门控(step 81 BWH/cup 先例);`lie`=VNDB「表面为真实为谎」语义原样;词表单源(vndb_tid UNIQUE 即 source map,无 map 表);无 trait 序列化 `[]`)。
  - 属性来源与 survivorship(step 81):真列由 `internal/jobs/charattrs` 从 VNDB 类型列(优先)与 Bangumi infobox(补缺)回填;同字段两源都有 → VNDB 胜(类型列 > 自由文本正则);人工编辑(`field_provenance` writer=`user`)永不被回填覆盖。
- 缺失 id → 404(与 labels/{id}/works、by-anchor 同义)。

> **读面无 site 绑定**(16 语义:绑定只作用于写端点 claim);读端点仍走 Basic S2S(无凭据 401)。

### 2.12 编辑面字节上传 — **已删除(wave 181)**

`POST /api/v1/catalog/edit/images` 曾把用户选的封面/截图 multipart 转发到图床,上传者由表单字段 `actor_uid` **断言**。wave 181 删除本端点:上传只剩用户面一份,见 **§4.4**(`POST /api/v1/user/catalog/edit/images`,上传者取自令牌 `id`)。preset 白名单、catalog 站图床身份、错误映射一律不变——搬走的只是「谁上传的」这个问题的答案来源。

### 2.13 最佳封面投票 — **已删除(wave 181)**

`PUT|DELETE /api/v1/catalog/works/{workID}/covers/{coverID}/vote` 曾以 `{"site":…,"actor":{"user_id":N}}` 断言投票人。wave 181 删除这对端点:一次凭据泄漏即可代任意用户投票,而「后端自己认证了用户」这个理由从未有过真实调用方。投票只剩用户面一份,见 **§4.1**;表(`catalog_cover_vote`)、service、「一人一作一票」的唯一键、级联规则均原样保留,只是没有第二扇门了。

票的语义(读侧仍然成立):

- 票是**参考数据**,只写 `catalog_cover_vote` 一张表——**绝不写 `sort_order`/`portrait_pinned`**,编辑的钉子恒压过它,下游各面自行决定拿票数做什么;NSFW 裁剪仍在读面按既有 `catalog_work_cover.sexual`/`violence` 走。仅**封面**有票(截图/立绘无,也无通用 `entity_type` 列)。
- 读回:票数挂在 §2.4 的 `covers[]` 的 `vote_count` 上。**S2S 读面不再有 `voted`**——它需要一个「问的人」,而 S2S 面没有已验的人;`voted` 只在用户面(§4.4 `listCatalogWorkCoversUser`)出现。
- **级联**:`catalog_cover_vote.cover_id` 带 `ON DELETE CASCADE` 外键——封面行一删,其票自动消失,无需任何 Go 代码记得这件事。
- **已知副作用(诚实记录)**:编辑面的 covers 字段是**整行集替换**(`editspec` 先删该作品全部 `source=curated` 封面行再重建,行 id 随之换新),因此**一次封面编辑会连带清空这些封面上的票**(非 curated 源的 vndb/bangumi/dlsite 行不受影响)。票是参考数据、重投成本低,故按级联语义如实接受;若日后要让票熬过编辑,正解是把编辑面改成**按 image_hash 就地 upsert**(保 id),而不是给票表加旁路。

### 2.14 `GET /catalog/edit-revisions/feed` — 编辑版本史游标 feed(只读,wave 155 W3;wave 180 富化)

下游产品**回放编辑史**的唯一入口(贡献者账、编辑 inbox)。与 §2 其余端点同一道 Basic client 认证,与 `GET /catalog/claim-events/feed` **同门同形**:按 id 升序、`since` 独占,消费方只存一个整数,回放从 `since=0` 起即为全量。

- 查询参数:`since`(独占游标,`0`=从头)· `limit`(默认 200,上限 1000)· `entity_family`(如 `catalog`)· `entity_type`(如 `catalog.work`)· **`site`(wave 180,只回放本租户的修订)**。
- 响应 `data`:`{ items[], next_since }`;空页 `next_since` 回显传入值(不倒带)。`items[]` 每项:`id` · `entity_family` · `entity_type` · `entity_id` · `seq` · `action`(0=created 1=merged 2=direct 3=reverted)· `changed_fields[]` · `actor_uid` · `amender_uid`(可空)· `proposal_id`(可空)· `site` · `created_at` · **`product_work_id`(可空,wave 180)**。
- **`product_work_id` = 产品侧作品 id**,由 `catalog_work` 的认领列(`site` + `product_work_id`)按 `entity_id` 投影而来,**仅 `entity_type=catalog.work` 且认领租户与该条修订的 `site` 相同时非空**;其余一律 `null`(未认领作品、他站修订、非作品实体)。消费方由此可直接按自己的 id 落账,无需逐条回查。
- feed **不带 `snapshot`**:它回答「谁在何时改了哪些字段」,整份快照请按需读 §4.2 / S2S 的 `revisions` / `diff`。

### 2.15 S2S 写面 — **已全部退役(wave 185)**

wave 181 之后,S2S 面上还剩五条会**写**的人类端点,每条都靠请求体里的 `actor` 断言操作者:

| 已删除的 op | 曾经的方法 · 路径 | 现在用 |
|-------------|-------------------|--------|
| `createEditProposal` | `POST /api/v1/catalog/edit/proposals` | `createEditProposalUser`(§4.2) |
| `withdrawEditProposal` | `POST /api/v1/catalog/edit/proposals/{id}/withdraw` | `withdrawEditProposalUser`(§4.2) |
| `getEditSchema` | `GET /api/v1/catalog/edit/schema/{entity_type}` | `getEditSchemaUser`(§4.2) |
| `submitCatalogWork` | `POST /api/v1/catalog/works/submit` | `submitCatalogWorkUser`(§4.3) |
| `actOnCatalogClaim` | `POST /api/v1/catalog/works/{id}/claim-actions/{action}` | `actOnCatalogClaimUser`(§4.3) |

它们当初留下只因两个产品仍在实调(letmoe 三条、moyu 两条);两边迁 Bearer 之后,跨仓普查(各兄弟仓 origin 分支)与 **48 小时生产访问日志**均确认零调用方,故一并删除——**404/405,不是 410**,与 §2.12 / §2.13 同一套办法。`/api/v1/catalog/edit/proposals` 这条路径**仍在**,但只答 `GET`(`listEditProposals`),POST 已无。

删的是门,不是语义:两面**共用同一 editing engine / registry / 站点 overlay**,认领两条**共用同一 service、同一状态机、同一 `catalog_claim_event` 账本**,故一条认领或一次编辑的历史,无论过去从后端还是现在从浏览器写入,都读作同一串事件。至此 **S2S 面(`/api/v1/catalog`)上再没有任何人类写端点**;仍在的写只有 §2.3 的 `claim`(机器侧的注册/认领,不涉及「代表某个人」),其余全是读:两条游标 feed(§2.14、`claim-events/feed`)、`listCatalogClaimsByUser`、`listEditProposals`、`listEditRevisions` / `diffEditRevisions`,以及 §2.1-2.11 的各读面。

## 3. Admin 四桶(Bearer JWT + **ren 超管角色**,前缀 `/api/v1/admin/catalog`)

人审治理面,把「机器不敢自动终判」的三类东西交给人。**门是三层**(wave 187b):client 闸(第三方应用 → 403,先于权限)→ 路径分支 → 权限键(`claims/*` 用 `catalog.claim.review`,其余用 `catalog.review`)。

| 桶 | 端点 | 是什么 |
|----|------|--------|
| **candidates** | `GET /candidates` · `POST /candidates/decide` | 匹配候选(如共享 twitter/pixiv handle 的名义对)——判「同一人/不是」 |
| **proposals** | `GET /proposals` · `POST /proposals/{id}/{action}` | 合并提案(把两个实体判为同一个)——approve/reject |
| **refs** | `GET /refs/probable` · `POST /refs/confirm` · `POST /refs/reject` | probable(1)级来源锚——升为 exact 或驳回 |
| **image-references** | `GET /image-references?hash=` · `POST /image-references/detach` | 某个图片 hash 被哪些 catalog 行引用(封面/截图/角色胸像/**角色胸像白底原件**/角色立绘/**角色立绘白底原件**/会社 logo/人物照片八种),以及一次性摘除这些引用——删图前的预检,避免删掉字节留下空画廊 |

admin 面走 oauth 的共享 JWT 中间件 + `RequireRole("ren")`(**超管专属**——目录人审是高权限运营面,普通 admin 不放行);**不经 site 绑定列**(它是运营人审,不是产品 S2S)。

## 4. 用户令牌写面(Bearer JWT + `catalog:edit`,前缀 `/api/v1/user/catalog`;wave 176)

catalog 的**第三张脸**,也是「用户写面」的起点。教义一句话:

> **actor 取自已验令牌,租户取自签发该令牌的 client——身份的任何一部分都不从请求体读。**

这正是与 S2S 写面的唯一区别。S2S 面上产品后端说「kungal 的用户 5 干的」,catalog 因为后端自证了身份而采信;后端一个 bug 或一次凭据泄漏,就能以任何人的名义写。本面上**没有可以撒谎的字段**:uid = 令牌 `id` claim,site = 令牌 `client_id` 对应 `oauth_clients.catalog_site`,两者都在服务端解出。

**鉴权链**(路径域 Fiber 中间件,顺序即拒绝顺序):

| 步 | 检查 | 失败 |
|----|------|------|
| 1 | `middleware.JWTAuth`:签名/过期(与 admin 面同一 accept-both verifier) | **401**(JWKS 不可达 → **503**,是本服务的故障而非调用方的) |
| 2 | 令牌 `id` claim > 0 | **401**(「令牌不指名任何用户」——票永不系统归因) |
| 3 | `scope` 含 **`catalog:edit`**(空格分隔,**整词**匹配) | **403**(message 点名缺失的 scope) |
| 4 | `client_id` claim 非空 | **403** |
| 5 | 该 client 在 `oauth_clients` 存在且 `catalog_site` 非空 | **403** |

第 4 步即「**一等登录令牌(`/auth/login`)被拒**」的原因:它没有 `client_id`(RFC 9068 §2.2 对它是可选),因而没有可归属的站点;若允许它自报 site,就等于把本面存在的意义(消灭断言)重新打开。用户令牌须经 OAuth 授权码流从某个 client 取得。

- **scope**:`catalog:edit` 是**用户 scope**(经 OP 同意页授予),与开发者平台的 **API key scope**(`internal/platform/devapi`)分属两套凭据、两条授予路径,不可混用。常量落在 catalog handler 包内,与 image 服务把 `image:upload` 写在自己鉴权中间件旁的先例一致。**唯一同名的是 `catalog:read`**(2026-09-06 起):`/v2/catalog` 只读面接受密钥**或**带该 scope 的用户令牌,所以同一个词在两条授予路径上都成立——仍然是一条请求一个凭据,不是把两套凭据合并。见 §5 的 public v2 face 条与 [developer-platform/10 §18](../developer-platform/10-native-app-integration.md)。

**第三方应用准入(wave R3,2026-08-17 起)**:自助注册的 user_login 应用**可以申请 `catalog:edit`**(此前 `selfServiceUserScopes` 按名拒绝)。第三方令牌走的就是本节这同一个面——**没有单独的 /v1 编辑路由**(E4 的裁定原文:「同一引擎 op 的 Bearer 投影,不是新写面」)。三条第三方专属规则:

1. **裁决帽**:第三方 client 的令牌永远 `ModerationCapped`——提案可,review/automerge/decline/revert 永不可,schema 投影里对它 `can_review=false`、`would_automerge=false`。这不是权限配置,是面的属性。
2. **未决提案帽**:经第三方 client 提交时,若该用户名下 open 状态的提案已有 **20** 条(跨租户计数、含第一方面提交的——提案表不记录提交面),新建被拒 **429**,message 给出数字与出路(等审核或撤回)。第一方面提交不受此帽。
3. **两句契约实话**(所有面共享,对第三方自动化尤其要紧):
   - **引用类校验发生在批准合并时,不在提案时**——`Validate` 只校形状(拿不到 db),撞上游键、被引用 id 存在性、车道归属都在 Apply 里查。提案保存成功 ≠ 能合入;审核者批准时可能得到 **422**(自 wave R3 起这些失败**保证**是 422 而非 500,提案可正常 decline 不再卡死)。不提供提案期预检:预检天生 best-effort(提案与合并之间随时可撞),写它进契约反而制造虚假承诺。
   - **提案引用的实体在提案期间被合并走 = 批准时拒绝**(404/422),不做静默 rebase——修订史永不记录提案人没写过的值。客户端经公开 redirects 面(`GET /v1/catalog/redirects`)重解析后重提。
- **前缀不相交**:`/api/v1/user/catalog` 与 `/api/v1/catalog`、`/api/v1/admin/catalog` 三者互不为前缀——Huma 注册在 Fiber **app** 上,路径域 `Use` 是唯一的闸,前缀一旦互相包含,S2S 的 Basic 链就会拦在用户调用前面。
- **spec**:两个写面同在 `docs/catalog/openapi.yaml`(tag `catalog-user`);运行时它是**独立的 humafiber API**,只是共用一份契约文档,便于调用方并排比较「断言 actor」与「令牌即 actor」。

### 4.1 `PUT|DELETE /user/catalog/works/{workID}/covers/{coverID}/vote` — 最佳封面投票(写)

用户写面的**首批 op**。语义、service、表、拒绝映射与 §2.13 **逐字相同**,只有两个身份值的来路不同:

- **无请求体**。S2S 面要断言的两个值(`site`、`actor.user_id`),恰是本面拒绝从线上读的两个值。
- 响应同形:`{ cover_id, vote_count, voted }`。
- 一人一作一票、`PUT` 到另一张封面 = 票搬家、`DELETE` 幂等 200、作品/封面不可投 → 404 —— 这些是**表的唯一键与 service 的规则**,不是某一张脸的意见,故跨面成立。
- `site` 仍只作来源标注、不入唯一键:同一个人经两个 client 登录投票,仍是**一张会移动的票**。

### 4.2 编辑提案与审核(写 + 只读投影,wave 177 三件套 + wave 178 审核面)

> ⚠️ **S2S 的编辑裁决端点已在 wave 181 删除**:`getEditProposal` / `amendEditProposal` / `mergeEditProposal` / `declineEditProposal` / `revertEditEntity` / `getEditSnapshot` 六条不复存在(404/405,不是 410)。理由同 §2.13——断言式 actor 意味着「后端说是谁就是谁」,一个 BFF 的 bug 或凭据泄漏即可代任意用户**裁决**编辑;跨仓普查确认它们零外部调用方。请改用本节的 op。两面**共用同一 editing engine、同一 registry、同一套站点 overlay**,只有 actor 的来路不同,故策略结论跨面一致。
>
> ⚠️ **过渡期留下的三条也已在 wave 185 删除**:`createEditProposal` / `withdrawEditProposal` / `getEditSchema` 不复存在(404/405,不是 410)。它们当初留着只为 **kun-letmoe-community 仍在实调**;letmoe 迁 Bearer 之后,跨仓普查(各兄弟仓 origin 分支)与 **48 小时生产访问日志**都确认零调用方,故按 wave 181 的同一套办法删除。孪生分别是 `createEditProposalUser` / `withdrawEditProposalUser` / `getEditSchemaUser`。**wave 183 起 `trust_tier` 在 infra 侧已有事实来源**——用户面在门口用权限键 `catalog.edit.trusted` 推导信任层级(见下方与 docs/auth/04 §2.3),letmoe 的 `ProposeTrusted` 通道在 Bearer 面同样成立,故删除不留能力缺口。
>
> S2S 编辑面**至此只剩读**,三条,各有各的理由,都不是过渡:
>
> - `listEditProposals` —— 第三人称统计(forum 个人主页、patch 创作者统计):那里的 `proposer_uid` 是**被看的那个人**,不是断言的「我」,故不属于清理的范畴。
> - `listEditRevisions` / `diffEditRevisions` —— **版本史读**,任何人都可读的公共投影,没有「令牌本人」这一维。

| op | 方法 · 路径 | 是什么 |
|----|-------------|--------|
| `createEditProposalUser` | `POST /user/catalog/edit/proposals` | 以令牌本人的名义提交编辑提案(策略允许时当场直编) |
| `withdrawEditProposalUser` | `POST /user/catalog/edit/proposals/{id}/withdraw` | 撤回**自己**的开放提案 |
| `getEditSchemaUser` | `GET /user/catalog/edit/schema/{entity_type}?entity_id=` | 字段 schema + **本令牌**的逐字段能力投影 |
| `getEditProposalUser` | `GET /user/catalog/edit/proposals/{id}` | 提案详情(含 amendments 与 effective_patch),wave 181 起是**唯一**的提案详情面 |
| `amendEditProposalUser` | `POST /user/catalog/edit/proposals/{id}/amendments` | 以本人名义修订开放提案(`{set?, unset?, note?}`;逐字段需 review 权) |
| `mergeEditProposalUser` | `POST /user/catalog/edit/proposals/{id}/merge` | 以本人名义合并开放提案(`{note?}`;逐字段 rebase,409 带冲突字段表) |
| `declineEditProposalUser` | `POST /user/catalog/edit/proposals/{id}/decline` | 以本人名义拒绝开放提案(`{note?}`,理由落 `decision_note`) |
| `revertEditEntityUser` | `POST /user/catalog/edit/revert` | 以本人名义回滚实体到历史版本(`{entity_type, entity_id, to_seq, note?}`;**无 `site`**——租户取自令牌 client) |

- **create 请求体 = 已删除的 S2S `EditProposalCreateRequest` 减去 `actor` 与 `site`**,即 `{entity_type, entity_id, patch, note?}`。这不是「精简版」,而是**把可以撒谎的地方全部删掉**:提案人 = 令牌 `id` claim,租户 = 令牌 client 的 `catalog_site`。响应与 S2S 逐字相同(`{proposal, merged, revision?}`)。
- **roles 取自令牌**:`middleware.JWTAuth` 的 `user_roles`(全局 `roles` claim ∪ `site_roles`)直接喂进该实体 family 的权限词表。因此**管理员令牌在本面同样触发自动合并**(kungal overlay:propose=open / automerge=review),与它在 S2S 面断言 `roles:["admin"]` 的结论一致;站点局部 moderator 只在**该站 client 签发的令牌**上成立(site_roles 的既有语义)。
- **withdraw 无请求体**:已删除的 S2S 撤回请求体只有一个 `actor`,减去之后什么也不剩。检查两道——先比对提案的租户与令牌 client 的 `catalog_site`(跨租户 → **403**),再由引擎校验「提案人 == 本人」(否则 **403** `ErrNotProposer`);已关闭的提案 → **409**,不存在 → **404**。
- **schema 投影不接受任何 actor 查询参数**:已删除的 S2S 版所带的 `user_id` / `roles` / `trust_tier` / `is_entity_owner` / `site` 在本面**从来不存在**——调用方无法询问「换成别人会怎样」。保留的 `entity_id` 描述的是**投影对象**(实体感知的 overlay 对它求值),不是调用者。
- **审核四件套(amend / merge / decline / revert)请求体 = S2S 版减去 `actor`(revert 再减 `site`)**,响应与 S2S 逐字相同(amend → `EditAmendmentView`,merge → `EditRevisionView`,decline → 关闭后的 `EditProposalView`,revert → `{proposal, revision}`)。审核者 = 令牌本人,`amender_uid` / `decided_by_uid` / 回滚 revision 的 `actor_uid` 都写它。
- **租户闸(四件套 + 详情读共用)**:先比对提案的 `site` 与令牌 client 的 `catalog_site`,不符 → **403**(先于任何引擎规则,跨租户调用方只知道「不是你的」);revert 无提案可比,租户即令牌 client 绑定值,直接作为 overlay 键与写入租户。
- **归属(ownership)是 catalog 自己持有的事实了(wave 178)**:`catalog_work.owner_user_id`(可空 bigint,**write-once**)——提交铸造(§2 submit)与**认领诞生**(claim 动作,`from_state IS NULL` 的那一次)各写一次,之后**永不覆盖**;NULL = 未知/历史行(手工回填脚本 `apps/api/cmd/migrate/backfill/owner-user-id.sql`,由 forum `galgame.creator_user_id` 与诞生事件两路补齐)。引擎经 spec 的 `OwnerUserID` 钩子**推导** `IsEntityOwner`(仅当钩子存在、调用者 uid 非 0、且存储 uid == 调用者 uid),因此 **kungal 的 owner-review 通道在用户面天然成立**,无需任何后端断言,forum 侧的镜像权限闸可以删除。
  - **推导只会把标志置 ON,永不置 OFF**:S2S 面断言的 `is_entity_owner` **照旧被采信**(某些 family 没注册钩子、某些产品有自己的归属定义),两者是并集关系。
  - **`trust_tier` 自 wave 183 起有了 infra 侧的事实来源**:令牌的角色若持有 catalog 权限键 **`catalog.edit.trusted`**,本面即以 `TrustTier = 2`(引擎的 `TrustedTier`,也是引擎唯一比较的层级值)求值策略;否则仍是 0。代码捆只给 admin/ren(等价于 letmoe 旧 S2S 断言给 staff 的标准),**产品站把这把键授给自己的角色(如 letmoe 的 `creator`)走权限矩阵控制台的叠加层,热替换 resolver,无需改代码或部署**。因此 **letmoe 的 ProposeTrusted 通道不再需要 S2S 后端代为担保**。S2S 面断言的 `trust_tier` 照旧被采信——两者是并集,推导只会把能力置 ON。(2026-09-09 起本键**只**管编辑引擎的信任层级;建档投稿免审是另一把键 `catalog.claim.trusted`,拆分动机与行为见 v2-implementation-notes 的 Stage 6 write。)
- 错误映射沿用 S2S 面的口味:未注册字段键/空 patch/空 delta → **422**,策略拒绝 → **403**,实体/提案/目标 revision 不存在 → **404**,rebase 冲突/提案已关闭 → **409**;令牌缺失或无效 → **401**,缺 `catalog:edit` scope → **403**(message 含 scope 字样)。

**第三方应用的姿态封顶(wave 186b)**:令牌**签发自哪个应用**是权限的一维。经**第三方开发者应用**(`oauth_clients.owner_user_id` 非空)签发的用户令牌,**永远拿不到 `editing.TrustedTier`**,即使其 roles 持 `catalog.edit.trusted`;它只能**提案**(走各租户的 open/queue 通道),绝不自动合并。同理,它**永远不是审核面**:认领裁决四动作(`approve`/`decline`/`ban`/`unban`)与开放面的审核队列视图(§4.5)对它一律 **403**,先于权限检查。理由是**信任与审核都是「人 × 第一方 client」这一对的属性,而不是人单独的属性**:用户的 roles 会随令牌进入他授权的**任何**应用,不封顶的话,某站用权限台授出的信任只要成员在一个第三方 UI 上登录一次就被借走,而该站从未同意被那里编辑。封顶**只减不增**:人在产品自家面上的一切权限一字未改。

**封顶下沉进引擎与后台面(wave 187)**:186b 留下的两个洞已闭合,三个面现在说同一句话。

- **187a — 编辑引擎有了 client 维度**。`editing.PolicyContext` 新增 `ModerationCapped bool`(装配点断言「这个**面**不是下裁决的地方」,引擎本身仍对 OAuth/client 一无所知);用户面的 `policyCtx` 由 `isThirdPartyClient(clientFromCtx(ctx))` 唯一地填它——本包里 PolicyContext 只在这一处诞生,故任何现有或将来的 op 都不可能忘记问。置位后引擎**两个咽喉点**同时失败:`Policy.AllowsReview`(amend / merge / decline / revert 与 schema 投影的 `can_review` 全部经此解析审核资格)与 `allowsAutomergeWithOwner`(automerge 的**全部**规则)。因此第三方令牌**无论 roles 持不持 `edit.catalog.work.review`、无论租户姿态多开放**(含 `automerge=always`、`automerge=review`、`automerge=owner`、以及 catalog 已推导出的 owner 归属),都**只能提案入队**,且**不能裁决自己排进去的那条**。之所以必须落在引擎而非只包一层 HasPerm:`always`/`trusted`/`owner` 三条 automerge 规则**根本不查权限键**,只包 HasPerm 会让开放租户照旧当场落盘。**保留的**是 tier-0 陌生人本就有的:提案、以及撤回自己的提案(withdraw 不是裁决)。
- **187b — 后台面有了 client 绑定**。`/api/v1/admin/catalog` 的 `AdminGate` 在两条权限分支(`catalog.review` / `catalog.claim.review`)**之前**先解析令牌的 client(与用户面同一个 `OAuthClientLookup` 注册表):第三方应用 → **403**「a third-party application is not a moderation surface」;令牌所指 client 未注册 → **403**。先于权限判定,故答复不因人是不是 staff 而变,不能当探针用。
- ⚠️ **明知的缺口(已在测试里钉住)**:平台自家控制台经 `/auth/login` 登录,其令牌**根本不带 `client_id` claim**(见 `utils.TokenClaims.ClientID`),因此 `AdminGate` 对**空** client id 放行。空 claim 只可能出自本 OP 自己的第一方会话流(即正被放行的那个面),缺口窄;但它只在这一点为真时才窄。待第一方会话流也有自己的注册 client(OIDC 标准化,docs/auth/03)后,应删掉该放行分支,改为像用户面 `UserGate` 一样 fail-closed。
- **线上形状零变化**:三个 spec(`openapi.yaml` / `admin-openapi.yaml` / `public-openapi.yaml`)逐字节不变——封顶只改谁被拒,不改请求/响应形状。

**行级压制字段 `<field>.suppressed`(wave R1a,2026-08-16 起在产)**:上游 importer 拥有的行不属于编辑面(编辑面只看 curated 车道),但人有时需要让某一行**不再出现在任何读面**。办法不是删它——下一轮导入会把它重新推回来——而是给它记一条负知识。

- **形状**:凡支持此能力的字段 `F`,`getEditSchemaUser` 的 `fields[]` 里**多一个** key `F.suppressed`(`kind=list`、`diff_hint=items`),策略与 `F` 逐字相同(继承父字段的站点 overlay)。目前两个:`catalog.work.titles.suppressed` 与 `catalog.character.aliases.suppressed`。
- **值 = 严格升序、去重的字符串数组**(不是集合语义的任意顺序数组)。乱序或重复 → **422**。元素是**内容派生**的行身份键:标题是 `title:<kind>:<lang>:<title>`(`kind` 是数字:0 official / 1 alias / 2 abbreviation),角色别名是 `alias:<kind>:<lang>:<name>`。之所以不用行 id:importer 删了重插同一行会换 id,压制会正好在它该生效的时候失效。
- **文本段必须是规范形式(wave 柱 7,2026-08-16 起)**:键里的自由文本段(标题、别名名)以**剥掉不可见控制符(零宽/BOM/bidi/word joiner)并去掉首尾空白(含全角空格)后的形式**参与身份。发一个非规范键 → **422**,错误里会把规范键递回来。原因:目录侧有一个字符串卫生作业会原地重写这些文本列,若键按原始文本组合,它跑过之后压制会静默失配、被压制的行重新出面且无任何东西报错。**注意这只抹「语义为零」的差异**——上游把文本本身改了(改错别字),压制过期是正确语义,不在此列。
- **悬空键合法**:只校格式,不校该行此刻是否存在——上游今天没推、明天推回来的行,压制照旧等在那里。
- ⚠️ **不要自己拼键**:格式在此列出是为了让你读懂请求,不是让你从渲染出来的文本反推。文本段的规范化、未来可能出现的实体 id 段,都会让手拼的键与服务端算出的键错开——而错开的后果是「保存 200、回读得到、永远不生效」。全部不可见字符构成的文本(生产有 1 行)规范化后为空,**没有合法键,因而不可压制**,只能走数据修复。
- **被压制的行仍物理留在表里**(importer 仍拥有它),只是**不出任何读面**,标题搜索与搜索索引同规排除。父字段 `F` 的编辑快照**照旧包含**这些行(它是全量替换语义,过滤掉会导致下一次保存把它们物理删掉)。
- 压制与解除各产一条 `entity_revision`,可 diff、可回滚、有归属,与普通编辑同一条审计通道。
- ⚠️ **通用渲染的 UI 注意**:如果编辑器是「遍历 `fields[]` 逐个渲染」,`F.suppressed` 会作为一个裸字符串数组出现。它要么按压制 UI 渲染(在 `F` 的行旁边给一个「隐藏此行」),要么显式跳过 `.suppressed` 后缀——**不要**当普通文本数组交给用户手打。

**撞上游键 → 显式 422(wave R1c,2026-08-16 起)**:凡存储唯一键**不含 source 维度**的列表字段,人工新建的行若与 importer 既有行同键,保存**整体拒绝(422,message 点名第一个撞键元素)**,而不是静默丢弃——此前的行为是「先删光 curated 车道再 `ON CONFLICT DO NOTHING`」:撞键行插不进去且无任何报错,一次保存就把人工车道整体蒸发。本波覆盖 `catalog.work.covers` / `catalog.work.screenshots`(撞 `(work_id, image_hash)`)、`catalog.work.labels`(撞 `(work_id, label_id, kind)`;存量 NULL source 边按机器行对待)、`catalog.work.links` 与 `catalog.label.links`(撞 `(source_id, external_id)` 锚且既有行非 curated)。credits(§2.5)与 character aliases 在各自字段波已按同一裁定落地。**这些新覆盖的字段今天没有 `.suppressed`**,422 文案如实说「上游行暂不可经编辑器重加/改样」,不指向不存在的压制字段;压制原语到位是后续字段波的事。

**已注册的 entity_type(wave R2a,2026-08-16 起)**:`catalog.work` · `catalog.character` · `catalog.release`(wave R2b)· `catalog.label` / `catalog.tag` / `catalog.engine` / `catalog.series`。编辑面按 `entity_type` 泛化,**新增一个类型不新增任何路由**——`getEditSchemaUser` 换个 `entity_type` 即可,三份 spec 逐字节不变。

- `catalog.character` 开的是**内容**:14 个标量(`display_name` / `lang` / `latin` / `description` / `gender` / 生日月日 / 血型 / 身高体重三围 / cup)+ `aliases`(**只含人工车道行**,上游别名不进字段值,要隐藏走 `aliases.suppressed`)+ `intros`(多语言,人工车道)。**身份不开**:`instance_of`(角色折叠)与外部锚仍归合并机器,永不注册。
- `catalog.release`(wave R2b,2026-08-16 起)开的是**既有行的就地编辑 + 隐藏**,六个字段:`kind`(冻结词表 0-4)/ `title` / `lang`(olang 词表 + 6 个 vndb 补码)/ `platform`(47 码冻结词表)/ `released`(**复合日期对象 `{y,m,d}` 或 null**,y∈[1950,2200] 必填、d 要求 m 在场;engine `KindDate` 的第一个使用者,这里就是该 Kind 的值形状定义)/ `hidden`(bool)。**没有新建 op、没有删除 op**(D8 裁定:能力缺席优于能力泛滥,引擎里没有这条路)。实体 id = `catalog_release.id`。
  - **`hidden` 的语义 = 写 `deleted_at`**,不是 `edit_suppressed_row`:该表全部渲染面与派生 job 本就过滤软删、importer 把软删行当占位不复活(有钉死测试)、release 行永不合并去重——三个先决齐备,压制因此零读路径改动。隐藏/解除隐藏都是普通字段编辑(可提案、可回滚、有归属)。锚→work 的身份解析**有意**不看软删(隐藏的 release 仍锚定身份,与 importer 占位语义一致)。
  - **人工编辑受柱 6 保护**:六字段全部盖 `field_provenance` 戳;`releasemeta`(填空日期)与 `workplatforms`(填空平台)在既有「只填空」守卫之上追加人工戳排除——人**清空**的日期/平台不再被机器回填。用户投稿铸的 curated 行日期自带 `user` 戳(「curated release」从此有实体标记)。
  - **隐藏行的发现面**:`getCatalogWorkByIDUser` 传 `include_hidden=true` 时返回含隐藏行的 `releases[]` 并逐行给 `hidden` 标记;缺省载荷与 S2S/公开面逐字节不变(`hidden` 字段 omitempty)。
- kungal 租户下 character 的 `automerge` 是 **never**(与词表同档,不与 work 同档):一个角色被它出现过的每一部作品共享,直落的波及面接近词表。有 review 权也只入队。
- **多语言简介的折叠规则修正(同波)**:作品与角色的简介读面都是「每 lang 只渲染一条」,此前排序让 **curated(source 12)排在所有上游 importer 之后**,于是人工写的同 lang 简介保存成功、回读得到、却永远不出面。现已改为**同 provenance 档内人工车道优先**(顺序:`provenance` → 人工车道 → 其余)。生产上有 10 部作品的简介渲染文本因此改变——**这是修复不是回归**,每一条都是「人保存过的文本终于开始出面」。`provenance` 仍排在最前,所以机翻永远不会盖过原文。

### 4.3 认领生命周期(投稿 + 八动作 + 我的认领,wave 179)

> ⚠️ **S2S 的两条认领写端点已在 wave 185 删除**:`submitCatalogWork`(`POST /api/v1/catalog/works/submit`)与 `actOnCatalogClaim`(`POST /api/v1/catalog/works/{id}/claim-actions/{action}`)不复存在(404/405,不是 410)。理由同 §4.2——断言式 actor 意味着「后端说是谁就是谁」,一个 BFF 的 bug 或凭据泄漏即可代任意用户投稿、撤回,乃至**裁决**别人的投稿。它们当初留着只为 **kun-galgame-patch(moyu)仍在实调**;moyu 迁 Bearer 之后,跨仓普查与 48 小时生产访问日志确认零调用方,故按 wave 181 的同一套办法删除。孪生即本节的 `submitCatalogWorkUser` / `actOnCatalogClaimUser`——两面**曾共用同一 service、同一状态机、同一 `catalog_claim_event` 账本**,只有身份的来路不同,故删除的是门,不是任何语义。

| op | 方法 · 路径 | 是什么 |
|----|-------------|--------|
| `submitCatalogWorkUser` | `POST /user/catalog/works/submit` | 以令牌本人的名义投稿:铸造 pending 认领(注册行 + 内容 + 诞生事件,一个事务),并把本人盖成 `owner_user_id` |
| `actOnCatalogClaimUser` | `POST /user/catalog/works/{id}/claim-actions/{action}` | 以令牌本人的名义推动认领:`claim`/`submit`/`publish`/`withdraw`(须**本人是条目所有者**——**或该条目尚无主时的第一认领人**,此时动作即认领)或 `approve`/`decline`/`ban`/`unban`(须令牌 roles 持 `catalog.claim.review`) |
| `listCatalogClaimsMine` | `GET /user/catalog/claims/mine?claim_state=&before=&limit=` | **本人**在**本令牌站点**上动过的认领(即 S2S `listCatalogClaimsByUser` 的自照版),响应同为 `CursorPage<UserClaimItem>` |

- **请求体 = 已删除的 S2S 版减去 `site` 与 `actor`**:submit 为 `{product_work_id?, fields, released?}`,action 为 `{product_work_id?, reason?}`。投稿人/操作者 = 令牌 `id` claim,租户 = 令牌 client 的 `catalog_site`。响应形状与 S2S 版逐字相同(`WorkSubmitResponse` / `ClaimActionResult`),幂等规则(`product_work_id` 给了 = claim 键精确;没给 = 只认 payload 里的身份锚;两者皆无 = 重试会二次铸造)也逐字相同。
- **权威一分为三**:
  - **审核四动作**——令牌 roles 经 catalog family 的权限词表解析 `catalog.claim.review`,与 S2S 面解析断言 actor 的**同一个 resolver**(由 DB 权限 overlay 热替换,故权限台授权即刻生效);不足 → **403**。
  - **所有者三动作(`submit`/`publish`/`withdraw`)——「无主即认领,有主即他人」**:
    - `owner_user_id` 是**别人** → **403**。这是本波**新长出的牙**:S2S 面只校验租户(uid 是后端断言的、只能采信),在 uid 无法断言的面上,把它和所有者对一次是免费的。
    - `owner_user_id` 为 **NULL** → **放行,并在同一条 UPDATE 里把调用者盖成所有者**(write-once,与 `claim` 动作、投稿铸造同一套规则)。这不是宽容,**这就是产品的主手势**:注册表的大宗是机器导入的镜像存量,躺在 `draft` 且无主(prod 2026-08:kungal 有 **53,486** 条这样的 draft;pending/declined **零**条无主),forum 向导的「认领这部游戏」正是一个人对其中一条调 `publish`。若把无主判成拒绝,等于用一个看起来像安全检查的规则 403 掉整个功能。**第一个动它的人成为主人**,此后上一条把其他人挡在外面。
    - 检查与盖章同在 `SELECT ... FOR UPDATE` 事务内:两个认领者被串行化,输的那个撞上的是**迁移规则的 409**(它要的起始状态已经没了),而不是半盖的所有权。
    - **staff 面不参与认领**:`RequireOwner` 未置时(staff 审核队列)移动无主认领**不盖任何所有者**——curator 按定义是在裁决别人的认领,移动一条不该把它变成他的。
  - **`claim`(none→draft)**——任何已登录用户皆可,因为这个动作**就是**上一条所检查的归属的诞生;它照旧要求 `product_work_id`,并沿用 write-once 盖章(已有所有者的行永不改归属)。
- **租户对审核动作也传**(与 S2S 面不同:那里为让 curator 跨租户裁决而把 site 置空)。本面的 moderator 是经**某一个产品的 client** 到达的,该 client 绑定的站点是它唯一可裁决的租户;跨租户 → **403**(既有租户闸直接给出)。平台级队列是 staff 面(`/api/v1/admin/catalog`)的活,那面背后是 staff JWT 而非 per-product 令牌。
- **`mine` 无 uid 参数**:uid 与 site 全部取自令牌,故它天然只答「我的」。`claim_state` 用与全站一致的闭合词表解析器(非法值 → **400**,message 为同一句),`before` = 上一页末行的 `last_event_id`,`total` 即该用户的统计值(「我发布的」= `claim_state=live&limit=1`)。
- **S2S 认领面剩下的全是读**(wave 185 删掉两条写之后),不是过渡期的残留:`listCatalogClaimsByUser`(**读别人的**认领,forum 的个人资料页靠它)在用户面**故意没有对应 op**——`mine` 是本人的列表、不是任何人的;`listCatalogClaimEvents` / `listCatalogEditRevisions` 两条游标 feed(产品侧对账 cron 的面)、`revisions` / `diff` 两条版本史读、各类第三人称统计投影与 staff 审核队列**仍只在 S2S / admin 面**——它们要么是机器消费者的面、没有「令牌本人」可言,要么是公共读、本就不问是谁。wave 180 后**人类写与人类只读均已在用户面齐备**,wave 181 与 185 依次删掉了 S2S 上残留的孪生:S2S 面上**不再有任何写**,更没有任何需要断言 uid 的人类动作。
- 错误映射沿用 S2S 面:非法迁移 → **409**(带 `ClaimTransitionInfo`),重复投稿 → **409**(带 `WorkSubmitConflictInfo`),作品不存在 → **404**,未知 action → **400**,`decline` 缺 reason / `claim` 缺 `product_work_id` / 投稿缺 `display_name` → **422**,越权(条目属于他人、跨租户、无审核权)→ **403**。
- **铸造时年龄轴按 refs/anchors 派生(2026-08-26 起)**:铸造 stub 的每条路(wave R3 之后只剩 v2 `POST /v2/me/claims`,带 refs 且 `field_values` 未显式给 `catalog.work.content_rating`;wave R4 起显式给了就照给的铸,派生整条跳过)在落行前查 `src_vndb`(非补丁 release `minage>=18 OR has_ero`)与 `src_bangumi`(`nsfw` 或 wiki 维护的「R18」meta_tag),命中即铸 `content_rating=2`;查不到或 staging schema 缺席(dev/test)→ 照旧铸 0,**永不因派生失败拒绝认领**。派生值直写列、不盖 provenance 戳,人工编辑随时可改。兜底 = 每周三 `backfill-release-meta` 的分级车道,证据阶梯 **vndb → dlsite age → erogamescape `erogame` → bangumi(nsfw/meta_tag)**,只填 `content_rating=0` 的行;成因:认领波曾铸出 16 部年龄轴空默认 0 的成人作品,而 v2 认领把 refs 只转成 links 不落锚,周车道也救不了它们。
- **铸造前的同名软闸(2026-09-05 起)**:只有**铸造**路走这道闸(`work_id`、以及能解析到存量作品的 `refs`,两条都是**认领**,没有新行可重)。落行前把本次提交的 `display_name` 与 `catalog.work.titles` 逐条折叠(NFKC 小写 + 去掉含 U+3000 的全部空白),与**同 medium** 的存量作品比对:命中即整个事务回滚,答 **409 `DUPLICATE_SUSPECTS`**,`suspects[]` 列出这些作品(`id` + `display_name`,至多 8 条,因为答案是给人看的确认框、不是普查),**一行不写**。同一请求体带 `confirm_duplicates: true` 重发即照旧铸造,且铸后仍照旧把这些对子记成 pending match candidate——确认是投稿人说「这是两部不同的作品」,不是替对账车道下判决(那条车道拿着日期、refs、标签等投稿人根本不会传的证据)。折叠式与准入门槛**复用去重探测器自己的 SQL**(`WorkTitleFoldSQL` / `WorkDupeNormEligibleSQL`:≥4 runes,或恰好 3 个 CJK 统一汉字,故 `PC版`、`abc` 永不触发),闸与探测器的语义不许分叉;跨 medium 不比对,因为跨媒介同名改编是 xmedia-import 有意铸的。

### 4.4 用户令牌只读面(wave 180)

wave 176-179 把人类的**写**搬完了;本波搬的是搬完写之后还留在 S2S 面的**四件人类只读/取字节的活**——编辑器启动读、提案列表(自照 + 审核队列)、封面票数、编辑图上传。它们都还带着一个 forum 必须**断言** uid 的参数(`?uid=` / `proposer_uid` / `actor_uid` 表单字段),而那正是本面存在的意义要消灭的东西。搬完之后,**forum 在 catalog 上不再有任何断言人类身份的 S2S 调用**。

> **wave 181 收尾**:搬迁完成之后,S2S 面上那些已无外部调用方的孪生端点被**删除**(§2.12 / §2.13 / §4.2 的 ⚠️ 块列出全部八条 + 图片上传),`?uid=` 与 `covers[].voted` 也随之从 S2S 读面消失。此后「以某个人的名义」这件事在 catalog 上**只有一扇门**,即本面。

| op | 方法 · 路径 | 是什么 |
|----|-------------|--------|
| `getEditSnapshotUser` | `GET /user/catalog/edit/snapshot?entity_type=&entity_id=` | 实体当前的注册字段值(编辑器的 bootstrap 读),响应 `EditSnapshotResponse`(wave 181 起是**唯一**的 snapshot 面) |
| `listEditProposalsUser` | `GET /user/catalog/edit/proposals?entity_type=&entity_id=&status=&limit=&mine=` | 提案列表:`mine=true` 是**本人**的提交史,`mine` 缺省是**审核队列**;响应 = S2S `EditProposalListResponse` 逐字相同 |
| `listCatalogWorkCoversUser` | `GET /user/catalog/works/{id}/covers` | 一部作品的封面票数,每张带**本令牌用户**是否投过(`{work_id, covers:[{id, image_hash, vote_count, voted}]}`) |
| `getCatalogWorkByIDUser` | `GET /user/catalog/works/{id}?include_hidden=` | 按 catalog id 读一部作品(与 S2S work-detail 同一响应束,wave R2b 起);`include_hidden=true` 时 `releases[]` 含编辑者隐藏的行并逐行标 `hidden: true`——这是隐藏 release 唯一的枚举面(解除隐藏的入口) |
| — | `POST /user/catalog/edit/images`(multipart:`file`、`preset`) | 编辑面图片上传;**不在 spec 里**(multipart 不入 Huma 面,见下) |

- **`mine` 与队列是一条路径上的两份权威**,这是本波唯一的新规则:
  - **`mine=true`** = 本人的提交史,除网关链外**不需要任何权限**——问的是自己的事。`proposer_uid` **不是参数**(布尔正是为了让它指不了别人),`site` 也**不是参数**(恒为令牌 client 的 `catalog_site`),故没有任何写法能翻到别人的或别租户的列表。
  - **`mine` 缺省** = **审核队列**,读到的是别人的开放工作(含 patch、提案人、决策备注),因此要求与 §4.2 裁决三件套(amend/merge/decline)**对该 `entity_type` 完全相同的审核权威**:令牌 roles 经该 family 的权限词表,由该 family spec 自己的逐字段 review 规则求值(实现上即 `SchemaProjection` 至少投影出一个 `can_review=true` 的字段——**没有另造一个「队列专用」权限键**,否则就是第二套要和第一套保持同步的权威)。不足 → **403**(message 提示可改用 `mine=true`)。
  - `entity_id` 会一并参与求值,故 **kungal overlay 的 owner-review 通道在队列上同样成立**:条目的所有者可以读**自己那条**的队列(`entity_type` + `entity_id`),但读不了整个类型的队列——归属是关于一个实体的事实。
  - `entity_type` **必填**(权威是按类型解析的),`entity_id` 可选,`status` 词表与 `limit` 上限(200,默认 50)与 S2S `listEditProposals` 逐字相同,非法 status → **422**。
- **snapshot 与 covers 不设租户闸**,这是**读面的既有教义**(§4.3 之外的读一律跨站开放),写下来免得被当成漏检:
  - `snapshot` 投影的就是各公共读早已渲染的当前字段值,里面没有任何租户私有事实;闸住它只会挡住 kungal 的编辑者预览一部 letmoe 认领了的作品,却什么也没藏住。
  - 封面票数同理:票数与封面一样是公共的,令牌新增的只有「**我**投没投」这一条属于调用者自己的事实。
  - 两者仍在完整网关链之后(客户端绑定的 `catalog:edit` 令牌);作品不存在 → **404**,实体/未注册类型不存在 → **404**(引擎映射)。
- **封面票数读 = S2S 作品详情里那份票数投影加上「我」**:同一个 `ReadService.WorkByID` + `ReadService.CoverVotes`(**一次批量查询**,永不逐封面查),同一套 advisory 语义(票**永不**改封面顺序、永不动编辑位 pin)。item 形状是详情里 `WorkCover` 的投影子集(`id` / `image_hash` / `vote_count`)再加 `voted`,因为要票数的 UI 手里已经有详情了。**`voted` 只在本面存在**(wave 181 起 S2S 详情连字段都没有了):它需要一个已验的观看者,而那正是本面唯一多出来的东西。
- **图片上传:上传者取自令牌,无 `actor_uid` 表单字段**(wave 181 起 S2S 孪生已删,本面是唯一的上传面)。其余与它当初**逐字相同**——同一份 preset 白名单(`galgame_banner` → `catalog_cover`、`galgame_screenshot` → `catalog_screenshot`)、同一个 catalog 站身份(故日常 refping 照旧保活)、同一套上游限额与错误映射(配额/审核拒绝 → **400**,上游失败 → **502**,未配置图床 client → **503**)。上传者 `first_uploader_sub` 取自**令牌 `id`**;旧 S2S 孪生的 `actor_uid` 字段即使带上也**被忽略**。
  - **它不进 OpenAPI**:body 是 multipart,故是普通 Fiber 路由而非 Huma op。契约就写在这里。
- 其余三条 op 与 §4.1-4.3 同在 `docs/catalog/openapi.yaml`,tag `catalog-user`。

## 4.5 开放面的审核队列视图(wave 186a)

审核队列不止在员工面和用户面各有一份:**开放 API 的 works 列表**也开了一扇按租户钉死的队列门 —— `GET /v1/catalog/works?status=pending`。它在本目录出现,是因为它用的是**本域的权限与租户模型**,虽然端点住在开放面的 spec(`public-openapi.yaml`)里。

- **词面**:`status=live|pending`,缺省 `live` = 今天的公开人口,**与本波前逐字节一致**。`pending` 选的是 `claim_state=pending`(`ClaimLifecycleService.PendingClaims` 读的同一列),**不是**注册行的 status 值 —— 注册行 status 轴 `live|stub|merged` 里根本没有审核态,投稿铸造出来就是 `live`。
- **双凭据**:机器键走 `X-API-Key`,`Authorization` 留给**审核员本人**的访问令牌(`middleware.OptionalJWT`,永不拦截,故既有调用方零影响)。
- **四道 403**:无用户身份 / 令牌未绑 client 或 client 未绑 `catalog_site` / **第三方应用**(`owner_user_id` 非空)/ roles 不持 `catalog.claim.review`。
- **租户钉死**:队列只出该 client 的 `catalog_site`(与 §4 的 `userActor` 同一套推导);`site=` 指名别人的站是 403,**不是**静默改回自己。平台级队列仍只在 admin 面。
- **拒绝永远响亮**:绝不静默降级回 live 集合 —— 审核员拿到空页会判定队列是空的。

契约与全部条款见 [developer-platform/02 §3.2.9](../developer-platform/02-public-api.md)。

## 4.6 游戏时长上报面(开放 API,`/v1/playtime`)

**它不在本节其余四张脸的家族里** —— 它住在开放 API 面(`/v1/*`),契约在 `public-openapi.yaml`,开发者在**开发者门户**里就能读到。放在这份文档里,是因为它写的表(`catalog_user_playtime`)和它喂的聚合(`catalog_work_playtime` 的 `nextmoe` 源)都是 catalog 的。

### 凭证:只有用户令牌,没有 API key

开放 API 的其余每一扇门都以机器 key 开路,这一扇不是,理由是**一个凭证已经够了**:

> 绑定 client 的用户访问令牌本身就带齐两个身份 —— `id` 说是哪个人,`client_id` 说是哪个应用。

再加一把 API key,证明的是已经证明过的事,代价却是**同一个 scope 词写在两个注册表里**(`developer_api_keys.scopes` 和 `oauth_clients.allowed_scopes`),两处授予、两处撤销。于是必然出现「key 有而 client 没有」的工单,而排查的人要先知道存在两套 scope。所以:一个凭证、一处吊销。`playtime:read` / `playtime:write` **不再判定**——任何已开通用户登录的应用都可以调这张脸。

链是 `JWTAuth → PlaytimeGate`:
1. 令牌指名一个用户(时长是某个人的);
2. 令牌**绑定 client**(应用 id 是主键第三段,也是剔除坏应用的把手;`/auth/login` 的一等会话令牌无 `client_id`,在此被拒);
3. 按 (应用, 用户) 限流(120 次/分)。限流器故障时**放行** —— 它是防跑飞的护栏,不是授权判定,Redis 抖一下不该让所有人记不了时长。

**不要求 `catalog_site` 绑定**:catalog 三张写面从它解析租户,因为它们写的每行都归属某个产品站;而时长归属的是用户与应用。要求 catalog 租户等于把这张脸存在的意义全挡在门外。

### 开发者怎么拿到能登录的应用

在**开发者门户自助注册**,不进 admin.nextmoe.dev。建 app 时带上 `user_login`:

```json
{ "name": "Kurumi", "user_login": {
    "redirect_uris": ["http://127.0.0.1:53682/callback"],
    "scopes": ["openid", "profile"] } }
```

后端据此把 app 置为 `is_public` + `grants: [authorization_code, refresh_token]`,并把申请的 scope 写进 `allowed_scopes`。四道护栏见 [developer-platform/05](../developer-platform/05-developer-portal.md) 的自助登录一节:回调白名单(仅 `https://` 与 `127.0.0.1`/`[::1]` 环回)、强制 PKCE、保留名拒绝、第三方姿态。桌面管理器走 RFC 8252 原生应用模式,环回回调**按端口无关匹配**(端口是运行时才知道的)。

### 端点

| op | 路径 |
|---|---|
| `reportPlaytime` | `PUT /v1/playtime/works/{workID}` |
| `reportPlaytimeByRef` | `PUT /v1/playtime/by-ref/{source}/{externalID}` |
| `reportPlaytimeBatch` | `POST /v1/playtime/batch`(≤200,逐条成败) |
| `listOwnPlaytime` | `GET /v1/playtime/mine?updated_since=` |
| `getOwnPlaytimeForWork` | `GET /v1/playtime/works/{workID}` |

`by-ref` 决定这张脸**能不能被接入**:管理器手上是 VNDB id 或 DLsite workno,从来不是我们的 work id;强制要 work id 等于把映射问题推给每一个应用作者。**只有 exact 锚解析** —— probable 链接是线索不是身份,拿它归属别人的时长就是记到错的游戏上。

### 数据模型三条(细节见 `model.CatalogUserPlaytime` 类型注释)

1. **粒度 = (user, work, client)**。两个管理器是对同一件事的两次独立测量,谁也不权威。分开存意味着写入时不必猜谁覆盖谁,也意味着乱报的应用**可以按名字整体剔除**。**无 site 列** —— 对第三方启动器它永远是空的,而恒空的列是 schema 一直在问、没人回答的问题。
2. **上报绝对总量,永不是增量**。增量在每次重试上重复计数、在每次重装上归零。重发同一个数字是 no-op,这是本面可以无脑重试的根据。
3. **一人在一部作品上的时长 = 各 client 的 MAX,不是 SUM**。两个管理器盯同一个存档不是两周目。

### 公开聚合

由 `cmd/aggregate-user-playtime` 离线产出,**没有任何用户写入能同步移动已发布的数字**。只统计 `finished`;先按用户把各 client 折成 MAX 再对用户取中位数(`percentile_disc`,真实观测值,与 vndb / erogamescape 同语义);**少于 3 个完成者不发布且删行**(用户 2026-08-09 裁定)—— 两个人定义的中位数比没有更坏,跌破阈值必须停发而不是冻结。计入还要过 `minutes ∈ [10, 60000]`。**这些闸只管投票权不管存储**:用户自己的行原样存、原样读得回来。

## 5. 鉴权形态

- **S2S face(`/api/v1/catalog/*`)**:`Authorization: Basic <b64(client_id:client_secret)>`,对 `oauth_clients` 注册表校验。任何有效一等 client 可**认证**;但——
- **写路径 per-client site 绑定**:`oauth_clients.catalog_site`(可空 text,size 64,无唯一约束——一站可多 client)。`POST /catalog/works/claim` 要求认证 client 的 `catalog_site` **非空**且 **== 请求体 `site`**,否则 **403**(未绑定或站点不匹配的信息写在 message)。未绑定的 client 根本不能 claim。**只读端点(resolve / redirects / by-anchor / credits / search)不受此限。** `site` 值即租户键(写入 `catalog_work.site`),**无白名单/注册表**——合法性只由「client 绑定值 == 请求 site」把关;新增消费站 = 给其 client 设 `catalog_site`,别无它步。
- **消费站开通(SQL,无管理 UI)**:直接设 `oauth_clients.catalog_site`。
  - galgame wiki(第一消费站):`UPDATE oauth_clients SET catalog_site='galgame_wiki' WHERE image_site_key='galgame_wiki' AND id <> 'galgame-wiki-admin';`
  - **letmoe(第二消费站,同人为主)**:`UPDATE oauth_clients SET catalog_site='letmoe' WHERE <letmoe client 定位>;`(dev = 本地主库执行即可复现;**prod = 用户 ops**,随 letmoe 上线 runbook 同批,核验 `SELECT id,catalog_site FROM oauth_clients WHERE catalog_site='letmoe'` 命中 letmoe 机密 client)。
- **admin face(`/api/v1/admin/catalog/*`)**:Bearer JWT(accept-both verifier)+ **ren 角色(超管专属)**,与 site 绑定列无关;wave 187b 起还要过 **client 闸**——令牌若签发自第三方应用(`oauth_clients.owner_user_id` 非空)一律 403,先于权限判定(空 `client_id` 的第一方会话令牌放行,见 §4.2 的缺口条)。
- **user face(`/api/v1/user/catalog/*`,wave 176)**:Bearer **用户**访问令牌(同一 accept-both verifier)+ `catalog:edit` scope + **client 绑定**。这里 `oauth_clients.catalog_site` 的用法与 S2S 写面**不同**:S2S 校验「绑定值 == 请求体 site」,user face **根本不收 site**——绑定值**就是**写入的租户。因此新增消费站的动作仍是同一条(给其 client 设 `catalog_site`),但一等登录令牌(无 `client_id`)在本面**永远**拿不到租户,只能走 OAuth 授权码流取得 client 绑定令牌。详见 §4。
- **编辑引擎提案桥面已死**(曾为 09-open-api-phase2 06b 的过渡参考):`/internal/edit/*`(scope `galgame:propose`、计量 face `galgame_internal_propose`)随 galgame 面退役**整体移除**——今天这些路径连 410 都不是,就是 404。编辑引擎的 S2S 面(`/api/v1/catalog/edit/*`)在 wave 181/185 两轮收缩后只剩 list(第三人称统计读)/revisions/diff **三条只读**,写与裁决全在用户面。「第三方实际开放」已于 **wave R3(2026-08-17)裁定并落地**:不复活桥面、不开 /v1 编辑路由,第三方经自助 user_login 申请 `catalog:edit` 用户 scope 后走用户面(见 §4 的第三方准入段)。
- **playtime face(`/v1/playtime/*`)**:Bearer 用户访问令牌 + **client 绑定**,**无 API key**,**不要求** `playtime:read` / `playtime:write`(任何已开通用户登录的应用都可以调),**不要求** `catalog_site`。详见 §4.6。
- **public face(`/v1/catalog/*`)**:开发者平台 API key(`Authorization: Bearer nm_live_…`)+ `catalog:read`,过 `internal/platform/devapi` 的中间件链(凭据解析 → 用量记账 → 限流 → 日配额 → scope)。**唯一例外 `GET /v1/catalog/stats` 无鉴权**:它只发布「目录有多大」的聚合数(LIVE works × medium + 身份族存量,无 `nsfw` 参数、人人同一份 payload),是公开站与开发者门户在任何人持 key 之前就要渲染的那几个数字;不计量、不限流,由 `Cache-Control: s-maxage=3600` 兜住。**该路由裸挂在 app 上且必须排在 `/v1/catalog` 分组之前**——Fiber 按注册序匹配、分组的 handler 即前缀 `Use`,挪到分组之后路由仍然通,只是悄悄退回 key 闸后面(钉子:`cmd/catalog/public_stats_route_test.go`,带反序对照)。
- **public v2 face(`/v2/catalog/*`,2026-09-06 起双凭证)**:`Authorization: Bearer` 收**应用密钥 `nmk_live_…`**(带 `catalog:read`)**或**带 `catalog:read` 的**用户访问令牌**,二选一,一条请求只带一个(refs/api-v2 D1)。闸按那一个值的前缀分道:`nmk_` 走密钥链,其余走用户令牌链;失败不互相兜底。用户令牌按 `u<uid>` 计量(该用户跨所有已授权应用共池,默认 100/分钟 + 10000/UTC 日),密钥仍按 tier 计量。**三条例外仍只收密钥**:`GET /v2/catalog/claim-events`(额外要运营授予的 `claim_events:read`)、`GET /v2/folders/holders`(额外要运营授予的 `folder_holders:read`,2026-09-09 起)与整个 `/v2/store`。前两条的拒绝码**故意不同**:claim-events 对用户令牌回 401 `INVALID_CREDENTIAL`,holders 回 403 `SCOPE_REQUIRED`——那把令牌本身是合法的,401 会把客户端推进一轮又一轮刷新一把没过期的令牌。原生桌面应用的完整接入见 [developer-platform/10 §18](../developer-platform/10-native-app-integration.md)。
- **v2 用户面(`/v2/me/*`、`/v2/moderation/*`)**:Bearer 用户访问令牌。这两族**默认按人判权**,不按「应用被同意了什么」判权;两组例外各有自己的 scope,因为它们动的都不是这个人独占的东西:
  - **编辑面**——`/v2/me/proposals`、`/v2/me/claims`、`/v2/me/cover-votes`、`/v2/me/edit-images`、`/v2/moderation/{proposals,claims,snapshots,reverts}`——要 **`catalog:edit`**,缺则 `403 SCOPE_REQUIRED`。名单落在 `apiv2/handler/identity.go` 的 `editingPlanePrefixes`,**OpenAPI 里那句 scope 说明由同一张表生成**,所以闸与文档不会各说各话;`TestEveryMeAndModerationPathDeclaresItsScope` 会在新路径未归类时红。
  - **收藏夹**——`/v2/me/folders`——要 `folder:read` / `folder:write`(写 scope 同时给读)。`GET /v2/me/folders/holdings`(批量成员关系)在这个前缀下,所以吃同一条闸。
  - **能力面**——`GET /v2/me/edit-capabilities/{object}`——回答「**我**能对这个族做什么」:逐字段的 `can_propose` / `can_review` / `would_automerge` / `locked`,由编辑引擎实算。它**不能**并进 `/v2/catalog/schemas/{object}`:那个面免凭证,而 **B34** 禁止同一个 URL 按凭证返回不同字段集(依据 A10),所以是两个 URL,按 `fields[].key` 关联,能力面不重复 schema 已经发过的形状元数据。站点开放 owner 通道时要带 `entity_id=`,否则 `would_automerge` 回答的是类型级问题。此前 v2 没有这个面,`SchemaProjection` 是死代码,下游只能把 `can_review` 写死成 true,于是给每个登录用户都画了审核按钮而服务端一律 403。
  - `/v2/moderation/folders`、`/v2/moderation/users/{uid}/folders` **不要** `catalog:edit`:它们属收藏夹域,按权限判;拿目录编辑的同意去开收藏夹审核的门是错配。
  - **历史与迁移**:v1 的 `UserGate` 在整个 `/api/v1/user/catalog` 前缀上要这条 scope,wave R3 删 v1 时没搬到 v2,`/v2/me/proposals` 因此接受只带 `openid profile` 的令牌并把编辑合入,直到 2026-09-08 补回。**补闸之前必须先补存量会话**——grant 在 `/authorize` 当场定死,refresh 永远加不宽 scope:当时 109,320 条存活会话里 61,502 条(69%)没有 `catalog:edit`,直接上闸就是全站编辑 403 且 90 天不自愈。补法是对**首方 auto_consent 且已 `catalog_site` 绑定、`allowed_scopes` 已含该 scope**的 client 的会话 `UPDATE sessions.scope`,第三方会话一律不动。
- `GET /openapi.json`(S2S spec)、`GET /healthz` 无鉴权。

## 6. 生成 spec

> wave R3(2026-08-27)只剩两个目标:S2S 的 `-catalog` 与公开 v1 的 `-catalog-public` 两个 flag 已随面删除,`docs/catalog/openapi.yaml` 与 `public-openapi.yaml` 两份 spec 亦然。

- admin:`go run ./cmd/gen-openapi -catalog-admin -o docs/catalog/admin-openapi.yaml`。
- v2(Huma 真路由,88 op):`go run ./cmd/gen-openapi -catalog-v2 -o docs/catalog/v2-openapi.yaml`——CI 的 freeze 门(test.yml)冻着它,另有 oasdiff 破坏门(spec-breaking.yml)与 developer 门户 docs-model 门(docs-model.yml)两个下游。
- 契约以生成的 spec 为准(Huma code-first,DTO 即契约);本 markdown 是语义说明。

## 7. 运维注记

- **schema 迁移**:`cmd/migrate catalog` 是 `kun_catalog` 的**唯一** schema 入口,幂等(AutoMigrate + `IF NOT EXISTS` 原始 SQL + 存在性守卫 seed)。生产随部署自动跑(compose `migrate-catalog` gate,catalog 服务 `depends_on: service_completed_successfully`);catalog 服务自身**不跑迁移**,只连接 + 就绪检查。
- ⚠️ **导入类 cmd 不随部署自动跑**:`reconcile-galgame-works` / `import-*` / `reindex-catalog` 等是手动运维工具(经 `tools` 镜像 + env-file),**部署不会触发**。跑完批量导入后需**手动** `reindex-catalog` 重建搜索索引(批量脚本不走写穿钩子)。
- **主库变更提醒**:`oauth_clients.catalog_site` 列落在**主库 `kun_galgame_infra`**(经 `cmd/migrate` AutoMigrate)——见工程侧变更时的迁移铁则。
- **服务拓扑**:catalog 内网端口 9281,产品后端经 `http://catalog:9281` 走 dokploy-network(无公开域名);web(oauth admin 前端)SSR 经 `NUXT_CATALOG_API_BASE_SSR=http://catalog:9281/api/v1`。

## 8. 展示轴镜像:`/v2/catalog/changes` 是镜像信道

产品站会把 catalog 的编辑展示判定(§1 的 `content_limit`)缓存成自己的一列,好在 SQL 里过滤列表页。此前**没有任何 feed 被声明**承载这个字段——论坛的代码里留着原话:「There is no feed for that field — the claim-event feed carries claim state only — so a full sweep is the only way an editor's flip reaches the local lists.」于是论坛与 moyu 各自退回夜间全量扫(约 300 次请求扫 ~7.8k 行),编辑翻一个标志最坏要等一整夜才到本地列表。**信道其实一直在**:`GET /v2/catalog/changes` 按 `(updated_at, id)` 升序翻 `catalog_work`。本节把它声明成契约。

**不变量**:凡改动作品的 **claim 状态**、**编辑展示轴**(`display_nsfw` / `content_rating`)或**作品存在性**的写路径,都会 bump `catalog_work.updated_at`,因而必在本 feed 现身。当前这些写路径是:编辑面 `applyWorkColumn`、claim 生命周期八动作、周跑 `releasemeta` 评级泳道、合并的认领转移与来源退役、导入(只 INSERT,新行自带新时间戳),以及子资源写路径统一走的 `repository.TouchWorks`。**其余字段(封面/标签/标题/简介/评分)尽力而为**——它们多数也会 touch 作品,但只有上面三项是承诺。

**判定配方**(与 §1 同一条,服务端语义源 `model.DisplayLimitKey(site, product_work_id, display_nsfw, content_rating)`):

```
content_limit = claimed_by.content_limit          若 claimed_by 块存在(仅已认领作品渲染)
              = content_rating == "r18" ? nsfw : sfw   否则
```

**镜像回路**:

1. **冷启动**:从空游标翻本 feed。它按「最旧更新优先」枚举**整个人口**,所以第一次翻完就是一次全量清点——不需要另一个「列全部 id」的面。每页拿到的 id 以 **≤100 一批**打 `/v2/catalog/works?ids=`,**两道闸全开**(`nsfw=true`,且**不传** `content_limit`),否则被闸掉的作品会在本地留成空洞。
2. **稳态**:存下 `next_cursor`,按自己的节奏轮询;游标是不透明串,原样回传。注意 feed 有 5 秒的在途事务水位,刚写的行会晚一拍出现——这不是丢失。
3. **`gone: true`**:该 id 已离开公开人口(被合并,或不再被服务),删掉本地行。
4. **被合并的 id** 同时出现在 `/v2/catalog/redirects`,那里给出接替它的规范 id——有 redirect 就**重指**而不是删除。

**本地谓词写法**:缓存列用可空列,`NULL` 表示「尚未同步」并且**放行**,例如 `WHERE content_limit IS NULL OR content_limit = 'sfw'`。冷启动期间未水合的行因此照常可见而不是整站空白;真正的权威始终是水合时 catalog 自己的闸,本地列只是列表页的快筛。
