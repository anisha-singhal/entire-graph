package gate

import (
	"fmt"
	"sort"
)

// EvidenceTier is the curveball's central addition: it separates what the graph
// proved from what the graph guessed, and both of those from what the graph
// could not see at all.
//
// Gate's original contract had no such field. A relation was projected as
// {FromID, ToID, Type} and every count derived from it was printed with the
// same authority, whether the provider had resolved the call exactly or matched
// it by name. That made two different failures indistinguishable:
//
//   - an over-claim: "156 dependents" presented as a count when some of those
//     edges were inferred;
//   - an under-claim, which is worse: an entity reached only through dynamic
//     dispatch, reflection or generated code resolves to *zero* dependents, and
//     zero dependents is exactly the condition under which Gate stays quiet and
//     returns keep. Gate was most confident where the graph was blindest.
//
// A tool whose pitch is "we report what nobody checked" must not convert
// "we could not see" into "there is nothing there".
type EvidenceTier string

const (
	// Confirmed: the provider resolved this exactly. A reader can act on it.
	Confirmed EvidenceTier = "confirmed"
	// Heuristic: the provider produced it by inference — a name match, a
	// reduced-confidence resolution. Probably true, not proven. Needs a look.
	Heuristic EvidenceTier = "heuristic"
	// Unresolvable: the graph could not analyse this region at all — the file
	// failed to parse, the language is inventory-only, or the call site is
	// dynamic. Absence of edges here is absence of evidence, never evidence of
	// absence, and Gate must say so rather than report a confident zero.
	Unresolvable EvidenceTier = "unresolvable"
)

// tierRank orders the tiers from most to least trustworthy so that combining
// evidence can take the weakest. Evidence does not average: one inferred edge
// in a chain makes the whole chain inferred.
func tierRank(t EvidenceTier) int {
	switch t {
	case Confirmed:
		return 0
	case Heuristic:
		return 1
	default:
		return 2
	}
}

// Weakest returns the less trustworthy of two tiers. This is how a multi-hop
// walk reports itself: a path that crosses one heuristic edge is heuristic
// end to end, because the conclusion is only as sound as its weakest step.
func Weakest(a, b EvidenceTier) EvidenceTier {
	if tierRank(b) > tierRank(a) {
		return b
	}
	return a
}

// Marker is the one-character sigil the text renderer puts on a number so a
// reader can tell tiers apart at a glance without reading a legend. Confirmed
// evidence is unmarked: the common case stays clean, and marks mean doubt.
func (t EvidenceTier) Marker() string {
	switch t {
	case Confirmed:
		return ""
	case Heuristic:
		return "~"
	case Unresolvable:
		return "?"
	}
	// An unset tier is marked, and loudly. This is the third place in this
	// package where the zero value silently meant "trusted" — EvidenceTier had
	// a Trusted() that answered for it, tierLabel rendered it as "confirmed",
	// and this returned no marker at all. Each one let a caller who forgot to
	// state a tier inherit full confidence, which is the Track 2 failure
	// committed inside the type meant to prevent it. Confirmed is now an
	// explicit arm so the default can mean what it should: we do not know.
	return "!"
}

// confirmedFloor is a guard, not the main rule. The provider's own resolution
// method decides the tier (see relationTier); this only catches an edge whose
// stated confidence is so low that the method label cannot be taken at face
// value. Measured against this repository, no "exact" edge falls below 0.7, so
// the floor is a safety net rather than a workhorse.
const confirmedFloor = 0.5

// exactResolutions are the provider resolution methods that identify the target
// definitively rather than by inference.
//
// This mapping is measured, not guessed. Running the snapshot over this
// repository (docs/graph-findings/curveball-resolution-distribution.txt) gives
// eight resolution methods:
//
//	31487 exact            <- the target was resolved outright
//	  738 import_resolved  <- an import chased to a real definition
//	 8217 import_external  \
//	 7190 package           |
//	 5186 name_only         |- inferred: a match by name, shape, package or
//	 4230 pattern           |  pattern, any of which can point at the wrong
//	 2552 type_inferred     |  symbol
//	   57 git_history      /
//
// An earlier version of this function keyed on confidence >= 0.9 instead, and
// that was wrong in a way worth recording: "exact" edges carry confidences of
// 1, 0.92, 0.85 AND 0.7, so the threshold demoted 6,737 genuinely exact edges
// and made almost every multi-hop walk report itself as inferred. A tool that
// marks everything uncertain has not become more honest — it has just moved the
// noise, and a reader learns to ignore the marker. Calibration is part of the
// claim.
var exactResolutions = map[string]bool{
	"exact":           true,
	"import_resolved": true,
}

// unresolvedResolutions are methods that amount to "we did not resolve this".
var unresolvedResolutions = map[string]bool{
	"unresolved": true,
	"none":       true,
}

// relationTier derives a tier from what the provider said about one relation.
//
// The zero value — no Resolution string, no Confidence — is Confirmed, and that
// is a deliberate, bounded choice rather than an oversight. Every projection
// Gate ships (see collect, internal/cli/gate.go) states both fields, so a
// stated-nothing relation only occurs in the synthetic fixtures that predate
// this distinction and describe fully-resolved code by construction. Preserving
// their meaning is exactly the curveball requirement that existing behaviour
// for fully resolved code keeps working. Any *unrecognised* resolution string
// lands in Heuristic, so a provider that grows a new tier degrades safely.
func relationTier(r Relation) EvidenceTier {
	if r.Resolution == "" && r.Confidence == 0 {
		return Confirmed
	}
	if unresolvedResolutions[r.Resolution] {
		return Unresolvable
	}
	if exactResolutions[r.Resolution] {
		if r.Confidence == 0 || r.Confidence >= confirmedFloor {
			return Confirmed
		}
		return Heuristic
	}
	// name_only, pattern, package, type_inferred, import_external, git_history,
	// or anything the provider adds later. Unknown means unproven.
	return Heuristic
}

// TierCounts is a dependent count broken down by how far each dependent may be
// trusted, plus whether the subject's own region could be analysed at all.
//
// A single tier label cannot carry this. "232 dependents, 180 proven" and
// "232 dependents, 3 proven" are very different claims, and collapsing both to
// "inferred" throws away the part a reviewer would actually act on.
type TierCounts struct {
	ConfirmedCount    int `json:"confirmed"`
	HeuristicCount    int `json:"heuristic"`
	UnresolvableCount int `json:"unresolvable"`
	// Region is Unresolvable when the graph could not analyse the file the
	// subject lives in. It overrides the counts: the dependents we found are a
	// floor, because the ones we missed left no trace to count.
	Region EvidenceTier `json:"region"`
}

// Total is the dependent count as printed.
func (c TierCounts) Total() int {
	return c.ConfirmedCount + c.HeuristicCount + c.UnresolvableCount
}

// Tier is the one-word summary, for the marker and for grouping.
//
// A dependent that lives in a region the graph could not analyse counts as
// unresolvable, not merely inferred, and it drags the whole composition with
// it. The difference matters: an inferred edge might point at the wrong symbol,
// but an unparsed file's own callers left no edge at all, so the number is a
// floor rather than an estimate. Calling that "heuristic" would understate it.
func (c TierCounts) Tier() EvidenceTier {
	if c.Region == Unresolvable || c.UnresolvableCount > 0 {
		return Unresolvable
	}
	if c.HeuristicCount > 0 {
		return Heuristic
	}
	return Confirmed
}

// Proven reports whether at least one dependent is established beyond
// inference. This, not the overall tier, is what the revert rule turns on: the
// claim "this breaking change has dependents" needs one proven dependent, not
// a fully proven set.
func (c TierCounts) Proven() bool {
	return c.Region != Unresolvable && c.ConfirmedCount > 0
}

// Describe renders the composition for a reader: bare when everything is
// proven, explicit about the split when it is not.
func (c TierCounts) Describe() string {
	if c.Region == Unresolvable {
		if c.Total() == 0 {
			return "dependents unresolvable (graph could not see this region)"
		}
		return fmt.Sprintf("%d+ dependents (unresolvable — this is a floor)", c.Total())
	}
	if c.HeuristicCount == 0 && c.UnresolvableCount == 0 {
		return fmt.Sprintf("%d dependents", c.Total())
	}
	// A "+" marks a floor: some caller lives where the graph cannot look, so
	// the real number can only be larger.
	if c.UnresolvableCount > 0 {
		return fmt.Sprintf("%d+ dependents (%d proven, %d inferred, %d through regions the graph could not analyse)",
			c.Total(), c.ConfirmedCount, c.HeuristicCount, c.UnresolvableCount)
	}
	if c.ConfirmedCount == 0 {
		return fmt.Sprintf("~%d dependents (none proven)", c.Total())
	}
	return fmt.Sprintf("%d dependents (%d proven, %d inferred)",
		c.Total(), c.ConfirmedCount, c.HeuristicCount)
}

// Analysis records how completely the graph could see the change under review,
// so the report can state its own limits instead of leaving a reader to assume
// the picture is whole.
type Analysis struct {
	// PartialPaths are files whose relations could not be fully resolved —
	// parse failures, inventory-only languages, dynamic dispatch. Any entity
	// living in one is Unresolvable regardless of how many edges point at it.
	PartialPaths []string `json:"partial_paths,omitempty"`
	// Reasons explains, per path, why analysis there is partial. Same order.
	Reasons []string `json:"partial_reasons,omitempty"`

	ConfirmedEntities    int `json:"confirmed_entities"`
	HeuristicEntities    int `json:"heuristic_entities"`
	UnresolvableEntities int `json:"unresolvable_entities"`
}

// Complete reports whether every entity in the change set rests on confirmed
// evidence. When this is false the report must not read as authoritative.
func (a Analysis) Complete() bool {
	return a.HeuristicEntities == 0 && a.UnresolvableEntities == 0 && len(a.PartialPaths) == 0
}

// SummariseEvidence counts the change set by tier, for the report header.
func SummariseEvidence(entities []ChangedEntity, partial []string, reasons []string) Analysis {
	a := Analysis{PartialPaths: append([]string(nil), partial...), Reasons: append([]string(nil), reasons...)}
	sort.Strings(a.PartialPaths)
	for _, e := range entities {
		switch e.DependentsTier {
		case Confirmed:
			a.ConfirmedEntities++
		case Heuristic:
			a.HeuristicEntities++
		default:
			a.UnresolvableEntities++
		}
	}
	return a
}
