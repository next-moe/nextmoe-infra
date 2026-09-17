package bgmimages

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func jpegHeader(sof byte, w, h int, comps [3][3]byte) []byte {
	var b []byte
	b = append(b, 0xff, 0xd8)
	app0 := []byte("JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00")
	b = append(b, 0xff, 0xe0, byte((len(app0)+2)>>8), byte(len(app0)+2))
	b = append(b, app0...)
	dqt := make([]byte, 65)
	b = append(b, 0xff, 0xdb, byte((len(dqt)+2)>>8), byte(len(dqt)+2))
	b = append(b, dqt...)
	frame := []byte{8, byte(h >> 8), byte(h), byte(w >> 8), byte(w), 3}
	for _, c := range comps {
		frame = append(frame, c[0], c[1]<<4|c[2], 0)
	}
	b = append(b, 0xff, sof, byte((len(frame)+2)>>8), byte(len(frame)+2))
	b = append(b, frame...)
	sos := []byte{3, 1, 0, 2, 0, 3, 0, 0, 0x3f, 0}
	b = append(b, 0xff, 0xda, byte((len(sos)+2)>>8), byte(len(sos)+2))
	b = append(b, sos...)
	b = append(b, 0xff, 0xd9)
	return b
}

func TestJPEGSizeReadsTheFrameHeaderImageJPEGRefuses(t *testing.T) {
	cases := []struct {
		id         int
		sof        byte
		w, h       int
		components [3][3]byte
	}{
		{2332, 0xc0, 165, 75, [3][3]byte{{1, 2, 2}, {2, 1, 1}, {3, 1, 3}}},
		{6500, 0xc2, 400, 80, [3][3]byte{{1, 2, 2}, {2, 1, 1}, {3, 1, 3}}},
		{88634, 0xc0, 185, 68, [3][3]byte{{1, 2, 2}, {2, 1, 1}, {3, 3, 2}}},
		{93816, 0xc2, 330, 308, [3][3]byte{{1, 2, 2}, {2, 1, 1}, {3, 3, 2}}},
	}
	for _, c := range cases {
		data := jpegHeader(c.sof, c.w, c.h, c.components)
		_, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err == nil || !strings.Contains(err.Error(), "luma/chroma subsampling ratio") {
			t.Fatalf("person %d: DecodeConfig = %v, want luma/chroma subsampling ratio", c.id, err)
		}
		path := filepath.Join(t.TempDir(), "logo.jpg")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		w, h, format, err := readDims(path)
		if err != nil || w != c.w || h != c.h || format != "jpeg" {
			t.Fatalf("person %d: readDims = %d %d %q %v, want %d %d jpeg nil", c.id, w, h, format, err, c.w, c.h)
		}
	}
}

func TestJPEGSizeRejectsBrokenHeaders(t *testing.T) {
	truncated := []byte{0xff, 0xd8, 0xff, 0xc0, 0x00, 0x11, 0x08, 0x00}
	zeroH := jpegHeader(0xc0, 10, 0, [3][3]byte{{1, 1, 1}, {2, 1, 1}, {3, 1, 1}})
	dht := []byte{8, 0, 1, 0, 1, 0, 0, 0}
	fillAndRST := []byte{0xff, 0xd8, 0xff, 0xff, 0xd0, 0xff, 0xc4, byte((len(dht) + 2) >> 8), byte(len(dht) + 2)}
	fillAndRST = append(fillAndRST, dht...)
	fillAndRST = append(fillAndRST, jpegHeader(0xc0, 12, 34, [3][3]byte{{1, 1, 1}, {2, 1, 1}, {3, 1, 1}})[2:]...)
	then := func(head ...[]byte) []byte {
		var b []byte
		for _, h := range head {
			b = append(b, h...)
		}
		return append(b, jpegHeader(0xc0, 12, 34, [3][3]byte{{1, 1, 1}, {2, 1, 1}, {3, 1, 1}})[2:]...)
	}

	for _, c := range []struct {
		name    string
		in      []byte
		wantErr bool
		w, h    int
	}{
		{"empty", nil, true, 0, 0},
		{"not soi", []byte{0x00, 0x00}, true, 0, 0},
		{"sos first", then([]byte{0xff, 0xd8, 0xff, 0xda, 0x00, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}), true, 0, 0},
		{"eoi first", then([]byte{0xff, 0xd8, 0xff, 0xd9}), true, 0, 0},
		{"height 0", zeroH, true, 0, 0},
		{"length 1", then([]byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x01}, make([]byte, 0xffff)), true, 0, 0},
		{"truncated frame", truncated, true, 0, 0},
		{"non marker", then([]byte{0xff, 0xd8, 0x00}), true, 0, 0},
		{"fill and rst0", fillAndRST, false, 12, 34},
	} {
		w, h, err := jpegSize(bytes.NewReader(c.in))
		if c.wantErr {
			if err == nil {
				t.Errorf("%s: err = nil", c.name)
			}
			continue
		}
		if err != nil || w != c.w || h != c.h {
			t.Errorf("%s: jpegSize = %d %d %v, want %d %d nil", c.name, w, h, err, c.w, c.h)
		}
	}
}

func TestReadDimsSizesTheProductionLogos(t *testing.T) {
	for _, c := range []struct {
		id         int
		sof        byte
		w, h       int
		components [3][3]byte
	}{
		{2332, 0xc0, 165, 75, [3][3]byte{{1, 2, 2}, {2, 1, 1}, {3, 1, 2}}},
		{6500, 0xc2, 400, 80, [3][3]byte{{1, 2, 2}, {2, 1, 1}, {3, 1, 2}}},
		{88634, 0xc0, 185, 68, [3][3]byte{{1, 2, 2}, {2, 1, 1}, {3, 2, 2}}},
		{93816, 0xc2, 330, 308, [3][3]byte{{1, 2, 2}, {2, 1, 1}, {3, 2, 2}}},
	} {
		path := filepath.Join(t.TempDir(), "logo.jpg")
		if err := os.WriteFile(path, jpegHeader(c.sof, c.w, c.h, c.components), 0o644); err != nil {
			t.Fatal(err)
		}
		w, h, format, err := readDims(path)
		if err != nil || w != c.w || h != c.h || format != "jpeg" {
			t.Errorf("person %d: readDims = %d %d %q %v, want %d %d jpeg nil", c.id, w, h, format, err, c.w, c.h)
		}
	}
}

func TestReadDimsKeepsTheDecoderError(t *testing.T) {
	dir := t.TempDir()
	notImage := filepath.Join(dir, "x")
	noFrame := filepath.Join(dir, "y.jpg")
	if err := os.WriteFile(notImage, []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(noFrame, []byte{0xff, 0xd8, 0xff, 0xd9}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := readDims(notImage); !errors.Is(err, image.ErrFormat) {
		t.Errorf("not an image: err = %v, want image.ErrFormat", err)
	}
	var formatErr jpeg.FormatError
	if _, _, _, err := readDims(noFrame); !errors.As(err, &formatErr) {
		t.Errorf("no frame header: err = %v, want the jpeg.FormatError", err)
	}
}

func TestAnOddlySampledLogoIsMirroredAndRecorded(t *testing.T) {
	data := jpegHeader(0xc2, 330, 308, [3][3]byte{{1, 2, 2}, {2, 1, 1}, {3, 3, 2}})
	f := newFake(t)
	f.add("persons/93816", &item{images: map[string]string{"large": "{srv}/pic/93816.jpg"}})
	f.add("pic/93816.jpg", &item{body: data})
	root := t.TempDir()

	st, err := Run(context.Background(), opts(f, KindPersons, root), []int{93816})
	if err != nil {
		t.Fatal(err)
	}
	if st.Downloaded != 1 || st.Errors != 0 {
		t.Fatalf("stats = %+v", st)
	}
	m := readManifest(t, root)
	if len(m) != 1 || m[0].ID != "93816" || m[0].File != "93816/logo.jpg" || m[0].W != 330 || m[0].H != 308 {
		t.Fatalf("manifest = %+v", m)
	}

	if err := os.WriteFile(filepath.Join(root, manifestName), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	st, err = Run(context.Background(), opts(f, KindPersons, root), []int{93816})
	if err != nil {
		t.Fatal(err)
	}
	if st.SkippedExist != 1 || st.Errors != 0 {
		t.Fatalf("rerun stats = %+v", st)
	}
	m = readManifest(t, root)
	if len(m) != 1 || m[0].ID != "93816" || m[0].File != "93816/logo.jpg" || m[0].W != 330 || m[0].H != 308 {
		t.Fatalf("backfill manifest = %+v", m)
	}
}

func TestAnOddlySampledCoverIsRecorded(t *testing.T) {
	data := jpegHeader(0xc0, 165, 75, [3][3]byte{{1, 2, 2}, {2, 1, 1}, {3, 1, 3}})
	f := newFake(t)
	f.add("subjects/2332", &item{images: map[string]string{"large": "{srv}/pic/2332.jpg"}})
	f.add("pic/2332.jpg", &item{body: data})
	root := t.TempDir()

	st, err := Run(context.Background(), opts(f, KindCovers, root), []int{2332})
	if err != nil {
		t.Fatal(err)
	}
	if st.Downloaded != 1 || st.Errors != 0 {
		t.Fatalf("stats = %+v", st)
	}
	m := readManifest(t, root)
	if len(m) != 1 || m[0].SubjectID != 2332 || m[0].File != "2332/cover.jpg" || m[0].W != 165 || m[0].H != 75 {
		t.Fatalf("manifest = %+v", m)
	}
}
