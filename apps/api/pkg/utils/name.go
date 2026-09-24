package utils

import (
	"regexp"
	"strings"
)

// invisibleNameChars are Unicode codepoints that are visually empty
// (whitespace / zero-width / format control / unassigned space-like)
// and MUST NOT appear in a username. Allowing any of these would let
// an attacker register a name that renders identically to an existing
// account ("kun" vs "kun<ZWSP>") and impersonate them.
//
// Written as `\u` / `\U` escapes — NOT raw bytes — for two reasons:
//  1. Go's parser rejects U+FEFF (BOM) inside string literals; raw-
//     byte form fails to compile.
//  2. Raw zero-width bytes silently corrupt on any future Read/Edit
//     roundtrip through editors that normalize whitespace.
//
// Mirrored from the legacy JS isValidName rule. The JS source used 4-
// digit `\uXXXX` escapes for some plane-1 codepoints (e.g. JS `ᴕ9`
// parses as `ᴕ` + `9` — a latent bug that made those entries
// no-ops). Go's `\U00012345` 8-digit escape works correctly for those,
// so the INTENT of the legacy rule actually executes here. If you sync
// the JS side, fix it there too.
var invisibleNameChars = []string{
	"	",
	" ",
	" ",
	"­",
	"͏",
	"؜",
	"ᅟ",
	"ᅠ",
	"឴",
	"឵",
	"᠎",
	" ",
	" ",
	" ",
	" ",
	" ",
	" ",
	" ",
	" ",
	" ",
	" ",
	" ",
	"​",
	"‌",
	"‍",
	"‎",
	"‏",
	" ",
	" ",
	"⁠",
	"⁡",
	"⁢",
	"⁣",
	"⁤",
	"⁥",
	"⁪",
	"⁫",
	"⁬",
	"⁭",
	"⁮",
	"⁯",
	"　",
	"⠀",
	"ㅤ",
	"\uFEFF",
	"ﾠ",
	"\U0001D159",
	"\U0001D173",
	"\U0001D174",
	"\U0001D175",
	"\U0001D176",
	"\U0001D177",
	"\U0001D178",
	"\U0001D179",
	"\U0001D17A",
	"\U000E0020",
}

var validNameRegex = regexp.MustCompile(`^[\pL\pN!~_@#$%^&*()+=\-]{1,17}$`)

func IsValidName(name string) bool {
	if strings.HasPrefix(name, "已注销") {
		return false
	}
	for _, ch := range invisibleNameChars {
		if strings.Contains(name, ch) {
			return false
		}
	}
	return validNameRegex.MatchString(name)
}
