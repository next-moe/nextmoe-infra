package main

import (
	"context"
	"testing"
)

func TestReclassifyStickersTakesLoneOnesOnly(t *testing.T) {
	chat, main, kungal, moyu := seedLegacy(t)
	ctx := context.Background()
	if _, err := run(ctx, deps{Chat: chat, Main: main, Kungal: kungal, Moyu: moyu, Rehost: fakeRehost, Apply: true}); err != nil {
		t.Fatal(err)
	}
	official := map[string]bool{hashS: true}
	types := func() map[string]string {
		out := map[string]string{}
		var rows []struct{ Hash, Type string }
		chat.Raw(`SELECT media->>'image_hash' AS hash, media->>'type' AS type FROM chat_message WHERE media IS NOT NULL`).Scan(&rows)
		for _, r := range rows {
			out[r.Hash[:1]] = r.Type
		}
		return out
	}

	if n, err := reclassifyStickers(ctx, chat, official, true); err != nil || n != 0 {
		t.Fatalf("a sticker inside an album stays a photo: %d %v", n, err)
	}
	must(t, chat, `UPDATE chat_message SET media_group_id = NULL WHERE media->>'image_hash' = ?`, hashS)
	if n, err := reclassifyStickers(ctx, chat, official, false); err != nil || n != 1 || types()["5"] != "photo" {
		t.Fatalf("dry run: %d %v %v", n, err, types())
	}
	if n, err := reclassifyStickers(ctx, chat, official, true); err != nil || n != 1 {
		t.Fatalf("apply: %d %v", n, err)
	}
	if got := types(); got["5"] != "sticker" || got["c"] != "photo" {
		t.Fatalf("types: %v", got)
	}
}
