---
title: 缓存与条件请求
eyebrow: API 基础
description: NextMoe API v2 的缓存契约：ETag 与 If-None-Match、Cache-Control 的两类声明、为什么大多数面不可共享缓存，以及写操作的 If-Match 与 Idempotency-Key。
---

# 缓存与条件请求

每个 200 的 GET 都带 `ETag`。多数面按 `private, no-store` 声明——这不是保守，是因为它们的 body 会随凭据能力位变化，缓存住哪一份都是错的。

## ETag 与 304 {#etag}

所有 200 的 GET 都带一个强 `ETag`，它是响应体的哈希——`fields=` 裁剪过的响应有它自己的验证器，所以条件请求在任何形状上都成立。

```bash
# 第一次
curl -i "https://api.nextmoe.dev/v2/catalog/works/207379" -H "Authorization: Bearer nmk_live_…"
< HTTP/1.1 200 OK
< ETag: "9f2a1c…"

# 之后带上它
curl -i "https://api.nextmoe.dev/v2/catalog/works/207379" \
  -H "Authorization: Bearer nmk_live_…" \
  -H 'If-None-Match: "9f2a1c…"'
< HTTP/1.1 304 Not Modified
```

> [!NOTE]
> `304` 同样计入限流。它省的是带宽和解析，不是调用次数——真正省调用的是把响应连同 `ETag` 存下来，在你自己的 TTL 内根本不发这个请求。

## Cache-Control {#cache-control}

| 面                                                                                      | 声明                                                              |
| --------------------------------------------------------------------------------------- | ----------------------------------------------------------------- |
| `/v2/problems`、`/v2/vocabularies`、`/v2/catalog/schemas/*`、`/v2/catalog/openapi.json` | `public, max-age=300, s-maxage=1800, stale-while-revalidate=3600` |
| 其余所有 2xx                                                                            | `private, no-store`                                               |
| 所有 4xx / 5xx                                                                          | `no-store`                                                        |

每个响应还带 `Vary: Authorization, Accept-Encoding`。

## 为什么可共享缓存的只有注册表面 {#why-private}

`public` 的前提是「同一个 URL，任何人拿到的字节相同」。注册表面满足这一点：错误码、词表、对象 schema、OpenAPI 文档不随谁在问而变化。

`/v2/catalog/*` 不满足。它是密钥门内的面，body 会随认领站点的围栏、nsfw 能力位、`claim_state` 可见范围而变——一个中间层如果自作主张把它当公共内容缓存下来，就会把 A 的视图发给 B。所以这里是**默认拒绝**：只有明确证明了「body 与凭据无关」的面才进 public 名单，其余一律 `private, no-store`。

> [!WARNING]
> `Vary: Authorization` 救不了这件事——这是实测过的：曾经有边缘节点在缓存里放了一份 `max-age=14400` 的 `/v2/catalog` 响应，而这个源站从来没有设过这个值。所以现在每一条 2xx 都显式声明自己的缓存意图，不给中间层留下「自己发明一个」的空间。

你自己的服务端当然可以缓存这些响应——你知道自己的密钥、也知道自己的可见范围。不要做的是把它们交给一个不理解这些围栏的共享中间层。

## 写操作的条件请求 {#if-match}

改动别人也能改的资源时用乐观并发：先 GET 拿 `ETag`，再把它放进 `If-Match` 写回去。

```bash
curl -X PATCH "https://api.nextmoe.dev/v2/me/proposals/8812" \
  -H "Authorization: Bearer <access_token>" \
  -H 'If-Match: "9f2a1c…"' \
  -H "Content-Type: application/json" \
  -d '{"note":"补充一条来源"}'
```

- 不带 `If-Match` 是 `428 PRECONDITION_REQUIRED`。
- 带了但对不上是 `412 PRECONDITION_FAILED`——说明有人在你读之后改过，重新 GET 再合并。
- `If-Match: *` 只表示「存在即可」，它**不能**用来绕过任何权限检查。

## 幂等键 {#idempotency}

`POST` 支持 `Idempotency-Key`。同一个计数身份、同一路径、同一个 key 的重复请求会重放首次的响应（保留 24 小时），网络超时后安全重试因此不会写两条。

```bash
curl -X POST "https://api.nextmoe.dev/v2/me/claims" \
  -H "Authorization: Bearer <access_token>" \
  -H "Idempotency-Key: 6f3c2b7a-1d54-4a90-9d1e-2c7f8b0a5e31" \
  -H "Content-Type: application/json" \
  -d '{ … }'
```

同一个 key 配上**不同的** body 是 `409 IDEMPOTENCY_KEY_REUSED`——这是在提醒你 key 生成有 bug，而不是在阻挠你。

首个请求还没处理完时，用同一个 key 重试会得到 `409 IDEMPOTENCY_REQUEST_IN_PROGRESS`，而**不会**把写操作执行第二遍。稍等再用同一个 key、同一个 body 重试，拿到的就是首个请求的响应。key 最长 255 字节。

## 要跟住变化，不要轮询 {#freshness}

把目录同步进自己的库，然后靠增量信道保持新鲜，比任何缓存策略都有效：`GET /v2/catalog/changes` 按 `(updated_at, id)` 升序枚举整个人口，冷启动翻一遍就是全量清点，之后只拉增量。配方见 [增量镜像目录](/docs/mirror)。
