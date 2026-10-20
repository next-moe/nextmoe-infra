package editspec

import (
	"context"

	catmodel "api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

var introTableCharacter = introTable{
	ownerCol:      "character_id",
	hasProvenance: true,
	empty:         func() any { return &catmodel.CatalogCharacterIntro{} },
	newRow: func(id int64, lang, intro string) any {
		return &catmodel.CatalogCharacterIntro{
			CharacterID: id, Lang: lang, Intro: intro,
			SourceID: curatedSourceID, Provenance: catmodel.IntroProvenanceSource,
		}
	},
	read: func(ctx context.Context, db *gorm.DB, ownerID int64) (map[string]string, error) {
		var rows []catmodel.CatalogCharacterIntro
		if err := curatedIntroScope(ctx, db, "character_id", ownerID).
			Where("provenance = ?", catmodel.IntroProvenanceSource).
			Find(&rows).Error; err != nil {
			return nil, err
		}
		out := make(map[string]string, len(rows))
		for _, r := range rows {
			out[r.Lang] = r.Intro
		}
		return out, nil
	},
}

func loadCharacterIntros(ctx context.Context, db *gorm.DB, characterID int64) ([]any, error) {
	return loadEntityIntros(ctx, db, introTableCharacter, characterID)
}
