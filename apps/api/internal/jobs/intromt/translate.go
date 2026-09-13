package intromt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Translator interface {
	Translate(ctx context.Context, jaText string, gloss Glossary) (zh string, model string, err error)
}

// The prompt is NOT part of src_hash (see hashCandidate), so editing these
// constants retranslates nothing on its own — every existing row still reads as
// up to date. Rewriting a corpus after a prompt change needs the --force lane.
// Putting the prompt in the hash instead would mark all 47k machine rows stale
// and hand the nightly a 47-hour run against a rate-limited gateway.
//
// Rule 3 replaced a blanket "keep every proper noun in the original script".
// 2026-08-23 measured what that produced across 18,168 machine rows: the model
// read ordinary katakana words as proper nouns and shipped them untranslated —
// ヒロイン 240 times, スキル, モンスター, メイド, エルフ, ペンライト — and left
// さん/ちゃん/くん on 1,417 names. Only 389 works had an actual character name
// that the glossary could have fixed. The complaint was never mostly about names.
//
// Rule 2 is the one rule here that deletes text, and it is deliberate. The
// sources are store listings: stripStructuredNoise removes the parts that carry
// a label a regex can match, and that left work 206900 still about 80% credits,
// streaming terms and changelog, and work 1898 about 60% "★500 downloads
// reached" notices. Nothing labelled marks those, so the judgement has to be the
// model's. It contradicts rule 1 on its face, which is why rule 1 says 简介正文
// and not 原文, and why rule 2 carries both an explicit test to apply and an
// instruction to keep anything it cannot classify.
//
// The test first read 「怎么买、怎么用」. 用 was meant to catch install steps, but
// it also matches a key-binding table — and the model dropped the game systems
// sitting next to one. Work 210428 lost 770 characters describing the two-season
// equipment mechanic that the shooter is built around, keeping only the story
// blurb. Hence (f) and the paragraph that follows it: mechanics are content,
// key tables are not.
const TranslateSystemPrompt = `你是资深的游戏本地化译者,专门把日文视觉小说(galgame)的作品简介忠实地翻译成简体中文。翻译要求:
1. 忠实、完整地翻译作品简介正文,不增删、不总结、不改写、不做任何评论。
2. 原文取自商品页,常混入不属于作品简介的内容。以下几类整段省略,既不翻译也不保留:
   (a) 运行环境与规格清单(对应系统、CPU、内存、显卡、分辨率、文件体积、有无语音、CG 与场景数量等);
   (b) 制作人员名单,以及素材、字体、音源、插件的使用鸣谢;
   (c) 版本更新履历、修正补丁说明,以及「○○下载达成追加」这类里程碑通知;
   (d) 二次创作、实况直播、录像转载的许可条款;
   (e) 购买提醒、重复购买与退款说明、商品与社交账号链接、官网导流、发售日与价格公告、促销活动说明;
   (f) 按键配置与操作指引、安装与卸载步骤、存档位置、故障排除。
   但**游戏的玩法、系统与机制属于作品内容,必须保留并翻译**——战斗与养成系统、分支与结局结构、关卡与角色的独有机制等,都是「这是一个什么样的作品」的一部分,不是操作指引。只有纯粹的按键表与安装步骤才归入 (f)。
   判据:这一段说的是「作品讲了什么」,还是「这件商品谁做的、怎么买、怎么装」?后者省略。无法判断时保留,照常翻译。
   若原文通篇都属于上述内容、没有任何简介正文,则照常完整翻译全文,不得输出空白。
3. 译文必须是纯正的简体中文。除第 4 条列出的两种情形外,译文里不得出现任何平假名或片假名。
4. 只有以下两种内容可以保留原文写法:
   (a) 作品标题、品牌/社团/厂牌/公司名、商标、游戏引擎与工具名。这类名称若有通行的中文或拉丁字母写法,优先使用通行写法(例:RPGツクール→RPG Maker)。
   (b) 人名或角色名首次出现时「中文译名(原文)」这一括注里的原文部分。
5. 普通名词与外来语一律译成中文,不得因为它写成片假名就当作专有名词保留。例:スキル→技能、ヒロイン→女主角、モンスター→怪物、メイド→女仆、エルフ→精灵、ダンジョン→地下城、サキュバス→魅魔、シナリオ→剧本、シリーズ→系列、ペンライト→荧光棒。判据:这个词在日语里是任何人都能使用的普通词汇吗?是,就必须翻译。
6. 人名与角色名的处理:
   (a) 术语对照表里有的,一律使用表中给定的中文译名;
   (b) 表里没有而原名是日文汉字的,保留汉字;
   (c) 表里没有而原名是外来语片假名的,使用中文通行译名(例:アリス→爱丽丝、マリア→玛丽亚、ソフィア→索菲亚);
   (d) 表里没有而原名是纯假名的日文名,使用其通行汉字写法,无法确定时按读音音译成汉字。
   任何情况下都不得在译文正文里留下裸露的假名人名。
7. 敬称与称呼(さん、ちゃん、くん、様、先輩 等)按中文习惯处理,不得保留假名。
8. 汉字一律使用简体字形(例:間→间、澤→泽、莊→庄)。
9. 删除汉字后面括号里的假名注音(例:万屋(よろずや)→万屋);括号里有实际信息的内容照常翻译保留。
10. 日式省略号「・・・」写作「……」。
11. 保持简介正文原有的段落与换行结构。例外:若正文是几乎没有分段的长文,请在翻译时按语义划分自然段——只调整分段排版,不改变、不增删任何内容。
12. 遇到无法确定的内容,按字面直译,不要留空或添加译注。
13. 只输出译文正文本身,不要输出原文、解释、前言、后记、标注或任何引号包裹。`

const TranslateSystemPromptEn = `你是资深的游戏本地化译者,负责把视觉小说(galgame)的英文作品简介忠实地翻译成简体中文。请注意:英文原文本身通常是从日文翻译而来的二次文本。翻译要求:
1. 忠实、完整地翻译作品简介正文,不增删、不总结、不改写、不做任何评论。
2. 原文常混入不属于作品简介的内容:运行环境与规格清单、制作人员名单与素材鸣谢、版本更新履历、二次创作与转载许可条款、购买提醒与商品链接、来源标注(如「[From Getchu]」)。这些整段省略,既不翻译也不保留。判据:这一段说的是「作品讲了什么」,还是「这件商品谁做的、怎么买、怎么用」?后者省略。无法判断时保留,照常翻译。若原文通篇都属于上述内容,则照常完整翻译全文,不得输出空白。
3. 名称的处理:
   (a) 作品标题、品牌/社团/厂牌/公司名、商标保留原写法;若有通行的中文写法则使用通行写法。
   (b) 人名与角色名一律译成中文:术语对照表里有的用表中给定的译名;表里没有而能辨认出日文汉字原名的,使用该汉字;其余按中文通行译名或音译写成汉字。不要在译文里留下未翻译的人名。
4. 保持简介正文原有的段落与换行结构。例外:若正文是几乎没有分段的长文,请在翻译时按语义划分自然段——只调整分段排版,不改变、不增删任何内容。
5. 英文原文可能带有转译造成的生硬表达;请按中文的自然表达翻译其含义,但不得改变信息内容。
6. 遇到无法确定的内容,按字面直译,不要留空或添加译注。
7. 只输出译文正文本身,不要输出原文、解释、前言、后记、标注或任何引号包裹。`

const (
	GlossaryHeader = `术语对照表(以下名称在本站已有确定的中文写法,原文 → 中文译名):`
	GlossaryRule   = `对照表中的名称必须使用给定的中文译名。其中人名与角色名在译文中首次出现时写作「中文译名(原文)」,此后一律只用中文译名;若中文译名与原文写法相同则不加括注。作品名与品牌/会社名直接使用中文译名,不加括注。不在对照表中的名称按正文第 4、6 条处理:品牌/会社名与作品名保留原文或其通行写法,人名与角色名一律译成中文,不得在译文正文里留下裸露的假名。`
)

func (g Glossary) PromptSection() string {
	if len(g) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(GlossaryHeader)
	for _, e := range g {
		sb.WriteString("\n")
		sb.WriteString(e.Src)
		sb.WriteString(" → ")
		sb.WriteString(e.Zh)
	}
	sb.WriteString("\n")
	sb.WriteString(GlossaryRule)
	return sb.String()
}

func withGlossary(base string, gloss Glossary) string {
	if len(gloss) == 0 {
		return base
	}
	return base + "\n\n" + gloss.PromptSection()
}

type HTTPTranslator struct {
	sourceLang SourceLang
	baseURL    string
	token      string
	model      string
	effort     string
	maxTokens  int
	http       *http.Client
}

func NewHTTPTranslator(baseURL, token, model string, maxTokens int) *HTTPTranslator {
	return &HTTPTranslator{
		baseURL:   strings.TrimRight(baseURL, "/"),
		token:     token,
		model:     model,
		maxTokens: maxTokens,
		http:      &http.Client{Timeout: 600 * time.Second},
	}
}

func (t *HTTPTranslator) Configured() bool { return t.baseURL != "" && t.token != "" }

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model           string        `json:"model"`
	Messages        []chatMessage `json:"messages"`
	MaxTokens       int           `json:"max_tokens"`
	Temperature     float64       `json:"temperature"`
	ReasoningEffort string        `json:"reasoning_effort,omitempty"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

var retrySchedule = []time.Duration{2 * time.Second, 8 * time.Second, 30 * time.Second, 60 * time.Second}

func (t *HTTPTranslator) Translate(ctx context.Context, jaText string, gloss Glossary) (string, string, error) {
	body := chatRequest{
		Model:           t.model,
		MaxTokens:       t.maxTokens,
		Temperature:     0,
		ReasoningEffort: t.effort,
		Messages: []chatMessage{
			{Role: "system", Content: withGlossary(t.systemPrompt(), gloss)},
			{Role: "user", Content: jaText},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", "", err
	}
	data, err := t.post(ctx, raw)
	if err != nil {
		return "", "", err
	}
	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil {
		return "", "", fmt.Errorf("decode chat response: %w (body: %s)", err, truncate(string(data), 300))
	}
	if cr.Error != nil {
		return "", "", fmt.Errorf("gateway error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", "", fmt.Errorf("gateway returned no choices")
	}
	if fr := cr.Choices[0].FinishReason; fr != "" && fr != "stop" {
		return "", "", fmt.Errorf("generation finished with finish_reason=%q — refusing partial output", fr)
	}
	model := cr.Model
	if model == "" {
		model = t.model
	}
	return strings.TrimSpace(cr.Choices[0].Message.Content), model, nil
}

func (t *HTTPTranslator) post(ctx context.Context, raw []byte) ([]byte, error) {
	var lastErr error
	for attempt := 0; ; attempt++ {
		data, retryable, err := t.postOnce(ctx, raw)
		if err == nil {
			return data, nil
		}
		lastErr = err
		if !retryable || attempt >= len(retrySchedule) {
			return nil, lastErr
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retrySchedule[attempt]):
		}
	}
}

func (t *HTTPTranslator) postOnce(ctx context.Context, raw []byte) (body []byte, retryable bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.token)

	resp, err := t.http.Do(req)
	if err != nil {
		return nil, ctx.Err() == nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("gateway http %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("gateway http %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	return data, false, nil
}

type MockTranslator struct{ Model string }

func (m MockTranslator) Translate(_ context.Context, jaText string, gloss Glossary) (string, string, error) {
	model := m.Model
	if model == "" {
		model = "stub"
	}
	return "【MT・rehearsal mock】[gloss:" + strconv.Itoa(len(gloss)) + "] " + firstRunes(jaText, 60), "mock:" + model, nil
}

func firstRunes(s string, n int) string {
	s = strings.NewReplacer("\n", " ", "\r", " ").Replace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func (t *HTTPTranslator) SetSourceLang(src SourceLang) { t.sourceLang = src }

// A reasoning model left on its own default treats "translate this paragraph"
// as a problem to think about: 2026-08-23 grok-4.6 spent an average of 8,165
// output tokens and 154 seconds on intros whose translation is ~800 tokens, and
// long ones ran past the gateway's 100s ceiling into the retry ladder. Omitted
// when empty, so a lane that does not set it is unaffected.
func (t *HTTPTranslator) SetEffort(effort string) { t.effort = effort }

func (t *HTTPTranslator) systemPrompt() string {
	if t.sourceLang == SourceEn {
		return TranslateSystemPromptEn
	}
	return TranslateSystemPrompt
}
