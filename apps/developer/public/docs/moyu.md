# moyu 补丁面

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

- 路径前缀：`/v2/moyu`
- 凭据：Authorization: Bearer nmk_live_…（任意有效 v2 应用密钥即可,无需任何 scope;唯一例外是 /v2/moyu/resources/{id}/file,要运营发放的 moyu:files）
- 端点数：6

## 使用须知

- 下游站点面：由 鲲 Galgame 补丁（www.moyu.moe）自己的服务提供，经平台网关联邦进来。契约由该站的仓库拥有，本站只镜像它的 OpenAPI 原文。
- 它只回答一件事：这部游戏在 moyu 上有哪些补丁资源。任意有效 nmk_ 应用密钥都能调，不需要任何 scope——网关只做身份、计量与限流，不做授权。
- 不带下载直链、提取码与解压密码，这是契约里写死的取舍而不是遗漏：在 moyu 上取链接是一次单独的、按资源限速的请求，就是为了不能被批量抓走。每行都给 web_url，把读者送过去。
- 唯一例外是 /v2/moyu/resources/{id}/file：对持有 moyu:files 的密钥给出托管资源的下载链接与解压密码。这个 scope 由运营发放、不能自助勾选，其余密钥一律 403 SCOPE_REQUIRED；只有 storage 为 s3 的资源有文件，答案一律 no-store，别缓存 download_url。
- 也不带游戏名、封面、标签与角色：那些归 catalog。每行都带 catalog_work_id，拿它去 /v2/catalog/works/{id} 解析——同一把密钥就能读。patch.id、vndb_id 与 catalog_work_id 是三个互不相等的 id 空间。
- /v2/moyu/patches/{id}/resources 可以用 type= 按资源类型筛选，type=save 就是这部游戏的存档。手上只有 catalog 作品 id 时，先用 /v2/moyu/patches?refs=catalog:<id> 拿到补丁页 id，再查它的资源。
- /v2/moyu/changes 是资源变更流：资源发布、换文件、改信息、隐藏、恢复、删除各记一行，按不透明 cursor 往后读。给在自己那边保留 moyu 资源副本的站点用，先全量扫一遍面，再跟这条流增量同步。
- /v2/moyu/patches 上的 refs= 是批量反查：一次最多 100 个 vndb:<id> 或 catalog:<id> 锚，没命中的原样回在 missing[] 里。这是「我手上这 100 部作品哪些有补丁」的一次性问法，此时 cursor 与 sort 会被拒。
- 错误全部是 RFC 9457 application/problem+json：网关写的 401（MISSING_CREDENTIAL / INVALID_CREDENTIAL）与 429（RATE_LIMITED / QUOTA_EXCEEDED）和补丁站写的其余错误同一个形状、同一份封闭注册表。按 HTTP status 分支，再看 code。
- 只能服务端调用。网关对包括 OPTIONS 在内的每个方法都验密钥，而预检不带认证头，所以浏览器直连必然 401——nmk_ 密钥本来也不该出现在浏览器里。

## 端点

### 补丁页

- `GET /v2/moyu/patches` — List or look up patch pages [详情](https://developer.nextmoe.dev/docs/moyu/listPatches.md)
- `GET /v2/moyu/patches/{id}` — One patch page [详情](https://developer.nextmoe.dev/docs/moyu/getPatch.md)
- `GET /v2/moyu/patches/{id}/resources` — The resources on one patch page [详情](https://developer.nextmoe.dev/docs/moyu/listPatchResources.md)

### 资源

- `GET /v2/moyu/resources/{id}` — One resource [详情](https://developer.nextmoe.dev/docs/moyu/getResource.md)
- `GET /v2/moyu/resources/{id}/file` — The file behind a hosted resource [详情](https://developer.nextmoe.dev/docs/moyu/getResourceFile.md)

### 变更流

- `GET /v2/moyu/changes` — Resource change feed [详情](https://developer.nextmoe.dev/docs/moyu/listResourceChanges.md)

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/moyu
