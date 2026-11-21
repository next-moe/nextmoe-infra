package introguard

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUntranslatableSourcesFromProd(t *testing.T) {
	for _, src := range []string{
		"Nobody has voted on this visual novel yet...",
		"No description.",
		"Twitter | YouTube | Tumblr",
		"Staff page: https://vndb.org/s36094.1",
		"DLsite: https://www.dlsite.com/pro/circle/profile/=/maker_id/VG02167.html",
		"https://web.archive.org/web/20030804114950fw_/http://stjp.com/tako/sunburnjyouhou1.htm",
		"Twitter: https://twitter.com/x\nFreem: https://www.freem.ne.jp/brand/10980",
		"Discord - Link",
		"Official English website",
		"公式サイト：https://example.jp/",
		"-",
		">\n\n",
		"<br />",
		"```\n```\n\n",
		"？？？",
		"……？",
	} {
		assert.True(t, Untranslatable(src), "%q", src)
	}
}

func TestTranslatableSourcesStayTranslatable(t *testing.T) {
	for _, src := range []string{
		"2nd year high school student.",
		"A porn actor from Miami.",
		"Height: 158cm\nBirthday: May 5",
		"Mod maker for Beskonechnoe leto.",
		"Created for [MicroReno 2014](http://vk.com/microreno2).",
		"主人公の妹。",
		"Brand closed down in May 2012.",
		"Defunct.",
		"Old site of the brand, where the first three games were sold.",
	} {
		assert.False(t, Untranslatable(src), "%q", src)
	}
}

func TestRejectsTheAnswersFoundOnProd(t *testing.T) {
	cases := []struct{ src, out, reason string }{
		{"Nobody has voted on this visual novel yet...",
			"您好，我已理解您的翻译要求。但是您尚未提供需要翻译的英文作品简介正文——目前消息中只包含翻译说明。\n\n请您把需要翻译的英文简介原文贴出来，我会按照您列出的全部规则进行翻译。",
			ReasonMeta},
		{"Twitter | YouTube | Tumblr",
			"你好,kaguya。我已理解本次翻译任务的要求与术语对照表。\n\n不过,你似乎还没有提供需要翻译的英文条目简介正文。",
			ReasonMeta},
		{"https://www.dlsite.com/girls/circle/profile/=/maker_id/RG01.html",
			"未提供需要翻译的英文原文，请提供具体的英文文本以便进行翻译。", ReasonMeta},
		{"https://vndb.org/s29035", "我无法直接访问外部链接。请将需要翻译的英文原文粘贴给我。", ReasonMeta},
		{"John's oldest child.",
			"You are an expert translator specializing in game localization, specifically for visual novels (galgame).",
			ReasonPrompt},
		{"A team consisting of Djonsaim and Navi.",
			"1. **分析原文**：“A team consisting of Djonsaim and Navi.”\n2. **理解要求**：将视觉小说相关的英文条目简介翻译成简体中文。",
			ReasonPrompt},
		{"Sarah's mother",
			"Sarah's mother is a kind and gentle woman who runs a small bakery in town. She has a warm smile.",
			ReasonNotZh},
		{"Twitter | Fanbox | Patreon | Afdian | Itch.io", "Twitter | Fanbox | Patreon | Afdian | Itch.io", ReasonNotZh},
		{"Sarah's mother",
			"莎拉的母亲是一位善良温柔的女性，在镇上经营着一家小面包店。她总是带着温暖的笑容，能让任何人的一天都变得明亮起来。镇上的孩子们都喜欢她烤的面包，每天放学后都会去店里买一个。",
			ReasonInflated},
	}
	for _, c := range cases {
		assert.Equal(t, c.reason, Reject(c.src, c.out), "%q", c.out)
	}
}

func TestKeepsRealTranslationsThatLookLikeReplies(t *testing.T) {
	cases := []struct{ src, out string }{
		{"国は謝罪した。しかし任務の内容や死因については、一切情報を提供しなかった。",
			"国家道了歉。然而，关于任务内容以及死因的相关信息，他们一概没有提供。"},
		{"通訳のいらないほどに日本語をマスターしているが、やや古めかしい言い回しを用いる。",
			"精通日语到不需要翻译的程度，但会使用略显古旧的表达方式。"},
		{"Translation notes:\nIn-game subtitles are translated, but most of the UI is still English.",
			"翻译说明：\n游戏内字幕已翻译，但大部分界面仍保留英文。"},
		{"こんにちは、ミアです。目が覚めたら、知らない場所に閉じ込められていました。",
			"你好，我是米娅(ミア)。醒来的时候，我被关在一个陌生的地方。"},
		{"「缶詰」と呼んでもOK。翻訳をしています、主に日本語→簡体字中国語",
			"叫我「缶詰」也可以。我在做翻译，主要是日语→简体中文。"},
		{"A team consisting of Djonsaim and Navi.", "由Djonsaim和Navi组成的团队。"},
	}
	for _, c := range cases {
		assert.Empty(t, Reject(c.src, c.out), "%q", c.out)
	}
}
