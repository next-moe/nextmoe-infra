# 原生桌面应用接入

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

分发出去的桌面客户端**没有机密可言**。应用密钥躺在用户机器上的可执行文件里，`strings` 一遍就出来，抓一次 HTTPS 也出来——而泄漏的是**你的**密钥：配额、限流、封禁都算在你的应用头上，吊销一次所有用户一起断。

所以 `/v2/catalog` 只读面**同时接受两种凭据**，二选一：

| 凭据 | 代表谁 | 按什么计配额 | 适合谁 |
|------|--------|-------------|--------|
| 应用密钥 `nmk_live_…` | 你的应用 | 按密钥（tier 决定速率与日配额） | 服务端、你自己控制的后端 |
| 用户访问令牌 | 授权给你的那个用户 | 按**用户**，跨该用户授权过的所有应用共池 | 分发出去的原生客户端 |

> [!NOTE]
> 一条请求只带一个凭据。服务端按 `Authorization` 里那**一个**值的前缀分道：`nmk_` 是应用密钥，其余按用户令牌解析；一种失败了不会再当另一种试一次。

`GET /v2/catalog/claim-events` 与整个 `/v2/store` 是例外，仍然只收应用密钥。

## 1 · 注册应用

在[控制台](/dashboard)建应用后，进入应用详情页的「用户登录」卡片开启并配置（`user_login`）——回调地址与 scope 随时可改，全程自助，无需联系平台管理员。落下来的配置形如下面这份，三件事随之定死：

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

2026-09 有下游读到这一节后在控制台里找不到对应的表单，据此判定回调地址只能由平台管理员代注册——那时门户确实没有这张表单，「用户登录」卡片就是为补上这个缺口而加的。

> [!NOTE]
> **移动端（Android / iOS）走同样的两条路，不需要自定义 scheme。**
> 环回监听在手机上同样成立——绑 `127.0.0.1:0`，端口无关匹配的规则与桌面完全一致。自定义 scheme 不开是刻意的：scheme 在移动系统上不可认领，任何应用都能抢注同一个 scheme 拦走授权码，而 PKCE 防不住由拦截方自己发起的流程。有自己域名的应用推荐第二条路：注册 `https://` 回调（本来就收），在系统侧配成 App Links / Universal Links（RFC 8252 §7.2）——域名归属由操作系统验证，授权完成后直接跳回应用，安全性与体验都优于环回。

## 2 · 起监听、开浏览器

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

## 3 · 换码

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

## 4 · 刷新与调用

刷新只走同一个端点：`grant_type=refresh_token` + `client_id`，同样不带 secret。**每次刷新都会轮换**——旧的立即失效，拿到新的必须原地覆盖钥匙串里那一条。

> [!WARNING]
> 第一方 `/api/v1/auth/refresh` 会**拒绝** client-bound 的 OAuth session。已经有集成方在这里撞过：它不是一条可替代的路径。

```http
GET https://api.nextmoe.dev/v2/catalog/works?limit=20
Authorization: Bearer <access token>
```

令牌**必须持有 `catalog:read`**，否则是 `403 SCOPE_REQUIRED`。2026-09-06 之前签发的令牌不带这个 scope，也不做追认——让用户重新授权一次即可。

配额按**用户**算：同一个人授权了三个管理器，三个共用一个桶（默认 100 次/分钟、10000 次/UTC 日）。这是有意的——否则「多注册几个应用」就是一条绕开配额的路。要更高吞吐就该用应用密钥，而那意味着你需要一个自己的服务端。

## 5 · 代码骨架

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

## 6 · 收藏夹同步

管理器还有一半工作在**用户自己的库**上。`/v2/me/folders` 是这份库在平台侧的规范存放处：收藏夹本身九个操作，加上夹内条目的读、增、删。

> [!IMPORTANT]
> 这是 `/v2/me` 上**唯一**真的看 scope 的一族。其余各面只认「这个人的令牌」，不问应用被授了什么；收藏夹是私人清单，那条规矩在这里等于把整份清单交给用户登录过的每一个应用。`folder:read` 覆盖 GET / HEAD，`folder:write` 覆盖其余方法**并且同时满足读**——只申请了写的管理器仍读得回自己写的东西。两个都要加进应用的 `user_login.scopes`，并在授权 URL 的 `scope` 里一并请求；缺了是 `403 SCOPE_REQUIRED`，响应里点名缺哪一个。

**冷启动**：全量各拉一次。前者按 id 升序，后者按 `updated_at` 升序，都用 `next_cursor` 续页，`null` 即到底。

```http
GET /v2/me/folders?limit=100
GET /v2/me/folders/<id>/items?limit=100
Authorization: Bearer <access token>
```

**稳态**：条目游标就是水位线。夹内条目按 `updated_at` 升序做 keyset 翻页，游标是不透明的 `cur_` 前缀字符串——不要解析、不要自己构造。把最后一页的 `next_cursor` 存下来，下次原样回放，拿到的就是这之后变过的条目。

```http
GET /v2/me/folders/<id>/items?cursor=<上次存的 next_cursor>&limit=100
```

- **重复添加不动水位线。** `PUT` 一条已经在夹里的条目是完全的空操作，`updated_at` 不变。所以每次启动整库上传一遍是安全的——若这一下会刷新时间戳，该用户其他设备上的客户端每次都得把整个收藏夹重拉一遍。
- **删除不会在增量里回放。** 游标只走还存在的行。要检测删除得重新全量拉一次该夹再与本地取差集；夹自身的 `item_count` 与 `updated_at` 可以用来判断值不值得拉。

**写**：逐条幂等，或者一次一百条。

```http
PUT    /v2/me/folders/<id>/items/<work_id>   → 200，已存在则原样返回
DELETE /v2/me/folders/<id>/items/<work_id>   → 204，本来就不在也是 204
POST   /v2/me/folders/<id>/items             → 207，{"items":[{"work_id":"..."}]}
```

批量一次最多 **100** 条，响应是 `207 Multi-Status`：`items[]` 与请求逐位对应，每项要么是 `{status:200, object:"folder_item", work_id}`，要么带一个完整的 problem 对象。整体不是事务，部分成功是正常结果——按项读 `status`，不要看 HTTP 状态码。

**边界**：每人最多 **200** 个收藏夹，每夹最多 **10,000** 条，超出 422。加入的作品必须是 `live`，隔离或不存在的 id 是 404。`is_default` 全用户单持有——设到另一个夹上会自动摘掉原持有者，传 `false` 是 422，默认夹在标记移走之前删不掉。`visibility: public` 目前只是存下来的意向，还没有公开浏览面。

**合并会移动条目**：目录把两部作品判为同一部时，指向被退役 id 的条目改指幸存者，并且**故意**刷新 `updated_at`。这是条目唯一一次在没人动它的情况下出现在增量里，因为你手上那个 id 已经不解析了。同一个夹里两边都收藏过的会合成一条，`item_count` 随之重算。

## 常见错误

| 症状 | 原因 |
|------|------|
| 授权时 `15006` | 请求的 scope 没在应用注册的 scope 里。到应用详情页「用户登录」卡片勾上缺的那个，重新授权。`catalog:read` 不会触发它——每个应用无需注册即可请求。 |
| 换码 `invalid_grant` | `redirect_uri` 与授权那步不是逐字节相同，或 `code_verifier` 对不上 challenge，或码已用过（授权码一次性）。 |
| `403 SCOPE_REQUIRED` | 打 `/v2/catalog` 而令牌不带 `catalog:read`，或打 `/v2/me/folders` 而不带 `folder:read` / `folder:write`。响应会点名缺哪一个；旧令牌不追认，重新走一次授权。 |
| `401 INVALID_CREDENTIAL` | 令牌过期，或者你把它打到了 `claim-events` / `/v2/store`——那两处只收应用密钥。 |
| 刷新 401 而令牌确实没过期 | 用了第一方 `/api/v1/auth/refresh`。OAuth session 只能经 `/oauth/token` 刷新。 |
| 注册时回调被拒 | `localhost`、自定义 scheme、带 fragment、或非环回的明文 http。移动端不必等 scheme 放开：环回照用，或注册 `https://` 回调配 App Links / Universal Links，见 [§1](#register)。 |

- [鉴权与凭据](/docs/authentication) — 两种凭据各自能开哪些面，失败长什么样。
- [接入用户数据](/docs/user-data) — 同一把用户令牌还能读写 `/v2/me`：时长、认领、编辑提案。

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/native-app
