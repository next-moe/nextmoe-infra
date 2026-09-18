package hltbattach

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKnownDates(t *testing.T) {
	assert.Equal(t, []string{"2021-11-18"}, knownDates("2021-11-18", ""))
	assert.Equal(t, []string{"2021-11-18"}, knownDates("", "2021-11-18"))
	assert.Empty(t, knownDates("0000-00-00", ""))
	assert.Empty(t, knownDates("2021-00-00", ""))
	assert.Empty(t, knownDates("2021-11-00", ""))
	assert.Empty(t, knownDates("", ""))
}
