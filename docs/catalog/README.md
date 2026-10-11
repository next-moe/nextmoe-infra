# Catalog 服务 契约文档

这里是 **nextmoe-infra** 的**跨媒介身份/图谱注册层**（`catalog`）的对外契约文档。服务把多来源(VNDB / Bangumi / DLsite / erogamescape / 用户)的作品、发行、人物、署名、厂牌收敛成一套**带来源锚(external_ref)与分级信任**的规范身份,供各产品站(galgame wiki / kungal / moyu / letmoe / 未来 NextMoe 聚合)通过 S2S 接入。

它与 [`image_service`](../image_service/README.md)、[`artifact`](../artifact/README.md) 并列为跨服务契约:图床管图片字节、artifact 管大文件字节,**catalog 管「身份与关系」**——谁是谁、哪个作品对应哪些外部 id、哪些名义指向同一个人。三者共享 OAuth Client 租户模型,但数据库/职责独立。

> **Tier-A 契约源**:本目录(`docs/catalog`)是该契约的**唯一源**;forum/patch 等下游仓的 `docs/catalog` 是 kungal-docs `pnpm docs:sync` 生成的横幅镜像,**不可手改**。改契约只改这里 →`../kungal-docs` 跑 `pnpm docs:sync --write` + `pnpm docs:audit`。见 [ownership](../../CLAUDE.md)。

## 文档索引

| # | 文件 | 内容 | 状态 |
|---|------|------|------|
| 01 | [service-and-contract.md](./01-service-and-contract.md) | 服务定位、registry/body 分界、admin 队列、鉴权形态、生成 spec、运维注记。**§2 S2S 与 §4 用户令牌面所述路由已于 2026-08-27 退役**,保留为历史记录 | 历史 + 在产混排 |
| — | [admin-openapi.yaml](./admin-openapi.yaml) | admin review 队列的 OpenAPI 3.1(`gen-openapi -catalog-admin` 生成) | 生成物 |
| — | [v2-openapi.yaml](./v2-openapi.yaml) | `/v2` 公开面的 OpenAPI 3.1(`gen-openapi -catalog-v2` 生成;Huma 真路由,契约测试打这些路由) | 生成物 |
| — | [v2-implementation-notes.md](./v2-implementation-notes.md) | v2 各波实现札记,含 wave R3 的 v1 拆除清单 | 已完成 |

> 🪦 **v1 已于 2026-08-27(wave R3)整面退役。** catalog 二进制不再服务 `/v1/catalog`、`/v1/news`、`/v1/store`、`/v1/playtime`、`/api/v1/catalog`、`/api/v1/user/catalog`——六个前缀一律 `410 Gone`(信封 `code=11`,`Link` 指向 `/v2`)。S2S 与公开 v1 的两份 spec(`openapi.yaml`、`public-openapi.yaml`)随代码一并删除;唯二在产的 catalog 面是 `/v2` 与 `/api/v1/admin/catalog`。

## 一句话总结

> **catalog 只管「规范身份 + 外部锚 + 关系」(外加用户对作品的个人事实:收藏、时长、状态、评分)。产品站保有自己的富展示行(intro/封面…),只把「这是哪一个作品/人物」的身份问题委托给 catalog:批量 resolve 规范 id、按 redirect feed 清理被合并的旧 id、通过 claim 把产品行认领到一个 catalog work、经读面(锚反查/署名/实体搜索)取回身份与关系。写路径按 per-client site 绑定授权;读面无绑定。**

## 关键设计速查

- **注册层 vs 展示体分界** —— catalog 存**身份/关系/来源锚**(work/release/credit_name/character/label/external_ref);**不存** intro、封面、点赞——那些是产品站(body)的;用户评分自 2026-10-11 起全网统一存于 catalog(见 01 §4.7)。
- **分级来源锚** —— `catalog_external_ref` 按 `link_kind` 分 exact(0)/probable(1)/related(2);exact 唯一约束保证「一个来源的一个外部 id 只锚一个实体」。
- **未认领注册行(R2)** —— 导入的作品可先以 `site=NULL` 存在(某产品尚未拥有它);`claim` 到来时按锚**认领已存在的身份,绝不铸第二个**。
- **S2S per-client site 绑定** —— 写端点 `claim` 要求认证 client 的 `oauth_clients.catalog_site` 非空**且** == 请求 site,否则 403;读端点不受限。
- **署名 vs 归属两种 work↔实体边** —— credit(个人署名:谁演/担任什么)与 work_label(组织归属:哪个社团/发行方负责)并存;消费拉动落地(D-01,DLsite 社团归属首用)。
- **迁移不随服务跑** —— `cmd/migrate catalog` 是唯一 schema 入口(随部署自动跑);**导入类 cmd(reconcile/import/reindex)不随部署自动跑**,需手动执行。
- **展示轴镜像信道** —— 把 `content_limit` 缓存到本地列的消费站,轮询 `GET /v2/catalog/changes`(claim 状态 / 展示轴 / 存在性三项承诺 bump `updated_at`;`gone` 条目即删本地行),别再夜间全量扫。见 [01 §8](./01-service-and-contract.md)。

## 非目标

- 不存产品展示字段(intro/封面/点赞/收藏——产品站各自持有)。
- ~~不做面向匿名用户的公开端点~~ —— 2026-10-11(spec 2.39.0)起 `/v2/catalog` 只读面(`claim-events` 除外)收无凭证请求,按 IPv4 地址或 IPv6 /64 限流(默认 30 次/分、1,000 次/日),见 [v2-implementation-notes](./v2-implementation-notes.md) 同日波次。写面、`/v2/me`、`/v2/moderation` 仍只服务持凭证的调用方。
- 不做自动跨源合并的「终判」——probable 锚与合并提案进 admin 三桶等人审。
