package gate

import (
	"fmt"
	"strings"
)

// Rule is printed with every run so the verdict is never a black box. A reader
// who disagrees with the decision can see the rule that produced it without
// reading this file.
const Rule = `RULE  revert   = a breaking change with >=1 dependent AND no covering test
      continue = a breaking change with dependents, OR any entity left unchecked,
                 OR a companion gap, OR a clone left undrifted
      keep     = none of the above
      unusable = no dimension produced evidence

DEGRADATION  a dimension that did not run cannot produce a finding against you.
             coverage unavailable  -> nothing may reach revert; cap at continue
             risk unavailable      -> cap at continue
             both unavailable      -> unusable (exit 5)

EVIDENCE     only confirmed structural evidence may raise a verdict.
             confirmed     proven by the graph            -> may reach revert
             heuristic ~   inferred, not proven           -> caps at continue
             unresolvable ?  the graph could not look     -> caps at continue,
                             and is always reported, never silently passed`

// Decide reduces the annotated entities and their findings to one verdict.
//
// The degradation branch is the reason this takes Availability. revert reads
// "has dependents AND no covering test". If coverage never ran, then no entity
// has a covering test, and every dependent-bearing change would satisfy the
// rule — a false accusation manufactured by a missing input rather than by
// anything wrong with the change. So an unavailable dimension caps the verdict
// instead of raising it.
func Decide(entities []ChangedEntity, findings []Finding, avail Availability) Verdict {
	if !avail.Risk && !avail.Coverage {
		return Unusable
	}

	if avail.Risk && avail.Coverage {
		for _, e := range entities {
			if e.ChangeType.Breaking() && e.Dependents > 0 && e.Coverage == Unchecked {
				// Only confirmed structural evidence may accuse. A dependent
				// count assembled from inferred edges is a reason to look, not
				// a reason to roll back, and revert is the one verdict a
				// reviewer cannot easily argue with. Inference caps at
				// continue, where the finding is still reported in full.
				if e.DependentsCounts.Proven() {
					return Revert
				}
			}
		}
	}

	if len(findings) > 0 {
		return Continue
	}
	for _, e := range entities {
		if e.Coverage == Unchecked {
			return Continue
		}
		// A clean-looking entity in a region the graph could not resolve is
		// not a pass. Its zero dependents and its coverage were both read off
		// a graph that could not see the calls into it, so the quiet result
		// is an artefact of the blind spot rather than evidence about the
		// change. Continue, so a human still looks.
		if e.DependentsTier == Unresolvable {
			return Continue
		}
	}
	return Keep
}

// DegradationNote explains a capped verdict in the report, so a reader is told
// that a dimension was missing rather than left to infer it from a suspiciously
// mild result.
func DegradationNote(avail Availability) string {
	switch {
	case !avail.Risk && !avail.Coverage:
		return "neither risk nor coverage produced evidence: verdict is unusable, not a pass"
	case !avail.Coverage:
		return "coverage did not run: no finding could reach revert, so this verdict is capped at continue"
	case !avail.Risk:
		return "risk did not run: dependent counts are absent, so this verdict is capped at continue"
	}
	return ""
}

// EvidenceNote states the report's own limits in one line, so a partial result
// is never read as an authoritative one. This is the curveball requirement
// made literal: the tool says what it could not see, in the header, before the
// reader reaches any number.
func EvidenceNote(a Analysis) string {
	if a.Complete() {
		return ""
	}
	var parts []string
	if a.UnresolvableEntities > 0 {
		parts = append(parts, fmt.Sprintf("%d in regions the graph could not resolve", a.UnresolvableEntities))
	}
	if a.HeuristicEntities > 0 {
		parts = append(parts, fmt.Sprintf("%d resting on inferred edges", a.HeuristicEntities))
	}
	if len(parts) == 0 {
		return "analysis is partial: this report is not authoritative"
	}
	return "PARTIAL ANALYSIS — " + strings.Join(parts, ", ") +
		". Counts below are floors, not totals; this report is not authoritative."
}

// Summarise counts entities by coverage state for the report header. Reporting
// unchecked as its own number, beside verified rather than folded into it, is
// the whole point: an empty selection is not evidence of safety.
func Summarise(entities []ChangedEntity) string {
	var verified, unchecked, noResolver int
	for _, e := range entities {
		switch e.Coverage {
		case Verified:
			verified++
		case Unchecked:
			unchecked++
		default:
			noResolver++
		}
	}
	return fmt.Sprintf("%d entities changed · %d verified · %d unchecked · %d no-resolver",
		len(entities), verified, unchecked, noResolver)
}
