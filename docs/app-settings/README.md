# 原生 App 配置读面(`/v2/apps`)

> **状态:已实现**(2026-10-21)。契约 = 本目录的 [`openapi.yaml`](./openapi.yaml)(由 Go 代码导出,CI 比对);
> 线上同一份在 `https://api.nextmoe.dev/v2/apps/openapi.json`。实现在
> `apps/api/internal/platform/settings/appface`,挂在 catalog 服务上(`/v2` 前缀归 catalog)。
> 配置中心本身的模型(三层解析、审计、传播)见 [`docs/deploy/18-config-center.md`](../deploy/18-config-center.md)。

## 0. 定位

一方原生 App(目前只有 kungal 的 Flutter App)是公开客户端,没有 client secret,用不了站点走的
`GET /api/v1/settings`(要 Basic Auth,见 [oauth 14](../integration/oauth/14-settings.md))。本面给它
一个**免凭证**的读面:App 用自己公开的 OAuth `client_id` 报出身份,拿到

1. 它所属站点必须遵守的全部**公开键**,按站点覆盖后的值——与该站点从 `/api/v1/settings` 读到的相同;
2. 为这个 App 声明的**产品参数**(配置中心的 `app` 域)。

App 的产品参数放进平台配置中心,是项目所有者 2026-10-20 的决定。站点网页自己的产品开关
(moyu 的 `site_setting` 之类)仍留在站点那里,不因此改变。

## 1. GET /v2/apps/{client_id}/settings

**鉴权**:无。不带凭证的调用与其它匿名 `/v2` 调用共用每个 IPv4 地址 / IPv6 /64 的限额
(配置中心 `apiv2.anon_rate_per_minute` / `apiv2.anon_quota_per_day`)。已登录的 App 可以带
`Authorization: Bearer <用户访问令牌>`,只改变计入哪个桶(改为按人),不改变响应。

`client_id` 必须是一个**公开客户端**(`oauth_clients.is_public=true`);不存在或不是公开客户端都回 404。
站点由该 client 的站点绑定(`oauth_clients.site_id`)决定;没有绑定则全部是平台值。

**成功响应**(200,v2 惯例的裸 JSON,没有 `code/message/data` 信封):

```json
{
  "$schema": "https://api.nextmoe.dev/AppSettings.json",
  "client_id": "kungal-app",
  "site_id": 2,
  "settings": {
    "app.kungal.home_rating_count": 6,
    "app.kungal.home_topic_count": 20,
    "app.kungal.home_work_rows": 2,
    "artifact.upload_enabled": true,
    "auth.name_change_cost": 17,
    "image.upload_enabled": true,
    "platform.notice": "",
    "platform.read_only": false,
    "shop.min_price": 100
  }
}
```

**条件请求**:200 带 `ETag`;下次请求带 `If-None-Match: <ETag>`,内容没变回 304、无 body。源站发的是强 ETag,
但请求带 `Accept-Encoding: gzip` 时 Cloudflare 压缩响应,会把它改成弱形式 `W/"…"`。原样带回即可:
源站按弱比较匹配,两种形式都回 304。
ETag 由整个 body 计算,任一值变化即变化。

**错误**:全部是 `application/problem+json`(v2 的 Problem 结构,按 `code` 判断,不要按 `detail`):

| HTTP | code | 触发条件 |
|------|------|----------|
| 404 | `NOT_FOUND` | 没有这个 client_id,或它不是公开客户端 |
| 422 | `VALIDATION_FAILED` | client_id 超过 50 字符或含 `[A-Za-z0-9._-]` 以外的字符 |
| 429 | `RATE_LIMITED` / `QUOTA_EXCEEDED` | 匿名限额用尽,带 `Retry-After` |
| 503 | `SERVICE_UNAVAILABLE` | 查 client 时主库不可用 |

## 2. 键目录

- **公开键**:与 `/api/v1/settings` 下发的同一批(`platform.*`、两个上传开关、`auth.name_change_cost`、
  `shop.min_price`),语义见 [oauth 14 §2](../integration/oauth/14-settings.md);`shop.min_price` 是萌萌点商店里
  商品可标的最低价。每个键的类型、范围与默认值也写在 `openapi.yaml` 的 `settings` 属性里。
- **App 键**:只下发给声明了它们的 App。

| 键 | 类型 | 默认 | App 必须做什么 |
|------|------|------|------|
| `app.kungal.home_topic_count` | int 5..100 | 20 | 首页显示的话题条数 |
| `app.kungal.home_work_rows` | int 1..4 | 2 | 首页作品网格的行数 |
| `app.kungal.home_rating_count` | int 0..20 | 6 | 首页显示的玩家评分条数,`0` = 隐藏该区块 |

App 键不可按站点覆盖,`kungal-app` 与 `kungal-app-desktop` 读同一组值。**键只增不删**:要改语义就加新键;
删除或改类型会被 spec-breaking CI 拦下。

## 3. 客户端行为(MUST)

1. 冷启动拉一次;回到前台时距上次成功拉取满 10 分钟再拉。带 `If-None-Match`。
2. 键缺失、类型不符或越界 → 用 App 内置默认值;未知键 → 忽略。所以加键不会破坏旧版本。
3. 网络错误、4xx/5xx → 沿用上次存下的快照;从没成功过就用内置默认值,不阻塞启动。
4. `platform.read_only=true` 时隐藏或禁用一切写入口;`platform.notice` 非空时在首页顶部展示。

## 4. 值从哪来、多快生效

catalog 进程每 30 秒从主库拉一次配置中心的平台行与站点行,本面读的是这份进程内快照(请求路径不查
`setting_overrides`,只按主键查一次 `oauth_clients`)。控制台改值后约 30 秒内本面生效,再加上 App
自己的拉取间隔。读面报告的是 catalog 进程看到的值(数据库行,否则代码默认);上传两开关在生产只靠
数据库行(18-config-center §18.4),所以与 `/api/v1/settings` 一致。

## 5. 给新 App 或新键

1. 在 `apps/api/internal/platform/settings/keys/app.go` 声明键(名字 `app.<app>.<key>`,给范围与中英说明),
   放进该 App 的键列表,并在 `appKeysByClient` 里把它的 client_id 指向这个列表;新 App 的列表还要并进
   `appDomain.Keys`。`keys_test.go` 的 golden 表补上键名。
2. 重新导出契约:`cd apps/api && go run ./cmd/gen-openapi -app-settings -o ../../docs/app-settings/openapi.yaml`。
3. 不需要迁移,不需要改控制台:键出现在控制台「原生 App」域里,有 `settings.write` 的人可以改。
