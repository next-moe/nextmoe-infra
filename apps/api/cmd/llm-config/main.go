// llm-config manages the ai gateway from the host, for changes that would
// otherwise need ren's admin session:
//
//	llm-config import [-apply] <config.json>   upsert providers/models/routes/scenes by name
//	llm-config set-key -provider <name> -from-env <VAR>   seal a provider key read from the environment
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"api/internal/infrastructure/database"
	"api/internal/platform/llm"
	"api/pkg/config"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	ctx := llm.WithActor(context.Background(), "llm-config")
	var err error
	switch os.Args[1] {
	case "import":
		err = runImport(ctx, os.Args[2:])
	case "set-key":
		err = runSetKey(ctx, os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		slog.Error("llm-config "+os.Args[1], "error", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: llm-config import [-apply] <config.json> | llm-config set-key -provider <name> -from-env <VAR>")
	os.Exit(2)
}

func admin() (*llm.Admin, error) {
	// KUN_LLM_KEK is stored under the ai scope only, with the gateway it seals.
	cfg, err := config.LoadService("ai")
	if err != nil {
		return nil, err
	}
	kek, err := llm.ParseKEK(cfg.LLM.KEK)
	if err != nil {
		return nil, err
	}
	db, err := database.NewPostgresDB(cfg.AIDatabase)
	if err != nil {
		return nil, err
	}
	return llm.NewAdmin(db.DB(), kek, nil), nil
}

func runImport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	apply := fs.Bool("apply", false, "write the changes; without it the plan is printed and rolled back")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		usage()
	}
	raw, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	var gc llm.GatewayConfig
	if err := json.Unmarshal(raw, &gc); err != nil {
		return fmt.Errorf("parse %s: %w", fs.Arg(0), err)
	}
	a, err := admin()
	if err != nil {
		return err
	}
	res, err := a.ImportConfig(ctx, gc, *apply)
	if err != nil {
		return err
	}
	out := json.NewEncoder(os.Stdout)
	out.SetIndent("", "  ")
	return out.Encode(res)
}

func runSetKey(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("set-key", flag.ExitOnError)
	name := fs.String("provider", "", "provider name")
	fromEnv := fs.String("from-env", "", "environment variable holding the key (never a flag value: argv is world-readable)")
	_ = fs.Parse(args)
	if *name == "" || *fromEnv == "" {
		usage()
	}
	key := strings.TrimSpace(os.Getenv(*fromEnv))
	if key == "" {
		return fmt.Errorf("%s is empty", *fromEnv)
	}
	a, err := admin()
	if err != nil {
		return err
	}
	if !a.KEKConfigured() {
		return fmt.Errorf("KUN_LLM_KEK is not set for the ai scope; the key cannot be sealed")
	}
	providers, err := a.Providers(ctx)
	if err != nil {
		return err
	}
	for _, p := range providers {
		if p.Name == *name {
			v, err := a.UpdateProvider(ctx, p.ID, llm.ProviderInput{APIKey: &key})
			if err != nil {
				return err
			}
			slog.Info("provider key sealed", "provider", v.Name, "id", v.ID)
			return nil
		}
	}
	return fmt.Errorf("no provider named %q", *name)
}
