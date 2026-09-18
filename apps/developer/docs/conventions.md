---
title: 请求与响应
eyebrow: API 基础
description: NextMoe API v2 的通用线格式：无信封、object 判别符、字符串 id、时间格式、null 语义、参数解析规则与请求关联头。
---

# 请求与响应

所有端点共享的线格式。读完这一页，端点参考里的每个响应都能直接看懂。

## 基础 {#basics}

| 项           | 值                                                          |
| ------------ | ----------------------------------------------------------- |
| Base URL     | `https://api.nextmoe.dev`                                   |
| 协议         | HTTPS only                                                  |
| 请求体       | `application/json`（仅写操作）                              |
| 成功响应     | `application/json`                                          |
| 错误响应     | `application/problem+json`                                  |
| 字符集       | UTF-8                                                       |
| 机器可读契约 | `https://api.nextmoe.dev/v2/catalog/openapi.json`（免密钥） |

## 没有信封 {#no-envelope}

一次成功的读，响应体**就是**那个资源；一次成功的集合读，响应体就是那个集合。没有 `{code, message, data}`，没有 `success`，没有 `timestamp`。成功与否只看 HTTP 状态码。

```json
// 详情
{ "object": "work", "id": "207379", "display_name": "…", … }

// 集合
{ "object": "list", "items": [ … ], "next_cursor": "cur_…" }
```

## `object` 是类型判别符 {#object}

每个资源、每个集合、每个内嵌子对象都带一个 `object`，值就是它的族名（`work` / `character` / `list` / `problem` …）。它是封闭词表，可以直接 switch。反过来，除它之外的任何字段都不要当判别符用——spec 里凡是写着 “Must not be used as a discriminant” 的字段，都是明确不承诺可判别的。

## id 是字符串 {#ids}

所有实体 id 都是**十进制字符串**（`"207379"`），不是 JSON number。它在库里是 int64，超出 JavaScript `Number` 的安全整数范围就会静默失真，而一旦发出去这个决定就再也收不回来。

字符串 id **不带类型前缀**——`work` 的 id 就是 `"207379"`，不是 `"work_207379"`。类型信息在 `object` 里，不重复编码进 id。

## 时间与日期 {#time}

- 时间戳是 RFC 3339 UTC：`"2026-08-29T02:51:07Z"`。
- 日历日期是 `YYYY-MM-DD`。
- 发售日精度单独用一个字段表达：`release_date_precision` 取 `day` / `month` / `year`。月精度的日期落在当月 1 号，年精度落在 1 月 1 日——**不要**把它当成真的发生在那一天。

## null 的语义 {#null}

- **数组与 map 永不 `null`。** 没有内容就是 `[]` 或 `{}`。
- **未记录的标量是 `null`，不是缺键。** 例如 `latin: null` 表示这条没有罗马字，而不是「这个字段不存在」。
- **默认瘦身时整块不出现。** `include=` 没点名的块根本不在响应里——这和「块存在但是空的」是两回事：前者是你没要，后者是真没有。
- 零值是合法值。`0` 票、`false`、`""` 都会照发，不会被当成「没有」抹掉。

> [!NOTE]
> 一个键的缺席只有一种解释。如果你发现某个键的消失可以有两种读法，那是缺陷，请报给我们。

## 参数解析：拒绝，不降级 {#params}

| 写法                            | 结果                                    |
| ------------------------------- | --------------------------------------- |
| `nsfw=true` / `nsfw=false`      | 生效                                    |
| `nsfw=1`、`nsfw=on`、`nsfw=yes` | `400 INVALID_PARAMETER`——不会当成 false |
| `limit=101`                     | `400 LIMIT_TOO_LARGE`——不会截断成 100   |
| `include=tags,nope`             | `400 UNKNOWN_INCLUDE`——不会只给 tags    |
| `sort=whatever`                 | `400 UNKNOWN_SORT`                      |
| `ids=` 超过 100 个              | `400 TOO_MANY_IDS`                      |

多值参数一律用英文逗号分隔，不用重复的 `key=a&key=b`。被降级过的调用方会把窄结果读成全部真相，所以这里宁可报错。

## 响应头 {#headers}

| 头                               | 说明                                                                               |
| -------------------------------- | ---------------------------------------------------------------------------------- |
| `ETag`                           | 每个 200 的 GET 都有。配 `If-None-Match` 拿 304，见 [缓存](/docs/caching)          |
| `Cache-Control`                  | 公开注册表面可缓存，其余 `private, no-store`                                       |
| `Vary`                           | `Authorization, Accept-Encoding`                                                   |
| `RateLimit` / `RateLimit-Policy` | 当前窗口余量与策略，见 [限流](/docs/rate-limits)                                   |
| `Retry-After`                    | 429 时给出等待秒数                                                                 |
| `X-Request-ID`                   | `req_` + 26 位 ULID。错误体里的 `request_id` 是同一个值——报问题时请带上它          |
| `Link`                           | 至少带 `rel="service-desc"` 指向本门户；`ENTITY_MERGED` 时额外带 `rel="canonical"` |
| `Deprecation` / `Sunset`         | 端点进入退役期时出现，见 [版本与演进](/docs/versioning)                            |

## 跨域 {#cors}

v2 允许任意 origin，并暴露 `ETag`、`Link`、`RateLimit*`、`Retry-After`、`Deprecation`、`Sunset`、`X-Request-ID`。

> [!CAUTION]
> **能跨域不等于该在浏览器里调。** 应用密钥是机密，放进前端等于公开发布它。浏览器要用数据，请让自己的后端代理；要代表用户读写，用用户访问令牌。

## 写操作 {#writes}

- 请求体是 JSON，`Content-Type: application/json`。
- `POST` 支持 `Idempotency-Key`：同一把密钥、同一路径、同一个 key 的重复请求会重放首次结果（保存 24 小时）；body 不同则是 `409 IDEMPOTENCY_KEY_REUSED`；首个请求还在处理时重试是 `409 IDEMPOTENCY_REQUEST_IN_PROGRESS`。
- 改动别人也能改的资源时要求乐观并发：把读到的 `ETag` 放进 `If-Match`。不带是 `428 PRECONDITION_REQUIRED`，带了但对不上是 `412 PRECONDITION_FAILED`。目前 `PATCH /v2/me/claims/{id}`、`PATCH /v2/me/proposals/{id}`、`POST /v2/me/proposals/{id}/amendments` 与两个 `moderation` 决策面都要求它。

写面的完整清单与流程见 [接入用户数据](/docs/user-data)。
