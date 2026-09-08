# 下游开放 API(面联邦)与 OpenAPI / SDK

> 拍板记录 2026-07-23。回答两个问题:**下游站点自己的开放 API 如何进平台**、**OpenAPI 在契约与客户端 SDK(含未来 Flutter App)中的角色**。供下游仓与后续执行者引用。本文档是拆分后新增的内容(非原稿拆出),章节号自 **§16** 起续用同一稳定锚点空间(§1–§15 见 01–07)。

## 16. 下游开放 API = 平台面联邦(不建第二平台)

### 16.1 裁定

- kungal / moyu / letmoe 等下游站点若开放自己的公开 API(话题列表、资源列表等 GET 小面),一律**并入 NextMoe 开发者平台,作为新 face**;**绝不**由各下游另建/另维护一套开发者平台。
- 不变量:**凭证 / 门户 / 计量 / 契约门,全生态只有一个平面**。第三方开发者一把 `nmk_` key、一个门户账号、一处用量与文档,横跨全部产品面。
- 业界对标:Google / Stripe / GitHub 全产品共用一个 developer console 与凭证体系,产品只是命名空间。本平台的 face / scope 抽象(02 §3.1;演进条款「新媒介加面」)从第一天就是为此设计的——下游面与未来的 manga / novel / anime 面走同一条路。

### 16.2 形态

- **scope**:每个下游面新增 `<site>:read`(如 `kungal:read`),命名沿用 §4 词表规则;敏感能力走审批 scope,不入自助集(旧稿在此援引 `galgame:nsfw` 为前例——它与 NSFW 能力位均已退役,不再是可照抄的形状)。(2026-09-08 改判:**免费只读下游面不设 scope**,任意有效密钥即可调用——首批接线当天 sticker 面的首个冒烟就被面主自己没勾的 scope 403 了,闸门在这类面上的职责是身份与计量,不是授权。`<site>:read` 形状保留给未来真需要授权的面,见 §16.5。)
- **计量**:face 字符串以 `<site>` / `<site>_<sub>` 命名,落 `developer_api_usage`(04 §12;face 列宽教训见 07 §14)。
- **域名**:默认统一 `api.nextmoe.dev/v2/<site>/*`(Traefik 路径分面,现有孪生 router 模式);品牌确需独立域时允许 per-site 域名,但凭证/计量平面**不分裂**——这是唯一不变量,域名只是表皮。(本条原稿写 `/v1/<site>`——写于 wave R3 之前,当时平台在产前缀就是 /v1;R3 整面退役 /v1 后每个 /v1 邻居都回 410 并 Link 指向 /v2,新公开面若钉回 /v1 会被第三方按门户「v1 已整面退役」的说法当成死面。2026-09-08 随首批下游面改判 `/v2/<site>`,见 §16.5。)
- **契约**:每个下游面提供合法 OpenAPI 文档,注册进门户构建(docs-model)+ oasdiff 破坏门 + operation-count 守卫,与 /v1 两面同一套纪律(02 §10)。

### 16.3 接入实现,两档

| 档 | 适用 | 做法 | 下游改动 |
|---|---|---|---|
| **A · 进程内中间件** | 下游后端为 Go,或面会长大/含写路径 | import 共享 devapi 中间件,面服务本地鉴权+计量(catalog 模式,04 §6) | 引一个包 + 挂路由 |
| **B · 网关侧终结** | 任意语言;GET-only 小面 | Traefik **ForwardAuth** 指向 oauth 暴露的 key 校验+计量端点,验完转发下游 | **零改动** |

两档对第三方完全同构(同 key、同门户、同计量)。选择原则:面小而纯读 → B;面要长大 → A。B 档的校验端点属平台侧,首个下游面立项时一并交付。

### 16.4 边界

- 下游面进平台 **≠** 下游数据进 catalog:face 联邦的是鉴权/计量/契约,数据与实现仍归下游仓。
- staff / 站内管理端点永不入面(与 03 §4、06 §11 同则)。
- **本节为预备拍板,触发式执行**:首个下游面立项时按此办,勿提前建设。(触发器已于 2026-09-08 点燃,见 §16.5。)

### 16.5 B 档校验端点(2026-09-08 立项交付)

首个下游面立项,触发 §16.4:**moyu**(www.moyu.moe 补丁资源)与 **sticker**(sticker.kungal.com 表情包)。B 档校验端点随本次交付;网关标签另接线,不在本变更落地。

**端点**:oauth 服务 `GET /internal/devapi/forward-auth?face=<name>`。生产 oauth 的 Traefik router 只覆盖 `/api/v1`、两条 OIDC 元数据路径与面板 web,`/internal/*` 靠路由隔离,不是靠保密。2xx 放行,其余状态原样回给调用方。401 无/坏密钥,429 超限;未注册或缺失 `face` 为 500(Traefik 接线错误,不是客户端错误)。首批曾各配一个 `<site>:read` 自助 scope(缺则 403),2026-09-08 当天即改判撤销:免费只读面收任意有效密钥,不查 scope。

成功时写三个响应头,始终全写,好让 Traefik `authResponseHeaders` 覆盖客户端原值:

- `X-NextMoe-Client-Id`
- `X-NextMoe-Key-Id`
- `X-NextMoe-Tier`

下游只可通过 Traefik `authResponseHeaders` 信任这三个头,不可信客户端原请求。

**路径前缀**:`/v2/<site>/*`(`/v2/moyu`、`/v2/sticker`)。首批接线时(2026-09-08)裁定跟随 §16.2 的修订:/v2 早已不是 catalog 专属(spec 内已有 catalog/store/news/folders/me/moderation/vocabularies 七个命名空间),`<face>` 并列其下是现成形状;门户 docs-model 与 oasdiff 门按 **spec 文档**切分而非按前缀,两份 spec 同描述 `/v2/...` 无冲突。

**计量**:B 档数的是闸门放行的请求,不是下游业务结果(与 A 档不对称,接受)。CDN 命中(`s-maxage` 生效时)不经 Traefik、不入计量——免费只读面接受少计,要严计量的面须自己去掉 `s-maxage`。`UsageRecorder.Record` 的 face 为注册表名(`moyu` / `sticker`),path 为该面静态 `pathLabel`(`/v2/moyu/*` / `/v2/sticker/*`),永不写具体 URI。注册表手维护于 `apps/api/internal/platform/devapi/forwardauth.go`;未注册 face 不得入计量(超长 face 会毒化后续整批 flush,见 model.go Face 列宽教训)。加一张脸 = 注册表一行;要授权的敏感面另立审批 scope 或走 A 档,不复用这条免 scope 通道。

**Traefik 标签配方**(占位符随下游服务别名与端口替换;http 孪生只做跳 https,forwardAuth 挂在 websecure):

```yaml
traefik.enable: "true"
traefik.http.routers.<svc>-pub.rule: Host(`api.nextmoe.dev`) && PathPrefix(`/v2/moyu`)
traefik.http.routers.<svc>-pub.entrypoints: websecure
traefik.http.routers.<svc>-pub.tls.certresolver: letsencrypt
traefik.http.routers.<svc>-pub.middlewares: <svc>-forwardauth
traefik.http.routers.<svc>-pub.service: <svc>-pub
traefik.http.routers.<svc>-pub-http.rule: Host(`api.nextmoe.dev`) && PathPrefix(`/v2/moyu`)
traefik.http.routers.<svc>-pub-http.entrypoints: web
traefik.http.routers.<svc>-pub-http.middlewares: redirect-to-https@file
traefik.http.routers.<svc>-pub-http.service: <svc>-pub
traefik.http.middlewares.<svc>-forwardauth.forwardauth.address: http://<oauth-alias>:9277/internal/devapi/forward-auth?face=moyu
traefik.http.middlewares.<svc>-forwardauth.forwardauth.authResponseHeaders: X-NextMoe-Client-Id,X-NextMoe-Key-Id,X-NextMoe-Tier
traefik.http.services.<svc>-pub.loadbalancer.server.port: "<port>"
```

sticker 面同一套,`PathPrefix(/v2/sticker)` + `face=sticker`(实测别名/端口:sticker-api:9421、moyu-api:5214、oauth:9277)。

ForwardAuth 对所有方法生效,浏览器 `OPTIONS` 预检不带认证头 → 会被 401。**下游面不支持浏览器直连 fetch**——这是想要的(nmk_ key 不该出现在浏览器),门户文档须写明,免得第三方去查自己的 CORS。

`moyu:read` / `sticker:read` 两个自助 scope 只活了不到一天:随本次交付上线,当天被改判撤销(常量删除、铸键拒收两串)——生产核实过零把密钥曾铸入任一,撤得比 news:read 还干净。

## 17. OpenAPI 契约与客户端 SDK(含 Flutter)

### 17.1 spec 是唯一机器契约

Huma 从 handler 生成 /v1 冻结 spec;今日已有三个消费者:门户 docs-model、CI oasdiff 破坏门、operation-count 守卫(02 §10)。一切「客户端如何知道 API 形状」的问题,答案都是 spec,不是散文文档。

### 17.2 客户端 SDK = spec codegen,按真实消费付费

- **Flutter App**(未来一方 App):openapi-generator(`dart-dio`)从门户发布的 /v1 spec 生成类型化 Dart SDK;放 **App 仓**,随 spec 演进重新生成;破坏性漂移由 oasdiff 门在源头拦截,App 端零手写 DTO。
- **纪律**(2026-07 下游 TS codegen 管线退役的教训):**只为真实被消费的端点集生成**;生成物无人 import 时,整条管线(含配套 workflow)一起退役,不留「以后可能用」。管线的死法要和它的生法一样干脆。
- **第三方 SDK**:门户提供 spec 下载即可,官方不维护多语言 SDK;有真实需求再逐语言评估。

### 17.3 非 Huma 面的 spec

下游面(§16)的 spec 可由其自身框架生成或手写——门户与 CI 门只吃**合法 OpenAPI 3 文档**,不要求产自 Huma;质量门同一套。

## 附:相关待办指针

- 门户登录升级为 OP 跳转 SSO(authorization code + PKCE)——已列为下一波待办,见 [05 §9.1](./05-developer-portal.md)。
