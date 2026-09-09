# sticker 表情包面

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

- 路径前缀：`/v2/sticker`
- 凭据：Authorization: Bearer nmk_live_…（任意有效 v2 应用密钥即可,无需任何 scope）
- 端点数：9

## 使用须知

- 下游站点面：由 sticker.kungal.com 自己的服务提供，经平台网关联邦进来。契约由该站的仓库拥有，本站只镜像它的 OpenAPI 原文。
- 任意有效 nmk_ 应用密钥都能调，不需要任何 scope。不要在铸密钥时去找 sticker:read——这个 scope 字符串不存在，勾了会被拒。
- 它不是第二份内容列表：站上每张表情都打了 catalog 的作品 id 与角色 id，所以这个面回答的是「这个 catalog 身份有哪些表情素材」。/v2/sticker/characters/{character_id}/stickers 与 /v2/sticker/works/{work_id}/packs 是主车道，其余是它们的脚手架。
- 只暴露已发布的表情包。草稿、隐藏、已删除、评论正文、审核状态与全部创作端点都不在这个面里，将来也不会有。
- 名字是多语言映射而不是字符串：键为 zh-cn / zh-tw / ja-jp / en-us / und，每个都可缺，und 放的是 catalog 里没有语言标记的显示名。回退链由你自己定，这个面永远不替你挑。
- 翻页用 page + limit（1-50），与 catalog 的游标翻页不同；nsfw 说的是表情包自己的分级，不是它所属游戏的——从 r18 游戏里剪出来的日常表情包是 all_ages，而它的 work.content_rating 仍然是 r18。
- 错误方言分两半：401 与 429 由网关写，body 是平台信封 {"code":10001,"message":"未授权，请先登录"}；其余全部是 RFC 9457 application/problem+json，code 取自平台那份封闭注册表。按 HTTP status 分支。
- 只能服务端调用：预检 OPTIONS 不带认证头，会被网关 401，浏览器直连没有出路。

## 端点

### 表情包

- `GET /v2/sticker/packs` — List published sticker packs [详情](https://developer.nextmoe.dev/docs/sticker/listPacks.md)
- `GET /v2/sticker/packs/{pack_id}` — One pack, with its stickers [详情](https://developer.nextmoe.dev/docs/sticker/getPack.md)

### 表情

- `GET /v2/sticker/stickers/{sticker_id}` — One sticker [详情](https://developer.nextmoe.dev/docs/sticker/getSticker.md)

### 角色

- `GET /v2/sticker/characters` — Catalog characters this site has stickers of [详情](https://developer.nextmoe.dev/docs/sticker/listCharacters.md)
- `GET /v2/sticker/characters/{character_id}` — One catalog character, as this site holds it [详情](https://developer.nextmoe.dev/docs/sticker/getCharacter.md)
- `GET /v2/sticker/characters/{character_id}/stickers` — Every sticker of one catalog character [详情](https://developer.nextmoe.dev/docs/sticker/listCharacterStickers.md)

### 作品

- `GET /v2/sticker/works` — Catalog works this site has material for [详情](https://developer.nextmoe.dev/docs/sticker/listWorks.md)
- `GET /v2/sticker/works/{work_id}/packs` — Packs about one catalog work [详情](https://developer.nextmoe.dev/docs/sticker/listWorkPacks.md)

### 标签

- `GET /v2/sticker/tags` — Tags, most used first [详情](https://developer.nextmoe.dev/docs/sticker/listTags.md)

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/sticker
