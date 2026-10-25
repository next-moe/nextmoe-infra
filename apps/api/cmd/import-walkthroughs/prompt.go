package main

const extractPromptVersion = "extract-v2"

const extractSystem = `You convert a Japanese visual-novel walkthrough (攻略チャート) into a JSON graph that follows the given JSON schema.

The walkthrough lists choices to pick in order, as segments: each segment starts somewhere (the beginning, a save, or right after an ending) and runs until an ending, a collection stop, or the next resume line.

Markers (formats vary by site):
- Entry points: 【START】から開始, 【RESTART】から開始, 【はじめから】, "ルートN" headings that start from the beginning. An entry unlocked by finishing an ending (※タイトル画面に【RESTART】が追加) sets unlocked_by to the key of the segment that reached that ending.
- Creating a save: ◆セーブ01 / 【セーブ１】 / 【セーブ２】（７回使用） → a step {step_type:"save", save:"<save key>"} at that position, and the save listed once in saves.
- Resuming: ◆セーブ01から / セーブ２から → a new segment with from {node_type:"save", key:"<save key>"}.
- Endings: ○○END, ○○ ＥＮＤ, ＢＡＤ ＥＮＤ, ノーマルEND, TRUE END, and the END closing an AFTER block → the segment's to {node_type:"ending", ending:{name, ending_type, characters}}.
- Collection stops are NOT endings: 差分CG回収後終了, シーン回収, 回想回収, ○○回収後終了 → to {node_type:"collect", ending:null}.
- A segment that stops before the next resume line without an ending or collection stop → to {node_type:"open", ending:null}.
- After-stories: ■寧々AFTER━━ then choices then END → a segment with from {node_type:"ending", key:"<key of the segment that reached the heroine ending>"} whose ending has ending_type "after".
- "A(B)" means pick A; B is the alternative kept for CG-difference collection → {step_type:"choice", text:"A", alt:["B"]}. A choice prefixed ☆ or marked 実績 has achievement:true.
- Non-choice actions such as （クイズ） or ミニゲーム → {step_type:"action", text:"（クイズ）"}.
- Headings that are character names (e.g. "綾地 寧々") or route names (e.g. "ルート２") group the segments below them: sections[].title verbatim, sections[].characters = the characters of that heading.

Rules:
- Every step text, alt, ending name, save label, section title, entry label and condition MUST be copied character-for-character from the input. Never translate, normalize width, or invent.
- Cover the WHOLE chart in source order; do not skip any choice line. Skip site chrome: update dates, brand link lists, store/affiliate links, Tweet buttons, 共通説明, page shortcuts, 後記, promotion lines.
- condition: a verbatim unlock condition line for the segment (e.g. ※誰か1人クリア後に), else null.
- ending.characters: keys of the characters whose route the ending concludes (from the ending name and the section heading). Empty for bad/normal endings with no specific character.
- characters: each story character once; merge "因幡 めぐる" and "めぐる" into one with aliases. Only characters who have routes, endings or headings — not the protagonist unless he has his own ending.
- Keys: characters c1.., sections s1.., entries start/restart/..., saves sv01.., segments g1...
- If the input has no choice chart at all (a review, a save-data download post, notes only), set no_chart true and leave every array empty.`

const extractSchemaJSON = `{"type":"object","additionalProperties":false,
 "required":["no_chart","characters","sections","entries","saves","segments"],
 "properties":{
  "no_chart":{"type":"boolean"},
  "characters":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["key","name","aliases"],
    "properties":{"key":{"type":"string"},"name":{"type":"string"},"aliases":{"type":"array","items":{"type":"string"}}}}},
  "sections":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["key","title","characters"],
    "properties":{"key":{"type":"string"},"title":{"type":"string"},"characters":{"type":"array","items":{"type":"string"}}}}},
  "entries":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["key","label","unlocked_by"],
    "properties":{"key":{"type":"string"},"label":{"type":"string"},"unlocked_by":{"type":["string","null"]}}}},
  "saves":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["key","label"],
    "properties":{"key":{"type":"string"},"label":{"type":"string"}}}},
  "segments":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["key","section","from","condition","steps","to"],
    "properties":{"key":{"type":"string"},"section":{"type":["string","null"]},
      "from":{"type":"object","additionalProperties":false,"required":["node_type","key"],
        "properties":{"node_type":{"type":"string","enum":["entry","save","ending"]},"key":{"type":"string"}}},
      "condition":{"type":["string","null"]},
      "steps":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["step_type","text","alt","achievement","save"],
        "properties":{"step_type":{"type":"string","enum":["choice","action","save"]},"text":{"type":["string","null"]},
          "alt":{"type":"array","items":{"type":"string"}},"achievement":{"type":"boolean"},"save":{"type":["string","null"]}}}},
      "to":{"type":"object","additionalProperties":false,"required":["node_type","ending"],
        "properties":{"node_type":{"type":"string","enum":["ending","collect","open"]},
          "ending":{"anyOf":[{"type":"null"},{"type":"object","additionalProperties":false,"required":["name","ending_type","characters"],
            "properties":{"name":{"type":"string"},"ending_type":{"type":"string","enum":["heroine","harem","true","normal","bad","after","other"]},
              "characters":{"type":"array","items":{"type":"string"}}}}]}}}}}}
 }}`

const translatePromptVersion = "translate-v2"

const translateSystem = `You translate short Japanese strings from a visual-novel walkthrough (in-game choice texts, ending names, route headings, unlock conditions) into Simplified Chinese (zh-Hans) as used by Chinese galgame players.

Input JSON: {"glossary":[{"ja":"綾地寧々","zh":"绫地宁宁"}],"strings":["...", ...]}
Output JSON: {"t":["<zh for strings[0]>", ...]} with exactly the same length and order as "strings".

Rules:
- Character names: use the glossary. A short form of a glossary name (given name only, family name with さん/先輩/ちゃん) uses the matching part of the glossary translation (寧々 → 宁宁, 綾地さん → 绫地).
- A personal name NOT derivable from the glossary must be kept exactly as written in Japanese. Never transliterate or invent a Chinese name for it.
- {N1}, {N2} … stand for personal names. Copy each token exactly as written, once, where that name belongs in the Chinese sentence; never translate, drop or add one.
- Keep END / AFTER / START / RESTART / BAD / TRUE / HAPPY in Latin letters; ノーマルEND → 普通END.
- Keep an "A(B)" parenthesis structure if present. Keep numbers and symbols such as ※ ☆ ●.
- Translate faithfully and concisely, without explanations. Explicit content is translated plainly, not softened.`

const translateSchemaJSON = `{"type":"object","additionalProperties":false,"required":["t"],
 "properties":{"t":{"type":"array","items":{"type":"string"}}}}`
