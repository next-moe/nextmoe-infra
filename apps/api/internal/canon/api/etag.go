package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
)

// withETag gives every successful GET a strong ETag over its body and
// answers If-None-Match with 304, as v2 does (D20).
func withETag(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		rec := &recorder{header: http.Header{}, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		for k, v := range rec.header {
			w.Header()[k] = v
		}
		if rec.status == http.StatusOK {
			sum := sha256.Sum256(rec.body.Bytes())
			tag := `"` + base64.RawURLEncoding.EncodeToString(sum[:18]) + `"`
			w.Header().Set("ETag", tag)
			if match(r.Header.Get("If-None-Match"), tag) {
				w.Header().Del("Content-Length")
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		w.WriteHeader(rec.status)
		_, _ = w.Write(rec.body.Bytes())
	})
}

func match(header, tag string) bool {
	for _, t := range strings.Split(header, ",") {
		t = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), "W/"))
		if t == tag || t == "*" {
			return true
		}
	}
	return false
}

type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header         { return r.header }
func (r *recorder) WriteHeader(code int)        { r.status = code }
func (r *recorder) Write(b []byte) (int, error) { return r.body.Write(b) }
