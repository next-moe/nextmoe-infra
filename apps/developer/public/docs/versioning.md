# 版本与演进

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

`/v2` 是唯一的第一方公开面，此后只做加法。删除与改名过不去 CI 的破坏性变更门——真要破坏，只能升主版本。

## 当前状态

| 面                                                                                                | 状态                                                                         |
| ------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `/v2`                                                                                             | **正式公开**（2026-08-25 GA），113 个端点，spec `2.20.0`                     |
| `/v2/moyu`、`/v2/sticker`                                                                         | **下游站点面**，由各自站点的仓库拥有契约，见[下游站点面](/docs/moyu-patches) |
| `/v1/catalog`、`/v1/news`、`/v1/store`、`/v1/playtime`、`/api/v1/catalog`、`/api/v1/user/catalog` | **已退役**（2026-08-27）。一律 `410 Gone`，`Link` 指向 `/v2`                 |

v1 是连同它的代码一起退役的，不是留一个转发层——这样就不会有人「暂时还能用」着用到明年。

## 什么算破坏性变更

| 算破坏                                     | 不算破坏                        |
| ------------------------------------------ | ------------------------------- |
| 删除一个字段、端点或参数                   | 新增一个字段                    |
| 给字段或参数改名                           | 新增一个端点                    |
| 收紧类型（可空变不可空、放宽的枚举变封闭） | 放宽约束（必填变可选）          |
| 给**封闭**词表加一个成员                   | 给**开放**词表加一个取值        |
| 改变一个已有取值的含义                     | 往错误码注册表里加一个新 `code` |
| 改变默认值                                 | 新增一个可选参数                |

> [!NOTE]
> 「给封闭词表加成员」算破坏，是因为封闭意味着我们承诺过成员集合就这些——你的 `switch` 可以没有 `default`。开放词表则相反：`x-vocabulary-closed: false` 就是在提前告诉你会有新值。所以[客户端契约](/docs/design#client-contract)要求你容忍开放词表里没见过的取值。

## 这条线怎么守住

- `/v2/catalog/openapi.json` 由运行中的路由生成，不是手写的。
- 每次改动都会拿改动前后的两份 spec 跑 oasdiff 的破坏性变更检查；命中就不许合并。
- 一批契约门跟着跑：错误码注册表互斥、词表封闭标注齐全、每个字段 `description` 非空、声明过的状态码完整、没有无约束的 `type: string`。
- 门户这份文档、`llms.txt` 与每页的 Markdown 孪生，都从同一份 spec 生成——文档和契约不会各说各话。

## 退役怎么通知

端点要退役时会先进入退役期，响应头带上 `Deprecation` 与 `Sunset`（两个头都在 CORS 的 expose 列表里，浏览器侧也读得到）。把它们接进监控——它们出现的那天，就是你还有时间从容迁移的那天。

退役生效后，那个路径返回 `410 Gone`，并指出接替它的面：

```http
HTTP/1.1 410 Gone
Link: <https://api.nextmoe.dev/v2>; rel="successor-version"
Content-Type: application/problem+json

{ "code": "GONE", "status": 410, … }
```

这就是 v1 六个前缀现在的样子。等到 `410` 才发现问题就只剩加急了，所以请让 `Deprecation` 触发告警，而不是等 `410` 触发工单。

## 跟住变化

- `GET /v2/catalog/openapi.json` 免密钥，`info.version` 是当前 spec 版本。把它拉进 CI，diff 一下就知道这次动了什么。
- 本站每个文档页都有 Markdown 孪生（路由后加 `.md`），全部端点内联在 [`/llms-full.txt`](/llms-full.txt)——适合让 agent 定期通读一遍。
- `/v2/vocabularies` 与 `/v2/catalog/schemas/{object}` 是运行时的词表与形状发现面，比任何文档都新。

设计层面的承诺与「我们不会做的事」，见 [API 设计原则](/docs/design)。

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/versioning
