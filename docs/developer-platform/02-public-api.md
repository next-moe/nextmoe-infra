# 公开 API 面与端点(精选 + 版本化)

> 本文承载 §3.1 原则、§3.2 v1 端点清单、§3.5 稳定性承诺与演进条款、§10 OpenAPI 策略。设计与命名约定见 [01-design.md](./01-design.md);§3.3 跨面互链、§3.4 面挂载模型见 [01 §3](./01-design.md)。

> 🪦 **v1 已于 2026-08-27(wave R3)整面退役。** `/v1/catalog`、`/v1/news`、`/v1/store`、`/v1/playtime`、`/api/v1/catalog`、`/api/v1/user/catalog` 六个前缀现在一律返回 `410 Gone`(仓内标准信封 `code=11`,带 `Link: <https://api.nextmoe.dev/v2>; rel="successor-version"`),服务它们的代码、spec 与门户页面已一并删除。**下文凡把某条 v1 路径写成在产的段落均为历史记录**;唯一在产的公开面是 `/v2`,契约见 [../catalog/v2-openapi.yaml](../catalog/v2-openapi.yaml) 与 [../catalog/v2-implementation-notes.md](../catalog/v2-implementation-notes.md)。

---

## 3. 公开 API 面与端点(精选 + 版本化)

### 3.1 原则

- **白名单暴露**:只把精选的只读端点放进 `api.nextmoe.dev/v1/…`;internal / admin / 写端点**永不**进入公开路由(物理上不挂到公开路由组)。**wave 207 收窄措辞**:被禁的是**注册表 / 编辑写面**——改注册行、认领、merge、审核队列这一族,它们至今一条都不在公开路由上。`/v1/playtime` 是**唯一**的公开写面,且写的**不是注册表**而是调用方自己的游玩记录:凭据是**用户自己的 Bearer 令牌**(不是机器 API key),一个用户只写得到自己那几行,见 §3.8。
- **URL 版本化** `/v1/`:一旦有了无法协调破坏性变更的外部开发者,版本化与弃用策略从"过早优化"变成"硬需求"。
- **弃用策略(v1)**:字段级弃用走 `Deprecation` / `Sunset` 响应头 + 门户公告 + 不少于 N 个月窗口。主版本退役从 **v2 GA** 起算，不是从 `/v2` 上线起算。
- **`/v2` GA(2026-08-25,上线于 2026-08-23)**:`/v2` 挂在 `cmd/catalog` 上，契约由 `docs/catalog/v2-openapi.yaml` 从真实路由生成。**preview 期结束，破坏性变更不再豁免**：上一句「破坏必须升 v2」现在无条件适用，由 `spec-breaking.yml` 的 oasdiff 门(G12)执行。凭证向所有人开放：任何应用在控制台自助铸造 `nmk_live_` / `nmk_test_`（定长 37，CRC32 校验位），不需要申请，轮换存量 `nm_live_` 钥匙换回的也是 `nmk_`；存量 `nm_live_` 继续只对 `/v1` 有效，直至单独排期的 v1 sunset。spec `info.version 2.0.0`、`info.x-stability: stable`，门户 preview 横幅已撤。v1 的 `Deprecation`/`Sunset` 自本日起算。客户端契约三句见门户 [/docs/design](https://developer.nextmoe.dev/docs/design)。
- **路径命名空间 = 面**:`/v1/catalog/*`、`/v1/galgame/*`,未来 `/v1/manga/*` 等。galgame 的领域词表(officials/tags/engines/series)全部收进 `/v1/galgame/` 之下,给未来媒介留干净的顶层命名空间。**(galgame 面已于 2026-07-30 摘牌,整族落 `410 Gone`;该命名空间原则不变。)wave 207 更正**:在产的面现在是**两个**——`/v1/catalog`(**37 op**,wave 213 波 1 加了十二条作品子资源;机器 API key:`X-API-Key`,或 `Authorization: Bearer nm_live_…`)与 `/v1/playtime`(5 op,**用户**访问令牌:`Authorization: Bearer <OAuth access token>`,见 §3.8),冻结 spec 合计 42 op。它们同进程(`cmd/catalog`)挂载但**凭据体系不同**,不要写成一句「公开面用 API key」。
- **公开投影与内部契约解耦**:公开面是从既有 Huma spec 精选出的**独立 spec**;内部 S2S/站点契约继续自由演进,互不牵制。

### 3.2 v1 端点清单(草案)

> §3.3 跨面互链、§3.4 面挂载模型见 [01 §3](./01-design.md)。

**galgame 面**(后端 = `cmd/catalog` 承载的 galgame 面(W3 起;撰文时为 `cmd/galgame`),内容真源):
>
> **🪦 已摘牌(wave 146,2026-07-30)——本节整表为历史记录。**
> doc 106 于 2026-07-28 把 `/v1/galgame/*` 判为 kungal 产品读面(wiki body 投影)而**非** canonical 数据 API,开 90 天绞杀窗(Sunset 2026-10-31)。窗口因证据充分被用户令**提前执行**:12 小时窗内 `/v1/catalog` 146,236 次 vs `/v1/galgame` 1 次(裸路径扫描器,401)。
> **下列 26 条 op 已整体摘除**:`/v1/galgame` 与 `/v1/galgame/*` 现返回 **`410 Gone`**,信封 `{code: 11, message}`,message 指向后继面 `/v1/catalog` 与 <https://developer.nextmoe.dev/docs/catalog>;响应保留 `Link: rel="successor-version"`,`Deprecation` / `Sunset` 头随面一同退役(它们宣告的是**未来**退役,退役后即为谎言)。410 而非 404 是刻意的:区分「已退役」与「路径手误」。
> 冻结 spec `docs/galgame_wiki/public-openapi.yaml` 已删除,`cmd/gen-openapi -galgame-public` 目标同退,三条 CI 契约门(spec-breaking / test 的 code→spec 冻结 / openapi-types)的清单同批清理,开发者门户 `docs-model.ts` 已重生成(46 → 20 op)。
> **端点/字段迁移映射见 `refs/proj/106` §6;canonical 面见下方「catalog 面」。**

| ~~公开端点(`/v1`)~~ 已 410 | 映射内部 | scope | 说明 |
|---|---|---|---|
| `GET /v1/galgame` | `GET /galgame`(List) | `galgame:read` | 分页/排序/搜索/发售范围;**游标分页**(见 [04 §8 备注](./04-platform-internals.md)) |
| `GET /v1/galgame/{id}` | `GET /galgame/:gid` | `galgame:read` | 详情;响应携带 `catalog_work_id`(跨面互链,见 [01 §3.3](./01-design.md)) |
| `GET /v1/galgame/batch` | `GET /galgame/batch` | `galgame:read` | 批量(brief/detail) |
| `GET /v1/galgame/search` | `GET /galgame/search` | `galgame:read` | Meilisearch |
| `GET /v1/galgame/calendar*` | calendar 三件套 | `galgame:read` | 已有 ETag/缓存,直接复用 |
| `GET /v1/galgame/officials` `…/{id}` `…/{id}/galgames` | official List/Get/members | `galgame:read` | 会社目录 + 成员 |
| `GET /v1/galgame/tags` `…/{id}` `…/{id}/galgames` | tag | `galgame:read` | |
| `GET /v1/galgame/engines` / `GET /v1/galgame/series` … | engine/series | `galgame:read` | |
| `GET /v1/galgame/changes` | (新增,updated 时间戳 keyset) | `galgame:read` | **变更流**(doc 19 D5,Phase 1):增量同步游标,管理器免全量重爬 |
| (Phase 3)`POST /v1/galgame/{id}/submit` 等 | 投稿/PR | `galgame:submit` | 需 OAuth2 用户授权 |

**catalog 面**(后端 = `cmd/catalog`,跨媒介身份/图谱真源):

| 公开端点(`/v1`) | scope | 说明 |
|---|---|---|
| `GET /v1/catalog/works/{id}` | `catalog:read` | 注册行:display_name / titles / medium / 分级 / 外部锚(来源白名单过滤,见 [06 §11](./06-security-compliance.md))/ **认领指针**(→ 内容面路由,见 [01 §3.3](./01-design.md))+ **全量聚合 facet**(wave 104 加法扩容:popularity/ratings/tags/playtimes/series/platforms/intros/covers/screenshots/characters/labels/releases——source 键归因、CDN 完整 URL、字符串词表);**R18 由调用方传参放行**:`nsfw=1` 出 r18 作品与 r18 关系端(works/lookup/names/characters/labels 同参;characters 另有 `spoilers=0-2` + sexual traits 随 nsfw),(该参数曾于 wave 213 波 3 起须凭证具备 NSFW 能力位,**能力位已于 2026-08-25 退役**,任何 key 传即放行,见 §3.2.10);缺省(不传 `nsfw`)隐藏与 Phase-1 逐字节一致,且与是哪把 key 无关;`updated` 恒在(doc 106);`releases[]` 每行带 `id`+`refs[]`,`tags[]` 每行带 canonical `canonical_id/tier/kind`(doc 106,未映射省略)。**A2-1e 加法**:`created`(RFC3339,注册行**进入 catalog 的时刻**——既不是发售日也不是产品侧创建时间)、`engines[]`(`{id,name}`,恒出空为 `[]`)、`links[]`(非身份外链,见 §3.2.2)、`labels[]` 每行 `lang`、`tags[]` 的**安全轴** `spoiler`/`sexual` + `spoilers=0\|1\|2` 参数(见 §3.2.3)。**A2-R1 修复**:`titles[]` 对**认领作品**来自 wiki 桥(四名称列 + 别名,见 §3.2.5)——此前认领作品的中文名/别名整体缺席;`labels[]`/`engines[]` 每行恒带 `work_count`、`tags[]` 映射行带 `work_count`(nsfw 感知,见 §3.2.6)。**wave 200 加法**:`releases[].labels[]`——**这一版次**的公司(谁开发、谁发行),形状与作品级 `labels[]` 逐字同(一家公司一条 + `kinds[]` + `work_count`);**恒在**,该版次无已知公司时为 `[]`(同 `refs[]` 之规)。Switch 移植的发行商与英文版的发行商是**两个版次**的事实,压平到作品级后无从归因,这个键即它们各自的归宿。**wave 207 更正 · `include=` 重块**:本行的 `include=`(逗号分隔词表 `relations,credits`,**缺省两块皆不出**,未知 token 静默忽略——§3.5 条款 2)是这两块的**唯一入口**;本表此前把 `works/{id}/credits` 与 `works/{id}/relations` 列作两条独立端点,**它们从未存在**(冻结 spec 里没有这两个 path),本波删除。`include=credits` 出 `credits[]`:按 role 分组 `{role_key, role_name, credits[]}`(role 名取中文 → 日文 → role_key 回退),每条署名 `{id, display_name, lang, latin, localized, character_id, character, label_id, label, source}`(**🔴 wave 209**:`name`→`display_name` + 补 `localized{}`;同波作品详情的 `characters[]` 花名册行及其 `voices[]` 同此改形并补 `localized{}`,`relations[].work` 与 `series_siblings[]` 的作品 brief 补作品名字块,自 wave 212 波 B 起该块是 `latin` + `localized{}`)——`id` 是 credit_name id,可直接喂 `names/{id}`;`label_id` 见 §3.2.1 ②b,`source` 见 §3.2.1 ②d。`include=relations` 出 `relations[]`:`{relation_type, phrase, work}`,`work` 为作品 brief(带 `claimed_by`),一条边双向渲染;**`nsfw=0` 时 r18 关系端整条丢弃**而非留空壳。**wave 207 补文 · `cover_slots`**:恒出键 `cover_slots{portrait, banner}`(该调用方一张可用封面都没有时整块 `null`),每槽 `{url, width, height, thumbhash, sexual, violence, source}` 或 `null`——与 works 列表 `include=covers` 的两槽**同一个挑选器**,槽位判据(**kind × 尺寸两道,不是「非竖版即 banner」**)见 §3.2.1 ④。**wave 213 波 3 加法 · `fields=` 稀疏投影**:逗号分隔的**本响应顶层键**白名单(不传 = 全量,与本波前逐字节一致)。`id` **恒在**,点不点名都出;未知 token **静默忽略、永不 400**(§3.5 条款 2);**只裁不改形**——留下的键其值与不投影时**逐字节相同**,绝不重塑键内部;**在 `include=` 之后生效**,故 `fields=relations` 而不传 `include=relations` **不会**把块展开,那个键只是缺席(有效输出 = include 决定的形状 ∩ fields 选择 + `id`)。派生键会加载它依赖的东西(`release_date` 与 `refs` 都要读 release 行、`cover_slots` 要读 covers、`claimed_by` 要读展示轴),但**依赖自己的键仍只在被点名时才出现**。**这是查询闸,不只是序列化闸**:`works/{id}?fields=display_name,id` 只跑 **1 条**查询(作品行本身),同一条请求不投影时在测试夹具上是 **24 条**——已退役的 galgame 面只裁 marshal 之后的字节、一次查询都省不掉,那正是本波要翻的案。服务端对**顺序与重复不敏感**,但请**按字母序书写 token**:CDN 按原始 URL 做键,同一选择的两种写法是两条缓存记录 |
| `GET /v1/catalog/works/{id}/covers` · `…/screenshots` · `…/tags` · `…/characters` · `…/credits` · `…/releases` · `…/intros` · `…/ratings` · `…/relations` · `…/series` · `…/links` · `…/engines` | `catalog:read` | **作品子资源(wave 213 波 1,十二条新端点,纯加法)**:上一行那份注册行有 29-31 个顶层键、其中 18 个是数组,tags/credits/characters/intros/releases/screenshots/covers 七块合计占 79.5% 的字节,而身份内核只占 7-9%——**只想要这部作品封面的下游站点,不该为其余的付账**。每条端点是**同一个块的独立地址**,不是它的第二种表示:item 用的就是父块的 DTO(`PublicCover`/`PublicTag`/`PublicRosterCharacter`/`PublicCreditGroup`/…),同 schema、同顺序、同挑选与压制规则,两面**共用同一个 mapper**,并有一条逐字段对拍的测试把它们钉在一起。**可见性 = 父面逐字**(LIVE galgame + `nsfw=` 同参、同 404):`works/{id}` 404 的作品,每一条子资源同样 404。**分页**:`limit` 1-100(缺省 100)+ `offset`,`next_offset` **只在确实还有行时出现**——这一面知道块的真实长度,所以它的缺席意味着「没有了」,不是「也许还有」。**有意的不对称,不是疏忽**:`works/{id}` 内嵌的数组**仍然不封顶**,给一个已发布字段加上限不是加性变更(§3.5 条款 1),统一留给 `/v2`;两面只差在边界,别的一处不差。逐条注意:`credits` 按**署名行**分页而不是按 role 组(否则单页仍然无界),跨页的 role 在两页各出一次自己的组,拼页 = 按 `role_key` 合并;`characters` 与父块一样**不设 spoiler 天花板**,每行自带 `spoiler` 交给调用方裁,故这条不收 `spoilers=`(只有 `tags` 收);`ratings` 是作品详情投影,带 `distribution`/`stats`;`relations` 不需要 `include=` 就能拿到,`nsfw=0` 时 r18 关系端整条丢弃且 `next_offset` 数的是丢完之后的行。**缓存分两档,判据是「编辑面有没有写这一块的字段」**:`ratings`/`relations` 没有编辑字段(唯一的写者是夜间评分车道与 VNDB 关系导入),`s-maxage=1800`;其余十条与 `works/{id}` 同档 `s-maxage=300`,免得编辑者的改动在子资源上比在父面上晚到。**成本**:每条只跑自己那一块的查询(实测 2-6 条,404 只花 1 条),而 `works/{id}` 一次约 30 条——`include=` 从来只是序列化开关,从不省一次查询,这十二条端点才是 |
| `GET /v1/catalog/works` | `catalog:read` | **作品浏览/列表(doc 106 G1,keyset)**:过滤 `content_rating`/`claimed`/`label_id`/`tag_id`(canonical)/`series_id`/**`engine_id`(A2-1b 第九过滤器,经 `catalog_work_engine`)**/`platform`/`released_after\|before`/`ids`(≤100);`sort=id\|updated`;item = 轻 brief(+`release_date`/`olang`/`cover` 单图/`updated`);`nsfw` 同参(能力位已于 2026-08-25 退役,任何 key 传即放行,见 §3.2.10);`next_cursor` 末页 null。**`include=` 富 brief 块(A2-1a 加法波)**:词表 `names,intros,labels,ratings,covers`(逗号分隔,**未知 token 静默忽略**,§3.5 条款 2);每块按页内 work id **批量加载**(无 N+1),未点名即整块缺席——**缺省(无 `include=`)响应与本波前逐字节相同**。`names` 出 `latin` + `localized{}`、`intros` 出与详情面逐字相同的 `[{lang, intro, source, machine}]`(见 §3.2.1 ①),`labels`/`ratings` 与详情面同形同口径(评分保持源原生分制,不聚合;**wave 200**:`labels` 一家公司一条,身份全集在 `kinds[]`),`covers` 出 `{portrait, banner}` 两槽、每槽带 `width/height/thumbhash`(见 §3.2.1);`ids=` + `include=` 即批量富取(两梯队的 batch 替代面)。**A2-1e**:`include=` 词表加 `refs`(该作品的 **exact 身份锚**,与详情面 `refs[]` 同构——work 级 ∪ release 级去重,exact-only 红线不破),`tag_id` 收**逗号分隔多值 AND**(≤10,见下)。**A2-R1 修复**:标题对**认领作品**来自 wiki 桥(见 §3.2.5);`labels` 块每行恒带 `work_count`(与详情面同数,见 §3.2.6)。**A2-R4 加法**:`claim_state=`(封闭词表 `none\|live\|draft\|pending\|declined\|hidden`,逗号分隔 IN 语义,非法 token 400,不传=不闸)——**与 `works/search` 同名参数逐字同义**,词条成员列表务必传 `claim_state=live` 以排除未发布/未认领行;与搜索面不同,这里是**读时库内谓词,改态立即生效**,见 §3.2.7。**A2-R5 加法**:`content_limit=`(封闭词表 `sfw|nsfw`,逗号分隔 IN 语义,非法 token 400,不传=不闸)——**编辑展示轴,不是年龄轴 `content_rating`**;做可索引面传 `content_limit=sfw`,见 §3.2.8。**186a 加法**:`status=`(封闭词表 `live|pending`,缺省 `live` = 与本波前逐字节一致)——`pending` 是**审核队列视图**,须**双凭据**(`X-API-Key` 机器键 + 审核员**本人**的 Bearer 令牌)且**按租户钉死**,不满足条件一律 **403**,见 §3.2.9。**wave 199 加法**:`label_rollup=1`(仅在 `label_id=` 同传时生效,单传静默忽略)——把 `label_id` **沿企业图向下扩一跳**到该厂牌的 imprint / subsidiary,即「控股会社自己名下不出版任何作品」的那张页面所需的口径;经子厂牌进来的每一行带 **`via_label{id, display_name, localized}`**(**🔴 wave 209**:`name`→`display_name` + 补 `localized{}`;会社自有作品不带),**必须渲染**——不说 `via <imprint>` 的上卷页等于把子厂牌的作品悄悄改挂到母公司名下。**不跟** `spawned`(分家出去的公司,目录是它自己的)与 `succeeded_by`(公司承继 = 两实体一箭头,wave 198 已裁);人口恰为 `labels/{id}` 的 `work_count + imprint_work_count`(两数不相交)。**wave 213 波 3 加法 · `fields=` 稀疏投影**:逗号分隔的**每个 item 的顶层键**白名单(不传 = 全量,与本波前逐字节一致);**信封不受影响**(`items`/`next_cursor`/`total`/`page`/`limit`/`facets` 永远原样)。`id` **恒在**;未知 token **静默忽略、永不 400**(§3.5 条款 2);**只裁不改形**;**在 `include=` 之后生效**——点名一个 include 门后的键(`intros`/`labels`/`ratings`/`covers`/`refs`/`latin`/`localized`)**不会**把它展开,两个参数都要给。它同样是**查询闸**:被 fields 裁掉的 include 块不再发查询,连今天无条件跑的三条(封面、展示轴、最早发售日)也随对应键一起关掉。服务端对顺序与重复不敏感,但请**按字母序书写 token**(CDN 按原始 URL 做键) |
| `GET /v1/catalog/changes` | `catalog:read` | **增量同步流(doc 106 G2,keyset)**:`{entity_type=work, cursor, limit}` → `[{entity_type, id, updated}]`;`next_cursor` 恒在(续轮询新行);**无 nsfw 门**(id+时间戳=身份非内容,详情跟查再门控)。**删除不经此流**——行离开 LIVE 集(软删/降级/退出 galgame 媒介)后只是从流中**静默消失**,不发 tombstone;**合并型消亡由 `GET /v1/catalog/redirects` 覆盖**(旧 id → canonical id),**镜像型消费者应周期性全量对账**(`works?sort=id` keyset 扫 id 全集,与本地镜像取差集即失效行)。`op` 字段登记为将来的加法扩展位(现不下发,消费端须按 §3.5 条款 2 忽略未知字段)。**流有意滞后 ~5 秒**(2026-07-28 cleanup 波):`updated_at` 是**语句时间**而非提交时间,不设滞后则长事务可能提交出一行 `updated_at` 已落在消费者水位之后的记录 → 该行被**永久跳过**;拒发 5 秒内的新行,使提交耗时 ≤5s 的在途事务不可能被漏掉 |
| `GET /v1/catalog/stats` | **无需凭据** | **注册表规模计数(149b,无参数)**:**本面唯一免凭据的端点**——匿名可调、不计量、不限流(带 key 调用亦可,闸不存在即 header 被忽略);它是公开站与开发者门户在任何人持 key 之前就要渲染的那几个数字,而载荷本身不含任何可渲染内容。裸挂在 app 上、排在 `/v1/catalog` 分组之前,见 [catalog/01 §5](../catalog/01-service-and-contract.md)。`works{total, by_medium[{medium_id, medium, count}]}` 只计 **LIVE 行**(`status=live`、未软删——stub / 已合并 / 软删行不属于目录),`total` = `by_medium` 之和,构造上不可能打架;`entities{labels, characters, credit_names, persons}` 为身份族存量(有软删列的三族只计未软删行;`catalog_credit_name` 无软删列,合并即改写而非立墓碑,故整表计)。**r18 作品计入**——这是不挂任何可渲染内容的聚合数,按 nsfw 拆分反而会公布 r18 人口规模,正是 nsfw 门要藏的东西;故本端点**无 `nsfw` 参数**,每个调用方拿到同一份 payload(`Cache-Control: s-maxage=3600`)。**内部仪表盘不上公开面**:审核队列水位、LLM 判定、锚 source × tier 交叉表、来源新鲜度、孤儿计数、claim 态矩阵是**运维遥测**(它们描述注册表如何被治理),留在 S2S `GET /api/v1/catalog/stats`(见 [catalog/01 §2.7](../catalog/01-service-and-contract.md)),两份 payload 各自独立演进 |
| `GET /v1/catalog/names/{id}`(+ `…/credits`) | `catalog:read` | 名义(credited identity;{id}=credit_name id,携 person_id+公开 sibling 名义)——**hidden 名义链接不出现在公开聚合**(既有可见性政策)。**wave 172 加法**:`photo_hash`(人物照片在图床的内容哈希,与作品封面 `image_hash` 同币种,**恒出**、空串=无照片)+ `gender` + `birth_y`/`birth_m`/`birth_d`(模糊生日,未记录则缺席);这五键**与 `person_id` 同一道可见性闸**——隐藏链接下一律不出,因为照片与生日是人物事实,公开它们等于泄露被隐藏的那条链接。**wave 175 加法**:`aliases`(**该名义自身**的书写变体,取自 `catalog_name_alias`;与厂牌 `aliases` 同形、去重、剔除名义自身、**恒出**、无别名为 `[]`;主要供给 = bangumi 中文名波的 zh-Hans 行;**🔴 wave 209 对象化** `{value, lang, kind, machine}`)。**wave 186 加法**:`links[]`(人物外链 `{source,url}`,供给=VNDB staff extlinks,官网/twitter/pixiv/ci-en 类型化 + 白名单站点 `web` 源完整 URL;身份空间站点 bgmtv/egs 恒不入;**随 `person_id` 同一道可见性闸**——hidden 链接下不出)。**不并入 sibling 名义的别名**:别名挂 credit_name(实体层铁律——身份在名义上),而各 sibling 自有 `/names/{id}` 记录携各自 `aliases`,并进来等于把一个身份的写法记到另一个名下。别名是**本名义的写法**、不是关于其背后人物的断言,故不受 link-visibility 闸约束。**wave 189 加法**:`intros[]` 由**双泳道**归并(每语言一条)——① **人物级** `catalog_person_intro`(catalog 原生行,自带 lang 列与行级 provenance,**随 `person_id` 同一道可见性闸**:传记是人物事实,隐藏链接下公开它等于泄露被隐藏的那条链接),② 既有 wave 108 的**名义级** bangumi 锚读时桥(per-name provenance,不构成人物身份断言)。同语言优先级 = 人物源文 > 名义桥 > 人物机翻(人物道领先是因为它**声明**语言,而桥只能由字形推断);槽内新增 `machine` 旗标,与 works/characters/labels 三面 intro 形状同构(见表③)。**孤儿名义不受影响**——无 person 链接者仍由名义桥独立作答,而孤儿是名义的绝大多数(11.9 万名义中约 10.6 万无 person 链接),此道不是等人物补齐后可退役的兜底。**wave 191 加法**:`display_name`+`lang`(名义记录名与其语言标签,`name{}` 分桶的诚实形式)+ `localized{}`(按 locale 取名的 map)——见 §3.7,分桶自本波起弃用。**wave 193 加法**:`siblings[]` 每行补 `display_name`+`lang`(191 漏了 sibling 行,而它正是分桶的最后一处消费点;当时刻意不补 `localized{}`,**wave 209 已补齐**,见 §3.7)。**🔴 wave 194 移除**:`name{}` 分桶不再下发(本记录与 `siblings[]` 行皆是),见 §3.7。v2.1 实施时由 persons/{id} 更名:实体层 credits 指向名义而非 person,公开词表与 resolve/redirects 的 "name" 键统一 |
| `GET /v1/catalog/characters/{id}` | `catalog:read` | 角色(含出演,spoiler 级字段)。**wave 191 加法**:`display_name`+`lang`、`aliases[]`(**本波之前该面一条别名都不发**,bangumi 中文名波灌进 `catalog_character_alias` 的角色中文名因此对下游完全不可达)、`localized{}`——见 §3.7。**🔴 wave 194 移除**:`name{}` 分桶不再下发,见 §3.7。**⚠️ wave 204 字节语义变更(2026-08-11,wave 207 补文)**:`image`(胸像)与 `figure`(立绘)两槽现供**已去背景的透明通道图**——白底原件在槽位上被**原地换掉**(`catalog_character.image_hash` / `figure_hash` 改指抠像结果),原件只留在 `image_source_hash` / `figure_source_hash` 两列,**公开面不可达、也不会有第二个 URL 给它**。URL 形状、键名、可选性一字未动,变的是字节:合成到非白底卡片上的消费端从此拿到正确结果,而**假定不透明白底**的消费端(直接叠白、或按白底做边缘/裁切处理)会看到渲染变化。作品详情 `characters[]` 行内的同名两键同此 |
| `GET /v1/catalog/labels` | `catalog:read` | **厂牌浏览/列表(A2-1b,keyset id ASC)**:过滤 `kind=`(封闭词表 `game_brand\|bunko\|publisher\|anime_studio\|doujin_circle\|group`,非法 token 400);item = `{id, display_name, localized, kind, work_count, logo_hash, has_relations}`(**wave 209** 补 `localized{}`——列表行与详情同一选举,浏览页免二次查详情;**wave 189** 补 `has_relations`,见 §②c;**wave 170** 补 `logo_hash`:厂牌 logo 在图床的内容哈希,与作品封面 `image_hash` 同币种,空串=无 logo);**合并走掉的厂牌不出列表**(merge 软删源行 + 写 redirect,旧 id 仍由 `/v1/catalog/redirects` 覆盖)。**A2-1e**:信封加 `total`(见下) |
| `GET /v1/catalog/labels/{id}`(+ `…/works`) | `catalog:read` | 厂牌/文库/社团;恒带 `intros[]`(多语言简介,按语言归并、`source`=来源键)与 `links[]`(官网/twitter/ci-en 外链,`{source,url}`;身份锚 exact/probable 永不入 `links`),无供给则为 `[]`;`refs[]` exact 身份锚(doc 106)。**A2-1e 加法**:`aliases[]`(别名,**排除 display_name**,恒出;**🔴 wave 209 对象化** `{value, lang, kind, machine}`,去重粒度从「跨语言同拼写」细化为 value+lang)、`lang`(display_name 自身的 BCP-47 标签,未记录则省略)、`work_count`(**nsfw 感知**,与 `labels` 列表行同一聚合——详情页与来路列表永不打架)。**wave 200 更正**:作品的 `labels[]` **一家公司只出一条**,其担任的全部身份进 `kinds[]`(排序、恒非空);单数 `kind` 保留但收紧为**最具识别力的那一个**(brand → circle → developer → publisher)。此前存储粒度 `(work,label,kind)` 让「既开发又发行」的公司占两行,**56,438 部作品**因此把同一家公司印了两遍。同波作品级归属收窄为「**原语言 · 非补丁** release 的公司」,移植 / 本地化 / 补丁方下沉到 release 层(新表 `catalog_release_label`),不再压平到作品。**wave 170 加法**:`logo_hash`(厂牌 logo 在图床的内容哈希,与作品封面 `image_hash` 同币种,恒出、空串=无 logo;作品记录的 `labels[]` chip 与厂牌列表行同带此键)。**wave 186 加法**:`relations[]`(会社关系图谱,`{id, display_name, localized, relation}`——**🔴 wave 209**:`name`→`display_name` + 补 `localized{}`;relation 词表 `parent|subsidiary|imprint|imprint_of|spawned|origin|succeeded_by|formerly`,镜像双向存储读面免反转,恒出、无边为 `[]`;供给=VNDB producer relations 经 exact 锚);`links[]` 供给扩容(VNDB producer extlinks:官网/twitter/pixiv/ci-en 类型化 + 白名单站点渲染为 `web` 源完整 URL),同时修复既有 steam/pixiv/web 行此前被渲染层静默丢弃的问题。**wave 191 加法**:`localized{}`(按 locale 取名的 map;既有扁平 `aliases[]` 原样保留)——见 §3.7。**wave 199 加法**:`imprint_work_count`(恒出)——沿 imprint / subsidiary **向下一跳**能多摸到多少作品,与 `work_count` 同一 nsfw 感知 + live 认领聚合。它是**第二个数,永不并入** `work_count`:并进去就抹掉了 wave 186 建图正是为了留住的那支箭头(读者再也分不清哪些是会社自己的作品)。两个人口**不相交**(母子共同署名的作品只算在母公司一侧),故 `work_count + imprint_work_count` 恰是 `works?label_id=<id>&label_rollup=1` 翻得到的行数 |
| `GET /v1/catalog/labels/{id}/relation-graph` | `catalog:read` | **会社关系图(wave 188)**:上一行的 `relations[]` 只有一跳,站在某品牌上看不见母公司旗下的兄弟品牌;本端点一次给出**整个连通企业家族**,供消费端直接画家族树。`data = {nodes:[{id,display_name,localized{},logo_hash,work_count}], edges:[{from,to,relation}]}`,两键恒出(无边为 `[]`),`nodes[0]` 恒为种子;节点名即 §3.7 的名字原语(08-19 跟进补齐——209 当波漏掉的唯一 label 投影,原短键 `name` 为 breaking 改名)。遍历 = 自种子的**广度优先** walk + visited 防环,**depth ≤ 4、nodes ≤ 60**,上限按广度生效(截断时留下离种子最近的一圈);软删(被合并掉)的 label 任何一跳都不出;**不分页**。边读作「**`to` 是 `from` 的 `relation`**」(与 `relations[].relation` 同一读法);因图镜像存,每个事实**只出一次** —— 仅出正向四值 `parent|imprint|spawned|succeeded_by`,四个反向值(`subsidiary|imprint_of|origin|formerly`)由**反读同一条边**得到(「X 的子公司」= `to` 为 X 且 relation 为 `parent` 的边);多源同一 `(from,to,relation)` 折叠为一条,`source` 不出面。`work_count` 与 `labels/{id}.work_count`、`labels` 列表行同一 nsfw 感知聚合。种子不存在 → 404,被合并掉 → 与 `labels/{id}` 同样的 **301 + `current_id`**,无边 → **单节点零边图**(不是 404) |
| `GET /v1/catalog/tags` | `catalog:read` | **规范 tag 浏览/列表(A2-1b,keyset id ASC)**:过滤 `tier=`(`core\|longtail\|hidden`)、`kind=`(`content\|meta`),两者皆封闭词表、非法 token 400;item = `{id, name, tier, kind, work_count, sexual}`(**A2-1f** 补 `sexual`,见 §3.2.4)。**A2-1e**:信封加 `total`(见下)。**批量加法**:`ids=`(逗号分隔规范 tag id,**≤100**,超出 400;非正整数/非数字 token 400)——**批量水合道**,与 `works?ids=` 逐字同义:`works/search?facets=tag_id` 回的 facet 行只带裸 id,用它**一次**把整页 facet 行解析成名字,而不是每行一发 `tags/{id}`。与 `tier=`/`kind=`/`has_works=` **合取**(不是旁路),不存在的 id 只是不匹配、**不报错**;`total` 与其余谓词一样随过滤收敛 |
| `GET /v1/catalog/tags/{id}` | `catalog:read` | **规范 tag(doc 106 G5)**:`{id, name, tier, kind}`(跨源规范词表 catalog_tag);**恒带 `intros[]`(A2-1b 加法)**——多语言简介,shape 与 `labels/{id}` 的 `intros[]` 一致(**wave 209 起同一个 `PublicIntro` schema** `{lang, intro, source, machine}`,tag 面 `machine` 恒 false 见表③;按语言归并低 source_id 胜出、`source`=公开来源键),无供给则为 `[]`;`include=works` 附带该规范 tag 下的作品(经 catalog_tag_source_map ⋈ catalog_work_tag,nsfw 门),按 `limit`/`offset` 翻页,**满页时**带 `next_offset`(= `offset+limit`),不满页则省略 = 到底。**A2-1e 加法**:`work_count`(**nsfw 感知**,与 `tags` 列表行同一聚合);**A2-1f 加法**:`sexual`(tag 级性内容轴,与列表行同一派生,见 §3.2.4) |
| `GET /v1/catalog/engines` | `catalog:read` | **引擎浏览/列表(A2-1b,keyset id ASC)**:无过滤;item = `{id, name, work_count, description, aliases}`(**A2-1e** 补齐后两键——引擎 facet 只有几百行、消费端一页渲染完,再为一行简介发第二趟请求是纯浪费)。VNDB 不发布引擎数据,该 facet 的唯一副本是 wiki 手工整理并由数据层退役波迁入的行。**A2-1e**:信封加 `total`(见下) |
| `GET /v1/catalog/engines/{id}` | `catalog:read` | **引擎条目(A2-1b)**:`{id, name, work_count, description, aliases, refs[]}`(后两键 A2-1e 补);`refs[]` 同 names/characters/labels 的 exact-only 身份锚(doc 106 G4),A2-0 落的 wiki eid 即在此浮出。非法 id 400、无此行 404 |
| `GET /v1/catalog/series` | `catalog:read` | **系列浏览/列表(2026-08-03 在产;wave 207 补录本行)**:keyset id ASC,与 labels / tags / engines 三条 taxonomy 道**同形**(`limit` 1-100 缺省 20、道内游标、信封带 `total`)。**它是 series id 的唯一发现入口**——系列**不进任何搜索索引**(`catalog/search` 的 `type=` 五族里没有它),没有这条道,调用方拿着 `series/{id}` 与 `works?series_id=` 却永远不知道 id 从哪来。item = `{id, display_name, source, work_count, has_nsfw}`:`work_count` **nsfw 感知**,等于**同一调用方**跟 `works?series_id=<id>` 翻页真正拿得到的行数(词条页与计数永不打架,同 §3.2 taxonomy 三道口径);`has_nsfw` 是「本行里有 r18 成员」的旗标。`source=` 泳道过滤(逗号分隔,**开词表**,未识别 token 出**空页而非 400**;现役 `curated` / `derived` / `dlsite`)见 §3.2.1 ②a——`total` 与 items 同一过滤总体。**系列无软删列**(importer 直接增删行),故本道无 `deleted_at` 谓词,未知 id 在下一行也是纯 404 |
| `GET /v1/catalog/series/{id}` | `catalog:read` | **系列条目(149c,聚合轨最后一处读面缺口)**:`works?series_id=` 过滤的那个分组实体终于有了地址——`{id, display_name, refs[], intros[]}`;`refs[]` = 该系列**行内**的来源锚(唯一键 `(source_id, external_id)`,不走 `catalog_external_ref`,故恒 1 元、构造上 exact),shape 与其它身份面的 `refs[]` 一致;`intros[]` = `{lang, intro, source}`(`source` 为公开来源键)——与 labels/tags 不同,**同一语言的多行不归并**:系列简介是退役波从 wiki 抢救来的手写正文、上游无从再生,第二个来源的正文是**另一份正文**而非劣质副本。`include=works` 附带成员作品(LIVE galgame 种群,`limit`/`offset` 与 `tags/{id}` 逐字同义,满页给 `next_offset`;`nsfw` 缺省丢弃 r18 成员,空成员集则整块省略)。**系列无合并/软删机制**(`catalog_redirect` 无 series 实体类型),故无此行即**纯 404,永不 301**。~~**刻意不开** `GET /v1/catalog/series` 列表道~~(superseded:列表道已开,见 §3.2 该行与 §②a 的 `source=` 过滤),作品详情的 series 内联块也**不加** `intro`(详情面保持精简) |
| `GET /v1/catalog/releases` | `catalog:read` | **发售动态时间线 · release 粒度(wave 174,keyset date + id ASC)**:日历的**下一粒度**——日历按作品**最早**发行日安放作品、一部只出现一次,故移植/复刻/本地化版在那里**构造上不可见**;本面把**每一行带日期的 release** 当条目、按其自身日期排序,港口与再版终于有位置。人口 = LIVE galgame 作品的未软删 release 且日期**至少精确到月**(`released_y` 且 `released_m` 非空)——只到年与无日期者**刻意不在本面**:它们排不进真实日期序,且已各有归宿(`calendar/pending` / `calendar/tba`)。`sort=date_desc\|date_asc`(缺省 `date_desc` 新→旧;**两个方向的破平都是 id ASC**),`date_from`/`date_to`(`YYYY-MM-DD`,闭区间,非法 400;**精度规则**:month 精度行落在该月**月首**即 `2024-06-00`,故 `date_from=2024-06-01` 会把它排除、`2024-05-31` 则收进);`kind=` 封闭词表 `default,digital,physical,trial,patch`(非法 token 400)——**缺省只出 default,digital,physical**,即**排除 trial 与 patch**:发售动态问的是「东西出了」,试玩版与汉化补丁要显式点名(`kind=patch` 即看本地化落地流);`lang=` 开放词表(逗号分隔或 `all`,缺省不闸),按 **`COALESCE(release.lang, work.olang)`** 匹配——dlsite/getchu 泳道一作一 SKU 且不记语言,而店铺 SKU 构造上就是作品原语,只认裸列会让半个注册表在任何 `lang=` 下消失;**条目仍印原始 `lang`**(未记录即空),coalesce 只是匹配规则不是编造值;`official=true\|false`(缺省不闸):该旗**只有 VNDB 泳道写**,`false` = 民间汉化/非官方版,**无此键者计为 official**(其余泳道落的是店铺 SKU,构造上即官方);`platform=` 开放词表,匹配 release 的**主 platform 列**(= works 列表同名过滤器的 release 腿逐字);`olang`/`nsfw`/`content_limit`/`limit`/`cursor`/`include` 与日历**逐字同义**(`olang` 缺省仍是策展的 `ja` + `zh*` 族)。**item = release 行 + 一个 `work` 块**:release 半边与详情面 `releases[]` 同形(`id`/`kind`/`date`/`title`/`lang`/`platform`/`platforms`/`refs`/**`labels`**,**release 级 exact 锚与版次公司恒在**、不受 `include=` 影响——时间线上一行提出的问题正是「这个移植是谁在出」,而 `work` 块答不了它),`work` = works 列表行逐字(`PublicWorkListItem`,`include=` 六词表施于此块);外加 **`is_first`**——该行是否为该作品**最早的带日期 release**,即**原版 vs 移植/再版**的判别位,按作品的整个带日期 release 集算出(**不随过滤器改变**:一行的 `is_first` 是这一行的属性)。`count` = 整个过滤集大小(非本页),`next_cursor` 末页 null;带**面级 ETag**(count + 最新 `created_at` + 最大 release id —— `catalog_release` 无 `updated_at`,故折三个数而非一个新鲜戳),`If-None-Match` 命中即在**装载任何页之前** 304 |
| `GET /v1/catalog/calendar` | `catalog:read` | **发售日历 · 月桶(A2-1c,keyset date ASC + id ASC)**:`month=YYYY-MM`(非法 400;**缺省 = 当前 Asia/Tokyo 月**,响应回显 `month`);收录**最早带年份 release 落在该月**的作品——**day 精度与 month 精度同桶**(month 精度排在该月月首,**不臆造 1 号**);item = works 列表行**逐字**(`PublicWorkListItem`,`include=` 五词表全支持),`nsfw` 同参;新增 `olang=` 人口过滤(见下);`count` = 整桶行数(非本页),`next_cursor` 末页 null;带**桶级 ETag**(见下);**A2-1e**:恒带 `meta{}` 导航框(见下) |
| `GET /v1/catalog/calendar/pending` | `catalog:read` | **发售日历 · 月份未定桶(A2-1c,keyset id ASC)**:`year=YYYY`(非法 400;缺省 = 当前 JST 年,响应回显 `year`);收录**最早 release 只精确到年**的作品——它们**刻意不出现在该年的任何月桶**里。人口/item/`olang`/ETag 语义与月桶逐字一致;`meta` 只带 `today`(非月寻址,无月界与前后翻) |
| `GET /v1/catalog/calendar/tba` | `catalog:read` | **发售日历 · TBA 桶(A2-1c,全局,keyset id ASC)**:有 release 行但**无一行带年份**的作品(已官宣、日期未定)。**无 release 行 = unknown,不进任何桶**——"没有 release"是"没有官宣",不是"日期待定" |
| `GET /v1/catalog/works/search` | `catalog:read` | **作品产品搜索(A2-1d,doc 126 D5;page/limit 分页)**:自由文本 `q=` 命中作品的**全部索引标题/别名**(含 search hint,仅供检索永不下发);**`search_intro=1` 另放宽到作品简介**(A2-1f,见 §3.2.4);过滤 `tag_id`(**多值 AND**,同列表)/`label_id`/`engine_id`/`series_id`/`released_after\|before`/`olang`/`content_rating`/`claimed`/`nsfw`(能力位已于 2026-08-25 退役,任何 key 传即放行,见 §3.2.10)——**与 works 列表同名参数逐字同义**(`released_*` 同样锚在**最早带年份 release** 的组合序数上,与列表 `release_date`、日历分桶三者同源)。**144 裁定:本面 `olang` 缺省 = 不设闸(全人口)**——搜索/浏览是身份触达面,没点名语言的调用方问的是「你有什么」,故服务端不替他收窄;这与日历缺省**故意不同**(日历 = `ja` + `zh*` 族,那是策展面)。`olang=all` 与显式集合(`olang=ja,en`)两面逐字同义,开放词表语义不变。`sort=relevance\|released_desc\|released_asc\|updated\|popularity`(缺省 relevance;**空 q 时 relevance 退化为 popularity** 即浏览序;`released_*` 两个方向都把**无日期作品排在最后**;`popularity` = 跨源信号 `log1p(max(bangumi collect 架, DLsite 下载数))`,**替代弃用面的 `view`**——那是 wiki 浏览量,catalog 无对应物,故 `sort=view` 是 400)。`facets=` 封闭词表 `content_rating,olang,claimed,tag_id,label_id,engine_id,series_id,source`(**非法 token 400**;外层键 = 可直接回传的**过滤参数名**,非索引字段名;`content_rating` 分布按公开字符串键计数不出枚举整数;每 facet 至多 100 个值)。`include=` 六词表全支持。**item = works 列表行逐字**(`PublicWorkListItem`,按 id 回库水化;**Meili 文档字段永不出 wire**)。`page` 缺省 1、非正/非数字 400,`limit` 1-100 缺省 20(超限截顶、非正/非数字 400)。**`q` 恰为 VNDB 作品 id(`v19658`)时短路**为该 id 的 exact 锚精查(全文会前缀串味:`v1965` 亦命中 `v19650`),仍套用调用方全部过滤器,**无解 = 空信封而非 404**。**`total`/`facets`/`items` 同门过滤**:翻完 `total` 页恰好收满 `total` 行,sfw 调用方的 `total` **已扣除**其永远拿不到的 r18 作品——**与弃用面 `content_limit` 陷阱(总数不过滤、items 过滤、sfw 翻页丢行)明令相反**。**A2-R1 区 C 加法**:`claim_state=`(封闭词表 `none\|live\|draft\|pending\|declined\|hidden`,逗号分隔 IN 语义,非法 token 400,不传=不闸)——产品站搜索务必传 `claim_state=live` 以排除未发布/未认领行,见 §3.2.7。**A2-R5 加法**:`content_limit=`(封闭词表 `sfw|nsfw`,逗号分隔 IN 语义,非法 token 400,不传=不闸)——与 `nsfw=`/`claim_state=` 正交,同编进那一条 Meili 表达式(故 `total`/facets/items 同门),见 §3.2.8。**wave 213 波 3 加法 · `fields=` 稀疏投影**:逗号分隔的**每个 item 的顶层键**白名单(不传 = 全量,与本波前逐字节一致);**信封不受影响**(`items`/`next_cursor`/`total`/`page`/`limit`/`facets` 永远原样)。`id` **恒在**;未知 token **静默忽略、永不 400**(§3.5 条款 2);**只裁不改形**;**在 `include=` 之后生效**——点名一个 include 门后的键(`intros`/`labels`/`ratings`/`covers`/`refs`/`latin`/`localized`)**不会**把它展开,两个参数都要给。它同样是**查询闸**:被 fields 裁掉的 include 块不再发查询,连今天无条件跑的三条(封面、展示轴、最早发售日)也随对应键一起关掉。服务端对顺序与重复不敏感,但请**按字母序书写 token**(CDN 按原始 URL 做键) |
| `GET /v1/catalog/search` | `catalog:read` | **实体自动补全**(`type=names\|characters\|labels\|works\|tags`,五索引;**`tags` 为 A2-1d 加法**,hit 镜像 labels 惯例并附 `tier`/`kind`)。**🔴 wave 209 改形**(此前的「hit shape 逐字节冻结」就此解除):`name` → `display_name`;各类 hit 一律补 `localized{}`(works 类 hit 的作品名字块自 wave 212 波 B 起也是它)——中文界面的搜索下拉从此不用二次查详情,见 §3.7 wave 209 节。至多 20 条扁平 hit、无过滤无分页 —— picker / 跳转框用面;**作品结果页(过滤/facets/排序/翻页/完整列表行)走 `GET /v1/catalog/works/search`** |
| `POST /v1/catalog/resolve` | `catalog:read` | 批量旧 ID → canonical(redirect 压平语义与内部一致) |
| `GET /v1/catalog/lookup` + `POST …/lookup/batch` | `catalog:read` | **外部 id 反查(killer,doc 19 §3.1,Phase 1)**:`?source=vndb&external_id=v19658` → work + `claimed_by` 指针;批量 ≤100。背书 = 四源 exact 锚(在产)。**`type=work\|name\|character\|label`(缺省 `work`,加法扩展)**:同一反查面按实体族分流——`work` 语义逐字不变(含 release 锚回落到属主 work),其余三族取**该族** exact 锚后委派各自详情投影(重块关闭),命中只填对应块 `name` / `character` / `label`,`work` / `claimed_by` 留空;批量每对可各带 `type`,响应回显**归一后**的 token(缺省对回显 `work`) |
| `GET /v1/catalog/redirects` | `catalog:read` | id 收敛事件 keyset 流(内部 S2S 面公开化,doc 19 §3.3) |

> 不进入公开路由:`/admin/*`、人审队列、merge/claim 等 S2S 写面、`/:gid/revert`、消息队列、site 管理等。
> catalog 面范围备注:`stub`(无锚且元数据不达标的未认领行)不进公开聚合——既有不变量,公开面直接继承;asmr/同人未认领波是否进 v1 投影,并入 [01 §15](./01-design.md) 再分发授权一起拍板(倾向:v1 先只放 galgame 可达闭包 + 跨媒介关系可达行,letmoe 上线时再扩)。
> **doc 106 加法(2026-07-28)**:`refs[]`(exact-only 身份锚)现同构出现在 names / characters / labels(此前仅 works 有);works 浏览列表 + changes 增量流 + 规范 tag 读面补齐了「可浏览 / 可增量同步 / release 与 tag 可寻址」四缺口。全部加法,spec-breaking 门背书;S2S/admin spec 逐字节不变。
>
> **锚存活性:上游删条目 ≠ 我们删锚(wave 207 补文)**:上游把某条记录删掉后,我们**不删这条 `catalog_external_ref` 行**,只给它打 `dead_at` 时间戳——删了下一轮导入会照规则把同一条锚重新推回来(负知识必须留档,同 `catalog_match_rejection` 之理)。**渲染面一律不出已死锚,公开面与 S2S 读面同一条规矩**:works 详情的 `refs[]`、works 列表 `include=refs`、**`releases[].refs[]`(S2S 读面的 `releases[].anchors` 同)**,以及 names / characters / labels / engines / series 各面,全部只出活锚。**只有直读数据库的导入 / 审计 / 合并工具仍看得见它**——它们本来就绕开读面自己写 SQL,这正是留档的用处。(wave 207 修正:此前 release 粒度的锚块**不过这道闸**,一条被上游删掉的 release 锚仍会渲染在 `releases[].refs[]` 里;当时 `dead_at` 只由 vndb 的 work 锚审计器写,故是潜在缺口而非线上事故。)**「不渲染」不等于「不解析」**:`lookup` / `resolve` / playtime 的 `by-ref` 上报仍然认已死锚——上游删了条目,消费端手里那个 id 还得能换回作品,否则留着这行就没有意义了。锚复活(上游又出现)时同一轮把 `dead_at` 清回 `NULL`,该行原地恢复渲染。当前唯一的写入方是周跑的 `cmd/audit-anchor-liveness`,覆盖 vndb / bangumi / erogamescape 的 work、release、character、person、credit_name、label 锚与全部 link_kind;每条 lane 自带镜像行数地板,erogamescape 另加 48 小时 freshness 规则——镜像半载或 EG 爬虫中途死亡时**拒绝执行**,否则会把整批活锚一次判死。
>
> **A2-1b taxonomy 读面加法(2026-07-29)**:三条 keyset 列表道(`labels` / `tags` / `engines`)+ `engines/{id}` + works 的 `engine_id` 过滤 + `tags/{id}` 的 `intros[]` + 详情面 `screenshots[]` 的 `width/height/thumbhash`。**公开 lookup 词表不扩**——仍是 `work\|name\|character\|label` 四族;engine / tag 的 id 解析走各自 detail / list 面,不进 lookup(它们是分类词表,不是可反查的跨源身份族)。全部加法,oasdiff 零 breaking。
>
> **A2-1c 发售日历加法(2026-07-29)**:三个 keyset 桶(`calendar` / `calendar/pending` / `calendar/tba`)+ 桶级 ETag + 新 `olang=` 人口参数。**item 零新字段**——就是 works 列表行本身(`include=` 词表一并继承),所以日历行和浏览行用同一套渲染代码;日历也**不新增**任何数据源或精度字段,它只是把既有的作品级 `release_date` 按序数分桶(语义见下)。全部加法,oasdiff 零 breaking。
>
> **A2-1e 供给补全加法(2026-07-29)**:本波**不新增端点**,只把既有端点上「消费方已经在用、catalog 侧却没有出口」的供给补齐。清单:`claimed_by.state`(R7)、engines `description`/`aliases`、labels `aliases`/`lang` 与 `labels[].lang`、works 列表 `include=refs`、详情 `engines[]`/`links[]`/`created`、三条 taxonomy 列表的 `total` 与 `labels/{id}`+`tags/{id}` 的 `work_count`、`tag_id` 多值 AND、日历 `meta{}`、`tags[]` 安全轴 + `spoilers=`。全部加法,oasdiff pinned 1.21.0 零 breaking;**缺省响应逐字节不变**(唯一新增的请求参数 `spoilers` 默认 0 = 旧行为)。
>
> **A2-R5 编辑展示轴加法(2026-07-29)**:本波**不新增端点**,只补一条**新的轴**——`claimed_by.content_limit`(恒出)+ works 列表 / works/search / 三个日历桶的 `content_limit=` 闸 + works 索引的 `content_limit` 可过滤字段。**年龄轴 `content_rating` 一字未动**(语义/参数/公面全保持)。全部加法,oasdiff pinned 1.21.0 零 breaking;**缺省响应除新增的 `claimed_by.content_limit` 键外逐字节不变**(新参数不传 = 不闸)。日历 ETag 人口键因此从 `nsfw × olang` 变为 `nsfw × olang × content_limit`——**校验子形态变了,缓存会失效一轮**,这是必须的(否则跨闸串味)。详见 §3.2.8。
>
> **`claimed_by.state` 语义(R7)**:`claimed_by` 从此恒带 `state`,词表 `live | draft | hidden` —— 这是 **catalog 自有的认领可见性词表**,不是任何产品的状态机(产品状态值永不进公开面)。
>
> | `state` | 含义 | 消费端 |
> |---|---|---|
> | `live` | 认领在产品面**公开可见** | 正常跟随指针,渲染认领徽章 |
> | `draft` | 存在但**尚未发布**(编辑态) | 不渲染产品内容;徽章可选,但不得当作已发布 |
> | `hidden` | 产品已**撤下**(封禁/退回) | **既不出徽章也不出内容** |
>
> - 这一位解决的是「`claimed_by` 是状态盲的」这个结构缺陷:没有它,下游按 `claimed_by` 再锚定会把产品已经撤下的词条在自己站上复活。
> - 投影由认领方的对账器维护(wiki:published→live、vndb-draft/pending→draft、banned/declined→hidden);**没有 draft/hidden 生命周期的认领方**(letmoe 等)不写这一列,读面渲染 `live`。
> - **词表外的值一律读作 `hidden`**——不认识的状态绝不对外发布。未认领行 `claimed_by` 仍是 `null`(不是一个带 state 的对象)。
>
> **`claimed_by.content_limit` 语义(A2-R5)**:`claimed_by` 另恒带 `content_limit`,词表 `sfw | nsfw`——**编辑展示轴**,与 `content_rating`(年龄轴)**不是同一件事**,详见 [§3.2.8](#328-编辑展示轴-content_limit闸a2-r5)。认领作品取的是 **wiki 正文的编辑判定**(人工设的 `galgame.content_limit`);未认领行没有 `claimed_by`,消费端按年龄轴回落(`r18→nsfw`,其余 `sfw`)。**凡吐 `claimed_by` 对象的面全带**——works 列表 item、works/search item、日历 item、`works/{id}` 详情(含 `relations[].work` / `series_siblings[]`)、`lookup` 单查与批量的 `work` + `claimed_by` 两块、各实体反查的作品 brief;`draft`/`hidden` 的认领同样带(编辑口径与可见性无关)。
>
> **taxonomy 三道的 `total` 语义**:等于**同一组过滤器**下的**整集**行数(不是本页、也不是游标之后的余量),所以把一条道翻到底收集到的行数恰好等于 `total`。它**不随 `nsfw` 变**——厂牌/tag/引擎行是身份而非内容,`nsfw` 在这三条道上只管每行的 `work_count`。
>
> **`tag_id` 多值(AND)**:`tag_id=7,12` = 该作品必须**同时**带映射到 7 和 12 的源 tag(facet 侧栏「再缩一个 tag」的语义),列表面与 `works/search` 逐字同义。**上限 10**;超限、非正整数、非数字一律 `400 tag_id must be up to 10 comma-separated positive integers`(**绝不静默丢过滤器**)。**单值行为与本波前逐字节相同**,重复 id 折叠。
>
> **日历 `meta{}` 导航框(R10)**:恒在。`today` = **Asia/Tokyo 当日**(`YYYY-MM-DD`,与缺省月/年同一时区),三个桶都有。月桶另有 `min_month`/`max_month`(**该调用方自己的人口门下**最早/最晚有成员的月)与 `has_prev`/`has_next`(由请求月对上述边界推导)。
>
> - **同门保证**:边界跑在 `nsfw` × `olang` 的**同一组门**下,所以「最新的非空月」= 「你自己能看到东西的最新月」——sfw 与 nsfw 调用方拿到不同边界是正确行为,不是不一致。
> - `has_next=false` 是**真的到头了**,不是「下个月恰好为空」;空月回跳直接用 `max_month`,不必逐月试探。
> - **人口为空**时 `min_month`/`max_month` **省略**(没有可跳转的月就不编一个),`has_prev`/`has_next` 仍明确给 `false`。
> - `pending`/`tba` **只带 `today`**:它们不是按月寻址的桶,前后翻箭头在那里没有指向。
> - **不进 ETag 键**:`meta` 完全由「桶级 ETag 已经折进去的人口键」加「写在 URL 里的请求月」决定,没有第三个自由度,所以缓存校验子不需要因它变化;它也在 `304` 短路**之后**才计算——命中缓存的请求依旧只付一次元查询。

> **A2-1f 供给微波(2026-07-29)**:`works/search` 加 `search_intro=`(简介检索,缺省关)+ `tags` 列表行与 `tags/{id}` 加 `sexual`。两项均为加法,缺省响应逐字节不变,oasdiff 零 breaking。语义见 §3.2.4。**部署注**:`search_intro` 需要跑一次 `reindex-catalog` 才有内容可匹配。

> **参数区间与越界语义(2026-07-28 cleanup 波)**:
>
> | 端点 | `limit` 区间 | 默认 |
> |---|---|---|
> | `GET /v1/catalog/works` | 1-100 | 20 |
> | `GET /v1/catalog/labels` / `…/tags` / `…/engines`(A2-1b 三条 taxonomy keyset 道) | 1-100 | 20 |
> | `GET /v1/catalog/calendar` / `…/calendar/pending` / `…/calendar/tba`(A2-1c 三个日历桶) | 1-100 | 20 |
| `GET /v1/catalog/releases`(wave 174 发售动态时间线) | 1-100 | 20 |
> | `GET /v1/catalog/changes` | 1-500 | 100 |
> | offset 型子列表(`names/{id}?include=credits`、`characters/{id}` / `labels/{id}` / `tags/{id}` / `series/{id}?include=works`) | 1-50 | 50 |
>
> - **越上限 clamp 到上限**(不回落默认值):`limit=1000` 在 works 面即 `limit=100`,而不是悄悄退回 20。
> - **非正数 / 非数字 400**:`limit=0`、`limit=-1`、`limit=abc` 一律 `400 limit must be a positive integer`,不再静默取默认值。
> - **`label_id` / `tag_id` / `series_id` / `engine_id` 同理**:缺省/空 = 不过滤;一旦给值就必须是正整数,`abc` / `0` / `-5` / `1.5` 一律 400(旧行为把非法值退化成 0 → 过滤器**静默消失**、返回不过滤的首页,是最坏的一类失败)。
> - **游标不跨道**:每条 keyset 道(works `id` / works `updated` / changes / labels / tags / engines / calendar / calendar-pending / calendar-tba / releases-date-desc / releases-date-asc——**时间线两个方向也是两条道**,同一位置在两向上意思相反)的 `next_cursor` 只在本道有效,拿去另一条道一律 `400 malformed cursor`。
> - `offset` 保持宽松(负数归 0,不 400)。
>
> **lookup `type` 词表(2026-07-29 加法波)**:`work`(缺省)/ `name` / `character` / `label`,两个面(GET 单查 + POST 批量)同一套。
>
> - **非法 token 一律 400**(`type must be one of work, name, character, label`):`type` 是**我方封闭词表**,拼错即调用方错误;批量中**任一对**非法即整个请求 400,不把该槽悄悄降级成 miss。对照:**未知 `source` 仍是 miss/404**——来源是开放注册表,不该因为我们尚未收录某个站点就把调用判为错误。
> - **`external_id` 归一只发生在 `work` 面**:vndb 作品接受 `v19658` 或裸 `19658`;`name` / `character` / `label` 按注册表存法**逐字匹配**(vndb 角色 `c1234`、厂牌 `p129`、staff 是**裸数字**)——给非作品面补 `v` 前缀只会 100% miss。
> - **可见性继承各实体详情面**:命中后委派 `names/{id}` / `characters/{id}` / `labels/{id}` 的投影(`include` 重块关闭),因此 `nsfw` 语义与那三个端点逐字一致(例:character 身份不因 `nsfw=0` 隐藏,只掉 sexual traits;r18 隐藏仍只是 `work` 面的规则)。
> - **响应加法**:`PublicLookupData` / 批量 item 新增可选块 `name` / `character` / `label`(不命中即整块省略),`work` / `claimed_by` 字段语义不变;批量 item 另加恒在的 `type` 回显。spec-breaking 门(oasdiff)背书为非破坏。
>
> **taxonomy 三道的 `work_count` 语义(2026-07-29 A2-1b 落账;口径于 2026-07-30 wave 146 统一为 live)**:`labels` / `tags` / `engines` 每行的 `work_count` 是 **nsfw 感知**的——它等于**同一调用方**用 `works?label_id=` / `?tag_id=` / `?engine_id=` **且带 `claim_state=live`** 翻页能真正拿到的行数,也就是词条页成员列表实际发出的那一次调用。
>
> - sfw 调用方(缺省)的计数**剔除 r18**;`nsfw=1` 给全量(该参数的能力位已于 2026-08-25 退役,任何 key 传即放行,见 §3.2.10)。计数与成员列表**永不打架**——这是刻意反着写弃用面的 `official.galgame_count`(恒 0 却挂着非空成员列表)。
> - 统计口径 = works 列表的种群谓词逐字复用:LIVE + galgame 媒介 + 未软删 + **`claim_state=live`**,`stub` / 其它媒介 / 软删行一律不计。
> - ⚠️ **`claim_state=live` 闸(2026-07-30,wave 146)**:此前计数把**未发布的 draft 行**与**未认领注册行**一并算入,而词条页成员列表按 §3.2.7 传 `claim_state=live`——两个数于是**系统性不等**,计数偏高(07-30 断面 galgame 媒介:live 10,927 vs draft 53,521 + 未认领 17,560,约 6 倍)。现在计数与成员列表**同一个闸**,且由**同一个谓词编译器**产出,构造上不可能再分叉。**这三个数会一次性下降**——这是既知虚高被修好,不是计数丢失。
> - **nsfw 轴不动**(§23「身份非内容」裁定不翻案):`nsfw` 在这三条道上仍然只管每行的 `work_count`、不管行本身是否存在,也不受本次 claim 闸影响。
> - **去重按作品**:一个作品对同一厂牌可有多条不同 `kind` 的归属边、可携带多个映射到同一规范 tag 的源 tag,计数只算一次。
> - 实现上是**页级批量 GROUP BY**(每页一条聚合查询),不是逐行 count。
> - ⚠️ **tag 的数来自上卷,是「截至某刻精确」而非「构造上精确」(2026-08-10,wave 201)**:`labels` / `engines` / `series` 仍是每次请求现算,**唯独 tag** 改读预算表 `catalog_tag_work_count`。原因是 tag 是唯一经映射表(`catalog_tag_source_map ⋈ catalog_work_tag`,约 120 万行)找作品的边,现算需 200–400ms 且挂在**每一次** `works/{id}` 上,占了 catalog 服务慢查询日志的 90%;换 join 顺序、加覆盖索引、把映射反规范化到边表三条路都试过,都只有 ~1.5× —— 这不是执行计划问题,是这道题本身就要算这么多。上卷由**读路径自己的聚合函数**产出(同一个谓词编译器,只是不带 id 过滤),所以它**只可能过期,不可能算的是另一道题**;刷新工具 `cmd/refresh-tag-counts` 全量重算 + 单事务换页,`computed_at` 列即新鲜度。数字仍然只承诺「点进去会拿到多少」,只是这句话的时态从「此刻」变成了「上次刷新时」——tag 计数只在批量导入后才会动,故取此权衡。
>
> **发售日历三桶语义(2026-07-29 A2-1c 落账)**:三个桶按**同一个分类锚**切分——作品的**最早一条「带年份、未软删」release** 的组合序数(`y*10000 + m*100 + d`,月/日未知记 0)。这**正是** works 列表投影为 `release_date` 的那个数,所以「一行落在哪个桶」与「这一行印着什么日期」永不打架。
>
> | 该作品最早 release 的精度 | 组合序数示例 | 落桶 |
> |---|---|---|
> | day(`2024-06-14`) | `20240614` | `calendar?month=2024-06` |
> | month(`2024-06`) | `20240600` | `calendar?month=2024-06`(排在该月**月首**,**不补 1 号**) |
> | year(`2024`) | `20240000` | `calendar/pending?year=2024`——**该年任何月桶都不出** |
> | 有 release 行但无一行带年份 | — | `calendar/tba` |
> | **无 release 行**(unknown) | — | **不进任何桶** |
>
> - **月窗判据 = 序数区间 `[y*10000+m*100, +99]`**:day 精度(1-31)与 month 精度(d=0)同时落入,year 精度(`y*10000`)天然出界。
> - **移植/复刻按最早那次归桶**:2024-05 首发、2024-06 复刻的作品在**五月**桶,和它 `release_date=2024-05-02` 一致。
> - **JST 定界**:galgame 发售日是日本民用日期,故 `month` / `year` 缺省 = **Asia/Tokyo 当前**月/年(固定 +09:00,JST 无夏令时);服务端解析出的窗口**回显**在响应的 `month` / `year` 里——缺省调用方否则无从得知拿到的是哪一格。
> - **人口 = works 列表谓词逐字**(LIVE + galgame 媒介 + 未软删;`nsfw=1` 才出 r18)**+ `olang=` 原语言门**:缺省 = **`ja` + 全部 `zh*` 家族**(VNDB 系西方目录会淹没新作月表);`olang=all` 关闭该门;也可给逗号分隔的显式集合(`olang=ja,en`)。**族缺省是日历(策展面)独有的**——`works/search` 的同名参数缺省相反 = **不设闸**(触达面,见上表 144 裁定)。**144 事故备忘**:该门此前是全局 no-op,因为 `catalog_work.olang` 全注册表恒 `ja`(没有任何建档 lane 写过真值);回填(`cmd/backfill-olang`,vndb 锚 → `src_vndb.vn.olang`,wiki 兜底 → `galgame.original_language` 映射)+ 全量 reindex 之后,本门才真正生效。**`olang` 是开放词表**(存的是上游 BCP-47 拼写 `ja` / `zh-Hans` / `zh-Hant` / `en` / `ko` …,**不是**弃用 wiki 面的产品 locale 形态 `ja-jp` / `zh-cn`),故无人使用的值 = **空桶,不是 400**——对照我方封闭词表(`content_rating` / `kind` / `tier`)拼错即 400。works 列表本身**本波不加** `olang=` 参数。**A2-R5 再加一道 `content_limit=` 门**(封闭词表 `sfw|nsfw`,非法 token 400,不传=不闸):它参与**分桶成员判定**、`count` 与 `meta` 框,因此也进下面的 ETag 人口键,见 §3.2.8。
> - **桶级 ETag**:`W/"cal-<桶键>-<人口键>-<count>-<max(updated_at) unix>"`,其中元查询(`count` + `max(updated_at)`)跑在**整个过滤集**上、**先于**任何分页加载,`If-None-Match` 命中即 `304` 短路(省掉整页 item 富化)。人口门(`nsfw` × `olang` × `content_limit`,A2-R5 起三段)进键——两个不同人口的 count 可能偶然相等,不能共用校验子。`limit` / `cursor` / `include` **不**进键:ETag 只需在**同一 URL** 内唯一,而这三个参数都写在 query string 里。`max(updated_at)` 取自 `catalog_work`——facet 写入统一 touch 宿主作品(与 changes 流同一纪律),所以改 release 日期会推动校验子。
> - `count` 是**整桶**行数(本页至多 `limit` 行),由上面那次必跑的元查询顺带给出。
>
> **封面严格度:列表面 > 详情面(对 sfw 调用方)**——`works` 列表的单图 `cover` 对 sfw 调用方会**丢弃 `sexual≠0` 的封面**(挑不出合规图时 `cover` 为空串),而详情面 `covers[]` / `screenshots[]` **恒发全量**并逐行带 `sexual` / `violence` 旗标交由消费端自行取舍。列表 `include=covers` 的两槽同样吃这条 sfw 规则(见 §3.2.1)。

**playtime 面**(后端 = `cmd/catalog` 同进程挂载;**平台第二个公开面,也是第一个用「用户 Bearer 令牌」而非 API key 认证的公开面**——语义与错误面详见 §3.8):

| 公开端点(`/v1`) | 凭据 | 说明 |
|---|---|---|
| `PUT /v1/playtime/works/{workID}` | 用户令牌(任意 app) | 上报**自己**在某作品上的游玩时长。body `{minutes, status?, last_played_at?}`,`minutes` 是**绝对累计值(分钟),永远不是增量**——重发同一个数是 no-op,故该调用**可安全重试**。按 `(user, work, client)` 三元组落行:同一用户的第二个 App **并排写**而不是覆盖。作品非 LIVE / 不存在 → 404。**不需要** `playtime:write` |
| `PUT /v1/playtime/by-ref/{source}/{externalID}` | 用户令牌(任意 app) | 同上,但用客户端手里已有的**外部 id** 寻址(`vndb`/`dlsite`/`getchu`/`bangumi`…),免去先跑一趟 lookup。**只认 exact 锚**;响应回显解析出的 `work_id`(带 `resolved_from`),客户端应缓存它。源键未知 / 无锚 → 404 |
| `POST /v1/playtime/batch` | 用户令牌(任意 app) | 首次登录的**库同步**:一次最多 200 条,每条可用 `work_id` **或** `source`+`external_id` 寻址。**逐条判定**——响应给 `{accepted, refused, results[]}`,`results[i]` 带 `{index, status(ok\|not_found\|rejected), work_id, error}`;**一条坏数据永不带崩整批**(不是事务) |
| `GET /v1/playtime/mine` | 用户令牌(任意 app) | 分页拉**自己**的全部记录,按 `updated_at` 升序;`updated_since=`(RFC 3339)只取该时刻之后变化的行,`limit` 1-200 缺省 200。响应的 `cursor` 就是本页最后一行的 `updated_at`,**原样回填成下一次的 `updated_since=`** 即增量续拉——第二台设备的回灌腿。**不需要** `playtime:read` |
| `GET /v1/playtime/works/{workID}` | 用户令牌(任意 app) | 自己在**某一部**作品上的记录,**跨自己的多个 App 折叠**:`minutes` 取 **MAX**(两个 App 盯同一份存档不是两周目),`status` 取那一行的状态但**任一行 finished 即整体 finished**,`last_played_at` 取最新,另给 `clients` = 折叠了几个 App。从未上报过 → `data` 为 **`null` 且是 200,不是 404**(评分表单据此问「你玩了 30 小时,要附上吗?」) |

### 3.2.1 D7 投影约定(2026-07-29 A2-1a 落账)

四条约定,定义公开面在「多语言」「模糊日期」「机翻」「封面槽」几处的**投影口径**。它们描述的是既有数据的**呈现约定**,不新增任何数据源。

**① `include=names` / `include=intros` 的语言口径**

catalog 内部按 BCP-47 存语言(`ja` / `zh-Hans` / `zh-Hant` / `en`、以及历史遗留的裸 `zh`),公开面**原样发这些标签**:`include=names` 出 `latin` + `localized{}`(键 = canonical BCP-47 tag),`include=intros` 出 `[{lang, intro, source, machine}]`。这两块此前被压进四个固定产品键(`ja-jp` / `zh-cn` / `zh-tw` / `en-us`),`ko` / `ru` 之类没有键可去的语言在块里被整批丢弃;该压缩层已于 **wave 212 波 B 整体删除**,公开面不再有任何产品键。

- **`include=` 的六个 token 拼写不随之改名**:`names,intros,labels,ratings,covers,refs` 逐字照旧,`include=names` 现在闸的是 `latin` + `localized{}`(未点名时**整键缺席而非发 `{}`**)。moyu 全站标题都走这个 token,改名会让它全站渲染空标题且两侧都不报错。
- **`localized{}` 每 locale 选唯一行**:定序自 wave 210 起为 **provenance → kind → id**——`source`(0)恒压 `machine`(1),同 provenance 内再取 kind 最低的一行(`official`(0) > `alias`(1) > `abbreviation`(2),与详情面 `titles[]` 同一序),同 kind 按行 id 升序。**源标题永远赢下它的 locale,机翻只能占源没填的空位**(与实体 `localized{}` 的机器填缺同一条规矩)。**`search_hint`(kind=3)永不公开**(既有硬规则,查询层即排除)。
- **`lang` 为空或不成其为 BCP-47 标签的行一律不入**,但不是丢失:详情面 `titles[]` 恒发**完整**行集合,搜索索引同样吃它们。wiki 正文的别名**无语言**(A2-R1,见 §3.2.5),即属此类——别名可搜可列,但不占任何 locale。
- **`intros` 与详情面是同一个数组**:每语言归并在读面已完成(每语言最优来源胜出 + 机翻让位于源文,见表③),列表块与 `works/{id}` 的 `intros` 对同一部作品给出逐字相同的数组。

**② release `date` ↔ 旧面 `release_date` + `release_precision`**

catalog 的 release 日期是**部分 ISO**:`YYYY` / `YYYY-MM` / `YYYY-MM-DD`,精度**由字符串长度自明**,不另发精度字段。与弃用的 `/v1/galgame` 面(`release_date` 是**归一化**的完整日期,精度另存 `release_precision`)对照:

| 旧面(`/v1/galgame`) | catalog `date` / `release_date` | 说明 |
|---|---|---|
| `release_date=2021-06-04`, `release_precision=day` | `"2021-06-04"` | 长度 10 |
| `release_date=2021-06-01`, `release_precision=month` | `"2021-06"` | 长度 7;**日不得臆造**,旧面归一化补的 `01` 是占位符,不要回读为 1 号 |
| `release_date=2021-01-01`, `release_precision=year` | `"2021"` | 长度 4;同上,月/日均为占位符 |
| `release_precision=tba` / `unknown` / `release_date=null` | **`null`** | 作品级 `release_date`(最早 release)与 release 级 `date` 同此口径 |

- 作品级 `release_date` = 该作品**最早**有年份的 release 的部分 ISO;无任何带年份的 release 即 `null`。
- 消费端解析建议:按长度分派(4 / 7 / 10),**不要**用 `Date.parse` 后取字段——那会把 `"2021"` 悄悄变成 1 月 1 日,正是本表要避免的失真。
- **日历三桶与本表同一个分类锚**(A2-1c):`calendar` / `calendar/pending` / `calendar/tba` 的桶籍就是由这里的作品级 `release_date` 决定的——长度 10 / 7 落月桶、长度 4 落 pending 桶、`null` 且有 release 行落 tba、`null` 且无 release 行不进任何桶。所以一行的**桶籍与它印出来的 `release_date` 永不打架**。

**②c 厂牌浏览行的 `has_relations` 位(wave 189 加法)**

`GET /v1/catalog/labels` 每行恒带 `has_relations`(布尔)——该厂牌是否有会社关系边,即 `labels/{id}/relation-graph` 打过去会不会有东西。

- **给的是旗标而不是边**:39,653 个厂牌里只有 2,139 个(**5.4%**)有任何关系边;把 `relations[]` 铺到每个浏览行 = 为二十分之一的行给整页加 join,而这条列表道是**刻意瘦**的。
- **它解决的正是 N+1**:列表消费方真正需要的不是边本身,而是**哪几行值得去打 relation-graph**;没有这一位,浏览 20 个厂牌就得盲打 20 次才能找出那 1 个有家族的。判据与 series 列表行的 `has_nsfw` 同源——"从过滤后的计数派生的旗标恰恰对最需要它的调用方读作 false"。
- 无关系边的行报 `false`,不省略键。

**②a series 浏览道的 `source=` 泳道过滤(wave 189 加法)**

`GET /v1/catalog/series` 收 `source=`(逗号分隔,**OPEN 词表**)——按行内已印出的同一个 `source` 键选泳道。现役三条:`curated`(手工归档,144 条)、`derived`(自动系列泳道产出,4,805 条)、`dlsite`(该 importer 归档,592 条)。

- **为什么是开词表而非闭词表**:哪些源产出系列是**注册表数据**、不是代码级枚举,闭词表会逼得每来一个 importer 就改一次代码并重发 spec。故语义照抄 `works/search` 的 `olang`——**未识别 token 返回空页,不是 400**。(对照:`content_limit` / `claim_state` / `facets` 是闭词表,因为它们的取值确实由代码定义。)
- `total` 与 items **同一过滤总体**(works/search 规则),故过滤后不会报回全库计数;游标不参与 `total`(它是泳道大小,不是"还剩多少可走")。
- 不传 = 不闸,与本波前逐字节相同。

**②b credits 的组织署名者(wave 189 加法)**

`works/{id}?include=credits` 的每个 credit 槽新增 `label_id` + `label`(厂牌显示名),**语义与既有的 `character_id` + `character` 完全对称**——后者是"这条 credit 演的角色",前者是"这条 credit 的组织署名者"(developer / publisher 一类**厂牌性质**的 role)。可寻址:`label_id` 直接进 `/v1/catalog/labels/{id}`。

- **署名者不取代被署名的名义**:两者共存(模型里 `label_id` 与 `credit_name_id` 并列),纯人物 credit 无署名者时两键**双双缺席**,不出 `0`。
- 覆盖率约 2%(101 万 credit 行中 21,419 行带 `label_id`),故绝大多数槽形状与本波前逐字节相同。
- **不违反裁定 7**:该白名单剔除的是 `note` / provenance / 审计类字段;署名者与角色同属**可寻址身份指针**,不是其中任何一类。

**②d credit 的来源归因 `source`(wave 189 加法,裁定 7 的唯一例外)**

每个 credit 槽新增 `source`,取值是与 refs / intros / screenshots / ratings **完全同一套公开源键拼写**(`erogamescape`;旧误拼 `erogamespace` 仍作入参别名接受)。

- **这是对裁定 7 的定向反转**(2026-08-07 裁定;原文见 `refs/plans/05-open-api/03-catalog-public-face.md` 第 7 条)。理由:该裁定写于 Phase-1,早于「响应携带 attribution 归源」成为本平台的对外卖点;本面每一个多源数组都已指名出处,唯独 credit 沉默,消费端拿两个上游的花名册对账时无法判断哪条是谁说的。
- **`note` 维持剔除**:自由文本、18.3% 覆盖(101 万行中 185,581 行非空)、未经审校,与本键不是一类。
- 无来源行的手工 credit **缺席该键**(`omitempty`),不出空串。

**③ intro `machine` 旗标语义**

| `machine` | 含义 | 消费端建议 |
|---|---|---|
| 缺席 / `false` | **源文**:来自 `source` 指名的上游站点原文 | 直接展示 |
| `true` | **机器翻译**(LLM,step 75 ja→zh-Hans 起):`source` 仍是**被翻译的那个源**,归因语义是"译自该源" | 展示时应标注「机翻」之类的提示,不与源文等同 |

- **机翻永不冒充源文**:某语言只要存在源文行,机翻行就在读面归并中落败、根本不出现;`machine=true` 只可能出现在"该语言没有任何源文"的语言上。
- 该旗标同时出现在作品详情与列表 `include=intros` 的 `intros[]` 每个元素,以及 `characters/{id}` / `labels/{id}` / `names/{id}` / `tags/{id}` 的 `intros[]`,语义逐字一致——**wave 209 起五个 intro 面在 spec 层就是同一个 schema(`PublicIntro`)**,消费端写一个渲染器即可;tag 面的 `machine` 现阶段**恒 false**(`catalog_tag_intro` 无 provenance 列,写入方只有 curated 编辑面)。
- **`names/{id}` 的双泳道特例**:该面的"某语言存在源文"判据跨**两条泳道**统一计算(人物级源文 + 名义级桥都算源文),故人物机翻只在两条道对该语言均无源文时才出现。

**④ 两槽判据**(与三表同批落账;**wave 207 更正**:同一挑选器也产出详情面的 `cover_slots`,两面逐字同义——本节读作两面共同的判据;**v3 波 2026-09-05 重写了择优与兜底**)

`covers` / `cover_slots` 出 `{portrait, banner}`,每槽 `{url, width, height, thumbhash, sexual, violence, source, origin}` 或 `null`。

- **⚠️ 判据是 kind × 尺寸两道,不是只看尺寸(wave 207 更正)**:本节此前写作「朝向来自真实尺寸,不来自 `kind`」——那对 `portrait` 的兜底档成立,对 `banner` 不成立,并已实爆过一次(碟面被当成 hero 图铺上详情页顶)。**v3 起 `pkg` 全族(`pkgfront` / `pkgback` / `pkgmed` / `pkgcontent` / `pkgside`,即盒封正面 / 封底 / 碟面 / 内页 / 侧标)一律不算封面画**:任何尺寸、两个槽都进不去,连 `first` 兜底也不进,**`portrait_pinned` 同样不例外**——生产里约 258 个 pkg 钉是退役前的重钉阶梯留下的,读面直接中和,不等重钉跑完。列表面的单图 `cover` 跳过同一族。
- 尺寸那一道:竖版 = `height > width × 1.05`(沿用 `cmd/pin-portrait-covers` 的 U 轨切点);横版 = `width / height ≥ 4/3`。两个朝向各有一条**宽度优先档**(是分档,不是门槛):竖版 `width ≥ 500`、横版 `width ≥ 800`(够宽才铺得开)。
- **同档内取最清晰的一张**:面积 `width × height` 最大者胜,同面积按 `image_hash` 字典序取小。**排序不跨档、也不跨安全轮**:先用 display-safe 的图把各档填满,只有仍然空着的档才由 sexual 轮补,故一张小的安全图恒胜一张大的露骨图;分档先于面积,所以一张 500 宽的竖版胜过一张更多像素的窄条。
- `portrait` = `portrait_pinned` 行(**v3 起限竖版或尺寸未知**:钉的写入路径没有形状门,生产里有 1920×1080 的宣传横幅被钉成卡面;量过且不是竖版的钉**不占这一槽**,但照常参与它形状合得上的其它档——一张被钉的横图仍是合格的 `banner`。尺寸未知的钉照旧生效,否则 image_service 还没量到的作品会整片空掉)→ 否则 `width ≥ 500` 竖版中最清晰者 → 否则任意宽度竖版中最清晰者 → 否则该调用方可见的首图(按 `sort_order`, `image_hash` 序)。**`portrait` 永不退到截图**:一张场景图不能替作品当门面。
- `banner` = `width ≥ 800` 横版中最清晰者 → 否则任意横版中最清晰者 → **否则退到该作品的截图**(v3 加法):只收横版截图,同样分 display-safe / sexual 两轮,轮内按 `sexual` 升序(安全的先,哪怕更小)→ 够宽(`≥ 800`)的先 → 面积大的先 → `image_hash` 小的先。**仍无(含 image_service 查询未接线、尺寸未知时)即 `null`**,绝不猜。
- **`origin` 指明该槽选自哪个池子**:`cover`(封面行)/ `screenshot`(截图兜底)。两槽恒带此键,`screenshot` 只可能出现在 `banner` 上。
- **整块为 `null` 的条件是「没有可用封面**且**没有可用截图」**:一部只有 pkg 封面、但有一张横版截图的作品,仍出一个 `portrait` 为 `null`、`banner` 为截图的 `cover_slots`。
- 只有一张可用封面时两槽可能指向同一图,这是预期。
- `width` / `height` / `thumbhash` 来自 image_service 的按需批量查询,**未知即三键一并省略**(消费端退回骨架屏);详情面 `covers[]` **与 `screenshots[]`** 每行同样带这三个可选键(A2-1a 加法,A2-1b 补齐 screenshots——两个粒度共用**同一次**批量查询,详情面对 image_service 仍只发一趟)。截图兜底只在 `banner` 落空时才额外取一趟截图行 + 一趟 meta,列表面整页合成一趟;全页都不需要兜底时零额外查询。
- sfw 调用方在**两槽**都永不见 `sexual≠0` 的封面(与列表单图 `cover` 同一规则;`violence` 同样不入门槛)。

**⑤ 实体图的 `*_meta`(加法)**

作品媒体(`covers` / `screenshots` / `cover_slots`)一直带 `width`/`height`/`thumbhash` + `sexual`/`violence`,**实体图此前只有一个 hash 或 URL**。现每个实体图槽旁并置一个可选对象,形状 `{width?, height?, thumbhash?, sexual?, violence?}`:

| 面 | 既有键(不变) | 新增对象 |
|---|---|---|
| `characters/{id}`、`works/{id}` 的 `characters[]` 花名册行、`works/{id}/characters` | `image` / `figure` | `image_meta` / `figure_meta` |
| `names/{id}` | `photo_hash` | `photo_meta` |
| `labels/{id}`、`labels` 列表行、`labels/{id}/relation-graph` 的节点 | `logo_hash` | `logo_meta` |

- **既有字符串键一字未动**,对象纯属加法;值同样取自 image_service 的按需批量查询(每个响应一趟,与封面/截图共用同一次),**查不到即整个对象缺席**,面照常作答。
- **`sexual`** = 该图的机器分级 `0 安全 / 1 性暗示 / 2 露骨`,与作品媒体轴同一把尺(图床 `0→0, 1→1, 2→2, 3→2`)。**缺席 ≠ 0**:缺席 = 尚未评级(刚上传、夜间 grader 未跑到),`0` = 已评级且判为安全。把两者混同就是把未审图当安全图渲染。
- **`violence`** = `0 无 / 1 暴力 / 2 血腥`,**目前恒缺席**:逐图暴力判定只有 VNDB 社区投票一处供给,那批图走的是作品媒体面;自动暴力分级实测不可用,实体图上没有可断言的值。字段先立,语义钉死为「**缺席 = 无已知判定**」——与作品媒体面 `violence` 的 `0` = 「该源没有这个轴」是同一句诚实话的两种写法。

### 3.2.2 作品级 `links[]`(2026-07-29 A2-1e 落账)

`GET /v1/catalog/works/{id}` 的 `links[]` 是该作品的**非身份外部网页链接**——商店页 / 官网 / 社交页,形状 `{source, url}`,恒在(无供给为 `[]`)。

- **与 `refs[]` 互不相交,这是硬红线**:`refs[]` 是身份锚(「这个作品在上游叫什么 id」,exact-only),`links[]` 是网页地址。同一条 `catalog_external_ref` 行按 `link_kind` 分流,exact/probable 永不入 `links`、related 永不入 `refs`——与 `labels/{id}` 画的是同一条线。
- **无 user 归属,也没有标题**:这些字节来自 wiki 用户提交的链接表,退役波 W0 以**平台策展身份**收编(`user_id` 从未随迁),因此弃用面的「按作者封禁过滤链接」在这里**自然消失,是设计而非疏漏**;链接的用户自填标题同样**没有**被收编,所以 `links[]` 里**不会有** `label`/`title` 键——凭空造一个就是编造。消费端请用 `source` 键(或 URL 的 host)作为标签。
- **URL 模板只覆盖能确定的来源**:`web`(external_id **本身就是完整 URL**)、`twitter`、`cien`、`steam`、`pixiv`、`official_site`。**`dlsite` / `dmm` 刻意不出现在 `links[]`**:注册表只存裸商品号(`RJ…` / `d_186489`),而它们的商店 URL 分区依来源而异(dlsite maniax/home/pro/soft;dmm digital/dlsoft),任何单一模板都会对一部分行 404——猜一个地址比不给更糟。这两类锚仍然以数据形式可达(详情面的 refs / releases 侧),只是本面不为它们编造地址。

### 3.2.3 tag 安全轴(2026-07-29 A2-1e 落账)

`GET /v1/catalog/works/{id}` 的 `tags[]` 每行恒带两个安全轴键,外加一个新的请求参数:

| 键 / 参数 | 含义 |
|---|---|
| `spoiler` | **该 work-tag 边**的剧透级别:`0` 无 / `1` 轻微 / `2` 严重 |
| `sexual` | **该 tag 本身**属于性内容类别(bool) |
| `spoilers=0\|1\|2` | 请求参数,剧透**上限**;缺省 `0` |

- **缺省安全**:`spoilers` 缺省 0,响应里**一条剧透 tag 都没有**——完全忽略这个轴的消费端天然安全,本波前的字节也因此一字不变。要做「点击展开剧透标签」的交互就显式传 `spoilers=1|2`。
- **覆盖面必须照实说**:这条轴的上游只有 **VNDB 系词表**(剧透值与性内容类别都出自那里,也正是剧透 tag 的实际所在)。**Bangumi / DLsite 的 folksonomy 上游根本没有剧透与类别概念**,所以那些行渲染成 `0` / `false` —— 这表示**该来源没有这条轴**,**不是**「已确认安全」的断言。消费端若要做严格门控,应结合 tag 的来源 `source` 判断。
- 词表外的 `spoilers` 值退化为缺省 0(与 `characters/{id}` 的 `spoilers` 姿态一致),不是 400。
- **明确不做的事**:安全门**没有**降级成作品级 `content_rating`。用作品分级当 tag 剧透门是静默暴露——一个全年龄作品照样可以有严重剧透 tag。

### 3.2.4 简介检索与 tag 级 `sexual`(2026-07-29 A2-1f 落账)

**① `search_intro=`(works 搜索)**

`GET /v1/catalog/works/search` 的 `q=` 缺省**只匹配标题族**(标题 / 别名 / latin / search hint)。传 `search_intro=1` 后**额外**匹配作品**简介正文**。

- **缺省逐字节不变**:索引里现在**确实存了**简介字段,但本面对每个请求显式把可搜索属性钉在标题族上。所以 A2-1f 之前写好的调用方,结果集一行不变——放宽是**请求级 opt-in**,不是索引级的既成事实。
- **简介永远排在标题之后**:简介字段在索引可搜索列表里位列标题族之后,而 Meilisearch 的 `attribute` 排序规则按该顺序给权重——**标题命中永远压过简介命中**,不会出现「正文里提了一嘴的作品挤掉同名作品」。
- **每语言截断 2000 字**:简介是 1-10 KB 的 markdown,而「用户记得的那句话」几乎总在开头设定段。2000 字(CJK 下一字即一词)覆盖前若干段,同时把 ~22.6 万作品的索引增量压在可接受范围;更深处的短语检索不到,是这条取舍**明写的代价**。
- **语言分桶**:简介按语言分桶存(ja / zh / 其它),日文走 jpn、中文走 cmn 分词;简繁两种译文并入同一 zh 桶(**为召回而合并**,你记得哪个版本都能搜到)。
- **正文永不下发**:与索引里其它字段一样,命中后 item 仍是按 id 回库水化的 works 列表行,Meilisearch 文档字段不出 wire。
- 未识别的取值(拼错)= `false`,不是 400:这个开关只会**放宽**结果,退化到窄的一侧才是安全方向。
- ⚠️ **需要 reindex**:简介是索引内容而非查询逻辑,所以本参数在**跑过一次 `reindex-catalog` 之前**只会返回空(索引里还没有简介字节)。

**② tag 级 `sexual`**

`GET /v1/catalog/tags` 的行与 `GET /v1/catalog/tags/{id}` 恒带 `sexual`(bool):该 **tag 本身**属于性内容类别。

- **派生**:规范 tag 经 **A2-0 落的身份锚**(`entity_type=tag`、`source=galgame_wiki`、`link_kind=exact`、`external_id` = wiki tag id)对上 wiki 词表行,取其 VNDB 血统的类别(`cont→content` / `ero→sexual` / `tech→technical`),**只有 `sexual` 有安全含义,故只投影它**。走 id 锚而非名字匹配:两条路今天解出的集合一模一样(901 content / 357 sexual / 267 technical),但名字键会在任一侧改名时**静默失联**。
- **⚠️ 覆盖面 = 与 §3.2.3 同一条款**:只有**映射进该词表**的 tag 才有这条轴。纯 Bangumi / DLsite folksonomy 长出来的规范 tag 上游**没有类别概念**,渲染 `false` —— 这表示**「该 tag 没有这条轴」,不是「已确认安全」**。要做严格门控,请结合 tag 的来源判断,**不要**把 `sexual=false` 当作安全断言。
- 与作品详情 `tags[]` 上那个 **per-edge** 的 `spoiler` / `sexual`(§3.2.3)是两个粒度:那里描述的是「这个作品的这条 tag 边」,这里描述的是「这个 tag 本身」。**§3.2.3 的既有语义本波一字未动。**

### 3.2.5 认领作品的标题供给(2026-07-29 A2-R1 落账;2026-07-30 W1-pre 本体化)

`titles[]`(详情)、`localized{}`(列表 `include=names`)与作品搜索索引的标题族,对**认领作品**(`claimed_by.site=galgame_wiki`)供的是 **wiki 正文的名字**——四个固定语言列 + 别名表。A2-R1 用读时桥接供这些字(catalog 自己的标题表对 87% 的认领作品是空的),**W1-pre(refs/proj/140)把该投影逐字物化进 `catalog_work_title` 并删掉桥**:同一批字,同一形状,来源改为 registry 自己的表,由镜面步跟随 wiki 编辑直至 wiki 表族退役。消费端无感——下表的「标题来源」列描述的是**供给内容**,不再是读取路径。

| 作品形态 | 标题来源 |
|---|---|
| **认领**(`site=galgame_wiki`) | wiki 正文的**四个名称列** → `official` 行(`ja` / `en` / `zh-Hans` / `zh-Hant`);wiki **别名表**每行 → `alias` 行,**`lang=""`**(经镜面步物化进 catalog 标题表) |
| **无正文**(未认领) | catalog 自己的标题行,逐字不变 |

- **一份真相**:认领作品的名字只有一套。桥接时代它只读桥、绝不回落到 catalog 标题行;本体化后镜面步在其属权范围内做**真差分**(增/改/**删**),wiki 编辑删掉的名字这边跟着消失,历史残行被清掉而不是被遮蔽。
- **别名不编造语言**:wiki 不记录别名的语言,故 `lang` 为空串。空 `lang` 不是 BCP-47 标签,落不进 §3.2.1 ① 的 `localized{}`,所以别名**只**出现在 `titles[]` 与搜索索引——这正是想要的:别名可搜可列,但不占某个 locale 的名字位。
- **同名只出一次**:别名字符串与某个名称列**完全相同**时只渲染一次(取 `official` 那行)。
- **`latin`**:wiki 正文没有罗马音列,故桥接行不带 `latin`;catalog 原生行的 `latin` 一如既往。
- ⚠️ **搜索新鲜度**:桥接标题进入搜索索引由 `reindex-catalog` 承载(每日 cron + 上线即时跑一次),所以 wiki 侧改名到「按新名搜得到」之间存在**一次 reindex 的滞后**;详情面/列表面是**读时**桥接,无滞后。

### 3.2.6 作品记录上的 chip `work_count`(2026-07-29 A2-R1 落账)

A2-1b 给 **taxonomy 浏览道与其详情面**发了 nsfw 感知的 `work_count`,但真正被渲染出来的**作品记录上的 chip** 一直没有——于是下游在每个厂牌/tag/引擎 chip 旁边渲染出恒定的「+ 0」。本波补齐:

| 位置 | 键 | 恒出? | 口径 |
|---|---|---|---|
| `works/{id}` 的 `labels[]` | `work_count` | **恒出** | ≡ `works?label_id={id}&claim_state=live` 同 nsfw 下的总数 ≡ `labels/{id}.work_count` |
| `works?include=labels` 的 `labels[]` | `work_count` | **恒出** | **与详情面同一次聚合、同一个数** |
| `works/{id}` 的 `engines[]` | `work_count` | **恒出** | ≡ `works?engine_id={id}&claim_state=live` ≡ `engines/{id}.work_count` |
| `works/{id}` 的 `tags[]` | `work_count` | **仅映射行** | ≡ `works?tag_id={canonical_id}&claim_state=live` ≡ `tags/{id}.work_count` |

- **nsfw 感知 + live 闸**:与 §3.2 的 taxonomy 不变量同一条(含 2026-07-30 wave 146 的 `claim_state=live` 统一)——数字等于**这个调用方**点进去真正能翻到的行数,不是边表行数。chip 与 taxonomy 三道走**同一个聚合函数**,六个面因此不可能各说各话。
- **未映射 tag 无此键**:没有 `canonical_id` 就没有落地页,也就没有数可报(与该行 `canonical_id`/`tier`/`kind` 三键同一省略规则)。已映射的行**一定**带这个键,**包括值为 0**——所以 `work_count` 缺席只意味「这条 tag 没进规范词表」,永远不意味「0 部作品」。
- **`labels[]`/`engines[]` 恒出**:这两处每一行都是可寻址身份,`0` 是一个真实答案;缺键与「消费端解析失败」不可区分,而那正是弃用面那个永久「+ 0」的来源。
- **`labels[]` 同时恒带 `logo_hash`(wave 170)**:厂牌 logo 在图床的内容哈希(与作品封面 `image_hash` 同币种,消费端据此拼 CDN URL),`works/{id}`、`works?include=labels`、`labels`/`labels/{id}` 四处同一个值;空串 = 该厂牌无 logo,与 `work_count=0` 同理是**真实答案而非缺席**。
- ℹ️ **认领作品的 wiki tag 现已计入(2026-07-30,W1-pre)**:`works?tag_id=` 与 taxonomy `work_count` 经 `catalog_tag_source_map ⋈ catalog_work_tag` 找作品,而认领作品的 wiki tag 曾是读时桥接的、不在那张边表里,于是这两个数**系统性偏低**。refs/proj/140 把 92 万条 wiki tag 边物化进了该表,**两个数因此一次性上涨**——这是既知不对称被修好,不是计数错误;数字承诺的**始终**只有「点进去会拿到多少」,现在它能连认领作品一起如实回答了。
- **成本**:每个 facet 每次请求(或每页)**一条批量 GROUP BY**,不是每 chip 一次。**tag 除外(wave 201)**:它改为按主键读上卷表 `catalog_tag_work_count`,现算的那条聚合曾是本服务最慢的查询、且挂在每一次 `works/{id}` 上——口径与新鲜度见 §3.2 的 taxonomy `work_count` 语义块。

### 3.2.7 works 两面的 `claim_state=` 闸(搜索面 = A2-R1 区 C;列表面 = A2-R4)

> 事故驱动:两个产品站把**未发布(draft)**与**未认领**的注册行渲染进了公开页面——搜索结果页与词条成员列表**两条 lane 同一个漏闸**,因为 `works/search` 与 `works` 此前**都没有任何 claim 态过滤供给**;`claimed` 只答「有没有产品站认领」,答不了「能不能给人看」。搜索面先补(A2-R1 区 C),列表面随后(A2-R4),**同名同义**。

`GET /v1/catalog/works/search` 与 `GET /v1/catalog/works` 各有 `claim_state=`:逗号分隔、**封闭词表 `none|live|draft|pending|declined|hidden`**(即 `claimed_by.state` 的六个取值,`none` = 未认领注册行),命中作品须**属于所列任一态**(IN 语义)。**两面逐字同义**——同一套词表、同一份解析、同一句 400 文案,把查询从一条 lane 挪到另一条只是换路径。

- **非法 token = 响亮 400**(与 `sort`/`facets`/`content_rating` 同一姿态)。静默忽略会把「请帮我排除草稿」变成「200 + 满屏草稿」,正是本参数要终结的事故。
- **无缺省 = 不闸**:不传这个参数,结果集与各自加参数前**逐字节一致**。
- **同一道门**:搜索面编译进与 `total`/`facets`/`items` 共享的那一条 Meili 过滤表达式,所以翻完 `total` 页恰好收满 `total` 行;列表面是**单条 SQL 谓词**,进的是与其余过滤器同一个 `WHERE` 合取——两面都不会重演弃用面「总数不过滤、items 过滤」的陷阱。
- **投影与读面同一份定义**:索引里的 `claim_state`、列表面的 SQL 谓词、记录上的 `claimed_by.state`,语义源都是 `model.ClaimStateKey`:`site` 为空或无 `product_work_id` → `none`;已认领而状态列为 NULL → `live`(零回归语义);0/1/2/3/4 → live/draft/hidden/pending/declined;**词表外 → 保守的 `hidden`**。所以 `claim_state=live` 选出的,恰好是详情页写着 `state: "live"` 的那些行。六态是这张表的一个**划分**:每行恰属其一,列全六态 = 不闸。**`pending`/`declined` 于编辑面本体化波(refs/plans/10 §3)加入**:同一条「认领可见度」轴的细分,不是任何产品 status 列的复制;两者与 `draft`/`hidden` 同样是「不要渲染」,`live` 仍是唯一可穿透到产品正文的取值。
- ⚠️ **两面新鲜度不同**:搜索面的 claim 态由 `reindex-catalog`(每日 cron)带进索引,与其余索引 facet 同律;**列表面是读时的库内谓词,claim 态一改立即生效**,不等索引。
- **消费建议**:产品站渲染自家目录的搜索 lane **与**词条成员列表一律传 `claim_state=live`,并**删掉客户端事后过滤**——客户端过滤修不了 `total`,翻页照样丢行。

### 3.2.8 编辑展示轴 `content_limit=` 闸(A2-R5)

> 事故驱动(doc 106 §38):下游把 catalog 的**年龄轴** `content_rating` 映成了自家的**编辑展示轴** `content_limit`,于是 r18 游戏被整体标成 NSFW 从公开面消失——注册表里 claimed live 的 10,929 部有 10,330 部(94.5%)是 `r18`,该站的可索引面因此从 6,117 部塌到 599 部。反向漏也在:该遮的没遮。**根因不是过滤写错了,是把两个不同的问题当成了一个。**

**两轴各答各的**,`claimed_by` 上并排出、两面都可闸:

| 轴 | 键 | 词表 | 回答的问题 |
|---|---|---|---|
| **年龄轴** | `content_rating`(记录顶层) | `all_ages` / `sensitive` / `r18` | **游戏本体**是什么分级——能不能卖给未成年人 |
| **编辑展示轴** | `claimed_by.content_limit` | `sfw` / `nsfw` | 我要**渲染的素材**(封面/截图/简介)能不能摆上公开页——能不能给搜索引擎索引 |

生产口径的实测交叉表(2026-07,claimed live):`sfw × r18` = **5,568**(被误标 NSFW 的主体)、`nsfw × all_ages` = **50**(反向漏)。所以两轴**互不是对方的放宽或收紧**,谁也替代不了谁。

**投影(单一语义源 `model.DisplayLimitKey`)**:

- **认领作品**(`site` 非空且有 `product_work_id`):读 **wiki 正文** `galgame.content_limit`,`nsfw` → `nsfw`,其余(含空值、无正文、词表外的值)→ `sfw`。这一支**完全不看** `content_rating`——编辑判定是人设的,注册表不去二次猜它。保守方向在这里是**保持可见**:过度遮蔽正是事故本身。
- **未认领(bodyless)**:没有编辑判定可读,只剩年龄轴——`r18` → `nsfw`,其余 → `sfw`(与下游既有的 `contentLimitFromRating` 一致,所以未认领行的行为**一字未变**)。
- 二值**全划分**:每行恰属其一,两个值都列 = 不闸。

**参数**:`GET /v1/catalog/works` / `GET /v1/catalog/works/search` / 三个日历桶各有 `content_limit=`,逗号分隔、**封闭词表 `sfw|nsfw`**、IN 语义、**三面逐字同义**(同一份解析、同一句 400 文案)。

- **非法 token = 响亮 400**(`content_limit must be a comma-separated subset of sfw, nsfw`)。⚠️ 注意 **wiki 面同名参数的第三个 token `all` 在这里是 400**:本面「不传」已经等于「两个都要」,再收 `all` 等于默许两个参数是同一个参数——而它们不是。
- **无缺省 = 不闸**:不传即与本波前**逐字节一致**。
- **与 `nsfw=` / `claim_state=` 正交**:三道门编进**同一个**合取(搜索面是那一条 Meili 表达式,列表面/日历是同一个 `WHERE`),所以 `total`/facets/items/月桶计数/`meta` 框永远在同一道门后面;开一道门绝不会把另一道带开。
- **日历另加一条**:该闸参与**分桶成员判定**,因此也进 **ETag 人口键**(`nsfw × olang × content_limit`)——否则两个不同人口会共用校验子而串味。
- ⚠️ **三面新鲜度不同**:搜索面的 `content_limit` 由 `reindex-catalog`(每日 cron)带进索引;**列表面与日历是读时的库内谓词,编辑一改立即生效**。
- **消费建议**:要做**可索引面 / SEO 面**的站点,闸 `content_limit=sfw`,并把渲染判定读 `claimed_by.content_limit` —— **不要**拿 `content_rating` 当展示门;年龄门该走 `nsfw=` / `content_rating=`,那是另一回事。

### 3.2.9 works 列表的审核队列视图 `status=`(186a)

`GET /v1/catalog/works` 新增 `status=`,**封闭词表 `live|pending`**,不传 = `live`:

- **`live`(缺省)** —— 今天的公开人口(注册行 `status=live`),**与本波前逐字节一致**;不传这个参数的调用方一个字都不受影响,也不会读你的 `Authorization` 头。
- **`pending`** —— **审核队列视图**:该租户「已投稿、等裁决」的作品(`claimed_by.state=pending`)。

**为什么词面是 `pending` 而不是注册行的 status 值**:注册行的 status 轴是 `live|stub|merged`,**没有任何审核态**——用户投稿铸造出来就是 `status=live`,「等谁裁决」这件事记在**认领轴** `claim_state=pending` 上(即 admin 面 `PendingClaims` 队列读的那一列)。`stub` 是导入器「元数据不达标」的堆,**构造上未认领**因而不属于任何租户;`merged` 是 404 墓碑。故本参数选的是**审核员真正工作的那个态**,顺带把注册行的 status 轴从 live 放宽到 `live ∪ stub`(投稿后被降级为 stub 的行仍是本租户要审的),**永不含 `merged`**。

**双凭据(Phase-2 裁定 6 的传输形态)**:开放 API 的唯一凭据是**机器键**,它只说明「哪个应用在调」,说明不了「哪个人在调」。审核是**人的权限**,所以队列视图额外读 `Authorization: Bearer <审核员本人的 OAuth access token>`,机器键此时走 `X-API-Key`。该 JWT 是 **OPTIONAL** 的(`middleware.OptionalJWT`,永不拦截),所以既有调用方——包括仍把 API key 放在 Bearer 槽的**旧单凭据形态**——一律不受影响。

**四道门,任一不过即 403**(顺序即代码顺序):

1. 没有已验证的用户身份(只有机器键)；
2. 令牌未绑 client、client 未注册、或该 client 未绑 `catalog_site` —— 没有可钉的租户；
3. 令牌签发自**第三方应用**(`oauth_clients.owner_user_id` 非空)—— 第三方 UI **永不是审核面**,后面站着谁都一样(186b 同一条封顶);
4. 令牌 roles 不持 `catalog.claim.review`。

**租户钉死**:队列只出**该令牌 client 自己的 `catalog_site`**,与用户写面 `userActor` 同一套推导——租户不从查询串来。`site=` 可以**重复**自己那一个值,**指名别人的站是 403**(不是静默改回自己):审核员若拿着「别人的队列」的请求收到自己的队列,页面上每一行都会被误读。**平台级队列不在开放面**,留在员工面(`/api/v1/admin/catalog`),那后面站的是员工 JWT 而不是某产品的令牌——而员工面自 wave 187b 起也过同一条 client 闸:员工的令牌若签发自第三方应用,同样 403。第三方应用没有「换个面就能审」的迂回。

**其他条款**:

- **拒绝永远是响亮的**,绝不静默降级回 `live` 集合——审核员拿到空页会判定「队列是空的」,那是本视图唯一不能给的错答案。
- **`status=pending` 与 `claim_state=` 不可同传**(400):本参数**就是**那道 claim 闸,同传等于对同一个问题要两个答案。
- **词表外 token = 响亮 400**(`status must be live|pending`),与 `sort`/`claim_state`/`content_limit` 同一姿态。
- ⚠️ **MCP 面拿不到第二凭据**:`catalog_works_list` 工具透传本参数,但 MCP 传输只带**一个**凭据(Bearer 槽里的 API key),故经 MCP 调 `status=pending` **必然 403**。参数照样透传,是为了让拒绝来自端点本身,而不是被静默丢掉的过滤器。
- 🔒 **本视图永不进共享缓存**(wave 213 波 3):`status=pending` 的响应带 `Cache-Control: private, no-store`,而 `status` 缺省 / `live` 仍是原来的 `public, s-maxage=60, …`(逐字节不变)。理由是这条道**随第二凭据变化**,而共享缓存只按 URL 做键——两个租户的审核员请求的是**同一个 URL**,缓存住任何一份都会把 A 站的待审队列发给 B 站。这不是「拿到旧页」,是跨租户泄露,所以选的是 `no-store` 而不是缩短 `s-maxage`。
- ℹ️ **本闸不是「pending 行的封锁线」**:`claim_state=pending` 作为普通过滤器**早已开放**(A2-R4,见 §3.2.7),`status=pending` 提供的是**开箱即用、按租户钉死的队列视图**与它的权限门,不收窄任何既有参数。

### 3.2.10 NSFW 能力闸(wave 213 波 3 加,2026-08-25 退役)

**这道闸已经不存在了。** `nsfw=` 参数仍然由调用方传,而**任何持 key 的 app 传 `nsfw=true` 都直接放行**——不再有能力位、不再有审批、不再有 403。挂在 `/v1/catalog` group 上的 `requireNSFWCapability` 与 v2 的 `NSFW_CAPABILITY_REQUIRED` problem 一并摘除。

- **参数语义一个字没改**:缺省(不带 `nsfw`)= sfw 投影,显式 `nsfw=true` 才含 r18,真值集(`1` / `true` / `yes`,大小写与首尾空白不敏感)仍由 handler 的同一个解析器认定,`content_rating=r18` 仍须配 `nsfw=true`(参数一致性校验,不是能力检查)。**不带 `nsfw` 的请求逐字节不变**——闸在时如此,闸走了也如此。
- **原判据(存档)**:凭证的 `nsfw_allowed` = `developer_api_keys.nsfw_allowed` **AND** `oauth_clients.dev_nsfw_allowed`,两级由管理员授予。两列留在库里且默认值已恢复,Go 侧字段与门户的申请入口均已删除。历史遗留的 `galgame:nsfw` scope 从来不由这道闸执法,现在同样什么都不执法。
- **退役影响面**:闸在时有效能力位为真的恰好是三把首方 S2S key,其余 30 把第三方 / 开发用 key 全为假——它们此前带 `nsfw=true` 吃 403,现在直接拿到 r18。首方调用不受影响(它们本来就放行)。
- **不在这条链上(历史)**:`/v1/catalog/stats`(挂在 group 之上,免凭据)与 `/v1/news`(另一个 group)从来不受这道闸约束。

### 3.5 稳定性承诺

- 已发布字段不删不改语义;只做**向后兼容**的新增。
- 公开 `content_limit` 语义统一(见 [06 §11](./06-security-compliance.md));各端点默认 = `sfw`。
- catalog 面的实体 ID 全局稳定,合并只产生 redirect,永不复用。~~`w`/`p`/`n`/`b`/`c` 前缀~~(superseded,2026-07-15 步骤 03 裁定 2:公开 id = 纯数字——与 galgame 面已冻结的 `catalog_work_id` 数字形态一致,路径已按实体类型分命名空间)。公开线源键 = 站点真拼写(`erogamescape`)。内部注册表键早期误拼作 `erogamespace`,曾靠投影层映射到 wire;注册表已改正,投影层随之删除,lookup 仍保留双拼容错。

**演进条款**(step 07 落账,Phase 2「查询灵活性」引入时形式化;五条共同定义"什么样的改动是加性、什么样必须升版本"):

1. **加法优先,永不改语义**:已发布字段的名称、类型、含义与 null 语义一律冻结;演进只能是**新增**可选字段 / 可选查询参数 / 新端点。任何"改"都不是加性,一律走破坏性变更流程(第 3 条)。新增可选参数(如 `include` / `fields`)与新增可选响应键,对既有客户端逐字节无影响——缺省响应恒等于冻结契约。
2. **客户端「必须忽略未知字段」= 契约条款**(升格):公开响应可能在任何时候新增字段;**合规客户端必须容忍并忽略它不认识的字段**。对称地,服务端对 `fields=` / `include=` 里的未知名**静默忽略、绝不 400**(双向前后兼容:老客户端遇新字段不炸,新客户端拼错字段名不炸)。这条对侧承诺正是"加法优先"能成立的前提——加字段对所有正确实现的消费者都无破坏。
3. **破坏性变更 = `/v2` 并行**:确需改语义 / 删字段 / 改类型时,新增 `/v2` 与 `/v1` **并行运行**,旧版打 `Deprecation` / `Sunset` 响应头 + 门户 changelog 公告 + **不少于 12 个月**的迁移窗口;窗口内 `/v1` 不下线、语义不动。
4. **内部面 = 公开契约的试验缓冲层**:新字段 / 新形状先在内部 S2S / 站点读面消化验证,形状稳定后再投影到公开面冻结。公开面永远是内部契约的**精选滞后投影**,不承载未经内部实战的实验形状——这样绝大多数迭代压力被内部面吸收,公开契约的破坏性变更趋近于零。
5. **新数据源 = 加键,新媒介 = 加面**:新增第四 / 第五源评分或外部锚,是在 `refs` / `scores` 等**键控对象**上加键(这正是把它们设计成键控对象而非并列标量字段的本意);新增媒介(manga / novel…)是加新面 `/v1/<medium>/*`。两者都是加性演进,天然不触碰既有契约。

### 3.6 错误体 = 房内信封(2026-08-08 wave 190 更正)

公开面的错误响应与生态其余部分同形,**不是** RFC7807:

```
HTTP/1.1 404 Not Found
Content-Type: application/json

{"code": 4, "message": "资源不存在"}
```

`code` 是房内错误码(`pkg/errors`),`message` 人读,可选 `data` 只在错误自带结构化载荷时出现(如认领冲突 409 回带占位方的身份)。**服务从第一天起就是这么答的**——`huma.NewError` 是包级变量,catalog 的每个运行时挂载点(`handler/admin.go`、`s2s.go`、`user_cover_votes.go`)在注册任何 op 之前都会把它换成房内信封。

> **但冻结 spec 一直写的是别的东西。** 唯一漏掉这次替换的调用者恰好是 spec-only 路径 `SetupCatalogPublicSpec`,于是它把 Huma 库存的 RFC7807 `ErrorModel` + `application/problem+json` 冻进了**已发布契约**,25 条 op 全中。也就是说:在此之前,门户文档和任何据此生成的 SDK,其错误处理写的是一个**任何部署都从未发出过的**响应体。
>
> 证伪方式是把冻结 YAML 与生产二进制在 `/v1/catalog/openapi.json` 上服务的 spec 逐字段对拍:错误信封是两者**唯一**的分歧,现已消除。
>
> 这一条**不走 §3.5 第 3 条的 `/v2` 并行流程**,因为它不是语义变更:线上行为一字未动,变的只是文档不再说谎——把它塞进 12 个月迁移窗口,只会让文档多骗一年。oasdiff 仍按破坏性记账(媒体类型收窄),故在 `docs/catalog/public-openapi-breaking-ignore.txt` 具名声明。**本条不伴随任何部署**:线上 spec 早已是正确的那份。

### 3.7 实体名的多语言面 `localized{}`(2026-08-08 wave 191 加法)

三张别名表 `catalog_character_alias` / `catalog_name_alias` / `catalog_label_alias` 是**同一个形状**:`(owner, name, lang)` 唯一、`kind`、`latin`、`is_primary_for_locale`。这是一行一个 (实体, 名字, 语言) 的标准建模,且**早已有供给**——bangumi 中文名波(`internal/jobs/bgmzhnames`)三条车道齐灌,实体简介机翻链的术语表还反过来读它,以保证译文里的人名与词条名一致。

**缺的一直是读面。** 厂牌与名义把行压成裸字符串、按拼写去重,`lang` 就此丢失;角色面**根本不发别名**。于是「这个角色的中文名是什么」在公开 API 上无法回答,尽管答案就在表里。而 `name{}` 分桶让情况更糟——它看起来像个答案:`{"ja": "…"}` 没有 `zh` 键,任何消费者都会读成「没有中文名」,而它实际的意思只是「记录名恰好是日文」。

本波在 `characters/{id}` · `names/{id}` · `labels/{id}` 三个记录上补齐同一组键(**全部为加法,缺省响应的既有键逐字节不变**):

| 键 | 含义 |
|---|---|
| `display_name` + `lang` | 记录名与它自身的 BCP-47 语言标签。`display_name` 恒非空;`lang` 未记录则省略。厂牌本已有这两键,本波补给角色与名义 |
| `localized{}` | **按 locale 取名的 map**,键 = 规范大小写的 BCP-47 标签(`zh-Hans`/`ja`/…),值 = `{value, kind, machine}`(`machine` 为 wave 209 加法,false 时省略)。恒出,无本地化名为 `{}`。**`kind` 分两族不相交词表**:实体名 = `translation\|spelling_variant`,**作品标题 = `official\|alias\|abbreviation`**(wave 212 波 A 起 works 也发此字段) |
| `aliases[]` | 也叫作什么(去重、剔除 display_name)。厂牌与名义本已有,本波**补给角色**——此前一条都不发。**wave 209 起从扁平字符串升级为对象数组 `{value, lang, kind, machine}`**(按 value+lang 去重,breaking,见下方 wave 209 节) |

三点设计交代:

1. **`localized` 是 map 不是数组。** 这个字段的全部工作就是取一次 `localized[myLocale]`,map 让它成为一行零遍历;数组会让每个消费者各写一遍扫描、各写错一遍(尤其 `zh-Hans` vs `zh` 的前缀匹配)。键是**开放词表**——哪些 locale 存在是数据不是枚举,读面不做任何值层面的归一化。

   **唯一的例外是标签自身的大小写(2026-08-08 修正)。** 键按 BCP-47 规范大小写发出:主子标签小写、四字母文字子标签首字母大写、两字母/三数字地区子标签大写(`pt-br` → `pt-BR`,`ZH-hans` → `zh-Hans`)。这不是发明数据——按标准自己的相等规则 `pt-br` 与 `pt-BR` 本就是同一个标签,而库里存的确实是 `pt-br`,不折叠则查 `pt-BR` 的消费者必然 miss。同理,**不成其为语言标签的值一律不进 `localized`**(库里有一行 `lang` 字面写着 `日语`):一个任何 locale 协商都匹配不上的键不是词表开放,是泄漏。这类行仍进 `aliases[]`,它在那里只是一个拼写。校验的是标签的**形状**而非 IANA 注册表——注册表校验会拒掉源日后可能填入的冷僻标签,而这里的职责是挡住非标签,不是裁定哪些语言存在。
2. **`localized` 与 `aliases[]` 回答的是两个不同问题,故对 display_name 的处理相反。** `aliases[]` 问「还叫什么」,剔除 display_name;`localized` 问「在 X 语言里叫什么」,**保留**与 display_name 同形的值——「中文名是美坂栞」在它同时也是记录名时依然成立且有用,而丢掉它正是分桶暗示「无中文名」的成因。同 locale 多行时的择优 = **source 行恒胜 machine 行(wave 209 起的第一层)**,其后 `is_primary_for_locale` 优先(bangumi 波刻意设置、每 owner 至多一行且从不改选),其次 translation 优于 spelling_variant,再次按 `(name, id)` 到达序;`kind=search_hint` 行两个投影都不入(其 kind 契约即「只供检索、永不展示」)。
3. **`localized{}` 对机翻名的规则:source 恒胜,machine 只可填缺(🔄 wave 209 修订;wave 178/191 原规是结构性拒收)。** 三张别名表自 wave 195 起携 `source_id` / `provenance` / `mt_model` 三列(与 `*_intro` 表同构);机翻行以 `provenance=machine` 落库,且**永不占 `is_primary_for_locale`**。wave 191-208 期间读面在选举 `localized{}` 时**跳过一切 machine 行**——当时的理由:名字译错的成本远高于简介(下游站点、搜索索引、URL、用户收藏会一起钉死一个错名,且它看起来毫无异常),故机翻名只入 `aliases[]`。**wave 209(2026-08-18 裁决)把这道闸从「拒收」改为「填缺」**:machine 行**仅当该 locale 没有任何 source 行时**才占槽,占槽时带 `machine: true`;同 locale 只要有 source 行,machine 行仍然落选(与表③ intro 块的 shadow-never-delete 同规)。改判动因:56,149 个角色(zh 名存量的 28.4%)**只有**机翻中文名,旧闸下公开面消费者一个也拿不到——而「从 `aliases[]` 里自行猜哪条是 zh 机翻名」在扁平字符串数组上结构性不可实现。防错名钉死的保险仍在:machine 行永不占 primary、选举层 source 恒胜,且槽位带旗标——要挡机翻的消费端按 `machine` 过滤即可,不挑的消费端拿到的永远是「有权威名用权威名,没有才用机翻」。另注:wave 178 的**纯汉字名直通行**(字形即中文写法,`provenance=source`、与 display_name 同形)一直正常参与选举——见第 2 点的同形保留规则。

4. **`localized` 永远是可选的,消费端必须有回退链——这是永久契约,不是过渡期妥协。** 本领域不存在 100% 覆盖,所以下游要按 `localized[myLocale]` → `display_name` → `latin` 逐级回退,**且不得把缺失渲染成空白或「暂无」**。

   2026-08-08 生产实测(与聚合翻译轨同口径核对一致):

   | 实体 | 总数 | 有 zh 名 | 覆盖 |
   |---|---|---|---|
   | character | 228,051 | 20,729 | 9.1% |
   | credit_name | 119,477 | 1,531 | 1.3% |
   | label | 39,653 | 610 | 1.5% |

   这三个数字**不是管线没跑完的中间态,而是各自不同的原因**,消费端的预期要分开建:

   - **character** 的低覆盖是暂时的。当前的 2 万条来自 bangumi 词典(wave 175),词典天然只覆盖一小撮;角色名全量补齐是 wave 178 P1,角色简介链收官后启动。**但更重要的是 9.1% 严重低估了实际可展示率**——没有 zh 名的角色里有 81,700 个(占全部角色 42%)名字是纯汉字无假名,「藤原芳佳」这类在中文界面**原样展示就是对的**。所以回退链的第二级 `display_name` 不是降级显示,而是对近半数角色的正确答案。P1 跑完后可中文展示率将从 9.1% 跳到 50%+,残量靠人审逐步爬升。
   - **credit_name 的低覆盖是 by design,不会改善。** 那是真人姓名——「新島夕」「いとうのいぢ」这种:汉字名不该"翻译",假名笔名多数也没有公认中文写法。**不要把这里的 1.3% 当缺陷上报**,也不要在 UI 上为 staff 名预留"中文名待补"的位置。
   - **label** 同为词典覆盖,与 character 同因。

   推论:任何"等 `localized` 覆盖率达标再上线"的下游计划都建立在错误前提上。该字段从上线第一天起就该按稀疏字段消费。

`name{ja,zh,other}` 分桶就此进入**弃用**:它的形状与语义不符(见上),替代物 `display_name`+`lang`+`localized` 已在同一批记录上就位,消费者可在它被移除**之前**完成迁移。

**wave 193 补齐:`names/{id}` 的 `siblings[]` 同样发 `display_name` + `lang`。** 191 把这组键补给了三个实体记录本身,却漏了 sibling 行,而 sibling 名正是分桶的最后一处消费点——不补,下游就无法在移除之前完成迁移。**加法,不需要迁移。**

### 🔴 wave 194(2026-08-08):`name{}` 分桶已**移除**

`PublicName` / `PublicCharacter` / `PublicSiblingName` 的 `name{ja,zh,other}` 键**不再下发**,因此 `names/{id}`、`characters/{id}`、`lookup`、`lookup/batch` 四条端点的响应少了这个键。**这是本 spec 的破坏性变更**,已在 `docs/catalog/public-openapi-breaking-ignore.txt` 具名声明。S2S 读面(`/api/v1/catalog/{names,characters}/{id}/works`)的 `NameHead` / `SiblingName` / `CharacterHead` 同批移除,改发 `display_name` + `lang`。

**expand 早于 contract 三波,且下游确已迁完才动手**:191 铺 `display_name`+`lang`+`localized{}`、192 修 locale 键、193 补 sibling,全部先行部署;唯一解码分桶的仓(kungal forum)完成迁移并上线后,才执行本波移除。patch 从不解码分桶(其 lookup 只读 `work`+`claimed_by`)。

替代读法就是那一行契约:**`localized[locale] ?? display_name ?? latin`**。

关于这次迁移,两点消费端需要知道的事实:

- **`display_name` 与分桶的值逐字节相同。** 读面按记录自身的 `lang` 把**那一个**名字投进**唯一一个**桶(`zh*`→`zh`,`ja*` 与空→`ja`,其余→`other`),所以任何时候最多一个桶有值。也就是说,那段「按偏好顺序挑第一个非空桶」的代码,拿到的**永远就是 `display_name`**。切过去是形状变更,不是行为变更,渲染结果不变——这也是本波敢在一个窗口内直接移除的依据。
- **真正拿到的新能力是分桶说不出的三件事**:① 日文名若有中文译名在库,分桶只发 `{"ja": …}`、`zh` 键缺席,已存在的译名**完全不可达**(这正是 `localized{}` 存在的理由);② `zh-Hans` 与 `zh-Hant` 塌进同一个 `zh` 桶;③ `lang` 为空的记录被投进 `ja` 桶,等于断言了一个源从未声明的语言。

`siblings[]` 在 wave 191-208 期间**刻意不发 `localized{}`**(顾虑是按 sibling 数量各发一次别名查询);**wave 209 起补齐**——读面对 sibling id 集合做**单次批量**选举,per-sibling 查询的顾虑不复存在,sibling 行自此与实体记录同携 `localized{}`。

### wave 209(2026-08-18):公开面名字原语**终局化**

此前一个实体名按投影不同有三种形状:detail 面 `display_name`、花名册/credits/relations/search hit 用 `name`、别名是裸 `[]string`。本波把 `/v1/catalog` 上**凡出现实体名的投影**统一为同一组三字段,并让 `localized{}` 覆盖到此前缺它的每一处:

- **统一原语**:`display_name` + `latin`(有则)+ `localized{}`;渲染规则恒为 **`localized[locale] ?? display_name ?? latin`**,按投影分支的渲染逻辑就此作废。覆盖面:实体 detail、labels 列表行、作品详情 `characters[]`(花名册)及其 `voices[]`、`credits[]` 条目、`names/{id}` 的 `siblings[]`、label `relations[]`、`via_label`、实体 search hit。
- **🔴 更名(breaking)**:search hit / 花名册角色与 `voices` / `credits` 条目 / label `relations` / `via_label` 的 `name` → `display_name`——同一字符串同响应换键,无值失联。
- **🔴 `aliases[]` 对象化(breaking)**:`{value, lang, kind, machine}`,按 value+lang 去重;裸串版本答不了「哪门语言」「谁写的」两问。
- **intro DTO 合并**:work/character/name/label/tag 五面共用一个 `PublicIntro{lang, intro, source, machine}`;tag 的 `machine` 恒 false(`catalog_tag_intro` 无 provenance 列)。
- **加法**:`PublicWorkBrief`(relations / series_siblings / lookup / tag·label·name·character 的 works 子列表)与 works 类 search hit 补上作品名字块(与 works 列表 `include=names` 同一选举;wave 212 波 B 起该块是 `latin` + `localized{}`);name/character/label 类 hit 补 `localized{}`。
- **机器填缺**:见第 3 点修订——machine 行只填无 source 行的 locale 槽,带 `machine: true`。

全部破坏性行在 `docs/catalog/public-openapi-breaking-ignore.txt` 逐条具名;契约总述另见 `docs/catalog/01-service-and-contract.md` §1。

### wave 210(2026-08-19):作品**标题**也进机器/源二分

209 做完了实体名,标题还差一步。本波给 `catalog_work_title` 加上和别名表、简介表同一根 `provenance` 轴(0=source / 1=machine),并把它露到公开面。

- **🔴 作品名字块的每个 locale 位对象化(breaking)**:从裸字符串变成 `{value, machine}`,`machine` 仅为真时出现。该块当时还是四个固定产品键,已于 wave 212 波 B 整体退役,但这一位旗标原样留在了接手它的 `localized{}` 条目上。逐条声明见 `public-openapi-breaking-ignore.txt`。
- **加法**:详情面 `titles[]` 每行补 `machine`(仅为真时出现);S2S 读面 `WorkTitle` 同补。
- **选举**:见 §3.2.1 ① 的定序修订——**源标题永远赢下自己的 locale**,机翻只填空位。要「只看人写的标题」的消费端按 `machine` 过滤即可;要「locale 里有个能读的名字」的消费端什么都不用做。
- **为什么需要这个旗标**:本波开始把日文原名机翻成中文标题写进目录。没有旗标,「发行商真的发过的中文名」和「我们翻的」在线上无从分辨,消费端也就没法选。供给侧同时补齐了两条**源**泳道(bangumi `name_cn`、VNDB 非机翻中文 release 标题),它们写的是 provenance=0——机翻只覆盖这两条都供不出的残量。

### wave 212(2026-08-19):works 补齐名字原语,四槽兼容层退役

209 统一了每个带名实体,210 给标题加上 provenance 轴——但**作品本身**从未拿到 209 的那组原语,它此前只有专为论坛留的四槽 `names{ja-jp, zh-cn, zh-tw, en-us}`。四槽是有损的:`lang` 为 ko/ru/vi 或未标注的标题没有槽位可去,构造上不可达。**A 波**给作品补上同一组原语并与四槽双发,**B 波**在论坛与 moyu 两家消费端迁完并上线后把兼容层整层删掉。

**A 波(加法)**

- **四个作品投影补 `latin` + `localized{}`**:`works/{id}` 详情(**恒带**,空为 `{}`)、`works` 列表行(**仅 `include=names`**,未点名时**整键缺席而非发 `{}`**,因为「没请求」和「没有」是两个断言)、作品 brief(与标题同一次查询,恒带)、works 类 search hit(只补 `localized{}`,`latin` 本已有)。
- **选举沿用四槽那一套**:同一批标题行、同一个 `(provenance, kind, id)` 定序、同一个 first-row-wins 扫描,源标题恒胜机翻、official 胜 alias,机翻占空位时带 `machine: true`。差别只在键——`localized{}` 的键是**任意 canonical BCP-47 tag**(`zh-hant` → `zh-Hant`,**裸 `zh` 保留、不猜字形**)而不是四个固定产品键,`lang` 为空或不成其为标签的行不入(它们仍在 `titles[]` 可列可搜)。
- **⚠️ `kind` 是两套不相交的词表**:实体名说 `translation|spelling_variant`,**作品标题说 `official|alias|abbreviation`**。同一个 `PublicLocalizedName` 结构承载两族,按实体类型读,别拿一套词表去校验另一套。
- **`latin`**:顺序扫标题行,取第一条 `title == display_name` 的行的 `latin`;无此行或该行无 latin 则整键缺席。
- **同波两处加法件**(relation-graph 节点的 `localized{}` 连同 `name`→`display_name` 改名已由同日独立波先行完成):`labels` 列表行补 `aliases[]`(与详情同语义,和该行 `localized{}` 共用同一次批查,不增查询)· 角色 `traits[]` 补 `localized{}` + `group_localized{}`(键恒 `zh-Hans`,`machine` 如实映射 `name_zh_provenance`)。
- **文档注记**:`tags/{id}` 的 `intros[].machine` **恒 false**,因为 `catalog_tag_intro` 没有 provenance 列——该旗在这一面记的是「未知」,不是「人写的」。

**B 波(删除,🔴 均为 breaking,逐条声明在 `public-openapi-breaking-ignore.txt`)**

- **四个作品投影的名字块整体删除**,渲染改用 `localized[locale] ?? display_name ?? latin`。公开面自此**不存在任何 `ja-jp` / `zh-cn` / `zh-tw` / `en-us` 键**。
- **`works` 列表的 `intros` 从四槽对象改成数组** `[{lang, intro, source, machine}]`——即 `works/{id}` 自 wave 209 起发的那个 `PublicIntro` 数组:一语言一元素,同一批行、同一套选举,两面对同一部作品给出逐字相同的数组。`include=intros` 未点名时整键缺席,与此前四槽块缺席同义。
- **详情面的简介键 `intro` 更名为 `intros`**——数组、元素、次序都不变,只换键,与列表块及 label / tag / character / name 各面对齐。**S2S 读面 `/api/v1/catalog/works/{id}` 的 `intro` 是另一张面,不动。**
- **角色 `traits[]` 的扁平 `name_zh` / `group_zh` 删除**,由 A 波补上的 `localized["zh-Hans"]` / `group_localized["zh-Hans"]` 接管(同一个串,另带 provenance)。**S2S 面的 `name_zh` / `group_name_zh` 不动。**
- **`include=` 的六个 token 拼写刻意不动**:`names,intros,labels,ratings,covers,refs` 逐字照旧。moyu 全站标题都走 `include=names`,把 token 跟着块一起改名会让它全站渲染空标题、而两侧都不报任何错——这是本波唯一一个会静默的失败模式,故有测试专门钉住这六个拼写。

### 3.8 playtime 面:用户令牌认证的公开写面(wave 207 补文)

平台的**第二个公开面**,端点清单见 §3.2「playtime 面」。它与 catalog 面同进程(`cmd/catalog`)挂载,但**除了 `/v1` 前缀之外几乎没有共同点**——写的是**调用方自己的**游玩记录,不是注册表;凭据是**用户的访问令牌**,不是机器 API key。

**认证链(与 catalog 面完全不同,这是本节最要紧的一句)**

| | catalog 面 | playtime 面 |
|---|---|---|
| 凭据 | 机器 API key:`X-API-Key: nm_live_…`(或 `Authorization: Bearer nm_live_…`) | **用户** OAuth 访问令牌:`Authorization: Bearer <access token>` |
| 主体 | 一把 key = 一个应用,与人无关 | 令牌里的**那一个用户**;写谁的行由令牌推导,请求里**没有** uid 参数 |
| 门 | key 有效性 + tier + 日配额 | JWT/JWKS 验签 → 令牌须**带用户身份**,否则 401;须**绑定 OAuth client**(`client_id`),否则 403 |
| scope | `catalog:read` | **无。**`playtime:read` / `playtime:write` 不再判定。任何已开通用户登录的应用,用它签出的用户令牌即可读写该用户自己的时长。这两个 scope 仍可出现在同意页(旧授权 URL 不 400),但缺它们不再 403 |
| 限流 | key 级配额 | **每 (client, user) 每分钟 120 次**,超出 429;Redis 不可用时**fail-open** |

- **令牌必须绑 client 不是形式主义**:记录按 `(user, work, client)` 三元组落行,`client_id` 是主键的一部分。没有它就没有「这条是哪个 App 报的」,读面的跨 App 折叠也就无从谈起——所以无 client 绑定的令牌(例如站内直接登录换来的那种)在本面一律 403,而不是悄悄写进一个空 client。**这条三元组也是「能签用户的应用永远删不掉」的根据**:`catalog_user_playtimes` 在 `kun_catalog`,主库那边的删除守卫结构上够不到它,只能改判能力,见 [§3.11](#311-应用生命周期归档删除与结算名册2026-08-29)。
- **一个用户写不到别人**:五条 op 没有一条接受「目标用户」参数。

**数据语义**

- `minutes` 是**绝对累计值**,取值 `0`-`60000`(1000 小时封顶,越界 400)。重发同一个数是 no-op,故所有写调用**天然幂等、可安全重试**——客户端崩溃后重放整份库存是预期用法。
- `status` 封闭词表 `playing` / `finished` / `dropped` / `on_hold`,缺省 `playing`;拼错 400。
- `last_played_at` 可选,RFC 3339;格式错 400。
- 作品必须是 **LIVE 注册行**——不存在、软删、非 live 一律 404(不给「先写着,等作品建好」的语义)。
- 同一用户的多个 App **并排存**、读时才折叠(`minutes` 取 MAX、任一 finished 即 finished、`last_played_at` 取最新),因此换设备/换客户端**不会覆盖**旧数据。

**它如何回到公开面(以及如何不回)**

- **只有 `finished` 的行进公开聚合**(`playing`/`dropped`/`on_hold` 与 `<10` 分钟的行一并剔除):聚合作业先按用户折叠(同用户多 client 取 MAX),再对用户取**中位数**,**至少 3 个上报用户**才写出一行,落 `catalog_work_playtime` 的 `nextmoe` 源。它出现在 catalog 面 `works/{id}` 的 `playtimes[]` 里,与 vndb / erogamescape 的同名块**同形同源键规则**(`source=nextmoe`)。
- **个人行永不上公开面**:公开面看得到的只有那个中位数与上报人数;谁玩了多久只有本人的用户令牌读得到。

**与 MCP 的关系**:playtime 面**刻意不进 MCP 工具面**,理由见 [09 §4](./09-mcp-server.md)。

### 3.9 key 上的 scope:自助集(授权制已于 2026-08-25 退役)

一把机器 API key 能带哪些 scope,现在只有**两档**,判据在 `devapi.selfServiceScopes` + `devapi.checkMintScopes`:

| 档 | 名单 | 谁决定 | 铸 key 时 |
|---|---|---|---|
| 自助 | `selfServiceScopes` = `catalog:read` / `store:read` | 调用方自己 | 控制台直接勾 |
| 不可得 | 其余全部(`news:read` / `galgame:nsfw` / `galgame:write` / `galgame:read` …) | —— | 400 `ErrScopeNotAllowed` |

**`store:read` 于 2026-08-26 进自助集**:它出生时(store 波,2026-08-25)是授权制的第二个租户;授权制整档退役后申请队列不复存在,自助勾选成为持有它的唯一路径。`/v1/store` 的请求时 scope 检查原样保留——没有它的 key 调分销面仍是 403。

**`galgame:read` 于 2026-08-18 退出自助集**:`/v1/galgame` 面在 wave 146 整体退役为 `410 Gone`,该 scope 此后不被任何活路由消费。已发出的旧 key **不动、不失效**(那个 scope 本就打不开任何东西);两条铸 key 路径的空 scopes 默认改为 `[catalog:read]`。

#### 授权制这一档的退役(2026-08-25)

2026-08-18 到 2026-08-25 之间存在过第三档「授权制」:`grantableScopes` = `news:read`(store 波曾在退役前一天把 `store:read` 一并列入),由用户在门户提交申请、平台在管理台审批,批准后才能自助勾选。**整档连同它的表、端点与策略位一并退役**,理由是它已经无事可决:`/v2/news` 自 v2 面上线起就是匿名公开的,同一批内容在另一条路径上早已人人可读,而 `/v1/news` 还在对没走过审批队列的 key 回 403。

退役后:`/v1/news` **仍要求一把有效的机器 key**,但**任意 scope 均可**——它不再检查 scope,与请求根本不带 scope 等价。`ScopeNewsRead` 常量保留在代码里,因为存量 key 的 `scopes` jsonb 里仍有这个字面量,读历史需要这个名字;那些 key **不动、不失效**,那个字符串现在什么都不执法。

一并删除的东西,备查:

| 已删 | 曾经是什么 |
|---|---|
| 表 `devapi_scope_applications`(`devapi.ScopeApplication`) | 每 `(user_id, scope)` 恰一行的申请单;`DROP TABLE IF EXISTS`,丢弃 3 行(1 approved / 2 pending,其中一条迟至 2026-08-26、退役已在进行中时才提交) |
| `POST` / `GET /api/v1/dev/scope-applications` | 自助提交与查看自己的申请 |
| `GET` / `POST …/approve` / `POST …/decline` `/api/v1/admin/devapi/scope-applications` | 管理台审批 |
| 策略能力 `scope.apply` | §3.10 矩阵里的第四行(生产 `devapi_policy_overrides` 从无此行) |
| `ErrScopeNeedsGrant` / `ErrScopeNotGrantable` / `gateForScope` | 三档判据本身 |

> **迁移**:DROP 由 `go run ./cmd/migrate`(库 `kun_galgame_infra`)执行,幂等;主库迁移自 `85ea4ab` 起随每次生产部署自动跑。

### 3.10 平台策略矩阵与应用审批(2026-08-18 落账)

上一节管的是「一把 key 能带哪些 scope」;本节管的是**「开发者在门户里能自助做到哪一步」**,由一张 **三能力策略矩阵** 决定(2026-08-25 前是四能力)。判据在 `devapi.capabilities`(代码注册表)+ `devapi_policy_overrides`(偏离默认的行)。**没有 override 行 = 代码默认**,删行 = 回到默认。

| capability | 允许的 mode | 默认 | 管什么 |
|---|---|---|---|
| `app.create` | `self_service` / `approval` / `disabled` | `self_service` | 自助创建应用 |
| `app.manage` | `self_service` / `disabled` | `self_service` | 自助编辑(`PATCH /dev/apps/:id`)与删除(`DELETE /dev/apps/:id`——归档或真删两臂,见 [§3.11](#311-应用生命周期归档删除与结算名册2026-08-29)) |
| `key.mint` | `self_service` / `disabled` | `self_service` | 自助铸造与轮换密钥 |

> 曾有第四行 `scope.apply`(提交授权制 scope 申请),随授权制一档于 2026-08-25 退役,见 §3.9。生产 `devapi_policy_overrides` 从未有过这一行,所以退役不需要清理数据。

**吊销永不入闸**:`POST /dev/apps/:id/keys/:id/revoke` 是止损动作,任何策略都关不掉它。**2026-08-29 换了路由**:这个动作原先写作 `DELETE /dev/apps/:id/keys/:id`,现在那条 DELETE 真的删行(见 [§3.11](#311-应用生命周期归档删除与结算名册2026-08-29)),吊销移到显式的 `revoke` 子路径;换的只是 URL,「止损动作不受任何策略约束」这条判据一字未动。§3.9 的 scope 判据同样**刻意不进矩阵**——它已有自己的机制与测试钉,两处真源必漂移。

**`app.create=approval` 的状态机**(`oauth_clients.dev_review_status`,值域 `approved` / `pending` / `declined`;`dev_review_note` 存拒绝理由,rune 计数上限 2000,`devapi.maxAppReviewNoteLen`):

```
自助创建 ──self_service──> approved + dev_enabled=true      （行为与本节前完全一致）
         └─approval──────> pending  + dev_enabled=false
pending ──admin approve──> approved + dev_enabled=true（清空 note）
        └─admin decline──> declined + dev_enabled=false + note（理由必填）
declined ──owner resubmit──> pending（清空 note;可先 PATCH 改名再提交）

任何态 ──owner DELETE / admin archive──> archived（dev_archived_at 非空 + dev_enabled=false
                                        + store_settlement_eligible=false;
                                        零引用且不具备登录能力的壳走另一臂:直接删行,见 §3.11）
archived ──admin PATCH dev_enabled=true──> approved + dev_enabled=true（清空 note 与 archived_at）
archived ──admin DELETE──> 行消失（须零引用 + 从不具备登录能力,否则 409;见 §3.11）
```

**归档与审核状态正交**:归档**不写** `dev_review_status`,一个被拒的应用归档后仍是 `declined`。所以「已归档」不是状态机的第四个值,而是压在这三个值之上的第二根轴——查询里它是 `dev_archived_at IS NULL` 这个谓词,不是某个 status 字面量。

- **只有 pending 可审**,对非 pending 调 approve/decline → **409**。
- **pending / declined 不能铸 key** → **409**(不是 403:这是状态冲突,不是权限问题)。判据写成 `status ∈ {pending, declined}` 而**不是** `status != 'approved'`——OAuth 控制台建的一方 client 不认识这两列,写进去的是空串,**空串刻意 fail-open**。
- ~~**pending / declined 不能停用** → **409**(pending 从未启用无可停,declined 本就 inert);门户对这两态隐藏「停用」。~~ **2026-08-29 翻案**:这条拒绝是为了不让「等待审核」悄悄变成「没有了」,但它与「上限计入 pending / declined」那条**合起来是个死锁**——被拒的申请永久占着五格之一,而自助面没有任何交回它的动作,**五次被拒 = 这个账号从此进不来平台**。现在这两态一律可删(删除即归档或真删,见 [§3.11](#311-应用生命周期归档删除与结算名册2026-08-29)),门户对它们**不再隐藏删除**。原先要防的东西由「归档是单向的、复活只有运营做得到」承接。
- **管理台 `PATCH /admin/devapi/apps/:id` 置 `dev_enabled=true` 时同时写 `approved`**——否则控制台放行的应用仍停在 pending,其 owner 在一个活着的应用上被拒铸 key。
- **5-app 上限把 pending / declined 一并计入**(`CountAppsByOwner` 本就不按状态过滤),否则被拒者可以无限刷申请。**2026-08-29 补**:计入的只有**未归档**的行——owner 域那三个查询(`ListAppsByOwner` / `GetAppByOwner` / `CountAppsByOwner`)一律带 `dev_archived_at IS NULL`,所以删除应用现在真的能把格子交回来。这也是本波存在的理由,见 [§3.11](#311-应用生命周期归档删除与结算名册2026-08-29)。
- 凭据中间件**不读** `dev_review_status`:`dev_enabled` 仍是唯一 auth 位。

**自助端点**(`/api/v1/dev/*`):

| 端点 | 说明 |
|---|---|
| `GET /api/v1/dev/policies` | 四 capability 的生效 mode map(`{"app.create":"approval", …}`),门户据此渲染禁用态与提示 |
| `POST /api/v1/dev/apps/:client_id/resubmit` | 仅 `declined` 可用 → 打回 `pending` 并清空 note;非 declined → **409** |
| `DELETE /api/v1/dev/apps/:client_id` | 删除应用(2026-08-29 由「停用」改写):吊销全部活钥匙 → 清掉其中没有用量的钥匙行 → **零引用即真删行、否则归档**,两臂见 §3.11;**开过 `user_login` 的应用永远走归档臂**;pending / declined 同样可删;受 `app.manage` 闸 |
| `POST /api/v1/dev/apps/:cid/keys/:id/revoke` | 吊销密钥(2026-08-29 从 `DELETE …/keys/:id` 迁来);**永不入策略矩阵** |
| `DELETE /api/v1/dev/apps/:cid/keys/:id` | 删除密钥行;未吊销 → **409**,曾服务过请求 → **409**,见 §3.11 |

**管理端点**(`/api/v1/admin/devapi/*`):

| 端点 | 权限 | 说明 |
|---|---|---|
| `GET /admin/devapi/apps?status=` | `devapi.manage` | `enabled`(缺省,兼容旧行为)/ `pending` / `declined` / `disabled` / **`archived`(2026-08-29 新增)** / `all`;**除 `all` 与 `archived` 外每个过滤器都排除已归档行**——owner 删掉的应用不是待审、不是「停用待决」、更不是活应用,留在工作标签页里只会往运营队列塞没人等的行。列表项带 owner、`review_status`、`review_note`、**`archived_at`**(未归档时整键缺席)、**`store_settlement_eligible`**、`created_at` |
| `PATCH /admin/devapi/apps/:client_id` | `devapi.manage` | body 全可选:`owner_user_id` / `dev_enabled` / `dev_tier` / `dev_rate_per_min` / `dev_quota_daily` / **`store_settlement_eligible`**(2026-08-29 新增,见 §3.11)。**`dev_enabled=true` 同时写 `approved` 并清空 `dev_archived_at`——这是平台上唯一的解归档动作**:一个被放回服务的应用必须重新出现在 owner 的列表里,否则他持有一个自己看不见的活应用 |
| `POST /admin/devapi/apps/:client_id/approve` | `devapi.manage` | 仅 pending,否则 **409** |
| `POST /admin/devapi/apps/:client_id/decline` | `devapi.manage` | body `{reason}` 必填(**rune** 计数 ≤2000),否则 **400**;仅 pending,否则 **409** |
| `POST /admin/devapi/apps/:client_id/archive` | `devapi.manage` | 运营侧归档:吊销全部活钥匙 + `dev_enabled=false` + 盖 `dev_archived_at` + 清 `store_settlement_eligible`。**它永不删行**(与门户 DELETE 的差别),且是下一行的前置 |
| `DELETE /admin/devapi/apps/:client_id` | `devapi.manage` | **硬删 `oauth_clients` 行**——全平台唯一会删这张表的运营动作。未归档 → **409**;`client_id` 还有任何引用 → **409**;**曾具备用户登录能力的 client 一律 409**(只归档不删)。守卫为何要这么宽、为什么第七个条件判的是能力而不是行数,见 §3.11 |
| `POST /admin/devapi/apps/:cid/keys/:id/revoke` | `devapi.manage` | 吊销密钥(2026-08-29 从 `DELETE …/keys/:id` 迁来,与自助面同一次换路由) |
| `DELETE /admin/devapi/apps/:cid/keys/:id` | `devapi.manage` | 删除密钥行;两道 409 与自助面**逐字相同**——那是审计完整性,不是权限,见 §3.11 |
| `GET /admin/devapi/keys` | `devapi.manage` | 跨全部应用的密钥清单(仅元数据)。`client_id=` / `state=active\|revoked\|expired\|all` / `page` / `limit`(≤200,缺省 50)→ `{items, total, page, limit}`。**无编辑端点**:行动作复用既有 per-app rotate / revoke |
| `GET /admin/devapi/policies` | `devapi.manage` | 注册表(labels / modes / default)+ 生效 mode + `editable`(调用者是否持 `devapi.policy_manage`) |
| `PUT /admin/devapi/policies/:capability` | **`devapi.policy_manage`** | body `{mode}`,upsert 一行;未知 capability 或该 capability 不允许的 mode → **400** |
| `DELETE /admin/devapi/policies/:capability` | **`devapi.policy_manage`** | 删行 = 回到代码默认 |

**capability 被关闭时**,对应自助端点返回 **403**(`ErrCapabilityDisabled`),文案说明该功能当前由平台关闭。**非 owner 仍先吃 404**:策略错误绝不能变成「别人有没有这个应用」的存在性预言机。

> **迁移**:本节新增一张表 `devapi_policy_overrides` + `oauth_clients` 两列,主库迁移**不随部署自动执行**——须手工 `go run ./cmd/migrate`(库 `kun_galgame_infra`)。

### 3.11 应用生命周期:归档、删除与结算名册(2026-08-29)

到本节前,平台**没有「删掉一个应用」这个动作**。`DELETE /dev/apps/:id` 叫停用,做的是 `dev_enabled=false`,而 `CountAppsByOwner` 不看 `dev_enabled` 也不看审核状态——被停用的、被拒的、还在排队的行**永久占着五格之一**,自助面没有任何把格子交回来的办法。§3.10 那两条规则(「pending / declined 不能停用」+「上限一并计入」)单看各自都成立,合起来是个死锁:**五次被拒 = 这个账号从此进不来平台**,而它一次也没做错什么。本节把停用换成删除。

`oauth_clients` 为此加两列:

| 列 | 类型 | 是什么 |
|---|---|---|
| `dev_archived_at` | `timestamptz NULL` | owner 在门户删除应用的时刻。**它不是软删的 `deleted_at`**——行还在、`client_id` 还在解析、每一条引用都还活着,变的只是「它不再属于 owner 的世界」 |
| `store_settlement_eligible` | `boolean NOT NULL DEFAULT false` | 这个应用是否参与每月 DLsite 优惠券池的分账,见下文「结算名册」 |

**归档做什么**:吊销该应用**全部**未吊销的钥匙 → `dev_enabled=false` → 盖 `dev_archived_at` → 清 `store_settlement_eligible`。**没有任何东西被删**:sessions、authorization codes、短链、计量历史与 `client_id` 本身全部原地留下——owner 的决定管的是「这个应用还是不是我的」,管不到已经发生过的事。

**归档让应用离开 owner 的世界(这是本波的要害)**:`ListAppsByOwner` / `GetAppByOwner` / `CountAppsByOwner` 三个 owner 域查询一律带 `dev_archived_at IS NULL`。于是已归档的应用在门户列表里消失、每一条 per-app 路由回 **404**、并且**不再计入 5-app 上限**。因为每一个自助操作都经 `GetAppByOwner` 解析,这道谓词同时也是「不可能在自己已删除的应用上铸钥匙」的那道门。**pending / declined 现在也能删**——§3.10 那条 `ErrAppNotApproved` 拒绝已翻案,理由见该节。**归档是单向的**:自助面没有解归档,复活只有运营的 `PATCH dev_enabled=true` 做得到(见 §3.10 管理端点表)。

**门户 `DELETE /dev/apps/:client_id` 有两臂,同一个动词**,顺序是:吊销全部活钥匙 → **删掉其中从未服务过任何请求的钥匙行** → 查一次引用(判据与下文运营侧硬删**同一个**),

- **零引用的壳 → 直接删行**,不留归档记录;
- **还有任何引用 → 归档**。

**中间那步不是顺手**:钥匙行本身就是一条引用,不清掉它「铸过一次就永远删不掉」。而 owner 之后**再也够不到这些钥匙**——已归档的应用对 `GetAppByOwner` 不可见,于是「建 → 铸 → 一次没调 → 删」会留下一行归档 + 一把只有运营清得掉的孤儿钥匙。删的判据与手工删钥匙**逐字相同**(见下表),`ErrKeyHasHistory` 在这里读作「这一把留下」而不是失败:**任何还被 usage 行指着的钥匙都活下来,并且把应用的行一起留下**。

真删那一臂也不是顺手做的方便:归档会把格子还给 owner,于是「建 → 删 → 再建」变成一个对 `oauth_clients` **无上限写行的循环**——而这恰恰是旧的「五格占死」在结构上排除掉的东西,本波若不补这一臂就是拆掉了唯一那道限。删掉那个从未被引用的壳把循环闭上,并且**复用运营硬删的同一道守卫**——门户这条路上消失的行,恰好是运营本来就被允许删的那一行。(两面只差在前置:运营侧要求**先归档**,门户侧的 DELETE 调用**本身**就是那个刻意动作。)

**密钥的 `DELETE` 现在真的是删,两道 409 守着**:

| 条件 | 结果 |
|---|---|
| 尚未吊销 | **409** ——删一把还活着的钥匙等于一次不留记录的吊销 |
| `last_used_at` 非空,或 `developer_api_usage` 里有它的 `key_id` | **409** ——`developer_api_usage` 背后**没有外键**,删掉钥匙行就把计量历史变成指向虚空的行 |

`last_used_at` 是这道判据里**耐久的那一半**:usage 行会被 `DeveloperUsageRetentionDays` 修剪掉,那个时间戳不会。**运营与 owner 拿到完全相同的两个 409**——这是关于审计链的完整性约束,不是权限问题,所以没有「管理员可以强删」这一档。

**吊销因此换到 `POST …/keys/:id/revoke`**(自助面与管理面同一次换路由,见 §3.10)。**两面的部署顺序可以任意**,而且正是这道守卫让它成立:一个还在用旧路由的前端把 DELETE 发过来,打到的是一把**没有吊销过**的活钥匙 → 409,而不是静默地把一份活凭证连同它的记录一起删掉。

**运营侧的硬删,以及守卫为什么这么宽**:`DELETE /admin/devapi/apps/:client_id` 要求应用**已归档**,且 `client_id` 在 `developer_api_keys` / `developer_api_usage` / `store_purchase_links` / `store_coupon_links` / `sessions` / `authorization_codes` 六张表里**零行**、`site_id IS NULL`,**并且这个 client 从来不具备签用户进来的能力**(下一段)。理由是 **`oauth_clients` 是一张双职表**——它既是开发者应用表,又是 OAuth 登录 client 表——而上面这些列**没有一个是外键**:Postgres 会痛快地接受任何 DELETE,然后留下一批仍在对着一个不存在的 client 验签的活 session、一批还在计点击而结算分母再也归因不到的短链、以及一段记在空白名下的计量。**「先归档」是守卫的一部分,不是礼貌**:一个 session 恰好全部过期的登录 client,长得和一个没用过的空壳**一模一样**,只有一次刻意的归档能把两者分开。

**第七个条件:能签用户的应用永远删不掉(只归档)**。判据不是「现在有没有登录」,而是**这个 client 有没有登录配置**——`is_public`、`grants` 非空、或有任何 `redirect_uris` 之一成立即算。两条理由缺一不可,外加一笔认下来的代价:

- **前六个条件里的 `sessions` / `authorization_codes` 会自己归零**:`internal/app/cleanup.go` 每小时**真删**(两张表都没有软删列)过期的 session 与授权码。所以那两个计数说的是「此刻有没有人登着」,**从来不是「这个 client 曾经做过什么」**——一个几年前签过几千个用户、如今 session 全过期的 client,在前六个条件下与一个空壳一模一样。而登录配置写在创建时、**没有任何清理作业碰它**,是手边唯一耐久的答案。
- **它必须耐久,是因为损害落在另一个库里**:`catalog_user_playtimes.client_id` 在 **`kun_catalog`**,记录按 `(user, work, client)` 三元组落行(§3.8);而只有能给用户签令牌的 client 才可能在那里有行。平台侧这道守卫**结构上够不到那个库**,不可能靠数行来判——所以它改判**能力**:凡是能登录的,一律不删。
- **刻意接受的残留**:一个开了 `user_login` 的应用,建完立刻删,留下的是**一行归档**而不是什么都不剩。这是「永不制造孤儿 playtime 行」的价钱,认了。**门户默认建出来的是纯 API 应用**(不传 `user_login` → `is_public=false`、`grants=[]`、`redirect_uris=[]`,见 [05 §9.2](./05-developer-portal.md)),不受这条影响;而 `user_login` 一旦开过就**关不掉**(`PATCH` 的 `user_login` 是整体替换且至少要一个回调 URI),所以这一位一经点亮就是终身的。

**第二道门:站点管理台的 `DELETE /api/v1/oauth/clients/:id`**。这条路由比开发者平台老,删的是同一张 `oauth_clients`,而它**从来没有任何守卫**——`SiteService.DeleteOAuthClient` 直接把行删了。于是上面那七个条件对第三方全部成立、对握着站点管理台的运营者本人一条都不成立:一个开发者的应用连同它的钥匙、计量与短链,可以从一条压根没听说过这些东西的路由上消失。判据现已收进 `devapi.Repository.EnsureDeletable`,两道门调的是**同一个函数**,`NewSiteService` 因此多一个**必填**参数——没有哪种接线能再造出一个悄悄丢掉守卫的 `SiteService`。两处只差一条:**「先归档」只约束有 owner 的行**。一个纯 OAuth 登录 client 没有归档这个动作(两个管理台都没有地方能给它盖 `dev_archived_at`),要求它先归档等于用一句没人执行得了的建议拒绝掉一切;它的保护是引用规则本身——登录 client 本来就永远过不了第七条。两种拒绝都回 **409**(信封 `code=7`),文案分别指向「先去开发者控制台归档」与「它还被引用着 / 能签用户」。

**结算名册(`store_settlement_eligible`)**:铸店铺短链**仍是自助**——`store:read` 自 2026-08-26 起就在 `selfServiceScopes` 里,门户直接勾(§3.9)。但每月的优惠券池是**定额**的,按**去重点击**的份额分(`store_link_daily_stats.uniques`,即结算用的那个数,不是 `total`),**每多一个参与者都稀释其余所有人**;于是「能不能铸链」和「分不分钱」从本波起是两个决定,后者是运营在这一列上写下的名册,而不是「谁手里有 `store:read`」。

- **存量行全部回填 `false`,包括已经持有 `store:read` 的那九个应用**。回填成 `true` 等于把「名册存在之前恰好勾过那个框的人」静默地招进了分账名单。
- 归档/删除**顺手清掉这一位**:短链会永远计下去,而一份定额池的份额不该继续累给一个已经被 owner 撤下的应用。
- **今天还没有任何结算查询读这一列**:首次结算是 2026-09,读它的那一波在后面。列现在就落,是因为**这个名册决定一旦晚于点击数据的累积就失效了**——到那时再定名册,定的是「已经分过的钱该怎么算」。

> **迁移**:本节的两列由 `devapi.AddOAuthClientDevColumns` 加,主库迁移**不随部署自动执行**——须手工 `go run ./cmd/migrate`(库 `kun_galgame_infra`)。`dev_archived_at` 可空,存量行全部回填 `NULL`(把「建在功能之前」读成「已删除」会清空每个开发者的应用列表)。`store_settlement_eligible` **保留 SQL DEFAULT** 而不像其余 `dev_*` 列那样 `DROP DEFAULT`:`false` 是 Go 零值,GORM 因此在**每一条 INSERT 里都省掉这一列**,一个没有 default 的 NOT NULL 列会直接拒掉整行。

---

## 10. OpenAPI 策略

v1 设计时"galgame 无 spec"的前提已过时——现状是**两个面都有 code-first spec**,工作量收敛为"公开投影":

- **galgame 面**:读面已 Huma 出谱(条件缓存端点为 spec-only 形态)。公开 `/v1` 投影 = 沿同一管线(`cmd/gen-openapi` 加一个 public 目标)产出**独立的公开 spec**(白名单端点 + `/v1` 前缀 + 公开 DTO),与内部 spec 解耦。
- **catalog 面**:服务自带 Huma spec(`/openapi.json`)。同法产出公开投影(白名单只读子集)。
- 产出 `api.nextmoe.dev/v1/catalog/openapi.json`(galgame 面的同名 spec URL 已随该面于 2026-07-30 摘牌,现落 410) → 门户 Scalar 渲染 → 第三方据此生成 SDK(TS 优先,`@kungal/api-*` 发包纪律届时启用)。**✅ spec URL 已上线(2026-07-28;galgame 面那条已于 2026-07-30 随面摘牌,现仅剩 catalog 一条)**:`cmd/catalog` 无鉴权在线服务——boot 时经 `cmd/gen-openapi` 同一 spec-only 管线构建一次(与仓内冻结 Tier-A YAML 恒等,CI 冻结门背书——**该"恒等"在 2026-08-08 前并不成立**:线上 boot 路径先挂 admin/s2s、房内错误信封已就位,离线生成路径不挂,于是两份 spec 的错误体一直不同;wave 190 让生成路径自己装信封后才真正恒等,详见 [§3.6](#36-错误体--房内信封2026-08-08-wave-190-更正)。冻结门只比 spec 与 spec,照不出这类"生成器与运行时不同源"的偏差),JSON 渲染,`Cache-Control: public, max-age=3600`;精确 GET 路由先于 `/v1` 键控组注册,故这两条免 key,其余 `/v1/*` 照旧要 key。门户侧为自建文档体验(06c 已弃 Scalar);SDK 生成策略见 [08](./08-downstream-faces-and-sdk.md)。
- 公开 spec 纳入 `docs:verify` + oasdiff 破坏性门,升级为 **Tier-A 对外契约**(在 kungal-docs 登记)。
