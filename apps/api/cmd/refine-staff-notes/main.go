package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/importer"
	"api/internal/platform/editing"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const otherStaffRoleID = 2

// notCuratedSource keeps this job off the human lane. It has to be spelled on
// BOTH statements, and for two different reasons that neither guard covers on
// its own: the UPDATE would move a hand-written credit to another role, and the
// INSERT copies source_id verbatim, so a curated row matching a vocabulary term
// mints a machine-made row wearing source_id 12 — which the curated apply
// ("delete every source 12 row for this work, re-insert the list") then reaps
// the next time a person saves, after it has been live on the read faces for a
// while and never once visible in their editor. A note a person corrected in
// place (catalog.work.credits_upstream) is theirs too: this job reads the note
// as the importer's text, and would move the row to whatever role the person's
// wording happens to name.
var notCuratedSource = editspec.NotCuratedLaneSQL("c.source_id") +
	" AND NOT COALESCE(" + editspec.HumanFieldProvenanceSQL("c.field_provenance", "note") + ", false)"

func main() {
	dsn := flag.String("dsn", "", "postgres DSN (required, always explicit)")
	apply := flag.Bool("apply", false, "write the moves (default: dry-run plan)")
	flag.Parse()
	if *dsn == "" {
		fmt.Fprintln(os.Stderr, "refine-staff-notes: --dsn is required")
		os.Exit(2)
	}

	db, err := database.OpenJobWith(*dsn, &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Warn),
	})
	if err != nil {
		fatal(err)
	}
	if err := run(context.Background(), db, *apply, os.Stdout); err != nil {
		fatal(err)
	}
}

func run(ctx context.Context, db *gorm.DB, apply bool, w io.Writer) error {
	reg := editing.NewRegistry()
	if err := editspec.RegisterAll(reg, db); err != nil {
		return err
	}

	roleName := map[int64]string{}
	{
		var rows []struct {
			ID     int64
			NameCN string
		}
		if err := db.Raw(`SELECT id, name_cn FROM catalog_role`).Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			roleName[r.ID] = r.NameCN
		}
	}

	var distinct []struct{ Note string }
	if err := db.Raw(`SELECT DISTINCT lower(btrim(note)) AS note
		FROM catalog_credit WHERE role_id = ? AND btrim(note) <> ''`, otherStaffRoleID).
		Scan(&distinct).Error; err != nil {
		return err
	}
	resolved := map[string][]int64{}
	notes := make([]string, 0, len(distinct))
	for _, d := range distinct {
		roles := importer.RefineVNDBStaffRoles(otherStaffRoleID, d.Note)
		if len(roles) == 1 && roles[0] == otherStaffRoleID {
			continue
		}
		resolved[d.Note] = roles
		notes = append(notes, d.Note)
	}
	sort.Strings(notes)

	var totalMove, totalInsert, totalSkip, totalRekey, totalDropped int64
	for _, note := range notes {
		roles := resolved[note]
		primary, extras := roles[0], roles[1:]

		var inserted int64
		for _, extra := range extras {
			n, err := copyToRole(db, note, extra, apply)
			if err != nil {
				return err
			}
			inserted += n
		}

		var movable, conflicted int64
		if err := db.Raw(`
			SELECT count(*) FILTER (WHERE NOT EXISTS (
			         SELECT 1 FROM catalog_credit t
			         WHERE t.work_id = c.work_id AND t.credit_name_id = c.credit_name_id
			           AND t.role_id = ? AND COALESCE(t.character_id,0) = COALESCE(c.character_id,0))),
			       count(*) FILTER (WHERE EXISTS (
			         SELECT 1 FROM catalog_credit t
			         WHERE t.work_id = c.work_id AND t.credit_name_id = c.credit_name_id
			           AND t.role_id = ? AND COALESCE(t.character_id,0) = COALESCE(c.character_id,0)))
			FROM catalog_credit c
			WHERE c.role_id = ? AND lower(btrim(c.note)) = ? AND `+notCuratedSource,
			primary, primary, otherStaffRoleID, note).Row().Scan(&movable, &conflicted); err != nil {
			return err
		}
		if movable == 0 && conflicted == 0 && inserted == 0 {
			continue
		}

		var rekeyed, dropped int
		if apply {
			if err := db.Transaction(func(tx *gorm.DB) error {
				var err error
				movable, rekeyed, dropped, err = reclassifyNote(ctx, tx, reg, note, primary)
				return err
			}); err != nil {
				return err
			}
		}

		names := make([]string, 0, len(roles))
		for _, r := range roles {
			names = append(names, roleName[r])
		}
		fmt.Fprintf(w, "%-32q -> %-16s move=%-6d insert=%-5d skip_conflict=%-5d rekey=%d/%d\n",
			note, strings.Join(names, "+"), movable, inserted, conflicted, rekeyed, dropped)
		totalMove += movable
		totalInsert += inserted
		totalSkip += conflicted
		totalRekey += int64(rekeyed)
		totalDropped += int64(dropped)
	}

	mode := "DRY-RUN (plan only)"
	if apply {
		mode = "APPLIED"
	}
	fmt.Fprintf(w, "\n%s: %d edges moved, %d composite copies inserted, %d left in 其他 (target credit already exists), %d suppressions rekeyed (%d dropped on collision)\n",
		mode, totalMove, totalInsert, totalSkip, totalRekey, totalDropped)
	return nil
}

func copyToRole(db *gorm.DB, note string, extra int64, apply bool) (int64, error) {
	if !apply {
		var n int64
		err := db.Raw(`
			SELECT count(*) FROM catalog_credit c
			WHERE c.role_id = ? AND lower(btrim(c.note)) = ? AND `+notCuratedSource+`
			  AND NOT EXISTS (
			    SELECT 1 FROM catalog_credit t
			    WHERE t.work_id = c.work_id AND t.credit_name_id = c.credit_name_id
			      AND t.role_id = ? AND COALESCE(t.character_id,0) = COALESCE(c.character_id,0))`,
			otherStaffRoleID, note, extra).Scan(&n).Error
		return n, err
	}
	res := db.Exec(`
		INSERT INTO catalog_credit
			(work_id, credit_name_id, label_id, role_id, character_id,
			 release_id, spoiler, note, source_id, created_at, updated_at)
		SELECT work_id, credit_name_id, label_id, ?, character_id,
		       release_id, spoiler, note, source_id, now(), now()
		FROM catalog_credit c
		WHERE c.role_id = ? AND lower(btrim(c.note)) = ? AND `+notCuratedSource+`
		ON CONFLICT DO NOTHING`,
		extra, otherStaffRoleID, note)
	return res.RowsAffected, res.Error
}

type noteMove struct {
	WorkID       int64 `gorm:"column:work_id"`
	CreditNameID int64 `gorm:"column:credit_name_id"`
	CharacterID  int64 `gorm:"column:character_id"`
}

// reclassifyNote moves the other-staff rows carrying this note onto their
// resolved role and drags their suppressions with them, in one transaction.
//
// role_id is a segment of the credit row identity, so this machine
// reclassification of the SAME assertion (same person, same work, same
// character) would otherwise strand every "this credit is wrong" a person
// recorded against the row: the key stops matching anything and the suppressed
// row comes back with nothing reporting it. Skipping curated rows does not fix
// that — a suppression is ABOUT an upstream row, so it is precisely the rows
// this job is allowed to touch whose keys must move.
func reclassifyNote(ctx context.Context, tx *gorm.DB, reg *editing.Registry,
	note string, primary int64) (moved int64, rekeyed, dropped int, err error) {
	var rows []noteMove
	if err := tx.Raw(`
		UPDATE catalog_credit c
		SET role_id = ?, updated_at = now()
		WHERE c.role_id = ? AND lower(btrim(c.note)) = ? AND `+notCuratedSource+`
		  AND NOT EXISTS (
		    SELECT 1 FROM catalog_credit t
		    WHERE t.work_id = c.work_id AND t.credit_name_id = c.credit_name_id
		      AND t.role_id = ? AND COALESCE(t.character_id,0) = COALESCE(c.character_id,0))
		RETURNING c.work_id, c.credit_name_id, COALESCE(c.character_id, 0) AS character_id`,
		primary, otherStaffRoleID, note, primary).Scan(&rows).Error; err != nil {
		return 0, 0, 0, err
	}
	if len(rows) == 0 {
		return 0, 0, 0, nil
	}
	moves := make([]editing.KeyMove, 0, len(rows))
	for _, r := range rows {
		moves = append(moves, editing.KeyMove{
			EntityID: r.WorkID,
			From:     editspec.CreditIdentity(otherStaffRoleID, r.CreditNameID, r.CharacterID),
			To:       editspec.CreditIdentity(primary, r.CreditNameID, r.CharacterID),
		})
	}
	rekeyed, dropped, err = reg.RekeySuppressed(ctx, tx, editspec.TypeWork, editspec.FieldWorkCredits, moves)
	if err != nil {
		return 0, 0, 0, err
	}
	return int64(len(rows)), rekeyed, dropped, nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "refine-staff-notes:", err)
	os.Exit(1)
}
