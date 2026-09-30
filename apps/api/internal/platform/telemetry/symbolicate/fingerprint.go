package symbolicate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

func MarkInApp(kind string, frames []Frame, prefixes []string) {
	for i := range frames {
		frames[i].InApp = frameInApp(kind, frames[i], prefixes)
	}
}

func frameInApp(kind string, f Frame, prefixes []string) bool {
	for _, p := range prefixes {
		if p == "" {
			continue
		}
		switch kind {
		case KindJava, KindANR:
			if strings.HasPrefix(f.Function, p) {
				return true
			}
		default:
			if strings.HasPrefix(f.File, p) {
				return true
			}
		}
	}
	return false
}

func SelectFrames(kind string, frames []Frame, prefixes []string) []Frame {
	MarkInApp(kind, frames, prefixes)
	var inApp []Frame
	for _, f := range frames {
		if f.InApp {
			inApp = append(inApp, f)
		}
	}
	if len(inApp) > 0 {
		return firstN(inApp, 5)
	}
	var nonSys []Frame
	for _, f := range frames {
		if !isSystem(kind, f) {
			nonSys = append(nonSys, f)
		}
	}
	if len(nonSys) > 0 {
		return firstN(nonSys, 5)
	}
	return firstN(frames, 5)
}

func firstN(frames []Frame, n int) []Frame {
	if len(frames) <= n {
		return frames
	}
	return frames[:n]
}

func isSystem(kind string, f Frame) bool {
	switch kind {
	case KindNative:
		p := f.Path
		if p == "" {
			p = f.Module
		}
		if strings.HasPrefix(p, "[") {
			return true
		}
		for _, pre := range []string{"/system/", "/apex/", "/vendor/", "/product/"} {
			if strings.HasPrefix(p, pre) {
				return true
			}
		}
		return false
	case KindJava, KindANR:
		for _, pre := range []string{
			"android.", "androidx.", "java.", "javax.",
			"kotlin.", "kotlinx.", "com.android.", "dalvik.",
			"libcore.", "sun.",
		} {
			if strings.HasPrefix(f.Function, pre) {
				return true
			}
		}
		return false
	default:
		return strings.HasPrefix(f.File, "dart:") || strings.HasPrefix(f.File, "package:flutter/")
	}
}

func FrameKey(f Frame) string {
	return f.Module + "|" + f.File + "|" + f.Function
}

type FingerprintInput struct {
	Kind          string
	ExceptionType string
	Message       string
	Method        string
	Path          string
	Status        string
	Frames        []Frame
}

func Fingerprint(in FingerprintInput) string {
	var parts []string
	switch in.Kind {
	case KindException:
		parts = []string{"exception", in.ExceptionType}
		parts = append(parts, keysOf(in.Frames)...)
	case KindContract:
		parts = []string{"contract", in.ExceptionType, in.Message}
		parts = append(parts, keysOf(in.Frames)...)
	case KindServer:
		parts = []string{"server", in.Method, in.Path, in.Status}
	case KindJava:
		parts = []string{"java", in.ExceptionType}
		parts = append(parts, keysOf(in.Frames)...)
	case KindANR:
		parts = []string{"anr"}
		parts = append(parts, keysOf(in.Frames)...)
	case KindNative:
		parts = []string{"native", firstToken(in.ExceptionType)}
		parts = append(parts, keysOf(in.Frames)...)
	default:
		parts = []string{in.Kind, in.ExceptionType}
		parts = append(parts, keysOf(in.Frames)...)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])[:32]
}

func keysOf(frames []Frame) []string {
	out := make([]string, len(frames))
	for i, f := range frames {
		out[i] = FrameKey(f)
	}
	return out
}

func firstToken(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, " \t("); i >= 0 {
		return s[:i]
	}
	return s
}

func Title(in FingerprintInput, mainFn string) string {
	switch in.Kind {
	case KindServer:
		return truncateChars(fmt.Sprintf("%s %s → %s", in.Method, in.Path, in.Status), 200)
	case KindANR:
		s := "ANR"
		if mainFn != "" {
			s = "ANR " + mainFn
		}
		return truncateChars(s, 200)
	default:
		if strings.TrimSpace(in.Message) == "" {
			return truncateChars(in.ExceptionType, 200)
		}
		msg := in.Message
		if i := strings.IndexByte(msg, '\n'); i >= 0 {
			msg = msg[:i]
		}
		return truncateChars(in.ExceptionType+": "+msg, 200)
	}
}

func Culprit(kind string, frames []Frame) string {
	if len(frames) == 0 {
		return ""
	}
	f := frames[0]
	if isDartFile(f.File) && f.File != "" {
		if f.Function == "" {
			return f.File
		}
		return f.Function + " (" + f.File + ")"
	}
	return f.Function
}

func isDartFile(file string) bool {
	return strings.HasPrefix(file, "package:") || strings.HasPrefix(file, "dart:") || strings.HasSuffix(file, ".dart")
}

func truncateChars(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

func AttrString(attrs map[string]any, key string) string {
	if attrs == nil {
		return ""
	}
	v, ok := attrs[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	default:
		return fmt.Sprint(t)
	}
}

func AttrStatus(attrs map[string]any, key string) string {
	if attrs == nil {
		return ""
	}
	v, ok := attrs[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case json.Number:
		i, err := t.Int64()
		if err == nil {
			return strconv.FormatInt(i, 10)
		}
		return t.String()
	default:
		return fmt.Sprint(t)
	}
}

func AttrBoolPtr(attrs map[string]any, key string) *bool {
	if attrs == nil {
		return nil
	}
	v, ok := attrs[key]
	if !ok || v == nil {
		return nil
	}
	switch t := v.(type) {
	case bool:
		b := t
		return &b
	case string:
		switch t {
		case "true":
			x := true
			return &x
		case "false":
			x := false
			return &x
		}
	}
	return nil
}

func Breadcrumbs(attrs map[string]any) []string {
	if attrs == nil {
		return []string{}
	}
	v, ok := attrs["app.breadcrumbs"]
	if !ok || v == nil {
		return []string{}
	}
	switch t := v.(type) {
	case []string:
		if t == nil {
			return []string{}
		}
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return []string{}
}
