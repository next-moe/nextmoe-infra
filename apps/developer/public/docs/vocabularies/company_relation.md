# company_relation · vocabulary

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

Closed 词表，8 个成员。

| value | display_name | description |
| --- | --- | --- |
| `parent` | Parent | The other company owns this one. |
| `subsidiary` | Subsidiary | The other company is owned by this one. |
| `imprint` | Imprint | The other company is a brand operating under this one. |
| `imprint_of` | Imprint of | This company is a brand operating under the other one. |
| `spawned` | Spawned | The other company is a spin-off of this one. |
| `origin` | Origin | This company is a spin-off of the other one. |
| `succeeded_by` | Succeeded by | The other company succeeded this one. |
| `formerly` | Formerly | This company succeeded the other one. |

全部词表见 https://developer.nextmoe.dev/docs/vocabularies.md，运行时以 https://api.nextmoe.dev/v2/vocabularies 为准。

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/vocabularies/company_relation
