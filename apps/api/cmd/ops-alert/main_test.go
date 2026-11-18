package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenderKeepsTheTailAndEscapes(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= keepLines+5; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	b.WriteString("<script>x</script>\n")
	out := render("2 repeats suppressed", b.String())
	assert.Contains(t, out, "[... 6 earlier lines omitted ...]")
	assert.NotContains(t, out, "line 6\n")
	assert.Contains(t, out, "line 7\n")
	assert.Contains(t, out, "&lt;script&gt;")
	assert.True(t, strings.HasPrefix(out, "<p><b>2 repeats suppressed</b></p>"))
}
