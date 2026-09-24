# 数据模型

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

同一部作品在六个上游各有一个页面。NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上这个答案取自哪个源。

## 一条记录，六个源

上游各有各的强项，也各有各的空洞和错误。我们不做「选一个源当真理」，也不做「把六个源的数字平均一下」——两种做法都会产出一个上游都不认的数。规则是：

- **单值字段做裁定**。标题、原始语言、发售日、分级这类只能有一个答案的字段，由规则加人工编辑裁定出一个值，并记录它取自哪个源。
- **多值事实并列保留**。评分、游玩时长、人气、译名、简介这些块按 `source` 并列返回，刻度保持源原生（VNDB 的 10 分制、ErogameScape 的 100 分制不会被归一成同一把尺）。要不要折算是你的产品决定，不是我们的。
- **生态站点自己的产出同样是一个源**。未萌生态站点整理的条目、译名，以及用户通过编辑提案提交的修改，和六个上游平级进入这份记录。

| 源           | 主要贡献                        |
| ------------ | ------------------------------- |
| VNDB         | 身份主锚、作品关系、角色 traits |
| Bangumi      | 中文名与条目、角色资料          |
| DLsite       | 同人与商业店铺条目              |
| ErogameScape | 评分与发售信息                  |
| Ci-en        | 创作者动态与厂牌外链            |
| Getchu       | 角色立绘、正文、截图            |

## 实体族

每个族都有集合面和详情面，每个族的 id 都能寻址——响应里出现 `xxx_id`，就一定存在对应的 `GET /v2/catalog/…/{id}`。

| 族          | 路径                       | 是什么                                                                   |
| ----------- | -------------------------- | ------------------------------------------------------------------------ |
| work        | `/v2/catalog/works`        | 作品。目录的中心，其余大多数东西挂在它上面                               |
| release     | `/v2/catalog/releases`     | 发行版本：平台、语言、发售日、媒介                                       |
| character   | `/v2/catalog/characters`   | 角色。`appearances` 反查它出演的作品                                     |
| credit_name | `/v2/catalog/credit-names` | **署名**。一个人在不同作品里用的不同名义，是署名表上真正出现的那个字符串 |
| person      | `/v2/catalog/persons`      | **人**。身份行；它名下的所有名义在 `persons/{id}/credit-names`           |
| company     | `/v2/catalog/companies`    | 厂牌 / 社团。`graph` 给出母子与厂牌关系                                  |
| tag         | `/v2/catalog/tags`         | 作品标签（正典词表）                                                     |
| trait       | `/v2/catalog/traits`       | 角色属性词表                                                             |
| series      | `/v2/catalog/series`       | 系列                                                                     |
| engine      | `/v2/catalog/engines`      | 引擎                                                                     |

> [!NOTE]
> `credit_name` 与 `person` 是**两个族**，不是一个族的两种视图。一个署名可能还没有归到人身上，一个人可能有十几个名义——把它们压成一个族，两边都会丢信息。做 staff 页读 `credit-names/{id}`，做人物页读 `persons/{id}`。

## 作品的子资源

作品详情默认只发身份内核。角色、标签、评分、封面、截图、外链、发行版本、简介、署名、系列、引擎这些块，要么在详情面用 `include=` 点名，要么走各自的子资源面单独分页读：

```http
GET /v2/catalog/works/{id}?include=tags,characters,ratings
GET /v2/catalog/works/{id}/tags        # 想分页 / 想单独缓存时走这里
GET /v2/catalog/works/{id}/characters
GET /v2/catalog/works/{id}/covers
```

反过来，「这个厂牌名下有哪些作品」不是厂牌实体里的一个内嵌数组，而是作品集合上的一个过滤器 `?company_id=` ——这样它自带分页和全套过滤条件。角色、系列、引擎、标签同理。

## 身份：目录 id 与外部 ref

每个实体有一个目录 id（十进制字符串），同时带一组 `refs` —— 它在各个上游的 id，形如 `source:external_id`。手里已经有 VNDB / Bangumi / DLsite / ErogameScape 的 id 时，直接反查即可，不必先按标题搜：

```http
GET /v2/catalog/works?refs=vndb:v19658,bangumi:302835
```

> [!WARNING]
> `refs` 是**身份锚**，不保证是一个可以打开的网页。比如署名的 VNDB ref 是一个 staff alias id，它自己没有页面。可点开的地址一律在 `links` 块里。

## 合并与重定向

同一个东西在上游被登记了两遍时，目录会把两行合并。被合并掉的那个 id **不会 301**，而是 `404` 加 `code: ENTITY_MERGED`，body 里给出 `object` 与 `current_id`，同时回一个 `Link rel=canonical`。

之所以不是重定向：静默跟随会让你以为自己拿到的还是原来那条记录，本地库里的旧 id 也永远不会被修正。要批量对账，读 `/v2/catalog/redirects` —— 它是一条按合并时间排序的游标 feed，每行给出 `old_id → current_id`。

## 内容分级与 nsfw 闸

- `content_rating` 是作品的**事实分级**：`all_ages` / `sensitive` / `r18`。
- `nsfw=` 是**调用方自己控制的闸**：缺省隐藏 r18，显式 `nsfw=true` 才可见。只认 `true` / `false`，写别的是 `400` 而不是按默认值处理。
- `content_limit`（`sfw` / `nsfw`）是**展示轴**：能不能给没选成人内容的读者看。每部作品都带（2.26.0 起）。已认领作品由认领方站点的编辑判定决定，未认领作品按事实分级；封面全部被判 explicit 的作品不论哪种都是 `nsfw`。它不等于事实分级，也不要用事实分级去推它。

这三个是正交的：一部 `all_ages` 的作品可以被某个站点标成 `nsfw` 展示轴，一部 `r18` 作品在 `nsfw=false` 下会整条消失而不是发个空壳。

## 认领

生态里的站点可以「认领」一部作品——声明这一条对应它自己库里的哪个条目。已认领作品的 `claim` 块给出 `site`、`site_work_id`、`state` 与 `content_limit`；未认领的作品 `claim` 是 `null`（这个键**永远不会消失**）。

`state` 里的 `live` 与 `draft` 是公开的；`pending` / `declined` / `hidden` 是各站自己的审核队列，需要持有 `claim_events:read` 的密钥并用 `site=` 指名自家站点才看得到。

## 编辑是公开的

目录的每一次修改都留痕，而且这份痕迹是公开只读的：`/v2/catalog/proposals` 是编辑提案，`/v2/catalog/revisions` 是修订历史。谁改了什么、被谁通过或拒绝，任何持密钥的应用都能读到。

- [全链走查](/docs/example) — 用两个真实系列把上面这些概念跑一遍。
- [词表](/docs/vocabularies) — 每个枚举的成员，以及它是开放还是封闭的。
- [增量镜像目录](/docs/mirror) — 把这份记录同步进自己的库，并保持新鲜。

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/concepts
