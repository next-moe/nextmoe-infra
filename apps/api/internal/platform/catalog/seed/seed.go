package seed

import (
	"embed"
	"fmt"
	"log/slog"

	"api/internal/platform/catalog/model"

	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

//go:embed data/roles.gen.yaml data/bangumi_role_map.gen.yaml
var dataFS embed.FS

const (
	vndbSourceID    int16 = 2
	bangumiSourceID int16 = 3
	dlsiteSourceID  int16 = 4
	egSourceID      int16 = 5
)

const (
	roleVoiceActor int64 = 1
	roleOtherStaff int64 = 2

	roleTranslator int64 = 3
	roleEditor     int64 = 4
	roleQA         int64 = 5

	roleIllustration    int64 = 184
	roleScenario        int64 = 247
	roleMusic           int64 = 209
	roleCharacterDesign int64 = 145
	roleVocal           int64 = 286

	roleLyric    int64 = 199
	roleComposer int64 = 158
	roleArrange  int64 = 115

	roleDirector int64 = 173
)

func handRoles() []model.CatalogRole {
	return []model.CatalogRole{
		{ID: roleVoiceActor, Key: "voice-actor", Category: "cast", NameCN: "声优", NameJA: "声優", NameEN: "Voice Actor"},
		{ID: roleOtherStaff, Key: "other-staff", Category: "other", NameCN: "其他", NameJA: "その他", NameEN: "Other Staff"},
		{ID: roleTranslator, Key: "translator", Category: "other", NameCN: "翻译", NameJA: "翻訳", NameEN: "Translator"},
		{ID: roleEditor, Key: "text-editor", Category: "other", NameCN: "编辑", NameJA: "編集", NameEN: "Editor"},
		{ID: roleQA, Key: "qa", Category: "other", NameCN: "QA", NameJA: "QA", NameEN: "QA"},
	}
}

func egRoleMap() []model.CatalogSourceRoleMap {
	m := map[string]int64{
		"1": roleIllustration, "2": roleScenario, "3": roleMusic, "4": roleCharacterDesign,
		"5": roleVoiceActor, "6": roleVocal, "7": roleOtherStaff,
	}
	out := make([]model.CatalogSourceRoleMap, 0, len(m))
	for sr, rid := range m {
		out = append(out, model.CatalogSourceRoleMap{SourceID: egSourceID, SourceRole: sr, RoleID: rid})
	}
	return out
}

func egMusicRoleMap() []model.CatalogSourceRoleMap {
	m := map[string]int64{
		"singers": roleVocal, "lyricists": roleLyric, "composers": roleComposer, "arrangers": roleArrange,
	}
	out := make([]model.CatalogSourceRoleMap, 0, len(m))
	for sr, rid := range m {
		out = append(out, model.CatalogSourceRoleMap{SourceID: egSourceID, SourceRole: sr, RoleID: rid})
	}
	return out
}

func dlsiteRoleMap() []model.CatalogSourceRoleMap {
	m := map[string]int64{
		"voice_by": roleVoiceActor, "illust_by": roleIllustration, "scenario_by": roleScenario,
		"music_by": roleMusic, "created_by": roleOtherStaff, "キャラデザ": roleCharacterDesign,
	}
	out := make([]model.CatalogSourceRoleMap, 0, len(m))
	for sr, rid := range m {
		out = append(out, model.CatalogSourceRoleMap{SourceID: dlsiteSourceID, SourceRole: sr, RoleID: rid})
	}
	return out
}

func vndbRoleMap() []model.CatalogSourceRoleMap {
	m := map[string]int64{
		"scenario":   roleScenario,
		"art":        roleIllustration,
		"chardesign": roleCharacterDesign,
		"music":      roleMusic,
		"songs":      roleVocal,
		"director":   roleDirector,
		"staff":      roleOtherStaff,
		"translator": roleTranslator,
		"editor":     roleEditor,
		"qa":         roleQA,
	}
	out := make([]model.CatalogSourceRoleMap, 0, len(m))
	for sr, rid := range m {
		out = append(out, model.CatalogSourceRoleMap{SourceID: vndbSourceID, SourceRole: sr, RoleID: rid})
	}
	return out
}

func media() []model.CatalogMedium {
	return []model.CatalogMedium{
		{ID: 1, Key: "galgame", NameCN: "Galgame"},
		{ID: 2, Key: "manga", NameCN: "漫画"},
		{ID: 3, Key: "novel", NameCN: "小说"},
		{ID: 4, Key: "anime", NameCN: "动画"},
		{ID: 5, Key: "asmr", NameCN: "ASMR"},
		{ID: 6, Key: "doujin_game", NameCN: "同人游戏"},
		{ID: 7, Key: "music", NameCN: "音乐"},
	}
}

func sources() []model.CatalogSource {
	return []model.CatalogSource{
		{ID: 1, Key: "user", TrustTier: 0, Note: "manual curation, not an import source"},
		{ID: 2, Key: "vndb", TrustTier: 1},
		{ID: 3, Key: "bangumi", TrustTier: 1},
		{ID: 4, Key: "dlsite", TrustTier: 0},
		{ID: 5, Key: "erogamescape", TrustTier: 1},
		{ID: 6, Key: "anilist", TrustTier: 1},
		{ID: 7, Key: "mal", TrustTier: 1},
		{ID: 8, Key: "steam", TrustTier: 2},
		{ID: 9, Key: "official_site", TrustTier: 2},
		{ID: 10, Key: "twitter", TrustTier: 2},
		{ID: 11, Key: "pixiv", TrustTier: 2},
		{ID: 12, Key: "curated", TrustTier: 0, Note: "first-party curated/human lane (was galgame_wiki until wave 161)"},
		{ID: 13, Key: "upscale", TrustTier: 0, Note: "first-party AI-upscaled cover derivation (galgame_cover.source='upscale')"},
		{ID: 14, Key: "cien", TrustTier: 2, Note: "cien creator-support platform (ci-en.net)"},
		{ID: 15, Key: "dmm", TrustTier: 2, Note: "DMM storefront (EG cross-reference lane, step 91)"},
		{ID: 16, Key: "web", TrustTier: 2, Note: "generic external web page (external_id = full URL, related links only)"},
		{ID: 17, Key: "getchu", TrustTier: 1, Note: "Getchu.com retailer pages (character rosters, story text, sample CG; anchored via VNDB extlinks)"},
		{ID: 18, Key: "derived", TrustTier: 1, Note: "first-party machine inference over catalog facts (wave 184 series builder)"},
		{ID: 19, Key: "nextmoe", TrustTier: 0, Note: "first-party measurements aggregated from our own users (playtime medians from catalog_user_playtime)"},
		{ID: 20, Key: "howlongtobeat", TrustTier: 1, Note: "HowLongToBeat playtime + rating aggregates (anchored via Steam appid, kun-howlongtobeat-api mirror)"},
		{ID: 21, Key: "censored", TrustTier: 0, Note: "first-party blurred stand-in derived from explicit official art (SFW cover face)"},
	}
}

func relationTypes() []model.CatalogRelationType {
	return []model.CatalogRelationType{
		{ID: 1, Key: "adaptation_of", Domain: model.RelationDomainWork, ForwardPhrase: "改编自", ReversePhrase: "被改编为"},
		{ID: 2, Key: "sequel_of", Domain: model.RelationDomainWork, ForwardPhrase: "是…的续作", ReversePhrase: "有续作"},
		{ID: 3, Key: "side_story_of", Domain: model.RelationDomainWork, ForwardPhrase: "是…的外传", ReversePhrase: "有外传"},
		{ID: 4, Key: "fandisc_of", Domain: model.RelationDomainWork, ForwardPhrase: "是…的 Fandisc", ReversePhrase: "有 Fandisc"},
		{ID: 5, Key: "collects", Domain: model.RelationDomainWork, ForwardPhrase: "收录", ReversePhrase: "被收录于"},
		{ID: 6, Key: "remake_of", Domain: model.RelationDomainWork, ForwardPhrase: "重制自", ReversePhrase: "被重制为"},
		{ID: 7, Key: "same_series", Domain: model.RelationDomainWork, ForwardPhrase: "同系列", ReversePhrase: "同系列", IsSymmetric: true},
		{ID: 8, Key: "same_setting", Domain: model.RelationDomainWork, ForwardPhrase: "同世界观", ReversePhrase: "同世界观", IsSymmetric: true},
		{ID: 9, Key: "crossover_with", Domain: model.RelationDomainWork, ForwardPhrase: "联动", ReversePhrase: "联动", IsSymmetric: true},
		{ID: 10, Key: "shares_character", Domain: model.RelationDomainWork, ForwardPhrase: "角色出演", ReversePhrase: "角色出演", IsSymmetric: true},
		{ID: 11, Key: "alternative_setting", Domain: model.RelationDomainWork, ForwardPhrase: "不同世界观", ReversePhrase: "不同世界观", IsSymmetric: true},
		{ID: 12, Key: "alternative_version", Domain: model.RelationDomainWork, ForwardPhrase: "不同演绎", ReversePhrase: "不同演绎", IsSymmetric: true},

		{ID: 20, Key: "imprint_of", Domain: model.RelationDomainEntity, ForwardPhrase: "是…旗下的厂牌/文库", ReversePhrase: "拥有厂牌/文库"},
		{ID: 21, Key: "renamed_from", Domain: model.RelationDomainEntity, ForwardPhrase: "前身为", ReversePhrase: "后改名为"},
		{ID: 22, Key: "subsidiary_of", Domain: model.RelationDomainEntity, ForwardPhrase: "是…的子公司", ReversePhrase: "有子公司"},
		{ID: 23, Key: "member_of", Domain: model.RelationDomainEntity, ForwardPhrase: "是…的成员", ReversePhrase: "有成员"},
	}
}

func loadGeneratedRoles() ([]model.CatalogRole, []model.CatalogSourceRoleMap, error) {
	var rolesDoc struct {
		Roles []RoleSeed `yaml:"roles"`
	}
	if err := unmarshalData("data/roles.gen.yaml", &rolesDoc); err != nil {
		return nil, nil, err
	}
	var mapDoc struct {
		Mappings []RoleMapSeed `yaml:"mappings"`
	}
	if err := unmarshalData("data/bangumi_role_map.gen.yaml", &mapDoc); err != nil {
		return nil, nil, err
	}
	if len(rolesDoc.Roles) == 0 || len(mapDoc.Mappings) == 0 {
		return nil, nil, fmt.Errorf("catalog seed: generated artifacts are empty — regenerate via seed/gen")
	}

	roles := make([]model.CatalogRole, len(rolesDoc.Roles))
	for i, r := range rolesDoc.Roles {
		roles[i] = model.CatalogRole{
			ID: r.ID, Key: r.Key, Category: r.Category,
			NameCN: r.NameCN, NameJA: r.NameJA, NameEN: r.NameEN,
		}
	}
	mappings := make([]model.CatalogSourceRoleMap, len(mapDoc.Mappings))
	for i, m := range mapDoc.Mappings {
		mappings[i] = model.CatalogSourceRoleMap{
			SourceID: bangumiSourceID, SourceRole: m.SourceRole,
			RoleID: m.RoleID, Note: m.Note,
		}
	}
	return roles, mappings, nil
}

func unmarshalData(name string, out any) error {
	raw, err := dataFS.ReadFile(name)
	if err != nil {
		return fmt.Errorf("catalog seed: read embedded %s: %w", name, err)
	}
	if err := yaml.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("catalog seed: parse %s: %w", name, err)
	}
	return nil
}

func Run(db *gorm.DB) error {
	roles, roleMap, err := loadGeneratedRoles()
	if err != nil {
		return err
	}
	roles = append(roles, handRoles()...)
	roleMap = append(roleMap, egRoleMap()...)
	roleMap = append(roleMap, egMusicRoleMap()...)
	roleMap = append(roleMap, dlsiteRoleMap()...)
	roleMap = append(roleMap, vndbRoleMap()...)

	if err := upsert(db, "catalog_medium", media(), []string{"id"}, []string{"name_cn"}); err != nil {
		return err
	}
	if err := upsert(db, "catalog_source", sources(), []string{"id"}, []string{"note", "key"}); err != nil {
		return err
	}
	if err := upsert(db, "catalog_role", roles, []string{"id"}, []string{"category", "name_cn", "name_ja", "name_en"}); err != nil {
		return err
	}
	if err := upsert(db, "catalog_source_role_map", roleMap, []string{"source_id", "source_role"}, []string{"note"}); err != nil {
		return err
	}
	if err := upsert(db, "catalog_relation_type", relationTypes(), []string{"id"}, []string{"forward_phrase", "reverse_phrase"}); err != nil {
		return err
	}
	if err := upsert(db, "catalog_platform", platforms(), []string{"id"}, []string{"display_name"}); err != nil {
		return err
	}
	return nil
}

func platforms() []model.CatalogPlatform {
	return []model.CatalogPlatform{
		{ID: 1, Key: "and", DisplayName: "Android"},
		{ID: 2, Key: "bdp", DisplayName: "Blu-ray Player"},
		{ID: 3, Key: "dos", DisplayName: "DOS"},
		{ID: 4, Key: "drc", DisplayName: "Dreamcast"},
		{ID: 5, Key: "dvd", DisplayName: "DVD Player"},
		{ID: 6, Key: "fm7", DisplayName: "FM-7"},
		{ID: 7, Key: "fm8", DisplayName: "FM-8"},
		{ID: 8, Key: "fmt", DisplayName: "FM Towns"},
		{ID: 9, Key: "gba", DisplayName: "Game Boy Advance"},
		{ID: 10, Key: "gbc", DisplayName: "Game Boy Color"},
		{ID: 11, Key: "ios", DisplayName: "iOS"},
		{ID: 12, Key: "lin", DisplayName: "Linux"},
		{ID: 13, Key: "mac", DisplayName: "macOS"},
		{ID: 14, Key: "mob", DisplayName: "Mobile (feature phone)"},
		{ID: 15, Key: "msx", DisplayName: "MSX"},
		{ID: 16, Key: "n3d", DisplayName: "Nintendo 3DS"},
		{ID: 17, Key: "nds", DisplayName: "Nintendo DS"},
		{ID: 18, Key: "nes", DisplayName: "Famicom"},
		{ID: 19, Key: "oth", DisplayName: "Other"},
		{ID: 20, Key: "p88", DisplayName: "PC-88"},
		{ID: 21, Key: "p98", DisplayName: "PC-98"},
		{ID: 22, Key: "pce", DisplayName: "PC Engine"},
		{ID: 23, Key: "pcf", DisplayName: "PC-FX"},
		{ID: 24, Key: "ps1", DisplayName: "PlayStation"},
		{ID: 25, Key: "ps2", DisplayName: "PlayStation 2"},
		{ID: 26, Key: "ps3", DisplayName: "PlayStation 3"},
		{ID: 27, Key: "ps4", DisplayName: "PlayStation 4"},
		{ID: 28, Key: "ps5", DisplayName: "PlayStation 5"},
		{ID: 29, Key: "psp", DisplayName: "PlayStation Portable"},
		{ID: 30, Key: "psv", DisplayName: "PlayStation Vita"},
		{ID: 31, Key: "sat", DisplayName: "Sega Saturn"},
		{ID: 32, Key: "scd", DisplayName: "Sega Mega-CD"},
		{ID: 33, Key: "sfc", DisplayName: "Super Famicom"},
		{ID: 34, Key: "smd", DisplayName: "Mega Drive"},
		{ID: 35, Key: "sw2", DisplayName: "Nintendo Switch 2"},
		{ID: 36, Key: "swi", DisplayName: "Nintendo Switch"},
		{ID: 37, Key: "tdo", DisplayName: "3DO"},
		{ID: 38, Key: "vnd", DisplayName: "VNDS"},
		{ID: 39, Key: "web", DisplayName: "Web Browser"},
		{ID: 40, Key: "wii", DisplayName: "Wii"},
		{ID: 41, Key: "win", DisplayName: "Windows"},
		{ID: 42, Key: "wiu", DisplayName: "Wii U"},
		{ID: 43, Key: "x1s", DisplayName: "Sharp X1"},
		{ID: 44, Key: "x68", DisplayName: "Sharp X68000"},
		{ID: 45, Key: "xb1", DisplayName: "Xbox"},
		{ID: 46, Key: "xb3", DisplayName: "Xbox 360"},
		{ID: 47, Key: "xbo", DisplayName: "Xbox One"},
		{ID: 48, Key: "xxs", DisplayName: "Xbox Series X/S"},
	}
}

func upsert[T any](db *gorm.DB, table string, rows []T, conflictCols, updateCols []string) error {
	columns := make([]clause.Column, len(conflictCols))
	for i, c := range conflictCols {
		columns[i] = clause.Column{Name: c}
	}
	res := db.Clauses(clause.OnConflict{
		Columns:   columns,
		DoUpdates: clause.AssignmentColumns(updateCols),
	}).Create(&rows)
	if res.Error != nil {
		return fmt.Errorf("catalog seed: upsert %s: %w", table, res.Error)
	}
	slog.Info("seeded registry", "table", table, "rows", len(rows), "affected", res.RowsAffected)
	return nil
}
