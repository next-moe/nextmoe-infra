package symbolicate

import (
	"fmt"
	"sort"
	"strings"
)

func SynthesizeDartStack(buildID string, textStart uint64, pcs []uint64) string {
	var b strings.Builder
	b.WriteString("*** *** *** *** *** *** *** *** *** *** *** *** *** *** *** ***\n")
	fmt.Fprintf(&b, "build_id: '%s'\n", buildID)
	b.WriteString("isolate_dso_base: 0, vm_dso_base: 0\n")
	for i, pc := range pcs {
		off := uint64(0)
		if pc >= textStart {
			off = pc - textStart
		}
		fmt.Fprintf(&b, "    #%02d abs %016x virt %016x _kDartSnapshotText+0x%x\n", i, pc, pc, off)
	}
	return b.String()
}

func FormatNativeStack(frames []Frame) string {
	var b strings.Builder
	for i, f := range frames {
		fmt.Fprintf(&b, "#%02d", i)
		if f.Module != "" {
			b.WriteString("  ")
			b.WriteString(f.Module)
		}
		if f.Function != "" {
			b.WriteString("  ")
			b.WriteString(f.Function)
		}
		if f.File != "" {
			if f.Line > 0 {
				fmt.Fprintf(&b, " (%s:%d)", f.File, f.Line)
			} else {
				fmt.Fprintf(&b, " (%s)", f.File)
			}
		}
		if i < len(frames)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func FormatDartStack(header string, frames []Frame) string {
	var b strings.Builder
	if h := strings.TrimRight(header, "\n"); h != "" {
		b.WriteString(h)
		b.WriteByte('\n')
	}
	for i, f := range frames {
		fmt.Fprintf(&b, "#%d      %s", i, f.Function)
		if f.File != "" {
			if f.Line > 0 {
				fmt.Fprintf(&b, " (%s:%d)", f.File, f.Line)
			} else {
				fmt.Fprintf(&b, " (%s)", f.File)
			}
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func DartHeader(stack string) string {
	var lines []string
	for _, line := range splitLines(stack) {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "#") {
			break
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func NeedsList(items ...string) string {
	seen := map[string]struct{}{}
	var out []string
	for _, it := range items {
		it = strings.TrimSpace(it)
		if it == "" {
			continue
		}
		if _, ok := seen[it]; ok {
			continue
		}
		seen[it] = struct{}{}
		out = append(out, it)
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

func SplitNeeds(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}

func ExceptionTypeFromRetrace(stack string) (typ, msg string) {
	for _, line := range splitLines(stack) {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "at ") || strings.HasPrefix(trim, "Caused by:") {
			continue
		}
		if i := strings.Index(trim, ":"); i >= 0 {
			return strings.TrimSpace(trim[:i]), strings.TrimSpace(trim[i+1:])
		}
		return trim, ""
	}
	return "", ""
}
