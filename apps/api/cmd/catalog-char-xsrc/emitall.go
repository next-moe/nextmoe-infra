package main

import (
	"fmt"
	"io"

	"api/internal/jobs/personadj"
)

// runEmitAll writes one worklist from both rounds. The 2026-08-06 wave executed
// round one before building the panel, so the panel saw the merged ids; a run
// that proposes both rounds on the same night has no such gap, and two separate
// worklists would each claim a character that sits in an auto group and in a
// panel pair (A–B auto, B–C accepted by the panel).
func runEmitAll(pairsPath, verdictsPath, pairs2Path, verdicts2Path, worklistPath, residualPath string, out io.Writer) error {
	pairs, err := loadPairs(pairsPath)
	if err != nil {
		return err
	}
	verdicts, err := personadj.LoadVerdicts(verdictsPath)
	if err != nil {
		return err
	}
	ppairs, err := loadPanelPairs(pairs2Path)
	if err != nil {
		return err
	}
	verdicts2, err := personadj.LoadVerdicts(verdicts2Path)
	if err != nil {
		return err
	}

	byKey := make(map[string]personadj.Verdict, len(verdicts))
	for _, v := range verdicts {
		byKey[v.Key] = v
	}
	sourcesBySide := map[int64][]string{}
	richBySide := map[int64]richness{}
	var edges []mergeEdge
	var held []string
	judged := 0
	for _, p := range pairs {
		sourcesBySide[p.A], sourcesBySide[p.B] = p.ASources, p.BSources
		richBySide[p.A], richBySide[p.B] = p.ARich, p.BRich
		v, ok := byKey[fmt.Sprintf("xsrc:%d:%d", p.A, p.B)]
		if !ok {
			continue
		}
		judged++
		if p.Qualified {
			if v.Verdict == personadj.VerdictMerge {
				held = append(held, reviewLine(p, v, "限定名条目"))
			}
			continue
		}
		if v.Verdict == personadj.VerdictMerge && v.Confidence >= autoConfidence && !p.Instance {
			edges = append(edges, mergeEdge{panelPair{pairMeta: p, Cat: catAuto, R1Conf: v.Confidence}, v.Confidence})
		}
	}
	autoEdges := len(edges)

	stats := map[string]int{}
	accepted, residual := panelEdges(ppairs, verdicts2, false, stats)
	for _, e := range accepted {
		sourcesBySide[e.p.A], sourcesBySide[e.p.B] = e.p.ASources, e.p.BSources
		richBySide[e.p.A], richBySide[e.p.B] = e.p.ARich, e.p.BRich
	}
	edges = append(edges, accepted...)

	groups, conflicts, applied := unionEdges(edges, sourcesBySide, richBySide, stats)
	residual = append(append(residual, conflicts...), held...)
	if err := writeGroups(worklistPath, groups); err != nil {
		return err
	}
	if err := writeResidual(residualPath, residual); err != nil {
		return err
	}
	fmt.Fprintf(out, "pairs=%d judged=%d auto_edges=%d held_qualified=%d panel_pairs=%d panel_accepted=%d "+
		"closed_distinct=%d closed_instance=%d residual=%d panel_unjudged=%d edge_conflict=%d "+
		"edges_applied=%d groups_emitted=%d\n",
		len(pairs), judged, autoEdges, len(held), len(ppairs)-countCat(ppairs, catSameSource), len(accepted),
		stats["closed_distinct"], stats["closed_instance"], stats["residual"], stats["unjudged"],
		stats["edge_conflict"], applied, len(groups))
	return nil
}
