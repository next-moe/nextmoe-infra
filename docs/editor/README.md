# 未萌编辑台（editor.nextmoe.com）

NextMoe 资料库的唯一编辑入口。所有第一方站点的用户在这里编辑作品、发行、角色、人物、署名名、厂商、标签、特质、系列、引擎，提交提案、审核、查看修订历史与回滚、新建条目、发起合并请求。

论坛、moyu、LetMoe 的编辑入口都已改为深链到这里（2026-10-21），各站自己的编辑页、编辑 BFF 与 `@nextmoe/edit-ui` 依赖已删除；App 没有资料编辑入口。`@nextmoe/edit-ui` 已退役：npm 包标记废弃、仓库归档、edit-ui.nextmoe.dev 下线。

## 架构

```
浏览器 ──► editor.nextmoe.com（apps/editor：Nuxt 4 ssr:false + Nitro 薄 BFF）
             │  页面请求无会话 → 302 account.nextmoe.com/api/v1/oauth/authorize（PKCE）
             │  /auth/callback 换令牌 → httpOnly cookie（令牌不进浏览器 JS）
             │  /api/v2/**  → catalog:9281/v2/**（服务端加 Authorization，带 CSRF 头校验）
             │  /api/editor-application → oauth:9277/api/v1/editor/applications
             ▼
         catalog 服务（编辑引擎 internal/platform/editing + editspec 注册表）
             │  提案 / 修订 / 回滚 / 新建 / 合并请求 / 审核队列
             ▼
         kun_catalog（catalog_* 正典表；edit_proposal / edit_revision / edit_suppressed_row；edit_policy_override）
```

- OAuth 应用 `nextmoe-editor`：public client（PKCE，无密钥）、`auto_consent`、`catalog_site=editor`、`union_site_roles=true`，作用域 `openid profile catalog:read catalog:edit`。由 `cmd/migrate` 在设置了 `KUN_EDITOR_PUBLIC_URL` 时幂等创建（只建不改，控制台的修改不会被覆盖）。
- 编辑站的 catalog 站点 `editor` 是中央站：可以看到、审核、回滚任何站点发起的提案；下游站点仍只看自己的。
- `union_site_roles`：令牌携带用户在所有站点的站点角色并集，因此论坛、moyu 的站点 moderator 在编辑台同样具有审核权。

## 登录（无感）

1. 访问任意页面且没有编辑台会话：服务端在返回 HTML 之前 302 到账号中心 authorize，带 PKCE、state，以及来访 URL 上的 `login_hint`。
2. 账号中心对 `auto_consent` 应用先跳到 `/api/v1/auth/oauth/silent-authorize`（刷新 cookie 只在 `/api/v1/auth` 路径下可见）。浏览器已有账号中心登录态、且 `login_hint` 为空或与当前账号一致时，直接签发授权码并 302 回 `/auth/callback`，不渲染任何页面。
3. 回调换取令牌、落 httpOnly cookie，302 回原页面。整个过程用户看不到登录页和登录按钮。
4. 只有在该浏览器上从未登录、登录已过期、或 `login_hint` 指向另一个账号时，才会显示账号中心的登录或选号页面。

账号中心的浏览器会话为「闲置续期 + 绝对上限」：同一浏览器上任一下游站刷新令牌都会续期；cookie 存活到绝对上限。两个时长可配置（见下表）。

## 权限与策略

| 实体 | 提案 | 审核 | 自动生效 | 作品主人可审 |
|---|---|---|---|---|
| 作品、发行 | 任何登录用户 | `edit.catalog.<type>.review` | 审核者或编辑者（`catalog.edit.trusted`）本人的修改 | 是 |
| 角色、厂商、标签、引擎、系列、人物、署名名、特质 | 任何登录用户 | 对应 `*.review` 权限 | 从不（共享实体，总需第二人审核） | 否 |

- 标签的 `tier`/`sexual`、特质的 `sexual` 只有审核者可以提案。
- 新建条目走同一套策略（字段键 `@create`）；标签、特质的新建只有审核者可以提案。
- 策略可在运行时覆盖：表 `edit_policy_override`（按实体类型或单个字段），管理接口 `GET /api/v1/admin/catalog/edit-policy`、`PUT`/`DELETE .../edit-policy/overrides`、`GET .../edit-policy/audit`，需要 `edit.policy.manage`（admin、ren）。覆盖写入即生效，其他进程 30 秒内跟随。
- 角色：`moderator`、`admin`、`ren` 具有全部编辑与审核权限；`creator` 的权限维持生产覆盖层现状；`editor`（编辑者）通过申请获得，提案免审直接生效（受每日额度限制），不能审核他人。合并请求的决定需要 `catalog.merge.review`（moderator、admin、ren）；删除请求的决定需要 `catalog.delete.review`（同样默认 moderator、admin、ren）。

## 配额与配置键

全部在配置中心（管理台 → 设置），热生效。

| 键 | 默认 | 说明 |
|---|---|---|
| `catalog.write_quota_window_hours` | 24 | 所有配额的滑动窗口 |
| `catalog.proposals_per_day` | 100 | 普通用户每窗口提案数 |
| `catalog.proposals_per_day_editor` | 500 | 编辑者 |
| `catalog.proposals_per_day_reviewer` | 1000 | 审核者 |
| `catalog.direct_edits_per_day_editor` | 100 | 编辑者免审直编数；用完后提交转为待审提案 |
| `catalog.open_proposals_max_user` | 30 | 普通用户同时待审上限（0 = 不限） |
| `catalog.open_proposals_max_editor` | 100 | 编辑者 |
| `catalog.open_proposals_max_reviewer` | 0 | 审核者（不限） |
| `catalog.merge_requests_per_day` / `_editor` / `_reviewer` | 10 / 50 / 500 | 合并请求 |
| `catalog.merge_cooling_off_hours` | 48 | 合并批准后的冷静期 |
| `catalog.discussion_posts_per_hour` | 30 | 每人每小时可发的讨论留言 |
| `catalog.deletion_requests_per_day` / `_editor` / `_reviewer` | 5 / 20 / 200 | 删除请求 |
| `catalog.deletion_cooling_off_hours` | 48 | 删除批准后的冷静期 |
| `catalog.edit_merge_reward` | 1 | 提案被他人合并后给提案人发放的萌萌点（0 = 停发） |
| `catalog.edit_merge_reward_window_days` | 7 | 编辑奖励任务每次回看的天数 |
| `auth.sso_session_idle_days` | 30 | 账号中心会话闲置续期 |
| `auth.sso_session_max_days` | 365 | 账号中心会话绝对上限 |
| `auth.editor_application_cooldown_hours` | 24 | 编辑者申请被拒后的再申请冷却 |
| `auth.creator_application_cooldown_hours` | 24 | 创作者申请同上 |

定时任务 `catalog-merge-requests`（每小时）执行冷静期已过、由编辑台发起并已批准的合并请求；配置在 `jobs.catalog_merge_requests.*`。

定时任务 `catalog-deletion-requests`（每小时，`jobs.catalog_deletion_requests.*`）执行冷静期已过、已批准的删除请求；作品在此期间已被合并或删除的，请求自动关闭。

定时任务 `catalog-edit-rewards`（每 15 分钟，`jobs.catalog_edit_rewards.*`）给编辑奖励：回看 `catalog.edit_merge_reward_window_days` 天内合并、且合并人不是提案人的提案（不分来源站点与条目类型；自己合并、免审直编、回滚都不发），按 `catalog.edit_merge_reward` 给提案人记一笔 `content_approved`，来源 `nextmoe-editor`，幂等键 `catalog_edit_merged:<提案 id>`。论坛此前已用 `galgame_edit_merged:<提案 id>` 发过的跳过，不会重复发放。

## 可编辑范围

除内部字段（各类 id、外键、时间戳、溯源戳、来源记账、`*_norm`、计数 / 评分 / 热度等派生值、机器队列、用户私有数据）外全部开放。各类型的完整字段清单以 `GET /v2/catalog/schemas/{object}` 为准；编辑台按该接口渲染，新字段上线后自动出现。

- 列表字段的上游行（各导入源写入的行）可以「隐藏」（`.suppressed` 伴随字段，行身份键见 `GET /v2/me/edit-context/{object}/{id}`），媒体行可以改分级；人工行写在 `source_id=12` 人工车道。
- 新建：`POST /v2/me/creations`（角色、厂商、署名名、系列、引擎、人物〈从未归属的署名名〉、发行〈挂在已有作品下〉、标签、特质）；同名可疑重复时返回 409 `DUPLICATE_SUSPECTS`，确认后带 `confirm_duplicates=true` 重提。作品仍通过认领流程 `POST /v2/me/claims` 新建；注意：认领流程目前只建 galgame 媒介的作品，其他媒介的作品暂不能在编辑台新建。
- 合并请求：`POST /v2/me/merge-requests`；审核者在 `/v2/moderation/merge-requests` 批准后进入冷静期，到期由定时任务或审核者执行；跨媒介的作品合并会被拒绝。
- 删除请求（目前只限作品）：`POST /v2/me/deletion-requests`（原因 `not_a_work` / `out_of_scope` / `spam_or_test` / `other`；`duplicate` 返回 422，重复条目走合并），`GET /v2/me/deletion-preview` 预览影响；审核者在 `/v2/moderation/deletion-requests` 批准后进入冷静期，到期执行。删除是软删除：外部对应全部保留作墓碑（导入不会重建），条目上的评分、收藏、游玩时长与状态保留但不显示；`decision=restore` 用同一个 id 恢复，数据一并回来。已删除作品的读接口返回 410 `ENTITY_DELETED`（带原因与时间）。编辑台入口：作品详情页 ⋯ 菜单「删除」，审核在 `/review/deletions`。拆分（unmerge）仍只有管理员可用。

## 人工编辑不会被导入任务覆盖

- 字段溯源戳 `field_provenance`：人工改过的列打 `curated`/`user` 戳，导入任务看到人工戳即跳过（包括人清空的值）。
- 人工车道：列表字段人工只写 `source_id=12` 的行，导入任务只写各自来源的行，互不删除。
- 行压制 `edit_suppressed_row`：隐藏的上游行在读路径过滤，导入任务再插回也不会显示；合并、撤销合并时随实体迁移和恢复。
- 每个可编辑字段都有「人工编辑 → 导入任务运行 → 值仍在」的测试；新注册列表字段若缺少 edit-context 行来源，完整性测试会失败。

## 回滚

- 每次生效都追加一条 `edit_revision`（改了哪些字段 + 改后完整快照）；实体第一次被编辑时另存编辑前的原始状态（`baseline_snapshot`），历史页显示为「导入原始状态」。
- `POST /v2/moderation/reverts`：`mode=restore` 恢复到某一版（整份覆盖）；`mode=undo` 只撤销某一版改动过的字段，后续版本动过同一字段时返回 409 `REVERT_CONFLICT`，审核者可 `force`；`baseline=true` 回到导入原始状态。回滚本身也是一条新修订，可以再回滚。

## 讨论

每条提案、合并请求（之后的拆分、删除请求同理）下面都有讨论区，挂在「锚点」`(anchor_kind, anchor_id)` 上，`anchor_kind` 就是锚点的对象类型（`proposal`、`merge_request`）。数据在 kun_catalog（`edit_discussion_post`、`edit_discussion_watcher`），不走社区服务：社区服务只接受带客户端密钥的站点服务端调用，编辑台是没有密钥的公开客户端，而谁能看某条合并请求、谁发起、谁处理这些判断都在 kun_catalog。

- 读：`GET /v2/catalog/discussions/{object}/{id}`。提案的讨论和提案一样公开；合并请求的讨论只有发起人和 `catalog.merge.review` 持有者能看。
- 写：`POST /v2/me/discussions`（仅第一方站点），作者可 `PATCH` 改、`DELETE` 删（留下「已删除」占位）。正文 ≤ 2000 字，最多提及 10 人，过内容审查（拒绝 = 422，扫描主体 `(nextmoe, edit_discussion)`）。
- 关注：发言人、被提及的人和锚点的参与者（提案人、处理人、修正者；合并请求的发起人）自动关注；`PUT /v2/me/discussion-watches` 关注或静音。静音后只有提及自己的留言会通知。
- 通知：`GET /v2/me/discussion-notices?since=` 与提案状态变化一起出现在顶栏通知里；点开直达提案页或审核页的 `#discussion`。
- 管理：`POST /v2/moderation/discussions/{id}/visibility` 隐藏或恢复，需要新权限 `catalog.discussion.moderate`（moderator / admin / ren）；被隐藏的原文和隐藏原因只有讨论管理员看得到。
- 注销账号：该账号的留言变为「已删除」占位，关注记录删除。
- 新的请求类型接入：在 `cmd/catalog/main.go` 给 `discussion.New` 注册一个 `discussion.Kind`（是否公开、存在性、谁能看、发起人、参与者、谁算审核者），前端在 `app/constants/discussion.ts` 加标签与跳转路由，页面里放 `<DiscussionThread anchor-kind="…" :anchor-id="…" />`。

## 编辑台页面

| 路由 | 内容 |
|---|---|
| `/` | 工作台：配额、我的提案、最近修订、审核待办 |
| `/works`、`/releases`、`/characters`、`/persons`、`/credit-names`、`/companies`、`/tags`、`/traits`、`/series`、`/engines` | 列表与搜索；`/{type}/{id}` 详情，`/{type}/{id}/edit` 编辑工作区（新标签页打开，见下节） |
| `/new/{family}`、`/new/work` | 新建条目（作品走认领流程）；顶栏「+」与各列表页「新建」进入 |
| `/review`、`/review/{id}` | 审核工作台：逐字段采纳 / 修正 / 拒绝 |
| `/review/merges` | 合并请求审核：批准 / 驳回，冷静期倒计时后执行（仅审核者可见） |
| `/me/merge-requests` | 我的合并请求（审核前可撤回） |
| `/history/{family}/{id}` | 修订历史、任意两版对比、回滚 |
| `/changes` | 全站最近修订 |
| `/me/proposals`、`/me/contributions`、`/me/drafts` | 我的提案、贡献统计、本地草稿 |
| `/apply` | 申请成为编辑者 |
| `/guidelines` | 编辑规范 |

## 编辑工作区

`/{type}/{id}/edit` 是独立的整屏工作区，不套编辑台的导航；编辑台里所有「编辑资料」入口都在新标签页打开它，下游深链也直接落在这里。

- 左栏只列这个条目的分区（作品：基础 / 标题 / 简介 / 标签 / 外链 / 封面 / 截图 / 角色 / 制作人员 / 厂商 / 引擎与平台 / 系列 / 关联作品 / 发行），每个分区显示条目数与未提交的修改数；顶部「返回工作台」，底部「编辑规范」。其他实体的分区按各自的字段分组生成。
- 当前分区与选中项写在地址里（`?section=roster&item=7937`），可以直接分享或刷新。
- 列表字段把人工行与资料源行合在一张表里：每行标来源，人工行可直接改、删，资料源行可隐藏 / 恢复（媒体可改分级、类型、主封面）；没有单独的「资料源导入」区块。封面、截图整体可拖拽排序（写 `catalog.work.cover_order` / `screenshot_order`），可把图片文件拖进来上传。
- 关联条目（角色、制作人员、厂商、引擎、系列、关联作品、发行）用「列表 + 详情」两栏：选中一项后，上半部分编辑它在本作中的设定（出场程度、职位、身份、关系等），下半部分直接编辑这个实体本身（分 tab）。宽屏默认选中第一项；窄屏先列表、再进入详情。
- 一次可以同时改作品和任意多个关联实体：切换不丢草稿，顶栏「查看更改」弹出全部改动的前后对比（新增 / 删除 / 修改 / 隐藏 / 调整顺序），「提交」把每个改动过的条目各生成一条提案，共用一段修改说明。草稿按条目保存在本机。
- 页面先出骨架再并行取数据，切换分区不等待网络。

## 下游接入

下游站点的「编辑」按钮改为新标签页打开深链：

```
https://editor.nextmoe.com/{type}/{id}/edit?from={site}&return={url}&login_hint={sub}
```

- `type`：`works`、`releases`、`characters`、`persons`、`credit-names`、`companies`、`tags`、`traits`、`series`、`engines`。
- `from`：`kungal`、`moyu`、`letmoe`、`nextmoe`；`return` 必须是该站点的源（`apps/editor/app/constants/returnSites.ts`），编辑台顶栏据此显示「返回 X」。
- `login_hint`：当前用户的 `sub`（UUID），保证同一浏览器多账号时进入的是同一个账号。
- 来源站归因：提案、修订、合并请求都带 `origin_site`（`kungal` / `moyu` / `letmoe` / `nextmoe` / `editor`），表示这次编辑是从哪个站来的。下游站直接调 API 时由服务端按 OAuth 客户端判定（moyu 与论坛共用 `catalog_site=kungal`，按客户端的社区租户区分）；从深链进入编辑台时，编辑台 BFF 把校验过的 `from` 记进 httpOnly cookie（30 天，新标签页也带着），代理 `/v2` 时以 `X-NextMoe-Origin-Site` 头发给 catalog，只有 `catalog_site=editor` 的令牌声明的来源才被采纳。`origin_site` 只用于展示与统计，不参与权限、配额、萌萌点。2026-10-21 之前的记录为 `null`，编辑台退回显示它归属的站点（`site`）。列表接口支持 `origin_site=` 筛选；`GET /v2/vocabularies/origin_site` 给出各站显示名与用户主页 URL 模板（`{id}` 即中央用户 id，各站自己的用户 id 与之相同）。

## 部署

- compose 服务 `editor`（镜像 `ghcr.io/next-moe/infra-editor`），Traefik 路由 `editor.nextmoe.com` 全量写在 `docker-compose.prod.yml`。
- `migrate` 任务带 `KUN_EDITOR_PUBLIC_URL=https://editor.nextmoe.com`，用于创建 `nextmoe-editor` 应用。
- 图片 CDN 基址：运行时配置 `NUXT_PUBLIC_IMAGE_CDN_BASE`，默认生产 CDN。
- 本地开发：`pnpm dev` 会一起启动编辑台（端口 9422）。
