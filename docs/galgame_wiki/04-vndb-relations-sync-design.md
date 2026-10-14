# VNDB 关联数据回填同步设计

> **已被取代（历史设计）**：本文描述的一次性补洞脚本 `cmd/sync-vndb-relations`
> 已删除，关联同步并入持续运行的 `cmd/sync-vndb-enrich`（与 links 一起，走带
> provenance 的 approach-B 对账，resolver 也统一了）。这套同步后来随 galgame wiki
> 退役（wave-161）整体删除，现状文档 `docs/sync/vndb/README.md` 也已移除；本文仅留作决策追溯。

## 背景

`sync-vndb` 脚本在插入新 galgame 时会 **顺带** 创建 `galgame_tag_relation` 和 `galgame_official_relation`。但存在两类"关联缺失"数据：

1. **moyu 迁移来的 galgame**（约 3600 条）——只导入了主表字段和 `vndb_id`，未建任何 tag/official 关联
2. **旧 sync-vndb 过滤掉的 tag 关联**——之前 `rating < 1.0` 的 tag 被跳过，导致这些 galgame 的 tag 关联不完整

本脚本的目标是 **按 vndb_id 回填** 这些缺失的 tag/official 关联，不动主表其他字段、不动已发布内容的草稿状态。

## 范围

- **不做**：canonical tag/official 元数据同步（description、aliases、searchable 等）——项目只需要 `name`/`category`/`lang`，按需从 VN 返回的内嵌数据创建
- **不做**：新增 galgame 主表记录（那是 `sync-vndb` 的职责）
- **只做**：把已有 galgame 的缺失 tag/official 关联补齐

## 数据流

```
Load all galgame (id, vndb_id)
        ↓
Compute has_tag_rel / has_off_rel (per galgame, 单次聚合查询)
        ↓
Queue: need_tag = ¬has_tag_rel   need_off = ¬has_off_rel
        ↓
Batch 100 vndb_ids from union(need_tag, need_off)
        ↓
POST /vn (filters=["or", [id,=,v1], ...])
        ↓
For each returned VN:
   if in need_tag → resolve tags → insert tag_relation (ON CONFLICT DO NOTHING)
   if in need_off → resolve officials → insert official_relation (ON CONFLICT DO NOTHING)
```

## Schema 变更

### `galgame_official` 新增 `original` 列

| 字段 | 类型 | 说明 |
|------|------|------|
| `original` | `VARCHAR`, default `''` | VNDB `developers[].original`，原文名（日文、中文、韩文等非罗马化形式） |

GORM 模型加字段，运行 `cmd/migrate-galgame` 触发 `ALTER TABLE`。

## 决策清单（all confirmed）

### 1. Tag / Official 元数据范围

- 现有项目所有 tag 和 official **都来自 VNDB**，不存在用户手动建的条目。因此：
  - 本脚本不同步 `description` / `aliases` 等元数据（未来需要再加也不迟）
  - tag/official 的 canonical 属性保持 `{name, category[, lang][, original]}` 四字段

### 2. 跳过判定（做法 β：分开判断）

- `SELECT 1 FROM galgame_tag_relation WHERE galgame_id = ? LIMIT 1` 无结果 → 需要同步 tag 关联
- `SELECT 1 FROM galgame_official_relation WHERE galgame_id = ? LIMIT 1` 无结果 → 需要同步 official 关联
- 两者独立；一个 VN 可能只需要同步其中一类

**为什么 β 而不是 α**：更鲁棒。如果某 VN 有 tag 关联但 official 关联缺失（反之亦然），α 会漏补；β 分开判断不会漏。

**实现**：一次 `LEFT JOIN + GROUP BY` 聚合算出所有 galgame 的两个 flag，避免 N+1。

### 3. Tag 过滤

- `lie=true` → **跳过**（审核标记为错误/玩笑的 tag，不应入库）
- `rating` → **无阈值**（VNDB 的 rating 是 `(0, 3]`，本身不可能为 0；同步全部有评分的 tag）
- 无其他过滤

**与 `sync-vndb` 的差异**：`sync-vndb` 过滤 `rating < 1.0`，本脚本不过滤。这意味着旧 sync-vndb 同步的 VN 也可能在本脚本里补出更多 tag 关联——这是预期行为。

### 4. Tag 命名（做法 γ：同现行 `sync-vndb`）

- tagMap 命中 → 用中文名去 `galgame_tag` 匹配；命中即复用，不命中则新建（name = 中文）
- tagMap 未命中 → 用英文原名去 `galgame_tag` 匹配；命中即复用，不命中则新建（name = 英文）

**为什么 γ 而不是 δ**：把 rating 阈值从 1.0 降到 0 后，会有大量低评分的冷门 tag 进来，其中很多不在 tagMap 里。选 δ（跳过 tagMap 未命中的）会让这些 tag 根本进不了库，破坏"覆盖所有 VNDB tag"目标。取舍是接受"中英混排"。

**Category 映射**：`cont→content`, `ero→sexual`, `tech→technical`

### 5. Official 去重与 `original` 字段处理

VNDB 返回 `developers[].original`（可能为空）和 `developers[].name`（英文/罗马化）。

**查询逻辑**：
```sql
SELECT * FROM galgame_official
WHERE name IN (dev.original, dev.name)
LIMIT 1
```

两个值可能相同（dev.original 为空时），`IN` 会自动去重。

**处理逻辑**：
- **命中** existing：
  - 复用 `existing.id`
  - 若 `existing.original` 为空且 `dev.original` 非空 → `UPDATE SET original = dev.original`（回填老记录）
- **未命中**：
  - `INSERT` 新记录，`name = dev.original if non-empty else dev.name`，`original = dev.original`

**为什么这么做**：现有 1577 条 official 是旧 `sync-vndb` 按 `dev.name` 建的（那时 `original` 字段还不存在）。如果新脚本直接"先查 original"，就会漏掉这些老记录，造成重复。`IN (original, name)` 兼容两边。

**类型映射**：`co→company`, `in→individual`, `ng→amateur`

### 6. 同步范围（做法 B：仅回填缺失）

- 遍历本地 galgame，只处理缺 tag 或缺 official 关联的
- 不做全量 VNDB 遍历

**为什么 B 而不是 A**：sync-vndb 已经给 ~5000 条新 VN 建好了完整关联；只需补 moyu 那 ~3600 条。做法 A 会对 5000 条已完成的再跑一遍，浪费 VNDB API 配额且无增益。

### 7. 幂等性

- 所有关联写入用 `INSERT ... ON CONFLICT DO NOTHING`（`galgame_tag_relation` 和 `galgame_official_relation` 是复合主键 `(galgame_id, *_id)`）
- 元数据 UPDATE（回填 `original`）只在字段为空时发生，重跑不会反复覆盖
- 脚本安全重跑；β 判定会自动跳过已完成部分
- **不加 `--dry-run`**（用户确认不需要）

## VNDB API 使用

- 端点：`POST https://api.vndb.org/kana/vn`
- 过滤：`["or", ["id","=","v1"], ["id","=","v2"], ...]`（最多 100 个 ID/次，符合"list of identifiers"官方指引）
- 字段：`id, tags.id, tags.name, tags.category, tags.rating, tags.spoiler, tags.lie, developers.name, developers.original, developers.type, developers.lang`
- 限流：2 秒/请求，429 则等 60 秒重试（同 sync-vndb）

## 性能估算

- 现有 8602 galgame，其中 ~3600 moyu 来的缺关联 + 少量 sync-vndb 过滤的 tag 缺失
- 假设 4000 VN 需处理 → 40 次 VNDB 请求 → **约 90 秒**完成
- Tag 关联行数：新增约 10-30 万行（平均每 VN 20-70 个 tag，filter=0 后数量上升）
- Official 关联行数：新增约 1-3 万行

## 脚本入口

**位置**：`apps/api/cmd/sync-vndb-relations/main.go`（方案 A，独立脚本而非 `sync-vndb` 的 `--relations` 子模式）

**为什么独立脚本**：
- 职责清晰——`sync-vndb` 管主表插入，`sync-vndb-relations` 管关联回填
- 跑的时机不一样——`sync-vndb` 定时跑，`sync-vndb-relations` 是 moyu 迁移后的一次性任务
- 改现有脚本会把单一职责函数搞复杂

**调用**：
```
go run ./cmd/sync-vndb-relations
```

无 flag——行为唯一：扫全库，按 β 判定补齐缺失关联。

## 日志示例

```
[INFO] loaded galgame records=8602
[INFO] relations coverage tag_missing=3602 off_missing=3612
[INFO] queued unique_vns=3620
[INFO] batch 1/37 fetched=100 tag_rels=+1843 off_rels=+148 new_tags=+12 new_officials=+31
...
[INFO] sync complete tag_relations=304521 official_relations=28104 new_tags=203 new_officials=1102
```

## 与 `sync-vndb` 的关系

| 脚本 | 职责 | 触发 |
|------|------|------|
| `sync-vndb` | 拉取 VN 主表数据，创建 galgame + 顺带建关联 | 定时/手动 |
| `sync-vndb-relations` | 对已有 galgame 按 vndb_id 回填缺失的 tag/official 关联 | 一次性，moyu 迁移后跑 |

未来若再新增迁移源（例如其它站点也带 vndb_id），跑一次 `sync-vndb-relations` 即可补齐。

## 决策过程（问答追溯）

以下是设计阶段的澄清问答，保留以便后人理解"为什么这么选"。

### 第一轮：6 个核心决策

**Q1：`galgame_tag` / `galgame_official` 没有 `vndb_id` 列——加迁移还是按 name 匹配？**
> A：所有 tag 和 official 都来自 VNDB，name 匹配足够可靠 → 不加 vndb_id 列

**Q2：元数据回填范围？**
> A：tag 不需要 alias/description；moyu 那批 galgame 需要回填 tag/official 关联

**Q3：description / aliases 的处理？producer.original 放哪？**
> A：tag/official 都不要 description/alias；official 加 `original` 字段单独存放原名

**Q4：tag / official 主 name 用什么语言？**
> A：tag 用 tagMap 映射的中文；official 优先 `dev.original`，fallback `dev.name`

**Q5：tag 过滤阈值？**
> A：阈值设为 0，同步全部 tag

**Q6：同步范围？**
> A：做法 B——只补现有 galgame 缺的关联，不跑全量

**Q7：要不要 `--dry-run`？**
> A：不需要

### 第二轮：4 个实现细节

**Q1：现有 1577 条 official 如何防止重复？**
> A：`WHERE name IN (dev.original, dev.name)` 二选一命中即复用，顺便回填 `original`

**Q2：`lie=true` 的 tag 包含还是跳过？**
> A：跳过

**Q3：重跑时如何跳过已有关联的 VN（X vs Y）？**
> A：做法 X——已有关联的 VN 跳过 VNDB API 调用，省时间

**Q4：脚本位置（A vs B）？**
> A：方案 A——新建独立脚本 `cmd/sync-vndb-relations/main.go`

### 第三轮：2 个边界情况

**Q1：跳过判定粒度（α 合判 vs β 分判）？**
> A：β——tag 和 official 分开判断，避免"有 tag 没 official"这种情况漏补

**Q2：不在 tagMap 里的 VNDB tag 怎么办（γ 建英文 vs δ 跳过）？**
> A：γ——保持 `sync-vndb` 的现有行为，tagMap 未命中则用英文建新 tag，接受"中英混排"
