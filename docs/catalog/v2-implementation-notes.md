# v2 implementation notes

This page records **binds that differ from `refs/api-v2`**. It is not a substitute for the design spec. Code + `docs/catalog/v2-openapi.yaml` are the machine-readable contract.

Date: 2026-08-24, GA declared 2026-08-25 (stage 9); news write face added 2026-08-25. v1 is unchanged; the v1 sunset is still not scheduled here.

## Done on this branch

- Protocol, problems, vocabularies, collection contract, repr types, CI gates G1–G16.
- Catalog read: works (list/detail/12 subs), companies+graph, tags, series, engines, releases, characters, credit-names, persons, traits, search, calendar, changes, redirects, stats, schemas/{object}, news.
- `GET /v2/catalog/works` binds 05 §6.1 filters (`q=`, `company_id=`, `tag_id=`, `series_id=`, `engine_id=`, `olang=`, dates, claim/content axes). `q=` / search sorts / `facets=` use the search engine (`WorksSearch`; OpenSearch is the search engine since 2026-09-01, Meilisearch retired 2026-09-02); other filter combinations use the live registry (`WorksList`). `sort=relevance` requires `q=`.
- `GET /v2/catalog/characters/{id}/appearances` is the character reverse-lookup collection (roster_role, spoiler, voices). Company reverse lookup is `works?company_id=`. Staff reverse lookup stays `credit-names/{id}/credits`.
- Character `view=full` carries D30 attributes (`gender`, `birthday` as `MM-DD`, measurements, `blood_type` as `a|b|ab|o`, `instance_of_id`). `description` / `extra` / `field_provenance` stay out, as D30.
- `/v2/me/playtimes` GET/PUT/DELETE and POST 207 batch; `/v2/me/cover-votes` GET/PUT/DELETE.
- `/v2/me/claims` list (keyset on last claim-event id)/create/get/withdraw; `/v2/me/proposals` list/create/get/patch/amend.
- `/v2/moderation/claims` queue + GET by id + decisions; `/v2/moderation/proposals` queue + decisions; reverts; snapshots. Queue and GET are site-fenced (`SITE_NOT_BOUND` when the token client has no catalog site).
- User token on `/v2/me` and `/v2/moderation`, not an application key; `private, no-store`. JWT `roles` are copied into the handler context so `HasPerm` matches v1.
- **D35** (2026-08-25): claim state machine completed (`PATCH {state: live|pending}`, decisions `ban`/`unban`, `catalog.claim.review` enforced); public edit history at `/v2/catalog/revisions` + `/v2/catalog/proposals`; `POST /v2/me/edit-images`; `GET /v2/moderation/proposals/{id}`. The v1 `claim-events/feed` and S2S `works/claim` stay on v1 by decision (D35d) and must be named individually in any `/api/v1/catalog/**` retirement.
- `/v2/me/news` list/create/get/patch (D34, 2026-08-25). Source-row-as-grant: `news_source.publisher_uid` is the authorisation. No schema change — `news/service/submission.go` writes the existing `kun_news` tables. Public read DTO (`repr.NewsItem`) is untouched.
- **Read-parity wave (2026-08-26, from the forum's v1 fallback report)**: rate limiting is now keyed to the application key's tier (deviation 29); the entity faces grow the data the v1 faces already carried (deviation 30); `works?refs=` that resolves nothing now answers an empty batch page + `missing[]` instead of silently dropping the IDs filter and returning page 1 of the whole catalogue (the same guard every other entity list already had).
- **Read-parity wave 3 (2026-08-27, four verified gaps from the forum)**: the spoiler ceiling is reachable at last — `spoiler=none|minor|major` on `works/{id}`, `works/{id}/tags` and `characters/{id}`, where v2 had hardcoded 0 (deviation 34); the company graph's `include=logo` stops being validated-then-discarded and puts the brand mark on the nodes that have one (deviation 35); `characters/{id}` stops throwing away the aliases, intros and refs `GetCharacter` already fetched, via `include=aliases,intros,refs`; and `lang` — the language of `display_name`, unreconstructable from `localized{}` — joins the company, credit-name and character shapes on every lane (deviation 36). Additive on the v2 wire; the only v1 change is one new optional key on the labels list projection.
- **Read-parity wave 2 (2026-08-26, the four gaps the forum still had after the first wave)**: the ratings block carries its vote histogram and spread on the detail faces (deviation 31); the work `companies` block carries the brand logo, tag detail takes `include=intros`, series grows `has_nsfw` plus `include=intros,refs` (deviation 32); `credit-names/{id}` becomes the staff read surface with person-level scalars and six include blocks, and its credits sub-face names the voiced character (deviation 33). Additive only — no v1 DTO, service or schema change.

## Spec deviations (with reason)

1. **`series` and `traits` `refs=`** — `catalog_external_ref` has no entity_type for series or trait (`constants.go` entity types stop at engine=8). Unknown refs go to `missing[]`. Not a new table.

2. **Character full fields use `omitempty`** — D30 says unrecorded is JSON `null`. `view=basic` must not emit those keys (default-thin). Unrecorded values on `view=full` are therefore omitted rather than `null`. Same projection trick as work `include=` blocks.

3. **Trait JSON uses `is_sexual`, not `sexual`** — G8: `Screenshot.sexual` is `*string` (`safe|suggestive|explicit`). A bool cannot share the property name. Work tags already use `is_sexual`.

4. **Cover vote is `up` only** — v1 stores one ballot per (user, work). There is no down column. `vote=down` is 422.

5. **Playtime GET empty is 404** — v1 returned 200 + null. v2 has no envelope; a missing row is `NOT_FOUND`.

6. **Playtime delete** — v1 had no DELETE. v2 DELETE removes `catalog_user_playtime` rows for that (user, work). Additive service method, existing table.

7. **`blood_type` public tokens are lowercase** (`a|b|ab|o`) per 04-representation. D30's table wrote `A|B|AB|O` as domain letters; the editing engine stores int16. Public JSON follows 04.

8. **Claim POST mint needs `display_name`** — D33. `SubmitWork` cannot mint without `catalog.work.display_name`. `refs` that miss `catalog_external_ref` become `catalog.work.links` URLs via the existing source URL templates. `work_id` and `refs` both absent is 422 even if `display_name` is present.

9. **Moderation queues and `/v2/me/proposals` are keyset-paginated** on event/proposal id (`cur_` + over-fetch).

10. **Works list `include=` hydrates the list-capable subset** (`titles`, `intros`, `companies`, `ratings`, `covers` slots, `refs`). Other FULL_SET tokens stay on detail / sub-resources. Facets on the SQL browse path return empty buckets; named facet counts come from the search engine. **Amended 2026-08-27:** this entry used to read "SQL `include_total` is omitted (search path returns Meili `total`)". The SQL lane now honors `include_total` with an exact `count(*)` over the same predicate set the page query uses, so `/v2/catalog/works` publishes `total` on both lanes. The old behavior was never defensible as a deviation: `include_total` was parsed and validated on the SQL lane (a bogus value 400s) and the published envelope doc on `List.total` says "Present only when `include_total=true`. Same visibility gate as items." — which contradicted a lane that silently never published it, while every other registry list did. It also hard-blocked the forum's flip, whose pagination is `Math.ceil(total/limit)` and read `null` as zero pages. The COUNT is **flag-gated**, unlike the taxonomy lanes' unconditional count, because the works predicate set is `EXISTS`-subquery heavy (label rollup, tag maps, series, engine, platform, release windows); it follows the `ReleaseFeedMeta` / `CalendarMeta` precedent instead. It is computed from the filter predicates **before** the cursor predicate is appended, so paging does not shrink it. The all-miss `ids=`/`refs=` batch now publishes `total: 0` like every other entity lane rather than dropping the key. **Amended again 2026-08-28 (deviation 100):** the parenthesised subset in this entry's first sentence was never what the code did — `workFromListItem` emitted four blocks, not six, and the list declared all eighteen detail tokens rather than the six named here. The list vocabulary is now `collect.WorkListInclude` and is eight tokens; what `titles` and `covers` mean on a collection lane is stated in 100 rather than implied here.

11. **`release_status=` is not bound** on `GET /v2/catalog/works`. It is in 05 §6.1; v1 list did not have it either. Calendar remains the status filter.

12. **Character reverse lookup is `/characters/{id}/appearances`**, not `works?character_id=`. Appearance rows carry edge metadata the works collection cannot. This fills the 03 hole left when S2S `characters/{id}/works` was 410'd.

13. **`PATCH /v2/me/claims/{id}` pre-reads the claim site-fenced, not owner-fenced** (D35a). The owner check belongs to `ClaimLifecycleService.Act` via `RequireOwner`, which adopts an unowned row for its first claimant. Reading the current state through the owner-scoped `ClaimsByActor` query instead made adopt-and-publish — the most frequent claim write in production — answer 404 for every machine-imported draft.

14. **`status` is added to the G8 named exception list** (`gates_repr.go`, D34a). 06 §7.2 / 03 §2.4 spell the news lifecycle member `status`, while `Problem.status` and `playtime_batch_item.status` are HTTP status codes and stay integers. This is the same concept 07 §2.1 already admits for `state`; the list grows by one, with the reason recorded beside it.

15. **Claim and proposal ETags are domain validators emitted as real `ETag` headers** (D35a). `"c<work_id>.<state>"` and `"p<id>.<updated_unix>"`. Before this, claim decisions validated `If-Match` against `"c<work_id>"` while the GET returned a body digest, so no client could obtain a value that passed; the test hardcoded the unobtainable string, which is why it stayed green. **Amended 2026-08-28 (deviation 57):** both shapes moved — neither advanced with the record they validate.

16. **The public edit-history collections carry no `nsfw=`, `facets=` or `refs=`** (D35b), and `entity_id=` without `object=` is 422 `INCONSISTENT_WITH` — entity ids are only unique within a family. The nsfw omission is deliberate: the two mirror crons read the collection ascending by id, and a visibility filter would silently skip rows the watermark has already passed. **Amended 2026-08-28 (deviation 56):** still true of the rows, and `include=diff` prints values with no axis at all — a measured, recorded leak, not an oversight.

17. **`edit_image` is not the `Image` type** (D35c) — it omits `source`, because freshly uploaded bytes have no upstream provenance. `sexual` / `violence` are always `null` on the receipt. The multipart file part declares `contentType: application/octet-stream` rather than an `image/*` list: `mime/multipart.CreateFormFile` stamps every part with octet-stream, so a narrower list rejects the only client this face exists for.

18. **The revision list query does not select the `snapshot` column**, and `field_diff` does not repeat `field_type` / `diff_hint`. Both are size decisions with a contract consequence: rendering metadata for a key lives in `GET /v2/catalog/schemas/{object}`, one definition rather than one per diff entry.

19. **Collection query parameters are repeated per input struct, never embedded.** huma v2.38 does not walk anonymous embedded structs when collecting query parameters, so an embedded `collectionInput` is dropped from the spec *and* never bound — the endpoint still answers 200 with `cursor` / `limit` / `sort` silently ignored. Four pre-existing routes are affected and are **not** fixed here (D35e): `GET /v2/catalog/redirects`, `GET /v2/me/playtimes`, `GET /v2/me/proposals`, `GET /v2/catalog/calendar`.

20. **`summary` on the news write bodies declares `maxLength: 2000`, not 200.** huma validates the generated schema before the handler runs and `problem.fieldFromHuma` maps *every* schema failure to reason `INVALID_FORMAT`; 06 §7.1 requires an over-long summary to come back as `TOO_LONG`. The 200-rune ceiling is therefore enforced in the handler (and again by the `news_item_preview_len` CHECK), and the schema bound is only a guard against an unbounded body. The **response** `summary` keeps `maxLength: 200`, which is the real contract.

21. **`{"status":"published"}` is `422`, not `409`.** The PATCH body declares `enum: [withdrawn]`, so the schema refuses it before the handler. 06 §7.2 says publishing "does not exist as a transition on the write face" — a value outside the field's vocabulary is the more accurate refusal than a state-machine 409. Every transition that *is* expressible but illegal (edit after publish, withdraw while pending, anything on a rejected item) is `409 me/INVALID_STATE_TRANSITION`.

22. **`GET/PATCH /v2/me/news/{id}` on another publisher's item is `404 NOT_FOUND`, not `403 news/SOURCE_NOT_YOURS`.** `SOURCE_NOT_YOURS` names the `source` member of a create body, where the caller chose the name. Returning it for an id lookup would make `/v2/me/news/{id}` an existence oracle over every other publisher's item ids — the same reason 10 §3.5 refuses to distinguish "not yours" from "does not exist".

23. **`SOURCE_INACTIVE` gates create and pending text edits, not withdrawal.** Obligation 3 (03 §2.4) requires a published item to stay withdrawable; deactivating a source must not strand its live items on the public face.

24. **Native `external_id` is `api_` + 32 hex characters.** `(source_key, external_id)` is UNIQUE and `lane` is not part of it. 月幕 writes bare snowflake digits and galgame 批评 writes `cv<id>#<ordinal>`, so the prefix is disjoint from both; a publisher who is also the `publisher_uid` on a partner source therefore cannot mint a row that the next importer pass adopts and overwrites.

25. **An application key on `/v2/me/news` is `401 INVALID_CREDENTIAL`,** not `403 me/USER_IDENTITY_REQUIRED`. This is the pre-existing `userAuth` bind shared by every `/v2/me` route: a `nmk_`/`nm_` key is rejected at the middleware. `USER_IDENTITY_REQUIRED` is what a token that resolves to no uid gets.

26. **`lane` defaults to `news`** when the create body omits it. The column is NOT NULL with no default and the public read type does not expose lane at all, so a default is invisible downstream; an unknown value is `422` with reason `UNKNOWN_VALUE`.

27. **The news item ETag is `"n<id>.<updated_at in microseconds>"`,** not a body digest. The value must survive a round trip through `If-Match`, and every write path bumps `updated_at`. The record is always re-read from Postgres after a write so the token comes from stored microseconds, never from a Go-side timestamp.

28. **Withdrawn items are `404` on `GET /v2/news/{id}`, not `410 GONE`.** Obligation 3 (03 §2.4) asks for a tombstone on the detail face. `news/service/public.go` filters `status = published AND dead_at IS NULL` for both list and detail, so list disappearance is correct and pending items never leak — only the detail status code is wrong. Pre-existing on the read face; not touched by the write wave.

29. **Rate limits follow the key tier, not the client IP** (2026-08-26). GA shipped a per-IP `100/min + 10,000/day` limiter that ignored the credential entirely, so a server-side integrator forwarding its whole site's reads from one egress IP burned the daily quota in minutes — the forum measured one page view of its companies index spending the backend's whole minute. Now: a request carrying a resolved `nmk_` key is limited per key at the devapi tier policy (`free` 60/min + 50k/day, `trusted` 600/min + 1M/day, `internal` unlimited; per-key overrides win), identical to the v1 face's `mw.RateLimit`/`mw.Quota`. Keyless `/v2` paths (problems, vocabularies, stats, schemas, openapi.json, and any request that never authenticates) keep the old per-IP defaults. `RateLimit-Policy` and the `X-RateLimit-*` headers now report the effective per-key numbers; unlimited keys get no rate headers. Mechanically the limiter moved from `protocol.Middleware` (pre-auth) to a `protocol.RateLimit` middleware behind the auth stack — so unauthenticated 401s are no longer counted, and a 429 is never remembered by the `Idempotency-Key` replay store (it would replay the refusal for 24h past the window reset).

30. **Entity faces carry the v1 read data, include-gated** (2026-08-26). The GA entity repr was id + names only, which is why the forum's entity pages fell back to v1. Additions — `companies`: `include=aliases,logo,intros,links` (aliases/logo fill on every lane from the list row; intros/links need the label detail read and fill on the detail face and the ids=/refs= batch lane, which upgrades to per-id detail reads when those tokens are present — the cursor list lane accepts but does not carry them), plus `has_works=` restored from v1 to skip the ~35k zero-work registry rows; `characters`: `include=image,figure,traits` on the detail face (`character_trait` rows with per-character spoiler/lie); `tags`: `is_sexual` joins the basic shape (the SFW gate the forum lost); `series`: `work_count` joins the basic shape (detail computes it from the same nsfw-gated count the list already had — this also adds `work_count` to the **v1** series detail projection); `engines`: `description` + `aliases` join the basic shape. The works face's engines block now refs a `WorkEngineRef` schema (same four properties as before) so the engine entity could grow without the work block lying `description: ""` on every row.

31. **The rating histogram and spread ride the detail faces only** (2026-08-26). `dto.PublicRating` has carried `distribution` and `stats` since the source importers filled them, but `ratingsFrom` dropped both, so the v2 face showed one aggregate number where v1 drew a chart. `Rating` now carries `distribution` (a `RatingBucket[]` on the source-native scale, ascending and sparse) and `stats` (`RatingStats`, all four members nullable). The presence rule is v1's, not a new one: the work detail face and `works/{id}/ratings` share `publicDetailRatings`, the works-list block goes through `publicRatings`, which never selected them — so a list response carries neither key and a client must not page ratings out of the list. `RatingBucket.score` is a `number`, not an `integer`, because G8 requires one JSON type per property name and `Rating.score` is already a `number`; every published scale is integral, so the wire bytes are the same. The denominator caveat is copied onto the field doc rather than left in the v1 DTO: bangumi, dlsite and howlongtobeat bars sum to `vote_count`, erogamescape bars come from a separately synced reviews mirror and are their own denominator, vndb bars omit private-list votes and sum to at most `vote_count`.

32. **Work companies carry the logo; tag and series detail grow their v1 blocks** (2026-08-26). `PublicWorkLabel.logo_hash` was already on the wire and thrown away, so a work page could not draw the brand mark v1 drew — `WorkCompany.logo` now fills from it through the same `ImageURL` builder deviation 30 added for the company face, which is why the work mappers take a `logoURL func(string) string` instead of reaching for the service. `tags/{id}` takes `include=intros`; `is_machine` is always `false` there and the doc says why (`catalog_tag_intro` has no provenance column, so `false` records *unknown*). `series/{id}` takes `include=intros,refs` and `has_nsfw` joins the basic shape on **every** lane — detail, cursor list and `ids=` batch — because `workCountsWithNSFW` already returned it beside `work_count` and the list DTO already carried it. `has_nsfw` is not `work_count > 0` under `nsfw=true`: it counts the same live-claimed members through the r18 *display* gate and is never narrowed by `nsfw=`, so a series legitimately reports `has_nsfw: true` with `work_count: 1` while the r18 member itself stays hidden. Both include blocks are detail-face only; the list route shares `SeriesSpec`, so it accepts the tokens and answers without them.

33. **`credit-names/{id}` is the staff read surface, and `repr.Person` stays thin** (2026-08-26). v1's staff page reads the NAME face, and `GetCreditName` was already calling `Public.Name` — the whole record — while mapping five fields out of it. The detail face now carries `gender`, `birth_year`, `birth_month` and `birth_day` as person-level facts reached through the name's public person link (absent when unrecorded or the link is hidden; the three birth parts are independently fuzzy), plus `include=aliases,photo,siblings,intros,links,refs`. `siblings` are the same person's other publicly linked names as basic `CreditName` entries, each stamped with that person id — a sibling shares the person by definition, and the sub-entries would otherwise arrive with `person_id: null` off a face reached through the person. The credits sub-face adds `character_name` beside `character_id` from `PublicNameRole.Character`, which `CreditNameCredits` was dropping. `repr.Person` is deliberately **not** enriched: the person face is an identity row, the name face is what a staff page reads, and the staff reverse lookup stays `credit-names/{id}/credits` (the decision recorded under Stage 5 read).

34. **The spoiler ceiling is a v2 closed enum on exactly three faces** (2026-08-27). The catalog service has always taken a spoiler ceiling; v2 passed a hardcoded `0` at three call sites and declared no parameter, so `spoiler>0` rows were permanently unreachable. `GET /v2/catalog/works/{id}`, `GET /v2/catalog/works/{id}/tags` and `GET /v2/catalog/characters/{id}` now take `spoiler=none|minor|major` (default `none`), mapped to v1's 0/1/2 at the boundary. It is **not** v1's integer: v2 publishes the closed `spoiler` vocabulary already used by `WorkTag.spoiler`, `CharacterTrait.spoiler` and `Appearance.spoiler`, and an unknown value is `400 UNKNOWN_ENUM_VALUE` like every other closed enum. The parameter is declared on three **dedicated** input structs, not on the shared `ResourceIDInput` / `WorkSubInput` — those are shared by every entity detail route and all twelve work sub-faces, and putting it there would publish an accepted-but-ignored `spoiler` on `engines/{id}`, `works/{id}/covers` and the rest, which is the exact defect deviation 35 refuses to widen. The doc keeps v1's nuance: only the VNDB-derived vocabulary records a spoiler level, Bangumi/DLsite folksonomy publishes no spoiler concept and those rows read `none`, so the default is the safe ceiling rather than an empty answer. Implementation trap, recorded in code: huma reflects over input structs and skips any field whose `IsExported()` is false — an **embedded** field takes its name from its type, so embedding the (then-unexported) `resourceIDInput` silently dropped `id`, `nsfw`, `view`, `include` and `fields` from both the generated spec and request binding while the route still answered 200. Both shared input types are exported for that reason alone.

35. **The company graph keeps company-set `include` validation; only `logo` applies** (2026-08-27). `getCatalogCompanyGraph` validated `include=` against `collect.CompanySpec()` and then discarded the parsed tokens, so `include=logo` passed the gate and changed nothing — `CompanyGraphNode` had no logo field at all, while v1's graph node carried `logo_hash` + `logo_meta`. The node now carries `logo`, present only when `include=logo` and that node has a logo, built through the same `imageURL` + `imageFromPublicMeta` path as the company face (the graph DTO carries `logo_meta`, so a node with dimensions and thumbhash gets them; a node whose image service did not answer still gets url + hash). The include set is deliberately **not** narrowed to a logo-only vocabulary: `aliases`, `intros` and `links` are 2xx on this route today, and rejecting them would be a breaking change. They are accepted and answered without, which the route Description now says out loud.

36. **`lang` joins the three entity shapes v1 carries it on** (2026-08-27). v1 publishes `lang` — "BCP-47 language of display_name" — on company (`PublicLabel` / `PublicLabelDetail`), credit name (`PublicName`) and character (`PublicCharacter`). v2 carried only `latin` + `localized{}`, and `localized{}` is a translations map: the language of `display_name` itself cannot be reconstructed from it. `repr.Company`, `repr.CreditName` and `repr.Character` now carry `lang` as an always-present nullable string beside `latin`, `null` when unrecorded. Scope is exactly those three — tags, series and engines do not carry the axis in v1 and do not gain it here. It is filled on **every** lane that emits these shapes: detail, cursor list, `ids=` batch, `credit-names/{id}?include=siblings`, `persons/{id}/credit-names` and the voice entries of `characters/{id}/appearances`. Two lanes had no source for it and got a purely additive **v1 read-projection extension** rather than a null that lies (deviation 30's precedent): `dto.PublicLabelListItem` gains `lang` and `LabelsList` selects it — the only change visible on a v1 wire, and only on `docs/catalog/public-openapi.yaml` — and the v2-private `catsvc.EntityListRow` gains `Lang`, with `CharactersList`, `NamesList` and `PersonNames` selecting `lang`. `PersonsList` and `TraitsList` share that row type but keep their own `SELECT`: `catalog_person` and `catalog_character_trait` have no such column, and `repr.Person` / `repr.Trait` do not publish one.

37. **Person `links[]` also mints from the two exact anchors that are addressable pages** (2026-08-27). `personLinks` selected only person-level `link_kind=related` rows, so the page-addressable identity anchors — the ones a staff page actually wants to link — were unreachable on both wires. `link_kind=exact` now mints for exactly two sources whose person-granularity id **is** a stable public page: **vndb** (`^s[0-9]+$` → `https://vndb.org/{id}`, 5,173/5,173 person rows conform) and **bangumi** (`^[0-9]+$` → `https://bgm.tv/person/{id}`, 2,767/2,767 conform, covering 2,740 distinct persons). An id of the wrong shape is skipped, never guessed. `probable` still never mints, and the other two sources holding person exact anchors stay excluded on purpose: **dlsite** (2,566 rows — creater ids have no public page) and **erogamescape** (4,428 — `creater.php` is unverified). Coverage of credit names carrying at least one link: **9,336 → 11,330 of 120,697** once vndb mints, **11,384** with bangumi added. The additions are purely additive on both wires (v1 `PublicName.Links`, shape `{source,url}` unchanged; v2 `credit-names/{id}?include=links`), the existing `link_visibility` gate is inherited unchanged, and the merged set keeps the single `ORDER BY r.source_id, r.external_id`. Minting is **person-only**: `workLinks` and the label links keep the shared `relatedLinkURL` template table, which is untouched. The incident this closes, recorded in code: the forum minted vndb URLs from the refs on a **credit name**, where a vndb ref is a staff **alias** id stored as bare digits (`1`, `10`, `100`) — `https://vndb.org/1` is an unrelated VN, and it shipped as a wrong-person page. `Ref.external_id`'s doc now says out loud that a ref is an identity anchor at that entity's own granularity and not necessarily a page, and that browseable URLs come from `links`.

38. **The silent 100-row cap on every embedded block is removed** (2026-08-27). A `cap100` helper truncated *every* `include=` block on the work, entity and credit-name faces — about 38 call sites — with no parameter, no `has_more`, and no entry in this ledger. v1 has no cap anywhere, so the two faces disagreed silently, and the truncation was already lying on production data: works with more than 100 rows in one block — tags 632 (max 264), credit rows 541 (max 340), characters 51 (max 354), releases 3 (max 160), relations 2 (max 322), covers 1 (max 104) — plus 651 characters with more than 100 trait links (max 227). It surfaced only because deviation 34's spoiler unlock pushed work-detail tag counts past 100 and the forum noticed a page that stopped at exactly 100. Blocks now return every row the read produced; the paged sub-faces (`works/{id}/tags` and the rest) remain the bounded access path and are unchanged. Live fixtures carry a work with 105 tag rows and a character with 105 trait links, and the tests assert the block agrees with the sub-face paged total.

39. **Company `latin` is filled, with the deviation-36 v1 projection extension** (2026-08-27). `repr.Company` declared `latin` from wave 3 and no lane ever populated it — a null that lies, on a column 542 of 38,317 live labels record. The root cause sat on the v1 side: the wave-209 name-primitive doctrine promises `display_name` + `latin` (when recorded) + `localized{}` on every projection that carries an entity name, but the two label head faces never got the second field — `PublicLabel` even documents the render rule `localized[yourLocale] ?? display_name ?? latin` while not carrying `latin`. Exactly like deviation 36's `lang`: `PublicLabel` and `PublicLabelListItem` gain `latin,omitempty`, `LabelWorks`/`LabelsList` select the column, and v2 maps it on the detail, cursor-list and `ids=` batch lanes. The v1 wire change is purely additive and only on `docs/catalog/public-openapi.yaml`. The deliberately thin projections stay thin: `WorkCompany` (the work-detail block), label `relations`, `via_label` and graph nodes never declared `latin` and do not gain it here.

40. **`/v2/me/playtimes` honors `include_total`** (2026-08-27). The lane parsed `include_total` like every other collection (bogus values 400) and then answered `"total": 0` unconditionally — the same accepted-then-ignored shape as deviation 10's works gap, found while fixing it. The cursor lane now counts the bearer's rows (all of them — the count ignores the `updated_at` cursor, deviation 10's count-before-cursor rule) and the `work_ids=` batch arm publishes the matched count, 0 on all-miss, instead of discarding the caller's flag.

41. **The playtime cursor could loop forever** (2026-08-27). Found by deviation 40's page-crawl test, which hung the suite at the 600s package timeout — twice. The `/v2/me/playtimes` cursor was the last row's `updated_at` in `time.RFC3339`, which truncates to whole seconds, and the next page selected `updated_at > cursor`: any two rows updated within the same second re-included the boundary row on every page, so a small-limit crawl never terminated and adjacent pages duplicated rows even when it did. The cursor is now `RFC3339Nano|work_id` — nanosecond precision plus a `work_id` tiebreak against the new `updated_at ASC, work_id ASC` order (`work_id` is unique per bearer) — and a bare-time cursor from before this change still parses, with the old semantics. The v1 `/playtime/mine` lane shares `ListMine` but keeps its strictly-after `updated_since` watermark semantics untouched: the tiebreak arm only engages when a work id is present, and v1 never passes one.

42. **`/v2/catalog/calendar` takes `content_limit=` and `olang=`** (2026-08-27). The v1 calendar validates both (`bogus` → 400) and feeds them into `CalendarFilter`; v2 declared neither, so `content_limit=bogus` answered 200 and an SFW reader got the unfiltered month — the third face to regress the editorial display gate after deviations 30 (tags `is_sexual`) and 34 (spoiler). Both are now declared on the calendar input and wired: `content_limit` through the same `closedCSV` the works lane uses, `olang` through a calendar-local parser because the defaults differ — absent `olang` on the calendar means the **home population, ja plus zh** (v1's zero `PublicOLang`, which v2 was already inheriting by passing the zero filter), while the works lane defaults to all languages; `olang=all` opts out.

43. **The calendar answers a `meta` navigation block** (2026-08-27). v1 serves `{today, min_month, max_month, has_prev, has_next}` off `CalendarBounds`; v2 returned a bare list, so a month navigator had no source for its arrows and the forum's month switcher died. The calendar output is now `CalendarList` — the list envelope plus an always-present `meta`. `today` (Asia/Tokyo) is always filled; the four navigation fields belong to the dated month window only and are `null` on the year-only and undated windows, exactly v1's shape: bounds computed under the same nsfw/olang/content_limit population, `min_month`/`max_month` null when that population has no dated release, `has_prev`/`has_next` false rather than null in that case.

44. **Work-detail roster rows carry `voices` and `identity`; covers carry `cover_kind`** (2026-08-27). `PublicRosterCharacter` has always carried both: the voice credits joined per character, and the opaque `roster:<character_id>` row identity a `catalog.work.roster.suppressed` proposal echoes back. v2's `WorkCharacter` mapped neither, so every character card lost its voice actors and no v2 client could file a roster suppression — the write engine accepts the patch, but the identity was published nowhere on /v2. `voices` maps to the same thin `CreditName` entries the appearances face already uses; `identity` is `omitempty` because a character reached only through a voice credit has no roster row to suppress (that arm reads `roster_role: unknown`, `spoiler: none`). `PublicCover.kind` — the upstream cover class from the pinning ladder (`main`, `dig`, `pkgfront`, …) — joins `Cover` as **`cover_kind`**: gate G8 forbids a bare `kind` property, so the field follows `title_kind`/`release_kind`/`tag_kind`. This reverses a recorded decision — `TestCoverHasRowIDNotKind` refused the field “until that vocabulary is closed” — because the values are upstream-derived and will never close; it ships as an explicitly **open** vocabulary under the `source`/`platform` convention (“must not be used as a discriminant”), empty when unrecorded.

45. **The company rollup lane names the intermediate imprint: `via_company`** (2026-08-27). On `company_id=&company_rollup=true`, v1 stamps each row reached through a one-hop imprint/subsidiary with `via_label` — the service computed it for v2 too (the zero `PublicFields` wants everything) and the v2 mapper threw it away, so a company page could not split its own works from its imprints' and the forum's own/imprint counts collapsed to N/0. `repr.Work` gains `via_company,omitempty`, a slim `{object, id, display_name, localized}` company stub, present only on rollup rows attributed through the hop and absent on direct attributions.

46. **`/v2/catalog/tags` honors `has_works=`** (2026-08-27). Deviation 30 restored `has_works=` on the companies lane and left the tags lane behind, though v1 carries it on both and `TagsListFilter.HasWorks` already existed — so the flag was silently ignored (the huma input never declared it) and a tag index drowned in zero-work rows. Declared and wired exactly like companies: `parse.Bool` (bogus → 400), same nsfw-gated `work_count > 0` predicate, total converges with the filter.

47. **User-token traffic is rate-limited per user, not per IP** (2026-08-28). Deviation 29 moved keyed traffic onto per-key tier limits and left "any request that never authenticates" on the per-IP default — but a user access token *does* authenticate and still resolved no limiter identity, so the whole `/v2/me` and `/v2/moderation` plane fell into the anonymous bucket. A first-party backend relays every logged-in user's call from one egress IP: the forum's entire user plane shared a single `100/min + 10k/day` bucket and answered 429 `QUOTA_EXCEEDED` from 2026-08-27T22:46Z, re-tripping after each UTC-midnight reset (`v2:quota:<forum egress IP>` measured 17,848 by 07:35Z the next day). Now `credentialLimitIdentity` falls through to the authenticated uid — key `u<uid>`, the default `100/min + 10,000/day` per user. Anonymous keyless paths (problems, vocabularies, stats, schemas, openapi.json) keep the per-IP default; an application key still wins over a uid when both are present.

48. **`claim_state=pending|declined|hidden` is readable on `/v2/catalog/works`, behind moderation authority** (2026-08-28). Wave R1 narrowed the closed set to `{none, live, draft}` and pointed the queue at `/v2/moderation/claims`, but the forum's live admin submission queue reads it from here (`claim_state=pending&claimed=true&site=kungal&sort=updated`) with an **application key**, and the moderation face demands a user token carrying `catalog.claim.review`. The queue answered 400 `UNKNOWN_ENUM_VALUE` in production and rendered as "no pending submissions" — the review POST on the same page still worked, so nothing looked broken. R1's intent is kept and only its instrument replaced: the three moderation states are admitted **only** to a credential holding the operator-granted `claim_events:read` scope, and then `site=` is **mandatory** and fenced to the caller's own catalog site (resolved from the key's oauth client, `oauth_clients.catalog_site`) — 403 `PERMISSION_REQUIRED` on another tenant's queue, 403 `SITE_NOT_BOUND` when the key's application has no site. Without the scope the request gets the **identical** 400 `UNKNOWN_ENUM_VALUE` with `allowed values: none, live, draft` it got before, byte for byte: the wider set must not be discoverable by the shape of the refusal, and no unauthorized caller sees any change at all. The adjudicated user-token half of the rule (`catalog.claim.review`) is **not reachable on this face** and is not implemented. **Amended 2026-09-06:** `catalogAuth` now does accept a user token on `/v2/catalog/*`, but the user lane sets no `catalogAuthz` at all — so the three moderation states stay out of the vocabulary for a person's token exactly as before, and for the same reason: the queue is an application's own per-site queue, and a person's token names no site to fence it against. Zero migrations; the service layer already supported the states (`WorksListFilter.ClaimStates`).

49. **Auth rejections get their own IP-keyed bucket** (2026-08-28). Deviations 29 and 47 moved the quota limiter behind the auth stack deliberately, and that left one hole nothing counted at all: a request the auth stack refuses never reaches `protocol.RateLimit`, so 401/403 volume was unbounded — a credential brute-force cost the attacker nothing and cost us one uncached `ResolveByHash` per distinct garbage token. A second, cheap bucket now counts **only the answers the quota limiter never saw** (`localsPastAuth` is set at the top of `RateLimit`, so reaching it at all is the proof auth let the request through) and blocks a source that crosses **120 failures in a rolling minute** for 60s, answering 429 `RATE_LIMITED` with `Retry-After`. It is keyed by IP because a request that failed to authenticate has no other identity, which is also its cost: a first-party backend sustaining more than 120 auth failures a minute from one egress IP blocks its own traffic for the rest of the window. The quota limiter stays behind auth — moving it back in front would reintroduce both regressions at once. A scanner-shaped test hits this: `route_gate_walk_test.go` opts out of the block marker, and says so.

50. **The limiter fails open, and now says so** (2026-08-28). `limiter.before` answered a store error with a bare `return nil` — no log, no metric — so one redis outage removed every rate limit and every daily quota across `/v2` at once, silently. Fail-open is kept **deliberately**: the store is the same redis every `/v2` read already tolerates losing, and failing closed would turn a redis blip into a total API outage; the sibling v1 limiter (`devapi/middleware.go`) has always failed open with a `slog.Warn` for the same reason. Every store error on the counting path now logs `v2 limiter store unavailable; failing open` at WARN, throttled to one line per 10s with a `suppressed` count so an outage does not melt the log pipeline.

51. **A 304 no longer refunds the limiter** (2026-08-28). `limiter.after` decremented both counters whenever `applyETag` rewrote the status to 304 — *after* the handler had already run the whole read, computed the body and hashed it. Unlimited `If-None-Match` polling therefore cost zero quota while doing full work. The refund is deleted rather than moved: the ETag is a digest of the body on every face that does not set its own, so there is nothing to short-circuit on before the work. A conditional request is a request.

52. **The idempotency key carries the authenticated caller, and is evaluated after auth** (2026-08-28). The key was `c.IP() + method + path + Idempotency-Key`, computed inside `protocol.Middleware`, which runs **before** the auth stack. A first-party backend relays every one of its users from one egress IP — the exact topology deviation 47 was written about — so two different users sending the same `Idempotency-Key` on the same path replayed each other's writes. Replay and remember moved into `protocol.Idempotency`, registered after `RateLimit`, and the key's identity segment is now the resolved application key (`k<key_id>`) or user (`u<uid>`), falling back to `ip:<addr>` only for a request that authenticated as neither. Placing it behind `RateLimit` also means a 429 short-circuits before the replay store is consulted at all, which is deviation 29's "a 429 is never memoised" made structural rather than conditional.

53. **The moderation proposal faces gate on review STANDING, resolved by the engine — type-level for the queue, entity-level for one item** (2026-08-28). `GET /v2/moderation/proposals` and `GET /v2/moderation/proposals/{id}` gated on `requireUser` + `requireSite` only, so any logged-in user with a site-bound client read the whole open queue and, with `include=patch`, every proposal's patch. The first fix asked a hand-maintained permission union (`perm.Moderates`) on both, and that was the wrong instrument for the item lane: 01-service-and-contract §4.4 says out loud that 「kungal overlay 的 owner-review 通道在队列上同样成立」 and forbids 「另造一个「队列专用」权限键」, and measurement agrees — `OwnerReview: true` exists in exactly two places, both inside the **kungal site overlay** (`editspec/work.go:65` for `catalog.work`, `release.go:54` for `catalog.release`), never in a `DefaultPolicy`, never for letmoe (whose overlay carries `AutomergeOwner` and no owner review), never for character or taxonomy. A permission union cannot see `entity_id`, so it cannot express that channel at all, and it refused precisely the callers the forum's own gate admits (`edit_handler.go` `reviewEntry`: `CanModerate(roles)` **OR** `isGameOwner(...)`; the `/galgame-edit/proposals/:id`, `/amend`, `/merge` and `/decline` routes carry no role gate of their own). The item lanes therefore ask the engine: `reviewsAnyField` derives `IsEntityOwner` from `spec.OwnerUserID` and returns true when **any** registered field's `EffectivePolicy(key, site).AllowsReview(actor)` does — the same call `AmendProposal` and `MergeProposal` already make, so site overlay, per-family permission and deviation 55's `ModerationCapped` are all honored without restating one of them. Owner-can-**decide** is kept, not narrowed to read: it is the live behaviour. The **type-level queue keeps `perm.Moderates`**, where there is no entity to own; `object=`+`entity_id=` (new, additive) narrows it to one entity that its owner may read without any permission, and the filter is applied to the query and not only to the check, because an owner arm that leaked the whole type's queue would be worse than the gap it closes. `entity_id=` without `object=` is 422 `INCONSISTENT_WITH`, matching the revisions face.

    **The engine channel is not forum's gate, and on unclaimed works it is narrower — deliberately.** Forum reaches the item face from three places, all on plain-`authed` routes and all through `proposalForReview` (`edit_handler.go:718` ProposalDetail via `reviewEntry`, `:802` Merge, `:857` Decline, at forum `2dbcd107`), and the infra call happens **first**, before forum's own `CanModerate OR isGameOwner` runs. The two owner notions resolve from different columns and are not the same person: forum's `isGameOwner` (`edit_handler.go:130`) goes `gidOf(workID)` → `ownerOf(gid)`, the **forum galgame-topic creator**, while `reviewsAnyField` derives `IsEntityOwner` from `spec.OwnerUserID` → **`catalog_work.owner_user_id`**, stamped by claim/submit. They agree wherever the work is claimed. They cannot agree where it is not: the catalog has no owner there at all, `OwnerUserID` returns nil, `IsEntityOwner` stays false, and only a review permission gets through. So a forum topic-creator holding no review permission goes 200 → 403 on the item face for a work the catalog considers unowned. **Measured on production `kun_catalog`, 2026-08-28** (`entity_type` is `catalog.work` for all 1,121 rows; `status` is the `editing` enum, `0=open 1=merged 2=declined 3=withdrawn`): `edit_proposal` total **1,121** — **1** open, **1,094** merged, 15 declined, 11 withdrawn; proposals whose work has `owner_user_id IS NULL` **73** (6.5%), of which **69 merged and 1 open**; `catalog_work` with an owner **11,307**. The residue is therefore view-only twice over: 69 of the 73 are already merged, so no decision remains to take on them, and for the rest `canDecide` reads `CanReview` from this same engine rule, so those callers were already looking at a page whose actions were refused. Narrowing is the point — `catalog_work.owner_user_id` is the catalog's statement about who owns a row, and a forum topic-creator flag is forum's, which must not confer catalog review standing on a row the catalog says nobody owns.

54. **The proposal write lanes gained the fence the read lane already had** (2026-08-28). `GetModerationProposal` has compared `rec.Site` since it shipped; `DecideProposal`, `PatchProposal`, `AmendProposal` and `RevertRevision` compared neither `Site` nor `ProposerUID`, so review authority on one tenant decided, amended and withdrew another tenant's proposals — `AmendProposal` gated on `AllowsReview` alone. All four now fence on `prop.Site` → 403 `TENANT_MISMATCH`, and patch/amend additionally require the proposer or review standing on that entity (deviation 53's `reviewsAnyField`, so the owner channel is not lost) → 403 `PERMISSION_REQUIRED`. The fences run independently of `If-Match`, because `protocol.IfMatch` treats `*` as "matches any non-empty validator" and that must not be a way past a check. `RevertRevision` additionally refuses a revision whose entity is claimed by another site (`spec.OwnerSite`; an unclaimed entity has no owner and is not fenced), and the **engine** now resolves the revert's field policy under the entity owner's site instead of the caller's, exactly as `AmendProposal`/`MergeProposal` already resolve it under `prop.Site` — a tenant whose overlay is looser than the owner's was reviewing the owner's fields under its own rules.

55. **`PolicyContext.ModerationCapped` is wired again** (2026-08-28). Wave 186b introduced the flag as "this caller's *surface* is not a place where verdicts are reached" and the v1 handler filled it from `isThirdPartyClient(clientFromCtx(ctx))`. Wave R3 deleted that surface and carried nothing to v2, so from 2026-08-27 the flag was set by **nothing** and every branch it guards was dead: a developer-owned app's token reached verdicts with whatever roles its user carried, and on an open tenant its own edits automerged. v2's `policyActor` fills it again from the token's OAuth client (`oauth_clients.owner_user_id IS NOT NULL`), plumbed through a new `handler.SiteBinding` that `LookupSite` already had to read that row for. Verified against production before shipping: every client bound to a catalog site there has `owner_user_id` NULL — forum `4ed9bc99…`, moyu `df3ff600…`, `letmoe-staging`, `news`, `trust` — so nothing first-party is capped today and the flag fires only for a future developer-owned app that is given a catalog site.

56. **`include=diff` on `/v2/catalog/revisions/{id}` still carries no display axis, deliberately, and it is a known leak** (2026-08-28). Deviation 16 keeps the revision **rows** free of `nsfw=` because the two mirror crons walk the collection ascending by id and a filtered row would be silently skipped past a watermark. `include=diff` is a different promise on the same face: it prints the entity's own field values, so an r18 work's titles and cover hashes are one query away on a plain `catalog:read` key, on a face with no axis at all. The obvious fix — declare `nsfw=` and answer 400 on an r18 entity without it — was written, measured against the consumers, and **reverted**: the forum's public `GET /galgame/:gid/edit/diff` (an unauthenticated route) proxies straight to this operation with an application key and no `nsfw`, and **5,798 of production's 12,835 revisions (45.2%) sit on works the display axis hides**, so the gate would have blanked nearly half of the edit-history diff view on the deploy that shipped it. Fixing it needs two phases, in this order: declare and honor `nsfw=` while leaving the refusal off is **not** acceptable (an accepted-and-ignored parameter is the exact defect class deviations 10, 40, 42 and 46 record), so the parameter and the refusal ship together only after forum, moyu and letmoe send `nsfw=true` on their diff reads. Until then the leak is recorded here rather than patched blind.

57. **Claim and proposal validators advance with the record** (2026-08-28). Amends deviation 15's shapes. `"c<work_id>.<state>"` was guessable without ever reading the claim and did not advance with it: an ABA round trip — pending → live → draft → pending — came back to the same string, so an `If-Match` taken before the trip still matched after it, and a `display_name` change still answered 304 on the GET. The claim validator is now `"c<id>.<sha256/128 of id, state, site, product_work_id, last_event.id, display_name>"` — the newest claim event id is the part that only ever goes up. The whole record is deliberately **not** hashed: `first_acted_at` and `acted_count` are the bearer's own aggregate and exist only on the me lane, so the write lane's pre-read would never agree with them. `ClaimByWorkID` grew a LATERAL join for that event, which also fills `last_event` on `GET /v2/moderation/claims/{id}` — its field doc has always promised it on single-claim reads — while `first_acted_at`/`acted_count` correctly stay absent there. `"p<id>.<updated_unix>"` became `UnixMicro`, matching the news validator (deviation 27): two amendments inside one second are ordinary, and at whole-second granularity the validator from before the first still matched after the second.

58. **The `/v2` user token must carry our own issuer** (2026-08-28). `jwt.ParseWithClaims` validates `exp`, `nbf` and `iat` by default and **nothing else**, so `iss` and `aud` were never checked; every service in the platform shares one HS256 secret and one JWK Set, so any token this OP ever signed parsed as a v2 user token. `cmd/catalog` now hands `/v2` a verifier requiring `iss == cfg.OIDC.Issuer` (`KUN_SITE_URL`, which sits in the shared compose env and is the OP's own issuer in every process, `https://oauth.kungal.com` in production). Only the `/v2` lane is narrowed; `middleware.JWTAuth` on the admin prefixes keeps the unrestricted verifier. **`aud` is deliberately not enforced**: a first-party login carries none at all (`AuthService.generateTokens` sets no `Audience`) and an OAuth client only carries one when its site has a domain (`clientAudience` returns nil otherwise), so a required audience would 401 the whole user plane. `alg` was already pinned, structurally — `oidctoken`'s keyfunc switches on the method type and hands an HMAC token the configured secret and an ECDSA/RSA token a JWK Set key, so `alg: none` and RS256→HS256 confusion both fail; both now have a regression test rather than only a reading of the code.

59. **A dev-app config change is a revocation** (2026-08-28). `AdminService.RevokeKey` has actively busted its own `devkey:<hash>` entry since the platform shipped, but the cached credential carries the **application's** `dev_enabled`, tier and per-app limits, not just the key's row — and `UpdateAppConfig` busted nothing. Disabling an app (which `ResolveByHash` refuses on) or tightening its tier therefore stayed unenforced for the 60s positive-cache TTL. `UpdateAppConfig` now deletes the cache entry of every key on the app; the cache key is built in one place (`credCacheKeyForStoredHash`) rather than by restating the `"devkey:" + hex` shape at each writer.

## Wave — the /v2 gate read a path fiber never matched on (2026-08-28)

`GET /v2/Catalog/works` answered **200 with no credential** on production, and so
did `/v2/CATALOG/works` and `/v2/Catalog/claim-events` — the last one returning
real operator rows (`actor_uid`, `reason`, `site`, `from_state`, `to_state`)
across every tenant. `/V2/catalog/works` 404'd, because Traefik's
`PathPrefix(/v2)` is case-sensitive; only the segments after `/v2` were
exploitable.

The mechanism is one fiber fact. `Route.match` compares against
`c.detectionPath` — `c.path` lowercased while `CaseSensitive` is off and with
trailing slashes stripped while `StrictRouting` is off — while `c.Path()`
returns the raw `c.path`. `catalogAuth` and `userAuth` both dispatched on
`c.Path()` with case-sensitive `strings.HasPrefix` over an **open**
`default: return c.Next()` arm, which is correct in itself (`/v2/me`,
`/v2/news`, `/v2/problems`, `/v2/vocabularies` legitimately take no application
key). So a case-variant path matched the route, missed every prefix in the gate,
and fell out the open arm. The same mismatch defeated the operator guard, which
compared `path == "/v2/catalog/claim-events"`: one trailing slash routed to the
same handler with the extra scope unchecked.

Three layers, because any one of them alone is a config flip away from
regressing:

1. **Both gates key on a normalized path** (`pkg/routepath.Normalize`: lowercase,
   trailing slashes trimmed), so the gate sees what fiber matched. This holds
   even if the router config is reverted. Deviation 75 moved this helper out of
   package `handler` and put the other six sites that read `c.Path()` behind it.
2. **`CaseSensitive: true`** on the shared fiber config, so variants 404 rather
   than falling through. **`StrictRouting` stays off**: `cmd/oauth` registers
   `sites.Get("/")` and `oauthClients.Get("/")` — i.e. `/api/v1/sites/` and
   `/api/v1/oauth/clients/` — while `apps/web` calls both without the trailing
   slash, so turning it on 404s the admin console's site and OAuth-client pages.
   Verified empirically against fiber v3.2.0 rather than assumed. Layer 1 covers
   the slash case instead.
3. **A walk over every published path** (`route_gate_walk_test.go`) issues the
   case-variant, `…/` and `…//` forms of every path in
   `docs/catalog/v2-openapi.yaml` with no credential and asserts the answer is
   still the gate's own (`MISSING_CREDENTIAL` / `NOT_FOUND`), never a
   handler's. Asserting merely "not a 2xx" was tried first and passed green
   against the unfixed gate — every face is unbound in a unit test, so a request
   that sails past the gate still answers 503.

`catalogAuth` also stopped treating a nil credential store as a grant: with no
lookup wired, every gated face answered anonymously. It is 503
`SERVICE_UNAVAILABLE` now, the same shape the lookup-error branch already used.
Two existing tests leaned on that escape hatch (`Bearer test` against a nil
store) and now carry a real credential store.

Zero migrations. No spec change beyond deviation 48's `claim_state` description.

60. **The list lanes published the last edit as the creation date** (2026-08-28). `workFromListItem` passed `it.Updated` into both the created and the updated argument of `repr.NewWork`, so `created_at` on the works list, the calendar and the search lane was the row's `updated_at`. Work 4 read `created_at 2026-08-26T03:08:36Z` on the list lane and `2026-07-06T10:45:35Z` on the detail lane — the same object with two creation dates. `dto.PublicWorkListItem` gains `created`, the four SQL lanes that build a `workListSourceRow` (works list, calendar page, release feed, search hydration) select `created_at`, and the mapper keeps `workFromDetail`'s fallback to `updated` for a row with no stamp.

61. **The releases lane was narrowed to the home languages** (2026-08-28). `ListReleases` and `GetRelease` built `catsvc.ReleaseFeedFilter` leaving `OLang` at its zero value, which `PublicOLang.predicate()` reads as `ja` plus `zh%` rather than "no gate" — the same trap PR #87 closed on the v1 works lane, on a struct that has since grown two more constructions. Neither operation declares an `olang=` parameter, so the narrowing was unreachable and unrecoverable: an `olang=en` work's release was published by `works/{id}/releases` and 404'd on `GET /v2/catalog/releases/{id}`. Both sites now spell out `PublicOLang{All: true}`, matching `Catalog.ListWorks`. The other three constructions were audited: the works list and search lanes take the caller's parsed `olang=` (default `All`), and the calendar's zero value is deliberate and commented (deviation 42).

62. **The work brief carries `olang`, `created_at` and `updated_at`** (2026-08-28). `workFromBrief` published empty strings for all three — required properties of the shared `Work` schema, two of them `format: date-time` — on `works/{id}/relations`, `characters/{id}/appearances` and `credit-names/{id}/credits`. A generated typed client rejects the whole page. The brief is documented as "the same type as the work collection, view=basic", so the fix fills the values rather than dropping the fields, which would have been a breaking removal from a stable 2.x schema. `dto.PublicWorkBrief` gains the three, and they are filled in `fillWorkBriefNames` — the one function every brief construction site already funnels through — rather than in each of the six callers.

63. **`/v2/catalog/changes` and `/v2/catalog/redirects` count their populations** (2026-08-28). Both feeds passed a literal `0` where every other collection passes its total, so `include_total=true` answered `total: 0` beside a non-empty `items[]` and a consumer sizing a backfill off the documented mirror lane (§8) read "nothing to mirror". `ChangesTotal` counts the same settled-galgame population `Changes` pages over and `RedirectsTotal` counts the redirect rows under the same `object=` filter, both before the cursor predicate — deviation 10's count-before-cursor rule.

64. **The redirect feed no longer drops a row with no merge time** (2026-08-28). The keyset compared `(merged_at, entity_type, old_id) > (?, ?, ?)` as a row value, and a row value containing a NULL compares NULL, never true: a `catalog_redirect` row with `merged_at IS NULL` was invisible on every page of the feed, so no mirror could learn about that merge. The comparison and the ORDER BY now run on `COALESCE(merged_at, to_timestamp(0))`, those rows publish `merged_at: 1970-01-01T00:00:00Z` (a legacy sentinel that also sorts first, which is where an unrecorded merge belongs), and the cursor advances on **every** row it returned — it previously advanced only on rows that had a merge time, which would have turned the newly visible rows into a crawl that never terminates.

65. **`/v2/catalog/persons` and `/v2/catalog/traits` fill their required fields** (2026-08-28). `PersonsList` selected only `id, display_name`, so `primary_credit_name_id` and `gender` — both required on the shared `Person` schema — were `null` on every row of the list lane while the detail face carried them; the same class as deviation 39's `Company.latin`. Both columns join the `SELECT` and `personFromRow` maps them exactly as `GetPerson` does. Alongside it, `EntityListRow.VndbTID` and `TraitRow.VndbTID` carried no column tag, so GORM looked for `vndb_t_id` and every trait row on **both** the list and the detail face shipped `vndb_tid: ""` — the recorded acronym-column trap, third occurrence after `olang` and `b_i_d`.

66. **`/v2/catalog/characters` answers `include=` and `view=`** (2026-08-28). `collect.CharacterSpec` has declared thirteen include tokens and a matching `FullSet` since wave 3, and the list lane read none of them: `include=traits` answered 200 with no block and `view=full` returned a key set byte-identical to basic. Rejected alternative — narrowing the declared vocabulary — because the detail face already serves all thirteen and a list that cannot ask for them is the weaker contract. Every token is now filled on **both** the cursor and the `ids=` lane through batched loaders that reuse the detail face's own election rules (the intro per-language fold, `entityAliasesBatch`, `entityRefsFor`, the trait join with its spoiler ceiling and `nsfw` gate), so the two faces cannot drift.

67. **A banned work leaves the public browse and search lanes unconditionally** (2026-08-28). `ban` writes only `claim_state`, never `status`, and the claim-state predicate ran only when the caller passed `claim_state=` — so a banned work stayed in the default `GET /v2/catalog/works` page and in `q=` search. The decision face's own description promises ban "hides it from any state", so that is the contract: the exclusion does not wait to be asked for, and only an explicit `claim_state=hidden` opts back in. The three service tests that asserted "no claim_state = no gate" encoded the defect and are rewritten. Beside it, `claimed=` tested only `w.site` while `claim_state=` also requires `product_work_id`, so a row with a site and no product id answered `claimed=true` and `claim_state=none` at the same time; `claimed=` now uses the shared `claimedSQL`.

68. **The work credits order is total and its curated lane is not inverted** (2026-08-28). `HumanLaneFirstNoProvenanceSQL` emitted `(source_id IN (1, 12)) DESC`; `catalog_credit.source_id` is nullable, `x IN (...)` over a NULL is NULL, and `DESC` defaults to NULLS FIRST — so the sourceless rows sorted **ahead** of the human lane the term exists to promote, inverting it for exactly the rows it was written for. `NULLS LAST` fixes it and is a no-op on the intro tables that share the helper. The same `ORDER BY` then stopped at `cn.id` while `uq_catalog_credit` is `(work_id, credit_name_id, role_id, COALESCE(character_id, 0))`: one voice actor on three characters of one work is three fully tied rows, and `works/{id}/credits` pages by OFFSET, where a tie duplicates or drops a row at the page boundary. `COALESCE(c.character_id, 0), c.id` is the tiebreak and `src.key` is pinned with `COLLATE "C"`.

69. **Blocks that were computed and thrown away, or never computed** (2026-08-28). Four of a kind, all "the schema declares it and no lane fills it": `Cover.vote_count` was a literal `0` while `ReadService.CoverVotes` sat with no caller, so the vote a reader had just cast never came back; the credit row `identity` was dropped by `creditGroupsFrom` and `repr.CreditEntry` had no field for it, which made `catalog.work.credits.suppressed` impossible to file from v2 (`role_id` is not published either, so the key cannot be rebuilt client-side) — the sibling of deviation 44's roster identity; `voices[].person_id` was declared on the shared `CreditName` schema and filled on neither the roster block nor the appearances face; and `repr.Appearance` carried no `identity` at all, so the same roster row was addressable from the work side and not from the character side. All four now project, with the person link following `link_visibility` exactly as the credit-name detail face does.

70. **`release_date` is a full date and `release_date_precision` is real** (2026-08-28). The catalog stores a fuzzy date — `partialISOFromOrdinal` yields `2024`, `2024-06` or `2024-06-15` — and v2 published it verbatim under `format: date` with the precision hard-coded to `"day"`. A year-only row shipped `"2024"`, which no date parser accepts, while claiming day precision, and a work releasing next year read `released`. The schema already said what to do: month dates sit on the 1st, year dates on January 1. Precision is now derived from the stored precision, the value is padded to match, and a date in the future reads `dated` rather than `released`. `Release.date` on the releases lane is **not** padded — that schema carries no precision field, so padding there would invent a day with no way to signal it; it is recorded here as a known partial value.

71. **The company graph declares its truncation** (2026-08-28). `LabelRelationGraph` walks at most 60 nodes and 4 hops and said so nowhere, so a family cut at the ceiling was indistinguishable on the wire from a complete one — the standing rule against silent caps, the same one deviation 38 applied to the embedded blocks. `CompanyGraph` gains `truncated`. It is not merely "the depth ran out": when the walk stops at the depth ceiling with a frontier left, one `EXISTS` asks whether that ring actually has an unvisited neighbour, so `truncated: false` means the graph is the complete connected family.

72. **Two counts and a query that meant something else** (2026-08-28). `works/{id}.series[].member_count` was a bare `count(*)` over `catalog_series_member` with no soft-delete, status, medium, claim-state or r18 predicate, so it disagreed with `series/{id}.work_count` for the same series while the spec says "members visible under the same NSFW gate"; it now comes from the same `workCountsFor(seriesWorkEdge)` the series faces use. `/v2/catalog/series?ids=5,5` returned two copies of row 5 because the series batch lane never consulted its `seen` map, unlike every sibling lane. And `credit-names?q=` was interpolated straight into `ILIKE '%' || q || '%'` with `NormalizeNameQuery` doing only `TrimSpace`: `q=%` returned a full page off an unbounded scan and `q=a_b` matched `axb`. Not injection — the value is still bound — but the caller's substring is now escaped and the pattern carries an explicit `ESCAPE`.

73. **`GET /v2/store/stats` no longer declares 502** (2026-08-28). It reused the purchase-link operation's error list, and `502 BAD_GATEWAY` is minted only by `storeErr`'s `ErrShortenerDown` arm, which only the link-minting path can reach. The stats face keeps 401/403/422/503; the minting face keeps 502.

74. **Every work search document must carry a `claim_state`** (2026-08-28). Deviation 67's unconditional `claim_state != 'hidden'` is a negated filter, and Meilisearch answers a negated filter with **zero hits and no error** when the attribute is absent from every document in the index — probed directly on v1.45: with one document carrying `claim_state` the field-less siblings come back, with none carrying it the same filter returns an empty page. `EntityDoc.ClaimState` is `omitempty` (the struct is shared with character/person/label documents, which have no claim state), so a `WorkDocInput` built without one produced a document with no such attribute and removed itself from every search; an index of them empties the whole works face silently. `BuildWorkDoc` — the only constructor of work documents — now normalises an empty `ClaimState` to `none`, which is what `model.ClaimStateKey` returns for an unclaimed work anyway. Production was never affected: `cmd/reindex-catalog` has always passed `model.ClaimStateKey(...)`, which never returns `""`, and the attribute has been in the work document since before 2026-08-09 with reindexes run since — it was the test fixtures that drifted from the production document shape, which is why the two guards are a unit assertion on `BuildWorkDoc` **and** a search over fixtures that never set the field. The rejected alternatives are recorded because each looks cheaper: dropping `omitempty` stamps `claim_state: ""` onto three unrelated document types, patching the fixtures re-creates the drift that hid this, and a positive list of the other five states silently omits the sixth the day one is added.
75. **The gate keys on what fiber matched, everywhere, from one helper** (2026-08-28). Deviation 48 fixed `catalogAuth` and `userAuth` and left six more sites reading the raw `c.Path()`: `catalog/handler/admin_gate.go` (a case variant falls to the `curation` branch), `apiv2/protocol/middleware.go` (skips the whole protocol layer), `protocol/limit.go` (skips rate limiting), `protocol/idempotency.go` (a different replay key), `middleware/rate_limit.go` (a separate limiter bucket per casing) and `protocol/headers.go` (no `Cache-Control` at all — the sixth, and the one the W1 leak actually went through). `routedPath` lived in package `handler`, which `protocol` cannot import, so the rule could not be shared and was instead missing in six places. It is now `pkg/routepath.Normalize`, a leaf package below all of them, and every site calls it. `CaseSensitive: true` (deviation 48) closes the case half at the router, but `StrictRouting` stays off — `cmd/oauth` registers `sites.Get("/")` — so the trailing-slash half is reachable today: `POST /v2/me/claims/` and `POST /v2/me/claims` routed to the same handler and took two different idempotency keys, which is one write running twice under one `Idempotency-Key`. The regression guard is a live replay through both spellings, not an assertion about the helper.

76. **Cache intent on /v2 is default-deny** (2026-08-28). `applyHeaders` stamped `private, no-store` on `/v2/me/` and `/v2/moderation/` only. That allowlist is narrower than the credentialed surface: all of `/v2/catalog/*` is application-key-gated and its bodies vary by site fence, nsfw capability and `claim_state` scope, yet declared no cache intent at all — so an intermediary was free to invent one, and one did. During the deviation-48 window Cloudflare cached `/v2/Catalog/works` and `/v2/Catalog/claim-events` 200s under a Cache Rule at `max-age=14400`, a value this origin never sets, one body carrying `actor_uid`. `Vary: Authorization` was already set and did not save us. Every /v2 response now declares an intent: the explicitly public lanes (`/v2/problems`, `/v2/vocabularies`, `/v2/catalog/schemas/*`, `/v2/catalog/openapi.json`) opt into `public, max-age=300`, and everything else is `private, no-store` because it said nothing. A third prefix was rejected as a fourth one waiting to happen. `/v2/news` is public but deliberately **not** in the opt-in set: nothing has measured its body as credential-invariant, and a wrong "public" is the failure being fixed. The rule reads `routepath.Normalize(c.Path())` (deviation 75), because a default-deny that still keys on the raw path is a default-deny with a hole in it.

77. **`/v2/me/proposals` honors `entity_type=`, `object=` and `entity_id=`, and refuses an unknown `state=`** (2026-08-28). `ListMyProposals` built `editing.ProposalFilter{ProposerUID, Limit, Status, BeforeID, Site}` by hand and set neither entity member, though `engine.go` has had both since the lane shipped and **all three consumers send them**: forum's `catalogclient/user_edit.go:229-234` sets `entity_type=catalog.work` **and** `entity_id`, moyu's `pkg/catalogv2/me.go:107-109` and letmoe's `infrastructure/catalogv2/me.go:141-143` each set `entity_id` alone. Neither parameter was declared either, so "existing proposals on this work" answered with the caller's proposals on every entity they had ever edited. The same hand-rolled block dropped an unknown `state=` on an `if ok {}` with no else and returned every state, while the public twin 400s `UNKNOWN_ENUM_VALUE` through the shared helper. Both die by routing this lane through `proposalQuery`, which the public and queue lanes already use. `entity_type=` (the editing-engine spelling `POST /v2/me/proposals` already takes in its body) and `object=` (the family spelling) name one filter; sending both with different families is 400 rather than an intersection. **`entity_id=` alone is accepted on this lane and stays 422 on the queue lanes**, deliberately: on the me lane every row is already fenced to one proposer, so an id that collides across families can at worst include the caller's own proposal, while on `/v2/moderation/proposals` naming one entity is the *authority* discriminant that admits an owner holding no review permission — an ambiguous id there is a hole, not a loose filter. `/v2/moderation/proposals` gains `entity_type=` too, because forum's single builder serves both paths and its family filter was being dropped on the queue as well.

78. **The claim and proposal decision responses carry the resulting state** (2026-08-28). `DecideClaim` returned `{object, id, decision, note}` and discarded the `From`/`To` that `ClaimActionResult` has always populated. `unban` has no fixed target — `claim_lifecycle.go` restores `priorState` — so forum hardcodes `to := "live"` (`user_claims.go:69-76`) and a moderator unbanning a claim that was banned out of `pending` is told it was published. `repr.DecisionRecord` **splits per route** into `ClaimDecisionRecord` and `ProposalDecisionRecord`, each carrying `from_state` (null when the subject had none) and `to_state`. The split is not cosmetic and is not optional: one record serving both routes can only carry the union of two disjoint state vocabularies (`none|live|draft|pending|declined|hidden` against `open|merged|declined|withdrawn`), and gate G14 refuses a string property that is in no class — the only honest class here is an enum, and a shared enum advertising the other route's values is exactly the defect D35 recorded when it split the decision *inputs* for the same reason. The old shared `decision` enum (`approve,decline,merge,ban,unban`) narrows to each route's own set as a side effect, which oasdiff does not flag: the removed values were never producible by the route that listed them. The proposal face fills the pair by **reading the proposal back** rather than deriving it from the verb, because deriving the outcome from the decision is the defect being fixed, not a shortcut worth copying.

79. **An amendment advances the proposal's validator** (2026-08-28). `editing.AmendProposal` created the `edit_proposal_amendment` row and never touched `edit_proposal`, while `proposalETag` is built from `edit_proposal.updated_at`. Reviewer A's `If-Match` therefore still matched after reviewer B amended, and the merge wrote B's `effectivePatch` while A believed they had approved their own read — the exact lost update the precondition exists to prevent (`driftedFields` only checks entity drift, not the amendment chain). The amend transaction now stamps `updated_at` with `UpdateColumn`, not `Update`, so GORM does not also auto-stamp the column it is being handed.

80. **The four proposal write operations send the ETag they declare** (2026-08-28). `createMyProposal`, `patchMyProposal`, `amendMyProposal` and `revertModeration` declared an `ETag` response header and left the field empty; huma skips empty-string headers and `protocol.applyETag` is GET/HEAD-only, so eight write operations declared the header and four never sent it. Every one of the four already holds the `*editing.Proposal` it would compute the validator from. A write that answers 201 and then requires a second GET to obtain the validator its own next call needs is a round trip the contract already promised to remove.

81. **The amendment response says what it appended** (2026-08-28). `POST /v2/me/proposals/{id}/amendments` answered a `ProposalRecord` whose `id` was the *proposal's*, with `seq` 0, `amender_uid` 0 and `note` set to the proposal's note — so a client could not learn the identity of the row it had just created. The re-read that follows the write already returns the amendment chain and was discarding it; the response now carries it in the `amendments` block, the same shape `include=amendments` publishes.

82. **`ids=` and `refs=` are refused where there is no batch read** (2026-08-28). The two parameters ride the shared `CollectionInput` onto every list operation, and four faces declared them, parsed them and had no code for them: `/v2/me/proposals`, `/v2/moderation/proposals`, `/v2/moderation/claims` and `/v2/me/news` (plus `/v2/me/claims` and `/v2/me/playtimes`, which has `work_ids=` instead). Because `q.Batch` was set, `finishList` also suppressed `next_cursor` and zeroed the limit, so `GET /v2/moderation/claims?ids=1234` answered 200 with the first 20 pending claims, no `missing[]` and no cursor. `collect.Spec` gains `NoBatch`, refused centrally in `Parse` exactly the way `sort=` is already refused when a collection declares no sort keys. Marked on the three specs that lack the lane (`ClaimSpec`, `NewsSubmissionSpec`, `PlaytimeSpec`) rather than on the many that have it: a wrong mark can then only over-refuse a named collection, never silently break the `ids=` hydration all three consumers run against the catalog lanes. No consumer sends either parameter to any of these six paths — checked against the query builders in all three repos, with the catalog lanes as the positive control that the search finds them where they exist.

83. **`fields=` is applied, once, where the ETag is computed** (2026-08-28). `collect.ApplyFields` had **zero production call sites**: `fields=` was declared on 40 operations, validated down to 400 `UNKNOWN_FIELD` on an unknown token, and then completely ignored — production returns the full 16-key work object for `works?fields=id`. It is honored in one post-response hook registered inside `protocol.Middleware`'s `c.Next()`, so the projection is already applied when `applyETag` hashes the body and the validator moves with the token set for free. `object` and `id` are always retained. The gate is **the operation's own declared parameter list**, taken from huma's document at startup, not the presence of the query string: projecting wherever `fields=` appears would honor it on the 48 operations that never declared it, `/v2/catalog/openapi.json` among them, trading an ignored parameter for an undeclared one. `fields=` absent is byte-identical to before, which is asserted rather than assumed. No consumer sends `fields=` today (positive control: they all send `include=`), so this is a promise being kept, not a behaviour change anyone is relying on.

84. **A collection that declares no facets rejects every facet** (2026-08-28). `collect.tokens()` skipped its allow-list entirely when the list was `nil` — `if allowed != nil && !contains(...)` — and `Facets` is the one `Spec` member that every specification except `WorkSpec` leaves unset. The result was measured in production: `works?facets=bogus_facet` correctly 400s while all seven entity list faces (companies, persons, characters, series, tags, traits, credit-names) answered 200 to the same token the spec promises is a 400 `UNKNOWN_FACET`. The guard is one character of logic — `nil` and empty now both mean "nothing is allowed" — and it is safe for `Include`/`Fields` because every specification sets those explicitly; the only `Spec{}` with neither is `ObjectSpec`'s `ok == false` return, which no caller reads.

85. **The search lane refuses `site=` and `platform=` instead of dropping them** (2026-08-28). `WorksSearchFilter` has no `Site` and no `Platform` member, so both parameters were accepted on `/v2/catalog/works` and silently discarded whenever the collection switched to the search index — and `facets=` alone is enough to switch it, which is how moyu and letmoe reach that lane. The answer was the unfiltered population. Neither is a filterable attribute on the works index (`WorksFilterableAttributes` in `search/index.go`), so making them work is a document-shape change plus a full reindex, not a parameter fix; until that is measured they are refused with 400 `MUTUALLY_EXCLUSIVE_PARAMETERS`, the same shape `owner_uid=` has carried on this lane since it shipped. No consumer combines either with `q=`/`facets=`/a search sort — checked in all three repos, with their `facets=` senders as the positive control. The registry lane still honours both.

86. **The three unbounded request arrays declare the shared batch bound** (2026-08-28). `POST /v2/me/playtimes` `items`, `POST /v2/me/claims` `refs` and `POST /v2/me/proposals/{id}/amendments` `unset` carried no `maxItems`, while `POST /v2/me/news` `work_ids` has declared 100 since it shipped — so the bound was an existing convention with three holes, and on the one operation whose entire contract is per-item partial failure the cap was a server surprise rather than a schema fact. Letmoe's `MintWork` sends `refs` with no chunking and forum forwards `unset` verbatim from an edit wizard; neither has a size bound of any kind. All three now declare `maxItems: 100`, and the convention is `collect.MaxBatchItems` plus a test that sweeps **every** `*InputBody` array property in the generated document, so the fourth one is caught the day it is added. Note the refusal for a request-body array is huma's 422 `VALIDATION_FAILED` with a JSON pointer, while the query-parameter batch lanes keep 400 `TOO_MANY_IDS`; that split is the API's existing one between body semantics and parameter syntax, not new drift.

87. **`If-Match` is required on the five operations that already reject its absence** (2026-08-28). Every `If-Match` header in the published document was `required: false` while `requireIfMatch` answers 428 `PRECONDITION_REQUIRED` on absence, so the document told generated SDKs the header was optional and the server refused every request that believed it. Letmoe is the live casualty: `catalogv2/me.go:155` takes an `etag` parameter its transport has no slot to send (`userDo` has no `If-Match` path) and its withdraw button is 428 on every click. The five are `patchMyClaim`, `patchMyProposal`, `amendMyProposal`, `decideModerationClaim` and `decideModerationProposal`. **`patchMyNewsItem` is deliberately not among them**: its `If-Match` is mandatory only for the withdraw transition and optional for a text edit, so `required: true` there would be a false statement — the count is five, not the six the header appears on. `spec-breaking.yml` flags all five as `request-parameter-became-required` and they are declared in `v2-openapi-breaking-ignore.txt`: this is a change to the *document*, not to the server, so no request that worked before stops working and none that failed before starts.

88. **The moderation claim queue lists every state it can decide, and its cursor carries the rows with no event** (2026-08-28). `PendingClaimsAfter` filtered `claim_state = pending` while the decision face acts on `live|draft|pending|declined` (ban) and `hidden` (unban), so **no** GET under `/v2/moderation/` could list a hidden claim and unban was reachable only by already knowing the work id. The queue now takes `claim_state=` (closed: live, draft, pending, declined, hidden) and still defaults to pending, so the widening is a parameter rather than a silent change of what the queue means. Two paging defects went with it. The watermark `(SELECT max(e.id) ... WHERE to_state = pending)` is NULL for a claim with no event in its current state, and `ORDER BY submitted_event_id ASC NULLS LAST` with a scalar `> before` cursor put those rows on page 1 and then made them unreachable — the comparison is NULL for exactly them — while the handler additionally emitted no `next_cursor` whenever a page ended on one. The key is now `(COALESCE(watermark, 9223372036854775807), work_id)` with a composite cursor, and the sentinel is inlined as literal SQL rather than bound because gorm's `Order()` understands only a string or a `clause.OrderByColumn` and **silently drops** anything else — a parameterised ORDER BY would have been no ORDER BY at all.

89. **`include_total` is the size of the collection, not of the page's remainder** (2026-08-28). `editing.ListProposalsWithTotal` and the moderation claim queue both counted a query that already carried the cursor predicate, so `total` shrank on every page of `/v2/me/proposals`, `/v2/moderation/proposals`, `/v2/catalog/proposals` and `/v2/moderation/claims` — contradicting deviation 10, which the sibling `user_claims.go` has always honoured. The count now runs on the filtered population and the cursor is added to the page query afterwards. Two findings that were *not* fixed are recorded here because both look like defects and cost a wave to disprove. **The 500 on every declared error set is huma's, not `collectionErrors`'**: `huma.Register` appends `http.StatusInternalServerError` to `op.Errors` for any operation that declares any error at all, so all 88 operations publish a 500 whatever that helper says, and removing it from the two compiled-in lanes was measured to change nothing in the generated document. **The "missing" 422 was never missing** for the same reason: huma appends it to every operation that takes a parameter or a body, which is 84 of 88 — the four without it are the four with no input parameters. Removing either would mean deleting a response huma injects, from a hand-maintained list of exceptions, which is the shape this file exists to avoid; the numbers are pinned by a test so the next reader checks them instead of the Go list.

90. **`/v2/news` declared a batch lane it never had** (2026-08-28). Deviation 82 marked `NewsSubmissionSpec`, `PlaytimeSpec` and `ClaimSpec` as `NoBatch` and missed `NewsSpec`. `ids=` and `refs=` ride the shared `CollectionInput` onto `/v2/news` too, `collect.Parse` set `q.Batch` and zeroed the limit, and `Catalog.ListNews` never reads `q.IDs` — so `GET /v2/news?ids=<a published id>` answered **200 with an empty `items[]`**, no `missing[]` and no `next_cursor`, which is worse than the page-1 answer deviation 82 was written about. `NewsSpec` is now `NoBatch`. The guard is `TestDeclaredBatchLaneIsHonouredOrRefused`, which drives `ids=<absent id>` at every one of the 26 operations declaring `ids=` and requires each to either refuse with 400 `INVALID_PARAMETER`/`parameter: ids` or answer 200 with `missing[]` present; the refusal set is a named list (10 today) so a face that stops refusing is a failure rather than a silent behaviour change. One face is unobservable in the fixture and named as such: `feedNoBatch("search")` sits behind the unbound-service 503 on `/v2/catalog/search`.

91. **`/v2/news` declared `include_total=` and never answered a total** (2026-08-28). Same face, same cause: `ListNews` built its page with `repr.NewList` instead of `finishList`, so it was the **one operation of the twenty-six declaring `include_total=` that answered no `total`** — and the forum's `newsclient.FeedQuery.values()` sets `include_total=true` on every request, so the omission was live. `news/service.FeedMeta` already computes the count over the same filter with no cursor predicate (deviation 89's rule), so the fix is one call behind `q.IncludeTotal`. The guard is `TestDeclaredIncludeTotalIsHonoured`, the sibling sweep of the one above.

92. **The contract walk sent no credential, so it only ever asserted the anonymous projection** (2026-08-28). `TestContractHitsEverySpecOperation` drove all 88 operations with no `Authorization` header; the credentialed contract had no test at all, and a gate that refused a valid key would have looked exactly like the surface working. `TestContractCredentialedArm` now walks the same app three times — anonymous, application key, user token — and requires every operation that answers 401 anonymously to open for one of the two credential kinds. It is "some credential opens it" and not "the key opens it" because an application key on `/v2/me` is a 401 by design; 78 of 88 operations are credential-gated and each opens for exactly one kind. The arm needs `liveUnlimitedStore`: three walks of mostly-401 from one httptest address blow the 120-a-minute auth-failure budget, and arms two and three came back 429 across the board — a comparison between two blocked runs.

93. **The live-read sweep was seventeen routes behind the route table** (2026-08-28). `TestLiveReads200` carried a hand-written list of concrete URLs; the document declares 71 GET operations and the list reached 54. Missing were `characters/{id}/appearances`, the whole news lane (`/v2/news`, `/v2/news/sources`, `/v2/news/{id}`, `/v2/me/news`, `/v2/me/news/{id}`), `claim-events`, `proposals` and `revisions`. The list is now templated and paired with `TestLiveReadsCoverEveryGet`, which requires every registered GET to be either swept (63) or named in `liveReadsNotSwept` with its reason (8 — four need a proposal/revision id the fixture does not mint, one needs a written playtime, and three need a service the live env does not bind). Being in both is also a failure. The substitution is longest-prefix and not `liveSubstitute`'s substring switch, which matches `/tags` inside `/v2/catalog/works/{id}/tags` and addresses a tag id as if it were a work.

94. **`embeddedCollectionPaths` checked six of forty collection faces** (2026-08-28). The one test proving collection parameters bind listed six paths by hand; the document declares 40 GETs carrying a `cursor`, so 34 — `/v2/me/news` among them — were never checked. The set is now read out of the document and classified into three shapes: 23 full `CollectionInput` faces, 14 `WorkSubInput` sub-faces, and 3 with an input of their own (`claim-events`, `proposals`, `revisions`), named with reasons. A face in none of the three fails the test. All 40 bind `limit` — the census found no new defect, which is the point of running it rather than reasoning about it.

95. **`REQUIRE_DB_TESTS` was a knob that lied, in 75 files** (2026-08-28). The flag exists so that a run which was supposed to have a database fails instead of skipping, and `dbtest.DSN()` implements it — but 75 of the 76 files reading `TEST_DATABASE_DSN` read `os.Getenv` directly and never reached it. Measured on the W2 branch: `REQUIRE_DB_TESTS=1` with no DSN printed `ok api/internal/platform/apiv2/handler 0.305s` for a wave of gate, fence, ETag and limiter tests that ran nothing. The two heaviest suites in this review, `catalog/service` and `catalog/handler`, were both in the set, so every "I ran it with the gate on" in this review rested on the DSN happening to be set. The fallback DSN is **two different defects wearing one costume**, and the difference is measurable rather than rhetorical. `origin/main` carries the literal at **62 sites**; `pg_hba_file_rules` on the development box returns `md5` for `local`, `127.0.0.1` and `::1`, so every local path authenticates by password.

- **58 sites carry `password=postgres`.** An explicit password overrides `~/.pgpass`, and under md5 a wrong one is rejected outright — measured through the same gorm/pgx driver the suites use: `failed SASL auth: FATAL: password authentication failed for user "postgres" (SQLSTATE 28P01)`. Those suites **cannot connect**, so they take the skip branch and print `ok`. Nothing ran.
- **4 sites omit the password entirely**, so `~/.pgpass` completes the DSN and they **do** connect. Same measurement, same driver: `CONNECTED, current_database()=kun_catalog_test`. They are `internal/jobs/entitylinks/db_test.go:28`, `internal/jobs/labelrelations/db_test.go:26`, `internal/jobs/orglabels/db_test.go:27` and `internal/jobs/releaselabels/releaselabels_test.go:29`. `kun_catalog_test` exists here, among 81 databases.

The credential-free shape is the more dangerous one **precisely because it succeeds**: a suite that skipped is at least visible in the runtime, while a green run that touched a database it was never assigned looks exactly like a green run that touched the right one — the iron-rule-14 breach, silent by construction. This is why `dbtest`'s header says *deliberately no default DSN* rather than *no default password*: the weaker rule would have blessed all four of these.

All 75 files now route through `dbtest`, the fallback literals are deleted, and the post-DSN refusals (unreachable server, failed migration) go through the new `dbtest.SkipMainf`/`Skipf`, which fail under the flag rather than exiting 0. Both directions are checked: nine packages across all four call shapes FAIL with the flag set and no DSN, and `go test ./...` with no DSN and no flags stays green, which is the `unit` job's contract.

**What `REQUIRE_DB_TESTS` still does not cover, by name.** The flag is now honest about `TEST_DATABASE_DSN` and about nothing else. Three families of service-gated tests remain outside it, and they are the whole of the 22 skips a full gated run reports — none of which ever came from the set swept above, because a `TestMain` that `os.Exit(0)`s emits no `--- SKIP` line at all:

- **`TEST_CATALOG_DATABASE_DSN` — 12 skips**, in `internal/jobs/catalog_image_refping_test.go` (5), `internal/jobs/labellogos/db_test.go` and `internal/jobs/personphotos/db_test.go` (7 between them).
- **`TEST_PG_DSN` — 3 skips**, in `internal/platform/auth/repository/{session_rotate,user_site_role_repository,registration_stats}_test.go`.
- **7 `"Requires mock repository implementation"` placeholders** in `internal/platform/auth/service/auth_service_test.go`, which are not service-gated at all.

The first two are the same defect shape in a different env var. They are deliberately **not** given the same treatment: `test.yml`'s integration job supplies neither service, so a hard failure would weld that job red — the 2026-08-02 outcome `dbtest` exists to prevent. What would have to be true is only that: CI provisioning a pre-migrated infra database and a catalog database, at which point routing them through `dbtest` is the same mechanical change. Until then, "`REQUIRE_DB_TESTS` is honest" means honest about one variable, not about every service a suite can need.

96. **The search suites shared one Meilisearch index prefix** (2026-08-28). `catalog/search` used `test_` and `catalog/service` used `test_svc_` against the one local instance, so two sessions running either suite at once shared every index and each test's cleanup `DeleteIndex` pulled the other run's corpus out mid-assertion. W4 made this load-bearing by removing the default host: a session that sets `MEILISEARCH_TEST_HOST` now inherits the shared prefix with it. `dbtest.SearchIndexPrefix` names indexes per process and `dbtest.SweepSearchIndexes` deletes them at the end of the run, because unique names without a sweep only trade the collision for a pile of dead indexes. Verified both ways: two concurrent copies of the search suite both pass with the per-process prefix and both fail with the prefix pinned back to `test_`, the failure naming `Index 'test_catalog_works' not found`.

97. **The `huma/v2 >= v2.39.0` floor in `CLAUDE.md` was never true** (2026-08-28). The note said a reader who "simplifies" the dependency back will break SSE. `go.mod` has pinned **v2.38.0 since the day huma was introduced** (d3fad0e9, 2026-06-22) and has never held another version; the module imports `huma/v2`, `adapters/humafiber` and `humatest` and **no** `huma/v2/sse` (positive control: `github.com/gofiber/fiber/v3` matches 123 times with the same command). The line entered in b53b39f4, a commit about *how to write comments*, as an illustration of the "invisible constraint" shape beside `merge_rehang.go`'s exclusions — which is a real example and still exists. The doc was the bug, not the pin: the example is replaced with a constraint this repo actually carries, huma dropping the query parameters of an unexported embedded input struct, which is why `CollectionInput` and `WorkSubInput` are exported and which has already cost this API two rounds of silently unbound routes (deviations 19 and 34).

98. **`/v2/news` declares and honours the four filters it was silently dropping** (2026-08-28). `Catalog.ListNews` passed a hard-coded empty `newssvc.FeedFilter{}` while the service's `FeedFilter` has always supported `Sources`, `Lanes`, `PublishedAfter` and `PublishedBefore`. The forum sends all four — `newsclient.FeedQuery.values()` sets `lane`, `source`, `published_after` and `published_before`, populated from user input by `internal/news/handler/news_handler.go` and the month/archive services — and because none was a **declared** parameter, `SkipValidateParams` dropped them without a word. Accept-and-ignore on a live public face with a known caller is the defect class this whole review exists to remove, so the four are now declared on `listNews` and wired through the existing filter. **This changes what the forum's 情報 page shows**, from the unfiltered feed to the filtered one it has been asking for since the face shipped: its lane tabs, its per-source views and its month archive have all been rendering the same unfiltered list, and they will start differing on the deploy that carries this. The change is the consumer's own expressed intent, not a new opinion about what that page should contain.

    Shapes follow what v2 already does rather than inventing new ones. `lane=` is the closed `news,column` vocabulary through the same `closedCSV` the works face uses for `content_limit=`, so an unknown lane is `400 UNKNOWN_ENUM_VALUE`. `source=` is comma-separated and **open** — its vocabulary is the `news_source` table, so an unknown key matches nothing rather than failing, exactly like `site=` on works — bounded at 20 keys because an open vocabulary with no bound is an unbounded `IN` list. `published_after=`/`published_before=` are RFC 3339 UTC instants via the already-tested `parse.DateTimeUTC`, **not** the `YYYY-MM-DD` of `released_after=`: `published_at` is a `timestamptz` the caller windows to the minute, and the one live caller already sends `time.RFC3339Nano`, which that parser accepts and which a date-only parameter would have rejected outright — turning a silent drop into a 400 for every request the forum makes. An inverted window is refused rather than served empty. All four are optional and additive; `oasdiff` reports no breaking change.

    The guard is `TestLiveNewsFeedFilters`, and its control is the half that matters: every arm asserts both what the filter **keeps** and what it drops, and `include_total=` over a narrowed population is asserted to be exactly 2 against a strictly larger unfiltered total. An empty result set is not evidence a filter ran — a filter narrowed to nothing would pass a drop-only test.

99. **The two proposal list lanes validate `fields=` against the proposal vocabulary** (2026-08-28). `/v2/me/proposals` and `/v2/moderation/proposals` parsed with `collect.ClaimSpec`, so `fields=` was the claim record's key set on a face whose items are proposals — bidirectionally wrong: `note`, `entity_id`, `entity_type`, `proposer_uid` and `decided_at` were `400 UNKNOWN_FIELD`, while `acted_count`, `product_work_id`, `last_event` and `display_name` were accepted and then projected away to nothing, which is deviation 83's silent-projection failure with the vocabularies swapped.

    Neither existing sibling fits, which is why this took a third spec rather than a one-word swap. `PublicProposalSpec` is the public transparency face and publishes no `patch`. `ProposalSpec` is the **detail** spec — `proposalInclude` uses it for `GET /v2/{me,moderation}/proposals/{id}` — and advertises `include=patch,amendments`, which these list lanes build with `proposalFrom` and never populate; adopting it would have traded one accept-and-ignore for two. It is also not `NoBatch`, so it would have opened a batch lane neither lane implements. `collect.ProposalListSpec` is therefore the proposal field vocabulary, an empty include set, the single `filed_desc` sort the shared `proposalQuery` builder actually applies, and `NoBatch` — so the refusal set the batch guard names is byte-identical before and after, which is asserted rather than assumed. No consumer sends `fields=` to these faces (positive control: the consumers that do send `fields=` send it to `/v2/catalog/works`, and every `fields`/`Fields` hit on the proposal faces is a response-body key, not a query parameter).

    **Left alone deliberately: `internal/platform/trust/**` is not gofmt-clean on `main`** — nine files, all pre-existing (verified against `HEAD`), all unrelated alignment. No gate runs gofmt, so this is invisible to CI; sweeping nine files in another track's area risks a conflict for no gain, and a whole-tree `gofmt -w` inside a test wave is exactly the unreviewable diff this file exists to prevent.

100. **The works list declared the detail face's whole include vocabulary and hydrated four blocks** (2026-08-28). `collect.WorkSpec()` was parsed by both `GET /v2/catalog/works` and `GET /v2/catalog/works/{id}`, so the list validated all eighteen tokens and `workFromListItem` answered four (`intros`, `companies`, `ratings`, `refs`). `include=tags,credits` was a **200 with neither block and no error**; ten tokens — `relations`, `releases`, `popularity`, `playtimes`, `series`, `platforms`, `screenshots`, `characters`, `engines`, `links` — did nothing at all; and `view=full`, which unions a twelve-token FULL_SET, emitted the same four. The published document said only "Unknown token is 400 UNKNOWN_INCLUDE", whose natural reading is that a known token is honoured, and deviation 10 lived in this file. A downstream consumer probed for a 400, got none, read this file, and shipped a per-work detail read behind concurrency 8 and a six-hour cache on its busiest page to recover what the list had accepted an ask for — an N+1 on the hottest page in the product, caused by the contract rather than by their code.

    The fix is split by what each token actually costs. `tags` and `credits` are **hydrated**: `loadWorkTags` was already batch-shaped (a subject slice in, a map out, two queries regardless of page size) and needed only an arm, and `WorkCredits` is widened into `workCreditsFor(ctx, []int64)` with the single-work path calling it with one id — a second batch copy would have grown a second implementation of the D9 identity projection and the D24/roster suppression predicate, which is the "third code copy" trap this repo has paid for before. Measured on `kun_catalog` before writing either: tags per work average 14.8 at the default spoiler ceiling, p99 88, max 261; credits average 6.0, p99 64, max 340 — so a 24-row browse page costs roughly 350 tag rows and 150 credit rows, and no truncation is needed or added. The other ten tokens are **refused**: `collect.WorkListSpec()` gives the collection its own `Include`/`FullSet`, so they answer the same `400 UNKNOWN_INCLUDE` any unknown token gets, through the existing check, and `view=full` on a list now means the full set that lane can serve.

    `titles` and `covers` stay narrower on a collection lane, and that is the contract rather than a remaining gap: `titles` elects `latin`/`localized` and `covers` elects the two cover slots — which is what puts the `sexual` grading on the base `cover`, load-bearing for letmoe's SFW gate. Both are on live consumer hot paths in exactly that shape (`WorkLoader.Brief` sends `include=titles,covers` on a 100-id batch; patch sends `titles,covers,refs,companies`; the forum sends `titles,covers,companies`), so emitting the detail face's full `titles[]` and `covers[]` arrays there would be a payload regression on every one of them, not a fix. The unbounded per-work galleries keep their sub-resources.

    `credits` is in `WorkListInclude` but **not** in `WorkListFullSet`, for the same reason it is not in `WorkFullSet` on the detail face: it is an explicit ask, not part of "everything".

    **What changes for a caller.** Additive: `include=tags` and `include=credits` now answer blocks, and `view=full` additionally carries `tags`. Refusing: the ten detail-only tokens 400 instead of returning 200 and nothing. No data a working request received before stops arriving — a token that produced nothing now says so. The three sibling repos were checked before the narrowing rather than after: the forum's list constants are `names,covers,labels` and `names,covers,refs` (renamed to `titles,covers,companies` / `titles,covers,refs` in `v2CatalogQuery`), patch sends `titles,covers,refs,companies`, letmoe sends `titles,covers` and `refs,covers`, and the forum's eighteen-token string is `v2CatalogDetailInclude["works"]`, which `v2DetailEntity` applies only to a two-segment path. None of them sends a refused token to a collection.

    The guards are `TestLiveWorksListHydratesTagsAndCredits` (list, `ids=` batch and detail must publish byte-identical blocks, and the blocks must be absent when not asked for), `TestLiveWorksListCreditsMatchTheSubResource`, `TestLiveWorksListRefusesDetailOnlyIncludes` (each of the ten is refused on both collections and still accepted on the detail face) and `TestLiveWorksListViewFullIsTheListFullSet`. At the service layer `TestWorksListCreditsAreTheDetailRowsBatched` pins that batching changes nothing but the round-trip count, including that one work's suppressed row cannot land in another's slice.

101. **The calendar had its own include mapper, and it never learned `titles`** (2026-08-28). `/v2/catalog/calendar` shares `workFromListItem` and `enrichWorkListItems` with the works list but translated tokens through `calendarWorksInclude`, which handled `companies`, `intros`, `ratings`, `covers` and `refs` and silently dropped `titles` — so `include=titles` on the calendar answered 200 with no `latin` and no `localized{}`, the two fields every card renders a Chinese name from, while the same token on the works list filled them. Two mappers for one shape is the defect; the calendar now calls `listWorksInclude` and `CalendarSpec` carries the same `WorkListInclude`/`WorkListFullSet` as the works list. The guard is `TestLiveCalendarHonoursTheListVocabulary`, which compares the calendar against the works list token by token over `WorkListInclude` rather than against fixture literals — one work must read the same through either collection, and both reads are taken together so an unrelated edit cannot decide it.

102. **`GET /v2/catalog/schemas/{object}` published only the detail vocabulary** (2026-08-28). The schema face is where a machine consumer discovers `include=`, and it answered `collect.ObjectSpec(object)` for both halves — which for `work` is now the detail face's eighteen tokens. Left alone, deviation 100 would have made this face hand a generator the tokens the collection refuses. `list_include` and `list_full_set` are added beside `include`/`full_set`; for every family but `work` the two pairs are equal, because those lists and details share a Spec, and `TestLiveWorkSchemaPublishesBothVocabularies` asserts both the equality and the one inequality.

103. **Listing `total` values are served through a 60-second in-process cache** (2026-09-01). Works list, labels/tags/engines/series, and the entity list faces all go through `taxonomyTotal`. That helper now caches by the full filter signature (table + WHERE clauses + bound args) for 60s, so `total` may lag writes by up to 60 seconds. Cursor predicates stay outside the key: the pre-existing "total counted before the cursor" property is preserved and strengthened — the total is now also stable across pages within the TTL.

104. **`GET /v2/store/prices/{id}` and `GET /v2/store/prices?ids=`** (2026-09-01). Storefront price quotes live in the store domain (`store_price_quotes` in the infra database), not the catalog registry. The catalog side only answers which exact live release-level DLsite/Steam anchors a visible galgame work has. Quotes are cached observations: a cold miss is fetched lazily, batched and coalesced per (storefront, region); the single-work face waits up to 1.5s, the batch face never waits. TTL is 6h, capped by a published sale end when that is sooner; a not-found row lasts 24h; a transport error writes `unavailable` for 15m so a down storefront is not hammered; a background loop refreshes rows requested in the last 7 days. The face is keyless (`v2Security` returns empty before the `/v2/store/` arm) and does not opt into `isPublicPath`, so Cache-Control stays the v2 default `private, no-store`. Visibility is the work-sub predicate minus nsfw: prices are content-neutral numbers, so an r18 work's quotes are served to everyone. Money is stored as int64 minor units (×100, including JPY) and published as a two-fraction decimal string (`amount`) plus ISO 4217 `currency`. FANZA/DMM and Getchu are out of this wave: the platform holds no FANZA affiliate credentials, and Getchu is HTML-only; anchors of a source without a fetcher are dropped and produce no quote row.

105. **Getchu joins the price face** (2026-09-05). Third storefront lane beside DLsite and Steam: `internal/platform/store/price/getchu.go` scrapes `https://www.getchu.com/item/{id}/` (the old `soft.phtml?id=` URLs 301 there), decoding EUC-JP and reading the cart block's tax-inclusive price plus the `定価` row's `税込` amount; discount is derived from the two, since Getchu publishes no rate. The page is served only with the `getchu_adalt_flag=getchu.com` cookie — a cookieless request is 302 → the age-gate attestation page, so the fetcher refuses redirects and turns them into loud fetch errors instead of parsing the gate page into `unavailable` rows. There is no batch endpoint: `Batch()=1` with a 2s gap makes the lane one page every two seconds, serial per the shared batcher. JPY only, `converted` stays empty (the Steam shape), region is `jp`. A dead item id is a clean 404 → not-found negative cache, which is common here: most of the ~14.9k release-level Getchu anchors are out-of-print titles whose cart says `ご注文の受付は停止中です` → `unavailable`. The anchor query in `public_store_anchors.go` now includes `'getchu'`; no config, no migration, no relay (Getchu serves the prod egress directly).

106. **Favorite folders become a catalog face** (2026-09-06). `/v2/me/folders` is nine operations — list / create / get / patch / delete on the folder, and list / batch-add / put / delete on its items — over two new tables, `catalog_user_folder` and `catalog_user_folder_item`. It is modelled on `/v2/me/playtimes` and not on anything in the forum: the forum and moyu each keep their own favorites table, and the point of putting the canonical store here is that a work id means the same thing in both. Caps are **200 folders per user** and **10,000 items per folder** (`model.FoldersPerUserMax` / `FolderItemsMax`), refused as 422 rather than silently truncated. `is_default` is single-holder per user and moves rather than toggles: setting it on one folder clears it on the previous holder, `is_default: false` is 422 (`ErrFolderDefaultOnly`), and the default folder cannot be deleted until the flag is moved — a client that could clear the flag could leave a user with no default and no way to name one. `visibility: public` is stored intent only; there is no public browsing face, and adding one later is additive. A work that is not `live` cannot be added (`ErrFolderWorkUnavailable` → 404), so a quarantined or absent id never becomes a membership row. **Spec is 2.12.0**, additive: 92 → 101 operations, three new schemas (`folder`, `folder_item`, the batch item). **This wave needs a migration**: `go run ./cmd/migrate catalog` against `KUN_CATALOG_PG_DATABASE` creates the two tables; the routes answer 503 `SERVICE_UNAVAILABLE` while `Catalog.Folders` is unbound, never a 500.

107. **The folder operations demand a scope, and the rest of `/v2/me` still does not** (2026-09-06). Every other `/v2/me` face gates on the person alone: any application the user has consented to may read their playtimes, because "the app is here with this person's token" was judged sufficient for data the person publishes anyway. Folders are not that — a folder is a private list, and "any consented app may call `/v2/me`" would hand the whole collection to every app the user ever signed into. `userAuth` therefore checks `folder:read` / `folder:write` (`folderScopeProblem`) for `/v2/me/folders` and everything under it, and for nothing else; GET and HEAD accept **either** scope, so a manager that only asked for write consent can still read back what it wrote, and every other method requires `folder:write`. Both join `selfServiceUserScopes`, so a self-service application can ask for them at consent. The gate reads `routepath.Normalize(c.Path())` for the same reason deviation 75 does — a gate keyed on the raw path is a gate with a hole in it. The refusal is 403 `SCOPE_REQUIRED` naming the missing scope, which is the only place on `/v2/me` that code appears.

108. **Re-adding a work to a folder does not touch `updated_at`** (2026-09-06). `PUT /v2/me/folders/{id}/items/{work_id}` is idempotent in the ordinary sense — the second call answers the stored row and creates nothing — and it is also idempotent on the **watermark**, which is the part that had to be decided rather than inherited. `updated_at` on a membership is the incremental-sync cursor (an `RFC3339Nano|work_id` keyset over `updated_at`, wrapped opaque per deviation 111), so a client that re-uploads its whole library on every launch — which is exactly what a desktop manager does — would, if the row were touched, move every membership to now and force every *other* client of that user to re-download the entire folder on its next pull. The upsert is `ON CONFLICT DO NOTHING` and the no-op branch re-reads the row instead of writing it; only a genuinely new membership bumps the folder's own `updated_at` and `item_count`. Deleting an absent membership is the same shape: 204, no row touched, no count moved.

109. **Folder memberships get their own merge-rehang statements, and these ones bump the cursor** (2026-09-06). The generic rehang in `workFacetStmts` repoints a facet's `work_id` from the retired id to the survivor; for `catalog_user_folder_item` that is wrong in three ways at once, so it takes three named statements instead. First, the unique key is `(folder_id, work_id)`, so a folder holding **both** sides of a merge would collide — the repoint is guarded by `NOT EXISTS` on the survivor in the same folder and the leftovers are deleted, per folder, so a user who had filed the duplicate and the original in one folder ends with one row and not an error. Second, the repoint sets `updated_at = now()` deliberately, against the invariant deviation 108 defends: a merge is precisely the case where every synced client **must** re-deliver the row, because the id it holds no longer resolves, and a membership that moved silently would leave every manager pointing at a retired work forever. Third, `item_count` is a stored counter and both of the first two statements move it, so every folder holding the survivor afterwards is recounted from `catalog_user_folder_item` rather than incremented — the merge is the one path where the delta is not knowable per folder. `merge_rehang.go`'s standing assertion is that every table referencing a merge-able entity is accounted for there by a statement or by a named exclusion; these rows are accounted for by the statements above, not by an exclusion.

110. **Two routes with an anonymous `Items []struct{…}` panic huma's schema registry at startup** (2026-09-06). `POST /v2/me/playtimes` declares its batch body as an inline anonymous struct slice, and writing `POST /v2/me/folders/{id}/items` the same way took the whole binary down at registration with `duplicate name: Item` — huma names an anonymous slice element after the field that carries it, so the second `Items` asks the registry to bind a name it already holds to a different shape. It is a startup panic, not a request-time error, so it fails every route at once and looks nothing like the two lines that caused it. The folder batch entry is the named type `folderBatchEntry`; the playtime one is left as it is, because renaming it would rename `PlaytimeBatchItem`'s request schema on a published spec for no gain. Any third batch face on `/v2/me` must name its element type.

111. **The `cur_` envelope goes on once, in the collection layer — 2.12.0 shipped the folder lanes putting it on twice** (2026-09-06; this entry replaces the one published with 2.12.0, which was wrong in both directions). `repr.List` publishes `pattern: ^cur_[A-Za-z0-9._~-]+$` on every `next_cursor`, and exactly two places apply it for the whole surface: `collect.Parse` strips it off the request, so a handler's `q.Cursor` is the **bare, already-decoded** keyset key, and `finishList` puts it back on the response. Every lane therefore reads and writes bare keys — `strconv.Atoi(q.Cursor)` for the search page number, `claimEventCursor`, `decodeRedirectInner`, the playtime lane's `RFC3339Nano|work_id`. The folder lanes were reviewed handler-locally, where a bare key assigned to `next` reads as a raw cursor going out, and were "fixed" before release by calling `collect.EncodeCursor` / `DecodeCursor` a second time. Decode-twice matches encode-twice, so it round-trips: `TestFolderCursorsAreOpaque` crawled green at `limit=1`, the `cur_` prefix was there, and only the bytes were wrong — `cur_` + base64 of `cur_` + base64 of the key. **The same review recorded that `/v2/me/playtimes` ships its key raw against the published pattern, and scheduled a wave to heal it. That was never true**: `finishList` has always wrapped it, and `TestLiveMyPlaytimesIncludeTotal` now asserts the emitted shape as the positive control the claim never had. 2.13.0 drops the second wrap; the folder lanes match their siblings and needed no legacy-cursor path, the face being two hours old with zero rows in production. The assertion that separates the two states is not the prefix — a double-wrapped cursor has it — but opening the payload: `requireOpaqueCursor` base64-decodes it and fails if the key is itself a cursor.

112. **A folder's `name` may be empty, and only the backfill can make one** (2026-09-06). `folder.name` carries `maxLength: 100` and deliberately no `minLength`, while `UserFolderService.Create` and `Patch` both refuse a blank name (`ErrFolderNameRequired`). That is not an oversight in either direction: a person naming a folder must name it, and the **default** folder was never named by anyone. The forum stores its default collections with `name = ''` — 6,656 of 6,879 — and renders the label from the owner's name at read time (`collectionDisplayName` → `${owner}的收藏夹`), so a name invented at import would freeze one viewer's language into every client's view of somebody else's folder. `cmd/import-favorites` therefore writes the empty name through, and clients must treat `name: ""` on an `is_default` folder as "unnamed, derive a label", not as a missing field. 10,230 of the 10,451 default folders the backfill produces are unnamed.

113. **The favorites backfill is re-runnable, and `catalog_user_folder_import` is what makes it so** (2026-09-06). `cmd/import-favorites` copies the forum's 7,598 collections (183,105 memberships), the forum's flat `galgame_favorite` table (129,735 rows, 98.8% of them already folded into collections by the forum's own July migration) and moyu's 59,890 flat rows into the folder tables. Re-running it must reuse folders rather than mint second copies, and `(owner_uid, name)` is not a key — three forum users hold two collections under one name — so each imported collection files a `catalog_user_folder_import` row keyed `(site, source_id)`. Four rules were adjudicated rather than inherited: (a) memberships keep the **source** `created_at`, and a work favourited on both sites keeps the earlier one and the later `updated_at`, because that column is the sync watermark of deviation 108 and must not travel backwards; (b) a flat favorite of a work its owner has already filed **anywhere** merges into the folder that holds it instead of appearing a second time in the default folder — the rule the forum used for its own fold, applied to both flat lanes so they cannot disagree; (c) a merged work is followed through `catalog_redirect` to its survivor, never skipped; (d) `moyu` ids resolve through the **exact** (`link_kind = 0`) vndb anchor only, since a probable anchor that lands a favorite on the wrong work is worse than one that does not migrate — 111 of moyu's 8,038 ids do not resolve (75 `pending-*` placeholders carrying 442 rows, 32 `wiki-<id>` which are catalog ids with a prefix and do migrate, 4 v-numbers with no anchor). Nothing in the schema keeps a user to one default folder, so the collection lane **adopts** a default a flat lane created earlier rather than landing a second one beside it. Rehearsed end to end against production snapshots: 11,172 folders / 243,543 memberships / 10,453 users, zero rows stamped at import time, `item_count` exact, one default per user, and multi-folder memberships identical to the source (2,214 pairs) — the import adds none of its own.

114. **The favorite count is a popularity row, and it is computed on read** (2026-09-07). `include=popularity` on a work now carries `{source: "nextmoe", metric: "favorites", value: N}` alongside the stored bangumi and dlsite rows, where `N` is the number of **distinct users** holding that work in any folder — a person who files the same work in three folders is one favorite, and the forum's `galgame.favorite_count`, which this replaces, counted the same way (`COUNT(DISTINCT user_id)`). Three things were decided rather than inherited. **It is not rolled up.** `catalog_user_folder_item` is the canonical favorites store, so a `catalog_work_popularity` row for it would be a second answer to a question the read path can already answer exactly; the playtime rollup in `jobs/userplaytime` exists because a median over per-user rows is genuinely a different computation, and a count is not. If this ever has to back `sort=popularity` it needs that treatment, and until then a stored copy buys only drift. **Private folders count.** The value is an aggregate over people and names none of them, excluding private folders would drop most of the imported corpus, and neither source table this replaces had a per-favorite visibility to honour. **It is emitted even when it is zero**, which no other popularity row is: every stored row is absent when its source publishes nothing, so absence there means unknown, while this count is always known and 0 means nobody. A client rendering "N people" would otherwise have to guess which absence it was looking at. **Spec is 2.14.0**, additive: no new operations, no new schemas, one new value in an open vocabulary. **Zero migrations.**

115. **Reading somebody else's folders is a new top-level family, and it is not `folder:read`** (2026-09-07). Deviation 106 shipped `visibility: public` as stored intent with no face to read it; `/v2/folders` is that face — `GET /v2/folders?owner_uid=`, `GET /v2/folders/{id}`, `GET /v2/folders/{id}/items`. Four things were decided. **The scope is `catalog:read`, not `folder:read`.** `folder:read` is the consent a person gives an application to read *their own* private lists (deviation 107); it says nothing about what its holder may see of other people, and requiring it here would mean an app needed a private-data consent to render a public page. **`owner_uid` is required**, so there is no platform-wide folder directory: a folder name is user-authored text on a public face, and an enumerable index of everyone's would be a new moderation surface nobody asked for — today you see someone's folders by going to their profile, and that is what this face reproduces. **The lane never widens for the owner.** A private folder is 404 here even to the person who owns it; `/v2/me/folders` is the face that knows about private folders, and a public URL whose bytes depend on who asked is a caching bug waiting to happen. It is 404 rather than 403 for the usual reason — a distinct refusal turns the id space into an oracle. **Items carry no editorial gate**, so `item_count` matches what the list returns; a caller that hides r18 applies its own gate when it hydrates the works, exactly as the forum already does over its own store. `folder.owner_uid` is new on the representation because a by-id fetch has to say whose folder it is — the same field the list face takes as its filter. **Spec is 2.15.0**, additive. **Zero migrations.**

116. **`contains_work_id` exists because the picker was reading 200 folders to answer one question** (2026-09-07). `GET /v2/me/folders?contains_work_id=` keeps only the folders that already hold that work. The add-to-folder picker asks exactly this on every work page, and without the filter a client had to page its owner's whole folder list — up to the 200-folder cap — and probe each one. It is one indexed join on `catalog_user_folder_item(work_id)`. It also gives the forum's `GET /galgame/{gid}/collections/mine` a one-call equivalent, which is what makes the collection cutover a like-for-like swap rather than a fan-out.

117. **Folders get a moderation face, because the cutover would otherwise drop one** (2026-09-07). `PATCH` and `DELETE /v2/moderation/folders/{id}` let a moderator rewrite or blank a folder's name and description, force it private, or delete it outright. This is not new capability: the forum has shipped `collection.edit_any` / `collection.delete_any` on its own collections all along, and moving the canonical store to the catalog without these two operations would have quietly retired a live moderator power over user-authored text that is now on a public face. Standing is `catalogPerm.Moderates` — the same union the rest of `/v2/moderation` uses. Two asymmetries against the owner's own operations are deliberate. A moderator **may blank the name**, which `UserFolderService.Create`/`Patch` refuse (`ErrFolderNameRequired`): the abuse being removed is usually the name itself, and choosing a replacement is the owner's to do — deviation 112 already has clients deriving a label for an unnamed folder. A moderator **may delete the default folder**, which the owner cannot: `is_default` is set by the folder's owner, so honouring it here would let anyone make a folder undeletable by marking it default. What a moderator may not touch is the folder's items — moderation acts on what a folder publishes, not on what somebody collected.

## Stage 6 write

| Route | Bind |
|---|---|
| `POST /v2/me/playtimes` | 207 list of playtime-or-problem items |
| `POST /v2/me/claims` | `work_id` → `Act(claim)`; else `refs` → `LookupEntityID` then claim or mint; else `site_work_id` and/or `field_values` → mint. Every mint is one `SubmitWork` call, `Trusted` from `catalog.edit.trusted` (wave R4). A mint whose titles collide with live same-medium works is refused 409 `DUPLICATE_SUSPECTS` unless `confirm_duplicates` is set; the claiming lanes never reach that gate |
| `GET/PATCH /v2/me/claims/{id}` | `{id}` is catalog work id. PATCH `{state: live\|pending\|withdrawn}` + If-Match, one `Act` call each (publish / submit / withdraw) |
| `POST /v2/me/edit-images` | multipart `preset` + `file`; `cover`/`screenshot` map to the image service's `catalog_cover`/`catalog_screenshot` |
| `GET/POST /v2/me/proposals` | `editing.Engine` |
| `GET/PATCH /v2/me/proposals/{id}` | PATCH withdraw or amend; If-Match |
| `POST /v2/me/proposals/{id}/amendments` | `AmendProposal` + If-Match |
| `GET /v2/moderation/claims/{id}` | Site-fenced `ClaimByWorkID` |
| `POST /v2/moderation/claims/{id}/decisions` | approve/decline/ban/unban + If-Match; requires `catalog.claim.review` |
| `GET /v2/moderation/proposals` | open proposals, `SITE_NOT_BOUND` without a catalog site |
| `GET /v2/moderation/proposals/{id}` | site-fenced; `include=patch` adds `patch` + `effective_patch`; ETag feeds its own decision |
| `POST /v2/moderation/proposals/{id}/decisions` | merge/decline + If-Match |
| `GET /v2/catalog/revisions` `/{id}` | public, application key; `sort=recorded_asc` is the watermark shape; `include=diff` + `diff_base=` |
| `GET /v2/catalog/proposals` `/{id}` | public, application key; no `patch`, no `decision_note`; `include=amendments` |
| `POST /v2/moderation/reverts` | `revision_id` loads `edit_revision` then `Revert` |
| `GET /v2/moderation/snapshots/{object}/{id}` | `CurrentSnapshot` |
| `GET /v2/me/news` | Items under `news_source.publisher_uid = caller`, pending included. Keyset on `news_item.id` DESC |
| `POST /v2/me/news` | `SubmissionService.Create` → always `status = pending`. 201 + `Location` + full body + `ETag`. `Idempotency-Key` via the shared POST middleware |
| `GET /v2/me/news/{id}` | `{id}` is the news item id; carries the `If-Match` ETag |
| `PATCH /v2/me/news/{id}` | Pending: edits `title`/`summary`/`source_url`/`banner_hash`/`work_ids`. Published: only `{"status":"withdrawn"}`, `If-Match` mandatory (428 without) |

`content_limit` on claim POST is not stored (no column on the claim row). Omit it.

News write specifics:

- **Zero schema change.** `upstream_category` and `banner_origin_url` are importer-only and are written as `''`; `news_item_work.confidence` is `0` (manual).
- **Re-moderation is inherited, not rebuilt.** `newsmoderate.Runner` selects `status = pending`, recomputes `NewsItem.Fingerprint()` (title + preview + lane) per row, and re-scores whenever no settled verdict exists for that exact digest. A pending text edit therefore re-enters the machine queue with no new code; a `source_url` / `banner_hash` / `work_ids` edit correctly does not.
- **`banner_hash` is a format check only** (`^[0-9a-f]{64}$`, the image service's sha256 hex). No call to the image service, so a syntactically valid hash for bytes that do not exist is accepted and renders as a broken banner in the moderation queue.
- **Refping tolerates native rows.** `news/imagerefs` collects `banner_hash <> ''` and never reads `banner_origin_url`, so an API-submitted banner with an empty origin is inside the daily sweep. ⚠️ The sweep authenticates as the **news** image client and is site-scoped: bytes a publisher uploaded under a different site's client will `not_found` on every ping and rot at the image service's TTL.

## Stage 7–8 (this repo)

- **MCP** — `cmd/mcp` loads `GET /v2/catalog/openapi.json` (or `KUN_MCP_OPENAPI_PATH`) and registers one read-only tool per GET on `/v2/catalog`, `/v2/news`, `/v2/problems`, `/v2/vocabularies`. Tool name = operationId. `nmk_` keys only. G10 compares tool params ⊇ HTTP query/path params.
- **SDK check** — `internal/platform/apiv2/sdk` Go client compiles and hits problems/vocabularies/works. TypeScript twin is the portal docs-model + explore relay.
- **Portal** — `apps/developer` documents the v2 face from `docs/catalog/v2-openapi.yaml`, HTML problem pages at `/problems/{domain}/{kebab}`, vocabularies, design principles. Explore and landing relay `/v2`.
- **Keys** — `/v2` rejects `nm_live_` / `nm_test_`. Mint and rotate issue `nmk_live_` / `nmk_test_` with CRC32 for every tier (GA); rotation upgrades a v1 key to `nmk_`. v1 still accepts both generations (malformed `nmk_` rejected offline).
- **Edge** — `docker-compose.prod.yml` routes `Host(api.nextmoe.dev) && PathPrefix(/v2)` to catalog. Landing this branch on `main` deploys that router with the catalog image. The developer portal and MCP compose projects stay manual.

## Stage 9–10

- **9 GA — declared 2026-08-25.** Landed with it: `nmk_` minting opened to every dev tier (rotation of a v1 key returns `nmk_`), the portal preview banner and every preview qualifier dropped, `info.version 2.0.0` / `x-stability: stable`, and `docs/catalog/v2-openapi.yaml` added to the `specs=` list in `spec-breaking.yml` (G12). That last one is the whole gate: the file was already in the workflow's trigger `paths` but not in `specs=`, so oasdiff had never actually diffed it — a v2 spec that looked checked and was not. Default masks and the existing error-code registry entries are frozen from this date. Still open: notify the 17 owners.
- **10 v1 410** — Deprecation/Sunset counted from GA (2026-08-25), brownout, then `/v1/**` 410.

## Tests that exist

- Spec-driven: `TestContractHitsEverySpecOperation` hits every registered op; undeclared status fails.
- Gates G2–G16 on the generated OpenAPI document.
- Per-bind unit tests for mapping, unbound 503, auth 401, NSFW, cursor, schema `field_type`.
- Live DB: `handler/live_*_test.go` (requires track `TEST_DATABASE_DSN`). One 200 GET per bound read, write happy paths, then a spec walk against the live app.
- News write: `handler/live_news_test.go` over HTTP (source grant 403/422, POST→pending with `Location`/`ETag`/`no-store`, field-level 422, `Idempotency-Key` replay, pending edit, 428/412/200 on withdrawal, rejected terminal, cross-publisher 404) and `news/service/submission_test.go` at the service layer, which drives the **real** `newsmoderate.Runner` to prove a text edit re-enters the machine queue and a metadata edit does not.

Search 200 paths need OpenSearch — the live suites bind it via `KUN_OPENSEARCH_TEST_HOST` (OpenSearch is the search engine since 2026-09-01; Meilisearch retired 2026-09-02); with it unbound those ops return declared 503. The news tables are created in the same track database by `newsmigrate.Run`, so the news read and write faces are bound in the live tests.

When a test database is assigned, run:

`GOMAXPROCS=8 go test -count=1 -p 1 ./internal/platform/apiv2/... ./internal/platform/news/...`

A DB-backed package that "passes" in well under a second ran nothing: read the `-v` PASS counts, not the `ok` line.

## Wave R1 — v2 fills for the v1 retirement (2026-08-27)

Four new operations, and the last v1-only reads that had no v2 answer are gone.

| Operation | Notes |
| --- | --- |
| `GET /v2/catalog/claim-events` | The claim lifecycle log. `sort=recorded_asc` is the watermark walk a mirror or a reward cron reads; `site=`/`actor_uid=`/`work_id=` narrow it, `ids=` is the batch lane. Needs `claim_events:read` **on top of** `catalog:read` |
| `DELETE /v2/me/claims/{id}` | Deletes a draft the caller owns. A live or pending claim must be withdrawn to draft first (`PATCH state=withdrawn`). Soft-deletes the `catalog_work` row and writes **no** claim event, matching v1's executor. Keyed on owner uid, not site |
| `GET /v2/store/purchase-links/{product_id}` | v1 `/v1/store/purchase-links/:product_id` with RFC 9457 problems and no envelope |
| `GET /v2/store/stats` | v1 `/v1/store/me/stats`. The bearer *application* is the subject, so the path drops the `me/` segment that only made sense next to a user face |
| `GET /v2/store/prices/{id}` | Keyless cached storefront quotes for one catalog work. 404 if the work is not a live galgame; 503 if the price service is off. R18 prices are not fenced |
| `GET /v2/store/prices` | Batch `ids=` lane, at most 100. Unknown ids go to `missing[]`; never 404. Does not wait on a cold miss |

`claim_events:read` is **operator-granted, never self-service** (`selfServiceScopes`
stays `[catalog:read, store:read]`): events carry decline reasons and the
moderator uid behind every decision, which is a different disclosure than the
registry rows `catalog:read` buys. The gate is two-stage in `catalogAuth` —
`catalog:read` first, then the extra scope for that one path — so a key without
it gets `SCOPE_REQUIRED` naming the missing scope rather than a bare 403.

Read parity and hardening in the same wave:

- **`/v2/me/claims` returns what the service always carried.** `claimRecordFrom`
  dropped 9 of `UserClaimItem`'s 13 fields; `site`, `product_work_id`,
  `last_event` (id / from_state / to_state / reason / actor_uid / created_at),
  `first_acted_at` and `acted_count` are now published. The last three come from
  the actor aggregate, so they are present only when the row carries a last
  event — the moderation queue rows have no aggregate and omit the block.
- **`kind=` and filters on `/v2/me/claims`.** `submitted` (default) keeps works
  the bearer owns, `audited` the ones they reviewed but do not own, `all`
  everything they touched. `claim_state=` takes all six states here — this is
  the bearer's own history, not the public registry — and `site=` also scopes
  `first_acted_at`/`acted_count`.
- **`claim_state=` on `/v2/catalog/works` narrows to `none,live,draft`.**
  `pending`/`declined`/`hidden` are moderation-workflow states; v1 kept its
  pending queue behind a moderation gate and the first v2 cut left all six open
  to any `catalog:read` key. The queue is `/v2/moderation/claims`.
- **`owner_uid=` on `/v2/catalog/works`** requires `site=` (owner uids are the
  claiming site's own user ids, so they are ambiguous alone → 422
  `INCONSISTENT_WITH`) and is refused on the search lane with
  `MUTUALLY_EXCLUSIVE_PARAMETERS` — the search index carries no claim owner.
- **The moderation claim reads demand `catalog.claim.review`.** `ListModerationClaims`
  and `GetModerationClaim` only required a site-bound user token; only
  `DecideClaim` checked the permission. A plain user could therefore read the
  whole pending queue including decline reasons.
- **`POST /v2/me/claims` accepts `{site_work_id, display_name}`.** That payload
  used to fall through to the "work_id or refs is required" 422 — the only
  anchored-mint shape v1 offered had no v2 equivalent. An existing
  `(site, product_work_id)` anchor still returns `ALREADY_EXISTS` (409) from
  `SubmitWork`, unchanged.

Shape notes worth keeping:

- `store_stat.link_kind`, not `kind`: **G8 forbids a bare `kind` property**, the
  same rule that produced `image.cover_kind`.
- `store_stats.from_date`/`to_date`, not `from`/`to`: G8 wants one shape per
  property name and `FieldDiff.from`/`to` are untyped (`any`). The query
  parameters are still `from=`/`to=` — parameters are not properties.
- `GET /v2/store/purchase-links/{product_id}` declares 404 because **G4** requires
  it of any path-parameter operation. Nothing mints one: `product_id` is a
  DLsite number this service never resolves against a catalog, so an
  unknown-but-well-formed id still gets a link.
- Both store operations always send `Cache-Control: private`. Every response is
  keyed to the calling application; a shared cache holding one site's short
  links and serving them to another hands the clicks to the wrong site.
- One `store/service.Service` instance is shared by the v1 and v2 faces. A
  second one would mint a second alias for the same `(client, product)` pair and
  split the click count that settlement reads.
- **`GET /v2/store/stats` deliberately answers without the shortener**: only the
  purchase-links op requires `Configured()`. Stats reads the database and never
  mints, and v1's `MyStats` only required the service to exist — gating both ops
  on `Configured()` would take the stats read away from a deployment missing the
  shortener credentials, which is exactly the regression this wave exists to
  avoid. An unbound service (`Catalog.Store == nil`) is the one case both refuse.

Two new problem codes in a new `store` domain: `STORE_QUOTA_EXCEEDED` (403) and
`STORE_LINK_UNAVAILABLE` (502). The 502 is deliberate and has no fallback to a
bare affiliate URL — a raw aff link bypasses the click counter, which is the one
thing the de-duplication promise to DLsite rests on.

Edge and portal: `/v2/store` needs no new Traefik router — `infra-v2-pub` already
matches `PathPrefix(/v2)`. The portal relay allowlist gained `v2/store/`.
oasdiff against the GA baseline reports **0 error, 8 warning** (all
`response-property-enum-value-added`, from `claim.state` gaining `none` and
`problem_type.domain` gaining `store`), so no entry was added to
`v2-openapi-breaking-ignore.txt`.

## Wave R3 — the total v1 teardown (2026-08-27)

Wave R1 filled the last capability gaps; this wave deleted what they replaced.
The catalog binary no longer serves any v1 face.

Removed, by family:

- **public `/v1/catalog`** — the fiber group in `cmd/catalog`, every
  `public_*.go` handler, the spec twins `public_huma.go` +
  `public_huma_work_subresources.go`, the `/v1/catalog/openapi.json` route and
  `docs/catalog/public-openapi.yaml` (+ its breaking-ignore).
- **`/v1/playtime`** — the mount, the gate plumbing and `user_playtime.go`. The
  `UserPlaytimeService` stays: `/v2/me/playtimes` rides it.
- **`/v1/news`** — the group, `news/handler/public.go`'s `PublicHandler`,
  `SetupNewsPublicSpec` and `docs/news/public-openapi.yaml`. The parsing helpers
  the admin queue shares moved to `news/handler/params.go`; the news services and
  `/api/v1/admin/news` are untouched.
- **`/v1/store`** — the group, `store/handler/public.go`, its spec twin and
  `docs/store/public-openapi.yaml`. The store service, `dev.go` (the portal's
  owner-usage panel) and `/v2/store` are untouched, and both v2 store ops still
  ride the single shared `store/service.Service` instance.
- **S2S `/api/v1/catalog`** — `S2SAuth`/`authenticateBasic`/`enforceSiteBinding`,
  `s2s.go`, `read.go`, `edit.go`, `lifecycle.go`, `user_claims.go`, the huma API
  build, the root `/openapi.json` dump and `docs/catalog/openapi.yaml`.
- **user `/api/v1/user/catalog`** — `user.go`, `user_claims_face.go`,
  `user_edit.go`, `user_edit_reads.go`, `user_read.go`, `user_cover_votes.go`
  and `edit_images.go`.

Six prefixes now answer `410 Gone` from
`internal/platform/catalog/handler/retired.go`: `/v1/catalog`, `/v1/news`,
`/v1/store`, `/v1/playtime`, `/api/v1/catalog`, `/api/v1/user/catalog`. The body
is the house envelope with `code: 11`, the header is
`Link: <https://api.nextmoe.dev/v2>; rel="successor-version"`, and neither
`Deprecation` nor `Sunset` is sent — the face is gone, not deprecating. They are
mounted **after** the admin groups and after the v2 setup, because fiber matches
in registration order; `retired_test.go` proves the non-capture rather than
assuming it (`/api/v1/admin/catalog/*` reaches its permission gate, `/v2/catalog/works`
and `/v2/store/stats` still answer 401 problem+json).

The galgame tombstone (`internal/galgameapp/publicgone.go`) named `/v1/catalog`
as its successor, which would have become a tombstone pointing at a tombstone.
It now points at `/v2`.

Two follow-on removals the teardown forced:

- `catalog/service.WorkService` (and its `deriveAnchorRating`) had exactly one
  non-test caller, the S2S claim op. v2 mints through
  `ClaimLifecycleService.SubmitWork`, so the type is gone;
  `DeriveContentRating` and the shared `sourceRefR18` stay.
- devapi usage metering was wired into the v1 fiber groups only. Deleting them
  would have removed every writer of `developer_api_usage` and every
  `TouchLastUsed` call, leaving the portal's usage panel and each key's
  "last used" permanently empty with nothing failing. The recorder is now a
  `/v2` prefix middleware recording under the face name `v2`.

`nm_` v1 application keys were left in place as inert rows: nothing reads them
any more (only `/v1` accepted the prefix, and `/v2` refuses it with
`INVALID_CREDENTIAL`), and deleting them would take the owner's key list and
usage history with them. **This wave carries no migration** — no model, no DDL.

Plumbing that went with the specs: the `-catalog`, `-catalog-public`,
`-news-public` and `-store-public` arms of `cmd/gen-openapi`; the matching regen
lines and diff paths in `test.yml`; the four retired specs in
`spec-breaking.yml`; the catalog S2S arm of `openapi-types.yml` together with
`apps/web/shared/types/generated/catalog-api.ts` (apps/web never imported it);
the five v1 faces in `apps/developer/scripts/faces.mjs` with their `FACE_GROUPS`
table, `EDIT_EXCLUDED_OPERATION_IDS`, the per-operation auth override table and
their `EXPECTED_OPERATION_COUNTS` entries; the `v1/store/` entry in the portal
relay allowlist; and the generated per-operation Markdown twins under
`apps/developer/public/docs/{catalog,edit,news,playtime,store}`.
`docs/news/` and `docs/store/` held nothing else and are gone as directories.

Traefik/compose were deliberately **left untouched**: the v1 routers point at the
same catalog service that now answers 410 on those prefixes, so removing them
would turn a documented 410 into a 404 that names no successor.

## Wave R4 — `POST /v2/me/claims` carries the wizard's field map (2026-08-27)

R1 filled the capability gaps and R3 deleted v1, but one caller was left without
a lane: moyu's publish wizard. Its v1 call `POST /api/v1/user/catalog/works/submit`
handed an **editing-engine field map** straight to `SubmitWork` — multi-language
official titles, lang-less aliases, `olang`, an explicit content rating,
`display_nsfw` and a zh-Hans intro — and `POST /v2/me/claims` had nowhere to put
one. Every v2 mint was born with a display name and links and nothing else. This
wave adds an optional `field_values` object to the request body: additive, no new
operation (88 stays 88), no response change, **no migration**.

**The property is `field_values`, not v1's `fields`.** Gate G8 wants one shape
per property name, and `object_schema.fields` is already an array of
`schema_field` descriptors — `fields` as an object fails `TestG7toG16`, the same
rule that produced `cover_kind` and `from_date`/`to_date`. `field_values` is not
a coined name: `snapshot.field_values` already carries exactly this map, so
`POST /v2/me/claims {field_values}` mints the work that
`GET /v2/moderation/snapshots/work/{id}` answers with the same key. (`patch`,
the proposal faces' spelling of the same map, was the other candidate; it reads
as an edit to something that exists.)

`field_values` is a bare `map[string]any` in the schema on purpose. The accepted
keys are the service's whitelist (`editspec.submissionFields`), and duplicating
it in the huma input would be a second copy to drift. An unrecognised key answers
`VALIDATION_FAILED` with pointer `/field_values/<key>`, from
`editspec.SubmissionFieldError` — which `claimWriteErr` had never mapped, so
before this wave it would have escaped as a 500.

The lanes:

| Body | Result |
|---|---|
| `work_id` + `field_values` | **422** `VALIDATION_FAILED`, pointer `/field_values` |
| `refs` that resolve to a work, **with** `field_values` | **409** `ALREADY_EXISTS`, naming the anchor and that work's id |
| `refs` that resolve to a work, no `field_values` | claims it — unchanged |
| `refs` with no match, `field_values` | mints; the ref-derived URLs are unioned into `catalog.work.links` |
| `site_work_id` + `field_values` | mints anchored to the site's own id |
| `field_values` alone | mints — moyu's exact shape |

Decisions behind that table:

- **A ref that already resolves is 409, not a silent claim.** Without
  `field_values` the old behaviour stands. With them the caller asserted content
  for a work they believe is new, so claiming the match would answer 201 while
  dropping the whole map on the floor. The last-resort problem now reads
  "work_id, refs, site_work_id, or field_values is required."
- **The ref anchors survive a caller who also sends links.** The URLs derived
  from `refs` are unioned into `catalog.work.links` rather than replacing or
  being replaced by the caller's list, deduped on the exact URL string. They are
  what `SubmissionAnchorsOf` turns into the identity anchors a later submission
  is recognised by, so losing them to a caller-supplied `links` would make the
  mint unrecognisable to its own retry.
- **`display_name` resolution.** Top-level `display_name` wins and is written
  into the map; otherwise `field_values["catalog.work.display_name"]` must be present;
  otherwise the existing `/display_name` required problem. The map's display name
  is only a **seed**: inside the same transaction `applyTitles` runs after
  `olang` and rewrites `catalog_work.display_name` to the official title in the
  work's `olang`. That is `ApplyWorkFields`'s standing behaviour and was v1's
  too — the wizard sends the zh-Hans name first with `olang: ja`, and the work is
  born under its Japanese title.
- **An explicit `content_rating` is honored.** When
  `field_values["catalog.work.content_rating"]` is present `DeriveContentRating` is
  skipped entirely: the field goes through editspec and stamps curated
  provenance, where the derived value is written straight to the column with no
  stamp so the weekly releasemeta lane can still correct it. When it is absent
  the derivation is exactly what it was.
- **The trusted fast lane came back.** v1 computed
  `catperm.Resolver.Can(roles, EditTrusted)` and passed it as
  `SubmitWorkParams.Trusted`, which mints `live` instead of `pending`; v2's
  `CreateClaim` never set it, so an editor holding `catalog.edit.trusted` lost
  the privilege by moving to /v2. It is now set on every mint lane. v1 also
  required `!isThirdPartyClient`; that half is **not** reproduced — see below.
- **`released` is deliberately still absent.** `SubmitWorkParams.Released` stays
  zero from /v2, so `ErrSubmitInvalidDate` is unreachable here and is not mapped.
  No caller ever sent it.

On the third-party half of v1's trusted condition: v2 has no notion of a
third-party client anywhere in `apiv2`, and site binding is **not** structurally
first-party-only. `ctxSite` is filled from `Options.LookupSite`, which in
`cmd/catalog` is `clientRepo.FindByClientID(clientID).CatalogSite` with no
first-party check, while third-party-ness is the independent column
`oauth_clients.owner_user_id`. Nothing in the schema forbids a row from carrying
both. What does hold is that **no code path can produce that row**: the only
writer of `owner_user_id` is `devapi.SelfServiceService.CreateApp`, which never
sets `catalog_site`; `devapi.AdminService.UpdateAppConfig` cannot write
`catalog_site`; and `site/service`'s first-party client creation does not set it
either. A third-party app with a catalog site can only be made by an operator
editing the column by hand. The guard would also not be a privilege boundary on
/v2: `patchMyClaim` lets a claim's owner move `pending → withdrawn → live`
(publish requires ownership, not a permission), so the same actor reaches `live`
in two more calls with or without `Trusted`. Adding the check would mean
widening `Options.LookupSite` to carry the client's owner, for a fence that is
already open next door.

## Wave — the changes feed is the display-axis mirror channel (2026-08-28)

Downstream mirrors of the editorial display verdict (`content_limit`) had no
declared change channel, so both the forum and moyu fell back to a nightly full
sweep — ~300 requests over ~7.8k rows to learn about a handful of editor flips.
The forum's code carries the lament verbatim: *"There is no feed for that field
— the claim-event feed carries claim state only — so a full sweep is the only
way an editor's flip reaches the local lists."* The channel already existed:
`GET /v2/catalog/changes` keysets `catalog_work` on `(updated_at, id)`. This
wave declares it, and closes the two holes that stopped it from being usable.

**Spec is 2.1.0.** Additive only: one optional response property, one longer
operation description, zero new operations (88 stays 88), **zero migrations**.

**Hole 1 — disappearances were invisible.** `PublicService.Changes` filtered
`deleted_at IS NULL AND status = live`, so a work that left the population left
the feed too and downstream kept the dead id until its next sweep. The
population is now every galgame row, with `gone = (deleted_at IS NOT NULL OR
status <> live)` computed per row and rendered as an **optional** `gone`
property — present only when true, following the repr package's pointer +
`omitempty` house style. `gone` is now exactly the complement of the works face:
the `ids=` lane serves live, non-deleted works regardless of claim state. The
widened query still rides `idx_catalog_work_updated_id` (a full index on
`(updated_at, id)`); EXPLAIN on the 229k-row dev catalog shows the same Index
Scan as before, with one fewer filter.

**Hole 2 — the one retirement write that did not bump.** `retireSource` in
`merge_execute.go` set `status = merged, site = NULL, product_work_id = NULL`
without touching `updated_at`, and the GORM soft delete that follows writes
`deleted_at` only — so an executed merge retired an id in total silence. It now
carries `updated_at = now()`. Every other recurring writer of the promised axis
was audited and already bumps: `editspec.applyWorkColumn` (GORM `Model.Update`,
`display_nsfw` / `content_rating`), the claim lifecycle's `Model.Updates` on
`catalog_work`, `releasemeta`'s rating lane (explicit `updated_at = now()`),
survivorship's claim move (explicit `now()`), importers (INSERT-only for these
columns), and `repository.TouchWorks` for every subresource write. The
survivorship merge of `display_name` / `olang` writes through
`tx.Table("catalog_work")`, which has no schema and therefore no auto
`updated_at` — it is covered because `ExecuteMerge` appends the target to
`TouchWorks`.

**What is promised, and what is not.** Claim state, the display axis and
existence surface here by contract. Covers, tags, titles, intros and ratings
surface best-effort — most of those writers touch the work as well, but no
touch-audit was done for them and the feed does not promise them.

Consumer recipe (bootstrap from an empty cursor, hydrate `ids=` in ≤100 batches
with both gates open, `gone` → drop, redirects → repoint, `NULL` cache column
means not-synced-yet and passes): [01 §8](./01-service-and-contract.md).

## Wave — the instruments (2026-08-28)

The four waves before this one fixed the API. This one fixes the tests that were
supposed to have caught them, so the findings are mostly about the shape of the
checks rather than about the surface. Four API defects fell out of building the
checks and all four are one shape — a parameter declared, parsed and then
ignored, or validated against the wrong vocabulary. Three are on `/v2/news`
(deviations 90, 91 and 98) and one on the proposal list lanes (99). The last of
them changes what a live consumer page renders, which is recorded in 98 rather
than left for the next reader to rediscover on deploy day.

**Spec is 2.2.0.** Additive only: four optional query parameters on `listNews`
(deviation 98) and its longer description, zero new operations (88 stays 88),
zero schema or response changes. `oasdiff` 1.21.0 reports "no breaking changes"
against `origin/main` and the other eight specs report "no changes detected".
The `listNews` MCP tool grows the same four parameters, so the **mcp container
must be restarted after the deploy** or it serves the pre-bump tool signature.

Three hand-maintained lists that a growing route table had already outgrown are
now derived from the document and guarded, each with a named exclusion list
carrying reasons: the live-read sweep (deviation 93), the collection-binding set
(94) and the batch-lane contract (90). The pattern is the one the repo doctrine
asks for — the list is the contract, and what is deliberately out of it says why.

**Zero migrations.**

## Wave — the works list answers what it accepts (2026-08-28)

Deviations 100, 101 and 102. One defect in three places: the collection faces
of the work family declared the **detail** face's include vocabulary. The works
list validated eighteen tokens and served four, the calendar translated through
a second mapper that had never learned `titles`, and the schema face published
the detail list as if it were the only one.

This is the same class the five preceding waves were written against — a
parameter declared, parsed, validated and then answered by nothing — and it is
the first one a consumer paid a production cost for rather than reporting from a
reading. Moyu's browse page carries a per-work detail read behind concurrency 8
and a six-hour in-process cache, on the site's busiest page, because
`include=tags,credits` answered 200 with neither block. That read becomes one
request on the deploy that carries this.

**Two halves, deliberately unequal.** `tags` and `credits` are hydrated, because
both are batch reads and the page cost was measured before either was written
(tags average 14.8 rows per work at the default ceiling, credits 6.0). The ten
tokens a list cannot serve in batch are refused through the collection's own
`Spec`, so they answer the existing `400 UNKNOWN_INCLUDE` instead of 200 and
silence. `titles` and `covers` keep their narrower collection meanings, which
three live consumers depend on in exactly that shape and which are now written
into the operation description instead of into this file only.

**Spec is 2.3.0.** Additive: two response properties on `object_schema`
(`list_include`, `list_full_set`), longer descriptions on `listCatalogWorks`,
`listCatalogCalendar` and `getCatalogSchema`. Zero new operations (88 stays 88),
zero request-schema changes. `oasdiff` 1.21.0 reports "no breaking changes"
against `origin/main`.

**One behaviour change a caller can see beyond the additions:** the ten
detail-only tokens now 400 on `/v2/catalog/works` and `/v2/catalog/calendar`.
The three sibling repos were enumerated before the narrowing, not after, and
none of them sends one; the eighteen-token string in the forum is its
`v2CatalogDetailInclude["works"]`, applied only to two-segment detail paths.

**Zero migrations.**

## Wave — the credit-role registry gets a face (2026-09-03)

Credit groups publish `role_key` and nothing answered what a key was: the
registry (`catalog_role`, ~231 live rows) had no route, so an editing client
made users hand-type integer role ids. `GET /v2/catalog/roles` and
`/v2/catalog/roles/{id}` mirror the engines lane — keyset cursor, `ids=` batch
lane — with two deliberate absences: `refs=` is not resolved (role has no
`catalog_external_ref` entity type; unknown refs land in `missing[]`, the
series precedent) and there are no work counts (`catalog_credit.role_id` is
unindexed and this face must not force an index for a picker).

**The incident the wave surfaced:** `CreditGroup.RoleKey` and
`NameCreditRole.RoleKey` declared pattern `^[a-z][a-z0-9_]*$` while 189 of the
231 live registry keys violate it — 39 are CJK (原画 alone heads 11,937 live
credit groups) and others start with a digit. Every real credit response was
already contradicting the published spec; a client that validated against it
would have rejected most of production. The pattern is removed and the docs on
both fields now say the key is not always ASCII.

**Spec is 2.5.0.** Additive: two new operations (90 → 92), one new `role`
schema, zero request-schema changes, and the `RoleKey` pattern removal — a
constraint loosening, not breaking.

**Zero migrations.**

## Wave — the schema face declares value shapes (2026-09-04)

`GET /v2/catalog/schemas/{object}` listed fields but said nothing about their
values: an editing client saw `field_type: enum` with no token set, `list`
with no element shape, and could not tell a nullable scalar from a required
one. Every `editing.FieldSpec` now carries an optional `ValueSpec` — the
declarative twin of its `Validate` closure — mirrored on the wire as
`vocabulary` / `base` / `nullable` / `element{type, members[]}`. Metadata
only: nothing reads it on the write path, so honesty rests entirely on the
editspec gates (`TestSchemaValueSpecCompleteness` fails on an undeclared
enum/list unless it sits on a named exclusion list with a reason;
`TestVocabularyTokensPassValidators` pushes every declared vocabulary through
the real validator, string tokens or integer codes with both out-of-range
ends; `TestScalarNullabilityMatchesValidators` probes `Validate(nil)` on
every scalar).

**The incident the wave surfaced:** the first draft of the contract said
"the wire carries the token's 0-based index in the vocabulary's published
order" — and `gender` / `blood_type` are 1-based, because their models
reserve 0 for null. A bare index contract would have made those two
vocabularies undeclarable (or worse, declared and off by one). The contract
is now `base + index` with `base` explicit on the wire, defaulting to 0.

Four vocabularies joined the registry so declarations could name them:
`olang` (49 language tags), `release_lang` (olang plus six release-only
codes, derived from the same slice so the union rule has one copy per side —
an internal editspec gate asserts the vocab and validator sets stay equal),
`platform` (47), and `intro_lang` (the closed 4-set intro languages). Their
values carry empty descriptions — a language name does not need prose — and
the vocabularies-zh coverage gate now skips empty sources instead of
demanding a translation of "".

**Spec is 2.6.0.** Additive: no new operations (92), new always-present
`vocabulary`/`base`/`nullable` properties and an optional `element` on the
schema field, four new vocabularies.

**Zero migrations.**

## Wave — the mint asks before it makes a duplicate (2026-09-05)

`POST /v2/me/claims` hard-blocked a mint on two collisions — the
`(site, product_work_id)` claim key and an exact source anchor — and let a
title collision through, filing it as a match candidate *after* the row
existed. That is the right answer for the machine reconciliation lane, which
judges with dates, refs and labels the submitter never sends, and the wrong
answer for a person who is about to create the second copy of a work that is
already there: nobody told them, and the receipt they never see is worked by
somebody else weeks later.

`SubmitWork` now runs a pre-mint gate inside the same transaction, after the
claim/anchor checks and before the insert. It folds the submission's
`display_name` and every `catalog.work.titles` string and matches them against
live works of the same medium; any hit aborts the transaction with 409
`DUPLICATE_SUSPECTS`, naming the works in `suspects[]` (`id` + `display_name`,
at most 8 — the answer is a confirm prompt, not a census). Nothing is written.
Re-sending the same body with `confirm_duplicates: true` mints exactly as
before, and the post-mint receipt still files the pairs as pending candidates:
confirming is the submitter saying "these are different works", not a verdict
that ends the reconciliation.

The folding and the eligibility floor are the detector's own SQL —
`WorkTitleFoldSQL` (NFKC lower, every whitespace rune including U+3000
removed) and `WorkDupeNormEligibleSQL` (≥4 runes, or exactly 3 CJK-unified
runes, so `PC版` and `abc` never gate anything). Sharing the expressions
rather than restating them is the point: a gate that refuses a mint the
detector would not have flagged, or lets through one it would, is worse than
no gate. The medium filter comes from the same lane — the cross-media import
deliberately mints an adaptation under the same title, and those are never
duplicates of each other.

Only the mint lanes reach the gate. `work_id`, and `refs` that resolve to an
existing work, are claims: there is no new row to be a duplicate of.

**The trap the wave surfaced:** the service suite's shared `submitFields`
fixture gives every submission the same zh-Hans alias title, so once the gate
existed, every fixture in the file was a duplicate of every other. The three
lanes in `TestSubmitWorkIssuedIdempotency` that deliberately re-send one title
through another door now go through `ConfirmDuplicates` — they are anchor
assertions, and routing them around the gate instead would have meant weakening
it to keep a fixture green.

**Spec is 2.7.0.** Additive: no new operations (92), one optional
`confirm_duplicates` request property on `createMyClaim`, one new problem code
`DUPLICATE_SUSPECTS` (409, domain `me`) and the optional `suspects[]` it
carries on `Problem`.

**Zero migrations.**

## Wave — the schema face says how an enum is carried (2026-09-05)

The previous wave published `vocabulary` and `base` on every field of
`GET /v2/catalog/schemas/{object}` and still left the wire encoding of a
scalar enum undiscoverable. `catalog.work.content_rating` and
`catalog.release.kind` take an **integer** (`base` plus the token's index in
the vocabulary's published order); `catalog.work.olang`,
`catalog.character.lang`, `catalog.release.lang` and
`catalog.release.platform` take the **token string** itself. Both looked
identical on the face — `field_type: enum`, a vocabulary, `base: 0` — so an
editing client had to guess, or probe the write face and read the 422. List
element members never had the problem: member `type` is `int` for the coded
ones and `enum` for the token ones.

`editing.ValueSpec` gains `Coded bool`, and the face publishes it as
`encoding: "int" | "token"`, present exactly when `vocabulary` is set. Four
FieldSpecs declare `Coded: true` — `content_rating`, `release.kind`, and
`character.gender` / `character.blood_type` (the two 1-based ones). No mirror
field on `ElementMember`: `type` already discriminates there, and a second
declaration of the same fact is a second thing to keep in step.

**The inference became an assertion.** `TestVocabularyTokensPassValidators`
used to *infer* string-coding by trying every token against the validator and
watching what stuck; it now reads the declaration and checks both directions,
so a lying `Coded` fails. A token field must accept every token, reject an
unknown token, and reject `Validate(float64(base))`; an int field must accept
every code in range, reject the first code past either end, and reject
`Validate(tokens[0])`. `TestSchemaValueSpecCompleteness` adds the two
coherence rules: `Coded` needs a vocabulary to index, and a nonzero `Base` is
meaningless without `Coded`.

**Vocabulary order, stated out loud.** A vocabulary's published order is
contractual **only** where it is read as `int` — there the wire value *is*
`base + index`. Under `token` encoding the order is presentational and may be
sorted for display. This came up on `platform`, which publishes `wiu` before
`win` (label-sorted) while the validator's accept set lists `win` before `wiu`
(a map literal, which has no order at all). Nothing contractual disagrees —
`platform` is token-encoded — but until now no document said so, and a client
reading the two orders side by side had no way to know which one it was
allowed to depend on.

**Spec is 2.8.0.** Additive: no new operations (92), one optional `encoding`
property on the schema field.

**Zero migrations.**

## Wave — the SFW face stops serving vandalized art (2026-09-05)

The default cover face used to show whatever legacy-wiki upload happened to be
graded safe — for pure R18 titles that was a hand-censored derivative (black
bars, white-block erasure, sticker overlays), because every official cover is
explicit and the round-1 scan skips `sexual >= 2`. The remediation replaces
those rows with first-party **blurred stand-ins**: a new `censored` source
(registry id 21, open `sources` vocabulary) whose bytes are derived from the
work's best explicit official art by collapsing it through a 24px intermediate
— a color-field ghost with no recoverable detail, safe for crawlers and
content raters.

**The election ladder, in order:** real safe art → (R18 opt-in only) real
explicit art → the censored ghost. The ghost fills **only the portrait slot**
and only when nothing else may: it never competes inside the main scans, so it
cannot shadow the explicit cover an R18 viewer should get, and it cannot
outrank real safe art. The banner keeps the real-art → screenshot ladder and
may stay null. `origin` on a slot gains the third value `censored` (WARN-level
per oasdiff, not ERR — additive on an already-open reading).

Stand-ins are pre-staged by `cmd/backfill-censored-covers`, whose candidate
gate judges the world as if the wiki-lineage rows (`curated`, `upscale`) were
already deleted — so ghosts land while the curated covers still serve, and the
face flips with no blank window when the deletion runs. `imagegradesync` lists
`censored` as never-refined: its `sexual=0` is true by construction, and a
grader that saw through the blur would blank the face the stand-in exists to
fill.

**Spec is 2.10.0.** Additive: no new operations (92), one enum value on
`origin`, one `sources` vocabulary value.

**Zero migrations** (the new source row seeds through the ordinary catalog
migrate).

## Wave — the catalog read surface takes either credential (2026-09-06)

A desktop manager (Tauri / Wails) that ships to users has nowhere to keep an
`nmk_` key: the binary is on the user's machine, `strings` reads it out, and a
leak is the developer's key with the developer's quota being spent by someone
else. Until now that was the only credential `/v2/catalog` took, so the advice
was "put a backend of your own in front" — which is not advice a client-only
application can follow.

`/v2/catalog/**` now accepts **an application key or a user access token**, and
the token must carry `catalog:read`, which joins `selfServiceUserScopes` so a
self-service login app can ask for it at consent. `/v2/store`, `/v2/me` and
`/v2/moderation` are untouched.

**One request, one credential** (refs/api-v2 D1) survives intact. The gate
dispatches on the single `Authorization: Bearer` value's own prefix: `nmk_` is
an application key, anything else is a user token. There is no second header,
and no fall-through — a key that fails its lane is refused, never re-read as a
token, or a revoked key would get a second reading it should not have.

Two catalog paths deliberately stay key-only, in the document as well as in the
gate: `GET /v2/catalog/claim-events`, whose extra `claim_events:read` is
operator-granted and has no consent form a person could tick, and everything
under `/v2/store`.

**Rate limiting** reuses the `u<uid>` bucket deviation 47 built: a user token on
the catalog face counts against that person's own `100/min + 10,000/day`
default, **pooled across every application they have authorised**. Per-`(client,
uid)` buckets were considered and rejected — that would make "register three
applications" a way to triple a person's quota. An application key still wins
over a uid when both are somehow present.

`UserIdentity` grows `Scopes`, read from the access token's own `scope` claim;
nothing else about token lookup changed, and there is no second validator. The
catalog lane and `userAuth` set the same Locals through one `applyUserIdentity`,
because the limiter reads `user_id` and would otherwise see a user bucket on one
face and the anonymous per-IP one on the other.

Tokens issued before this wave do not carry `catalog:read`, and nothing
grandfathers them: the user re-consents once.

**Spec is 2.11.0.** Additive: no new operations (92); 46 catalog operations gain
`userToken` as a second, alternative security requirement (an OR, never an AND)
and say so in their descriptions. oasdiff reports no breaking change.

**Zero migrations.**
