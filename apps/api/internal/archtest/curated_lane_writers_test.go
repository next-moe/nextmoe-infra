package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// reviewedLaneWriters lists every raw UPDATE / DELETE that a job or command
// issues against a table holding a curated (editor) lane, with why it cannot
// rewrite an editor's row. The list is the contract: a new statement fails
// this test until someone checks it keeps out of source 12 and records why.
//
// The 2026-09 wiki-lineage cleanup deleted curated covers by source with no
// revision; 18 covers users had uploaded through the editor in August went
// with it, and every later edit's history showed the editor deleting them.
//
// Deliberately out of scope: GORM writes through a loaded struct
// (tx.Model(&row).Updates), which carry no SQL text to scan, and
// internal/platform, whose merge and editing paths write revisions of their own.
var reviewedLaneWriters = map[string]string{
	"internal/jobs/aicovers/aicovers.go DELETE FROM catalog_work_cover WHERE work_id = ? AND source_id = ?":                                                                                                                                                                          "bound to source ai_redraw",
	"internal/jobs/aicovers/aicovers.go UPDATE catalog_work_cover SET portrait_pinned = false, updated_at = now() WHERE work_id = ? AND portrait_pinned AND id <> ?":                                                                                                                 "runs only after the editor-pin check came back false",
	"internal/jobs/aicovers/aicovers.go UPDATE catalog_work_cover SET portrait_pinned = true, updated_at = now() WHERE id = ?":                                                                                                                                                       "pins the ai_redraw row it just inserted",
	`internal/jobs/historyrepair/stamp.go UPDATE catalog_work_cover SET field_provenance = jsonb_set(field_provenance, ARRAY[?::text], jsonb_build_array(jsonb_build_object( 'source', ?::text, 'at', to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')))) WHERE`: "the one-off repair stamps editor uploads in the curated lane; it changes provenance only",
	"internal/jobs/officialcovers/officialcovers.go UPDATE catalog_work_cover SET portrait_pinned = false, updated_at = now() WHERE work_id = ? AND portrait_pinned AND NOT (":                                                                                                       "excludes the curated lane in its WHERE",
	"internal/jobs/repincovers/write.go UPDATE catalog_work_cover SET portrait_pinned = false, updated_at = now() WHERE work_id = ? AND portrait_pinned AND id <> ?":                                                                                                                 "candidates exclude works with a curated or human pin",
	"internal/jobs/repincovers/write.go UPDATE catalog_work_cover SET portrait_pinned = true, updated_at = now() WHERE id = ? AND work_id = ?":                                                                                                                                       "candidates exclude works with a curated or human pin",
	"internal/jobs/repincovers/write.go DELETE FROM catalog_work_cover WHERE id IN ?":                                                                                                                                                                                                "ids come from source upscale only",
}

var laneTableWrite = regexp.MustCompile(`(?i)\b(?:update|delete\s+from)\s+(?:only\s+)?catalog_(?:work_cover|work_screenshot|work_label)\b`)

func TestMachineWritersOfCuratedLaneTablesAreReviewed(t *testing.T) {
	root := moduleRoot(t)
	found := map[string]string{}
	for _, rel := range []string{"cmd", "internal/jobs"} {
		if err := collectLaneWrites(filepath.Join(root, filepath.FromSlash(rel)), root, found); err != nil {
			t.Fatalf("scan %s: %v", rel, err)
		}
	}
	var unreviewed, stale []string
	for key, where := range found {
		if _, ok := reviewedLaneWriters[key]; !ok {
			unreviewed = append(unreviewed, where+"  key: "+key)
		}
	}
	for key := range reviewedLaneWriters {
		if _, ok := found[key]; !ok {
			stale = append(stale, key)
		}
	}
	slices.Sort(unreviewed)
	slices.Sort(stale)
	if len(unreviewed) > 0 {
		t.Errorf("a job writes a table with a curated lane; keep it out of source 12 and add it to reviewedLaneWriters:\n  %s",
			strings.Join(unreviewed, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("reviewedLaneWriters names statements that no longer exist:\n  %s", strings.Join(stale, "\n  "))
	}
}

func collectLaneWrites(path, root string, found map[string]string) error {
	fset := token.NewFileSet()
	space := regexp.MustCompile(`\s+`)
	return filepath.WalkDir(path, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(filename, ".go") || strings.HasSuffix(filename, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, filename, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", filename, err)
		}
		rel, err := filepath.Rel(root, filename)
		if err != nil {
			rel = filename
		}
		rel = filepath.ToSlash(rel)
		ast.Inspect(file, func(node ast.Node) bool {
			lit, ok := node.(*ast.BasicLit)
			if !ok {
				return true
			}
			value, ok := goStringLiteral(lit)
			if !ok {
				return true
			}
			loc := laneTableWrite.FindStringIndex(value)
			if loc == nil {
				return true
			}
			stmt := strings.TrimSpace(space.ReplaceAllString(value[loc[0]:], " "))
			found[rel+" "+stmt] = fmt.Sprintf("%s:%d", rel, fset.Position(lit.Pos()).Line)
			return true
		})
		return nil
	})
}
