package gate

import (
	"fmt"
	"sort"
)

// MaxHops bounds the blast-radius walk. Two is the useful ceiling: one hop
// answers "who calls this", two answers "who is exposed if the caller does not
// absorb the change", and three starts returning most of the repository for any
// symbol a utility function touches.
const MaxHops = 2

// riskFindingLimit caps how many risk findings are reported. The rest are still
// counted in the entity list and the review order; this only bounds the section.
const riskFindingLimit = 10

// Risk annotates each entity with its graph dependent count and reports the
// breaking changes that have dependents.
//
// Only breaking changes open a finding. A body change with a hundred callers is
// not a risk finding: its signature still holds, so the callers still compile
// and still mean what they meant. Reporting it would bury the changes that can
// actually break a caller.
//
// Entities are annotated in place; the returned findings are the reportable
// subset, most dependents first.
func Risk(entities []ChangedEntity, ix *Index, hops int) []Finding {
	if hops < 1 {
		hops = 1
	}
	if hops > MaxHops {
		hops = MaxHops
	}

	var findings []Finding
	// absent collects removals and renames, which cannot be looked up in a
	// snapshot of the head commit. They are reported once, together.
	var absent []ChangedEntity
	for i := range entities {
		entity := &entities[i]
		ids := ix.Resolve(entity.Name, entity.Path)
		if len(ids) == 0 {
			// The diff named an entity the graph has no symbol for. Gate used
			// to skip these in silence, leaving them in the report with a
			// confident "0 dependents". They are now tiered Unresolvable and
			// carry a verification path instead.
			entity.DependentsTier = Unresolvable
			// The counts must say unresolvable too, not just the tier: the
			// renderer reads the composition, and an empty TierCounts renders
			// as a confident "0 dependents" — the exact claim this branch
			// exists to refuse.
			entity.DependentsCounts = TierCounts{Region: Unresolvable}
			entity.VerifyHint = verifyHint(ix, entity)

			// A removed or renamed entity is absent from the head snapshot by
			// definition — that is what removal means — so "not in the graph"
			// here restates the change type rather than discovering anything.
			// Emitting one finding per removal spends the section's whole
			// budget on a tautology: measured on pytest-dev/pytest, 10 of 10
			// risk findings were removals, 9 of them deleted test methods from
			// a single class, and nothing that might actually break could
			// appear. They are aggregated into one honest line below instead.
			//
			// Everything else about them is unchanged: still tiered
			// Unresolvable, still counted in Analysis, still forcing continue
			// through Decide's unresolvable arm, still ranked in the review
			// order. Only the per-entity finding goes.
			if entity.ChangeType == Removed || entity.ChangeType == Renamed {
				absent = append(absent, *entity)
				continue
			}

			findings = append(findings, Finding{
				Dimension: DimRisk,
				Subject:   entity.Anchor,
				Tier:      Unresolvable,
				Summary: fmt.Sprintf("%s %s is not in the graph: no dependent analysis was possible here",
					entity.Kind, entity.ChangeType),
				Verify: entity.VerifyHint,
			})
			continue
		}
		if entity.SymbolID == "" {
			entity.SymbolID = ids[0]
		}

		dependents, counts := ix.DependentsWithTier(ids, hops)
		entity.Dependents = len(dependents)
		entity.DependentsTier = counts.Tier()
		entity.DependentsProven = counts.ConfirmedCount
		entity.DependentsCounts = counts
		if counts.Tier() != Confirmed {
			entity.VerifyHint = verifyHint(ix, entity)
		}

		// A change in a region the graph could not resolve gets a finding even
		// with zero dependents — especially with zero dependents. That is the
		// under-claim the Track 2 curveball is about: no edges here does not
		// mean no callers, it means no visibility, and staying silent would
		// present a blind spot as a clean bill of health.
		if counts.Region == Unresolvable {
			// An externally-registered entry point explains itself far better
			// than a generic "could not resolve": the reader needs to know the
			// callers are HTTP requests, not that the parser struggled.
			reason, external := ix.EntryPointReason(ids)
			if external {
				reason += ": its callers are external invocations, not call sites the graph can see"
			} else if partial, ok := ix.PartialReason(entity.Path); ok {
				reason = partial
			} else {
				reason = "the graph holds no resolvable relations for this symbol"
			}
			findings = append(findings, Finding{
				Dimension: DimRisk,
				Subject:   entity.Anchor,
				Tier:      Unresolvable,
				Summary: fmt.Sprintf("%s %s: dependents could not be resolved (%s) — the %d shown is a floor, not a count",
					entity.Kind, entity.ChangeType, reason, len(dependents)),
				Evidence: dependentEvidence(dependents),
				Verify:   entity.VerifyHint,
			})
			continue
		}

		if !entity.ChangeType.Breaking() || len(dependents) == 0 {
			continue
		}

		summary := fmt.Sprintf("%s %s has %d dependent(s) within %d hop(s)",
			entity.Kind, entity.ChangeType, len(dependents), hops)
		if counts.Tier() == Heuristic {
			summary += fmt.Sprintf(" — %d proven, %d inferred",
				counts.ConfirmedCount, counts.HeuristicCount+counts.UnresolvableCount)
		}
		findings = append(findings, Finding{
			Dimension: DimRisk,
			Subject:   entity.Anchor,
			Tier:      counts.Tier(),
			Summary:   summary,
			Evidence:  dependentEvidence(dependents),
			Verify:    entity.VerifyHint,
		})
	}

	if len(absent) > 0 {
		findings = append(findings, absentFinding(absent))
	}

	sort.Slice(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if len(a.Evidence) != len(b.Evidence) {
			return len(a.Evidence) > len(b.Evidence)
		}
		if a.Subject.Path != b.Subject.Path {
			return a.Subject.Path < b.Subject.Path
		}
		return a.Subject.Name < b.Subject.Name
	})
	if len(findings) > riskFindingLimit {
		findings = findings[:riskFindingLimit]
	}
	return findings
}

// evidencePerFinding bounds how many dependents are named under one finding.
// The count in the summary stays exact; this only limits the listing.
const evidencePerFinding = 5

func dependentEvidence(dependents []Symbol) []string {
	shown := dependents
	if len(shown) > evidencePerFinding {
		shown = shown[:evidencePerFinding]
	}
	evidence := make([]string, 0, len(shown)+1)
	for _, d := range shown {
		if d.Path == "" {
			evidence = append(evidence, d.Name+" (unresolved)")
			continue
		}
		evidence = append(evidence, fmt.Sprintf("%s @ %s:%d", d.Name, d.Path, d.Line))
	}
	if rest := len(dependents) - len(shown); rest > 0 {
		evidence = append(evidence, fmt.Sprintf("... and %d more", rest))
	}
	return evidence
}

// verifyHint is the fallback path: when Gate cannot prove a relationship, it
// says how a human or an agent can settle it directly from source. A claim the
// tool cannot stand behind must at minimum come with the way to check it.
func verifyHint(ix *Index, entity *ChangedEntity) string {
	// An entry point is verified by finding its registrations, not its callers.
	if _, external := ix.EntryPointReason(ix.Resolve(entity.Name, entity.Path)); external {
		return fmt.Sprintf("rg -n -B3 'def %s|func %s' -- '*'   # check the decorators/registrations above it, and every caller of the route",
			entity.Name, entity.Name)
	}
	if reason, partial := ix.PartialReason(entity.Path); partial && reason != "" {
		return fmt.Sprintf("rg -n '\\b%s\\b' -- %s   # %s", entity.Name, "'*'", reason)
	}
	return fmt.Sprintf("entire graph neighbors --repo . --symbol %s --file %s --relation CALLS --direction in",
		entity.Name, entity.Path)
}

// absentFinding states, once, what Gate cannot tell you about removals and
// renames — and states the real limitation rather than the definition of the
// word "removed".
//
// Gate builds one snapshot, of the head commit. A symbol deleted in the change
// under review is not in it, so its callers cannot be counted at all: not
// "zero callers", but "no way to ask". That is worth saying, and worth saying
// exactly once no matter how many entities a refactor deleted.
func absentFinding(absent []ChangedEntity) Finding {
	// Lead with source removals: a deleted test is usually intentional, while
	// a deleted source symbol is the one that might strand a caller.
	sort.SliceStable(absent, func(i, j int) bool {
		iTest, jTest := IsTestPath(absent[i].Path), IsTestPath(absent[j].Path)
		if iTest != jTest {
			return !iTest
		}
		if absent[i].Path != absent[j].Path {
			return absent[i].Path < absent[j].Path
		}
		return absent[i].Name < absent[j].Name
	})

	var sourceCount int
	for _, e := range absent {
		if !IsTestPath(e.Path) {
			sourceCount++
		}
	}

	evidence := make([]string, 0, evidencePerFinding+1)
	for _, e := range absent {
		if len(evidence) == evidencePerFinding {
			break
		}
		label := fmt.Sprintf("%s @ %s:%d", e.Name, e.Path, e.Line)
		if IsTestPath(e.Path) {
			label += " (test)"
		}
		evidence = append(evidence, label)
	}
	if rest := len(absent) - len(evidence); rest > 0 {
		evidence = append(evidence, fmt.Sprintf("... and %d more", rest))
	}

	return Finding{
		Dimension: DimRisk,
		Subject:   absent[0].Anchor,
		Tier:      Unresolvable,
		Summary: fmt.Sprintf(
			"%d entities were removed or renamed (%d outside tests): their callers cannot be counted, because the graph is built from the head commit and these are no longer in it",
			len(absent), sourceCount),
		Evidence: evidence,
		Verify:   "git log -p -1 -- <path>   # and search the base commit for callers of the removed name",
	}
}
