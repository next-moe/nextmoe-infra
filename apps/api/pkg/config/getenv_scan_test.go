package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The one-off tools under cmd/ read their own flags' defaults from the
// environment and never report a snapshot, so they are outside this rule;
// internal/testsupport reads the test harness's variables, not configuration.
var serviceRoots = []string{
	"../../internal",
	"../../cmd/oauth", "../../cmd/catalog", "../../cmd/community", "../../cmd/chat",
	"../../cmd/trust", "../../cmd/image", "../../cmd/artifact", "../../cmd/ai",
	"../../cmd/agent", "../../cmd/telemetry", "../../cmd/telemetry-worker", "../../cmd/migrate",
}

var directRead = regexp.MustCompile(`os\.(Getenv|LookupEnv)\(`)

// A variable a service reads around config never reaches the console, which
// is the gap this registry exists to close.
func TestServicesReadEnvironmentThroughConfig(t *testing.T) {
	for _, root := range serviceRoots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && strings.Contains(path, "testsupport") {
				return filepath.SkipDir
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if loc := directRead.FindIndex(src); loc != nil {
				line := 1 + strings.Count(string(src[:loc[0]]), "\n")
				t.Errorf("%s:%d reads the environment directly; use config.Env so the console sees it", path, line)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
