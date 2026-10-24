package textsafety

import (
	"encoding/json"
	"strings"
)

const (
	TitlePromptVersion = "title-v1"
	IntroPromptVersion = "intro-v1"
)

const titlePrompt = `You review the names of visual novels before they appear on a public, search-indexed catalog page that must be safe for work. Every name listed for a work is shown on its page, so judge the WORST name of each work.

Classes:
- "safe": no sexual meaning. Romance, love, dating, kissing, girlfriends, school life, violence, horror, gore and dark themes are safe.
- "suggestive": sexual innuendo, fetish or adult-genre wording, or sexualised framing without naming explicit acts. Examples: 人妻, 痴漢, 寝取られ/NTR, 調教, 催眠 used sexually, 淫, 発情, 孕ませ, エロ/ero, 肉便器, lewd puns, "hentai", "sex" in a sexual sense.
- "explicit": names sexual acts, genitals, or pornographic terms outright.

Names are in Japanese, Chinese, English and other languages; judge the meaning. A brand-new coined word with no sexual meaning is safe. confidence is your certainty from 0 to 1.

Reply with ONLY one JSON object, no prose and no code fences, with exactly one entry per input id:
{"items":[{"id":1,"class":"safe","confidence":0.97}]}

Works:
`

const introPrompt = `You review game descriptions before they appear on a public, search-indexed web page that must be safe for work. Each description is split into numbered sentences "[n] ...".

For each description return:
- "class": "safe" (no sexual content at all), "mild" (mostly fine, but some sentences are sexual), or "explicit" (sexual content is the core of the text).
- "confidence": your certainty from 0 to 1.
- "drop": the numbers of every sentence that must be removed to make the description safe for work. Remove a sentence that mentions sexual acts, genitals, nudity, sexual violence, fetishes, sexualised descriptions of bodies, H-scenes / エッチシーン / Hシーン / CG or scene counts of sex scenes, or adult-only marketing such as 18禁, 抜きゲー, 濃厚, 凌辱. Keep plot, setting, characters, gameplay, staff, voice and release facts. Romance, kissing and love confessions are fine. A safe description has "drop": [].

Reply with ONLY one JSON object, no prose and no code fences, with exactly one entry per input id:
{"items":[{"id":1,"class":"mild","confidence":0.9,"drop":[4,5]}]}

Descriptions:
`

const rewritePrompt = `Write a short safe-for-work description for each game below, for a public, search-indexed catalog page. You get the game's title, the language to write in ("lang"), and its original description, which contains adult content.

Rules:
- 1 to 3 sentences in the requested language, using only facts from the original that are not sexual: setting, premise, the characters' roles, genre, gameplay.
- No sexual content, no innuendo, no fetish words, and do not mention that the game is for adults.
- Do not invent anything that the original does not say.
- If nothing non-sexual is left to say, return "".

Reply with ONLY one JSON object, no prose and no code fences, with exactly one entry per input id:
{"items":[{"id":1,"intro":"..."}]}

Games:
`

func jsonLines(items []any) string {
	var b strings.Builder
	for _, it := range items {
		raw, _ := json.Marshal(it)
		b.Write(raw)
		b.WriteByte('\n')
	}
	return b.String()
}
