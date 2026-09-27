package service

import (
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
)

type recordingImages struct {
	mu      sync.Mutex
	uploads []string
	pinged  [][]string
	fail    error
}

func (r *recordingImages) Meta(_ context.Context, hashes []string) (map[string]ImageMeta, error) {
	out := map[string]ImageMeta{}
	for _, h := range hashes {
		out[h] = ImageMeta{Width: 10, Height: 20}
	}
	return out, nil
}

func (r *recordingImages) Upload(_ context.Context, body io.Reader, filename, sub string) (*UploadedImage, error) {
	if r.fail != nil {
		return nil, r.fail
	}
	b, _ := io.ReadAll(body)
	r.mu.Lock()
	r.uploads = append(r.uploads, filename+"|"+sub+"|"+string(b))
	r.mu.Unlock()
	return &UploadedImage{Hash: strings.Repeat("a", 64), Width: 640, Height: 480, Thumbhash: "th"}, nil
}

func (r *recordingImages) Ping(_ context.Context, hashes []string) (int64, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pinged = append(r.pinged, append([]string(nil), hashes...))
	return int64(len(hashes)), 0, nil
}

func TestUploadImageNamesTheUploader(t *testing.T) {
	r := newRig(t, 1)
	imgs := &recordingImages{}
	r.svc.images = imgs
	res, err := r.svc.UploadImage(context.Background(), actor(1), "cat.png", strings.NewReader("bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Media.Type != "photo" || res.Media.Width != 640 || res.Media.URL != "https://img.example/aa/aa/"+strings.Repeat("a", 64)+".webp" {
		t.Fatalf("result: %+v", res)
	}
	if len(imgs.uploads) != 1 || imgs.uploads[0] != "cat.png|chat:1|bytes" {
		t.Fatalf("uploads: %v", imgs.uploads)
	}
	imgs.fail = ErrImageQuota
	_, err = r.svc.UploadImage(context.Background(), actor(1), "x.png", strings.NewReader("x"))
	if !errors.Is(err, ErrImageQuota) {
		t.Fatalf("quota: %v", err)
	}
	r.svc.images = nil
	_, err = r.svc.UploadImage(context.Background(), actor(1), "x.png", strings.NewReader("x"))
	wantErr(t, err, ErrImagesDisabled)
}

func TestUploadImageRateLimit(t *testing.T) {
	r := newRig(t, 1)
	r.svc.images = &recordingImages{}
	for i := 0; i < uploadsPerMinute; i++ {
		if _, err := r.svc.UploadImage(context.Background(), actor(1), "x", strings.NewReader("x")); err != nil {
			t.Fatal(err)
		}
	}
	_, err := r.svc.UploadImage(context.Background(), actor(1), "x", strings.NewReader("x"))
	var rl *RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("want RateLimitError, got %v", err)
	}
}

func TestPingImagesSendsLiveChatPhotosOnly(t *testing.T) {
	r := newRig(t, 1, 2)
	imgs := &recordingImages{}
	r.svc.images = imgs
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	send := func(hash string) int64 {
		res, err := r.svc.Send(ctx, actor(1), conv, SendInput{Media: &MediaInput{Type: "photo", ImageHash: hash}})
		if err != nil {
			t.Fatal(err)
		}
		return res.Message.Seq
	}
	a, b, c := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	send(a)
	send(a)
	gone := send(b)
	send(c)
	r.send(t, 1, conv, "text only")
	if _, err := r.svc.DeleteMessages(ctx, actor(1), conv, []int64{gone}, true); err != nil {
		t.Fatal(err)
	}
	updated, missing, err := r.svc.PingImages(ctx)
	if err != nil || updated != 2 || missing != 0 {
		t.Fatalf("ping: %d %d %v", updated, missing, err)
	}
	got := append([]string(nil), imgs.pinged[0]...)
	sort.Strings(got)
	if len(imgs.pinged) != 1 || len(got) != 2 || got[0] != a || got[1] != c {
		t.Fatalf("pinged %v: distinct live photos only", imgs.pinged)
	}
}
