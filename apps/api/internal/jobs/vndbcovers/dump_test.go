package vndbcovers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVNDBImageRel(t *testing.T) {
	for id, want := range map[string]string{
		"cv150":    "cv/50/150.jpg",
		"cv5":      "cv/05/5.jpg",
		"cv122088": "cv/88/122088.jpg",
		"ch12":     "ch/12/12.jpg",
	} {
		got, ok := vndbImageRel(id)
		assert.True(t, ok, id)
		assert.Equal(t, want, got, id)
	}
	for _, bad := range []string{"", "cv", "cvx1", "CV12", "cv-3"} {
		_, ok := vndbImageRel(bad)
		assert.False(t, ok, bad)
	}
}
