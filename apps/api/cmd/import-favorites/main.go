// Command import-favorites is step 1 of the favorites unification: it copies
// the forum's galgame collections and the two sites' flat favorites into the
// catalog folder tables, preserving the source timestamps. It only reads the
// sites; nothing here flips a read or write path.
package main

import (
	"flag"
	"log/slog"
	"os"
	"strings"
	"time"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/model"
	"api/pkg/config"
	"api/pkg/logger"

	"gorm.io/gorm"
)

type counters struct {
	FoldersCreated  int
	FoldersReused   int
	ItemsInserted   int
	ItemsMerged     int
	SkippedNotLive  int
	SkippedNoAnchor int
	SkippedPending  int
	SkippedOverCap  int
	Redirected      int
}

func (c counters) log(pass string) {
	slog.Info("pass done", "pass", pass,
		"folders_created", c.FoldersCreated, "folders_reused", c.FoldersReused,
		"items_inserted", c.ItemsInserted, "items_merged", c.ItemsMerged,
		"skipped_not_live", c.SkippedNotLive, "skipped_no_anchor", c.SkippedNoAnchor,
		"skipped_pending", c.SkippedPending, "skipped_over_cap", c.SkippedOverCap,
		"redirected", c.Redirected)
}

func (c *counters) add(o counters) {
	c.FoldersCreated += o.FoldersCreated
	c.FoldersReused += o.FoldersReused
	c.ItemsInserted += o.ItemsInserted
	c.ItemsMerged += o.ItemsMerged
	c.SkippedNotLive += o.SkippedNotLive
	c.SkippedNoAnchor += o.SkippedNoAnchor
	c.SkippedPending += o.SkippedPending
	c.SkippedOverCap += o.SkippedOverCap
	c.Redirected += o.Redirected
}

type importer struct {
	cat   *gorm.DB
	forum *gorm.DB
	moyu  *gorm.DB
	works *workResolver
	apply bool
	batch int

	defaults  map[int64]int64    // owner_uid -> default folder id
	sizes     map[int64]int      // folder id -> item count as this run believes it
	perUser   map[int64]int      // owner_uid -> folder count
	seen      map[[2]int64]bool  // (folder, work) memberships already accounted for
	owned     map[[2]int64]int64 // (owner_uid, work) -> a folder already holding it
	dryRunSeq int64
}

func main() {
	var (
		forumDB  = flag.String("forum-db", "kungalgame", "forum database name (same PG server as kun_catalog)")
		moyuDB   = flag.String("moyu-db", "kungalgame_patch", "moyu database name (same PG server as kun_catalog)")
		catDSN   = flag.String("catalog-dsn", "", "explicit catalog DSN; skips the KUN_CATALOG_PG_* config when all three are given")
		forumDSN = flag.String("forum-dsn", "", "explicit forum DSN")
		moyuDSN  = flag.String("moyu-dsn", "", "explicit moyu DSN")
		apply    = flag.Bool("apply", false, "actually write; default is a dry run that only reports")
		only     = flag.String("only", "all", "passes to run: all, forum, forum-flat, moyu (comma separated)")
		batch    = flag.Int("batch", 1000, "rows per insert batch")
	)
	flag.Parse()

	cat, forum, moyu, closers, err := open(*catDSN, *forumDSN, *moyuDSN, *forumDB, *moyuDB)
	if err != nil {
		fatal("open databases", err)
	}
	defer func() {
		for _, c := range closers {
			_ = c()
		}
	}()

	if *batch < 1 {
		*batch = 1
	}

	imp := newImporter(cat, forum, moyu, *apply, *batch)
	if err := imp.loadState(); err != nil {
		fatal("load catalog state", err)
	}

	passes := map[string]bool{}
	for _, p := range strings.Split(*only, ",") {
		passes[strings.TrimSpace(p)] = true
	}
	run := func(name string) bool { return passes["all"] || passes[name] }

	var total counters
	if run("forum") {
		c, err := imp.importForumCollections()
		if err != nil {
			fatal("forum collections", err)
		}
		c.log("forum")
		total.add(c)
	}
	if run("forum-flat") {
		c, err := imp.importForumFlat()
		if err != nil {
			fatal("forum flat", err)
		}
		c.log("forum-flat")
		total.add(c)
	}
	if run("moyu") {
		c, err := imp.importMoyu()
		if err != nil {
			fatal("moyu", err)
		}
		c.log("moyu")
		total.add(c)
	}

	if imp.apply {
		if err := imp.recountItems(); err != nil {
			fatal("recount item_count", err)
		}
	}
	total.log("total")
	if !imp.apply {
		slog.Info("dry run: nothing was written; re-run with -apply")
	}
}

func newImporter(cat, forum, moyu *gorm.DB, apply bool, batch int) *importer {
	return &importer{
		cat: cat, forum: forum, moyu: moyu, apply: apply, batch: batch,
		defaults: map[int64]int64{}, sizes: map[int64]int{}, perUser: map[int64]int{},
		seen: map[[2]int64]bool{}, owned: map[[2]int64]int64{},
	}
}

// open resolves the three connections. The three DSN flags exist because the
// rehearsal runs against a credential-free DSN (the password comes from
// ~/.pgpass, which pgx reads) while config.Load refuses to start without
// KUN_PG_PASSWORD set.
func open(catDSN, forumDSN, moyuDSN, forumDB, moyuDB string) (cat, forum, moyu *gorm.DB, closers []func() error, err error) {
	if catDSN != "" && forumDSN != "" && moyuDSN != "" {
		for _, dsn := range []string{catDSN, forumDSN, moyuDSN} {
			db, oerr := database.OpenJob(dsn)
			if oerr != nil {
				return nil, nil, nil, closers, oerr
			}
			switch {
			case cat == nil:
				cat = db
			case forum == nil:
				forum = db
			default:
				moyu = db
			}
		}
		return cat, forum, moyu, closers, nil
	}

	cfg, cerr := config.Load()
	if cerr != nil {
		return nil, nil, nil, closers, cerr
	}
	logger.Init(cfg.Server.Env)
	for _, name := range []string{cfg.CatalogDatabase.DBName, forumDB, moyuDB} {
		dbCfg := cfg.CatalogDatabase
		dbCfg.DBName = name
		conn, oerr := database.NewPostgresDB(dbCfg)
		if oerr != nil {
			return nil, nil, nil, closers, oerr
		}
		closers = append(closers, conn.Close)
		switch {
		case cat == nil:
			cat = conn.DB()
		case forum == nil:
			forum = conn.DB()
		default:
			moyu = conn.DB()
		}
	}
	return cat, forum, moyu, closers, nil
}

// loadState reads what the catalog already holds so a re-run reuses folders
// instead of creating a second copy, and so the per-user and per-folder caps
// are judged against the real totals rather than this run's share of them.
func (i *importer) loadState() error {
	var folders []model.CatalogUserFolder
	if err := i.cat.Find(&folders).Error; err != nil {
		return err
	}
	for _, f := range folders {
		i.perUser[f.OwnerUID]++
		i.sizes[f.ID] = 0 // recounted from the membership rows below; the stored counter is a cache
		if f.IsDefault {
			i.defaults[f.OwnerUID] = f.ID
		}
	}
	var members []struct {
		FolderID int64
		WorkID   int64
		OwnerUID int64
	}
	if err := i.cat.Raw(`SELECT folder_id, work_id, owner_uid FROM catalog_user_folder_item`).
		Scan(&members).Error; err != nil {
		return err
	}
	for _, m := range members {
		i.sizes[m.FolderID]++
		i.seen[[2]int64{m.FolderID, m.WorkID}] = true
		if _, held := i.owned[[2]int64{m.OwnerUID, m.WorkID}]; !held {
			i.owned[[2]int64{m.OwnerUID, m.WorkID}] = m.FolderID
		}
	}
	// A folder deleted after it was imported used to leave its
	// catalog_user_folder_import row behind, and importForumCollections trusts
	// that row as "this collection is already folder N" without checking that N
	// still exists — so a re-run would count the collection as reused and write
	// its memberships into a folder id nothing owns. The delete paths now clear
	// the row; this clears anything orphaned before they did, so a re-run
	// re-imports the collection instead of writing into a hole.
	var stale []model.CatalogUserFolderImport
	if err := i.cat.Raw(`SELECT p.site, p.source_id FROM catalog_user_folder_import p
		LEFT JOIN catalog_user_folder f ON f.id = p.folder_id WHERE f.id IS NULL`).
		Scan(&stale).Error; err != nil {
		return err
	}
	if len(stale) > 0 {
		slog.Warn("import provenance outlived its folder", "rows", len(stale), "apply", i.apply)
		if i.apply {
			if err := i.cat.Exec(`DELETE FROM catalog_user_folder_import p
				WHERE NOT EXISTS (SELECT 1 FROM catalog_user_folder f WHERE f.id = p.folder_id)`).
				Error; err != nil {
				return err
			}
		}
	}

	w, err := newWorkResolver(i.cat)
	if err != nil {
		return err
	}
	i.works = w
	slog.Info("catalog state", "folders", len(folders), "users_with_folders", len(i.perUser),
		"live_works", w.liveCount(), "work_redirects", w.redirectCount(), "vndb_anchors", w.anchorCount())
	return nil
}

// defaultFolder answers the folder the flat favorites of a user land in,
// creating it when the user never had one. The name is deliberately empty:
// the forum stores its default collections unnamed and derives the label from
// the owner's name at render time, so a name invented here would freeze a
// label the sites compute per viewer.
func (i *importer) defaultFolder(uid int64) (int64, *counters, error) {
	var c counters
	if id, ok := i.defaults[uid]; ok {
		return id, &c, nil
	}
	if i.perUser[uid] >= model.FoldersPerUserMax {
		c.SkippedOverCap++
		return 0, &c, nil
	}
	f := model.CatalogUserFolder{OwnerUID: uid, Visibility: model.FolderVisibilityPrivate, IsDefault: true}
	if i.apply {
		if err := i.cat.Create(&f).Error; err != nil {
			return 0, &c, err
		}
	} else {
		f.ID = i.nextDryRunID()
	}
	i.defaults[uid] = f.ID
	i.perUser[uid]++
	c.FoldersCreated++
	return f.ID, &c, nil
}

// place routes one membership through the per-folder cap and into the pending
// batch. A pair the catalog already holds is still emitted — the conflict
// clause is what reconciles the two sites' timestamps — but it is counted as a
// merge and does not spend cap headroom.
func (i *importer) place(b *itemBatch, c *counters, folderID, workID, uid int64, created, updated time.Time) error {
	key := [2]int64{folderID, workID}
	if i.seen[key] {
		c.ItemsMerged++
	} else {
		if i.sizes[folderID] >= model.FolderItemsMax {
			c.SkippedOverCap++
			return nil
		}
		i.seen[key] = true
		i.sizes[folderID]++
		c.ItemsInserted++
	}
	if _, held := i.owned[[2]int64{uid, workID}]; !held {
		i.owned[[2]int64{uid, workID}] = folderID
	}
	return b.add(itemRow{FolderID: folderID, WorkID: workID, OwnerUID: uid, Created: created, Updated: updated})
}

// nextDryRunID hands out folder ids no row can hold, so a dry run can route
// items to the folder it would have created without ever writing one.
func (i *importer) nextDryRunID() int64 {
	i.dryRunSeq--
	return i.dryRunSeq
}

func fatal(msg string, err error) {
	slog.Error(msg, "error", err)
	os.Exit(1)
}

func visibilityOf(v string) int16 {
	if v == "public" {
		return model.FolderVisibilityPublic
	}
	// private and restricted both land private: catalog has no viewer list, so
	// private is the only mapping of "restricted" that cannot leak the folder.
	return model.FolderVisibilityPrivate
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
