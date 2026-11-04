package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/importer"
	"api/pkg/logger"

	"gorm.io/gorm"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED; rehearsal locally, live only in the acceptance run)")
	egDSN := flag.String("eg-dsn", "", "erogamescape staging DSN (default: same host as --dsn, dbname=erogamescape)")
	dlsiteDSN := flag.String("dlsite-dsn", "", "dlsite staging DSN (default: same host as --dsn, dbname=dlsite)")
	apply := flag.Bool("apply", false, "write (default: dry-run — survey + samples, nothing written)")
	limit := flag.Int("limit", 0, "cap works actually minted (0 = all); the survey is always full-pool")
	sampleOut := flag.String("sample-out", "", "directory to write random100.tsv + collisions.tsv + edition_ambiguous.tsv")
	flag.Parse()

	logger.Init("development")
	if *dsn == "" {
		slog.Error("--dsn is required (refusing to guess the catalog DB — pass the rehearsal copy locally)")
		os.Exit(1)
	}

	catalogDB, err := openDB(*dsn)
	if err != nil {
		slog.Error("catalog db connect", "error", err)
		os.Exit(1)
	}
	egDB, err := openDB(deriveDSN(*egDSN, *dsn, "erogamescape"))
	if err != nil {
		slog.Error("erogamescape db connect", "error", err)
		os.Exit(1)
	}
	dlsiteDB, err := openDB(deriveDSN(*dlsiteDSN, *dsn, "dlsite"))
	if err != nil {
		slog.Error("dlsite db connect", "error", err)
		os.Exit(1)
	}

	im := importer.New(catalogDB, egDB, importer.Options{DryRun: !*apply, Limit: *limit})
	st, err := im.RunBgmType4Gated(dlsiteDB)
	if err != nil {
		slog.Error("bgm-type4-gated failed", "error", err)
		os.Exit(1)
	}
	slog.Info("bgm-type4-gated summary",
		"pool_total", st.PoolTotal, "excluded_console_mobile", st.ExcludedConsoleMobile, "eligible_pool", st.EligiblePool,
		"sig_p", st.SigP, "sig_t", st.SigT, "sig_x", st.SigX, "sig_a", st.SigA, "gated_total", st.GatedTotal,
		"skipped_ascii_xonly", st.SkippedASCIIXOnly,
		"title_collisions", st.TitleCollisions, "skipped_intra_collision", st.SkippedIntraCollision,
		"to_create", st.ToCreate, "to_quarantine", st.ToQuarantine, "quarantined", st.Quarantined,
		"edition_guarded", st.EditionGuarded, "edition_ambiguous", st.EditionAmbiguous,
		"edition_refs_created", st.EditionRefsCreated,
		"works_created", st.WorksCreated, "titles_created", st.TitlesCreated,
		"anchors_created", st.AnchorsCreated, "revisions_created", st.RevisionsCreated)
	slog.Info("bgm-type4-gated overlap matrix",
		"p_and_t", st.PT, "p_and_x", st.PX, "t_and_x", st.TX, "all_three", st.All3,
		"p_only", st.POnly, "t_only", st.TOnly, "x_only", st.XOnly, "a_only", st.AOnly)

	if *sampleOut != "" {
		if err := writeSamples(*sampleOut, &st); err != nil {
			slog.Error("write samples", "error", err)
			os.Exit(1)
		}
		slog.Info("samples written", "dir", *sampleOut, "random", len(st.RandomSample), "collisions", len(st.CollisionSamples),
			"edition_ambiguous", len(st.EditionAmbiguousSubjects))
	}
	if !*apply {
		slog.Info("DRY RUN — nothing written; re-run with --apply")
	}
}

func openDB(dsn string) (*gorm.DB, error) {
	return database.OpenJob(dsn)
}

var dbNameRe = regexp.MustCompile(`\bdbname=\S+`)

func deriveDSN(override, base, dbName string) string {
	if override != "" {
		return override
	}
	if dbNameRe.MatchString(base) {
		return dbNameRe.ReplaceAllString(base, "dbname="+dbName)
	}
	return strings.TrimSpace(base) + " dbname=" + dbName
}

func writeSamples(dir string, st *importer.BgmGatedStats) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var rb strings.Builder
	rb.WriteString("subject_id\tname\tname_cn\tsignals\n")
	for _, s := range st.RandomSample {
		fmt.Fprintf(&rb, "%d\t%s\t%s\t%s\n", s.SubjectID, tsv(s.Name), tsv(s.NameCN), s.Signals)
	}
	if err := os.WriteFile(filepath.Join(dir, "random100.tsv"), []byte(rb.String()), 0o644); err != nil {
		return err
	}
	var cb strings.Builder
	cb.WriteString("subject_id\tname\tname_cn\tcollided_norm\texisting_work_id\texisting_title\n")
	for _, c := range st.CollisionSamples {
		fmt.Fprintf(&cb, "%d\t%s\t%s\t%s\t%d\t%s\n", c.SubjectID, tsv(c.Name), tsv(c.NameCN), tsv(c.CollidedNorm), c.WorkID, tsv(c.WorkTitle))
	}
	if err := os.WriteFile(filepath.Join(dir, "collisions.tsv"), []byte(cb.String()), 0o644); err != nil {
		return err
	}
	if err := writeSampleTSV(filepath.Join(dir, "ascii_dropped.tsv"), st.ASCIIDroppedSamples); err != nil {
		return err
	}
	if err := writeSampleTSV(filepath.Join(dir, "ascii_survivors.tsv"), st.ASCIISurvivorSamples); err != nil {
		return err
	}
	var eb strings.Builder
	eb.WriteString("subject_id\tname\tname_cn\tmatched_norm\trelease_holder_work_ids\n")
	for _, e := range st.EditionAmbiguousSubjects {
		ids := make([]string, len(e.WorkIDs))
		for i, id := range e.WorkIDs {
			ids[i] = strconv.FormatInt(id, 10)
		}
		fmt.Fprintf(&eb, "%d\t%s\t%s\t%s\t%s\n", e.SubjectID, tsv(e.Name), tsv(e.NameCN), tsv(e.MatchedNorm), strings.Join(ids, ","))
	}
	return os.WriteFile(filepath.Join(dir, "edition_ambiguous.tsv"), []byte(eb.String()), 0o644)
}

func writeSampleTSV(path string, samples []importer.BgmGatedSample) error {
	var b strings.Builder
	b.WriteString("subject_id\tname\tname_cn\tsignals\n")
	for _, s := range samples {
		fmt.Fprintf(&b, "%d\t%s\t%s\t%s\n", s.SubjectID, tsv(s.Name), tsv(s.NameCN), s.Signals)
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func tsv(s string) string {
	return strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(s)
}
