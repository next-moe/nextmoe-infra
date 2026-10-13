# 收藏夹

> NextMoe·未萌 开放 API —— ACGN 数据，以此为准。同一部作品在六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。

- Base URL：https://api.nextmoe.dev
- 文档：https://developer.nextmoe.dev/docs
- MCP 端点：https://mcp.nextmoe.dev/mcp
- 调用与编辑都完全免费，没有付费档位，只有一层防滥用的限流。

**署名**：目前阶段使用 NextMoe·未萌 API，可以将 API 的名字标记为『鲲 Galgame 论坛』（如果你使用 Galgame 数据）或『LetMoe·一启萌』（如果你使用同人游戏数据）。

收藏夹是这个人自己的作品清单，存在中央，所有 NextMoe 站点读的是同一份。游戏管理器怎么做增量同步，见 [原生桌面应用接入 · 收藏夹同步](/docs/native-app#folders)；这一页是这组资源本身的语义。

## 我的收藏夹

```http
GET    /v2/me/folders
POST   /v2/me/folders
GET    /v2/me/folders/{id}
PATCH  /v2/me/folders/{id}
DELETE /v2/me/folders/{id}
GET    /v2/me/folders/holdings?work_ids=
```

这是 `/v2/me` 上要 scope 的一族。`folder:read` 覆盖 GET / HEAD，`folder:write` 覆盖其余方法，并且**同时满足读**——只申请了写的应用仍读得回自己写的东西。缺了是 `403 SCOPE_REQUIRED`，响应里点名缺的是哪一个。

列表按 id 升序。`contains_work_id=` 只留已经收了这部作品的夹，给「加入收藏夹」的选择器用。`GET /v2/me/folders/{id}` 在这个人没有这只夹时是 404。

`POST` 的 body 是名字，加上可选的备注、`visibility`（`private` 或 `public`，缺省 `private`）和 `is_default`。`name` 与 `description` 是给人看的，不能拿来当判别条件。

`PATCH` 是部分更新。`description` 送空串等于清掉备注。`is_default` 只接受 `true`：默认标记移到这只夹上，原来那只自动失去标记。送 `false` 是 422，要改默认就去另一只夹上设 `true`。

`DELETE` 删掉夹和里面的条目，204，没有 body。默认夹在标记移走之前删不掉，是 422。

每人最多 **200** 只夹，超出 422。

`GET /v2/me/folders/holdings?work_ids=` 一次问最多 100 部作品各在哪些夹里。只返回至少在一只夹里的作品，`folder_ids` 不为空；这个人没收藏的、以及不存在的 id，都不出现。各种可见性都算，因为这些夹都属于这个人。`work_ids` 必填，这是批量读，不分页。同样要 `folder:read`（`folder:write` 也行）。

## 条目

```http
GET    /v2/me/folders/{id}/items
PUT    /v2/me/folders/{id}/items/{work_id}
DELETE /v2/me/folders/{id}/items/{work_id}
POST   /v2/me/folders/{id}/items
```

读按 `updated_at` 做 keyset 翻页。最后一页的 `next_cursor` 可以存下来，以后原样回放，当作增量水位。删除不会在这道增量里回放。

`PUT` 把一部作品放进夹里，本来就在则原样返回，不改 `updated_at`。`DELETE` 拿出去，本来就不在也是 204。作品必须是 `live` 的；隔离的或不存在的 id 是 404。每只夹最多 **10,000** 条，超出 422。

`POST /v2/me/folders/{id}/items` 批量加入，一次最多 100 条，响应是 `207 Multi-Status`。`items[]` 与请求逐位对应，每一项要么是一条 `folder_item`，要么是一个 problem。整体不是事务，部分成功是正常结果，按项读 `status`。

```json
{ "items": [{ "work_id": "1" }] }
```

目录把两部作品判成同一部时，指向被退役 id 的条目会改指幸存者，并刷新 `updated_at`。同一只夹里两边都收藏过的合成一条。

## 公开读取

`visibility: public` 的夹出现在 `/v2/folders`。需要应用密钥，或带 `catalog:read` 的用户令牌。`folder:read` 开不了这个面。不带凭据也不行。

```http
GET /v2/folders?owner_uid={uid}
GET /v2/folders/{id}
GET /v2/folders/{id}/items
```

`owner_uid` 必填，没有全站目录。列表按 id 升序，只含这个账号的公开夹。私密夹不会出现，连主人自己来读也没有——主人的完整清单在 `/v2/me/folders`。

`GET /v2/folders/{id}` 在这只夹不公开时是 404，不存在和主人设成 private 是同一种 404。条目列表的形状与 `/v2/me/folders/{id}/items` 相同，按 `updated_at` 翻页，夹里有什么就返回什么，不做内容过滤，所以 `item_count` 与返回的条数一致。调用方要藏 R18，在水合作品时自己滤。

## 谁收藏了这部作品

```http
GET /v2/folders/holders?work_id={work_id}
```

给一部作品发通知时用：哪些账号的某只夹里有它。各种可见性都算，私密夹的主人也在等这个消息。回答按 `owner_uid` 升序分页，只有 uid——没有夹的 id、名字、可见性或数量，不能拿来翻成「谁收藏了什么」的索引。没有人收藏是空列表，不是 404。

只要应用密钥，而且要在 `catalog:read` 之上再有运营发放的 `folder_holders:read`。这个 scope 不能在控制台自助勾选。用户令牌来是 `403 SCOPE_REQUIRED`，不要把它当成过期去刷新。

## 审核

```http
PATCH  /v2/moderation/folders/{id}
DELETE /v2/moderation/folders/{id}
GET    /v2/moderation/users/{uid}/folders
DELETE /v2/moderation/users/{uid}/folders
```

门槛是 `catalog.folder.moderate`。`moderator`、`admin`、`ren` 默认有，权限控制台可以授给别的角色。审核编辑或认领的权限不包括这一条。和评分删除不同，这里认的是令牌上的角色并集，站点授予的角色也算。不需要 `catalog:edit`，也不需要 `folder:read`。

`PATCH` 改任何一只夹的名字和备注，或把它强制设成 `private`（这样它就从 `/v2/folders` 消失，条目还在）。名字可以收成空串，这是审核面独有的。不动夹里的作品，也不动 `is_default`。

`DELETE /v2/moderation/folders/{id}` 删掉夹和条目，204。默认夹也删得掉——默认标记是主人自己的导航，在这里尊重它就等于谁都能造出一只删不掉的夹。

`GET /v2/moderation/users/{uid}/folders` 是同一路径上 DELETE 的预览：这个账号的每一只夹，含私密的，按 id 升序，翻页方式与 `/v2/me/folders` 相同。不列条目，确认时看 `item_count` 即可。一只都没有是 200 加空列表，不是 404。

`DELETE /v2/moderation/users/{uid}/folders` 在一个事务里删掉这个账号的全部夹、成员关系和指向它们的导入记录。响应是一张回执，带删除的夹数和条目数。一只都没有是 200，计数为 0，不是 404。

---
本页来源 · NextMoe 开发者平台 · https://developer.nextmoe.dev/docs/folders
