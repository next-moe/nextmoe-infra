# letmoe 下载资源链接面

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

- 路径前缀：`/v2/letmoe`
- 凭据：Authorization: Bearer nmk_live_…（任意有效 v2 应用密钥即可,无需任何 scope;唯一例外是 /v2/letmoe/resources/{id}/file,要运营发放的 letmoe:files）
- 端点数：4

## 使用须知

- 下游站点面：由 一起萌（www.letmoe.com）自己的服务提供，经平台网关联邦进来。契约由该站的仓库拥有，本站只镜像它的 OpenAPI 原文。
- 它只回答一件事：这部游戏在 letmoe 上能下载到什么。作品 id 就是 catalog 的作品 id，也是 letmoe 自己的游戏 id——/v2/letmoe/works/{id}、站上的 /game/{id} 与 /v2/catalog/works/{id} 是同一个数。
- 任意有效 nmk_ 应用密钥都能调元数据接口，不需要任何 scope——网关只做身份、计量与限流，不做授权。元数据里不带下载链接、提取码与解压密码，每条资源都给 web_url，把读者送过去。
- 唯一例外是 /v2/letmoe/resources/{id}/file：对持有 letmoe:files 的密钥给出全部下载方式——letmoe 托管文件的直链、外部网盘链接连同提取码与解压密码、以及与 moyu 补丁合并的下载。这个 scope 由运营发放、不能自助勾选，其余密钥一律 403 SCOPE_REQUIRED。答案一律 no-store，每次成功都给资源记一次下载，所以在读者点下载时再调，不要预取。
- 也不带游戏名、封面、标签与 staff：那些归 catalog，同一把密钥读 /v2/catalog/works 即可。
- /v2/letmoe/works 上的 ids= 是批量问法：一次最多 100 个作品 id，没有可见资源的原样回在 missing[] 里。这是「我手上这 100 部作品哪些能在 letmoe 下载」的一次性问法，此时 cursor、sort、limit 与 include_total 会被拒。
- 约定与 catalog /v2、moyu 面相同：id 是十进制字符串，集合按不透明 cursor 翻页（limit 1-100，超过 100 是 LIMIT_TOO_LARGE、不截断，include_total=true 才给 total），200 带 ETag 可 304。每个接口都接受 nsfw，缺省 false：成人作品不出现在列表里，它的资源单条读取是 404。
- 错误全部是 RFC 9457 application/problem+json：网关写的 401（MISSING_CREDENTIAL / INVALID_CREDENTIAL）与 429（RATE_LIMITED / QUOTA_EXCEEDED）和 letmoe 写的其余错误同一个形状、同一份封闭注册表。按 HTTP status 分支，再看 code。
- 只能服务端调用：预检 OPTIONS 不带认证头，会被网关 401，浏览器直连没有出路——nmk_ 密钥本来也不该出现在浏览器里。

## 端点

### 作品

- `GET /v2/letmoe/works` — List or look up games with resources [详情](https://developer.nextmoe.dev/docs/letmoe/listWorks.md)
- `GET /v2/letmoe/works/{id}/resources` — The resources of one game [详情](https://developer.nextmoe.dev/docs/letmoe/listWorkResources.md)

### 资源

- `GET /v2/letmoe/resources/{id}` — One resource [详情](https://developer.nextmoe.dev/docs/letmoe/getResource.md)
- `GET /v2/letmoe/resources/{id}/file` — The download options of a resource [详情](https://developer.nextmoe.dev/docs/letmoe/getResourceFile.md)

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/letmoe
