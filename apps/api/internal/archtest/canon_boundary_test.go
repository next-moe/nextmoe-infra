package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// canonAllowedInternal is everything under api/internal that canon may import.
// The legacy catalog, its Fiber middleware and its GORM database package are
// deliberately absent: canon is on net/http, ent and database/sql (D23).
// authz is the standard-library-only permission type the console shares;
// markdown is the goldmark renderer v2 cooks short reviews with.
var canonAllowedInternal = []string{"/internal/canon", "/internal/testsupport", "/internal/platform/authz", "/internal/platform/markdown"}

var canonForbiddenModules = []string{"gorm.io/", "github.com/gofiber/"}

// canonImporters is every package outside canon allowed to import it:
// cmd/migrate runs its migrations.
var canonImporters = []string{"cmd/canon", "cmd/migrate", "internal/canon"}

// canonPermImporters may import canon's permission keys and nothing else of
// canon: the console registry lists them beside every other domain.
var canonPermImporters = []string{"internal/platform/permissions"}

func TestCanonStaysOffTheLegacyStack(t *testing.T) {
	root := moduleRoot(t)
	mod := modulePath(t, root)
	var violations []string
	for _, dir := range []string{"internal/canon", "cmd/canon"} {
		walkImports(t, filepath.Join(root, dir), func(file, imp string) {
			if strings.HasPrefix(imp, mod+"/internal/") && !hasAnyPrefix(strings.TrimPrefix(imp, mod), canonAllowedInternal) {
				violations = append(violations, rel(root, file)+" imports "+imp)
			}
			if hasAnyPrefix(imp, canonForbiddenModules) {
				violations = append(violations, rel(root, file)+" imports "+imp)
			}
		})
	}
	if len(violations) > 0 {
		t.Errorf("canon must not depend on the legacy stack:\n  %s", strings.Join(violations, "\n  "))
	}
}

func TestOnlyCanonAndItsMigrationImportCanon(t *testing.T) {
	root := moduleRoot(t)
	canon := modulePath(t, root) + "/internal/canon"
	var violations []string
	for _, top := range []string{"cmd", "internal", "pkg"} {
		walkImports(t, filepath.Join(root, top), func(file, imp string) {
			if imp != canon && !strings.HasPrefix(imp, canon+"/") {
				return
			}
			if imp == canon+"/perm" && hasAnyPrefix(rel(root, file), canonPermImporters) {
				return
			}
			if !hasAnyPrefix(rel(root, file), canonImporters) {
				violations = append(violations, rel(root, file)+" imports "+imp)
			}
		})
	}
	if len(violations) > 0 {
		t.Errorf("only %v may import canon:\n  %s", canonImporters, strings.Join(violations, "\n  "))
	}
}

func walkImports(t *testing.T, dir string, visit func(file, imp string)) {
	t.Helper()
	fset := token.NewFileSet()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range f.Imports {
			imp, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			visit(path, imp)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan %s: %v", dir, err)
	}
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func rel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(r)
}
