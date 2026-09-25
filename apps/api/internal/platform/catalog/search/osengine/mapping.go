package osengine

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed analysis.json
var analysisJSON []byte

const (
	SchemaVersion   = 2
	maxResultWindow = 500000

	IndexCreditNames = "catalog_credit_names"
	IndexCharacters  = "catalog_characters"
	IndexLabels      = "catalog_labels"
	IndexWorks       = "catalog_works"
	IndexTags        = "catalog_tags"
	IndexSeries      = "catalog_series"
	IndexEngines     = "catalog_engines"
	IndexTraits      = "catalog_traits"
)

var IndexUIDs = []string{
	IndexCreditNames,
	IndexCharacters,
	IndexLabels,
	IndexWorks,
	IndexTags,
	IndexSeries,
	IndexEngines,
	IndexTraits,
}

var (
	jaCopy  = []string{"titles", "titles_ja", "titles_ngram", "titles_kw"}
	zhCopy  = []string{"titles", "titles_zh", "titles_ngram", "titles_pinyin", "titles_kw"}
	mixCopy = []string{"titles", "titles_ngram", "titles_kw"}
)

func IndexBody(uid string) (map[string]any, error) {
	props, err := propertiesFor(uid)
	if err != nil {
		return nil, err
	}
	var analysis map[string]any
	if err := json.Unmarshal(analysisJSON, &analysis); err != nil {
		return nil, fmt.Errorf("analysis.json: %w", err)
	}
	return map[string]any{
		"settings": map[string]any{
			"index": map[string]any{
				"number_of_shards":   1,
				"number_of_replicas": 0,
				"max_result_window":  maxResultWindow,
			},
			"analysis": analysis,
		},
		"mappings": map[string]any{
			"dynamic":    false,
			"_meta":      map[string]any{"schema_version": SchemaVersion},
			"properties": props,
		},
	}, nil
}

func propertiesFor(uid string) (map[string]any, error) {
	props := nameProperties()
	switch uid {
	case IndexWorks:
		props["intro_ja"] = textField("ja", nil)
		props["intro_zh"] = textField("zh", nil)
		props["intro_other"] = textField("mixed", nil)
		props["content_rating"] = map[string]any{"type": "integer"}
		props["claimed"] = map[string]any{"type": "boolean"}
		props["claim_state"] = keywordField()
		props["content_limit"] = keywordField()
		props["olang"] = keywordField()
		props["tag_ids"] = map[string]any{"type": "long"}
		props["label_ids"] = map[string]any{"type": "long"}
		props["engine_ids"] = map[string]any{"type": "long"}
		props["series_ids"] = map[string]any{"type": "long"}
		props["released_ord"] = map[string]any{"type": "integer"}
		props["updated_ts"] = map[string]any{"type": "long"}
	case IndexCreditNames:
		props["person_id"] = map[string]any{"type": "long"}
	case IndexLabels:
		props["kind"] = map[string]any{"type": "integer"}
	case IndexTags:
		props["kind"] = map[string]any{"type": "integer"}
		props["tier"] = map[string]any{"type": "integer"}
	case IndexCharacters:
		props["catalog_id"] = map[string]any{"type": "long"}
		props["gender"] = map[string]any{"type": "integer"}
		props["trait_ids"] = map[string]any{"type": "long"}
		props["trait_ids_sfw"] = map[string]any{"type": "long"}
	case IndexSeries, IndexEngines:
	case IndexTraits:
		props["sexual"] = map[string]any{"type": "boolean"}
	default:
		return nil, fmt.Errorf("unknown index %q", uid)
	}
	return props, nil
}

func nameProperties() map[string]any {
	props := map[string]any{
		"id":            keywordField(),
		"entity_type":   keywordField(),
		"name_ja":       textField("ja", jaCopy),
		"name_zh":       textField("zh", zhCopy),
		"name_other":    textField("mixed", mixCopy),
		"aliases_ja":    textField("ja", jaCopy),
		"aliases_zh":    textField("zh", zhCopy),
		"aliases_other": textField("mixed", mixCopy),
		"latin":         textField("latin", mixCopy),
		"sources":       keywordField(),
		"source_keys":   keywordField(),
		"popularity":    map[string]any{"type": "float"},
		"titles":        textField("mixed", nil),
		"titles_ja":     textField("ja", nil),
		"titles_zh":     textField("zh", nil),
		"titles_ngram":  textField("cjk_ngram", nil),
		"titles_pinyin": textField("zh_pinyin", nil),
		"titles_kw": map[string]any{
			"type":         "keyword",
			"normalizer":   "kw_norm",
			"ignore_above": 128,
		},
	}
	return props
}

func textField(analyzer string, copyTo []string) map[string]any {
	f := map[string]any{"type": "text", "analyzer": analyzer}
	if copyTo != nil {
		f["copy_to"] = copyTo
	}
	return f
}

func keywordField() map[string]any {
	return map[string]any{"type": "keyword"}
}
