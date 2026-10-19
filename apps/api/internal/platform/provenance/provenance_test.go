package provenance_test

import (
	"encoding/json"
	"testing"

	"api/internal/platform/provenance"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsHuman(t *testing.T) {
	for _, source := range []string{"user", "curated"} {
		assert.True(t, provenance.IsHuman(source), "%q is a human lane", source)
	}
	for _, source := range []string{"vndb", "bangumi", "dlsite", "derived", "upscale", ""} {
		assert.False(t, provenance.IsHuman(source), "%q is a machine importer", source)
	}
	assert.Equal(t, []string{"user", "curated"}, provenance.HumanSources())
}

func TestFirstSource(t *testing.T) {
	doc := []byte(`{"display_name":[{"source":"curated","at":"2026-08-15T00:00:00Z"},` +
		`{"source":"vndb","at":"2026-01-01T00:00:00Z"}],"olang":[]}`)

	assert.Equal(t, "curated", provenance.FirstSource(doc, "display_name"),
		"the array head is the most recent writer")
	assert.Empty(t, provenance.FirstSource(doc, "olang"))
	assert.Empty(t, provenance.FirstSource(doc, "content_rating"))
	assert.Empty(t, provenance.FirstSource(nil, "display_name"))
	assert.Empty(t, provenance.FirstSource([]byte(`not json`), "display_name"))
}

func TestPrependPutsTheNewWriteAtTheHead(t *testing.T) {
	doc := map[string]json.RawMessage{
		"cup": json.RawMessage(`[{"source":"vndb","at":"2026-01-01T00:00:00Z"}]`),
	}
	provenance.Prepend(doc, "cup", "getchu", "2026-10-19T00:00:00Z")
	provenance.Prepend(doc, "hip_cm", "getchu", "2026-10-19T00:00:00Z")

	var cup []provenance.Entry
	require.NoError(t, json.Unmarshal(doc["cup"], &cup))
	assert.Equal(t, []provenance.Entry{
		{Source: "getchu", At: "2026-10-19T00:00:00Z"},
		{Source: "vndb", At: "2026-01-01T00:00:00Z"},
	}, cup)
	assert.Equal(t, "getchu", provenance.Head(doc["hip_cm"]))
	assert.Empty(t, provenance.Head(doc["waist_cm"]))
	assert.Empty(t, provenance.Head(json.RawMessage(`[]`)))
}
