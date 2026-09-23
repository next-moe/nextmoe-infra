// Stock the moemoepoint shop from a directory of frames drawn by
// scripts/shop-frames/gen.py and the launch.json beside it: upload each
// frame's still PNG and animated WebP, create the item, publish it, and put
// it on sale at its price. An item whose name already exists is skipped, so a
// rerun only adds what is missing.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"api/internal/infrastructure/database"
	imgStorage "api/internal/platform/image/storage"
	ledgerService "api/internal/platform/ledger/service"
	"api/internal/platform/shop/model"
	shopService "api/internal/platform/shop/service"
	"api/pkg/config"
	"api/pkg/logger"
)

type entry struct {
	File        string `json:"file"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Price       int64  `json:"price"`
}

func main() {
	dir := flag.String("dir", "", "directory holding launch.json and <file>.png / <file>.webp (REQUIRED)")
	actor := flag.Uint("actor", 0, "user id recorded as the items' creator (REQUIRED)")
	run := flag.Bool("run", false, "write (default: dry-run)")
	flag.Parse()
	logger.Init("development")
	if *dir == "" || *actor == 0 {
		fmt.Fprintln(os.Stderr, "usage: shop-seed -dir <frames> -actor <uid> [-run]")
		os.Exit(2)
	}

	raw, err := os.ReadFile(filepath.Join(*dir, "launch.json"))
	if err != nil {
		fail("read launch.json", err)
	}
	var entries []entry
	if err := json.Unmarshal(raw, &entries); err != nil {
		fail("parse launch.json", err)
	}

	cfg, err := config.Load()
	if err != nil {
		fail("load config", err)
	}
	db, err := database.NewPostgresDB(cfg.Database)
	if err != nil {
		fail("connect", err)
	}
	store, err := imgStorage.NewClient(cfg.ImageS3)
	if err != nil {
		fail("object storage", err)
	}
	shop := shopService.New(db.DB(), ledgerService.New(db.DB()), store, cfg.ImageService.CDNBase)
	ctx := context.Background()

	existing, err := shop.ListItems(ctx, "")
	if err != nil {
		fail("list items", err)
	}
	have := map[string]bool{}
	for _, it := range existing {
		if it.Kind == model.KindAvatarFrame {
			have[it.Name] = true
		}
	}

	for _, e := range entries {
		if have[e.Name] {
			fmt.Printf("skip  %s: an avatar frame with this name exists\n", e.Name)
			continue
		}
		static, err := os.ReadFile(filepath.Join(*dir, e.File+".png"))
		if err != nil {
			fail(e.Name, err)
		}
		animated, err := os.ReadFile(filepath.Join(*dir, e.File+".webp"))
		if err != nil {
			fail(e.Name, err)
		}
		if !*run {
			fmt.Printf("would %s: %d + %d bytes, %d moemoepoints\n", e.Name, len(static), len(animated), e.Price)
			continue
		}
		if err := stock(ctx, shop, e, static, animated, *actor); err != nil {
			fail(e.Name, err)
		}
	}
	if !*run {
		fmt.Println("dry run: pass -run to write")
	}
}

func stock(ctx context.Context, shop *shopService.Shop, e entry, static, animated []byte, actor uint) error {
	s, err := shop.UploadAsset(ctx, static, actor)
	if err != nil {
		return err
	}
	a, err := shop.UploadAsset(ctx, animated, actor)
	if err != nil {
		return err
	}
	render, _ := json.Marshal(model.AvatarFrameRender{Static: s.Key, Animated: a.Key})
	item, err := shop.CreateItem(ctx, shopService.ItemInput{
		Kind: model.KindAvatarFrame, Name: e.Name, Description: e.Description, Render: render,
	}, actor)
	if err != nil {
		return err
	}
	if _, err := shop.TransitionItem(ctx, item.ID, shopService.ItemPublish); err != nil {
		return err
	}
	offer, err := shop.CreateOffer(ctx, shopService.OfferInput{
		Price: e.Price, Rewards: []model.Reward{{ItemID: item.ID}},
	}, actor)
	if err != nil {
		return err
	}
	if _, err := shop.TransitionOffer(ctx, offer.ID, shopService.OfferActivate); err != nil {
		return err
	}
	fmt.Printf("stocked %s: item %d, offer %d at %d, %s\n", e.Name, item.ID, offer.ID, e.Price, s.URL)
	return nil
}

func fail(what string, err error) {
	slog.Error(what, "err", err)
	os.Exit(1)
}
