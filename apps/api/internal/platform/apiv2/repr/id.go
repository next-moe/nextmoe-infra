package repr

import (
	"strconv"
	"time"
)

func ID(n int64) string {
	if n <= 0 {
		return ""
	}
	return strconv.FormatInt(n, 10)
}

func ParseID(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func TimeUTC(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05Z")
}

func Cursor(payload string) string {
	if payload == "" {
		return ""
	}
	return "cur_" + payload
}

func ParseCursor(s string) (string, bool) {
	const p = "cur_"
	if len(s) <= len(p) || s[:len(p)] != p {
		return "", false
	}
	return s[len(p):], true
}
