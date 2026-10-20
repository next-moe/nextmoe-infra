# work_relation · vocabulary

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

Closed 词表，12 个成员。

| value | display_name | description |
| --- | --- | --- |
| `adaptation_of` | Adaptation of | This work is an adaptation of the other. |
| `sequel_of` | Sequel of | This work is a sequel of the other. |
| `side_story_of` | Side story of | This work is a side story of the other. |
| `fandisc_of` | Fandisc of | This work is a fandisc of the other. |
| `collects` | Collects | This work collects the other. |
| `remake_of` | Remake of | This work is a remake of the other. |
| `same_series` | Same series | The two works belong to the same series. Symmetric. |
| `same_setting` | Same setting | The two works share a setting. Symmetric. |
| `crossover_with` | Crossover with | The two works crossover. Symmetric. |
| `shares_character` | Shares character | The two works share a character. Symmetric. |
| `alternative_setting` | Alternative setting | The two works share characters in a different setting. Symmetric. |
| `alternative_version` | Alternative version | The two works are alternative versions. Symmetric. |

全部词表见 https://developer.nextmoe.dev/docs/vocabularies.md，运行时以 https://api.nextmoe.dev/v2/vocabularies 为准。

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/vocabularies/work_relation
