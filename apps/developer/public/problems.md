# Problem types

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

## 错误码注册表（顶层 `code`，共 43 个）

`code` 是封闭注册表里的稳定标识；`errors[].reason` 是另一套互不重叠的字段级词表。认不得的 `code` 一律按 `status` 兜底——我们会往注册表里加新成员。

### platform

| code | HTTP | title | description |
| --- | --- | --- | --- |
| `MALFORMED_BODY` | 400 | Malformed body | Request body is not valid JSON, or is not a valid instance of the declared media type. |
| `INVALID_PARAMETER` | 400 | Invalid parameter | A parameter is syntactically wrong: a boolean that is not true/false, an integer that is not an integer, a date that is not YYYY-MM-DD. |
| `UNKNOWN_ENUM_VALUE` | 400 | Unknown enum value | A closed vocabulary received an unknown token at parse time. |
| `MUTUALLY_EXCLUSIVE_PARAMETERS` | 400 | Mutually exclusive parameters | Two parameters that cannot appear together were both sent. |
| `LIMIT_TOO_LARGE` | 400 | Limit too large | limit is greater than 100. The value is not clamped. |
| `TOO_MANY_IDS` | 400 | Too many ids | ids= or refs= named more than 100 items. |
| `INVALID_CURSOR` | 400 | Invalid cursor | The cursor cannot be parsed or is no longer valid. |
| `UNKNOWN_INCLUDE` | 400 | Unknown include | include= received a token that is not in that collection's vocabulary. |
| `UNKNOWN_FIELD` | 400 | Unknown field | fields= received a top-level key that does not exist. |
| `UNKNOWN_SORT` | 400 | Unknown sort | sort= received a key this collection has not declared. |
| `UNKNOWN_FACET` | 400 | Unknown facet | facets= received a name this collection does not support. |
| `MISSING_CREDENTIAL` | 401 | Missing credential | The request has no Authorization header. |
| `INVALID_CREDENTIAL` | 401 | Invalid credential | A credential was sent but it is invalid, expired, or revoked. |
| `SCOPE_REQUIRED` | 403 | Scope required | The credential is valid but lacks the scope this operation needs. |
| `FIRST_PARTY_ONLY` | 403 | First party only | This operation is only available to first-party clients. |
| `NOT_FOUND` | 404 | Not found | Nothing visible exists at this URL. |
| `METHOD_NOT_ALLOWED` | 405 | Method not allowed | The path exists but this method does not. |
| `IDEMPOTENCY_KEY_REUSED` | 409 | Idempotency key reused | The same Idempotency-Key was sent with a different request body. |
| `IDEMPOTENCY_REQUEST_IN_PROGRESS` | 409 | Idempotency request in progress | A request with the same Idempotency-Key is still being processed. Retry after it completes. |
| `GONE` | 410 | Gone | This URL existed and has been permanently retired. |
| `PRECONDITION_FAILED` | 412 | Precondition failed | If-Match did not match the current representation. |
| `UNSUPPORTED_MEDIA_TYPE` | 415 | Unsupported media type | The request body media type is not supported. |
| `VALIDATION_FAILED` | 422 | Validation failed | The request is syntactically valid but semantically not. errors[] is present and non-empty. |
| `PRECONDITION_REQUIRED` | 428 | Precondition required | This operation requires If-Match and none was sent. |
| `RATE_LIMITED` | 429 | Rate limited | The short-window rate limit was exceeded. Retry-After is in seconds. |
| `QUOTA_EXCEEDED` | 429 | Quota exceeded | The daily quota is exhausted. Retry-After is until the next window. |
| `INTERNAL_ERROR` | 500 | Internal error | A bug on our side, including the output of panic recovery. |
| `SERVICE_UNAVAILABLE` | 503 | Service unavailable | A dependency is unavailable. The request may be retried. |

### catalog

| code | HTTP | title | description |
| --- | --- | --- | --- |
| `ENTITY_MERGED` | 404 | Entity merged | The entity was merged into another. object and current_id are present, and Link rel=canonical is sent. |

### me

| code | HTTP | title | description |
| --- | --- | --- | --- |
| `USER_IDENTITY_REQUIRED` | 403 | User identity required | An application key was used for an operation that needs a user token. |
| `SITE_NOT_BOUND` | 403 | Site not bound | The client behind the token is not bound to a catalog site. |
| `RELEASE_CREATION_DISABLED` | 403 | Release creation disabled | The proposal tried to create a release. This is a product constraint, not a defect. |
| `ALREADY_EXISTS` | 409 | Already exists | The same subject already has a live record for this target. |
| `DUPLICATE_SUSPECTS` | 409 | Duplicate suspects | The mint's titles match live works of the same medium; suspects[] names them. Nothing was written. Re-send with confirm_duplicates=true to mint anyway — the pairs are still filed for reconciliation. |
| `INVALID_STATE_TRANSITION` | 409 | Invalid state transition | The current state does not allow this transition. detail names the current state and the legal targets. |
| `CLAIM_NOT_OWNED` | 403 | Claim not owned | The claim has an owner and it is another user. Only the owner may publish, submit, or withdraw it; an unowned claim is adopted by its first claimant. |

### moderation

| code | HTTP | title | description |
| --- | --- | --- | --- |
| `PERMISSION_REQUIRED` | 403 | Permission required | The token lacks the permission this decision needs. |
| `TENANT_MISMATCH` | 403 | Tenant mismatch | The target does not belong to the caller's catalog site. |
| `DECISION_ALREADY_MADE` | 409 | Decision already made | This item has already been decided. detail names who decided and when. |

### news

| code | HTTP | title | description |
| --- | --- | --- | --- |
| `SOURCE_NOT_YOURS` | 403 | Source not yours | The named news source is not bound to this user. A source that does not exist is not distinguished, so source names cannot be enumerated. |
| `SOURCE_INACTIVE` | 422 | Source inactive | The news source is bound correctly but has been deactivated. detail names who to ask to restore it. |

### store

| code | HTTP | title | description |
| --- | --- | --- | --- |
| `STORE_QUOTA_EXCEEDED` | 403 | Store quota exceeded | The application has minted the maximum number of purchase links. |
| `STORE_LINK_UNAVAILABLE` | 502 | Store link unavailable | The link shortener is unavailable; no link was issued — there is deliberately no fallback to a bare affiliate URL. |

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/problems
