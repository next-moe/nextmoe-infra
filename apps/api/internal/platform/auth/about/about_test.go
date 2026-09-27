package about

import (
	"strings"
	"testing"
)

func TestCook(t *testing.T) {
	cases := []struct {
		name, src string
		has       []string
		hasNot    []string
	}{
		{"empty", "  \n", nil, []string{"<"}},
		{"emphasis and line breaks", "**喜欢** *galgame*\n第二行",
			[]string{"<strong>喜欢</strong>", "<em>galgame</em>", "<br"}, nil},
		{"headings start at h3", "# 大标题\n## 小标题",
			[]string{"<h3>大标题</h3>", "<h4>小标题</h4>"}, []string{"<h1", "<h2"}},
		{"links open elsewhere without passing rank", "[主页](https://example.com)",
			[]string{`href="https://example.com"`, `nofollow`, `target="_blank"`}, nil},
		{"an image is a link to it", "![立绘](https://example.com/a.png)",
			[]string{`href="https://example.com/a.png"`, ">立绘</a>"}, []string{"<img"}},
		{"raw html is dropped", "<script>alert(1)</script><b onclick=x>hi</b>",
			nil, []string{"<script", "onclick", "alert(1)"}},
		{"javascript links lose their href", "[x](javascript:alert(1))",
			nil, []string{"javascript:"}},
		{"relative links lose their href", "[x](/admin)",
			nil, []string{`href="/admin"`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Cook(c.src)
			for _, want := range c.has {
				if !strings.Contains(got, want) {
					t.Errorf("Cook(%q) = %q, missing %q", c.src, got, want)
				}
			}
			for _, bad := range c.hasNot {
				if strings.Contains(got, bad) {
					t.Errorf("Cook(%q) = %q, should not contain %q", c.src, got, bad)
				}
			}
		})
	}
}
