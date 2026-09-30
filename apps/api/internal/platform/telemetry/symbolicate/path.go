package symbolicate

import (
	"path"
	"strings"
)

func NormaliseDartPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "file://")
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "package:") || strings.HasPrefix(p, "dart:") {
		return p
	}
	p = strings.ReplaceAll(p, "\\", "/")
	idx := strings.LastIndex(p, "/lib/")
	if idx < 0 {
		return path.Base(p)
	}
	dir := p[:idx]
	pkg := dir
	if i := strings.LastIndex(dir, "/"); i >= 0 {
		pkg = dir[i+1:]
	}
	pkg = stripPubVersion(pkg)
	rest := p[idx+len("/lib/"):]
	return "package:" + pkg + "/" + rest
}

func stripPubVersion(name string) string {
	i := strings.LastIndex(name, "-")
	if i <= 0 || i+1 >= len(name) {
		return name
	}
	if name[i+1] >= '0' && name[i+1] <= '9' {
		return name[:i]
	}
	return name
}

func dartFileBase(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
