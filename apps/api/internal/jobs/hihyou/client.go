package hihyou

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"
)

const (
	// SpaceMID is Galgame 批评's bilibili space. The column lives there and
	// nowhere else we can link to: 期1–37 predate the account and exist only on
	// the WeChat 公众号, which has no public permalink.
	SpaceMID = 2072586344

	apiBase   = "https://api.bilibili.com"
	homeURL   = "https://www.bilibili.com/"
	indexPath = "/x/space/article"
	viewPath  = "/x/article/view"

	// codeRateLimited is 请求过于频繁. It is a ROLLING PER-IP QUOTA, not a
	// per-request throttle: a four-attempt retry burst spent ~140 requests to
	// recover 2 articles, while one attempt per article with a 30s gap and a
	// 600s cooldown between passes recovered 30. Hitting harder recovers less.
	codeRateLimited = -509

	userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0 Safari/537.36"
)

// ErrRateLimited is returned instead of a body so a caller cannot mistake the
// -509 envelope for an article and write it into the corpus.
var ErrRateLimited = fmt.Errorf("bilibili: %d 请求过于频繁", codeRateLimited)

type Client struct {
	http    *http.Client
	apiBase string
	homeURL string

	rateLimited int
}

// NewClient performs no I/O. The buvid3 handshake is Warm's job so that a run
// can repeat it per pass — a stale cookie is indistinguishable from the quota in
// the response, so every pass re-acquires one rather than guessing.
func NewClient() (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &Client{http: &http.Client{Jar: jar, Timeout: 60 * time.Second},
		apiBase: apiBase, homeURL: homeURL}, nil
}

func (c *Client) RateLimitedCount() int { return c.rateLimited }

// Warm fetches the homepage for its buvid3 cookie. Without it every API call
// returns -509 unconditionally; with it the code appears only intermittently.
func (c *Client) Warm(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.homeURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (c *Client) get(ctx context.Context, path string, q url.Values, referer string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBase+path+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	var env struct {
		Code int    `json:"code"`
		Msg  string `json:"message"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, err
	}
	if env.Code == codeRateLimited {
		c.rateLimited++
		return nil, ErrRateLimited
	}
	if env.Code != 0 {
		return nil, fmt.Errorf("bilibili: code %d (%s)", env.Code, env.Msg)
	}
	return body, nil
}

// IndexEntry is one article in the space listing. The listing carries the title,
// which is where the Gal周报 filter and the issue number come from — there is no
// need to fetch a review or a translation just to discover it is not a weekly.
type IndexEntry struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	PublishTime int64  `json:"publish_time"`
}

type indexPage struct {
	Data struct {
		Articles []IndexEntry `json:"articles"`
		Count    int          `json:"count"`
	} `json:"data"`
}

// Index returns one page of the space's articles plus the total count. ps is
// capped at 30 upstream.
func (c *Client) Index(ctx context.Context, page, size int) ([]IndexEntry, int, []byte, error) {
	q := url.Values{}
	q.Set("mid", fmt.Sprint(SpaceMID))
	q.Set("pn", fmt.Sprint(page))
	q.Set("ps", fmt.Sprint(size))
	q.Set("sort", "publish_time")
	body, err := c.get(ctx, indexPath, q, fmt.Sprintf("https://space.bilibili.com/%d/article", SpaceMID))
	if err != nil {
		return nil, 0, nil, err
	}
	var p indexPage
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, 0, nil, err
	}
	return p.Data.Articles, p.Data.Count, body, nil
}

// Article fetches one cv. The raw bytes are returned alongside the parsed value
// because the corpus stores the upstream response verbatim: the segmentation
// predicate is still moving, and re-deriving it must never mean asking bilibili
// again.
func (c *Client) Article(ctx context.Context, cv int64) (*Article, []byte, error) {
	q := url.Values{}
	q.Set("id", fmt.Sprint(cv))
	body, err := c.get(ctx, viewPath, q, fmt.Sprintf("https://www.bilibili.com/read/cv%d", cv))
	if err != nil {
		return nil, nil, err
	}
	a, err := ParseArticle(body)
	if err != nil {
		return nil, nil, err
	}
	return a, body, nil
}
