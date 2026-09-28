package workengines

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"golang.org/x/text/unicode/norm"
	"gorm.io/gorm"
)

const ruleVNDBEngine = "rule:vndb-engine"

var bracketed = regexp.MustCompile(`[(（\[【].*?[)）\]】]`)

func engineKey(s string) string {
	s = norm.NFKC.String(s)
	s = bracketed.ReplaceAllString(s, " ")
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// aliasesByName folds spellings into the VNDB engine named by the map key.
// VNDB itself files TyranoBuilder games under TyranoScript and every RPG Maker
// version under RPG Maker, and carries stray duplicates of both (Tyranobuilder,
// RPG Maker MV, RPG-Maker 2000, KRKR2/KAG3, Renpy, Ren'py) that would
// otherwise each become an engine of their own.
var aliasesByName = map[string][]string{
	"KiriKiri": {"吉里吉里", "吉里吉里2", "吉里吉里Z", "吉里吉里/KAG", "吉里吉里/KAG3", "吉里吉里2/KAG3",
		"吉里吉里Z/KAG3", "KAG", "KAG3", "krkr", "krkr2", "krkrz", "KRKR2/KAG3", "KiriKiri2", "KiriKiriZ",
		"KiriKiri2/KAG3", "KiriKiri/KAG3"},
	"TyranoScript": {"ティラノスクリプト", "ティラノビルダー", "ティラノビルダーPRO", "TyranoBuilder",
		"TyranoBuilder PRO", "Tyrano", "ティラノ", "TyranoScript V5", "ティラノスクリプトV5"},
	"RPG Maker": {"RPGツクール", "RPGツクールMV", "RPGツクールMZ", "RPGツクールVX", "RPGツクールVX Ace",
		"RPGツクールXP", "RPGツクール2000", "RPGツクール2003", "RPGツクール95", "RPG Maker MV", "RPG Maker MZ",
		"RPG Maker VX", "RPG Maker VX Ace", "RPG Maker XP", "RPG Maker 2000", "RPG Maker 2003", "RPG-Maker 2000",
		"RMMV", "RMMZ", "RMVA", "RMVX", "RMXP", "RPGMV", "RPGMZ", "rpgmarker", "rpg marker", "RPG制作大师"},
	"Wolf RPG Editor": {"WOLF RPGエディター", "WOLF RPGエディタ", "WOLF RPG", "ウディタ", "ウルフRPGエディター",
		"WOLF RPG Editor", "WOLF"},
	"Ren'Py":              {"RenPy Engine", "Ren'Py Engine"},
	"NScripter":           {"NScript", "Nscript", "NScripter Engine"},
	"BGI/Ethornell":       {"Ethornell", "Ethronell", "BGI", "BURIKO General Interpreter"},
	"DxLib":               {"DXライブラリ", "DX Library"},
	"Unreal Engine":       {"Unreal", "UE"},
	"LiveMaker":           {"ライブメーカー", "Live Maker"},
	"Unity":               {"Unity3D", "Unity 3D", "Unity Engine", "ユニティ"},
	"Flash Player":        {"Flash", "Adobe Flash", "Macromedia Flash"},
	"Macromedia Director": {"Director", "Adobe Director"},
	"Godot":               {"Godot Engine"},
	"YU-RIS":              {"YU-RIS/ERIS", "YU-RIS Engine", "ERIS"},
	"NVLMaker":            {"The NVL Maker", "NVL Maker"},
	"Artemis Engine":      {"Artemis"},
	"GameMaker":           {"Game Maker"},
	"Comic Maker":         {"ComicMaker"},
	"SiglusEngine":        {"Siglus"},
	"CatSystem2":          {"CatSystem 2", "CS2"},
	"Visual Novel Maker":  {"VN Maker"},
	"Light.vn":            {"Lightvn"},
}

var aliasKeys = func() map[string]string {
	out := map[string]string{}
	for target, aliases := range aliasesByName {
		t := engineKey(target)
		for _, a := range aliases {
			if k := engineKey(a); k != "" && k != t {
				out[k] = t
			}
		}
	}
	return out
}()

var prefixRules = func() [][2]string {
	var out [][2]string
	for group, prefixes := range map[string][]string{
		"RPG Maker":       {"RPGツクール", "RPGツクル", "RPG Maker", "RPGMaker", "rpgmarker"},
		"KiriKiri":        {"吉里吉里", "krkr", "KiriKiri", "KAGeXpress"},
		"TyranoScript":    {"ティラノ", "Tyrano"},
		"LiveMaker":       {"LiveMaker", "LiveMarker", "ライブメーカー"},
		"Wolf RPG Editor": {"WOLF RPG", "ウディタ"},
		"NScripter":       {"NScript"},
		"Ren'Py":          {"RenPy"},
		"Unity":           {"Unity"},
		"Godot":           {"Godot"},
		"YU-RIS":          {"YU-RIS"},
		"Artemis Engine":  {"Artemis"},
		"SiglusEngine":    {"Siglus"},
	} {
		for _, p := range prefixes {
			out = append(out, [2]string{engineKey(p), engineKey(group)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i][0]) > len(out[j][0]) })
	return out
}()

func prefixGroup(key string) (string, bool) {
	for _, r := range prefixRules {
		if strings.HasPrefix(key, r[0]) {
			return r[1], true
		}
	}
	return "", false
}

func groupKey(name string) string {
	k := engineKey(name)
	if t, ok := aliasKeys[k]; ok {
		return t
	}
	return k
}

type vndbEngine struct {
	ID          string
	Name        string
	Description string
	Releases    int
}

type vocab struct {
	db     *gorm.DB
	apply  bool
	source int16
	st     *Stats

	vndb       map[string]vndbEngine
	canonical  map[string]string
	byGroup    map[string]int64
	byVNDB     map[string]int64
	nextDryRun int64
}

func loadVocab(ctx context.Context, db *gorm.DB, apply bool, vndbSource int16, st *Stats) (*vocab, error) {
	v := &vocab{
		db: db, apply: apply, source: vndbSource, st: st,
		vndb: map[string]vndbEngine{}, canonical: map[string]string{},
		byGroup: map[string]int64{}, byVNDB: map[string]int64{},
	}
	var engines []vndbEngine
	if err := db.WithContext(ctx).Raw(`
		SELECT e.id, e.name, e.description, count(r.id) AS releases
		FROM src_vndb.engines e LEFT JOIN src_vndb.releases r ON r.engine = e.id
		GROUP BY e.id, e.name, e.description`).Scan(&engines).Error; err != nil {
		return nil, fmt.Errorf("load vndb engines: %w", err)
	}
	if len(engines) == 0 {
		return nil, fmt.Errorf("src_vndb.engines is empty — run ingest-vndb first")
	}
	sort.Slice(engines, func(i, j int) bool { return engines[i].ID < engines[j].ID })
	for _, e := range engines {
		v.vndb[e.ID] = e
		g := groupKey(e.Name)
		if g == "" {
			continue
		}
		cur, ok := v.canonical[g]
		if !ok || betterCanonical(e, v.vndb[cur], g) {
			v.canonical[g] = e.ID
		}
	}

	var catalog []struct {
		ID   int64  `gorm:"column:id"`
		Name string `gorm:"column:name"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT id, name FROM catalog_engine ORDER BY id`).
		Scan(&catalog).Error; err != nil {
		return nil, fmt.Errorf("load catalog engines: %w", err)
	}
	var refs []struct {
		EntityID   int64  `gorm:"column:entity_id"`
		ExternalID string `gorm:"column:external_id"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT r.entity_id, r.external_id FROM catalog_external_ref r
		JOIN catalog_engine e ON e.id = r.entity_id
		WHERE r.entity_type = ? AND r.source_id = ? AND r.link_kind = ?`,
		model.EntityTypeEngine, vndbSource, model.LinkKindExact).Scan(&refs).Error; err != nil {
		return nil, fmt.Errorf("load vndb engine refs: %w", err)
	}
	for _, r := range refs {
		v.byVNDB[r.ExternalID] = r.EntityID
		if e, ok := v.vndb[r.ExternalID]; ok {
			if g := groupKey(e.Name); g != "" {
				if _, taken := v.byGroup[g]; !taken {
					v.byGroup[g] = r.EntityID
				}
			}
		}
	}
	for _, e := range catalog {
		if g := groupKey(e.Name); g != "" {
			if _, taken := v.byGroup[g]; !taken {
				v.byGroup[g] = e.ID
			}
		}
	}
	return v, nil
}

func betterCanonical(cand, cur vndbEngine, group string) bool {
	candExact, curExact := engineKey(cand.Name) == group, engineKey(cur.Name) == group
	if candExact != curExact {
		return candExact
	}
	if cand.Releases != cur.Releases {
		return cand.Releases > cur.Releases
	}
	return cand.ID < cur.ID
}

func (v *vocab) resolveVNDB(ctx context.Context, vndbID string) (int64, bool, error) {
	if id, ok := v.byVNDB[vndbID]; ok {
		return id, true, nil
	}
	e, ok := v.vndb[vndbID]
	if !ok {
		return 0, false, nil
	}
	id, ok, err := v.resolveGroup(ctx, groupKey(e.Name))
	if err != nil || !ok {
		return 0, false, err
	}
	if err := v.link(ctx, id, vndbID); err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func (v *vocab) resolveGroup(ctx context.Context, group string) (int64, bool, error) {
	if group == "" {
		return 0, false, nil
	}
	if id, ok := v.byGroup[group]; ok {
		return id, true, nil
	}
	canon, ok := v.canonical[group]
	if !ok {
		return 0, false, nil
	}
	e := v.vndb[canon]
	id, err := v.create(ctx, e)
	if err != nil {
		return 0, false, err
	}
	v.byGroup[group] = id
	return id, true, nil
}

func (v *vocab) create(ctx context.Context, e vndbEngine) (int64, error) {
	v.st.EnginesCreated++
	if !v.apply {
		v.nextDryRun--
		return v.nextDryRun, nil
	}
	aliases, _ := json.Marshal([]string{})
	row := model.CatalogEngine{Name: e.Name, Description: e.Description, Aliases: aliases}
	if err := v.db.WithContext(ctx).Create(&row).Error; err != nil {
		return 0, fmt.Errorf("create engine %q: %w", e.Name, err)
	}
	return row.ID, nil
}

func (v *vocab) link(ctx context.Context, engineID int64, vndbID string) error {
	v.byVNDB[vndbID] = engineID
	v.st.EnginesLinked++
	if !v.apply {
		return nil
	}
	_, err := repository.InsertRefIfAbsent(v.db.WithContext(ctx), model.CatalogExternalRef{
		EntityType: model.EntityTypeEngine, EntityID: engineID, SourceID: v.source,
		ExternalID: vndbID, LinkKind: model.LinkKindExact, MatchedBy: ruleVNDBEngine,
	})
	if err != nil {
		return fmt.Errorf("link engine %d to vndb %s: %w", engineID, vndbID, err)
	}
	return nil
}
