package importer

import (
	"fmt"
	"log/slog"
	"strconv"

	"api/internal/platform/catalog/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ruleBgmType4Gated  = "rule:bgm-type4-gated"
	bgmGatedMinLen     = 4
	bgmGatedChunk      = 1000
	bgmGatedSampleSeed = 78
	bgmGatedSampleN    = 100
	bgmCollisionSample = 20
	bgmASCIISample     = 30
)

const (
	corpusEG     uint8 = 1 << 0
	corpusDLsite uint8 = 1 << 1
	corpusVNDB   uint8 = 1 << 2
)

const (
	sqlPCPlatforms   = `array['PC','Windows']`
	sqlPCFamily      = `array['PC','Windows','Mac','Linux','DOS']`
	sqlConsoleMobile = `array['PS','PS2','PS3','PS4','PS5','PSP','PSV','NS','NS2','3DS','Wii','WiiU',` +
		`'Xbox','XBOX','Xbox360','XboxOne','XSX','NGC','GB','GBA','GBC','FC','SFC','SS','MD','DC','N64','NDS','WS','PCE','街机','Android','iOS','Symbian']`
	sqlGenreStrict = `array['VN','乙女']`
	sqlGenreAdv    = `array['ADV','AVG']`
	sqlAgeRated    = `array['R18','全年龄']`
	sqlGalgameFolk = `array['galgame','黄油','エロゲ','エロゲー','美少女ゲーム','ギャルゲー']`
)

var dlsiteGameTypes = []string{
	"ノベル", "デジタルノベル", "アドベンチャー", "ロールプレイング", "シミュレーション",
	"アクション", "パズル", "その他ゲーム", "テーブル", "シューティング", "タイピング", "クイズ",
}

type BgmGatedStats struct {
	PoolTotal             int
	ExcludedConsoleMobile int
	EligiblePool          int

	SigP, SigT, SigX    int
	PT, PX, TX, All3    int
	POnly, TOnly, XOnly int

	GatedTotal            int
	SkippedASCIIXOnly     int
	TitleCollisions       int
	SkippedIntraCollision int
	ToCreate              int
	ToQuarantine          int
	Quarantined           int

	EditionGuarded     int
	EditionAmbiguous   int
	EditionRefsCreated int

	WorksCreated     int
	TitlesCreated    int
	AnchorsCreated   int
	RevisionsCreated int

	RandomSample     []BgmGatedSample
	CollisionSamples []BgmGatedCollision

	ASCIIDroppedSamples  []BgmGatedSample
	ASCIISurvivorSamples []BgmGatedSample

	EditionAmbiguousSubjects []BgmGatedEdition
}

type BgmGatedSample struct {
	SubjectID int64
	Name      string
	NameCN    string
	Signals   string
}

type BgmGatedCollision struct {
	SubjectID    int64
	Name         string
	NameCN       string
	CollidedNorm string
	WorkID       int64
	WorkTitle    string
}

type poolRow struct {
	ID         int64  `gorm:"column:id"`
	Name       string `gorm:"column:name"`
	NameCN     string `gorm:"column:name_cn"`
	NameNorm   string `gorm:"column:name_norm"`
	NameCNNorm string `gorm:"column:name_cn_norm"`
	NSFW       bool   `gorm:"column:nsfw"`
	SigP       bool   `gorm:"column:sig_p"`
	SigT       bool   `gorm:"column:sig_t"`
	Excluded   bool   `gorm:"column:excluded"`
}

type wtNorm struct {
	workID int64
	title  string
}

type candidate struct {
	row            poolRow
	signals        string
	collidedWorkID int64
}

func (im *Importer) RunBgmType4Gated(dlsiteDB *gorm.DB) (BgmGatedStats, error) {
	var st BgmGatedStats
	if im.eg == nil || dlsiteDB == nil {
		return st, fmt.Errorf("bgm-type4-gated needs both erogamescape (eg) and dlsite connections")
	}

	xsrc, err := im.loadCrossSourceNorms(dlsiteDB)
	if err != nil {
		return st, fmt.Errorf("load cross-source corpus: %w", err)
	}
	wt, err := im.loadExistingWorkTitleNorms()
	if err != nil {
		return st, fmt.Errorf("load existing work titles: %w", err)
	}
	editions, err := im.loadEditionCorpus()
	if err != nil {
		return st, fmt.Errorf("load edition corpus: %w", err)
	}
	pool, err := im.loadGatedPool()
	if err != nil {
		return st, fmt.Errorf("load pool: %w", err)
	}

	var toCreate []candidate
	var toQuarantine []candidate
	var toAttach []editionAttach
	for _, r := range pool {
		st.PoolTotal++
		if r.Excluded {
			st.ExcludedConsoleMobile++
			continue
		}
		st.EligiblePool++
		p, t := r.SigP, r.SigT

		var nMask, cnMask uint8
		if runeLen(r.NameNorm) >= bgmGatedMinLen {
			nMask = xsrc[r.NameNorm]
		}
		if runeLen(r.NameCNNorm) >= bgmGatedMinLen {
			cnMask = xsrc[r.NameCNNorm]
		}
		combined := nMask | cnMask
		xRaw := combined != 0

		asciiDrop, asciiSurvivor := false, false
		if xRaw && !p && !t && pureASCIIHits(r, nMask, cnMask) {
			if corpusCount(combined) >= 2 {
				asciiSurvivor = true
			} else {
				asciiDrop = true
			}
		}
		x := xRaw && !asciiDrop
		tallySignals(&st, p, t, x)

		if asciiDrop {
			st.SkippedASCIIXOnly++
			if len(st.ASCIIDroppedSamples) < bgmASCIISample {
				st.ASCIIDroppedSamples = append(st.ASCIIDroppedSamples, bgmSampleOf(r, "X"))
			}
			continue
		}
		if asciiSurvivor && len(st.ASCIISurvivorSamples) < bgmASCIISample {
			st.ASCIISurvivorSamples = append(st.ASCIISurvivorSamples, bgmSampleOf(r, "X"))
		}
		if !(p || t || x) {
			continue
		}
		st.GatedTotal++
		if col, ok := collide(r, wt); ok {
			st.TitleCollisions++
			if len(st.CollisionSamples) < bgmCollisionSample {
				st.CollisionSamples = append(st.CollisionSamples, col)
			}
			toQuarantine = append(toQuarantine, candidate{
				row: r, signals: signalString(p, t, x), collidedWorkID: col.WorkID,
			})
			continue
		}
		matched, works := editions.editionHit(r)
		if len(works) == 1 {
			st.EditionGuarded++
			toAttach = append(toAttach, editionAttach{subjectID: r.ID, workID: works[0]})
			continue
		}
		if len(works) > 1 {
			st.EditionAmbiguous++
			st.EditionAmbiguousSubjects = append(st.EditionAmbiguousSubjects, BgmGatedEdition{
				SubjectID: r.ID, Name: r.Name, NameCN: r.NameCN, MatchedNorm: matched, WorkIDs: works,
			})
			continue
		}
		toCreate = append(toCreate, candidate{row: r, signals: signalString(p, t, x)})
	}

	toCreate = dropIntraCollisions(toCreate, &st)
	st.ToCreate = len(toCreate)
	st.ToQuarantine = len(toQuarantine)
	st.RandomSample = pickRandomSample(toCreate)

	if im.dryRun {
		logBgmGated(&st, true)
		return st, nil
	}
	if err := im.attachEditionRefs(toAttach, &st); err != nil {
		return st, err
	}
	createLive := toCreate
	createQ := toQuarantine
	if im.limit > 0 {
		if len(createLive) > im.limit {
			createLive = createLive[:im.limit]
			createQ = createQ[:0]
		} else {
			remain := im.limit - len(createLive)
			if remain < len(createQ) {
				createQ = createQ[:remain]
			}
		}
	}
	if err := im.createGatedWorks(createLive, &st, model.WorkStatusLive); err != nil {
		return st, err
	}
	if err := im.createGatedWorks(createQ, &st, model.WorkStatusQuarantine); err != nil {
		return st, err
	}
	logBgmGated(&st, false)
	return st, nil
}

func (im *Importer) createGatedWorks(cands []candidate, st *BgmGatedStats, status int16) error {
	for start := 0; start < len(cands); start += bgmGatedChunk {
		end := min(start+bgmGatedChunk, len(cands))
		chunk := cands[start:end]
		if err := im.catalog.Transaction(func(tx *gorm.DB) error {
			return im.createGatedChunk(tx, chunk, st, status)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (im *Importer) createGatedChunk(tx *gorm.DB, chunk []candidate, st *BgmGatedStats, status int16) error {
	works := make([]model.CatalogWork, len(chunk))
	for i, c := range chunk {
		display := c.row.Name
		olang := "ja"
		if display == "" {
			display = c.row.NameCN
			olang = "zh-Hans"
		}
		cr := model.ContentRatingAllAges
		if c.row.NSFW {
			cr = model.ContentRatingR18
		}
		works[i] = model.CatalogWork{
			MediumID: mediumGalgame, OLang: olang, DisplayName: display,
			ContentRating: cr, Status: status,
			Extra: datatypes.JSON(`{}`), FieldProvenance: datatypes.JSON(`{}`),
		}
	}
	if err := tx.CreateInBatches(works, 1000).Error; err != nil {
		return err
	}

	var titles []model.CatalogWorkTitle
	for i, c := range chunk {
		wid := works[i].ID
		if c.row.Name != "" {
			titles = append(titles, model.CatalogWorkTitle{WorkID: wid, Lang: "ja", Title: c.row.Name, Kind: model.WorkTitleKindOfficial})
		}
		if c.row.NameCN != "" && c.row.NameCN != c.row.Name {
			titles = append(titles, model.CatalogWorkTitle{WorkID: wid, Lang: "zh-Hans", Title: c.row.NameCN, Kind: model.WorkTitleKindOfficial})
		}
	}
	if err := tx.CreateInBatches(titles, 1000).Error; err != nil {
		return err
	}
	titlesByWork := make(map[int64][]model.CatalogWorkTitle, len(chunk))
	for _, t := range titles {
		titlesByWork[t.WorkID] = append(titlesByWork[t.WorkID], t)
	}

	refs := make([]model.CatalogExternalRef, len(chunk))
	revs := make([]model.CatalogRevision, len(chunk))
	for i, c := range chunk {
		wid := works[i].ID
		refs[i] = selfRef(model.EntityTypeWork, wid, bangumiSource, strconv.FormatInt(c.row.ID, 10), ruleBgmType4Gated)
		revs[i] = importedRev(model.EntityTypeWork, wid, workSnapshotJSON(works[i], titlesByWork[wid]))
	}
	if err := im.batchRefsRevs(tx, refs, revs); err != nil {
		return err
	}

	if status == model.WorkStatusQuarantine {
		var cands []model.CatalogMatchCandidate
		for i, c := range chunk {
			if c.collidedWorkID == 0 {
				continue
			}
			a, b := works[i].ID, c.collidedWorkID
			if b < a {
				a, b = b, a
			}
			cands = append(cands, model.CatalogMatchCandidate{
				EntityType: model.EntityTypeWork, AID: a, BID: b,
				Reason: model.CandidateReasonNameNormEqual,
				Status: model.CandidateStatusPending,
			})
		}
		if len(cands) > 0 {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&cands).Error; err != nil {
				return err
			}
		}
		st.Quarantined += len(works)
	}

	st.WorksCreated += len(works)
	st.TitlesCreated += len(titles)
	st.AnchorsCreated += len(refs)
	st.RevisionsCreated += len(revs)
	return nil
}

func logBgmGated(st *BgmGatedStats, dry bool) {
	slog.Info("bgm-type4-gated survey",
		"dry", dry, "pool_total", st.PoolTotal, "excluded_console_mobile", st.ExcludedConsoleMobile,
		"eligible_pool", st.EligiblePool, "sig_p", st.SigP, "sig_t", st.SigT, "sig_x", st.SigX,
		"gated_total", st.GatedTotal, "p_and_t", st.PT, "p_and_x", st.PX, "t_and_x", st.TX, "all_three", st.All3,
		"p_only", st.POnly, "t_only", st.TOnly, "x_only", st.XOnly,
		"skipped_ascii_xonly", st.SkippedASCIIXOnly,
		"title_collisions", st.TitleCollisions, "skipped_intra_collision", st.SkippedIntraCollision,
		"to_create", st.ToCreate, "to_quarantine", st.ToQuarantine, "quarantined", st.Quarantined,
		"edition_guarded", st.EditionGuarded, "edition_ambiguous", st.EditionAmbiguous,
		"edition_refs_created", st.EditionRefsCreated,
		"works_created", st.WorksCreated, "titles_created", st.TitlesCreated,
		"anchors_created", st.AnchorsCreated, "revisions_created", st.RevisionsCreated)
}
