package vndbtitles

import "strings"

const vndbLangEnglish = "en"

type existingTitle struct {
	Lang  string
	Title string
}

type decisionIn struct {
	Titles       []existingTitle
	DisplayName  string
	DisplayHuman bool
	OLang        string
	OLangTitle   string
	ENTitle      string
}

type decision struct {
	InsertOLang      bool
	InsertEN         bool
	OLangPresent     bool
	ENPresent        bool
	FillDisplay      bool
	SkipDisplayHuman bool
}

func (d decision) writes() bool {
	return d.InsertOLang || d.InsertEN || d.FillDisplay
}

func decide(in decisionIn) decision {
	var d decision
	if hasTitle(in.Titles, in.OLangTitle) {
		d.OLangPresent = true
	} else {
		d.InsertOLang = true
	}
	if in.OLang != vndbLangEnglish && in.ENTitle != "" {
		if in.ENTitle == in.OLangTitle || hasTitle(in.Titles, in.ENTitle) {
			d.ENPresent = true
		} else {
			d.InsertEN = true
		}
	}
	if strings.Trim(in.DisplayName, " ") == "" {
		if in.DisplayHuman {
			d.SkipDisplayHuman = true
		} else {
			d.FillDisplay = true
		}
	}
	return d
}

// The 2026-07-06 wiki fold filed 1,217 original-language titles under another
// language (a Russian title as en); matching on the language too would add the
// same string to the work a second time.
func hasTitle(titles []existingTitle, title string) bool {
	for _, t := range titles {
		if t.Title == title {
			return true
		}
	}
	return false
}
