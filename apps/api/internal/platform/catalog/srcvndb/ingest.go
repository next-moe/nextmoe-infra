package srcvndb

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

func EnsureSchema(db *gorm.DB) error {
	if err := db.Exec(`CREATE SCHEMA IF NOT EXISTS src_vndb`).Error; err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	m := db.Migrator()
	if m.HasTable(&Char{}) && !m.HasColumn(&Char{}, "bloodt") {
		if err := m.DropTable(&Char{}); err != nil {
			return fmt.Errorf("drop pre-72 chars: %w", err)
		}
	}
	if m.HasTable(&VN{}) && !m.HasColumn(&VN{}, "c_length") {
		if err := m.DropTable(&VN{}); err != nil {
			return fmt.Errorf("drop pre-91 vn: %w", err)
		}
	}
	if err := db.AutoMigrate(
		&VN{}, &VNTitle{}, &VNRelation{}, &Char{}, &CharName{}, &CharVN{}, &Image{},
		&Staff{}, &StaffAlias{}, &VNStaff{}, &VNSeiyuu{},
		&Trait{}, &TraitParent{}, &CharTrait{}, &Tag{}, &TagParent{}, &TagVN{},
		&Release{}, &ReleaseVN{}, &ReleaseProducer{}, &ReleasePlatform{}, &ReleaseTitle{}, &Engine{}, &Producer{},
		&Extlink{}, &ReleaseExtlink{},
		&VNExtlink{}, &ProducerExtlink{}, &StaffExtlink{}, &ProducerRelation{},
		&PortraitBackfill{}, &IngestRun{}, &VNVoteStats{},
	); err != nil {
		return fmt.Errorf("automigrate src_vndb: %w", err)
	}
	return nil
}

var Files = []string{
	"vn", "vn_titles", "chars", "chars_names", "chars_vns", "images",
	"vn_relations",
	"staff", "staff_alias", "vn_staff", "vn_seiyuu",
	"traits", "traits_parents", "chars_traits",
	"tags", "tags_parents", "tags_vn",
	"producers", "releases", "releases_vn", "releases_producers", "releases_platforms", "releases_titles", "engines",
	"extlinks", "releases_extlinks",
	"vn_extlinks", "producers_extlinks", "staff_extlinks", "producers_relations",
}

type FileReport struct {
	Rows    int64 `json:"rows"`
	Skipped int64 `json:"skipped"`
}

type Report struct {
	DumpDir  string
	PerFile  map[string]FileReport
	Duration time.Duration
}

func Run(db *gorm.DB, dumpDir, only string) (*Report, error) {
	started := time.Now()
	report := &Report{DumpDir: dumpDir, PerFile: map[string]FileReport{}}

	for _, name := range Files {
		if only != "" && name != only {
			continue
		}
		var fr FileReport
		err := db.Transaction(func(tx *gorm.DB) error {
			var err error
			fr, err = ingestFile(tx, dumpDir, name)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("ingest %s: %w", name, err)
		}
		report.PerFile[name] = fr
	}
	report.Duration = time.Since(started)

	counts, _ := json.Marshal(report.PerFile)
	run := IngestRun{
		DumpLabel:  filepath.Base(dumpDir),
		Counts:     string(counts),
		DurationMS: report.Duration.Milliseconds(),
		StartedAt:  started,
	}
	if err := db.Create(&run).Error; err != nil {
		return nil, fmt.Errorf("record ingest_run: %w", err)
	}
	return report, nil
}

func ingestFile(tx *gorm.DB, dumpDir, name string) (FileReport, error) {
	newTableLoader, ok := loaders[name]
	if !ok {
		return FileReport{}, fmt.Errorf("unknown dump file %q", name)
	}

	cols, err := readHeader(filepath.Join(dumpDir, name+".header"))
	if err != nil {
		return FileReport{}, err
	}
	idx := make(map[string]int, len(cols))
	for i, c := range cols {
		idx[c] = i
	}

	f, err := os.Open(filepath.Join(dumpDir, name))
	if err != nil {
		return FileReport{}, err
	}
	defer f.Close()

	table := "src_vndb." + name
	if err := tx.Exec(`TRUNCATE ` + table + ` RESTART IDENTITY`).Error; err != nil {
		return FileReport{}, fmt.Errorf("truncate %s: %w", table, err)
	}

	ld := newTableLoader(tx, time.Now())
	var report FileReport
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		fields := strings.Split(string(line), "\t")
		get := func(col string) (string, bool) {
			i, ok := idx[col]
			if !ok || i >= len(fields) {
				return "", false
			}
			v, isNull := copyUnescape(fields[i])
			if isNull {
				return "", false
			}
			return v, true
		}
		skipped, err := ld.add(get)
		if err != nil {
			return FileReport{}, err
		}
		if skipped {
			report.Skipped++
		} else {
			report.Rows++
		}
	}
	if err := sc.Err(); err != nil {
		return FileReport{}, err
	}
	if err := ld.flush(); err != nil {
		return FileReport{}, err
	}
	return report, nil
}

func readHeader(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read header %s: %w", path, err)
	}
	return strings.Split(strings.TrimRight(string(b), "\r\n"), "\t"), nil
}

type getter = func(col string) (string, bool)

type tableLoader interface {
	add(get getter) (skipped bool, err error)
	flush() error
}

type loader[T any] struct {
	tx     *gorm.DB
	decode func(get getter) (T, bool)
	batch  []T
}

func newLoader[T any](tx *gorm.DB, decode func(get getter) (T, bool)) *loader[T] {
	return &loader[T]{tx: tx, decode: decode}
}

func (l *loader[T]) add(get getter) (bool, error) {
	row, ok := l.decode(get)
	if !ok {
		return true, nil
	}
	l.batch = append(l.batch, row)
	if len(l.batch) >= batchSize {
		return false, l.flush()
	}
	return false, nil
}

func (l *loader[T]) flush() error {
	if len(l.batch) == 0 {
		return nil
	}
	if err := l.tx.CreateInBatches(l.batch, batchSize).Error; err != nil {
		return err
	}
	l.batch = l.batch[:0]
	return nil
}

const batchSize = 2000

var loaders = map[string]func(tx *gorm.DB, now time.Time) tableLoader{
	"vn":                  newVNLoader,
	"vn_titles":           newVNTitleLoader,
	"vn_relations":        newVNRelationLoader,
	"chars":               newCharLoader,
	"chars_names":         newCharNameLoader,
	"chars_vns":           newCharVNLoader,
	"images":              newImageLoader,
	"staff":               newStaffLoader,
	"staff_alias":         newStaffAliasLoader,
	"vn_staff":            newVNStaffLoader,
	"vn_seiyuu":           newVNSeiyuuLoader,
	"traits":              newTraitLoader,
	"traits_parents":      newTraitParentLoader,
	"chars_traits":        newCharTraitLoader,
	"tags":                newTagLoader,
	"tags_parents":        newTagParentLoader,
	"tags_vn":             newTagVNLoader,
	"producers":           newProducerLoader,
	"releases":            newReleaseLoader,
	"releases_vn":         newReleaseVNLoader,
	"releases_producers":  newReleaseProducerLoader,
	"releases_platforms":  newReleasePlatformLoader,
	"releases_titles":     newReleaseTitleLoader,
	"engines":             newEngineLoader,
	"extlinks":            newExtlinkLoader,
	"releases_extlinks":   newReleaseExtlinkLoader,
	"vn_extlinks":         newVNExtlinkLoader,
	"producers_extlinks":  newProducerExtlinkLoader,
	"staff_extlinks":      newStaffExtlinkLoader,
	"producers_relations": newProducerRelationLoader,
}

func getStr(get getter, col string) string {
	v, _ := get(col)
	return v
}

func getInt16(get getter, col string) int16 {
	return int16(getInt64(get, col))
}

func getInt(get getter, col string) int {
	return int(getInt64(get, col))
}

func getInt64(get getter, col string) int64 {
	v, ok := get(col)
	if !ok || v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func getBool(get getter, col string) bool {
	v, _ := get(col)
	return v == "t"
}

func getInt16Ptr(get getter, col string) *int16 {
	v, ok := get(col)
	if !ok {
		return nil
	}
	n, err := strconv.ParseInt(v, 10, 16)
	if err != nil {
		return nil
	}
	p := int16(n)
	return &p
}

func getIntPtr(get getter, col string) *int {
	v, ok := get(col)
	if !ok {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil
	}
	return &n
}

func getFloat64Ptr(get getter, col string) *float64 {
	v, ok := get(col)
	if !ok {
		return nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil
	}
	return &f
}

func getBoolPtr(get getter, col string) *bool {
	v, ok := get(col)
	if !ok {
		return nil
	}
	b := v == "t"
	return &b
}

func copyUnescape(s string) (string, bool) {
	if s == `\N` {
		return "", true
	}
	if !strings.Contains(s, `\`) {
		return s, false
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '\\':
				b.WriteByte('\\')
			default:
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String(), false
}
