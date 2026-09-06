# sources · vocabulary

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

Open 词表，21 个成员。

| value | display_name | description |
| --- | --- | --- |
| `user` | User | Manual curation, not an import source. |
| `vndb` | VNDB | The VNDB catalog. |
| `bangumi` | Bangumi | The Bangumi catalog. |
| `dlsite` | DLsite | DLsite storefront identities. |
| `erogamescape` | ErogameScape | ErogameScape. |
| `anilist` | AniList | AniList. |
| `mal` | MyAnimeList | MyAnimeList. |
| `steam` | Steam | Steam storefront. |
| `official_site` | Official site | A publisher or brand official site. |
| `twitter` | Twitter / X | A Twitter / X identity. |
| `pixiv` | Pixiv | Pixiv. |
| `curated` | Curated | First-party curated / human lane. |
| `upscale` | Upscale | First-party AI-upscaled cover derivation. |
| `cien` | Ci-en | Ci-en creator-support platform. |
| `dmm` | DMM | DMM storefront. |
| `web` | Web | A generic web page. external_id is the full URL. |
| `getchu` | Getchu | Getchu.com retailer pages. |
| `derived` | Derived | First-party machine inference over catalog facts. |
| `nextmoe` | NextMoe | First-party measurements aggregated from our users. |
| `howlongtobeat` | HowLongToBeat | HowLongToBeat playtime and rating aggregates. |
| `censored` | Censored | First-party blurred stand-in derived from explicit official art. |

全部词表见 https://developer.nextmoe.dev/docs/vocabularies.md，运行时以 https://api.nextmoe.dev/v2/vocabularies 为准。

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/vocabularies/sources
