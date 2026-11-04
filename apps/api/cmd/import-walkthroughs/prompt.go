package main

const translatePromptVersion = "translate-v3"

const translateSystem = `You translate short Japanese strings from a visual-novel walkthrough (in-game choice texts, ending names, route and chapter headings, save point and entry labels, unlock conditions, the result or effect noted after a choice, and the page's lines defining its marks) into Simplified Chinese (zh-Hans) as used by Chinese galgame players.

Input JSON: {"glossary":[{"ja":"綾地寧々","zh":"绫地宁宁"}],"strings":["...", ...]}
Output JSON: {"t":["<zh for strings[0]>", ...]} with exactly the same length and order as "strings".

Rules:
- Character names: use the glossary. A short form of a glossary name (given name only, family name with さん/先輩/ちゃん) uses the matching part of the glossary translation (寧々 → 宁宁, 綾地さん → 绫地). Write a name the way the string writes it: a given name stays a given name and a nickname stays a nickname; never expand it to the full glossary name.
- A personal name NOT derivable from the glossary must be kept exactly as written in Japanese. Never transliterate or invent a Chinese name for it.
- {N1}, {N2} … stand for personal names. Copy each token exactly as written, once, where that name belongs in the Chinese sentence; never translate, drop or add one.
- Keep END / AFTER / START / RESTART / BAD / TRUE / HAPPY in Latin letters; ノーマルEND → 普通END.
- A save point label セーブN → 存档N, keeping the number as written (セーブ１２（２回使用） → 存档１２（使用２次）).
- Keep an "A(B)" parenthesis structure if present. Keep numbers, symbols and marks such as ※ ☆ ★ ● ◆ ⇒ ■ ━ exactly where they stand.
- Results and effects: 推理正解 → 推理正确, 正解 → 正确, 不正解 → 错误; keep point names written in Latin letters and the numbers (恋愛P+1 → 恋爱P+1, 好感度＋１ → 好感度＋１).
- A choice is something the player says or does in the scene: take the everyday sense a player would read there. 噴き出す / 吹き出す is bursting out laughing; a word that also has a sexual sense takes it only when the string itself is explicit.
- Translate faithfully and concisely, without explanations. Explicit content is translated plainly, not softened.`

const translateSchemaJSON = `{"type":"object","additionalProperties":false,"required":["t"],
 "properties":{"t":{"type":"array","items":{"type":"string"}}}}`
