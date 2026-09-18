# 错误处理

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

错误是一等资源类型，不是成功响应的一个变体。它有自己的 media type、自己的 schema、自己的稳定标识，和成功响应在类型系统里可判别。

## 形状

所有 4xx / 5xx 响应都是 RFC 9457 `application/problem+json`：

```http
HTTP/1.1 400 Bad Request
Content-Type: application/problem+json
X-Request-ID: req_01JBQ7X4M2K9P3W5T8ZVN6HRDC

{
  "type": "https://developer.nextmoe.dev/problems/platform/unknown-include",
  "title": "Unknown include",
  "status": 400,
  "code": "UNKNOWN_INCLUDE",
  "detail": "include= received a token that is not in this collection's vocabulary: nope",
  "instance": "/v2/catalog/works?include=tags,nope",
  "request_id": "req_01JBQ7X4M2K9P3W5T8ZVN6HRDC",
  "errors": [
    {
      "parameter": "include",
      "reason": "UNKNOWN_VALUE",
      "detail": "nope",
      "params": { "allowed": ["titles", "refs", "intros", "covers", "companies", "ratings", "tags", "credits"] }
    }
  ]
}
```

| 字段                    | 恒在 | 说明                                                             |
| ----------------------- | ---- | ---------------------------------------------------------------- |
| `type`                  | 是   | 稳定的 problem type URI，解析到 [本站的错误码页](/problems)      |
| `title`                 | 是   | 这个类型的固定英文短语，不随请求变化                             |
| `status`                | 是   | 与状态行一致的 HTTP 状态码                                       |
| `code`                  | 是   | **顶层稳定标识**，封闭注册表里的 UPPER_SNAKE 字符串              |
| `detail`                | 是   | 英文、随请求变化的说明。没什么可补充时是空串。**不要拿它做分支** |
| `instance`              | 是   | 出错的路径与查询串                                               |
| `request_id`            | 是   | `req_` + 26 位 ULID，与 `X-Request-ID` 同值。报问题时请带上      |
| `errors`                | 是   | 字段级失败明细。不是字段级错误时是 `[]`，**永远不是 null**       |
| `object` / `current_id` | 否   | 仅 `ENTITY_MERGED` 时出现                                        |

## 两层标识，两个注册表

顶层用 `code`，`errors[]` 里用 `reason`——**故意不同名**。两个层级用同一个键名，会让「读错嵌套层」这个最常见的消费端 bug 拿到一个看起来合理的值，而不是 `undefined`。两份注册表的成员集合互不重叠。

- `code`：这次请求整体为什么失败。例如 `UNKNOWN_INCLUDE`、`SCOPE_REQUIRED`、`RATE_LIMITED`。
- `reason`：某个具体字段为什么不合格。封闭的一小套：`REQUIRED`、`INVALID_FORMAT`、`OUT_OF_RANGE`、`TOO_LONG`、`TOO_SHORT`、`TOO_MANY_ITEMS`、`TOO_FEW_ITEMS`、`DUPLICATE_ITEM`、`UNKNOWN_VALUE`、`NOT_ALLOWED_VALUE`、`UNKNOWN_REFERENCE`、`IMMUTABLE`、`INCONSISTENT_WITH`、`NOT_PERMITTED`。

`errors[]` 的每一项**恰好**带 `pointer`、`parameter`、`header` 三者之一，指出出问题的位置：`pointer` 是请求体里的 JSON Pointer（RFC 6901），`parameter` 是查询或路径参数名，`header` 是请求头名。

## 本地化：`params`

`detail` 是英文诊断信息，**不要**把它显示给终端用户，也不要去解析它。要拼出「标题最多 233 个字」这样的本地化文案，读 `reason` 和 `params`：

| `reason`         | `params`                                                       |
| ---------------- | -------------------------------------------------------------- |
| `TOO_LONG`       | `{ "max_length": 233 }`                                        |
| `TOO_SHORT`      | `{ "min_length": 1 }`                                          |
| `OUT_OF_RANGE`   | `{ "minimum": 0, "maximum": 60000 }`（数值界才有，可只有一边） |
| `TOO_MANY_ITEMS` | `{ "max_items": 100 }`                                         |
| `TOO_FEW_ITEMS`  | `{ "min_items": 1 }`                                           |
| `UNKNOWN_VALUE`  | `{ "allowed": ["wish", "doing", …] }`（封闭词表才有）          |
| 其余             | 不出现                                                         |

每个 `reason` 可能携带哪些键写在注册表里：`GET /v2/problems/reasons` 每项的 `param_names`。`params` 缺席时按 `reason` 出一句不带数字的通用文案即可。

## 怎么分支

1. **先看 HTTP `status`。** 这是唯一保证你永远认得的东西。
2. **再看 `code`。** 认得就走专门分支。
3. **认不得就按 `status` 兜底。** 这是[客户端契约](/docs/design#client-contract)的第三条：我们会往注册表里加新的 `code`，你的客户端不能因此崩掉。

不要拿 `detail` 或 `title` 做分支——`detail` 随请求变化，两者都是英文散文。

## 按状态码的处置

| 状态          | 典型 `code`                                                                                            | 该怎么办                                                                                |
| ------------- | ------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------- |
| `400`         | `INVALID_PARAMETER` `UNKNOWN_INCLUDE` `UNKNOWN_SORT` `LIMIT_TOO_LARGE` `TOO_MANY_IDS` `INVALID_CURSOR` | 你的请求有 bug。**不要重试**，修代码                                                    |
| `401`         | `MISSING_CREDENTIAL` `INVALID_CREDENTIAL`                                                              | 带上凭据；用户令牌则刷新一次再试                                                        |
| `403`         | `SCOPE_REQUIRED` `USER_IDENTITY_REQUIRED` `PERMISSION_REQUIRED` `TENANT_MISMATCH`                      | 换凭据或补 scope。重试同一请求没有意义                                                  |
| `404`         | `NOT_FOUND` `ENTITY_MERGED`                                                                            | `ENTITY_MERGED` 时读 `current_id` 重指本地行；其余当作不存在                            |
| `405`         | `METHOD_NOT_ALLOWED`                                                                                   | 路径在，方法不在                                                                        |
| `409`         | `ALREADY_EXISTS` `IDEMPOTENCY_KEY_REUSED` `INVALID_STATE_TRANSITION`                                   | 先读当前状态再决定，不要盲目重试                                                        |
| `409`         | `IDEMPOTENCY_REQUEST_IN_PROGRESS`                                                                      | 同一个 key 的首个请求还没跑完：稍等，用**同一个 key、同一个 body** 重试即可拿到它的结果 |
| `410`         | `GONE`                                                                                                 | 这个 URL 永久退役了。v1 的六个前缀都在这里                                              |
| `412` / `428` | `PRECONDITION_FAILED` / `PRECONDITION_REQUIRED`                                                        | 重新 GET 拿最新 `ETag`，带上 `If-Match` 再写                                            |
| `429`         | `RATE_LIMITED` `QUOTA_EXCEEDED`                                                                        | 按 `Retry-After` 等待。见 [限流与配额](/docs/rate-limits)                               |
| `5xx`         | `INTERNAL_ERROR` `SERVICE_UNAVAILABLE`                                                                 | 指数退避 + 抖动重试；连续失败就降级到本地缓存                                           |

## ENTITY_MERGED

被合并掉的实体不 301。请求它会拿到 `404` + `code: ENTITY_MERGED`，body 里带 `object` 与 `current_id`，响应头带 `Link rel="canonical"`：

```json
{
  "type": "https://developer.nextmoe.dev/problems/catalog/entity-merged",
  "status": 404,
  "code": "ENTITY_MERGED",
  "object": "work",
  "current_id": "207379",
  …
}
```

之所以不是重定向：静默跟随会让你以为拿到的还是原来那条记录，本地库里的旧 id 也永远不会被修正。批量对账读 [`/v2/catalog/redirects`](/docs/mirror#merges)。

## 完整注册表

每个 `type` URI 都能在本站打开，写着它的状态码、含义和触发条件。

- [错误码](/problems) — 按域分组的全部错误码，中英对照，每个码一页。
- [限流与配额](/docs/rate-limits) — 429 的两种成因，以及正确的退避方式。

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/errors
