package archtest

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var (
	cronEnvRef      = regexp.MustCompile(`\$\{?(KUN_[A-Z0-9_]+)(:?[-=?+])?`)
	cronEnvAssigned = regexp.MustCompile(`(?:^|[\s'"])(?:-e |export )?(KUN_[A-Z0-9_]+)=`)
)

// The root crons build DSNs from `docker inspect` of the catalog and oauth
// containers. #533 dropped KUN_PG_PORT/USER and the database names from
// compose because they equal the code defaults, and from the 2026-11-16 deploy
// every such cron failed silently ("invalid port").
func TestCronEnvironmentIsInCompose(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "docker-compose.prod.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]struct {
			Environment map[string]any `yaml:"environment"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(raw, &compose); err != nil {
		t.Fatal(err)
	}
	provided := map[string]bool{}
	for _, svc := range []string{"catalog", "oauth"} {
		env := compose.Services[svc].Environment
		if len(env) == 0 {
			t.Fatalf("compose service %s has no environment", svc)
		}
		for k := range env {
			provided[k] = true
		}
	}

	missing := map[string][]string{}
	err = filepath.WalkDir(filepath.Join(root, "scripts", "prod-cron"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".sh") || strings.HasSuffix(path, "test.sh") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		assigned := map[string]bool{}
		for _, m := range cronEnvAssigned.FindAllStringSubmatch(string(src), -1) {
			assigned[m[1]] = true
		}
		rel, _ := filepath.Rel(root, path)
		for _, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			for _, m := range cronEnvRef.FindAllStringSubmatch(line, -1) {
				name, defaulted := m[1], m[2] != ""
				if defaulted || assigned[name] || provided[name] {
					continue
				}
				missing[name] = append(missing[name], rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(missing))
	for n := range missing {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		t.Errorf("%s is read by %v but neither the catalog nor the oauth service sets it in docker-compose.prod.yml", n, uniq(missing[n]))
	}
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
