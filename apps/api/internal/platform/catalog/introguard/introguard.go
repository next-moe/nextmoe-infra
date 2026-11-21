// Package introguard tells a translatable intro source from one with nothing
// to translate, and a model's translation from a reply that is not one.
//
// Both machine-translation lanes wrote whatever the model returned. On
// 2026-11-21 the forum showed work 279247's intro as "您好，我已理解您的翻译要求。
// 但是您尚未提供需要翻译的英文作品简介正文……": its only source was VNDB's
// "Nobody has voted on this visual novel yet...", pasted into the editor as an
// intro. The same audit found label intros of bare links answered with
// "请提供需要翻译的英文原文", a character's "John's oldest child." answered with
// the system prompt in English, and "Sarah's mother" expanded into an invented
// paragraph about a bakery.
package introguard

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	reURL      = regexp.MustCompile(`(?i)(?:https?://|www\.)\S+`)
	reHTMLTag  = regexp.MustCompile(`</?[a-zA-Z][^>]*>`)
	reSegments = regexp.MustCompile(`\s+[-–—]\s+|[\n|/,、・·•]+`)
	reLinkWord = regexp.MustCompile(`(?i)^(?:(?:old|older|original|official|new|main|personal|private|staff|english|japanese|chinese|archived|current|former)\s+)*` +
		`(?:twitter|itch\.?io|rednote|xiaohongshu|blog|blogger|patreon|github|discord|instagram|ko-?fi|subscribestar|xiaoheihe|youtube|furaffinity|fur affinity|` +
		`pixiv(?:\s*fanbox)?|fanbox|telegram|deviantart|tumblr|newgrounds|weibo|reddit|steam|booth|mastodon|bluesky|game\s*jolt|gamejolt|linkedin|facebook|twitch|tiktok|` +
		`vgen|freem|novelgame|wiki|renderosity|pornhub|hentai-foundry|lava\s*top|dzen|pinterest|pintrest|bilibili|niconico|nicovideo|kickstarter|indiegogo|gumroad|` +
		`linktree|carrd|tapas|webtoon|fantia|ci-en|skeb|dlsite|fanza|dmm|getchu|vndb|lemma\s*soft(?:\s*forums?)?|website|web\s*site|site|homepage|home\s*page|hp|` +
		`page|profile|circle|link|links|archive|mail|e-?mail|contact|shop|store|online\s+store|` +
		`公式サイト|公式HP|公式ホームページ|公式ブログ|公式ツイッター|ホームページ|ブログ|サイト|旧サイト|ツイッター|サークル|官网|官方网站|微博|主页)` +
		`(?:\s+(?:page|profile|account|link|archive|site|channel|group|server|blog))*$`)
)

// placeholders are site interface strings that reach an intro field by copy and
// paste. They are matched after lowercasing and trimming trailing dots.
var placeholders = []string{
	"nobody has voted on this visual novel yet",
	"no description",
	"no description available",
	"no description yet",
}

// IsPlaceholder reports text that is a copied site interface string, not an
// intro.
func IsPlaceholder(text string) bool {
	s := strings.ToLower(strings.TrimSpace(text))
	s = strings.TrimRight(s, ".…。 ")
	for _, p := range placeholders {
		if s == p {
			return true
		}
	}
	return false
}

// LinksOnly reports text made of nothing but URLs and the names of the sites
// they point to ("Twitter | Itch.io", "Staff page: https://vndb.org/s36094").
func LinksOnly(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	rest := reURL.ReplaceAllString(text, " ")
	for _, seg := range reSegments.Split(rest, -1) {
		seg = strings.TrimFunc(seg, func(r rune) bool {
			return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
		})
		if seg == "" {
			continue
		}
		if !reLinkWord.MatchString(seg) {
			return false
		}
	}
	return true
}

// Untranslatable reports a source with nothing a translation could carry: a
// copied interface string, links and link names, or markup and punctuation.
func Untranslatable(src string) bool {
	if IsPlaceholder(src) || LinksOnly(src) {
		return true
	}
	return substance(reHTMLTag.ReplaceAllString(reURL.ReplaceAllString(src, ""), "")) == 0
}

func substance(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			n++
		}
	}
	return n
}

const (
	ReasonMeta     = "meta-reply"
	ReasonPrompt   = "prompt-echo"
	ReasonNotZh    = "not-chinese"
	ReasonInflated = "inflated"
)

var (
	// A reply addressed to the requester. Each pattern needs the requester or
	// the request itself in it: "他们一概没有提供" and "精通日语到不需要翻译的程度"
	// are real translations on prod.
	reMeta = regexp.MustCompile(`(?:提供|贴出|粘贴|发送|发给我|给出|附上)[^。\n]{0,12}(?:需要|要|待)翻译的|` +
		`(?:需要|要|待)翻译的[^。\n]{0,8}(?:原文|文本|正文|简介|内容|条目)|` +
		`(?:理解|收到|明白)了?(?:您|你)?的?翻译(?:要求|任务|需求)|` +
		`(?:您|你)(?:似乎|好像|可能)?(?:尚未|还没有?|没有|未)(?:提供|给出|附上|贴出|发送)|` +
		`按照(?:您|你)?(?:列出|给出|提供|上述)?的?(?:全部|所有)?(?:规则|要求)(?:进行)?翻译|` +
		`无法(?:直接)?(?:访问|打开|查看)(?:外部)?(?:链接|网址|网页|URL)|` +
		`(?i:please provide|you (?:haven't|have not|did not|didn't) provide|no (?:source )?text (?:was )?provided|as an ai)`)
	// Strings only the system prompts contain, or the scaffolding a reasoning
	// model prints when it answers with its analysis.
	rePrompt = regexp.MustCompile(`(?i)you are an? (?:expert |professional |senior )?(?:game )?translator|` +
		`资深的?游戏本地化译者|术语对照表|\*\*(?:分析原文|理解要求)\*\*|简介正文`)
	reHan = regexp.MustCompile(`\p{Han}`)
)

// Reject names why out is not a translation of src, or returns "" when it can
// stand as one. The meta and prompt checks are skipped when src itself trips
// them, so a source that talks about translation is not held against its own
// translation. An output with no Han character is never a zh-Hans translation:
// on prod those were link lists and names copied through verbatim.
func Reject(src, out string) string {
	switch {
	case reMeta.MatchString(out) && !reMeta.MatchString(src):
		return ReasonMeta
	case rePrompt.MatchString(out) && !rePrompt.MatchString(src):
		return ReasonPrompt
	case !reHan.MatchString(out):
		return ReasonNotZh
	case inflated(src, out):
		return ReasonInflated
	}
	return ""
}

func inflated(src, out string) bool {
	s := utf8.RuneCountInString(strings.TrimSpace(src))
	return utf8.RuneCountInString(strings.TrimSpace(out)) > 2*s+40
}
