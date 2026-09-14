package migrate

import (
	"fmt"
	"regexp"
	"strings"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

// catalog_work.cover_art_all_explicit answers one question about a work: does
// it own electable cover art, all of which is graded explicit? A work in that
// state cannot honour a place on the SFW shelf — the election has nothing safe
// to put in the portrait slot and falls through to the blurred 'censored'
// stand-in, so in production work 208100 showed its real cover to nobody, not
// even to a reader in NSFW mode.
//
// The write that produces this is the CLAIM, which is worth knowing because it
// leaves no trace to find it by. An unclaimed work takes its shelf from
// content_rating, which an r18 game cannot get wrong; claiming it swaps that
// for display_nsfw, which defaults to false. Five works arrived this way in the
// fourteen hours before this shipped — 211105, 217049, 222397, 208100, 229339 —
// with no revision, no cover write and no re-grade among them.
//
// Three decisions are worth recording, because each one closes a way this was
// going to rot:
//
//   - A trigger, not Go. Sixty-odd cmd/ binaries write catalog_work_cover, and
//     the nightly image grader rewrites `sexual` long after any request has
//     ended. Enumerating the writers is how the previous attempt ended up with
//     961 works on the wrong shelf; the database is the only place every writer
//     passes through.
//   - The column is read-only to GORM (model tag `->;-:migration`), which is
//     why it is created here rather than by AutoMigrate. A Save() carrying a
//     stale struct would otherwise hand the work back to the SFW shelf.
//   - A flip bumps updated_at. Downstream sites mirror the display axis into a
//     local column and poll GET /v2/catalog/changes, which orders by updated_at
//     — a flip that does not bump leaves every mirror stale forever.
//
// The `censored` source is excluded because those rows ARE the fallback, and
// the packaging kinds because the election skips them: a photo of the box is
// not a safe cover. Both lists are interpolated from the Go constants so this
// file cannot drift from model.PackagingCoverKinds.
func coverArtGrade(db *gorm.DB) error {
	if err := db.Exec(`ALTER TABLE catalog_work
		ADD COLUMN IF NOT EXISTS cover_art_all_explicit boolean NOT NULL DEFAULT false`).Error; err != nil {
		return fmt.Errorf("create catalog_work.cover_art_all_explicit: %w", err)
	}
	fn, err := coverArtGradeFunctionSQL()
	if err != nil {
		return err
	}
	if err := db.Exec(fn).Error; err != nil {
		return fmt.Errorf("create catalog_work_cover_art_grade(): %w", err)
	}
	if err := db.Exec(coverArtGradeTruncateSQL).Error; err != nil {
		return fmt.Errorf("create catalog_work_cover_art_grade_truncate(): %w", err)
	}
	for _, t := range coverArtGradeTriggers() {
		if err := db.Exec(`DROP TRIGGER IF EXISTS ` + t.name + ` ON catalog_work_cover`).Error; err != nil {
			return fmt.Errorf("drop trigger %s: %w", t.name, err)
		}
		if err := db.Exec(t.stmt).Error; err != nil {
			return fmt.Errorf("create trigger %s: %w", t.name, err)
		}
	}
	return backfillCoverArtGrade(db)
}

// shelfBeforeSQL is the display axis as it stood before cover_art_all_explicit
// existed, frozen. The backfill needs it to tell the rows whose stored value
// changes from the rows whose published content_limit changes with it: only
// the latter are worth a bump, and only a row that was sfw can move.
var shelfBeforeSQL = `(CASE
	WHEN (site IS NOT NULL AND site <> '' AND product_work_id IS NOT NULL) THEN display_nsfw
	ELSE content_rating = ` + contentRatingR18SQL + `
	END)`

// backfillCoverArtGrade seeds the column for rows that predate the trigger.
// Both statements are guarded on a real change, so every later run touches
// nothing — the trigger has kept the column current since the first one.
//
// The bump is conditional because both unconditional answers are wrong.
// Bumping every flipped row would replay 16,329 works through
// GET /v2/catalog/changes and float them to the top of every recently-updated
// face, for works whose shelf did not move. Bumping none would leave a work
// whose content_limit really did move invisible to every downstream mirror
// forever. On production 2026-09-14 the first run flipped 16,329 rows and
// exactly one of them moved shelf: work 229339, claimed 25 minutes earlier.
func backfillCoverArtGrade(db *gorm.DB) error {
	allExplicit, err := CoverArtAllExplicitSQL()
	if err != nil {
		return err
	}
	// Either direction moves the shelf exactly when the row was sfw without the
	// new clause, so both statements carry the same CASE.
	bump := `updated_at = CASE WHEN ` + shelfBeforeSQL + ` THEN updated_at ELSE now() END`
	for _, stmt := range []string{
		`UPDATE catalog_work SET cover_art_all_explicit = true, ` + bump + `
		  WHERE NOT cover_art_all_explicit AND id IN (` + allExplicit + `)`,
		`UPDATE catalog_work SET cover_art_all_explicit = false, ` + bump + `
		  WHERE cover_art_all_explicit AND id NOT IN (` + allExplicit + `)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("backfill cover_art_all_explicit: %w", err)
		}
	}
	return nil
}

var contentRatingR18SQL = fmt.Sprint(model.ContentRatingR18)

// coverArtRowSQL is the "this row is electable cover art" predicate, shared by
// the trigger and the backfill so they cannot disagree about what counts.
func coverArtRowSQL(cover, source string) (string, error) {
	kinds, err := packagingKindArraySQL()
	if err != nil {
		return "", err
	}
	return source + `.key <> '` + model.CensoredSourceKey + `' AND ` + cover + `.image_hash <> ''
		   AND ` + cover + `.kind <> ALL (` + kinds + `)`, nil
}

var safeSQLLiteral = regexp.MustCompile(`^[a-z0-9_]+$`)

func packagingKindArraySQL() (string, error) {
	quoted := make([]string, 0, len(model.PackagingCoverKinds))
	for _, k := range model.PackagingCoverKinds {
		if !safeSQLLiteral.MatchString(k) {
			return "", fmt.Errorf("packaging cover kind %q is not a bare identifier and cannot be inlined into the trigger", k)
		}
		quoted = append(quoted, "'"+k+"'")
	}
	return "ARRAY[" + strings.Join(quoted, ", ") + "]::text[]", nil
}

func coverArtGradeFunctionSQL() (string, error) {
	art, err := coverArtRowSQL("c", "s")
	if err != nil {
		return "", err
	}
	exists := func(extra string) string {
		return `EXISTS (SELECT 1 FROM catalog_work_cover c
			                     JOIN catalog_source s ON s.id = c.source_id
			                    WHERE c.work_id = t.id AND ` + art + extra + `)`
	}
	return `CREATE OR REPLACE FUNCTION catalog_work_cover_art_grade() RETURNS trigger
	LANGUAGE plpgsql AS $fn$
	DECLARE
		touched bigint[];
	BEGIN
		-- Sorted so concurrent statements take the catalog_work row locks in one
		-- order and cannot deadlock against each other.
		IF TG_OP = 'INSERT' THEN
			touched := ARRAY(SELECT DISTINCT work_id FROM graded_new ORDER BY 1);
		ELSIF TG_OP = 'DELETE' THEN
			touched := ARRAY(SELECT DISTINCT work_id FROM graded_old ORDER BY 1);
		ELSE
			touched := ARRAY(SELECT work_id FROM (
				SELECT work_id FROM graded_new UNION SELECT work_id FROM graded_old
			) u ORDER BY 1);
		END IF;
		UPDATE catalog_work w
		   SET cover_art_all_explicit = g.all_explicit, updated_at = now()
		  FROM (
			SELECT t.id AS work_id,
			       ` + exists("") + `
			       AND NOT ` + exists(` AND c.sexual < `+fmt.Sprint(model.SexualExplicit)) + ` AS all_explicit
			  FROM unnest(touched) AS t(id)
		  ) g
		 WHERE w.id = g.work_id AND w.cover_art_all_explicit IS DISTINCT FROM g.all_explicit;
		RETURN NULL;
	END
	$fn$`, nil
}

// coverArtGradeTruncateSQL is not paranoia: TRUNCATE fires neither the row nor
// the statement triggers above, so emptying catalog_work_cover on its own would
// leave every work claiming its art is explicit when it owns none.
const coverArtGradeTruncateSQL = `CREATE OR REPLACE FUNCTION catalog_work_cover_art_grade_truncate() RETURNS trigger
	LANGUAGE plpgsql AS $fn$
	BEGIN
		UPDATE catalog_work SET cover_art_all_explicit = false, updated_at = now()
		 WHERE cover_art_all_explicit;
		RETURN NULL;
	END
	$fn$`

func coverArtGradeTriggers() []struct{ name, stmt string } {
	return []struct{ name, stmt string }{
		{"trg_catalog_work_cover_art_grade_ins", `CREATE TRIGGER trg_catalog_work_cover_art_grade_ins
			AFTER INSERT ON catalog_work_cover
			REFERENCING NEW TABLE AS graded_new
			FOR EACH STATEMENT EXECUTE FUNCTION catalog_work_cover_art_grade()`},
		// No `UPDATE OF sexual, ...` column list here: Postgres answers
		// "transition tables cannot be specified for triggers with column
		// lists" (SQLSTATE 0A000), so the trigger fires for every update of a
		// cover row and leans on the IS DISTINCT FROM guard to write nothing.
		{"trg_catalog_work_cover_art_grade_upd", `CREATE TRIGGER trg_catalog_work_cover_art_grade_upd
			AFTER UPDATE ON catalog_work_cover
			REFERENCING NEW TABLE AS graded_new OLD TABLE AS graded_old
			FOR EACH STATEMENT EXECUTE FUNCTION catalog_work_cover_art_grade()`},
		{"trg_catalog_work_cover_art_grade_del", `CREATE TRIGGER trg_catalog_work_cover_art_grade_del
			AFTER DELETE ON catalog_work_cover
			REFERENCING OLD TABLE AS graded_old
			FOR EACH STATEMENT EXECUTE FUNCTION catalog_work_cover_art_grade()`},
		{"trg_catalog_work_cover_art_grade_trunc", `CREATE TRIGGER trg_catalog_work_cover_art_grade_trunc
			AFTER TRUNCATE ON catalog_work_cover
			FOR EACH STATEMENT EXECUTE FUNCTION catalog_work_cover_art_grade_truncate()`},
	}
}

// CoverArtAllExplicitSQL is the definition of cover_art_all_explicit written as
// a query returning the work ids the column should be true for. The trigger
// keeps the column current; this recomputes it from the cover rows, which is
// what lets cmd/audit-cover-shelf verify the column instead of re-reading the
// mechanism that wrote it. Sharing the predicate is the point — sharing the
// mechanism would make the check unable to fail.
func CoverArtAllExplicitSQL() (string, error) {
	art, err := coverArtRowSQL("c", "s")
	if err != nil {
		return "", err
	}
	return `SELECT c.work_id FROM catalog_work_cover c
		    JOIN catalog_source s ON s.id = c.source_id
		   WHERE ` + art + `
		   GROUP BY c.work_id
		  HAVING bool_and(c.sexual >= ` + fmt.Sprint(model.SexualExplicit) + `)`, nil
}
