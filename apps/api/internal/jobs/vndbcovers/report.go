package vndbcovers

import (
	"fmt"
	"os"
	"text/tabwriter"
)

func printForecast(plan []planRow, opts Opts) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	mode := "DRY RUN — no downloads, no uploads, no writes"
	if opts.Apply {
		mode = "APPLY"
	}
	fmt.Fprintf(w, "backfill-vndb-covers forecast (%s)\n", mode)
	fmt.Fprintln(w, "work\tanchor\timage\tshape\tsexual\tviolence\taction")

	capped := 0
	for _, row := range plan {
		if !row.actionable() {
			fmt.Fprintf(w, "%d\t%s\t%s\t-\t-\t-\tskip\n", row.WorkID, row.VNDBID, row.Reason)
			continue
		}
		action := "would-upload"
		if opts.Apply {
			capped++
			if opts.Limit > 0 && capped > opts.Limit {
				action = "over-limit"
			} else {
				action = "upload"
			}
		}
		pin := shapeLabel(row.Img.Dims)
		if portrait(row.Img.Dims) {
			pin += " (pinned)"
		}
		fmt.Fprintf(w, "%d\t%s\tfound\t%s\t%d\t%d\t%s\n", row.WorkID, row.VNDBID, pin,
			RatingLevel(row.Img.Sexual), RatingLevel(row.Img.Violence), action)
	}
	w.Flush()
}

func printTotals(s *Stats) {
	fmt.Printf("totals: candidates=%d with_image=%d no_image=%d portrait=%d landscape=%d uploaded=%d dedup=%d rejected=%d errors=%d\n",
		s.Candidates, s.Planned, s.NoImage, s.Portrait, s.Landscape, s.Uploaded, s.Dedup, s.Rejected, s.Errors)
}
