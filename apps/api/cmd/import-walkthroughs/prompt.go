package main

const translatePromptVersion = "translate-v3"

const translateSystem = `You translate short Japanese strings from a visual-novel walkthrough (in-game choice texts, ending names, route headings, save point and entry labels, unlock conditions) into Simplified Chinese (zh-Hans) as used by Chinese galgame players.

Input JSON: {"glossary":[{"ja":"綾地寧々","zh":"绫地宁宁"}],"strings":["...", ...]}
Output JSON: {"t":["<zh for strings[0]>", ...]} with exactly the same length and order as "strings".

Rules:
- Character names: use the glossary. A short form of a glossary name (given name only, family name with さん/先輩/ちゃん) uses the matching part of the glossary translation (寧々 → 宁宁, 綾地さん → 绫地).
- A personal name NOT derivable from the glossary must be kept exactly as written in Japanese. Never transliterate or invent a Chinese name for it.
- {N1}, {N2} … stand for personal names. Copy each token exactly as written, once, where that name belongs in the Chinese sentence; never translate, drop or add one.
- Keep END / AFTER / START / RESTART / BAD / TRUE / HAPPY in Latin letters; ノーマルEND → 普通END.
- A save point label セーブN → 存档N, keeping the number as written (セーブ１２（２回使用） → 存档１２（使用２次）).
- Keep an "A(B)" parenthesis structure if present. Keep numbers and symbols such as ※ ☆ ●.
- Translate faithfully and concisely, without explanations. Explicit content is translated plainly, not softened.`

const translateSchemaJSON = `{"type":"object","additionalProperties":false,"required":["t"],
 "properties":{"t":{"type":"array","items":{"type":"string"}}}}`
