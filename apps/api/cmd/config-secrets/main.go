package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"

	"api/internal/infrastructure/database"
	"api/internal/platform/envconfig"
	"api/pkg/config"
	"api/pkg/secretbox"
)

var goServices = map[string]bool{
	"oauth": true, "image": true, "artifact": true, "catalog": true, "community": true, "chat": true,
	"trust": true, "ai": true, "agent": true, "telemetry": true, "telemetry-worker": true, "canon": true,
}

type container struct {
	Config struct {
		Env    []string          `json:"Env"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
}

type row struct {
	scope, key, value string
}

func main() {
	if len(os.Args) < 2 || os.Args[1] != "import" {
		fmt.Fprintln(os.Stderr, "usage: docker inspect <containers> | config-secrets import [-apply] [-global=false] [-keys K1,K2]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	apply := fs.Bool("apply", false, "write the rows; without it only the plan is printed")
	global := fs.Bool("global", true, "store a value every container shares once for every service; turn off when importing a project that does not share the infra environment")
	extra := fs.String("keys", "", "comma-separated non-secret variables to import as well (secrets are always imported)")
	note := fs.String("note", "imported from the running deployment", "audit note")
	_ = fs.Parse(os.Args[2:])

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	kek := config.StoreKey()
	if kek == nil {
		slog.Error(config.KEKEnv + " is not set")
		os.Exit(1)
	}

	var containers []container
	if err := json.NewDecoder(os.Stdin).Decode(&containers); err != nil {
		slog.Error("read docker inspect JSON from stdin", "error", err)
		os.Exit(1)
	}
	rows := plan(containers, *global, wanted(*extra))

	db, err := database.NewPostgresDB(cfg.Database)
	if err != nil {
		slog.Error("connect", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	store := envconfig.NewStore(db.DB())
	existing, err := store.Secrets(context.Background())
	if err != nil {
		slog.Error("read config_secrets", "error", err)
		os.Exit(1)
	}
	have := map[string]string{}
	for _, e := range existing {
		have[e.Scope+"\x00"+e.Key] = e.Fingerprint
	}

	written := 0
	for _, r := range rows {
		fp := secretbox.Fingerprint(kek, r.value)
		scope := r.scope
		if scope == "" {
			scope = "(all)"
		}
		action := "write"
		if old, ok := have[r.scope+"\x00"+r.key]; ok {
			action = "replace"
			if old == fp {
				action = "same"
			}
		}
		fmt.Printf("%-8s %-40s %-18s %s\n", action, r.key, scope, fp)
		if !*apply || action == "same" {
			continue
		}
		if _, err := store.Put(context.Background(), kek, envconfig.PutInput{
			Action: envconfig.ActionImport, Scope: r.scope, Key: r.key, Value: r.value, Note: *note,
		}); err != nil {
			slog.Error("write", "key", r.key, "scope", r.scope, "error", err)
			os.Exit(1)
		}
		written++
	}
	if !*apply {
		fmt.Println("dry run — nothing written; rerun with -apply")
		return
	}
	fmt.Printf("%d rows written\n", written)
}

func wanted(extra string) func(string) bool {
	set := map[string]bool{}
	for _, k := range strings.Split(extra, ",") {
		if k = strings.TrimSpace(k); k != "" {
			set[k] = true
		}
	}
	return func(key string) bool {
		return config.Storable(key) && (config.IsSecret(key) || set[key])
	}
}

// plan reproduces the current deployment exactly: a value goes in once for
// every service only when every container already holds that same value, and
// otherwise once per container that holds one. A shared row for a value only
// some containers had would hand it to the rest — chat's empty trust client
// would start forwarding to trust.
func plan(containers []container, global bool, want func(string) bool) []row {
	values := map[string]map[string]string{}
	services := 0
	for _, c := range containers {
		svc := c.Config.Labels["com.docker.compose.service"]
		if !goServices[svc] {
			continue
		}
		services++
		for _, kv := range c.Config.Env {
			k, v, _ := strings.Cut(kv, "=")
			if v == "" || !want(k) {
				continue
			}
			if values[k] == nil {
				values[k] = map[string]string{}
			}
			values[k][svc] = v
		}
	}
	var out []row
	for k, bySvc := range values {
		if global && len(bySvc) == services && allSame(bySvc) {
			for _, v := range bySvc {
				out = append(out, row{"", k, v})
				break
			}
			continue
		}
		for svc, v := range bySvc {
			out = append(out, row{svc, k, v})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].key != out[j].key {
			return out[i].key < out[j].key
		}
		return out[i].scope < out[j].scope
	})
	return out
}

func allSame(m map[string]string) bool {
	first, set := "", false
	for _, v := range m {
		if !set {
			first, set = v, true
		} else if v != first {
			return false
		}
	}
	return true
}
