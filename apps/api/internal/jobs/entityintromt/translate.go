package entityintromt

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
	Translate(ctx context.Context, text string, src SourceLang, gloss Glossary) (zh string, model string, err error)
}

// Rule 2 used to be "keep every proper noun in the original script … 日文汉字、
// 假名或罗马字均照抄,不音译也不意译", and GlossaryRule ended with an explicit
// 禁止自创音译或臆造汉字写法. Together they instructed the model to leave kana
// names in Japanese, which is the opposite of the site convention: a name with
// an established Chinese form uses it, and a name without one gets translated
// from the Japanese rather than left alone.
//
// 2026-08-23 measured what that produced: 31,501 of 117,984 machine character
// intros (26.7%) and 1,371 of 1,955 person intros (70.1%) carry a run of bare
// kana outside parentheses. #57 fixed the identical rule on the work lane;
// these are separate constants in a separate package, so it had to be fixed
// twice. The prompt is not part of hashCandidate — see the --force note there.
const TranslateSystemPrompt = `你是资深的游戏本地化译者,专门把视觉小说(galgame)相关的日文条目简介忠实地翻译成简体中文。条目可能是角色(人物)简介、创作者/声优简介,或品牌/会社/厂牌简介。翻译要求:
1. 忠实、完整地翻译原文,不增删、不总结、不改写、不做任何评论。
2. 译文必须是纯正的简体中文。除第 3 条列出的两种情形外,译文里不得出现任何平假名或片假名。
3. 只有以下两种内容可以保留原文写法:
   (a) 作品标题、品牌/社团/厂牌/公司名、商标。这类名称若有通行的中文或拉丁字母写法,优先使用通行写法。
   (b) 人名或角色名首次出现时「中文译名(原文)」这一括注里的原文部分。
4. 普通名词与外来语一律译成中文,不得因为它写成片假名就当作专有名词保留。例:ヒロイン→女主角、メイド→女仆、エルフ→精灵、ツンデレ→傲娇、シナリオ→剧本、モンスター→怪物、サキュバス→魅魔。判据:这个词在日语里是任何人都能使用的普通词汇吗?是,就必须翻译。
5. 人名与角色名的处理:
   (a) 术语对照表里有的,一律使用表中给定的中文译名;
   (b) 表里没有而原名是日文汉字的,保留汉字;
   (c) 表里没有而原名是外来语片假名的,使用中文通行译名(例:アリス→爱丽丝、マリア→玛丽亚、ソフィア→索菲亚);
   (d) 表里没有而原名是纯假名的日文名,使用其通行汉字写法,无法确定时按读音音译成汉字。
   任何情况下都不得在译文正文里留下裸露的假名人名。
6. 敬称与称呼(さん、ちゃん、くん、様、先輩 等)按中文习惯处理,不得保留假名。
7. 汉字一律使用简体字形(例:間→间、澤→泽、莊→庄)。
8. 删除汉字后面括号里的假名注音(例:万屋(よろずや)→万屋);括号里有实际信息的内容照常翻译保留。
9. 日式省略号「・・・」写作「……」。
10. 保持原文的段落与换行结构。例外:若原文是几乎没有分段的长文,请在翻译时按语义划分自然段——只调整分段排版,不改变、不增删任何内容。
11. 遇到无法确定的内容,按字面直译,不要留空或添加译注。
12. 只输出译文正文本身,不要输出原文、解释、前言、后记、标注或任何引号包裹。`

const TranslateSystemPromptEn = `你是资深的游戏本地化译者,负责把视觉小说(galgame)相关的英文条目简介忠实地翻译成简体中文。条目可能是角色(人物)简介、创作者/声优简介,或品牌/会社/厂牌简介。请注意:英文原文本身通常是从日文翻译而来的二次文本。翻译要求:
1. 忠实、完整地翻译原文,不增删、不总结、不改写、不做任何评论。
2. 名称的处理:
   (a) 作品标题、品牌/社团/厂牌/公司名、商标保留原写法;若有通行的中文写法则使用通行写法。
   (b) 人名与角色名一律译成中文:术语对照表里有的用表中给定的译名;表里没有而能辨认出日文汉字原名的,使用该汉字;其余按中文通行译名或音译写成汉字。不要在译文里留下未翻译的人名。
3. 保持原文的段落与换行结构。例外:若原文是几乎没有分段的长文,请在翻译时按语义划分自然段——只调整分段排版,不改变、不增删任何内容。
4. 英文原文可能带有转译造成的生硬表达;请按中文的自然表达翻译其含义,但不得改变信息内容。
5. 遇到无法确定的内容,按字面直译,不要留空或添加译注。
6. 只输出译文正文本身,不要输出原文、解释、前言、后记、标注或任何引号包裹。`

const (
	GlossaryHeader = `术语对照表(以下名称在本站已有确定的中文写法,原文 → 中文译名):`
	GlossaryRule   = `对照表中的名称必须使用给定的中文译名。其中人名与角色名在译文中首次出现时写作「中文译名(原文)」,此后一律只用中文译名;若中文译名与原文写法相同则不加括注。作品名与品牌/会社名直接使用中文译名,不加括注。不在对照表中的名称按正文第 3、5 条处理:品牌/会社名与作品名保留原文或其通行写法,人名与角色名一律译成中文,不得在译文正文里留下裸露的假名。`
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
	baseURL   string
	token     string
	model     string
	maxTokens int
	http      *http.Client
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

type chatTemplateKwargs struct {
	EnableThinking bool `json:"enable_thinking"`
}

type chatRequest struct {
	Model              string              `json:"model"`
	Messages           []chatMessage       `json:"messages"`
	MaxTokens          int                 `json:"max_tokens"`
	Temperature        float64             `json:"temperature"`
	ChatTemplateKwargs *chatTemplateKwargs `json:"chat_template_kwargs,omitempty"`
}

type chatResponseMessage struct {
	Role             string `json:"role"`
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      chatResponseMessage `json:"message"`
		FinishReason string              `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

var retrySchedule = []time.Duration{2 * time.Second, 8 * time.Second, 30 * time.Second, 60 * time.Second}

func (t *HTTPTranslator) Translate(ctx context.Context, text string, src SourceLang, gloss Glossary) (string, string, error) {
	body := chatRequest{
		Model:       t.model,
		MaxTokens:   t.maxTokens,
		Temperature: 0,
		Messages: []chatMessage{
			{Role: "system", Content: withGlossary(systemPrompt(src), gloss)},
			{Role: "user", Content: text},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", "", err
	}
	zh, model, reasoning, finish, err := t.complete(ctx, raw)
	if err != nil {
		return "", "", err
	}
	// The gateway files an answer written without thinking under reasoning_content and leaves
	// content empty: seven characters came back empty every night from 2026-09-13 to 09-18.
	// The reasoning text cannot stand in for the answer, because when the model does think it is
	// the thinking; asked again with thinking off, all of them answered in content. A length stop
	// is a reasoning spiral, not a short ceiling (see backfill-char-zh-names), so it gets one
	// fresh roll.
	if finish == "length" {
		zh, model, reasoning, finish, err = t.complete(ctx, raw)
		if err != nil {
			return "", "", err
		}
	}
	if finish != "" && finish != "stop" {
		return "", "", fmt.Errorf("generation finished with finish_reason=%q — refusing partial output", finish)
	}
	if zh == "" && strings.TrimSpace(reasoning) != "" {
		body.ChatTemplateKwargs = &chatTemplateKwargs{EnableThinking: false}
		raw, err = json.Marshal(body)
		if err != nil {
			return "", "", err
		}
		zh, model, _, finish, err = t.complete(ctx, raw)
		if err != nil {
			return "", "", err
		}
		if finish != "" && finish != "stop" {
			return "", "", fmt.Errorf("generation finished with finish_reason=%q — refusing partial output", finish)
		}
	}
	return zh, model, nil
}

func (t *HTTPTranslator) complete(ctx context.Context, raw []byte) (content, model, reasoning, finish string, err error) {
	data, err := t.post(ctx, raw)
	if err != nil {
		return "", "", "", "", err
	}
	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil {
		return "", "", "", "", fmt.Errorf("decode chat response: %w (body: %s)", err, truncate(string(data), 300))
	}
	if cr.Error != nil {
		return "", "", "", "", fmt.Errorf("gateway error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", "", "", "", fmt.Errorf("gateway returned no choices")
	}
	model = cr.Model
	if model == "" {
		model = t.model
	}
	msg := cr.Choices[0].Message
	return strings.TrimSpace(msg.Content), model, msg.ReasoningContent, cr.Choices[0].FinishReason, nil
}

func systemPrompt(src SourceLang) string {
	if src == SourceEn {
		return TranslateSystemPromptEn
	}
	return TranslateSystemPrompt
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

func (m MockTranslator) Translate(_ context.Context, text string, _ SourceLang, gloss Glossary) (string, string, error) {
	model := m.Model
	if model == "" {
		model = "stub"
	}
	return "【MT・rehearsal mock】[gloss:" + strconv.Itoa(len(gloss)) + "] " + firstRunes(text, 60), "mock:" + model, nil
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
