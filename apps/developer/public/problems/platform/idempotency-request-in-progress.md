# IDEMPOTENCY_REQUEST_IN_PROGRESS · Problem type

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## Idempotency request in progress

A request with the same Idempotency-Key is still being processed. Retry after it completes.

- `code`：`IDEMPOTENCY_REQUEST_IN_PROGRESS`
- HTTP：409
- 域：platform
- `type`：https://developer.nextmoe.dev/problems/platform/idempotency-request-in-progress

错误体的字段与分支顺序见 https://developer.nextmoe.dev/docs/errors.md，全部错误码见 https://developer.nextmoe.dev/problems.md。

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/problems/platform/idempotency-request-in-progress
