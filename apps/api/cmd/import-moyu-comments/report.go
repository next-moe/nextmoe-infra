package main

import (
	"fmt"
	"io"
)

type Report struct {
	SourceComments int
	Walls          int
	GameWalls      int
	ResourceWalls  int

	ThreadsCreated  int
	ThreadsExisting int
	PostsInserted   int
	PostsExisting   int
	LedgerRows      int

	SourceLikes   int
	LikesToInsert int
	LikesInserted int
	LikesExisting int
	LikesOrphaned int

	TrustSeeded  int
	TrustPresent int

	DanglingParents int
	HiddenRows      int
	OverLenRows     int
	UnparsableEdits int
}

func (r *Report) print(w io.Writer, apply bool) {
	mode := "DRY RUN (no writes)"
	if apply {
		mode = "APPLY"
	}
	fmt.Fprintf(w, "\n================ import-moyu-comments · %s ================\n", mode)
	fmt.Fprintf(w, "  source comments                       : %d\n", r.SourceComments)
	fmt.Fprintf(w, "  walls (game / resource)               : %d (%d / %d)\n", r.Walls, r.GameWalls, r.ResourceWalls)
	fmt.Fprintf(w, "  threads created / already present     : %d / %d\n", r.ThreadsCreated, r.ThreadsExisting)
	fmt.Fprintf(w, "  posts inserted / already present      : %d / %d\n", r.PostsInserted, r.PostsExisting)
	fmt.Fprintf(w, "  ledger rows written                   : %d\n", r.LedgerRows)
	fmt.Fprintf(w, "  likes: source %d → to insert %d, inserted %d, present %d, orphaned %d\n",
		r.SourceLikes, r.LikesToInsert, r.LikesInserted, r.LikesExisting, r.LikesOrphaned)
	fmt.Fprintf(w, "  trust rows seeded / already present    : %d / %d\n", r.TrustSeeded, r.TrustPresent)
	fmt.Fprintf(w, "  anomalies: dangling-parent=%d hidden=%d over-%drunes=%d unparsable-edit=%d\n",
		r.DanglingParents, r.HiddenRows, maxRunes, r.OverLenRows, r.UnparsableEdits)
	fmt.Fprintf(w, "==========================================================\n")
}
