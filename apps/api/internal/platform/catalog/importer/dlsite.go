package importer

import (
	"encoding/json"
	"strconv"
	"strings"

	"api/internal/platform/catalog/model"

	"gorm.io/datatypes"
)

const dlChunk = 2000

type dlWork struct {
	workno        string
	name          string
	kana          string
	makerExt      string
	contentRating int16
	stub          bool
	y, m, d       *int16
	credits       []dlCredit
}

type dlCredit struct {
	createrExt string
	roleID     int64
}

type dlNamed struct {
	ext, name string
}

func dlEdgeKind(labelKind int16) int16 {
	if labelKind == model.LabelKindPublisher {
		return model.WorkLabelKindPublisher
	}
	return model.WorkLabelKindCircle
}

type createrJSON struct {
	id, name, classification string
}

func parseCreaters(raw datatypes.JSON) []createrJSON {
	var obj map[string][]struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		Classification string `json:"classification"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	var out []createrJSON
	for cls, arr := range obj {
		for _, c := range arr {
			if c.ID == "" {
				continue
			}
			classification := c.Classification
			if classification == "" {
				classification = cls
			}
			out = append(out, createrJSON{id: c.ID, name: c.Name, classification: classification})
		}
	}
	return out
}

func dlContentRating(age string) int16 {
	switch strings.TrimSpace(age) {
	case "1":
		return model.ContentRatingAllAges
	case "2":
		return model.ContentRatingSensitive
	case "3":
		return model.ContentRatingR18
	default:
		return model.ContentRatingAllAges
	}
}

// RG is the ordinary circle prefix, but RG01001 is DLsite's own publishing
// brand on スマホ版 and ported editions. Read as a circle, "DLsite" stood as the
// circle on 4,612 works and as the only company on 585 galgames.
var dlsiteHouseMakers = map[string]bool{"RG01001": true}

func dlLabelKind(makerID string) int16 {
	if strings.HasPrefix(makerID, "BG") || dlsiteHouseMakers[makerID] {
		return model.LabelKindPublisher
	}
	return model.LabelKindDoujinCircle
}

func ymdParts(ymd string) (y, m, d *int16) {
	if len(ymd) < 10 {
		return nil, nil, nil
	}
	yy, err1 := strconv.Atoi(ymd[0:4])
	mm, err2 := strconv.Atoi(ymd[5:7])
	dd, err3 := strconv.Atoi(ymd[8:10])
	if err1 != nil || err2 != nil || err3 != nil {
		return nil, nil, nil
	}
	y16, m16, d16 := int16(yy), int16(mm), int16(dd)
	return &y16, &m16, &d16
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func filterNew(m map[string]dlNamed, anchors map[string]int64, source int16) []dlNamed {
	out := make([]dlNamed, 0, len(m))
	for ext, n := range m {
		if anchors[anchorKey(source, ext)] == 0 {
			out = append(out, n)
		}
	}
	return out
}

func workSnapshotJSON(w model.CatalogWork, titles []model.CatalogWorkTitle) datatypes.JSON {
	b, _ := json.Marshal(map[string]any{"work": w, "titles": titles})
	return b
}

func releaseSnapshotJSON(r model.CatalogRelease) datatypes.JSON {
	b, _ := json.Marshal(map[string]any{"release": r})
	return b
}
