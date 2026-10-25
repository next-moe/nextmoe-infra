// Package migrate owns the kun_catalog schema migration: the AutoMigrate
// table list plus the idempotent raw-SQL section for everything AutoMigrate
// cannot express. It is called by cmd/migrate-catalog and by the catalog
// integration tests (which migrate their test database with the exact
// production schema).
package migrate

import (
	"fmt"

	"api/internal/platform/accountpurge"
	"api/internal/platform/catalog/discussion"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"gorm.io/gorm"
)

// Run applies the full catalog schema (tables + raw SQL). Idempotent: safe
// to run on every deploy and repeatedly against the same database. Seeds are
// NOT part of schema migration — call seed.Run separately.
func Run(db *gorm.DB) error {
	if err := preMigrate(db); err != nil {
		return err
	}
	if err := db.AutoMigrate(
		// Registry (vocabulary) tables, doc 17 R1. catalog_role before
		// catalog_source_role_map so the role_id FK can be created.
		&model.CatalogMedium{},
		&model.CatalogSource{},
		&model.CatalogRole{},
		&model.CatalogSourceRoleMap{},
		&model.CatalogRelationType{},

		// Entity family (step 03), in FK dependency order. person and
		// credit_name reference each other: person is created first without
		// its primary_credit_name_id FK, which the raw-SQL section adds once
		// both tables exist.
		&model.CatalogPerson{},
		&model.CatalogCreditName{},
		&model.CatalogNameAlias{},
		&model.CatalogLabel{},
		&model.CatalogLabelAlias{},
		&model.CatalogLabelIntro{}, // multilingual label intros (refs/proj/83 E2b)
		&model.CatalogCharacter{},
		&model.CatalogCharacterAlias{},
		&model.CatalogCharacterIntro{},             // multilingual character intros (step 65 field PR C1)
		&model.CatalogCharacterIntroPanelVerdict{}, // P3 panel kept-verdict cache (wave 214 deferred item)
		&model.CatalogCharacterTrait{},             // VNDB trait vocabulary (step 93)
		&model.CatalogCharacterTraitParent{},       // trait hierarchy DAG edges (step 93, raw material)
		&model.CatalogCharacterTraitLink{},         // character×trait links (step 93, ~2.9M rows)
		&model.CatalogPersonIntro{},                // multilingual person intros (step 65 field PR C1)

		// Polymorphic infrastructure (no FKs by design).
		&model.CatalogRedirect{},
		&model.CatalogEntityUsage{},
		&model.CatalogRevision{},
		// Claim lifecycle log (refs/plans/10 03 §3). Polymorphic-adjacent: no
		// FK to catalog_work, because the transition history must outlive the
		// work row exactly as catalog_revision's snapshots do.
		&model.CatalogClaimEvent{},

		// Work graph (step 04): registry work/title/release, then edges.
		&model.CatalogWork{},
		&model.CatalogWorkTitle{},
		&model.CatalogWorkIntro{}, // bodyless multilingual intro (step 52 media-aggregation pilot)
		// Text safety (2026-10): LLM verdicts on titles and intros, keyed by a
		// content hash, plus catalog_work.title_nsfw (nullable; NULL = not judged
		// yet, and the display axis keeps reading the age rating for it). All
		// three are additive and start empty / NULL; cmd/text-safety fills them.
		&model.CatalogWorkTitleSafety{},
		&model.CatalogWorkIntroSafety{},
		// 2026-10-24: the same verdicts for character intros, keyed by
		// md5(catalog_character_intro.intro). New, empty table.
		&model.CatalogCharacterIntroSafety{},
		// Bodyless cover and screenshot images (steps 53/54). 2026-10-12 adds
		// merged_in boolean NOT NULL DEFAULT false to both: a work merge sets it
		// on the loser's rows it moves, and every cover election, and the
		// banner's screenshot fallback, ranks those rows below the survivor's
		// own. Before it the loser's sharper art won the survivor's portrait and
		// banner. AutoMigrate adds the column as metadata (PG 11+, no rewrite);
		// every existing row takes false, so rows moved by merges before this
		// change keep competing as the survivor's own — nothing records which
		// they were.
		&model.CatalogWorkCover{},
		&model.CatalogWorkScreenshot{},
		&model.CatalogCoverVote{}, // advisory best-cover votes (wave 175); its cover FK is raw SQL below
		// Editor UX wave (2026-10-20): the cover and screenshot order a person
		// sets across every source (catalog.work.cover_order / screenshot_order).
		// New and empty; a work with no rows keeps the order it was read in
		// before — covers by sort_order, screenshots by source then sort_order.
		&model.CatalogWorkMediaOrder{},
		// Bodyless source-native ratings (step 58a media-aggregation facet A).
		// Wave 205 added the nullable distribution/stats jsonb columns; both land
		// NULL on every existing row and stay that way until backfill-work-ratings
		// runs again (it is in the bgm weekly, so untouched trees self-heal there).
		&model.CatalogWorkRating{},
		&model.CatalogWorkTag{},            // bodyless verbatim folksonomy tags (step 58b media-aggregation facet B)
		&model.CatalogWorkPopularity{},     // bodyless per-metric popularity counters (step 62 popularity facet)
		&model.CatalogWorkPlaytime{},       // per-source playtime estimates (step 91 playtime facet — no claimed bridge)
		&model.CatalogUserPlaytime{},       // per-user/per-client playtime reports; aggregates INTO the row above as source nextmoe
		&model.CatalogUserWorkState{},      // per-user per-work play state (state/rating track P1); backfilled below from playtime statuses only while that column still exists
		&model.CatalogUserFolder{},         // per-user favorite folders (favorites unification wave); canonical store for forum+moyu favorites
		&model.CatalogUserFolderItem{},     // folder memberships; (owner_uid, updated_at) is the manager-sync cursor
		&model.CatalogUserFolderImport{},   // source-collection provenance for the favorites import (since retired); made that backfill re-runnable
		&model.CatalogUserEntityFollow{},   // per-user follows of catalog entities; companies only, see the CHECK
		&model.CatalogUserTagBlock{},       // per-user tag blocks that clients pass as exclude_tag_id / exclude_tag_tree (2026-10); new, no backfill
		&model.CatalogUserPrivacy{},        // per-account privacy switches (2026-10-13); created empty on purpose: no row means public, so every existing account's play states and playtimes become readable on /v2/work-records — a product decision taken knowing the forum had told its users play records were private
		&model.CatalogSeries{},             // work series entity (step 94, dlsite lane first)
		&model.CatalogSeriesMember{},       // series membership (step 94)
		&model.CatalogSeriesIntro{},        // multilingual series intros (refs/plans/10 W0 ruling 3)
		&model.CatalogSeriesNameOverride{}, // reviewed names for machine-owned lanes (wave 185)
		&model.CatalogPlatform{},           // platform vocabulary registry (step 96)
		&model.CatalogWorkPlatform{},       // work-level platform facet (step 96, bgm lane)
		// Per-user work ratings, one per person per work across every site
		// (2026-10). Created empty and filled once from the forum's
		// galgame_rating (ids kept) and LetMoe's game_rating by
		// cmd/import-user-ratings, deleted 2026-10-13 after all three sites
		// switched; its ledger table catalog_user_work_rating_import is no
		// longer created and is left in place on production until it is
		// dropped. Deletions leave a tombstone so site mirrors reading
		// /v2/ratings/changes can drop them.
		&model.CatalogUserWorkRating{},
		&model.CatalogUserWorkRatingTombstone{},
		// catalog_release. Wave R2b (2026-08-16) adds field_provenance jsonb
		// NOT NULL DEFAULT '{}' so human edits of kind/title/lang/platform/date
		// and hide/unhide survive importer backfills. AutoMigrate is enough:
		// the column has a default, so PG 11+ adds it as metadata without
		// rewriting existing rows, all of which take '{}' — "nobody has
		// claimed any column on this row".
		&model.CatalogRelease{},
		&model.CatalogWorkRelation{},
		&model.CatalogEntityRelation{},
		&model.CatalogWorkLabel{},    // work↔label attribution edge (step 18)
		&model.CatalogReleaseLabel{}, // release↔label attribution edge (wave 200)
		// work↔character roster edge (step 45). Wave R2c-2 (2026-08-16) adds
		// field_provenance jsonb NOT NULL DEFAULT '{}' — the first row-level
		// provenance column on a CHILD table, needed because kind/spoiler became
		// editable and merge survivorship would otherwise overwrite what a person
		// set. AutoMigrate is enough: the column has a default, so PG 11+ adds it
		// as metadata without rewriting the 218,846 existing rows, all of which
		// take '{}' — "nobody has claimed any column on this row".
		&model.CatalogWorkCharacter{},
		&model.CatalogLabelRelation{}, // label↔label corporate-structure edge (wave 186)

		// Engine facet (refs/plans/10 W0 ruling 4): the wiki family's engine
		// layer has no upstream to regenerate from, so it is migrated rather
		// than discarded. catalog_engine before catalog_work_engine so the
		// engine_id FK can be created.
		&model.CatalogEngine{},
		&model.CatalogWorkEngine{},

		// Tag canonical layer (step 74, doc 70a): the cross-source convergence
		// vocabulary ABOVE the original tag layers. catalog_tag before
		// catalog_tag_source_map so the tag_id FK can be created.
		&model.CatalogTag{},
		&model.CatalogTagSourceMap{},
		&model.CatalogTagIntro{}, // multilingual tag intros (refs/plans/10 W0 ruling 3)
		&model.CatalogTagRejection{},
		// Tag hierarchy DAG (2026-10 tag blocking): VNDB's tags_parents
		// projected onto canonical tags by the catalog-tag-parents job (origin
		// 0), plus reviewed edges for tags no VNDB tag maps to (origin 1).
		// New and empty; the job fills it on its first run.
		&model.CatalogTagParent{},
		// The tag chips' precomputed work_count. After catalog_tag, whose id it
		// keys on; derived data, so it carries no FK — a tag merge that removes
		// the tag simply leaves a row the refresh drops on its next pass.
		&model.CatalogTagWorkCount{},

		// Reconciliation family (step 04).
		&model.CatalogExternalRef{},
		&model.CatalogMatchRejection{},
		&model.CatalogMatchCandidate{},
		&model.CatalogMergeProposal{},
		// 2026-10-12: every row a merge drops instead of moving — the loser's
		// intros where the survivor already has one, machine titles, colliding
		// duplicates, and in absorb mode every imported row of the edition — is
		// written here as to_jsonb(row) in the merge's own transaction, keyed by
		// proposal, so MergeService.Unmerge can put it back on the rebuilt
		// entity. Until now a merge deleted those rows outright and an unmerge
		// restored only the work row and its titles. New and empty; nothing is
		// backfilled, since rows dropped by earlier merges are gone.
		&model.CatalogMergeArchive{},
		// 2026-10-21: undoing a merge under the source's original id. The
		// archive above only holds rows a merge dropped; rows it moved by
		// UPDATE left no trace, so an unmerge could not take them back and had
		// to mint a new id, which downstream sites (same ids as the catalog)
		// cannot follow. catalog_merge_movement records, per executed merge,
		// every row that referenced the source before it ran, every row the
		// merge inserted on the survivor, and both entity rows as they were.
		// catalog_merge_proposal gains amended_by/amended_at (a reviewer
		// changed the requester's field and release choices) and
		// undone_by/undone_at (status 5, undone); catalog_redirect gains
		// revoked_at, set when an undo hands the old id back. All additive and
		// empty: merges executed before this have no movement rows, and the
		// editor offers undo only where they exist. A movement row belongs to its
		// proposal and is deleted with it.
		&model.CatalogMergeMovement{},
		&model.CatalogSurvivorshipRule{},

		// Credit edges (step 05). 2026-10-21 adds field_provenance jsonb NOT NULL
		// DEFAULT '{}' for catalog.work.credits_upstream, the in-place correction
		// of an imported credit's note / spoiler / release / company. Until now
		// the only way was hide-and-re-add, which uq_catalog_credit refused. The
		// constant default means PG 11+ adds the column as metadata without
		// rewriting the ~1.02M existing rows, all of which take '{}' ("nobody has
		// claimed any column on this row").
		&model.CatalogCredit{},

		// 2026-10-24: walkthrough charts (new, empty). One row per crawled
		// walkthrough page (誠也の部屋 / 愚者の館) holding only the choice graph
		// the import job derived from it — entries, saves, choices, endings —
		// never the page prose. catalog_walkthrough_character pins each chart
		// character key to a catalog character, matched within the work's
		// roster or created name-only by the import. No existing row changes.
		// 2026-10-25: walkthroughs become editable and can be written from scratch,
		// so the table gains lang (the 4,048 imported rows take the 'ja' default,
		// which is what the read face had hard-coded) and field_provenance (all
		// take '{}': no field has been edited yet), and
		// catalog_walkthrough_character.matched_by gains the 'curated' value for a
		// pin a person set.
		// 2026-10-26: import_review (nullable, every existing row NULL) records
		// what the gate found missing from a chart the import kept hidden for an
		// editor to complete.
		&model.CatalogWalkthrough{},
		&model.CatalogWalkthroughCharacter{},

		// 2026-09-25: how far this service has read the deleted-accounts list
		// (oauth doc 17). A new table with no rows; the first run starts from
		// the beginning of the list.
		&accountpurge.Cursor{},
	); err != nil {
		return fmt.Errorf("catalog automigrate: %w", err)
	}
	// Editing-engine tables (E0, charter ruling 2): the edit_* family lives
	// on the catalog pool — single revision-log query surface — and rides
	// the same single migration entry point. The editing package owns its
	// DDL; catalog imports editing (platform-internal), never the reverse.
	if err := editing.AutoMigrate(db); err != nil {
		return fmt.Errorf("editing automigrate: %w", err)
	}
	// edit_discussion_post / edit_discussion_watcher (2026-10, editor phase 2):
	// new empty tables for threads under proposals and merge/split/deletion
	// requests. Additive; no existing rows are touched.
	if err := discussion.AutoMigrate(db); err != nil {
		return fmt.Errorf("discussion automigrate: %w", err)
	}
	if err := EditLegacyColumns(db); err != nil {
		return err
	}
	if err := dropRetiredOrg(db); err != nil {
		return err
	}
	if err := migrateDeletionRequests(db); err != nil {
		return err
	}
	if err := migrateSplitRequests(db); err != nil {
		return err
	}
	return rawSQL(db)
}

// dropRetiredOrg removes catalog_org and catalog_label.org_id (wave 195). It
// runs AFTER AutoMigrate on purpose: AutoMigrate is additive and would happily
// leave both behind forever, which is how a retired table survives a
// retirement.
//
// It REFUSES rather than drops if either holds data. Nothing has ever written
// them — the table has never held a row in any database, production included —
// so on every real catalogue this is a drop or a no-op. But a guarded drop and
// an unguarded one are indistinguishable right up to the day they are not, and
// the failure mode of the unguarded version is destroying rows nobody knew
// existed. If this ever fires, the answer is to look at what wrote them, not
// to delete the check.
//
// Idempotent: after the first run both objects are gone and every statement is
// a guarded no-op.
func dropRetiredOrg(db *gorm.DB) error {
	var orgRows, labelRefs int64
	if db.Migrator().HasTable("catalog_org") {
		if err := db.Raw(`SELECT count(*) FROM catalog_org`).Scan(&orgRows).Error; err != nil {
			return fmt.Errorf("count catalog_org: %w", err)
		}
	}
	if db.Migrator().HasColumn(&model.CatalogLabel{}, "org_id") {
		if err := db.Raw(`SELECT count(*) FROM catalog_label WHERE org_id IS NOT NULL`).Scan(&labelRefs).Error; err != nil {
			return fmt.Errorf("count catalog_label.org_id: %w", err)
		}
	}
	if orgRows > 0 || labelRefs > 0 {
		return fmt.Errorf("catalog_org retirement: refusing to drop, %d org rows and %d labels carry an org_id", orgRows, labelRefs)
	}
	// The column first — it carries the FK that would block the table drop.
	if err := db.Exec(`ALTER TABLE catalog_label DROP COLUMN IF EXISTS org_id`).Error; err != nil {
		return fmt.Errorf("drop catalog_label.org_id: %w", err)
	}
	if err := db.Exec(`DROP TABLE IF EXISTS catalog_org`).Error; err != nil {
		return fmt.Errorf("drop catalog_org: %w", err)
	}
	return nil
}

// EditLegacyColumns adds the strangler-migration bookkeeping columns to the
// engine tables (E2a, 03 号裁定 3/5). They are populated ONLY by the one-shot
// legacy transform (cmd/migrate-galgame-editing) — the engine and every new
// write path never set them:
//
//   - edit_revision.legacy_action:  the original galgame_revision action string
//     (created/updated/claimed/...) — honest provenance for migrated rows; the
//     engine action column keeps the 4-value vocabulary.
//   - edit_revision.legacy_id / edit_proposal.legacy_pr_id: the source-row PK.
//     Keys the transform's idempotent upsert AND keeps old wire ids stable
//     (revision feed cursors, PR permalinks) via COALESCE(legacy_id, id).
//   - legacy_meta (both): old-wire-only baggage the engine does not model
//     (revision note/is_minor/reverted_to/null-changed_fields marker; PR
//     title/message/original snapshot/base_revision).
//   - idx_edit_revision_wire_id: expression index over the wire id the
//     merged-revision feed (galgame/editquery) orders and cursors by.
//
// legacy_action / legacy_id back the surviving merged-revision feed; the rest
// (legacy_pr_id / legacy_meta) are frozen provenance the retired E3b old-wire
// adapter read, kept until the legacy tables drop. Exported so tests + the
// single migration entry point provision the exact production schema; the
// columns live on catalog-pool tables (charter ruling 9).
func EditLegacyColumns(db *gorm.DB) error {
	for _, stmt := range []string{
		`ALTER TABLE edit_revision ADD COLUMN IF NOT EXISTS legacy_action text`,
		`ALTER TABLE edit_revision ADD COLUMN IF NOT EXISTS legacy_id bigint`,
		`ALTER TABLE edit_revision ADD COLUMN IF NOT EXISTS legacy_meta jsonb`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_edit_revision_legacy_id
		    ON edit_revision(legacy_id) WHERE legacy_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_edit_revision_wire_id
		    ON edit_revision ((COALESCE(legacy_id, id)))`,
		`ALTER TABLE edit_proposal ADD COLUMN IF NOT EXISTS legacy_pr_id bigint`,
		`ALTER TABLE edit_proposal ADD COLUMN IF NOT EXISTS legacy_meta jsonb`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_edit_proposal_legacy_pr_id
		    ON edit_proposal(legacy_pr_id) WHERE legacy_pr_id IS NOT NULL`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("edit legacy columns: %w", err)
		}
	}
	return nil
}

// preMigrate runs BEFORE AutoMigrate: it adds NOT-NULL-without-default columns
// to already-populated tables (which AutoMigrate cannot — `ADD COLUMN … NOT
// NULL` with no default rejects a table with rows). Each block is guarded on
// the table already existing (a fresh DB has AutoMigrate create the table with
// the column NOT NULL from the model, no backfill needed) and is idempotent.
func preMigrate(db *gorm.DB) error {
	// catalog_work_character.spoiler (step 47): 0 (none) is a meaningful value,
	// so the column is NOT NULL with no default. On a table that already holds
	// step-45 roster edges, add it nullable → backfill 0 (existing Bangumi/EG
	// edges carry no spoiler) → set NOT NULL. Idempotent; skipped on a fresh DB
	// where the table does not exist yet (AutoMigrate creates it NOT NULL).
	if err := db.Exec(`
		DO $$
		BEGIN
			IF to_regclass('catalog_work_character') IS NOT NULL THEN
				ALTER TABLE catalog_work_character ADD COLUMN IF NOT EXISTS spoiler smallint;
				UPDATE catalog_work_character SET spoiler = 0 WHERE spoiler IS NULL;
				ALTER TABLE catalog_work_character ALTER COLUMN spoiler SET NOT NULL;
			END IF;
		END $$`).Error; err != nil {
		return fmt.Errorf("premigrate catalog_work_character.spoiler: %w", err)
	}

	// catalog_work_intro.provenance (step 75, MT pilot): 0=source / 1=machine is
	// a meaningful zero value, so the column is NOT NULL with no default. On a
	// table that already holds source-only intro rows (steps 52/55/57), add it
	// nullable → backfill 0 (every existing row IS upstream source text) → set
	// NOT NULL. Idempotent; skipped on a fresh DB where the table does not exist
	// yet (AutoMigrate then creates it NOT NULL from the model). The sibling
	// columns src_hash / mt_model carry a DEFAULT '' so AutoMigrate adds THEM to
	// the populated table directly — only the no-default provenance needs this.
	if err := db.Exec(`
		DO $$
		BEGIN
			IF to_regclass('catalog_work_intro') IS NOT NULL THEN
				ALTER TABLE catalog_work_intro ADD COLUMN IF NOT EXISTS provenance smallint;
				UPDATE catalog_work_intro SET provenance = 0 WHERE provenance IS NULL;
				ALTER TABLE catalog_work_intro ALTER COLUMN provenance SET NOT NULL;
			END IF;
		END $$`).Error; err != nil {
		return fmt.Errorf("premigrate catalog_work_intro.provenance: %w", err)
	}

	// catalog_{character,person,label}_intro.provenance (entity intro MT,
	// refs/proj/172): the same 0=source / 1=machine axis catalog_work_intro
	// grew in step 75, added to the three entity intro tables so machine
	// translations never masquerade as upstream source text. Same recipe:
	// meaningful zero → NOT NULL with no default → add nullable, backfill 0
	// (every existing row IS source text), set NOT NULL. src_hash / mt_model
	// carry DEFAULT '' and are AutoMigrate's to add.
	for _, t := range []string{"catalog_character_intro", "catalog_person_intro", "catalog_label_intro"} {
		if err := db.Exec(`
			DO $$
			BEGIN
				IF to_regclass('` + t + `') IS NOT NULL THEN
					ALTER TABLE ` + t + ` ADD COLUMN IF NOT EXISTS provenance smallint;
					UPDATE ` + t + ` SET provenance = 0 WHERE provenance IS NULL;
					ALTER TABLE ` + t + ` ALTER COLUMN provenance SET NOT NULL;
				END IF;
			END $$`).Error; err != nil {
			return fmt.Errorf("premigrate %s.provenance: %w", t, err)
		}
	}

	// catalog_{character,name,label}_alias.provenance (wave 195): the same
	// 0=source / 1=machine axis, carried onto the three alias tables BEFORE any
	// machine lane exists. That order is the whole point — the 0 backfill is
	// only provably correct while every row in these tables is still a name a
	// human source wrote, which is true today and stops being true the moment a
	// fallback-translation lane runs once. Afterwards the same UPDATE would be a
	// guess, and nothing in the table could tell the guess from the fact.
	// Same recipe as the intro tables: meaningful zero → NOT NULL, no default →
	// add nullable, backfill 0, set NOT NULL. The sibling source_id (nullable)
	// and mt_model (DEFAULT '') are AutoMigrate's to add.
	for _, t := range []string{"catalog_character_alias", "catalog_name_alias", "catalog_label_alias"} {
		if err := db.Exec(`
			DO $$
			BEGIN
				IF to_regclass('` + t + `') IS NOT NULL THEN
					ALTER TABLE ` + t + ` ADD COLUMN IF NOT EXISTS provenance smallint;
					UPDATE ` + t + ` SET provenance = 0 WHERE provenance IS NULL;
					ALTER TABLE ` + t + ` ALTER COLUMN provenance SET NOT NULL;
				END IF;
			END $$`).Error; err != nil {
			return fmt.Errorf("premigrate %s.provenance: %w", t, err)
		}
	}

	// catalog_work_title.provenance (wave 210): the same 0=source / 1=machine
	// axis the intro and alias tables carry, added BEFORE the work-title MT lane
	// exists — the 0 backfill is only provably correct while every title row is
	// still one a human source wrote. The 605 forum rows the wave reclassifies to
	// 1 are re-marked by the lane afterwards, from evidence, not by this UPDATE.
	// Same recipe: meaningful zero → NOT NULL with no default → add nullable,
	// backfill 0, set NOT NULL. src_hash / mt_model are NULLABLE here (only a
	// machine row carries them), so AutoMigrate adds them by itself.
	if err := db.Exec(`
		DO $$
		BEGIN
			IF to_regclass('catalog_work_title') IS NOT NULL THEN
				ALTER TABLE catalog_work_title ADD COLUMN IF NOT EXISTS provenance smallint;
				UPDATE catalog_work_title SET provenance = 0 WHERE provenance IS NULL;
				ALTER TABLE catalog_work_title ALTER COLUMN provenance SET NOT NULL;
			END IF;
		END $$`).Error; err != nil {
		return fmt.Errorf("premigrate catalog_work_title.provenance: %w", err)
	}

	// The W1-pre nativization columns (refs/proj/140,
	// refs/plans/10-data-layer-retirement/02-w1pre-bridge-nativization.md): three
	// axes that used to exist ONLY in the wiki body the read face bridged, and that
	// nativizing those bridges needs on the catalog's own tables.
	//
	//	catalog_work_tag.{spoiler,sexual}  the tag SAFETY axis (galgame_tag_relation
	//	                                   .spoiler_level + galgame_tag.category)
	//	catalog_tag.sexual                 the same flag on the canonical vocabulary
	//	catalog_work.display_nsfw          the EDITORIAL DISPLAY axis (A2-R5's
	//	                                   galgame.content_limit = 'nsfw')
	//
	// Every one carries a MEANINGFUL zero (0 = no spoiler, false = not
	// sexual-category / no NSFW display material), so they are NOT NULL with NO
	// default — which is exactly what AutoMigrate cannot add to a populated table.
	//
	// The 0/false backfill is not a placeholder, it is the correct value of the rows
	// that are already there. The tag rows are Bangumi/DLsite folksonomy and the
	// canonical vocabulary they map to, and neither source publishes a safety axis.
	// A work's false means "no editorial declaration of NSFW display material",
	// which is what the display projection already read for a bodyless work (it
	// consults the age axis instead) and for a claimed work whose body says anything
	// other than 'nsfw' — the mirror step then writes the claimed rows' real value.
	for _, c := range []struct{ table, column, typ, zero string }{
		{"catalog_work_tag", "spoiler", "smallint", "0"},
		{"catalog_work_tag", "sexual", "boolean", "false"},
		{"catalog_tag", "sexual", "boolean", "false"},
		{"catalog_work", "display_nsfw", "boolean", "false"},
	} {
		if err := db.Exec(`
			DO $$
			BEGIN
				IF to_regclass('` + c.table + `') IS NOT NULL THEN
					ALTER TABLE ` + c.table + ` ADD COLUMN IF NOT EXISTS ` + c.column + ` ` + c.typ + `;
					UPDATE ` + c.table + ` SET ` + c.column + ` = ` + c.zero + ` WHERE ` + c.column + ` IS NULL;
					ALTER TABLE ` + c.table + ` ALTER COLUMN ` + c.column + ` SET NOT NULL;
				END IF;
			END $$`).Error; err != nil {
			return fmt.Errorf("premigrate %s.%s: %w", c.table, c.column, err)
		}
	}
	return nil
}

// rawSQL is the post-AutoMigrate section — everything AutoMigrate cannot
// express. Every statement is idempotent, so this section reruns freely.
func rawSQL(db *gorm.DB) error {
	// (1) NFKC-folded STORED generated columns. Built while the tables are
	// empty: adding a STORED generated column later means a full table
	// rewrite. No indexes on the norm columns yet — the consuming steps add
	// the ones their queries need. The models map these columns read-only
	// and exclude them from AutoMigrate.
	for _, tc := range []struct{ table, column string }{
		{"catalog_credit_name", "name"},
		{"catalog_name_alias", "name"},
		{"catalog_label_alias", "name"},
		{"catalog_character_alias", "name"},
		{"catalog_label", "display_name"},
		{"catalog_character", "display_name"},
		{"catalog_work_title", "title"},
	} {
		stmt := `ALTER TABLE ` + tc.table + ` ADD COLUMN IF NOT EXISTS ` + tc.column + `_norm text
			GENERATED ALWAYS AS (lower(normalize(` + tc.column + `, NFKC))) STORED`
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("create %s.%s_norm: %w", tc.table, tc.column, err)
		}
	}

	// (2) person → credit_name FK (the second half of the mutual reference).
	// Guarded because ADD CONSTRAINT has no IF NOT EXISTS.
	fkExists, err := constraintExists(db, "catalog_person", "fk_catalog_person_primary_credit_name")
	if err != nil {
		return err
	}
	if !fkExists {
		if err := db.Exec(`
			ALTER TABLE catalog_person
			    ADD CONSTRAINT fk_catalog_person_primary_credit_name
			    FOREIGN KEY (primary_credit_name_id) REFERENCES catalog_credit_name(id)
		`).Error; err != nil {
			return fmt.Errorf("add primary_credit_name FK: %w", err)
		}
	}

	// (3) catalog_entity_usage is a hot-update narrow table: reserve page
	// space so last_confirmed_at rewrites stay HOT (same setting as the
	// image usage table pattern). Setting the same value again is a no-op.
	if err := db.Exec(`ALTER TABLE catalog_entity_usage SET (fillfactor = 85)`).Error; err != nil {
		return fmt.Errorf("set entity_usage fillfactor: %w", err)
	}

	// (4) Table-layer CHECK constraints. ADD CONSTRAINT validates existing
	// rows, which is instant on these empty/small tables.
	for _, cc := range []struct{ table, name, expr string }{
		// extra jsonb governance (doc 17 R9): object-only + 64KB cap — the
		// stock-PG stand-in for a pg_jsonschema key whitelist.
		{"catalog_work", "chk_catalog_work_extra_object", `jsonb_typeof(extra) = 'object'`},
		{"catalog_work", "chk_catalog_work_extra_size", `pg_column_size(extra) <= 65536`},
		{"catalog_release", "chk_catalog_release_extra_object", `jsonb_typeof(extra) = 'object'`},
		{"catalog_release", "chk_catalog_release_extra_size", `pg_column_size(extra) <= 65536`},
		// Relation edges never point at themselves.
		{"catalog_work_relation", "chk_catalog_work_relation_distinct", `a_work_id <> b_work_id`},
		{"catalog_entity_relation", "chk_catalog_entity_relation_distinct", `a_id <> b_id`},
		// Candidate pairs are normalized a<b — pinned at the table layer.
		{"catalog_match_candidate", "chk_catalog_match_candidate_order", `a_id < b_id`},
		// A rejection without a reason is useless: the row exists to tell
		// future importers and reviewers why the pairing is wrong.
		{"catalog_match_rejection", "chk_catalog_match_rejection_reason", `reason <> ''`},
		// Only companies may be followed until a person lane exists, and opening one also needs its merge rehang statements.
		{"catalog_user_entity_follow", "chk_catalog_user_entity_follow_type", "entity_type IN (3)"},
		{"catalog_tag_parent", "chk_catalog_tag_parent_distinct", `tag_id <> parent_id`},
		{"catalog_tag_parent", "chk_catalog_tag_parent_origin", `origin IN (0, 1)`},
		{"catalog_user_tag_block", "chk_catalog_user_tag_block_mode", `mode IN (0, 1)`},
		{"catalog_user_work_rating", "chk_catalog_user_work_rating_overall", `overall BETWEEN 1 AND 10`},
		{"catalog_user_work_rating", "chk_catalog_user_work_rating_aspects", `COALESCE(art, 1) BETWEEN 1 AND 10 AND COALESCE(story, 1) BETWEEN 1 AND 10 AND
			COALESCE(music, 1) BETWEEN 1 AND 10 AND COALESCE("character", 1) BETWEEN 1 AND 10 AND
			COALESCE(route, 1) BETWEEN 1 AND 10 AND COALESCE("system", 1) BETWEEN 1 AND 10 AND
			COALESCE(voice, 1) BETWEEN 1 AND 10 AND COALESCE(replay_value, 1) BETWEEN 1 AND 10`},
		{"catalog_user_work_rating", "chk_catalog_user_work_rating_recommend", `recommend BETWEEN 1 AND 5`},
		{"catalog_user_work_rating", "chk_catalog_user_work_rating_spoiler", `spoiler_level IN (0, 1, 2)`},
		// 2026-10-24, new empty table: the chart is a JSON object, and the largest
		// extracted chart was far below 512KB.
		{"catalog_walkthrough", "chk_catalog_walkthrough_chart_object", `jsonb_typeof(chart) = 'object'`},
		{"catalog_walkthrough", "chk_catalog_walkthrough_chart_size", `pg_column_size(chart) <= 524288`},
		{"catalog_walkthrough", "chk_catalog_walkthrough_status", `status IN (0, 1)`},
		// 2026-10-25: every existing row is 'ja' through the column default.
		{"catalog_walkthrough", "chk_catalog_walkthrough_lang", `lang IN ('ja', 'zh-Hans')`},
	} {
		exists, err := constraintExists(db, cc.table, cc.name)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if err := db.Exec(`ALTER TABLE ` + cc.table + ` ADD CONSTRAINT ` + cc.name + ` CHECK (` + cc.expr + `)`).Error; err != nil {
			return fmt.Errorf("add check %s on %s: %w", cc.name, cc.table, err)
		}
	}

	// cmd/import-user-ratings (deleted 2026-10-13) kept the forum's rating ids
	// (4,814 at most in 2026-10), so the ids the catalog issues itself start at
	// 1,000,001: a rating written through /v2 before the import would otherwise
	// have taken an id the import needed. GREATEST never lowers the sequence, so
	// the statement is safe on every run.
	if err := db.Exec(`SELECT setval(seq, GREATEST(COALESCE(pg_sequence_last_value(seq::regclass), 0), 1000000))
		FROM pg_get_serial_sequence('catalog_user_work_rating', 'id') AS seq`).Error; err != nil {
		return fmt.Errorf("floor catalog_user_work_rating ids: %w", err)
	}

	// (5) Partial/expression unique indexes.
	for _, ix := range []struct{ name, stmt string }{
		// The exact-tier anti-squatting line (doc 10 invariant 5): one
		// external identity can be exact-linked to at most one entity per
		// entity_type. Partial — probable/related tiers coexist freely.
		{"uq_catalog_external_ref_exact", `
			CREATE UNIQUE INDEX IF NOT EXISTS uq_catalog_external_ref_exact
			    ON catalog_external_ref(source_id, external_id, entity_type)
			    WHERE link_kind = 0`},
		// One OPEN merge proposal per (entity_type, source, target) pair;
		// closed proposals (approved/executed/rejected/withdrawn) keep full
		// history.
		{"uq_catalog_merge_proposal_open", `
			CREATE UNIQUE INDEX IF NOT EXISTS uq_catalog_merge_proposal_open
			    ON catalog_merge_proposal(entity_type, source_entity_id, target_entity_id)
			    WHERE status = 0`},
		// Credit uniqueness (doc 10 §4.5): expression index because NULL
		// character_id must collide (COALESCE to 0), which a plain UNIQUE
		// would treat as distinct.
		{"uq_catalog_credit", `
			CREATE UNIQUE INDEX IF NOT EXISTS uq_catalog_credit
			    ON catalog_credit(work_id, credit_name_id, role_id, COALESCE(character_id, 0))`},
	} {
		if err := db.Exec(ix.stmt).Error; err != nil {
			return fmt.Errorf("create index %s: %w", ix.name, err)
		}
	}

	// (6) Plain secondary indexes for the canonical public read face (doc 106
	// W1). Both are pure query-support indexes — no semantics, rerun freely.
	for _, ix := range []struct{ name, stmt string }{
		// Reverse lookup canonical tag → works: catalog_tag_source_map keys
		// (source_id, source_name) and the UNIQUE on catalog_work_tag leads
		// with work_id, so a (name, source_id) probe needs its own index
		// (GET /v1/catalog/tags/{id} include=works + works?tag_id=).
		{"idx_catalog_work_tag_name_source", `
			CREATE INDEX IF NOT EXISTS idx_catalog_work_tag_name_source
			    ON catalog_work_tag(name, source_id)`},
		// Keyset lanes for the public works list (sort=updated) and the
		// changes feed ((updated_at, id) ascending resume cursor).
		{"idx_catalog_work_updated_id", `
			CREATE INDEX IF NOT EXISTS idx_catalog_work_updated_id
			    ON catalog_work(updated_at, id)`},
		// Series-closure walk (wave 117): loadSeriesSiblings recurses over
		// same_series edges on EVERY work detail read, probing one node at a
		// time from both endpoint columns. Partial + INCLUDE keeps the whole
		// recursion on Index Only Scans (~112 kB each) instead of two full
		// relation-table scans per round.
		//
		// The literal 7 is catalog_relation_type.id for key "same_series",
		// pinned by seed.go (`{ID: 7, Key: "same_series", ...}`) and mirrored
		// by service.seriesRelationTypeID. That coupling is deliberate: type 7
		// is this index's only consumer, so a partial index is the smallest
		// thing that works. Changing the seed id means changing these two
		// index predicates too.
		{"idx_catalog_work_relation_series_a", `
			CREATE INDEX IF NOT EXISTS idx_catalog_work_relation_series_a
			    ON catalog_work_relation (a_work_id) INCLUDE (b_work_id)
			    WHERE relation_type_id = 7`},
		{"idx_catalog_work_relation_series_b", `
			CREATE INDEX IF NOT EXISTS idx_catalog_work_relation_series_b
			    ON catalog_work_relation (b_work_id) INCLUDE (a_work_id)
			    WHERE relation_type_id = 7`},
		// GET /v2/catalog/characters?trait_id= EXISTS probe. Without it a rare
		// trait walks all ~200k characters by primary key (measured ~750 ms for
		// a 5-character trait on a production-sized copy); with it the probe is
		// driven from the trait side and stays index-only.
		{"idx_catalog_character_trait_link_trait_char", `
			CREATE INDEX IF NOT EXISTS idx_catalog_character_trait_link_trait_char
			    ON catalog_character_trait_link (trait_id, character_id) INCLUDE (spoiler_level)`},
		// Every cover election (work detail, every works list, calendar,
		// search hydration) counts how many live works hold each candidate
		// image, so a bundle's box art loses to the work's own (2026-10-12,
		// forum todo #231). uq_catalog_work_cover leads with work_id, so that
		// count was a full scan of catalog_work_cover per request — ~50 ms on a
		// 400k-row synthetic copy, ~1 ms with this index.
		{"idx_catalog_work_cover_image_hash", `
			CREATE INDEX IF NOT EXISTS idx_catalog_work_cover_image_hash
			    ON catalog_work_cover (image_hash)`},
	} {
		if err := db.Exec(ix.stmt).Error; err != nil {
			return fmt.Errorf("create index %s: %w", ix.name, err)
		}
	}

	// (7) cover vote → cover FK with ON DELETE CASCADE (wave 175). The column is
	// a plain bigint on the model (a GORM association would copy the referenced
	// identity PK's full type onto it), so the referential rule is declared here.
	// CASCADE is the whole cleanup story: a vote for a cover that no longer
	// exists is not a vote, and no deletion path — editorial, merge or GC — has
	// to remember this table. Guarded because ADD CONSTRAINT has no IF NOT EXISTS.
	voteFK, err := constraintExists(db, "catalog_cover_vote", "fk_catalog_cover_vote_cover")
	if err != nil {
		return err
	}
	if !voteFK {
		if err := db.Exec(`
			ALTER TABLE catalog_cover_vote
			    ADD CONSTRAINT fk_catalog_cover_vote_cover
			    FOREIGN KEY (cover_id) REFERENCES catalog_work_cover(id) ON DELETE CASCADE
		`).Error; err != nil {
			return fmt.Errorf("add cover vote FK: %w", err)
		}
	}

	// (7b) user playtime → work FK with ON DELETE CASCADE. Same reasoning as
	// the vote FK above: the column is a plain bigint on the model, and a
	// report about a work that no longer exists is not a report. A merge
	// rehangs rows onto the survivor before the loser is deleted, so CASCADE
	// only ever fires on a genuine deletion.
	playtimeFK, err := constraintExists(db, "catalog_user_playtime", "fk_catalog_user_playtime_work")
	if err != nil {
		return err
	}
	if !playtimeFK {
		if err := db.Exec(`
			ALTER TABLE catalog_user_playtime
			    ADD CONSTRAINT fk_catalog_user_playtime_work
			    FOREIGN KEY (work_id) REFERENCES catalog_work(id) ON DELETE CASCADE
		`).Error; err != nil {
			return fmt.Errorf("add user playtime FK: %w", err)
		}
	}

	// (7c) user work state → work FK, same reasoning and same CASCADE as 7b.
	workStateFK, err := constraintExists(db, "catalog_user_work_state", "fk_catalog_user_work_state_work")
	if err != nil {
		return err
	}
	if !workStateFK {
		if err := db.Exec(`
			ALTER TABLE catalog_user_work_state
			    ADD CONSTRAINT fk_catalog_user_work_state_work
			    FOREIGN KEY (work_id) REFERENCES catalog_work(id) ON DELETE CASCADE
		`).Error; err != nil {
			return fmt.Errorf("add user work state FK: %w", err)
		}
	}

	// (7d) 2026-10-24, new empty tables: a walkthrough belongs to its work and
	// its character pins to both ends. A merge rehangs work_id / character_id
	// onto the survivor before the loser row is deleted, so CASCADE only fires
	// on a genuine deletion.
	for _, fk := range []struct{ table, name, stmt string }{
		{"catalog_walkthrough", "fk_catalog_walkthrough_work",
			`FOREIGN KEY (work_id) REFERENCES catalog_work(id) ON DELETE CASCADE`},
		{"catalog_walkthrough", "fk_catalog_walkthrough_source",
			`FOREIGN KEY (source_id) REFERENCES catalog_source(id)`},
		{"catalog_walkthrough_character", "fk_catalog_walkthrough_character_walkthrough",
			`FOREIGN KEY (walkthrough_id) REFERENCES catalog_walkthrough(id) ON DELETE CASCADE`},
		{"catalog_walkthrough_character", "fk_catalog_walkthrough_character_character",
			`FOREIGN KEY (character_id) REFERENCES catalog_character(id) ON DELETE CASCADE`},
	} {
		exists, err := constraintExists(db, fk.table, fk.name)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if err := db.Exec(`ALTER TABLE ` + fk.table + ` ADD CONSTRAINT ` + fk.name + ` ` + fk.stmt).Error; err != nil {
			return fmt.Errorf("add %s: %w", fk.name, err)
		}
	}

	// State/rating track P1 (2026-09): fold the v1-era playtime statuses into
	// catalog_user_work_state. Only deliberate statuses migrate — finished(1),
	// dropped(2), on_hold(3). playing(0) rows carry no intent: the v2 PUT has
	// hardcoded 0 since the v1 retirement (2026-08-27), and before that it was
	// the form default. Any finished beats dropped beats on_hold across a
	// user's clients. DO NOTHING keeps re-runs from clobbering later explicit
	// writes.
	//
	// catalog_user_playtime.status is retired. The fold runs only while the
	// column still exists; the DROP COLUMN immediately below is why. A re-run
	// after the drop must not error: a fresh database never had the column
	// (the model no longer declares it), production already folded, and a
	// straggler still holding the column still needs the fold.
	hasStatusCol, err := columnExists(db, "catalog_user_playtime", "status")
	if err != nil {
		return err
	}
	if hasStatusCol {
		if err := db.Exec(`
			INSERT INTO catalog_user_work_state (actor_uid, work_id, state, created_at, updated_at)
			SELECT actor_uid, work_id,
			       CASE WHEN bool_or(status = 1) THEN 3
			            WHEN bool_or(status = 2) THEN 5
			            ELSE 4 END,
			       MIN(created_at), MAX(updated_at)
			  FROM catalog_user_playtime
			 WHERE status IN (1, 2, 3)
			 GROUP BY actor_uid, work_id
			ON CONFLICT (actor_uid, work_id) DO NOTHING`).Error; err != nil {
			return fmt.Errorf("backfill catalog_user_work_state: %w", err)
		}
	}
	if err := db.Exec(`ALTER TABLE catalog_user_playtime DROP COLUMN IF EXISTS status`).Error; err != nil {
		return fmt.Errorf("drop catalog_user_playtime.status: %w", err)
	}

	// (9) The editorial display axis's derived half — see cover_art_grade.go.
	if err := coverArtGrade(db); err != nil {
		return err
	}

	// (8) lang tags that are not language tags (wave 195). Four rows hold a
	// language NAME where the column's declared vocabulary is BCP-47 — the
	// author answered "which language" in prose instead of in codes:
	//
	//	catalog_label       36162  'japanese'   catalog_label 36184 'ja,ch'
	//	catalog_label       36164  '日语'        catalog_label_alias 2686 '日语'
	//
	// This is not a data-quality opinion, it is the column violating its own
	// type, which is why it heals here rather than in a cmd/heal one-shot: a
	// consumer that reads lang as BCP-47 cannot match any of these, so wave 192
	// had to teach the read face to DROP them from localized{} — the face is
	// still carrying that workaround for four rows. Each UPDATE matches the
	// exact malformed string, so it is a no-op on every later run and can never
	// widen. 'ja' is not an inference: it is what each author asserted, with
	// 'ja,ch' reduced to the primary of the two it crammed into a single-value
	// column. The Chinese half of that pair (alias 2687 黄鸭组, lang '') is left
	// alone — an empty lang is "unrecorded", which is legal, not malformed.
	for _, fix := range []struct{ table, bad string }{
		{"catalog_label", "japanese"},
		{"catalog_label", "日语"},
		{"catalog_label", "ja,ch"},
		{"catalog_label_alias", "日语"},
	} {
		if err := db.Exec(
			`UPDATE `+fix.table+` SET lang = 'ja' WHERE lang = ?`, fix.bad,
		).Error; err != nil {
			return fmt.Errorf("heal %s.lang %q: %w", fix.table, fix.bad, err)
		}
	}

	// catalog_character_trait.sexual_family is the published is_sexual gate: a
	// trait is in the sexual family when it is sexual itself or any ancestor
	// through catalog_character_trait_parent is. Three upstream rows sit under a
	// sexual parent or group with sexual=false (Off-screen Rape 2195, Off-screen
	// Sex Only 2417, Naked in Front of an Audience 1551) and leaked into SFW
	// responses when the gate read `sexual`. The statement is idempotent: a
	// second run updates no rows.
	if err := db.Exec(model.TraitSexualFamilySQL).Error; err != nil {
		return fmt.Errorf("backfill catalog_character_trait.sexual_family: %w", err)
	}

	// (10) Engines at release grain (2026-09-28). VNDB records an engine per
	// release, and the only engine layer the catalog had was the 2,755 work
	// edges migrated from the wiki. catalog_release.engine_id is nullable, so
	// AutoMigrate adds it to the populated table without a rewrite and every
	// existing row starts unrecorded; import-work-engines fills it from VNDB.
	// The FK is declared here like the vote FK in (7); ON DELETE SET NULL
	// because a deleted engine leaves a release unrecorded, not gone.
	// catalog_work_engine's unique key grows source_id
	// (uq_catalog_work_engine_source, created by AutoMigrate) so a curated row
	// and a Bangumi row may name the same engine; the old two-column key is
	// dropped. Every existing row satisfies the wider key.
	releaseEngineFK, err := constraintExists(db, "catalog_release", "fk_catalog_release_engine")
	if err != nil {
		return err
	}
	if !releaseEngineFK {
		if err := db.Exec(`
			ALTER TABLE catalog_release
			    ADD CONSTRAINT fk_catalog_release_engine
			    FOREIGN KEY (engine_id) REFERENCES catalog_engine(id) ON DELETE SET NULL
		`).Error; err != nil {
			return fmt.Errorf("add release engine FK: %w", err)
		}
	}
	if err := db.Exec(`DROP INDEX IF EXISTS uq_catalog_work_engine`).Error; err != nil {
		return fmt.Errorf("drop uq_catalog_work_engine: %w", err)
	}
	return nil
}

// constraintExists reports whether a named constraint exists on a table.
func constraintExists(db *gorm.DB, table, name string) (bool, error) {
	var exists bool
	if err := db.Raw(
		`SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = ? AND conrelid = ?::regclass)`,
		name, table,
	).Scan(&exists).Error; err != nil {
		return false, fmt.Errorf("check constraint %s on %s: %w", name, table, err)
	}
	return exists, nil
}

func columnExists(db *gorm.DB, table, column string) (bool, error) {
	var n int64
	if err := db.Raw(
		`SELECT count(*) FROM information_schema.columns WHERE table_name = ? AND column_name = ?`,
		table, column,
	).Scan(&n).Error; err != nil {
		return false, fmt.Errorf("check column %s on %s: %w", column, table, err)
	}
	return n > 0, nil
}
