package bgmimages

import (
	"context"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
)

const (
	coverName   = "cover.jpg"
	logoBase    = "logo"
	logoStaging = "logo.download"
)

type imageSizes struct {
	Images struct {
		Large  string `json:"large"`
		Medium string `json:"medium"`
		Grid   string `json:"grid"`
	} `json:"images"`
}

func coverURL(body []byte) (string, error) {
	var s imageSizes
	if err := json.Unmarshal(body, &s); err != nil {
		return "", err
	}
	return normalizeURL(s.Images.Large), nil
}

func personURL(body []byte) (string, error) {
	var s imageSizes
	if err := json.Unmarshal(body, &s); err != nil {
		return "", err
	}
	for _, u := range []string{s.Images.Large, s.Images.Medium, s.Images.Grid} {
		if u = normalizeURL(u); u != "" {
			return u, nil
		}
	}
	return "", nil
}

func (r *runner) cover(ctx context.Context, id int) error {
	sid := strconv.Itoa(id)
	rel := path.Join(sid, coverName)
	dest := filepath.Join(r.opts.Out, sid, coverName)
	if fileExists(dest) {
		atomic.AddInt64(&r.stats.SkippedExist, 1)
		if !r.recorded[sid] {
			if err := r.recordCover(id, dest, rel); err != nil {
				r.fail(id, "manifest backfill", err)
			}
		}
		return nil
	}

	body, status, err := r.http.getJSON(ctx, apiURL(r.opts.APIBase, "subjects", id))
	if err != nil {
		return r.apiFailure(ctx, id, status, err)
	}
	u, err := coverURL(body)
	if err != nil {
		r.fail(id, "parse subject", err)
		return nil
	}
	if u == "" {
		atomic.AddInt64(&r.stats.NoImage, 1)
		return nil
	}
	if _, err := r.http.download(ctx, u, dest); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r.fail(id, "download "+u, err)
		return nil
	}
	if err := r.recordCover(id, dest, rel); err != nil {
		r.fail(id, "record dims", err)
		return nil
	}
	atomic.AddInt64(&r.stats.Downloaded, 1)
	return nil
}

func (r *runner) recordCover(id int, dest, rel string) error {
	w, h, _, err := readDims(dest)
	if err != nil {
		return err
	}
	return r.append(manifestRecord{SubjectID: id, File: rel, W: w, H: h})
}

func (r *runner) person(ctx context.Context, id int) error {
	sid := strconv.Itoa(id)
	dir := filepath.Join(r.opts.Out, sid)
	if existing := existingLogo(dir); existing != "" {
		atomic.AddInt64(&r.stats.SkippedExist, 1)
		if !r.recorded[sid] {
			if err := r.recordPerson(id, existing, ""); err != nil {
				r.fail(id, "manifest backfill", err)
			}
		}
		return nil
	}

	body, status, err := r.http.getJSON(ctx, apiURL(r.opts.APIBase, "persons", id))
	if err != nil {
		return r.apiFailure(ctx, id, status, err)
	}
	u, err := personURL(body)
	if err != nil {
		r.fail(id, "parse person", err)
		return nil
	}
	if u == "" {
		atomic.AddInt64(&r.stats.NoImage, 1)
		return nil
	}
	staging := filepath.Join(dir, logoStaging)
	contentType, err := r.http.download(ctx, u, staging)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r.fail(id, "download "+u, err)
		return nil
	}
	_, _, format, decodeErr := readDims(staging)
	dest := filepath.Join(dir, logoBase+"."+logoExt(format, contentType, u))
	if err := os.Rename(staging, dest); err != nil {
		r.fail(id, "rename staging file", err)
		return nil
	}
	if decodeErr != nil {
		r.fail(id, "decode "+dest, decodeErr)
		return nil
	}
	if err := r.recordPerson(id, dest, u); err != nil {
		r.fail(id, "record dims", err)
		return nil
	}
	atomic.AddInt64(&r.stats.Downloaded, 1)
	return nil
}

// The kun-bangumi-api writer put the bare basename in file; the infra readers
// join file onto the mirror root, so they found these logos only through their
// <root>/<id>/logo.<ext> fallback.
func (r *runner) recordPerson(id int, dest, srcURL string) error {
	w, h, _, err := readDims(dest)
	if err != nil {
		return err
	}
	sid := strconv.Itoa(id)
	return r.append(manifestRecord{ID: sid, File: path.Join(sid, filepath.Base(dest)), W: w, H: h, URL: srcURL})
}

func existingLogo(dir string) string {
	matches, err := filepath.Glob(filepath.Join(dir, logoBase+".*"))
	if err != nil {
		return ""
	}
	for _, m := range matches {
		base := filepath.Base(m)
		if base != logoStaging && !strings.Contains(base, ".part-") && fileExists(m) {
			return m
		}
	}
	return ""
}

func logoExt(format, contentType, url string) string {
	switch format {
	case "jpeg":
		return "jpg"
	case "png", "gif", "webp":
		return format
	}
	ct := strings.ToLower(contentType)
	for _, ext := range []string{"png", "webp", "gif"} {
		if strings.Contains(ct, ext) {
			return ext
		}
	}
	u := strings.ToLower(url)
	for _, ext := range []string{"png", "webp", "gif"} {
		if strings.HasSuffix(u, "."+ext) {
			return ext
		}
	}
	return "jpg"
}
