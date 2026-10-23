package protocol

import "strings"

func IfNoneMatch(header, etag string) bool {
	if header == "" || etag == "" {
		return false
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" {
			return true
		}
		if stripWeak(candidate) == stripWeak(etag) {
			return true
		}
	}
	return false
}

// Cloudflare rewrites every validator it compresses to W/"…", so a browser can
// only ever send back the weak form; strong comparison refused every editor
// review decision. These validators are version tokens, not byte digests, so
// the W/ prefix carries no information here.
func IfMatch(header, etag string) bool {
	if header == "" {
		return false
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" {
			return etag != ""
		}
		if stripWeak(candidate) == stripWeak(etag) {
			return true
		}
	}
	return false
}

func stripWeak(tag string) string {
	return strings.TrimPrefix(tag, "W/")
}
