package gate

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// reviewOrderLimit is how many entities the review order names before it
// summarises the rest. An agent-written change set is routinely dozens of
// entities; naming all of them reproduces the problem Gate exists to solve.
const reviewOrderLimit = 5

// ReviewOrder ranks changed entities by how much they need a human's eyes:
// dependents descending, ties broken by unchecked first.
//
// Deliberately not dependents multiplied by uncheckedness. Uncheckedness is
// binary, so the product zeroes every verified entity and would sort a
// 9-dependent verified change below a 1-dependent unchecked one. Dependents are
// the risk; coverage breaks ties.
func ReviewOrder(entities []ChangedEntity) []ChangedEntity {
	ordered := make([]ChangedEntity, len(entities))
	copy(ordered, entities)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.Dependents != b.Dependents {
			return a.Dependents > b.Dependents
		}
		if (a.Coverage == Unchecked) != (b.Coverage == Unchecked) {
			return a.Coverage == Unchecked
		}
		return a.Path < b.Path
	})
	return ordered
}

// WriteJSON emits the machine-readable report.
func WriteJSON(w io.Writer, report Report) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

// WriteText renders the human report: verdict first, then what to read, then
// the findings grouped by dimension, then the rule that produced the verdict.
func WriteText(w io.Writer, report Report, all bool) {
	fmt.Fprintf(w, "VERDICT  %s\n", report.Verdict)
	fmt.Fprintf(w, "         %s\n", Summarise(report.Entities))
	// The limits of the analysis are stated before any count, so a reader
	// cannot absorb a number without first learning how far to trust it.
	if note := EvidenceNote(report.Analysis); note != "" {
		fmt.Fprintf(w, "         %s\n", note)
	}
	if note := DegradationNote(report.Available); note != "" {
		fmt.Fprintf(w, "         %s\n", note)
	}
	fmt.Fprintf(w, "         %s..%s\n", short(report.Base), short(report.Head))
	if report.Checkpoint != "" {
		fmt.Fprintf(w, "         checkpoint %s\n", report.Checkpoint)
	}

	writeReviewOrder(w, report.Entities, all)
	writeFindings(w, report.Findings)

	writeAnalysis(w, report.Analysis)

	if report.VerifyCommand != "" {
		fmt.Fprintf(w, "\nVERIFY\n  %s\n", report.VerifyCommand)
	}
	writeWarnings(w, report.Warnings)

	fmt.Fprintf(w, "\n%s\n", Rule)
	fmt.Fprintf(w, "\nexit %d\n", report.Verdict.ExitCode())
}

// writeWarnings prints one line per warning, except that a code repeated across
// files is collapsed to a count. Five identical E_PARSE_ERROR lines about
// vendored grammar headers bury the one warning that is about the change under
// review.
func writeWarnings(w io.Writer, warnings []string) {
	seen := map[string]int{}
	var order []string
	for _, warning := range warnings {
		code, _, _ := strings.Cut(warning, " ")
		if seen[code] == 0 {
			order = append(order, warning)
		}
		seen[code]++
	}
	for _, warning := range order {
		code, _, _ := strings.Cut(warning, " ")
		fmt.Fprintf(w, "\nWARNING  %s", warning)
		if n := seen[code]; n > 1 {
			fmt.Fprintf(w, " (and %d more %s)", n-1, code)
		}
		fmt.Fprintln(w)
	}
}

func writeReviewOrder(w io.Writer, entities []ChangedEntity, all bool) {
	if len(entities) == 0 {
		return
	}
	ordered := ReviewOrder(entities)
	shown := ordered
	if !all && len(shown) > reviewOrderLimit {
		shown = shown[:reviewOrderLimit]
	}

	fmt.Fprintf(w, "\nREVIEW ORDER — %d entities changed, read these %d first\n\n",
		len(entities), len(shown))
	for i, e := range shown {
		fmt.Fprintf(w, " %d. %-24s @ %s:%d\n", i+1, e.Name, e.Path, e.Line)
		fmt.Fprintf(w, "    %s · %s · %s%s\n",
			e.ChangeType, dependentsPhrase(e), e.Coverage, coveringTestSuffix(e))
	}

	if rest := ordered[len(shown):]; len(rest) > 0 {
		// Saying why the remainder does not need eyes is what makes the cut
		// trustworthy: Gate is not hiding them, it is accounting for them.
		fmt.Fprintf(w, "\n The remaining %d entities: %s\n", len(rest), remainderReason(rest))
		fmt.Fprintln(w, " Full list: --all")
	}
}

func writeFindings(w io.Writer, findings []Finding) {
	byDimension := map[Dimension][]Finding{}
	for _, f := range findings {
		byDimension[f.Dimension] = append(byDimension[f.Dimension], f)
	}
	for _, dim := range []Dimension{DimRisk, DimCoverage, DimCompanions, DimClones} {
		group := byDimension[dim]
		if len(group) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s\n", sectionTitle(dim))
		for _, f := range group {
			// Confirmed findings carry no bracket. Marks mean doubt, and a
			// label on every line is a label nobody reads — the same reason
			// the confirmed tier has no sigil.
			label := ""
			if f.Tier != Confirmed {
				label = "   [" + tierLabel(f.Tier) + "]"
			}
			fmt.Fprintf(w, "  %s%s @ %s:%d%s\n",
				f.Tier.Marker(), f.Subject.Name, f.Subject.Path, f.Subject.Line, label)
			fmt.Fprintf(w, "    %s\n", stripTags(f.Summary))
			for _, e := range f.Evidence {
				fmt.Fprintf(w, "      - %s\n", stripTags(e))
			}
			// A claim Gate cannot stand behind ships with the way to settle it.
			// The tier no longer gates this: an absence claim needs a check
			// most of all, and coverage findings are confirmed observations
			// that nothing was found — which is exactly what a reader cannot
			// distinguish from a test the graph could not see. Risk findings
			// are unaffected, because Risk only attaches a hint when its own
			// evidence is short of confirmed.
			if f.Verify != "" {
				fmt.Fprintf(w, "      verify: %s\n", stripTags(f.Verify))
			}
		}
	}
}

func sectionTitle(dim Dimension) string {
	switch dim {
	case DimRisk:
		return "RISK — breaking changes with dependents"
	case DimCoverage:
		return "COVERAGE — changed and unchecked"
	case DimCompanions:
		return "COMPANION GAP — habitually changed together, not this time"
	case DimClones:
		return "CLONE DRIFT — near-duplicate siblings left behind"
	}
	return string(dim)
}

func coveringTestSuffix(e ChangedEntity) string {
	if len(e.CoveringTests) == 0 {
		return ""
	}
	return " (" + e.CoveringTests[0] + ")"
}

func remainderReason(rest []ChangedEntity) string {
	var unchecked int
	for _, e := range rest {
		if e.Coverage == Unchecked {
			unchecked++
		}
	}
	if unchecked == 0 {
		return "no dependents and no findings"
	}
	return fmt.Sprintf("%d still unchecked, none with dependents", unchecked)
}

func short(ref string) string {
	const shortSHALen = 12
	if len(ref) > shortSHALen {
		return ref[:shortSHALen]
	}
	return ref
}

// tierLabel is the words behind the marker, for the findings sections where
// there is room to be explicit rather than terse.
//
// Confirmed has its own arm and the zero value falls through to "untiered",
// which is the opposite of how this function was first written and the reason
// it is worth a comment. The original had no Confirmed case and let everything
// unmatched — including the empty string — return "confirmed", so a Finding
// whose Tier nobody set printed as proven. Every coverage finding did exactly
// that.
//
// A default that fails open is the same defect as a dependent count of zero
// that means "we could not look": in both, silence is rendered as assurance.
// So the default now fails closed, and the next caller who forgets to state a
// tier gets a visible "untiered" rather than a free pass.
func tierLabel(t EvidenceTier) string {
	switch t {
	case Confirmed:
		return "confirmed"
	case Heuristic:
		return "heuristic — inferred, verify before acting"
	case Unresolvable:
		return "unresolvable — the graph could not analyse this region"
	}
	return "untiered — no evidence tier was recorded; treat as unverified"
}

// dependentsPhrase renders a dependent count together with how far it can be
// trusted. An unresolvable count is deliberately not printed as a bare number:
// "0 dependents" and "could not resolve dependents" are different claims, and
// printing the first when the second is true is the failure the Track 2
// curveball named.
func dependentsPhrase(e ChangedEntity) string {
	return e.DependentsCounts.Describe()
}

// analysisPathLimit bounds each partial-analysis group the same way every
// other section is bounded; the counts above it stay exact.
const analysisPathLimit = 8

// NotAnalysedPrefix marks a partial-analysis reason as "the provider never
// looked" rather than "the provider looked and failed". The collect layer
// stamps it; writeAnalysis groups on it.
//
// The two are not equally alarming and must not share a list. A file that
// failed to parse is a hole in a language Gate otherwise resolves; a file in an
// inventory-only language was never going to yield relations, and saying
// "analysis could not complete" about it overstates what went wrong. Both are
// still disclosed — the curveball requires that analysis identify when it is
// partial — but a reader needs to see which is which to know where to look.
const NotAnalysedPrefix = "[[not-analysed]]"

// RuntimeDispatchPrefix marks a partial-analysis reason as "this file parsed
// perfectly and its callers still may not be here" — reflection, dynamic
// import, plugin loading.
//
// It is the third bucket because it is a third claim, and the most important
// one this tool makes. A parse failure says the graph could not read the code.
// An inventory-only file says the graph never tried. Runtime dispatch says the
// graph read everything, understood it, and the answer is still incomplete —
// which is the Track 2 curveball stated exactly. Printing it under "failed to
// parse" was both untrue and a waste of the strongest evidence Gate produces:
// on Django, 99 files were bucketed that way and none of them had failed.
//
// A tag rather than a typed field so that the Index keeps one channel for
// partial analysis and the collect and signal layers keep their current
// signatures; NotAnalysedPrefix established the pattern.
//
// Both sentinels are bracketed so they cannot collide with ordinary prose. The
// first spelling was the bare phrase "not analysed:", which also occurs in the
// SCOPE warning Gate writes for excluded files — so a grep for leaked tags
// matched a legitimate sentence and could not tell the two apart. A marker
// meant to be invisible should be impossible to type by accident.
const RuntimeDispatchPrefix = "[[runtime-dispatch]]"

// stripTags removes the grouping tags from anything about to be shown to a
// reader. They are machinery: they tell writeAnalysis which bucket a path
// belongs in and mean nothing to the person reading the report.
//
// It is applied centrally, at the point of printing, rather than at each site
// that builds a string. The tags are attached to a partial-analysis reason, and
// that reason is reused in three places — the bucketed listing, a finding's
// summary, and its verify hint — written across two files. Stripping at the
// three construction sites means the next reuse leaks, and the first one
// already did: a verify line read
//
//	rg -n '\bTestIssue14445\b' -- '*'   # runtime dispatch: this file imports pkgutil ...
//
// on pytest. Printing is the one place every path converges.
func stripTags(s string) string {
	for _, tag := range []string{RuntimeDispatchPrefix, NotAnalysedPrefix} {
		s = strings.ReplaceAll(s, tag+" ", "")
		s = strings.ReplaceAll(s, tag, "")
	}
	return s
}

// writeAnalysis prints what the graph could not see, and the legend that makes
// the markers elsewhere in the report readable. It is skipped entirely when the
// analysis is complete, so a fully resolved repository's output is unchanged.
func writeAnalysis(w io.Writer, a Analysis) {
	if a.Complete() {
		return
	}

	fmt.Fprintf(w, "\nEVIDENCE — what this report rests on\n")
	fmt.Fprintf(w, "  %d confirmed · %d heuristic (~) · %d unresolvable (?)\n",
		a.ConfirmedEntities, a.HeuristicEntities, a.UnresolvableEntities)
	fmt.Fprintln(w, "  confirmed    proven by the graph — safe to act on")
	fmt.Fprintln(w, "  ~ heuristic  inferred from a partial match — check the source")
	fmt.Fprintln(w, "  ? unresolvable  the graph could not look here — absence of a")
	fmt.Fprintln(w, "                  finding is NOT evidence of safety")

	if len(a.PartialPaths) == 0 {
		return
	}

	var runtime, failed, notAnalysed []string
	for i, path := range a.PartialPaths {
		reason := ""
		if i < len(a.Reasons) {
			reason = a.Reasons[i]
		}
		trimmed := strings.TrimSpace(stripTags(reason))
		line := path
		if trimmed != "" {
			line += " (" + trimmed + ")"
		}
		switch {
		case strings.HasPrefix(reason, RuntimeDispatchPrefix):
			runtime = append(runtime, line)
		case strings.HasPrefix(reason, NotAnalysedPrefix):
			notAnalysed = append(notAnalysed, line)
		default:
			failed = append(failed, line)
		}
	}

	fmt.Fprintf(w, "\n  where the graph could not see:\n")
	// Ordered by how much each says about the change under review. Runtime
	// dispatch leads: the code parsed, so this is the graph reporting the limit
	// of what static analysis can know rather than a gap in what it read.
	writePartialGroup(w, "parsed fine, but callers may be resolved at runtime", runtime)
	writePartialGroup(w, "failed to parse", failed)
	writePartialGroup(w, "never analysed for relations", notAnalysed)
}

// writePartialGroup prints one bounded, counted group of partial-analysis
// paths. The heading carries the exact total, so the cap shortens the listing
// without ever shortening the claim.
func writePartialGroup(w io.Writer, title string, lines []string) {
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(w, "    %s (%d):\n", title, len(lines))
	shown := lines
	if len(shown) > analysisPathLimit {
		shown = shown[:analysisPathLimit]
	}
	for _, line := range shown {
		fmt.Fprintf(w, "      - %s\n", line)
	}
	if rest := len(lines) - len(shown); rest > 0 {
		fmt.Fprintf(w, "      ... and %d more\n", rest)
	}
}
