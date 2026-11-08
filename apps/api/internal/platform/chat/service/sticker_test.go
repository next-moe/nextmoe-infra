package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"api/internal/platform/chat/content"
	"api/internal/platform/chat/dto"
)

type fakeStickers map[string]bool

func (f fakeStickers) IsOfficial(_ context.Context, hash string) (bool, error) { return f[hash], nil }

type downStickers struct{}

func (downStickers) IsOfficial(context.Context, string) (bool, error) {
	return false, errors.New("sticker face: status 503")
}

var (
	stickerHash = strings.Repeat("5", 64)
	photoHash   = strings.Repeat("6", 64)
)

func stickerRig(t *testing.T) *rig {
	r := newRig(t, 1, 2)
	r.svc.images = fakeImages{
		stickerHash: {Width: 512, Height: 480, Thumbhash: "st"},
		photoHash:   {Width: 800, Height: 600},
	}
	r.svc.stickers = fakeStickers{stickerHash: true}
	return r
}

func sticker() *MediaInput { return &MediaInput{Type: MediaSticker, ImageHash: stickerHash} }

func TestSendSticker(t *testing.T) {
	r := stickerRig(t)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	res, err := r.svc.Send(ctx, actor(1), conv, SendInput{Media: sticker()})
	if err != nil {
		t.Fatal(err)
	}
	m := res.Message.Media
	if m == nil || m.Type != MediaSticker || m.Width != 512 || m.Height != 480 || m.Thumbhash != "st" {
		t.Fatalf("media: %+v", m)
	}
	if want := "https://img.example/55/55/" + stickerHash + "_320.webp"; m.URL != want {
		t.Fatalf("url %q, want %q", m.URL, want)
	}

	reply, err := r.svc.Send(ctx, actor(2), conv, SendInput{Text: "nice", ReplyToSeq: &res.Message.Seq})
	if err != nil {
		t.Fatal(err)
	}
	if p := reply.Message.ReplyTo; p == nil || p.MediaType == nil || *p.MediaType != MediaSticker || p.Text != "" {
		t.Fatalf("reply_to: %+v", p)
	}
	back, err := r.svc.Send(ctx, actor(1), conv, SendInput{Media: sticker(), ReplyToSeq: &reply.Message.Seq})
	if err != nil {
		t.Fatalf("a sticker may reply: %v", err)
	}
	if back.Message.ReplyTo == nil || back.Message.ReplyTo.Text != "nice" {
		t.Fatalf("reply_to: %+v", back.Message.ReplyTo)
	}
}

func TestSendStickerRefuses(t *testing.T) {
	r := stickerRig(t)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	for name, in := range map[string]SendInput{
		"text":        {Text: "caption", Media: sticker()},
		"entities":    {Text: "b", Entities: []content.Entity{bold(0, 1)}, Media: sticker()},
		"album":       {Media: sticker(), MediaGroupID: ptr(int64(7))},
		"card":        {Media: sticker(), Context: &ContextInput{Kind: "topic", ID: "1", Title: "t", URL: "https://www.kungal.com/topic/1"}},
		"not a pack":  {Media: &MediaInput{Type: MediaSticker, ImageHash: photoHash}},
		"bad type":    {Media: &MediaInput{Type: "file", ImageHash: photoHash}},
		"bad hash":    {Media: &MediaInput{Type: MediaSticker, ImageHash: "ab"}},
		"no metadata": {Media: &MediaInput{Type: MediaSticker, ImageHash: strings.Repeat("7", 64)}},
	} {
		if name == "no metadata" {
			r.svc.stickers = fakeStickers{stickerHash: true, strings.Repeat("7", 64): true}
		}
		_, err := r.svc.Send(ctx, actor(1), conv, in)
		var ie *InvalidError
		if !errors.As(err, &ie) {
			t.Errorf("%s: want InvalidError, got %v", name, err)
		}
	}
	r.svc.stickers = nil
	_, err := r.svc.Send(ctx, actor(1), conv, SendInput{Media: sticker()})
	wantErr(t, err, ErrStickersDisabled)
	r.svc.stickers = downStickers{}
	_, err = r.svc.Send(ctx, actor(1), conv, SendInput{Media: sticker()})
	wantErr(t, err, ErrStickersDisabled)
}

func TestStickerCannotBeEdited(t *testing.T) {
	r := stickerRig(t)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	res, err := r.svc.Send(ctx, actor(1), conv, SendInput{Media: sticker()})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := dto.ParseID(res.Message.ID)
	_, err = r.svc.Edit(ctx, actor(1), id, "added later", nil)
	wantInvalid(t, err)
}

func TestStickerPassesAPendingRequest(t *testing.T) {
	r := stickerRig(t)
	ctx := context.Background()
	res, err := r.svc.EnsureDirect(ctx, actor(1), 2)
	if err != nil {
		t.Fatal(err)
	}
	conv, _ := dto.ParseID(res.Conversation.ID)
	_, err = r.svc.Send(ctx, actor(1), conv, SendInput{Media: &MediaInput{Type: MediaPhoto, ImageHash: photoHash}})
	wantErr(t, err, ErrRequestLimit)
	if _, err := r.svc.Send(ctx, actor(1), conv, SendInput{Media: sticker()}); err != nil {
		t.Fatalf("an official sticker is not uploaded media: %v", err)
	}
	for i := 1; i < pendingMessageLimit; i++ {
		r.send(t, 1, conv, "hello")
	}
	_, err = r.svc.Send(ctx, actor(1), conv, SendInput{Media: sticker()})
	wantErr(t, err, ErrRequestLimit)
}

func TestPingImagesSkipsStickers(t *testing.T) {
	r := stickerRig(t)
	imgs := &recordingImages{}
	r.svc.images = imgs
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	for _, in := range []*MediaInput{sticker(), {Type: MediaPhoto, ImageHash: photoHash}} {
		if _, err := r.svc.Send(ctx, actor(1), conv, SendInput{Media: in}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := r.svc.PingImages(ctx); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, batch := range imgs.pinged {
		got = append(got, batch...)
	}
	sort.Strings(got)
	if len(got) != 1 || got[0] != photoHash {
		t.Fatalf("pinged %v: stickers belong to the sticker site's client", got)
	}
}
