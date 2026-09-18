package problem

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var gatedReasons = map[string]bool{
	"ReasonTooLong":      true,
	"ReasonTooShort":     true,
	"ReasonTooManyItems": true,
	"ReasonTooFewItems":  true,
	"TOO_LONG":           true,
	"TOO_SHORT":          true,
	"TOO_MANY_ITEMS":     true,
	"TOO_FEW_ITEMS":      true,
}

func TestFieldErrorBoundReasonsCarryParams(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), ".."))
	violations, files, err := scanFieldErrorParams(root)
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Fatal("scan found zero files")
	}
	if len(violations) > 0 {
		t.Fatalf("FieldError literals missing Params:\n  %s", strings.Join(violations, "\n  "))
	}

	dir := t.TempDir()
	bad := `package x
import "api/internal/platform/apiv2/problem"
var _ = problem.FieldError{Reason: problem.ReasonTooLong, Detail: "x"}
`
	if err := os.WriteFile(filepath.Join(dir, "bad.go"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	control, n, err := scanFieldErrorParams(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("positive control scan found zero files")
	}
	if len(control) == 0 {
		t.Fatal("positive control: a TooLong literal without Params must fail the scan")
	}

	empty := t.TempDir()
	_, _, err = scanFieldErrorParams(empty)
	if err == nil {
		t.Fatal("scan of a tree with no .go files must fail")
	}
}

// OUT_OF_RANGE and UNKNOWN_VALUE are excluded: P4 allows those reasons
// without params when the bound is a date or a DB-backed vocabulary.
func scanFieldErrorParams(root string) (violations []string, files int, err error) {
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			base := info.Name()
			if base == "vendor" || base == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files++
		fset := token.NewFileSet()
		f, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		ast.Inspect(f, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			switch {
			case isFieldErrorType(cl.Type):
				if v := fieldErrorParamsViolation(fset, path, cl); v != "" {
					violations = append(violations, v)
				}
			case isFieldErrorSliceType(cl.Type):
				for _, elt := range cl.Elts {
					inner, ok := elt.(*ast.CompositeLit)
					if !ok || inner.Type != nil {
						continue
					}
					if v := fieldErrorParamsViolation(fset, path, inner); v != "" {
						violations = append(violations, v)
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		return violations, files, err
	}
	if files == 0 {
		return nil, 0, fmt.Errorf("scan found zero files under %s", root)
	}
	return violations, files, nil
}

func isFieldErrorType(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name == "FieldError"
	case *ast.SelectorExpr:
		return t.Sel.Name == "FieldError"
	}
	return false
}

func isFieldErrorSliceType(expr ast.Expr) bool {
	arr, ok := expr.(*ast.ArrayType)
	if !ok {
		return false
	}
	return isFieldErrorType(arr.Elt)
}

func fieldErrorParamsViolation(fset *token.FileSet, path string, cl *ast.CompositeLit) string {
	reason := ""
	hasParams := false
	for _, elt := range cl.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "Reason":
			reason = reasonName(kv.Value)
		case "Params":
			hasParams = true
		}
	}
	if !gatedReasons[reason] || hasParams {
		return ""
	}
	pos := fset.Position(cl.Pos())
	return fmt.Sprintf("%s:%d: FieldError %s without Params", path, pos.Line, reason)
}

func reasonName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.BasicLit:
		if t.Kind == token.STRING {
			return strings.Trim(t.Value, `"`)
		}
	}
	return ""
}
