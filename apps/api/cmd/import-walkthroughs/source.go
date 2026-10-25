package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"gorm.io/gorm"
)

var chartSites = []string{"seiya", "sagaoz_foolmaker"}

type sourceEntry struct {
	ID        int64
	Site      string
	SourceURL string
	Title     string
	Author    string
	UpdatedOn string
	BodyHTML  string
	GameID    *int64
}

type entryFilter struct {
	IDs   []int64
	Limit int
}

func loadEntries(ctx context.Context, src *gorm.DB, f entryFilter) ([]sourceEntry, error) {
	q := src.WithContext(ctx).Table("entry").
		Select("id, site, source_url, title, author, updated_on_site AS updated_on, body_html, game_id").
		Where("kind = 'walkthrough' AND site IN ? AND body_html <> ''", chartSites).
		Where("title !~ '�' AND body_html !~ '�{3,}'").
		Order("id")
	if len(f.IDs) > 0 {
		q = q.Where("id IN ?", f.IDs)
	}
	if f.Limit > 0 {
		q = q.Limit(f.Limit)
	}
	var out []sourceEntry
	return out, q.Scan(&out).Error
}

var blockTags = map[string]bool{
	"p": true, "div": true, "br": true, "tr": true, "li": true, "table": true, "hr": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "dt": true, "dd": true,
}

var skipTags = map[string]bool{"script": true, "style": true, "iframe": true, "noscript": true}

var (
	inlineSpace = regexp.MustCompile(`[ \t\x{3000}]+`)
	lineBreaks  = regexp.MustCompile(` *\n[ |\n]*`)
)

func chartText(body string) string {
	z := html.NewTokenizer(strings.NewReader(body))
	var b strings.Builder
	skip := 0
	for {
		switch z.Next() {
		case html.ErrorToken:
			t := strings.ReplaceAll(b.String(), " ", " ")
			t = inlineSpace.ReplaceAllString(t, " ")
			t = lineBreaks.ReplaceAllString(t, "\n")
			return strings.TrimSpace(t)
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			if skipTags[tag] {
				skip++
			}
			if blockTags[tag] {
				b.WriteByte('\n')
			}
			if tag == "td" || tag == "th" {
				b.WriteString(" | ")
			}
			if tag == "img" && hasAttr {
				for {
					k, v, more := z.TagAttr()
					if string(k) == "alt" && len(v) > 0 {
						b.WriteString("[" + string(v) + "]")
					}
					if !more {
						break
					}
				}
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			tag := string(name)
			if skipTags[tag] && skip > 0 {
				skip--
			}
			if blockTags[tag] {
				b.WriteByte('\n')
			}
		case html.TextToken:
			if skip == 0 {
				b.Write(z.Text())
			}
		}
	}
}

func hashText(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
