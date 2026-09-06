package gate

import (
	"strings"
	"testing"
)

// Why an unresolvable region must never read as a clean bill of health.
//
// Reflected is called only through reflection, so the graph holds no edges into
// it. Before the curveball that rendered as "0 dependents" — indistinguishable
// from a genuinely unreferenced function. The count is not the bug; presenting
// it as knowledge is.
func TestUnresolvableRegionIsNotReportedAsZeroDependents(t *testing.T) {
	ix := partialIndex()
	entities := []ChangedEntity{entity("Reflected", pathReflected, 15, SignatureChanged)}

	Risk(entities, ix, 2)

	if got := entities[0].Dependents; got != 0 {
		t.Fatalf("fixture expects no resolvable dependents, got %d", got)
	}
	if got := entities[0].DependentsTier; got != Unresolvable {
		t.Fatalf("tier = %q, want %q: a symbol in a region the graph cannot analyse "+
			"must not carry a confident count", got, Unresolvable)
	}
	phrase := dependentsPhrase(entities[0])
	if strings.Contains(phrase, "0 dependents") {
		t.Fatalf("rendered %q: an unresolved count must not render as a bare zero", phrase)
	}
	if !strings.Contains(phrase, "unresolvable") {
		t.Fatalf("rendered %q: the reader must be told the graph could not look", phrase)
	}
}

// Why a blind spot must produce a finding rather than silence.
//
// Silence is how Gate reports "nothing to worry about". A region it could not
// analyse has to say so out loud, or absence of evidence is served as evidence
// of absence.
func TestUnresolvableRegionProducesAFindingWithAVerificationPath(t *testing.T) {
	ix := partialIndex()
	entities := []ChangedEntity{entity("Reflected", pathReflected, 15, BodyChanged)}

	findings := Risk(entities, ix, 2)

	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1: an unanalysable region must be reported", len(findings))
	}
	if findings[0].Tier != Unresolvable {
		t.Fatalf("tier = %q, want %q", findings[0].Tier, Unresolvable)
	}
	if findings[0].Verify == "" {
		t.Fatal("finding has no verification path: a claim Gate cannot prove must " +
			"ship with the way to settle it")
	}
	if !strings.Contains(findings[0].Summary, "reflective dispatch") {
		t.Fatalf("summary %q does not say why analysis was partial", findings[0].Summary)
	}
}

// Why a body change in a blind region still gets reported.
//
// Risk normally ignores body changes: the contract holds, so callers still
// compile. That reasoning depends on knowing who the callers are. Where the
// graph cannot see them, the exemption is not earned.
func TestUnresolvableBodyChangeIsReportedEvenThoughBodyChangesAreNormallyExempt(t *testing.T) {
	resolved := []ChangedEntity{entity("Dispatch", "pkg/dispatch.go", 9, BodyChanged)}
	if got := Risk(resolved, partialIndex(), 2); len(got) != 0 {
		t.Fatalf("findings = %d, want 0: a resolved body change is still exempt", len(got))
	}

	blind := []ChangedEntity{entity("Reflected", pathReflected, 15, BodyChanged)}
	if got := Risk(blind, partialIndex(), 2); len(got) != 1 {
		t.Fatalf("findings = %d, want 1: the exemption assumes we can see the callers", len(got))
	}
}

// Why one inferred edge makes the whole chain inferred.
//
// Caller -> Dispatch is exact, Dispatch -> Handler is a name match. Walking two
// hops back from Handler crosses both. A conclusion is only as sound as its
// weakest step, so the count is heuristic even though half of it is proven.
func TestOneInferredEdgeMakesTheWholeWalkHeuristic(t *testing.T) {
	ix := partialIndex()
	// Adapter <- Dispatch is inferred; Dispatch <- Caller is exact. The walk
	// crosses both, so half of this count is proven — and it is still reported
	// as inferred, because the chain is only as sound as its weakest step.
	deps, counts := ix.DependentsWithTier([]string{idAdapter}, 2)

	if len(deps) != 2 {
		t.Fatalf("dependents = %v, want Dispatch and Caller", names(deps))
	}
	if counts.Tier() != Heuristic {
		t.Fatalf("tier = %q, want %q: crossing one inferred edge makes the chain inferred",
			counts.Tier(), Heuristic)
	}
	// Both were reached through the inferred hop, so neither is proven — and
	// the revert rule must therefore not fire on them.
	if counts.ConfirmedCount != 0 {
		t.Fatalf("proven = %d, want 0: every path here crosses the inferred edge",
			counts.ConfirmedCount)
	}
	if counts.Proven() {
		t.Fatal("Proven() is true with no confirmed dependent")
	}
}

// Why the count is reported as a composition rather than one label.
//
// Reporting only the weakest edge made every large blast radius read
// "inferred", which fired the marker on exactly the high-fan-out symbols that
// most need a trustworthy number. A reader needs to know how much of the count
// is proven, not merely that something in it was not.
func TestDependentCountsSeparateProvenFromInferred(t *testing.T) {
	ix := partialIndex()
	// Dispatch is reached only by Caller, through an exact edge.
	_, counts := ix.DependentsWithTier([]string{idDispatch}, 2)
	if counts.ConfirmedCount != 1 || counts.HeuristicCount != 0 {
		t.Fatalf("counts = %+v, want 1 proven and 0 inferred", counts)
	}
	if got := counts.Describe(); got != "1 dependents" {
		t.Fatalf("Describe = %q, want a bare count when everything is proven", got)
	}
	if !counts.Proven() {
		t.Fatal("a dependent reached by an exact edge must count as proven")
	}

	mixed := TierCounts{ConfirmedCount: 180, HeuristicCount: 52, Region: Confirmed}
	if got := mixed.Describe(); got != "232 dependents (180 proven, 52 inferred)" {
		t.Fatalf("Describe = %q: a reader must be told how much of the count holds", got)
	}
	if !mixed.Proven() {
		t.Fatal("180 proven dependents must satisfy Proven()")
	}
}

// Why a walk that reaches an unparsed file is unresolvable, not merely inferred.
//
// Handler is reached from Dispatch (inferred) and from generated code the
// parser could not read. The second is worse than inference: behind it may be
// callers that produced no edge at all, so the count is a floor.
func TestAWalkThroughAnUnparsedFileIsUnresolvableNotHeuristic(t *testing.T) {
	_, counts := partialIndex().DependentsWithTier([]string{idHandler}, 2)
	if counts.Tier() != Unresolvable {
		t.Fatalf("tier = %q, want %q: an unparsed source of edges makes the count a floor",
			counts.Tier(), Unresolvable)
	}
}

// Why heuristic evidence may not reach revert.
//
// revert is the one verdict a reviewer cannot easily argue with, so it requires
// proof. Handler is a breaking change with dependents and no covering test —
// the exact revert shape — but its dependents rest on an inferred edge. That is
// a reason to look, not a reason to roll back.
func TestHeuristicDependentsCannotReachRevert(t *testing.T) {
	both := Availability{Risk: true, Coverage: true}

	proven := ChangedEntity{
		Anchor: Anchor{Name: "Dispatch", Path: "pkg/dispatch.go", Line: 9}, Kind: "function",
		ChangeType: SignatureChanged, Dependents: 1, Coverage: Unchecked,
		DependentsTier:   Confirmed,
		DependentsCounts: TierCounts{ConfirmedCount: 1, Region: Confirmed},
	}
	if got := Decide([]ChangedEntity{proven}, nil, both); got != Revert {
		t.Fatalf("verdict = %q, want %q: confirmed evidence must still reach revert", got, Revert)
	}

	inferred := proven
	inferred.DependentsTier = Heuristic
	inferred.DependentsCounts = TierCounts{HeuristicCount: 1, Region: Confirmed}
	if got := Decide([]ChangedEntity{inferred}, nil, both); got == Revert {
		t.Fatal("verdict reached revert on inferred evidence: only confirmed " +
			"structural evidence may carry the strongest conclusion")
	}
}

// Why a quiet unresolvable entity cannot reach keep.
//
// Everything about this entity looks fine — verified coverage, no dependents,
// no findings. All of that was read off a graph that could not see into its
// file, so the calm is an artefact of the blind spot, not a fact about the code.
func TestUnresolvableEntityCannotReachKeep(t *testing.T) {
	both := Availability{Risk: true, Coverage: true}

	clean := ChangedEntity{
		Anchor: Anchor{Name: "Reflected", Path: pathReflected, Line: 15}, Kind: "function",
		ChangeType: BodyChanged, Dependents: 0, Coverage: Verified,
		DependentsTier: Confirmed,
	}
	if got := Decide([]ChangedEntity{clean}, nil, both); got != Keep {
		t.Fatalf("verdict = %q, want %q: a genuinely clean entity must still reach keep", got, Keep)
	}

	blind := clean
	blind.DependentsTier = Unresolvable
	blind.DependentsCounts = TierCounts{Region: Unresolvable}
	if got := Decide([]ChangedEntity{blind}, nil, both); got != Continue {
		t.Fatalf("verdict = %q, want %q: a clean reading from a region the graph "+
			"could not analyse is not a pass", got, Continue)
	}
}

// Why the report must state its own limits before it states a number.
func TestPartialAnalysisIsAnnouncedBeforeAnyCount(t *testing.T) {
	complete := Analysis{ConfirmedEntities: 3}
	if !complete.Complete() {
		t.Fatal("an all-confirmed analysis must report itself complete")
	}
	if note := EvidenceNote(complete); note != "" {
		t.Fatalf("EvidenceNote = %q, want empty: a complete analysis adds no caveat", note)
	}

	partial := Analysis{ConfirmedEntities: 1, UnresolvableEntities: 2, HeuristicEntities: 1}
	note := EvidenceNote(partial)
	if !strings.Contains(note, "PARTIAL ANALYSIS") {
		t.Fatalf("EvidenceNote = %q: a partial analysis must announce itself", note)
	}
	if !strings.Contains(note, "not authoritative") {
		t.Fatalf("EvidenceNote = %q: a partial report must refuse the word authoritative", note)
	}
}

// Why an unstated relation stays confirmed.
//
// Every projection Gate ships states Confidence and Resolution. A relation that
// states neither only occurs in fixtures that predate the distinction and
// describe fully resolved code, and the curveball requires those to keep
// working unchanged. An unrecognised resolution string is a different matter:
// unknown means unproven.
func TestRelationTierDefaultsToConfirmedButUnknownResolutionsDoNot(t *testing.T) {
	cases := []struct {
		name string
		in   Relation
		want EvidenceTier
	}{
		{"unstated (pre-curveball fixture)", Relation{}, Confirmed},
		{"exact and high confidence", Relation{Resolution: "exact", Confidence: 0.95}, Confirmed},
		// Measured: real "exact" edges carry confidences down to 0.7. Demoting
		// those made almost every walk read as inferred, so the method decides
		// the tier and confidence is only a floor.
		{"exact at the low end of its real range", Relation{Resolution: "exact", Confidence: 0.7}, Confirmed},
		{"import chased to a definition", Relation{Resolution: "import_resolved", Confidence: 0.9}, Confirmed},
		{"exact but implausibly low confidence", Relation{Resolution: "exact", Confidence: 0.3}, Heuristic},
		{"matched by name only", Relation{Resolution: "name_only", Confidence: 0.8}, Heuristic},
		{"matched by pattern", Relation{Resolution: "pattern", Confidence: 0.8}, Heuristic},
		{"inferred from a type", Relation{Resolution: "type_inferred", Confidence: 0.9}, Heuristic},
		{"external import", Relation{Resolution: "import_external", Confidence: 0.78}, Heuristic},
		{"resolution the provider adds later", Relation{Resolution: "speculative"}, Heuristic},
		{"explicitly unresolved", Relation{Resolution: "unresolved"}, Unresolvable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := relationTier(tc.in); got != tc.want {
				t.Fatalf("relationTier = %q, want %q", got, tc.want)
			}
		})
	}
}

// Why the fully resolved fixture must be untouched by all of this.
//
// The curveball requires existing behaviour for fully resolved code to keep
// working. The original fixture states no confidence anywhere, so every entity
// in it must still come back confirmed and every count must still render bare.
func TestFullyResolvedRepositoryIsUnaffectedByTiering(t *testing.T) {
	entities := []ChangedEntity{entity("VerifyToken", "pkg/auth.go", 88, SignatureChanged)}
	findings := Risk(entities, fixtureIndex(), 2)

	if entities[0].DependentsTier != Confirmed {
		t.Fatalf("tier = %q, want %q on a fully resolved fixture",
			entities[0].DependentsTier, Confirmed)
	}
	if got := dependentsPhrase(entities[0]); got != "3 dependents" {
		t.Fatalf("rendered %q, want a bare count: confirmed evidence carries no marker", got)
	}
	for _, f := range findings {
		if f.Tier != Confirmed {
			t.Fatalf("finding %q tier = %q, want confirmed", f.Subject.Name, f.Tier)
		}
		if f.Tier.Marker() != "" {
			t.Fatalf("confirmed finding carries marker %q, want none", f.Tier.Marker())
		}
	}
	analysis := SummariseEvidence(entities, nil, nil)
	if !analysis.Complete() {
		t.Fatal("a fully resolved change set must report a complete analysis")
	}
}

// Why the report prints what it could not see, not just what it found.
//
// The evidence section is the fallback path made concrete: it names the files
// analysis could not complete and gives the legend that makes every marker
// elsewhere in the report readable.
func TestEvidenceSectionNamesWhatCouldNotBeAnalysed(t *testing.T) {
	var buf strings.Builder
	writeAnalysis(&buf, Analysis{
		ConfirmedEntities: 4, HeuristicEntities: 2, UnresolvableEntities: 1,
		PartialPaths: []string{pathGenerated, pathTemplate},
		Reasons:      []string{"E_PARSE_ERROR", "ERB is inventory-only"},
	})
	out := buf.String()

	for _, want := range []string{
		"EVIDENCE", pathGenerated, "E_PARSE_ERROR", pathTemplate, "ERB is inventory-only",
		"absence of a", "NOT evidence of safety",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("evidence section missing %q:\n%s", want, out)
		}
	}
}

// Why a fully resolved run prints no evidence section at all.
//
// Existing behaviour for fully resolved code must be untouched, and that
// includes the shape of the output: no new section, no new legend, nothing for
// a reader of a clean repository to skip past.
func TestEvidenceSectionIsAbsentWhenAnalysisIsComplete(t *testing.T) {
	var buf strings.Builder
	writeAnalysis(&buf, Analysis{ConfirmedEntities: 9})
	if buf.String() != "" {
		t.Fatalf("a complete analysis printed an evidence section:\n%s", buf.String())
	}
}

// Why the partial-path listing is bounded like every other section.
func TestPartialPathListingIsBounded(t *testing.T) {
	var paths []string
	for i := 0; i < analysisPathLimit+5; i++ {
		paths = append(paths, "gen/file"+string(rune('a'+i))+".go")
	}
	var buf strings.Builder
	writeAnalysis(&buf, Analysis{UnresolvableEntities: 1, PartialPaths: paths})
	if !strings.Contains(buf.String(), "... and 5 more") {
		t.Fatalf("listing was not bounded:\n%s", buf.String())
	}
}

// Why an unresolvable count renders as a floor rather than a total.
func TestUnresolvableDependentsRenderAsAFloor(t *testing.T) {
	counts := TierCounts{ConfirmedCount: 3, HeuristicCount: 1, UnresolvableCount: 2, Region: Confirmed}
	got := counts.Describe()
	if !strings.HasPrefix(got, "6+ dependents") {
		t.Fatalf("Describe = %q: a count with invisible callers must be marked a floor", got)
	}
	if counts.Tier() != Unresolvable {
		t.Fatalf("tier = %q, want %q", counts.Tier(), Unresolvable)
	}
	if !counts.Proven() {
		t.Fatal("three exactly-resolved dependents must still count as proven")
	}
}

// Why an entity the graph has no symbol for must not render as "0 dependents".
//
// Regression test. Risk's not-in-graph branch set the tier but left the counts
// empty, and an empty TierCounts renders as a bare zero — so the review order
// printed "0 dependents" for a symbol nobody could analyse, while the risk
// section correctly called it unresolvable. One report, two contradictory
// claims, and the confident one came first.
func TestEntityMissingFromTheGraphNeverRendersAsZeroDependents(t *testing.T) {
	entities := []ChangedEntity{entity("GhostMethod", "pkg/ghost.go", 3, SignatureChanged)}

	findings := Risk(entities, partialIndex(), 2)

	if entities[0].DependentsTier != Unresolvable {
		t.Fatalf("tier = %q, want %q", entities[0].DependentsTier, Unresolvable)
	}
	phrase := dependentsPhrase(entities[0])
	if strings.Contains(phrase, "0 dependents") {
		t.Fatalf("review order rendered %q: a symbol absent from the graph must "+
			"not be reported as having no dependents", phrase)
	}
	if !strings.Contains(phrase, "unresolvable") {
		t.Fatalf("review order rendered %q, want the unresolvable phrasing", phrase)
	}
	if len(findings) != 1 || findings[0].Tier != Unresolvable {
		t.Fatalf("findings = %+v, want one unresolvable finding", findings)
	}
}

// Why a method must resolve by its qualified name.
//
// Regression test, found by running Gate on gorilla/mux. The graph keys symbols
// by bare name ("ServeHTTP") while the semantic diff reports methods qualified
// ("Router.ServeHTTP"). An index that knew only the bare name resolved neither,
// so every changed method in that repository was reported as a region the graph
// could not analyse — including Router.ServeHTTP, which the graph holds in full.
//
// This is the Track 2 failure running backwards: claiming blindness about code
// that is perfectly visible. It costs exactly as much trust as over-claiming,
// because a marker that fires wrongly is a marker readers learn to ignore.
func TestMethodsResolveByQualifiedNameNotJustBareName(t *testing.T) {
	symbols := []Symbol{{
		ID: "repo:Go:mux.go:method:ServeHTTP", Name: "ServeHTTP",
		QualifiedName: "Router.ServeHTTP", Path: "mux.go", Line: 188, Kind: "method",
	}, {
		ID: "repo:Go:mux.go:function:caller", Name: "caller",
		Path: "mux.go", Line: 400, Kind: "function",
	}}
	relations := []Relation{{
		FromID: "repo:Go:mux.go:function:caller", ToID: "repo:Go:mux.go:method:ServeHTTP",
		Type: "CALLS", Confidence: 1, Resolution: "exact",
	}}
	ix := NewIndex(symbols, relations)

	if got := ix.Resolve("Router.ServeHTTP", "mux.go"); len(got) != 1 {
		t.Fatalf("Resolve(qualified) = %v, want the method: a diff reports methods qualified", got)
	}
	if got := ix.Resolve("ServeHTTP", "mux.go"); len(got) != 1 {
		t.Fatalf("Resolve(bare) = %v, want the method: bare lookup must keep working", got)
	}

	entities := []ChangedEntity{entity("Router.ServeHTTP", "mux.go", 188, BodyChanged)}
	Risk(entities, ix, 2)
	if entities[0].DependentsTier == Unresolvable {
		t.Fatal("a method the graph holds in full was reported as unresolvable")
	}
	if entities[0].Dependents != 1 {
		t.Fatalf("dependents = %d, want 1", entities[0].Dependents)
	}
}

// Why a route handler can never report a confident zero.
//
// Found by running Gate on pallets/flask. A signature change to the
// decorator-registered `login` view was reported as "0 dependents" at tier
// confirmed — Gate at its most certain about the most externally-visible code
// in the application, and wrong in the direction that costs the most. Nothing
// in the repository calls a view function; the web framework does, through a
// registration the parser cannot follow.
//
// The graph held the evidence all along (HANDLES_ROUTE, 509 of them in flask).
// Gate simply did not read it. A confident zero here is the Track 2 failure in
// its purest form: absence of call sites served as evidence of no callers.
func TestRouteHandlersNeverReportAConfidentZero(t *testing.T) {
	const handler = "repo:Python:auth.py:function:login"
	symbols := []Symbol{{
		ID: handler, Name: "login", Path: "auth.py", Line: 85, Kind: "function",
	}}
	relations := []Relation{{
		FromID: handler, ToID: "external:route:/login", Type: "HANDLES_ROUTE",
		Confidence: 0.8, Resolution: "pattern",
	}}
	ix := NewIndex(symbols, relations)

	reason, external := ix.EntryPointReason([]string{handler})
	if !external {
		t.Fatal("a HANDLES_ROUTE source must be recognised as an external entry point")
	}
	if !strings.Contains(reason, "/login") {
		t.Fatalf("reason = %q, want the route named so a reader can find it", reason)
	}

	entities := []ChangedEntity{entity("login", "auth.py", 85, SignatureChanged)}
	findings := Risk(entities, ix, 2)

	if entities[0].DependentsTier != Unresolvable {
		t.Fatalf("tier = %q, want %q: a view function has callers the graph cannot see",
			entities[0].DependentsTier, Unresolvable)
	}
	if phrase := dependentsPhrase(entities[0]); strings.Contains(phrase, "0 dependents") {
		t.Fatalf("rendered %q: an HTTP endpoint must never read as having no dependents", phrase)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1: a change to an entry point must be reported", len(findings))
	}
	if !strings.Contains(findings[0].Summary, "route handler") {
		t.Fatalf("summary %q does not explain that callers are external", findings[0].Summary)
	}
	if findings[0].Verify == "" {
		t.Fatal("an unverifiable entry point must ship with a way to check it")
	}
}

// Why a plain function is not swept up by the entry-point rule.
//
// The rule must fire on registration, not on being in a file that contains one.
func TestOrdinaryFunctionsAreNotTreatedAsEntryPoints(t *testing.T) {
	const handler = "repo:Python:auth.py:function:login"
	const helper = "repo:Python:auth.py:function:hashPassword"
	symbols := []Symbol{
		{ID: handler, Name: "login", Path: "auth.py", Line: 85, Kind: "function"},
		{ID: helper, Name: "hashPassword", Path: "auth.py", Line: 20, Kind: "function"},
	}
	relations := []Relation{
		{FromID: handler, ToID: "external:route:/login", Type: "HANDLES_ROUTE",
			Confidence: 0.8, Resolution: "pattern"},
		{FromID: handler, ToID: helper, Type: "CALLS", Confidence: 1, Resolution: "exact"},
	}
	ix := NewIndex(symbols, relations)

	if _, external := ix.EntryPointReason([]string{helper}); external {
		t.Fatal("a helper called by a route handler is not itself an entry point")
	}
	entities := []ChangedEntity{entity("hashPassword", "auth.py", 20, SignatureChanged)}
	Risk(entities, ix, 2)
	if entities[0].DependentsTier != Confirmed {
		t.Fatalf("tier = %q, want %q: this symbol's caller is an ordinary call site",
			entities[0].DependentsTier, Confirmed)
	}
	if entities[0].Dependents != 1 {
		t.Fatalf("dependents = %d, want 1 (login calls it)", entities[0].Dependents)
	}
}

// Why an unset tier must be marked rather than render clean.
//
// Third instance of the same defect in this package: EvidenceTier once had a
// Trusted() that answered true only for Confirmed but was consulted for the
// zero value; tierLabel rendered an unset tier as "confirmed"; and Marker
// returned no sigil for it. Each let a caller who forgot to state a tier
// inherit full confidence — the exact failure the tier system exists to
// prevent, committed inside the type meant to prevent it.
//
// Confirmed is an explicit arm so the default arm can mean "we do not know".
func TestUnsetTierIsMarkedNotSilent(t *testing.T) {
	if got := Confirmed.Marker(); got != "" {
		t.Fatalf("Confirmed.Marker() = %q, want empty: marks mean doubt", got)
	}
	if got := Heuristic.Marker(); got != "~" {
		t.Fatalf("Heuristic.Marker() = %q, want ~", got)
	}
	if got := Unresolvable.Marker(); got != "?" {
		t.Fatalf("Unresolvable.Marker() = %q, want ?", got)
	}

	var unset EvidenceTier
	if got := unset.Marker(); got == "" {
		t.Fatal("an unset tier rendered with no marker, which reads as confirmed")
	}
	if got := unset.Marker(); got != "!" {
		t.Fatalf("unset marker = %q, want !", got)
	}
}

// Why a file that imports reflection cannot report confident zeros.
//
// Regression, and an instructive one: this case was originally caught only by
// accident. Handlers.Refund is dispatched reflectively, and Gate reported it
// unresolvable because the diff said "Handlers.Refund" while the graph indexed
// "Refund" — a name-lookup bug producing the right answer for the wrong reason.
// Fixing that lookup made the symbol resolve cleanly and report "0 dependents"
// at tier confirmed. The bug walked back in through the door the fix opened.
//
// The graph does carry the signal: the file IMPORTS reflect. A file that can
// invoke its own symbols by name at runtime cannot treat a missing call edge as
// a missing caller.
func TestFilesImportingReflectionCannotReportConfidentZeros(t *testing.T) {
	const refund = "repo:Go:pkg/refund.go:method:Refund"
	symbols := []Symbol{{
		ID: refund, Name: "Refund", QualifiedName: "Handlers.Refund",
		Path: "pkg/refund.go", Line: 9, Kind: "method",
	}}
	relations := []Relation{{
		FromID: "repo:file:pkg/refund.go", ToID: "external:import:reflect",
		Type: "IMPORTS", Confidence: 0.8, Resolution: "name_only",
	}}
	ix := NewIndex(symbols, relations)

	reason, partial := ix.PartialReason("pkg/refund.go")
	if !partial {
		t.Fatal("a file importing reflect must be marked partial")
	}
	if !strings.Contains(reason, "runtime") {
		t.Fatalf("reason = %q, want it to explain runtime invocation", reason)
	}

	entities := []ChangedEntity{entity("Handlers.Refund", "pkg/refund.go", 9, SignatureChanged)}
	Risk(entities, ix, 2)

	if entities[0].DependentsTier != Unresolvable {
		t.Fatalf("tier = %q, want %q", entities[0].DependentsTier, Unresolvable)
	}
	if phrase := dependentsPhrase(entities[0]); strings.Contains(phrase, "0 dependents") {
		t.Fatalf("rendered %q: a reflectively reachable symbol must not read as unused", phrase)
	}
}

// Why an ordinary file is untouched by the reflection rule.
//
// The rule keys on the import, not on the repository containing one somewhere.
// Without this, one reflect import would make every file in the project
// unresolvable and the signal would be worthless.
func TestReflectionRuleIsScopedToTheImportingFile(t *testing.T) {
	symbols := []Symbol{
		{ID: "a", Name: "Plain", Path: "pkg/plain.go", Line: 3, Kind: "function"},
		{ID: "b", Name: "Caller", Path: "pkg/plain.go", Line: 9, Kind: "function"},
	}
	relations := []Relation{
		{FromID: "repo:file:pkg/refund.go", ToID: "external:import:reflect",
			Type: "IMPORTS", Confidence: 0.8, Resolution: "name_only"},
		{FromID: "b", ToID: "a", Type: "CALLS", Confidence: 1, Resolution: "exact"},
	}
	ix := NewIndex(symbols, relations)

	if _, partial := ix.PartialReason("pkg/plain.go"); partial {
		t.Fatal("a file that does not import reflect must not be marked partial")
	}
	entities := []ChangedEntity{entity("Plain", "pkg/plain.go", 3, SignatureChanged)}
	Risk(entities, ix, 2)
	if entities[0].DependentsTier != Confirmed {
		t.Fatalf("tier = %q, want %q on an ordinary file", entities[0].DependentsTier, Confirmed)
	}
}

// Why removals get one aggregate finding instead of one each.
//
// Risk's not-in-graph branch fires whenever Resolve misses, and a removed
// entity is missing from a head-commit snapshot by definition. So the message
// "not in the graph: no dependent analysis was possible" restates the change
// type rather than reporting a discovery — and it costs a finding slot every
// time. Measured on pytest-dev/pytest: 10 of 10 risk findings were removals,
// nine of them deleted test methods from one class, and the cap of 10 meant
// nothing that might actually break could appear at all.
//
// The gate is not weakened. These entities stay tiered Unresolvable, stay
// counted in Analysis, and still force continue through Decide.
func TestRemovalsAreAggregatedIntoOneFindingNotOneEach(t *testing.T) {
	ix := partialIndex()
	entities := []ChangedEntity{
		entity("GoneOne", "pkg/tests/a_test.go", 10, Removed),
		entity("GoneTwo", "pkg/tests/a_test.go", 20, Removed),
		entity("GoneThree", "pkg/tests/a_test.go", 30, Removed),
		entity("RealSource", "pkg/service.go", 40, Removed),
	}

	findings := Risk(entities, ix, 2)

	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1 aggregate: four removals must not spend four slots", len(findings))
	}
	f := findings[0]
	if f.Tier != Unresolvable {
		t.Fatalf("tier = %q, want %q", f.Tier, Unresolvable)
	}
	if !strings.Contains(f.Summary, "4 entities were removed or renamed") {
		t.Fatalf("summary = %q, want the total", f.Summary)
	}
	if !strings.Contains(f.Summary, "1 outside tests") {
		t.Fatalf("summary = %q: a source removal matters more than a deleted test", f.Summary)
	}
	if !strings.Contains(f.Evidence[0], "RealSource") {
		t.Fatalf("evidence leads with %q, want the non-test removal first", f.Evidence[0])
	}

	// Every removed entity still carries its tier, so the verdict still moves.
	for _, e := range entities {
		if e.DependentsTier != Unresolvable {
			t.Fatalf("%s tier = %q, want %q: aggregating the finding must not "+
				"downgrade the evidence", e.Name, e.DependentsTier, Unresolvable)
		}
	}
	if got := Decide(entities, findings, Availability{Risk: true, Coverage: true}); got == Keep {
		t.Fatal("removals reached keep: aggregation must not weaken the gate")
	}
}

// Why a non-removal missing from the graph still gets its own finding.
//
// The aggregate is for the tautology only. An entity that is present in the
// change set, not removed, and still absent from the graph is a genuine
// finding — an unsupported language, or a file the parser dropped.
func TestNonRemovalsMissingFromTheGraphStillReportIndividually(t *testing.T) {
	entities := []ChangedEntity{entity("MysteryThing", "pkg/weird.zig", 5, SignatureChanged)}

	findings := Risk(entities, partialIndex(), 2)

	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}
	if !strings.Contains(findings[0].Summary, "not in the graph") {
		t.Fatalf("summary = %q, want the per-entity message", findings[0].Summary)
	}
	if strings.Contains(findings[0].Summary, "removed or renamed") {
		t.Fatal("a signature change was folded into the removals aggregate")
	}
}
