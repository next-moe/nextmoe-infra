---
title: 原生桌面应用接入
eyebrow: 集成指南
description: Tauri / Wails 写的游戏管理器怎么读 /v2/catalog：用用户访问令牌而不是塞进二进制的应用密钥，环回回调 + PKCE 的完整流程与两份代码骨架。
---

# 原生桌面应用接入

分发出去的桌面客户端**没有机密可言**。应用密钥躺在用户机器上的可执行文件里，`strings` 一遍就出来，抓一次 HTTPS 也出来——而泄漏的是**你的**密钥：配额、限流、封禁都算在你的应用头上，吊销一次所有用户一起断。

所以 `/v2/catalog` 只读面**同时接受两种凭据**，二选一：

| 凭据 | 代表谁 | 按什么计配额 | 适合谁 |
|------|--------|-------------|--------|
| 应用密钥 `nmk_live_…` | 你的应用 | 按密钥（tier 决定速率与日配额） | 服务端、你自己控制的后端 |
| 用户访问令牌 | 授权给你的那个用户 | 按**用户**，跨该用户授权过的所有应用共池 | 分发出去的原生客户端 |

> [!NOTE]
> 一条请求只带一个凭据。服务端按 `Authorization` 里那**一个**值的前缀分道：`nmk_` 是应用密钥，其余按用户令牌解析；一种失败了不会再当另一种试一次。

`GET /v2/catalog/claim-events` 与整个 `/v2/store` 是例外，仍然只收应用密钥。

## 1 · 注册应用 {#register}

在[控制台](/dashboard)建应用时开启用户登录（`user_login`），三件事随之定死：

```json
{
  "name": "Kurumi",
  "user_login": {
    "redirect_uris": ["http://127.0.0.1/callback"],
    "scopes": ["openid", "profile", "catalog:read"]
  }
}
```

- **强制 PKCE**。开了用户登录的应用一律是 public client，OAuth 服务在缺 `code_challenge` 时拒签授权码。你**不需要**、也**不应该**在客户端里放 `client_secret`。
- **回调只收环回**。`http://127.0.0.1/callback` 与 `http://[::1]/callback` 是仅有的明文形状。`localhost` 按名拒——它过主机名解析，可以被指向别处，`127.0.0.1` 不能。自定义 scheme（`myapp://callback`）**不支持**，注册时就被拒。
- **端口无关匹配**（RFC 8252 §7.3）。注册时写不写端口都行，服务端比对环回回调时忽略端口，scheme / host / path / query 仍精确匹配。运行时监听哪个临时端口由你决定，不必回控制台改注册。

## 2 · 起监听、开浏览器 {#authorize}

先绑 `127.0.0.1:0` 让内核分配临时端口，拿到端口再拼 `redirect_uri`；`code_verifier` 取 43–128 字符的高熵随机串，`code_challenge = BASE64URL(SHA256(verifier))`；`state` 另取一个，回调里逐字比对。

```http
GET https://oauth.kungal.com/api/v1/oauth/authorize
  ?client_id=<your-client-id>
  &redirect_uri=http%3A%2F%2F127.0.0.1%3A53682%2Fcallback
  &response_type=code
  &scope=openid%20profile%20catalog%3Aread
  &state=<random>
  &code_challenge=<S256>
  &code_challenge_method=S256
```

该端点 302 到登录/同意页，用户同意后浏览器跳回你的环回地址，带 `code` 与 `state`。

> [!WARNING]
> 用**系统浏览器**，绝不用内嵌 WebView（RFC 8252 §8.12）。内嵌视图里应用能读到用户输入的口令和 OP 的 cookie，用户也无从判断自己是不是在真的 OP 上——同意页上那个「第三方应用」标记会因此失去全部意义。Tauri 用 opener / shell 插件，Wails 用 `runtime.BrowserOpenURL`。

## 3 · 换码 {#exchange}

POST 到 `https://oauth.kungal.com/api/v1/oauth/token`，`application/x-www-form-urlencoded` 或 JSON 皆可，**不带 `client_secret`**，带 `code_verifier`：

```http
POST /api/v1/oauth/token
Content-Type: application/x-www-form-urlencoded

grant_type=authorization_code
&code=<授权码>
&redirect_uri=<与第 2 步逐字节相同>
&client_id=<your-client-id>
&code_verifier=<第 2 步的 verifier>
```

响应是裸 RFC 6749（没有 `{code,message,data}` 外壳）：`access_token`（JWT，15 分钟）、`refresh_token`（不透明串）、`expires_in`、`scope`。失败是 `{"error": "...", "error_description": "..."}`。

**把令牌交给操作系统钥匙串**，不要写明文文件或应用配置目录。Tauri 用 keyring / stronghold 插件，Wails（Go）用 `github.com/zalando/go-keyring`。access token 短命，留在内存里就行；refresh token 必须落钥匙串。

## 4 · 刷新与调用 {#call}

刷新只走同一个端点：`grant_type=refresh_token` + `client_id`，同样不带 secret。**每次刷新都会轮换**——旧的立即失效，拿到新的必须原地覆盖钥匙串里那一条。

> [!WARNING]
> 第一方 `/api/v1/auth/refresh` 会**拒绝** client-bound 的 OAuth session。已经有集成方在这里撞过：它不是一条可替代的路径。

```http
GET https://api.nextmoe.dev/v2/catalog/works?limit=20
Authorization: Bearer <access token>
```

令牌**必须持有 `catalog:read`**，否则是 `403 SCOPE_REQUIRED`。2026-09-06 之前签发的令牌不带这个 scope，也不做追认——让用户重新授权一次即可。

配额按**用户**算：同一个人授权了三个管理器，三个共用一个桶（默认 100 次/分钟、10000 次/UTC 日）。这是有意的——否则「多注册几个应用」就是一条绕开配额的路。要更高吞吐就该用应用密钥，而那意味着你需要一个自己的服务端。

## 5 · 代码骨架 {#sketches}

两份都省去了错误处理与日志，只留形状。

**Tauri（Rust）**：

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

#[tauri::command]
async fn sign_in(app: tauri::AppHandle) -> Result<String, String> {
    let verifier = URL_SAFE_NO_PAD.encode(rand::random::<[u8; 32]>());
    let challenge = URL_SAFE_NO_PAD.encode(Sha256::digest(verifier.as_bytes()));
    let state = URL_SAFE_NO_PAD.encode(rand::random::<[u8; 16]>());

    // 端口 0：内核挑端口，注册的环回回调按 RFC 8252 §7.3 忽略端口
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
    // 系统浏览器，永远不用内嵌 webview
    tauri_plugin_opener::open_url(&url, None::<&str>).map_err(|e| e.to_string())?;

    let req = server.recv().map_err(|e| e.to_string())?;
    let q: HashMap<String, String> = form_urlencoded::parse(
        req.url().split_once('?').map(|(_, q)| q).unwrap_or("").as_bytes(),
    )
    .into_owned()
    .collect();
    req.respond(tiny_http::Response::from_string("可以关闭此页面。")).ok();
    if q.get("state") != Some(&state) {
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

    // refresh token 每次用都轮换：覆盖，不是追加
    app.keyring()
        .set_password("nextmoe", "refresh_token", &tokens.refresh_token)
        .map_err(|e| e.to_string())?;
    Ok(tokens.access_token)
}
```

**Wails（Go）**：

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
	runtime.BrowserOpenURL(ctx, oauthBase+"/oauth/authorize?"+q.Encode())

	codeCh := make(chan string, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, "可以关闭此页面。")
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

	// public client：用 code_verifier，没有 client secret
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
	// 轮换：这是覆盖，不是新增
	if err := keyring.Set("nextmoe", "refresh_token", tok.RefreshToken); err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}
```

## 常见错误 {#pitfalls}

| 症状 | 原因 |
|------|------|
| 授权时 `15006` | 请求的 scope 不在应用的 `allowed_scopes` 内。到控制台把 `catalog:read` 加进用户登录的 scope。 |
| 换码 `invalid_grant` | `redirect_uri` 与授权那步不是逐字节相同，或 `code_verifier` 对不上 challenge，或码已用过（授权码一次性）。 |
| `403 SCOPE_REQUIRED` | 令牌不带 `catalog:read`。旧令牌不追认，重新走一次授权。 |
| `401 INVALID_CREDENTIAL` | 令牌过期，或者你把它打到了 `claim-events` / `/v2/store`——那两处只收应用密钥。 |
| 刷新 401 而令牌确实没过期 | 用了第一方 `/api/v1/auth/refresh`。OAuth session 只能经 `/oauth/token` 刷新。 |
| 注册时回调被拒 | `localhost`、自定义 scheme、带 fragment、或非环回的明文 http。 |

- [鉴权与凭据](/docs/authentication) — 两种凭据各自能开哪些面，失败长什么样。
- [接入用户数据](/docs/user-data) — 同一把用户令牌还能读写 `/v2/me`：时长、认领、编辑提案。
