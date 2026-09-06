package vndbcovers

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// The Kana API admits ~200 requests per 5 minutes, so the metadata phase alone
// for a full-population run spends ~45 minutes being rate-limited, and VNDB asks
// bulk consumers to use the daily database dump instead (vndb.org/d14). A
// manifest is that dump reduced to this job's fields; rows with an empty url are
// VNs the dump knows to have no cover.
var manifestHeader = []string{"vndb_id", "url", "width", "height", "sexual", "violence"}

func loadManifest(path string) (map[string]*vnImage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	rd := csv.NewReader(f)
	header, err := rd.Read()
	if err != nil {
		return nil, fmt.Errorf("read manifest header: %w", err)
	}
	if strings.Join(header, ",") != strings.Join(manifestHeader, ",") {
		return nil, fmt.Errorf("manifest header %q, want %q", strings.Join(header, ","), strings.Join(manifestHeader, ","))
	}

	out := make(map[string]*vnImage)
	for line := 2; ; line++ {
		rec, err := rd.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("manifest line %d: %w", line, err)
		}
		id := strings.TrimSpace(rec[0])
		if id == "" {
			return nil, fmt.Errorf("manifest line %d: empty vndb_id", line)
		}
		u := strings.TrimSpace(rec[1])
		if u == "" {
			out[id] = nil
			continue
		}
		img := &vnImage{URL: u}
		w, err := strconv.Atoi(rec[2])
		if err != nil {
			return nil, fmt.Errorf("manifest line %d: width %q", line, rec[2])
		}
		h, err := strconv.Atoi(rec[3])
		if err != nil {
			return nil, fmt.Errorf("manifest line %d: height %q", line, rec[3])
		}
		if w > 0 && h > 0 {
			img.Dims = []int{w, h}
		}
		if img.Sexual, err = strconv.ParseFloat(rec[4], 64); err != nil {
			return nil, fmt.Errorf("manifest line %d: sexual %q", line, rec[4])
		}
		if img.Violence, err = strconv.ParseFloat(rec[5], 64); err != nil {
			return nil, fmt.Errorf("manifest line %d: violence %q", line, rec[5])
		}
		out[id] = img
	}
	return out, nil
}
