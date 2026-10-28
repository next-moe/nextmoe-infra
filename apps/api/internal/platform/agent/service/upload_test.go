package service

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"api/internal/platform/agent/model"
)

var testPolicy = UploadPolicy{MaxFiles: 4, MaxChars: 50, Kinds: []UploadKind{
	{model.UploadImage, 1 << 20, []string{"image/png", "image/jpeg"}},
	{model.UploadText, 1 << 10, []string{".txt", ".md"}},
	{model.UploadDocument, 1 << 20, []string{".pdf", ".docx"}},
}}

func TestPrepareUploadScalesPicturesForVision(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3000, 1000))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	u, err := PrepareUpload(testPolicy, "wide.png", "image/png", buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := image.Decode(bytes.NewReader(u.Data))
	if err != nil || u.Kind != model.UploadImage || u.MediaType != "image/jpeg" || got.Bounds().Dx() != visionEdge || got.Bounds().Dy() != 522 {
		t.Fatalf("an opaque picture is fitted to %dpx and sent as JPEG: kind=%s type=%s err=%v", visionEdge, u.Kind, u.MediaType, err)
	}

	clear := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	clear.Set(1, 1, color.NRGBA{A: 0x80})
	buf.Reset()
	_ = png.Encode(&buf, clear)
	if u, _ := PrepareUpload(testPolicy, "alpha.png", "image/png", buf.Bytes()); u == nil || u.MediaType != "image/png" {
		t.Fatal("a picture with transparency stays PNG")
	}
}

func TestPrepareUploadReadsTextAndRefusesOtherFiles(t *testing.T) {
	u, err := PrepareUpload(testPolicy, "notes.MD", "application/octet-stream", []byte("\xef\xbb\xbf"+strings.Repeat("字", 60)))
	if err != nil || u.Kind != model.UploadText || len([]rune(u.Text)) != 50 || !u.Truncated {
		t.Fatalf("text is read by extension, the BOM dropped and the length capped: %+v err=%v", u, err)
	}
	if _, err := PrepareUpload(testPolicy, "gbk.txt", "text/plain", []byte{0xc4, 0xe3}); !errors.Is(err, ErrUploadUnread) {
		t.Fatalf("non-UTF-8 text: err = %v", err)
	}
	if _, err := PrepareUpload(testPolicy, "a.exe", "application/x-msdownload", []byte("MZ")); !errors.Is(err, ErrUploadType) {
		t.Fatalf("unknown kind: err = %v", err)
	}
	if _, err := PrepareUpload(testPolicy, "big.txt", "text/plain", bytes.Repeat([]byte("a"), 2<<10)); !errors.Is(err, ErrUploadTooLarge) {
		t.Fatalf("over the size limit: err = %v", err)
	}
	noImages := UploadPolicy{MaxFiles: 4, MaxChars: 50, Kinds: testPolicy.Kinds[1:]}
	if _, err := PrepareUpload(noImages, "a.png", "image/png", []byte("x")); !errors.Is(err, ErrUploadType) {
		t.Fatalf("pictures are refused without a vision scene: err = %v", err)
	}
}

func TestPrepareUploadExtractsDocuments(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("word/document.xml")
	_, _ = w.Write([]byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
		`<w:p><w:r><w:t>第一段</w:t></w:r></w:p><w:p><w:r><w:t>second</w:t><w:tab/><w:t>cell</w:t></w:r></w:p></w:body></w:document>`))
	_ = zw.Close()
	u, err := PrepareUpload(testPolicy, "report.docx", "", buf.Bytes())
	if err != nil || u.Kind != model.UploadDocument || u.Text != "第一段\nsecond\tcell" {
		t.Fatalf("docx text: %q err=%v", u.Text, err)
	}

	u, err = PrepareUpload(testPolicy, "hello.pdf", "application/pdf", minimalPDF("Hello NextMoe"))
	if err != nil || !strings.Contains(u.Text, "Hello NextMoe") || u.MediaType != "application/pdf" {
		t.Fatalf("pdf text: %+v err=%v", u, err)
	}
	if _, err := PrepareUpload(testPolicy, "broken.pdf", "application/pdf", []byte("%PDF-1.4 nonsense")); !errors.Is(err, ErrUploadUnread) {
		t.Fatalf("a broken PDF is unreadable, not a crash: err = %v", err)
	}
}

func minimalPDF(text string) []byte {
	stream := fmt.Sprintf("BT /F1 12 Tf 72 712 Td (%s) Tj ET", text)
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}
