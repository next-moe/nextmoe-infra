package symbolicate

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var dartBuildIDRe = regexp.MustCompile(`(?m)build_id:\s*'([0-9a-fA-F]+)'`)

func DartBuildID(stack string) string {
	m := dartBuildIDRe.FindStringSubmatch(stack)
	if len(m) != 2 {
		return ""
	}
	return strings.ToLower(m[1])
}

func ParseDecodedDart(stack string) []Frame {
	var out []Frame
	for _, line := range splitLines(stack) {
		trim := strings.TrimSpace(line)
		if trim == "" || isAsyncSuspension(trim) {
			continue
		}
		if !strings.HasPrefix(trim, "#") {
			continue
		}
		fn, loc, ok := splitDecodedDart(trim)
		if !ok {
			continue
		}
		file, lineNo := splitLocation(loc)
		out = append(out, Frame{
			Function: strings.TrimSpace(fn),
			File:     NormaliseDartPath(file),
			Line:     lineNo,
			Raw:      strings.TrimSpace(line),
		})
	}
	return out
}

func ParseUndecodedDart(stack string) []Frame {
	var out []Frame
	for _, line := range splitLines(stack) {
		trim := strings.TrimSpace(line)
		if !strings.HasPrefix(trim, "#") {
			continue
		}
		fn, ok := undecodedDartSymbol(trim)
		if !ok {
			continue
		}
		out = append(out, Frame{
			Function: fn,
			Raw:      strings.TrimSpace(line),
		})
	}
	return out
}

func ParseDartStack(stack string) []Frame {
	if DartBuildID(stack) != "" {
		decoded := ParseDecodedDart(stack)
		if len(decoded) > 0 {
			return decoded
		}
		return ParseUndecodedDart(stack)
	}
	return ParseDecodedDart(stack)
}

func splitDecodedDart(line string) (function, loc string, ok bool) {
	line = strings.TrimSpace(line)
	hash := 0
	for hash < len(line) && line[hash] == '#' {
		hash++
	}
	i := hash
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i == hash {
		return "", "", false
	}
	rest := strings.TrimSpace(line[i:])
	open := strings.LastIndex(rest, "(")
	close := strings.LastIndex(rest, ")")
	if open < 0 || close < open {
		return "", "", false
	}
	function = strings.TrimSpace(rest[:open])
	loc = strings.TrimSpace(rest[open+1 : close])
	if function == "" {
		return "", "", false
	}
	return function, loc, true
}

func undecodedDartSymbol(line string) (string, bool) {
	const marker = "_kDartSnapshotText+"
	i := strings.Index(line, marker)
	if i < 0 {
		return "", false
	}
	sym := strings.TrimSpace(line[i:])
	if j := strings.IndexAny(sym, " \t"); j >= 0 {
		sym = sym[:j]
	}
	return sym, sym != ""
}

func splitLocation(loc string) (file string, line int) {
	loc = strings.TrimSpace(loc)
	if loc == "" {
		return "", 0
	}
	col := 0
	if i := strings.LastIndex(loc, ":"); i >= 0 {
		if n, err := strconv.Atoi(loc[i+1:]); err == nil {
			col = n
			loc = loc[:i]
			_ = col
		}
	}
	if i := strings.LastIndex(loc, ":"); i >= 0 {
		if n, err := strconv.Atoi(loc[i+1:]); err == nil {
			return loc[:i], n
		}
	}
	return loc, 0
}

func isAsyncSuspension(s string) bool {
	return strings.TrimSpace(s) == "<asynchronous suspension>"
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n")
}

func ParseJava(stack string) []Frame {
	var out []Frame
	for _, line := range splitLines(stack) {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "Caused by:") {
			break
		}
		f, ok := parseJavaFrame(trim)
		if !ok {
			continue
		}
		out = append(out, f)
	}
	return out
}

func parseJavaFrame(raw string) (Frame, bool) {
	trim := strings.TrimSpace(raw)
	if !strings.HasPrefix(trim, "at ") {
		return Frame{}, false
	}
	trim = strings.TrimSpace(trim[3:])
	open := strings.LastIndex(trim, "(")
	close := strings.LastIndex(trim, ")")
	if open < 1 || close < open {
		return Frame{}, false
	}
	fn := strings.TrimSpace(trim[:open])
	if fn == "" {
		return Frame{}, false
	}
	inside := strings.TrimSpace(trim[open+1 : close])
	file := inside
	lineNo := 0
	switch inside {
	case "Native Method", "Unknown Source", "":
		file = ""
	default:
		if i := strings.LastIndex(inside, ":"); i >= 0 {
			if n, err := strconv.Atoi(inside[i+1:]); err == nil {
				lineNo = n
				file = inside[:i]
			}
		}
	}
	return Frame{
		Function: fn,
		File:     file,
		Line:     lineNo,
		Raw:      "at " + trim,
	}, true
}

func MainThreadSection(text string) string {
	lines := splitLines(text)
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), `"main"`) {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return text
	}
	var out []string
	for _, line := range lines[start:] {
		t := strings.TrimLeft(line, " \t")
		if t == "" || strings.HasPrefix(t, `"`) {
			break
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

var debuggerdLine = regexp.MustCompile(`^\s*#(\d+)\s+pc\s+([0-9a-fA-F]+)\s+(.*)$`)

func ParseDebuggerd(stack string) []Frame {
	var out []Frame
	for _, line := range splitLines(stack) {
		m := debuggerdLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		pc, err := strconv.ParseUint(m[2], 16, 64)
		if err != nil {
			continue
		}
		modulePath, fn, buildID := parseDebuggerdRest(strings.TrimSpace(m[3]))
		fn = stripPlusOffset(fn)
		if fn == "" {
			fn = fmt.Sprintf("+0x%x", pc)
		}
		out = append(out, Frame{
			Function: fn,
			Module:   dartFileBase(modulePath),
			Path:     modulePath,
			Raw:      strings.TrimSpace(line),
			PC:       pc,
			BuildID:  strings.ToLower(buildID),
		})
		if len(out) >= 64 {
			break
		}
	}
	return out
}

func parseDebuggerdRest(rest string) (module, function, buildID string) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", "", ""
	}
	if strings.HasPrefix(rest, "[") {
		end := strings.Index(rest, "]")
		if end >= 0 {
			module = rest[:end+1]
			rest = strings.TrimSpace(rest[end+1:])
		}
	} else {
		sp := strings.IndexAny(rest, " \t")
		if sp < 0 {
			return rest, "", ""
		}
		module = rest[:sp]
		rest = strings.TrimSpace(rest[sp:])
	}
	for rest != "" {
		if !strings.HasPrefix(rest, "(") {
			break
		}
		end := strings.Index(rest, ")")
		if end < 0 {
			break
		}
		inner := rest[1:end]
		rest = strings.TrimSpace(rest[end+1:])
		if bid, ok := strings.CutPrefix(inner, "BuildId:"); ok {
			buildID = strings.TrimSpace(bid)
			continue
		}
		if function == "" {
			function = inner
		}
	}
	return module, function, buildID
}

func stripPlusOffset(fn string) string {
	i := strings.LastIndex(fn, "+")
	if i <= 0 {
		return fn
	}
	off := fn[i+1:]
	if strings.HasPrefix(off, "0x") || strings.HasPrefix(off, "0X") {
		off = off[2:]
	}
	if off == "" {
		return fn
	}
	for _, c := range off {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return fn
		}
	}
	return fn[:i]
}
