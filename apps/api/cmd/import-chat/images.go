package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"api/internal/platform/chat/dto"
	"api/pkg/imageclient"
)

type collectingImages struct{ refs map[imageRef]bool }

func (c *collectingImages) resolve(ref imageRef) (dto.Media, bool) {
	c.refs[ref] = true
	return dto.Media{Type: "photo", ImageHash: ref.Hash}, true
}

type resolvedImages map[imageRef]dto.Media

func (r resolvedImages) resolve(ref imageRef) (dto.Media, bool) {
	m, ok := r[ref]
	return m, ok
}

// rehost gives every old image the hash chat will reference. Stickers keep
// theirs; an upload is fetched from the image service and uploaded again
// through chat's own client, which dedups by content and records chat as a
// user of the image, so chat's daily reference ping keeps it alive after the
// old site stops pinging.
func rehost(ctx context.Context, cli *imageclient.Client, baseURL string, refs map[imageRef]bool) (resolvedImages, int) {
	out := resolvedImages{}
	failed := 0
	var stickers []string
	for ref := range refs {
		if ref.Sticker {
			stickers = append(stickers, ref.Hash)
		}
	}
	for start := 0; start < len(stickers); start += 1000 {
		end := min(start+1000, len(stickers))
		metas, err := cli.MetaBatch(ctx, stickers[start:end])
		if err != nil {
			slog.Error("sticker meta", "err", err)
			continue
		}
		for h, m := range metas {
			out[imageRef{Hash: h, Sticker: true}] = dto.Media{Type: "photo", ImageHash: h, Width: m.Width, Height: m.Height, Thumbhash: m.Thumbhash}
		}
	}
	httpc := &http.Client{Timeout: 60 * time.Second}
	base := strings.TrimRight(baseURL, "/")
	for ref := range refs {
		if ref.Sticker {
			continue
		}
		res, err := reupload(ctx, httpc, cli, base, ref.Hash)
		if err != nil {
			failed++
			slog.Warn("re-host image", "hash", ref.Hash, "err", err)
			continue
		}
		out[ref] = dto.Media{Type: "photo", ImageHash: res.Hash, Width: res.Width, Height: res.Height, Thumbhash: res.Thumbhash}
	}
	return out, failed
}

func reupload(ctx context.Context, httpc *http.Client, cli *imageclient.Client, base, hash string) (*imageclient.UploadResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/image/"+hash, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	return cli.UploadWithSub(ctx, bytes.NewReader(body), hash+".webp", "message", "chat:import")
}
