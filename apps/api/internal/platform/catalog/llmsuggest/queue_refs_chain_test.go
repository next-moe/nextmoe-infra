package llmsuggest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVNDBLegVetoesButIsNotRequired(t *testing.T) {
	// A missing vndb id on either side is an absence, not a disagreement. As a
	// precondition it froze 2,921 dmm/steam refs at chain-unproven with the
	// reason "work holds no exact vndb anchor", all of which the staged EG row
	// confirmed the moment it was asked.
	assert.False(t, vndbDisagrees(nil, "v100"), "no anchor on our side is not a conflict")
	assert.False(t, vndbDisagrees([]string{"v100"}, ""), "an empty EG column is not a conflict")
	assert.False(t, vndbDisagrees(nil, ""))

	assert.False(t, vndbDisagrees([]string{"v100"}, "v100"))
	assert.False(t, vndbDisagrees([]string{"V100"}, "v100"), "ids are compared normalised")
	assert.False(t, vndbDisagrees([]string{"v200", "v100"}, "v100"), "one matching anchor is enough")

	assert.True(t, vndbDisagrees([]string{"v200"}, "v100"), "both sides name a vndb id and they differ")
	assert.True(t, vndbDisagrees([]string{"v200", "v300"}, "v100"))
}
