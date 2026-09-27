# Chat service and contract

The machine-readable contract is [`openapi.yaml`](./openapi.yaml), exported from
the Go code (`go run ./cmd/gen-openapi -chat`) and served live at
`GET https://api.nextmoe.dev/v2/chat/openapi.json`. This page is the human
companion: the rules the schema cannot say.

Design record: `refs/plans/17-chat/00-design.md` (plan 17).

## 1. What it is

Direct messages (and, next, groups) shared by every NextMoe site and app. A
conversation belongs to the people in it, not to a site: two users have one
direct conversation, and it is the same one on kungal, moyu, letmoe and the
App. Every message records the site it was sent from (`context.site` on a
context card; `origin_site` in storage) for provenance only.

- Service: `cmd/chat`, port 9285, database `kun_chat`.
- It reads `kun_galgame_infra` (token clients, user names and avatars, the
  deleted-accounts feed) and `kun_community` (follows, blocks, trust level).
- Realtime pushes go through Centrifugo.

## 2. Authentication

A token whose account has since been deleted still verifies until it expires;
chat refuses every write from it (`401 INVALID_CREDENTIAL`).

A user access token (`Authorization: Bearer …`) on every operation except the
spec. Application keys (`nmk_…`) are refused.

- `chat:read` covers GET; `chat:write` covers everything and also grants reads.
- Both scopes are **first-party only**: only Ren can add them to a client's
  `allowed_scopes`, and the self-service developer portal never offers them.
  A site must also request them in its authorize `scope=`; a token issued
  before that carries neither and gets `403 SCOPE_REQUIRED`.
- The token's client decides the caller's site (its community tenant) and the
  hosts a context card may point at (the https hosts of its redirect URIs).

Web sites call chat from their BFF with the session's access token; the App
calls it directly.

## 3. Conventions

The same as the rest of `/v2`:

- ids are decimal strings; positions (`seq`, `update_seq`), counts, offsets and
  lengths are numbers;
- every resource carries `object`; lists are `{object: "list", items, …}` and
  never null;
- errors are RFC 9457 problems. Chat's own codes live under
  `https://developer.nextmoe.dev/problems/chat/`: `CHAT_BLOCKED`,
  `CHAT_NOT_ACCEPTING`, `CHAT_REQUEST_LIMIT`, `CHAT_EDIT_WINDOW_CLOSED`,
  `CHAT_NOT_PERMITTED`.
- A conversation or message the caller cannot see answers `404`, whether or not
  it exists.

### Text and entities

A message is plain `text` plus formatting `entities`; there is no HTML or
markdown on the wire. Offsets and lengths count **UTF-16 code units**, as in
Telegram, which is how JavaScript and Dart index strings.

- Types: `bold`, `italic`, `underline`, `strikethrough`, `spoiler`, `code`,
  `pre` (optional `language`), `blockquote`, `text_link` (`url`), `mention`
  (`user_id`), and `url`.
- `url` entities are added by the server; one a client sends is dropped. Links
  inside `code`, `pre` or a `text_link` are not detected.
- Entities may nest but never partly overlap; nothing nests inside `code` or
  `pre`; at most 100 per message.
- Text is trimmed (entities shift with it) and may be at most 4096 UTF-16 code
  units. A message needs text or media.
- A mention must name a member of the conversation.

Clients turn markdown-style shortcuts into entities before sending; KunUI
exports the parser and its inverse for editing.

## 4. Conversations and the caller's dialog

`PUT /v2/chat/direct/{user_id}` returns the caller's direct conversation with a
user, creating it if needed. It sends nothing; a conversation with no message
stays out of both people's lists.

Each member has their own dialog state (`me` on a conversation): read position,
unread count, manual unread mark, cleared-through position, mute, archive,
pin rank, draft, and whether they have accepted the conversation.

- `PATCH /conversations/{id}/me` — `muted` (+ `muted_until`; omitted = until
  unmuted, stored as 9999-12-31), `archived`, `pinned` (at most 5),
  `marked_unread`.
- `PUT /conversations/{id}/draft` — synced to the caller's other devices; empty
  text and no reply clears it.
- `POST /conversations/{id}/clear-history` — hides everything so far from the
  caller alone; `{"remove": true}` also takes the conversation out of the lists
  until the next message. Deleting a message request is this call.
- An archived conversation that is not muted comes back to the inbox when a new
  message arrives. The sender's own archive is left alone.

`GET /conversations?folder=inbox|archive|requests` pages by most recent
activity. The inbox's first page starts with the pinned conversations, highest
rank first.

## 5. Who can start a conversation

Settings (`GET` / `PATCH /v2/chat/settings`):

| field | values | default |
|---|---|---|
| `allow_incoming` | `all`, `following`, `none` | `following` |
| `accept_requests` | boolean | `true` |
| `allow_group_invites` | `all`, `following`, `none` | `following` (groups phase) |

When a direct conversation is created, the recipient's settings decide:

- the sender is inside `allow_incoming` (`all`, or `following` and the
  recipient follows the sender) → the conversation is accepted for both;
- otherwise, with `accept_requests` → it lands in the recipient's **message
  requests** (`me.accepted = false`);
- otherwise → `403 CHAT_NOT_ACCEPTING`.

A sender cannot send requests while their account is under 72 hours old **and**
their community trust level is 0 (`403 CHAT_NOT_ACCEPTING`); they can still
message people who follow them. Each user may start at most 20 requests a day
(`429`). Acceptance is final: unfollowing later does not send a conversation
back to requests.

While a request is pending:

- the requester may send at most 3 plain-text messages with no links, no media
  and no context card (`403 CHAT_REQUEST_LIMIT`), and an edit may not add a link;
- neither side can pin (`403 CHAT_REQUEST_LIMIT`);
- the requester sees `peer_read_seq = 0` and receives no `read_outbox`, and
  neither side sees typing;
- the recipient accepts with `POST /conversations/{id}/accept` or by replying;
  after acceptance the requester learns the read position.

A block in either direction (community, `PUT /users/{id}/blocking/{target}`)
refuses creating the conversation, sending, editing, reacting and pinning
(`403 CHAT_BLOCKED`, without saying which side blocked), and typing stops
reaching the other side.

## 6. Messages

- `POST /conversations/{id}/messages` answers `201`. `seq` numbers the
  conversation's messages from 1 with no gaps.
- `client_message_id` (a UUID the client makes up) makes a send idempotent:
  resending it returns the first attempt's message and writes nothing. It comes
  back on the sender's own copy only.
- Replies: `reply_to_seq`, optionally `reply_quote` `{text, offset}` quoting part
  of the replied message; the server checks the quote is really there and
  attaches its entities. Messages carry a `reply_to` preview (text cut to 120
  UTF-16 code units). `deleted: true` with empty text means the original is
  gone for the caller: deleted for everyone, or hidden or cleared by the
  caller themselves. When a message is deleted for everyone, the quotes other
  messages took from it are removed too, and those messages get an
  `edit_message` update.
- Photos: upload the bytes first with `POST /v2/chat/images` (multipart, one
  part named `file`; 20 a minute) and send the returned `image_hash` as
  `media {type: "photo", image_hash}`. Width, height and thumbhash come from the
  image service, not the client. An album is several photo messages sharing a
  `media_group_id`. Chat stores its photos under its own image client (preset
  `message`) and pings their references daily: image-service reference pings
  are scoped to the uploading client, so a photo uploaded through a site's own
  image client would not be kept alive by chat.
- `context {kind, id, title, url}`: a card for the page a conversation is about.
  The URL must be https on one of the calling client's hosts; `site` is set by
  the server.
- `silent: true` delivers without a notification sound.
- Sending marks everything read for the sender.
- Limits: 30 messages a minute per user (`429` with `Retry-After`).

Editing (`PATCH /messages/{id}`): own messages, within 48 hours
(`409 CHAT_EDIT_WINDOW_CLOSED`).

Deleting (`POST /conversations/{id}/messages/delete {seqs, for_everyone}`):

- `for_everyone: true` — own messages only (`403 CHAT_NOT_PERMITTED`), with no
  time limit. The content is erased and the row stays as a tombstone, so `seq`
  stays gapless; reactions go with it. Tombstones are not listed.
- `for_everyone: false` — hides any visible messages from the caller alone.

Reactions (`PUT /messages/{id}/reaction {reaction}`): one per person per
message; a different key replaces it, `null` removes it. The vocabulary is
`GET /v2/chat/reactions`, the forum's set (Telegram's defaults), stored as keys.

Pins (`PUT` / `DELETE /conversations/{id}/pins/{seq}`): several messages can be
pinned; pinning posts a `message_pinned` service message. 10 pin changes a
minute per user. `GET /conversations/{id}` carries `pinned_seqs` and
`pinned_messages`, the messages themselves in the same order (most recently
pinned first, at most 100, as the caller sees them), so a pinned bar can show
a message that is not in the loaded page.

## 7. Sync: the update stream

Every change that concerns a user is written to that user's **update stream**,
numbered from 1 with no gaps (`update_seq`). The stream is the source of truth;
realtime pushes only speed it up.

1. On load: `GET /v2/chat/state` → `last_update_seq` and unread counts; load the
   conversation list.
2. On every update received: if `update_seq == local + 1`, apply it; if it is
   `<= local`, it was already applied; if it is bigger, there is a gap — wait
   half a second for the missing ones, then `GET /v2/chat/updates?after=local`.
3. On reconnect or when the page returns to the foreground:
   `GET /v2/chat/updates?after=local`.
4. `too_long: true` means the position is older than the kept stream (30 days):
   reload the conversation list and state.

`GET /v2/chat/updates` returns the updates together with the messages
`new_message` / `edit_message` name (as the caller sees them now), the
conversations they name, and the users involved.

| kind | data | who gets it |
|---|---|---|
| `new_message` | `{seq}` | every member, the sender included |
| `edit_message` | `{seq}` | every member |
| `delete_messages` | `{seqs}` | every member |
| `message_reactions` | `{seq}` | every member |
| `pinned_messages` | `{seqs, pinned}` | every member |
| `read_inbox` | `{max_seq, unread_count}` | the reader (their other devices) |
| `read_outbox` | `{max_seq}` | the senders of the newly read messages |
| `dialog` | the changed fields: `accepted`, `muted_until`, `archived`, `pinned_rank`, `marked_unread`, `draft` | the member |
| `hide_messages` | `{seqs, unread_count}` | the member |
| `clear_history` | `{through_seq, removed}` | the member |
| `member` | `{user_id, action}`; `action: "deleted"` when an account is erased | the other members |
| `conversation` | changed fields (groups phase) | every member |

## 8. Realtime

`POST /v2/chat/realtime-token` → `{token, expires_at, url}`. Connect a
Centrifugo client (official JS and Dart SDKs) to `url`
(`wss://api.nextmoe.dev/connection/websocket`) with the token, and fetch a new
one from the same endpoint when the SDK asks for a refresh (tokens last 15
minutes). The connection is subscribed to the caller's own channel on the
server side; the client never names a channel.

Each push is one of:

```json
{"type": "update", "update": {…}, "message": {…}, "reactions": […]}
{"type": "typing", "conversation_id": "12", "user_id": "34"}
```

An `update` push carries the same update the sync face would return;
`new_message` and `edit_message` pushes also carry the member's own view of the
message (left out for a member who hid or cleared that message), and
`message_reactions` pushes carry the member's view of the counts.
Pushes are best effort — apply them with the rule in §7 and a lost one is
recovered from the stream.

Typing: `POST /conversations/{id}/typing` while the user types, at most every
5 seconds; receivers drop the indicator after 6 seconds without a repeat.
Typing is never stored and spends no update number.

## 9. Reports

`POST /messages/{id}/report {reason, note?}` — reasons `spam`, `harassment`,
`sexual`, `violence`, `illegal`, `other`. Only someone else's message. Chat
keeps a snapshot of what the reporter could see (the message and up to 10
before it); reporting is consent to disclose them. A second report of the same
message by the same person returns the first.

Reports are forwarded to trust's unified inbox under the site they were filed
on, as subject kind `chat_message` (subject id = message id), with the snapshot
in the context note. Chat relays for every site, so its trust client is a
forwarder (in trust's `KUN_TRUST_FORWARDER_CLIENT_IDS`). Forwarding is switched
on by giving chat those credentials, which must wait until `chat_message` is
registered for each site: until then reports wait as `pending`. Trust refusing
a report outright marks it `failed` and it is not retried. Moderators see only
reported snapshots; there is no face for reading anyone's conversations.

Trust's decision comes back to `POST /trust/callback` on chat (internal only,
signed with `X-Trust-Timestamp` / `X-Trust-Signature` like every trust
callback), so each site registers `chat_message` with callback
`http://chat:9285/trust/callback`, the secret chat holds in
`KUN_TRUST_CALLBACK_SECRET`, and `notify_on_dismiss` on.

| trust action | chat does | open reports of the message |
|---|---|---|
| remove (2) or hide (1) | deletes it for everyone, exactly as its sender could: tombstone, quotes of it removed, `delete_messages` to every member | `removed` |
| none (0) | nothing | `dismissed` |

Hide is a removal because a conversation has nothing to restore a hidden
message into. A removal is final: a later dismissal leaves `removed` in place,
and a removal after a dismissal replaces it. Resolved reports are never
forwarded again.

## 10. Account deletion

Chat consumes the deleted-accounts feed hourly (consumer `chat`). For each
erased account every message it sent is deleted for everyone (erased, kept as a
tombstone), its reactions, drafts, hidden marks, update stream and settings are
removed, and it leaves its conversations; the others get `delete_messages` and
`member {action: "deleted"}`. A conversation nobody is left in is removed.
The account's words are erased from every report snapshot, its own reports lose
their note, and the quotes other messages took from its messages are removed.
This differs from Telegram,
which keeps a deleted account's messages; it matches community's purge.

## 11. Retention

Messages are kept until deleted. The update stream keeps 30 days.

## 12. History from kungal and moyu

`cmd/import-chat` copies the old sites' direct messages into chat, so a pair
that talked on kungal or moyu finds that history in their one conversation.
It reads the forum's and moyu's `chat_*` tables and writes through the same
service code a send uses.

- One conversation per pair. A pair that talked on both sites gets both
  histories interleaved by time (9 pairs in production), which is why both
  sources are imported in one run before any site opens chat.
- Both people are accepted: a history is consent. Messages keep their time,
  edited mark and origin site; recalled or deleted ones become tombstones;
  moyu replies point at the new `seq`; moyu reactions map onto the vocabulary
  and unknown ones are dropped. Read positions come from kungal's read
  receipts; moyu kept none, so its messages count as read.
- Markdown becomes text and entities. Uploaded images are re-hosted through
  chat's image client, so `KUN_CHAT_IMAGE_CLIENT_*` must be set; stickers
  (`![sticker](/image/<hash>_320)`) keep their hash; an image hosted elsewhere
  becomes a `[图片]` link. An album of photos from one old message shares a
  `media_group_id`.
- Pairs with a deleted account are left out: the purge would erase them.
- Every message is keyed by its source (`chat_import_message`), so a re-run
  imports only what is new. The first import writes no updates; a later run
  (a sweep while a site is cutting over) appends and announces what it adds.
  A pair that has already talked natively in chat is left out
  (`ErrNativeHistory`): old messages cannot go in before existing ones.

Run it with the chat container's environment on the docker network; without
`-apply` it only prints the plan (production dry run, 2026-09-27: 2,683 pairs,
11,241 messages, 517 tombstones, 422 photos, 164 stickers, 1 external image).
