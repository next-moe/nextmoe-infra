# 原生桌面应用接入(Tauri / Wails)

> 拍板 2026-09-06。回答一个问题:**分发出去的桌面客户端(游戏管理器、启动器、同步工具)拿什么凭证读 `/v2/catalog`**。答案是**用户访问令牌**,不是塞进二进制的 `nmk_` 应用密钥。本文档是拆分后新增的内容,章节号自 **§18** 起续用同一稳定锚点空间(§1–§15 见 01–07,§16/§17 见 [08](./08-downstream-faces-and-sdk.md))。自助注册与四道护栏见 [05 §9.2](./05-developer-portal.md);凭证词表见 [03 §4](./03-auth-and-tiers.md)。

---

## 18. 原生桌面应用接入

### 18.1 为什么不能把 `nmk_` 装进二进制

应用密钥是**机密**,而分发出去的桌面客户端**没有机密可言**。密钥躺在用户机器上的可执行文件里,`strings` 一遍就出来;抓一次 HTTPS 也出来。它一旦泄漏:

- 泄漏的是**你的**密钥。配额、限流、封禁都记在你的应用头上,而拿去用的人不是你的用户。
- 吊销的代价是**所有人一起断**。轮换意味着发新版本,而用户不会同时升级。
- 出问题时没有可归因的主体:用量表里只有一把 key,分不出是谁打的。

这不是本平台的特殊规定,是 RFC 8252(OAuth 2.0 for Native Apps)整篇文档的前提。因此自 **2026-09-06(v2 spec 2.11.0)**起,`/v2/catalog` 只读面**同时接受两种凭证**:

| 凭证 | 主体 | 计量口径 | 适合谁 |
|------|------|---------|--------|
| 应用密钥 `nmk_live_…` | 你的应用 | 按 key(tier 决定速率与日配额) | 服务端、你自己控制的后端 |
| 用户访问令牌 | 授权给你的那个用户 | 按**用户**(跨该用户授权过的所有应用共池) | 分发出去的原生客户端 |

**一条请求只带一个凭证**(refs/api-v2 D1)。网关按 `Authorization: Bearer` 里那一个值的前缀分道:`nmk_` 走应用密钥,其余走用户令牌。不存在「两个都带」的形状,也不存在「key 失败了再当令牌试一次」的兜底。

**唯一例外**:`GET /v2/catalog/claim-events` 仍然只收应用密钥。它额外要 `claim_events:read`,而那是运营方按需授予应用的 scope,同意页上没有对应的条目可勾。

### 18.2 注册应用

在开发者门户按 [05 §9.2](./05-developer-portal.md) 建一个带 `user_login` 的应用:

```json
{
  "name": "Kurumi",
  "user_login": {
    "redirect_uris": ["http://127.0.0.1/callback"],
    "scopes": ["openid", "profile", "catalog:read"]
  }
}
```

三件事在这里定死:

1. **`is_public=true`、强制 PKCE**。给了 `user_login` 的应用一律是 public client,OAuth 服务在缺 `code_challenge` 时**拒签授权码**。你**不需要**、也**不应该**在客户端里放 `client_secret`。
2. **回调只收环回**。`http://127.0.0.1/callback` 与 `http://[::1]/callback` 是仅有的明文形状;`localhost` 按名拒(它过主机名解析,可以被指向别处),自定义 scheme(`myapp://callback`)**不支持**——注册时就被拒。
3. **端口无关匹配**(RFC 8252 §7.3)。注册时写不写端口都行,服务端比对环回回调时**忽略端口**,scheme / host / path / query 仍精确匹配。运行时监听哪个临时端口由你决定,不必回门户改注册。

### 18.3 完整流程

以下每一步都是必需的,顺序不可换。

**第 1 步:生成 PKCE 与 state。** `code_verifier` 是 43–128 个字符的高熵随机串;`code_challenge = BASE64URL(SHA256(code_verifier))`,`code_challenge_method=S256`。`state` 另取一个随机串,回调里必须逐字比对。

**第 2 步:起一个环回监听器。** 绑 `127.0.0.1:0` 让内核分配临时端口,拿到实际端口后再拼 `redirect_uri`。监听器只服务这一次授权,收到 code 立即关闭。

**第 3 步:用系统浏览器打开授权 URL。**

```
https://oauth.kungal.com/api/v1/oauth/authorize
  ?client_id=<你的 client_id>
  &redirect_uri=http%3A%2F%2F127.0.0.1%3A53682%2Fcallback
  &response_type=code
  &scope=openid%20profile%20catalog%3Aread
  &state=<random>
  &code_challenge=<S256 challenge>
  &code_challenge_method=S256
```

该端点 302 到 OP 前端的登录/同意页,用户同意后浏览器跳回你的环回地址,带 `code` 与 `state`。

**绝不使用内嵌 WebView**(RFC 8252 §8.12)。内嵌视图里应用能读到用户输入的口令与 OP 的 cookie,用户也无从判断自己是在真的 OP 上——同意页上那个「第三方应用」标记因此失去全部意义。Tauri 用 opener / shell 插件,Wails 用 `runtime.BrowserOpenURL`,两者都会交给系统默认浏览器。

**第 4 步:换码。** POST 到 `https://oauth.kungal.com/api/v1/oauth/token`,`application/x-www-form-urlencoded` 或 JSON 皆可,**不带 `client_secret`**,带 `code_verifier`:

```
grant_type=authorization_code
code=<授权码>
redirect_uri=<与第 3 步逐字节相同>
client_id=<你的 client_id>
code_verifier=<第 1 步的 verifier>
```

成功响应是 RFC 6749 §5.1 的裸 JSON(**没有 `{code,message,data}` 信封**):`access_token`(15 分钟)、`refresh_token`(不透明串)、`expires_in`、`scope`。失败是 §5.2 的 `{"error","error_description"}`。

**第 5 步:把令牌交给操作系统钥匙串。** 不写明文文件,不写应用配置目录。Tauri 用 keyring / stronghold 插件,Wails(Go)用 `github.com/zalando/go-keyring`。access token 短命可以只留在内存里,refresh token 必须持久化到钥匙串。

**第 6 步:刷新只走 `/oauth/token`。** `grant_type=refresh_token` + `client_id`,同样不带 secret。**第一方 `/api/v1/auth/refresh` 会拒绝 client-bound 的 OAuth session**——已经有集成方在这里撞过,它不是一条可替代的路径。每次刷新都轮换:旧的 refresh token 立即失效,拿到新的必须原地覆盖钥匙串里的那一条。

**第 7 步:调用。**

```
GET https://api.nextmoe.dev/v2/catalog/works?limit=20
Authorization: Bearer <access token>
```

令牌**必须持有 `catalog:read`**,否则是 `403 SCOPE_REQUIRED`。2026-09-06 之前签发的令牌不带这个 scope,也不做任何追认——用户重新授权一次即可。

### 18.4 配额

用户令牌打 `/v2/catalog` 按**用户**计量,不按应用:同一个人授权了三个管理器,三个共用同一个桶。这是有意的(GitHub 同款口径)——否则「多注册几个应用」就是一条绕开配额的路。

默认值由 `apiv2.default_rate_per_minute` = **100 次/分钟** 与 `apiv2.default_quota_per_day` = **10000 次/UTC 日** 给出(配置中心可调,以运行时值为准)。超出是 `429`,响应带 `RateLimit-Policy` / `RateLimit-Remaining`。

对照:应用密钥按 tier 计量,free 档 60/分钟、50000/日。**要更高的吞吐就该用应用密钥,而那意味着你需要一个自己的服务端**——把用户令牌的桶做大不是本面的选项。

### 18.5 代码骨架

两份都省去了错误处理与日志,只保留形状。

**Tauri(Rust)**——PKCE + 环回监听 + 换码:

```rust
use base64::{engine::general_purpose::URL_SAFE_NO_PAD, Engine};
use serde::Deserialize;
use sha2::{Digest, Sha256};
use std::collections::HashMap;
use tiny_http::Server;

const OAUTH: &str = "https://oauth.kungal.com/api/v1";
const CLIENT_ID: &str = "your-client-id";

#[derive(Deserialize)]
struct TokenResponse {
    access_token: String,
    refresh_token: String,
}

fn pkce() -> (String, String) {
    let verifier: String = URL_SAFE_NO_PAD.encode(rand::random::<[u8; 32]>());
    let challenge = URL_SAFE_NO_PAD.encode(Sha256::digest(verifier.as_bytes()));
    (verifier, challenge)
}

#[tauri::command]
async fn sign_in(app: tauri::AppHandle) -> Result<String, String> {
    let (verifier, challenge) = pkce();
    let state: String = URL_SAFE_NO_PAD.encode(rand::random::<[u8; 16]>());

    // Port 0: the kernel picks the port, and the registered loopback callback
    // matches regardless of it (RFC 8252 §7.3).
    let server = Server::http("127.0.0.1:0").map_err(|e| e.to_string())?;
    let port = server.server_addr().to_ip().unwrap().port();
    let redirect = format!("http://127.0.0.1:{port}/callback");

    let url = format!(
        "{OAUTH}/oauth/authorize?client_id={CLIENT_ID}&redirect_uri={}\
         &response_type=code&scope={}&state={state}\
         &code_challenge={challenge}&code_challenge_method=S256",
        urlencoding::encode(&redirect),
        urlencoding::encode("openid profile catalog:read"),
    );
    // System browser, never an embedded webview (RFC 8252 §8.12).
    tauri_plugin_opener::open_url(&url, None::<&str>).map_err(|e| e.to_string())?;

    let req = server.recv().map_err(|e| e.to_string())?;
    let q: HashMap<_, _> = form_urlencoded::parse(
        req.url().split_once('?').map(|(_, q)| q).unwrap_or("").as_bytes(),
    )
    .into_owned()
    .collect();
    req.respond(tiny_http::Response::from_string("You can close this window."))
        .ok();
    if q.get("state").map(String::as_str) != Some(state.as_str()) {
        return Err("state mismatch".into());
    }

    let tokens: TokenResponse = reqwest::Client::new()
        .post(format!("{OAUTH}/oauth/token"))
        .form(&[
            ("grant_type", "authorization_code"),
            ("code", q.get("code").ok_or("no code")?),
            ("redirect_uri", &redirect),
            ("client_id", CLIENT_ID),
            ("code_verifier", &verifier),
        ])
        .send()
        .await
        .map_err(|e| e.to_string())?
        .json()
        .await
        .map_err(|e| e.to_string())?;

    // The refresh token rotates on every use: overwrite, never append.
    app.keyring()
        .set_password("nextmoe", "refresh_token", &tokens.refresh_token)
        .map_err(|e| e.to_string())?;
    Ok(tokens.access_token)
}
```

**Wails(Go)**——同一条流程:

```go
const (
	oauthBase = "https://oauth.kungal.com/api/v1"
	clientID  = "your-client-id"
	scopes    = "openid profile catalog:read"
)

func (a *App) SignIn(ctx context.Context) (string, error) {
	verifier := randomURLSafe(32)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	state := randomURLSafe(16)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer ln.Close()
	redirect := fmt.Sprintf("http://127.0.0.1:%d/callback", ln.Addr().(*net.TCPAddr).Port)

	q := url.Values{
		"client_id": {clientID}, "redirect_uri": {redirect},
		"response_type": {"code"}, "scope": {scopes}, "state": {state},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}
	// System browser. An embedded webview would see the user's password and the
	// OP's cookies, and the consent page's third-party marker would mean nothing.
	runtime.BrowserOpenURL(ctx, oauthBase+"/oauth/authorize?"+q.Encode())

	codeCh := make(chan string, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, "You can close this window.")
		codeCh <- r.URL.Query().Get("code")
	})}
	go srv.Serve(ln)
	defer srv.Close()

	var code string
	select {
	case code = <-codeCh:
	case <-time.After(5 * time.Minute):
		return "", errors.New("authorization timed out")
	}

	// Public client: code_verifier instead of a client secret.
	resp, err := http.PostForm(oauthBase+"/oauth/token", url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {redirect}, "client_id": {clientID},
		"code_verifier": {verifier},
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", err
	}
	// Rotation: this replaces the stored one, it does not add to it.
	if err := keyring.Set("nextmoe", "refresh_token", tok.RefreshToken); err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}
```

刷新只是同一个端点换一组参数:

```go
resp, err := http.PostForm(oauthBase+"/oauth/token", url.Values{
	"grant_type": {"refresh_token"}, "client_id": {clientID},
	"refresh_token": {stored},
})
```

### 18.6 常见错误

| 症状 | 原因 |
|------|------|
| `15006` | 请求的 scope 不在应用的 `allowed_scopes` 内。到门户把 `catalog:read` 加进 `user_login.scopes`。 |
| 授权码换取 `invalid_grant` | `redirect_uri` 与第 3 步不是逐字节相同,或 `code_verifier` 对不上 challenge,或码已用过(授权码一次性)。 |
| `403 SCOPE_REQUIRED` | 打 `/v2/catalog` 而令牌不带 `catalog:read`,或打 `/v2/me/folders` 而不带 `folder:read` / `folder:write`(§18.7)。响应点名缺的是哪一个;旧令牌不追认,重新走一次授权。 |
| `/v2/catalog` 返回 `401 INVALID_CREDENTIAL` | 令牌过期、签发方不是本 OP,或者你把令牌打到了 `claim-events` / `/v2/store`——那两处只收应用密钥。 |
| 刷新返回 401 而令牌确实没过期 | 用了第一方 `/api/v1/auth/refresh`。OAuth session 只能经 `/oauth/token` 刷新。 |
| 注册时回调被拒 | `localhost`、自定义 scheme、带 fragment、或非环回的明文 http。见 [05 §9.2](./05-developer-portal.md) 护栏 1。 |

### 18.7 收藏夹同步

管理器的另一半工作是**用户自己的库**。`/v2/me/folders` 是这份库在平台侧的规范存放处：收藏夹本身九个操作,加上夹内条目的读、增、删。它和论坛、moyu 各自的收藏表不是一回事——把规范副本放在这里,是为了同一个 work id 在三处指同一部作品。

**两个 scope,而且是真的强制的。** `/v2/me` 的其余各面只认「这个人的令牌」,不看应用被授了什么;收藏夹是唯一的例外,因为一份收藏是私人清单,「凡是被授权过的应用都能读 `/v2/me`」等于把整份清单交给用户登录过的每一个应用。

| scope | 覆盖 |
|-------|------|
| `folder:read` | 所有 GET / HEAD |
| `folder:write` | 其余全部方法,并且**同时**满足读 |

只要在应用的 `user_login.scopes` 里加上它们,并在授权 URL 的 `scope` 里一并请求即可。缺了就是 `403 SCOPE_REQUIRED`,响应里点名缺的是哪一个。只申请了写权限的管理器仍然读得回自己写的东西——这是有意的,不必为了读回一次自己的写而多要一个勾。

**冷启动:全量拉一次。**

```
GET /v2/me/folders?limit=100
GET /v2/me/folders/<id>/items?limit=100
```

前者按 id 升序翻页,后者按 `updated_at` 升序翻页,两者都用 `next_cursor` 续页,`next_cursor` 为 `null` 即到底。

**稳态:条目游标就是水位线。** 夹内条目按 `updated_at` 升序做 keyset 翻页(`work_id` 做同刻并列的破平);对外游标是不透明的 `cur_` 前缀字符串,客户端不要解析、不要自己构造。把最后一页的 `next_cursor` **存下来**,下次原样回放,拿到的就是这之后变过的条目:

```
GET /v2/me/folders/<id>/items?cursor=<上次存的 next_cursor>&limit=100
```

两件事必须清楚:

- **重复添加不动水位线。** `PUT` 一条已经在夹里的条目是完全的空操作,`updated_at` 不变。所以管理器每次启动整库上传一遍是安全的——如果这一下会刷新时间戳,这个用户其他设备上的客户端每次都要把整个收藏夹重新拉一遍。
- **删除不会在增量里回放。** 游标只走存在的行。要检测删除,需要重新全量拉一次该夹后与本地取差集;`item_count` 与夹自身的 `updated_at` 可以用来判断值不值得拉。

**写:逐条幂等,或者一次一百条。**

```
PUT    /v2/me/folders/<id>/items/<work_id>      → 200,已存在则原样返回
DELETE /v2/me/folders/<id>/items/<work_id>      → 204,本来就不在也是 204
POST   /v2/me/folders/<id>/items                → 207,body 为 {"items":[{"work_id":"..."}]}
```

批量一次最多 **100** 条,响应是 `207 Multi-Status`:`items[]` 与请求逐位对应,每项要么是 `{status:200, object:"folder_item", work_id}`,要么带一个完整的 problem 对象。整体不是事务——部分成功是正常结果,按项读状态,不要看 HTTP 状态码。

**边界。** 每人最多 **200** 个收藏夹,每夹最多 **10,000** 条,超出是 422。加入的作品必须是 `live`,隔离或不存在的 id 是 404。`is_default` 全用户单持有:设到另一个夹上会自动摘掉原持有者,传 `false` 是 422,默认夹在把标记移走之前删不掉。`visibility: public` 目前只是存下来的意向,没有公开浏览面。

**合并会移动条目。** 目录侧把两部作品判为同一部时,指向被退役 id 的条目会改指幸存者,并且**故意**刷新 `updated_at`——这是唯一一次条目在没人动它的情况下出现在增量里,因为你手上那个 id 已经不解析了。同一个夹里两边都收藏过的情况会合成一条,`item_count` 随之重算。
