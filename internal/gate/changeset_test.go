package gate

import (
	"bytes"
	"strings"
	"testing"
)

// Why prose entities must leave the change set before any signal runs.
//
// Gate resolves every changed entity against the graph and reports the ones it
// cannot find as regions the graph could not analyse. A Markdown heading is
// never in the symbol graph, so a change set that touches a design document
// manufactures "unresolvable" entities out of prose. On this repository that
// was 66 of 583, nearly all of them headings in Gate's own PLAN.md.
//
// The cost is not clutter. The unresolvable marker's meaning is "the graph
// could not see here, so absence of a finding is not evidence of safety", and a
// marker that fires on a heading is one a reader learns to skip — so it is not
// believed on the day it fires on a template that really is hiding a caller.
func TestProseEntitiesLeaveTheChangeSetAndAreCounted(t *testing.T) {
	entities := []ChangedEntity{
		{Anchor: Anchor{Name: "3.-The-moat", Path: "PLAN.md"}, Kind: "section"},
		{Anchor: Anchor{Name: "Decide", Path: "internal/gate/verdict.go"}, Kind: "function"},
		{Anchor: Anchor{Name: "", Path: "PLAN.md"}, Kind: "code_fence"},
		{Anchor: Anchor{Name: "Report", Path: "internal/gate/types.go"}, Kind: "type"},
	}

	analysable, prose := PartitionProse(entities)

	if prose != 2 {
		t.Fatalf("prose = %d, want 2", prose)
	}
	if len(analysable) != 2 {
		t.Fatalf("kept %d entities, want 2: %+v", len(analysable), analysable)
	}
	for _, e := range analysable {
		if IsProseKind(e.Kind) {
			t.Fatalf("prose entity %q survived the filter", e.Kind)
		}
	}
}

// Why "setting" is not prose.
//
// A changed configuration value can genuinely break something, so dropping it
// would be the silent narrowing this package exists to prevent. It stays in the
// change set and is reported as having no resolvable dependents — which is
// true, and is a different statement from not being reported at all.
func TestConfigurationEntitiesAreNotTreatedAsProse(t *testing.T) {
	if IsProseKind("setting") {
		t.Fatal("a changed setting can break something; it must stay in the change set")
	}
	if IsProseKind("module") {
		t.Fatal("module is a code container, not prose")
	}
	if !IsProseKind("section") || !IsProseKind("code_fence") {
		t.Fatal("markdown structure must be filtered")
	}
	// Regression: found by running Gate on pytest and scrapy. "document" is the
	// whole-file entity for a prose file, and leaving it in the change set let
	// doc/en/reference/plugin_list.rst be reported as a registered tool handler
	// with external callers.
	if !IsProseKind("document") {
		t.Fatal("a whole .rst document is prose; left in, it collects entry-point claims")
	}
}

// Why the blind-spot classification is a denylist and not a capability lookup.
//
// The obvious rule was "mark a file as a blind spot when its language supports
// none of the relations the walk follows". Measured against the provider's own
// capability report, that rule is wrong: Markdown, CSS, Patch, Git Ignore,
// HTML, Vue and Svelte all advertise exactly [CONTAINS, DEFINES]. It cannot
// separate a Vue component, which embeds JavaScript and can absolutely hide a
// caller, from a CHANGELOG that cannot hide anything.
//
// So the list is Gate's own, and it fails toward disclosure: anything
// unrecognised is treated as able to hide a caller.
func TestOnlyInertLanguagesAreExcusedFromBeingBlindSpots(t *testing.T) {
	for _, language := range []string{"Markdown", "JSON", "TOML", "Git Ignore", "Patch", "CSS"} {
		if CanHideACaller(language) {
			t.Errorf("%s cannot invoke code; reporting it as a blind spot is noise", language)
		}
	}
	// The template languages are the reason a capability lookup was rejected.
	for _, language := range []string{"Vue", "Svelte", "HTML", "ERB"} {
		if !CanHideACaller(language) {
			t.Errorf("%s embeds code and can hide a caller the graph never sees", language)
		}
	}
	if !CanHideACaller("Some Language Invented Next Year") {
		t.Fatal("an unclassified language must fail toward disclosure, not toward a clean bill of health")
	}
	if !CanHideACaller("") {
		t.Fatal("an unknown language is exactly where a confident silence is least defensible")
	}
}

// Why an unset evidence tier must never render as "confirmed".
//
// Regression test. tierLabel's switch had no arm for the zero value, so ""
// fell through the default and printed "confirmed" — and Coverage() never set
// a tier, so every coverage finding in every report claimed proof it had never
// been asked for.
//
// This is the same defect as a dependent count of zero that actually means "we
// could not look": in both, silence renders as assurance. A default that fails
// open in a tool whose entire claim is calibrated confidence is worse than a
// wrong number, because it is wrong in the direction of trust.
func TestAnUnsetTierNeverRendersAsConfirmed(t *testing.T) {
	var unset EvidenceTier

	if label := tierLabel(unset); strings.Contains(label, "confirmed") {
		t.Fatalf("tierLabel(unset) = %q: an unstated tier must not read as proven", label)
	}
	if marker := unset.Marker(); marker == "" {
		t.Fatal("an unmarked finding reads as confirmed; an unset tier must carry a sigil")
	}
	if label := tierLabel(Confirmed); label != "confirmed" {
		t.Fatalf("tierLabel(Confirmed) = %q, want the plain word", label)
	}
	if Confirmed.Marker() != "" {
		t.Fatal("confirmed evidence stays unmarked: marks mean doubt")
	}
}

// Why coverage findings state a tier of their own.
//
// Coverage is graph-derived too, so "no test covers this" is exactly as
// dependent on the graph having been able to look as a dependent count is. The
// curveball hardened the risk dimension and left this one reading [confirmed]
// on the strength of an unset field.
func TestCoverageFindingsCarryAnExplicitTierAndAWayToCheckThem(t *testing.T) {
	entities := []ChangedEntity{{
		Anchor:           Anchor{Name: "Load", Path: "pkg/config.go", Line: 31},
		Kind:             "function",
		ChangeType:       SignatureChanged,
		Coverage:         Unchecked,
		DependentsCounts: TierCounts{ConfirmedCount: 2, Region: Confirmed},
		Dependents:       2,
	}}

	findings := Coverage(entities)

	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}
	if findings[0].Tier == "" {
		t.Fatal("coverage finding left its tier unset, which renders as confirmed")
	}
	if findings[0].Tier != Confirmed {
		t.Fatalf("tier = %q: a clean region genuinely was searched for tests", findings[0].Tier)
	}
	// An absence claim is the one a reader cannot check by looking at what is
	// printed, because nothing is printed. It ships with the command instead.
	if findings[0].Verify == "" {
		t.Fatal("an absence claim must carry the way to settle it")
	}
}

// Why a CI job is not reported as untested.
//
// Found on gin-gonic/gin: nine of ten changed entities were GitHub Actions
// workflows and jobs, and every one was reported "unchecked: no test covers it"
// at tier confirmed — Gate asserting with full confidence that jobs.trivy-scan
// has no unit test. True, and a category error. "unchecked" is meant to carry
// the accusation that somebody should have written a test; nobody was ever
// going to unit-test a release job.
//
// They stay in the change set — a broken release job is a real problem, and
// risk still ranks them — but the coverage dimension reports NoResolver, the
// state that already means "no jurisdiction here" and is never rendered as a
// finding against the author.
func TestPipelineEntitiesAreOutsideCoverageJurisdiction(t *testing.T) {
	entities := []ChangedEntity{
		{Anchor: Anchor{Name: "jobs.trivy-scan", Path: ".github/workflows/scan.yml"}, Kind: "job"},
		{Anchor: Anchor{Name: "codeql", Path: ".github/workflows/codeql.yml"}, Kind: "workflow"},
	}

	ResolveCoverage(entities, fixtureIndex(), true)

	for _, e := range entities {
		if e.Coverage != NoResolver {
			t.Fatalf("%s %q coverage = %q, want %q", e.Kind, e.Name, e.Coverage, NoResolver)
		}
	}
	if findings := Coverage(entities); len(findings) != 0 {
		t.Fatalf("findings = %+v, want none: a CI job has no unit test to be missing", findings)
	}
	// They must not be filtered out of the change set the way prose is: a
	// broken release job is a real defect, just not a coverage one.
	if kept, prose := PartitionProse(entities); prose != 0 || len(kept) != 2 {
		t.Fatalf("pipeline entities were dropped from the change set: kept %d, prose %d", len(kept), prose)
	}
}

// Why an unresolvable region is not reported as untested.
//
// The edges that would prove a test reaches this code are the same edges the
// parser failed to produce. Reporting Unchecked there charges the author for
// Gate's blind spot — the coverage-side twin of the dependent count that was
// printed as a confident zero.
//
// It must not become a free pass either: Decide keeps such an entity at
// continue through its own unresolvable arm, so demoting the coverage claim
// costs no strictness.
func TestAnUnanalysableRegionIsNoResolverRatherThanUnchecked(t *testing.T) {
	entities := []ChangedEntity{entity("Template", pathTemplate, 1, SignatureChanged)}
	ix := partialIndex()

	ResolveCoverage(entities, ix, true)

	if entities[0].Coverage != NoResolver {
		t.Fatalf("coverage = %q, want %q: a region the parser could not read cannot be searched for tests",
			entities[0].Coverage, NoResolver)
	}
	if findings := Coverage(entities); len(findings) != 0 {
		t.Fatalf("findings = %+v, want none: NoResolver is a limit of Gate, not a defect in the change", findings)
	}
}

// Why the coverage section is ranked before it is capped.
//
// The section prints at most coverageFindingLimit findings. Truncating in
// entity order truncates alphabetically by path, so a 40-dependent unchecked
// type in internal/sem was cut while a 1-dependent helper in internal/cli
// survived, purely because "c" sorts before "s". The cap should decide how much
// to print, never what matters.
func TestTheCoverageCapKeepsTheWidestBlastRadiusNotTheFirstAlphabetically(t *testing.T) {
	var entities []ChangedEntity
	// aaa/ sorts first and has one dependent; zzz/ sorts last and has many.
	for i := 0; i < coverageFindingLimit; i++ {
		e := entity("Small", "aaa/small.go", i, BodyChanged)
		e.Coverage, e.Dependents = Unchecked, 1
		entities = append(entities, e)
	}
	widest := entity("Widest", "zzz/wide.go", 1, SignatureChanged)
	widest.Coverage, widest.Dependents = Unchecked, 40
	entities = append(entities, widest)

	findings := Coverage(entities)

	if len(findings) != coverageFindingLimit {
		t.Fatalf("findings = %d, want the cap %d", len(findings), coverageFindingLimit)
	}
	if findings[0].Subject.Name != "Widest" {
		t.Fatalf("first finding is %q, want Widest: the cap must not drop the widest blast radius",
			findings[0].Subject.Name)
	}
}

// Why the partial-analysis listing is grouped rather than one flat list.
//
// A file that failed to parse is a hole in a language Gate otherwise resolves.
// A file in an inventory-only language was never going to yield relations.
// Both are disclosed — the curveball requires that analysis identify when it is
// partial — but printing them as one undifferentiated list of "files analysis
// could not complete" overstates the second and buries the first.
func TestPartialAnalysisSeparatesParseFailuresFromFilesNeverAnalysed(t *testing.T) {
	analysis := Analysis{
		PartialPaths: []string{"app/views/show.html.erb", "pkg/api.gen.go", "src/flask/cli.py"},
		Reasons: []string{
			NotAnalysedPrefix + " ERB is inventory-only, so a call from it would leave no edge",
			"E_PARSE_ERROR: dependent references in this file may be undercounted",
			RuntimeDispatchPrefix + " Python dynamic import is imported here",
		},
		UnresolvableEntities: 3,
	}

	var buf bytes.Buffer
	writeAnalysis(&buf, analysis)
	out := buf.String()

	runtime := strings.Index(out, "callers may be resolved at runtime")
	parse := strings.Index(out, "failed to parse")
	never := strings.Index(out, "never analysed for relations")
	if runtime < 0 || parse < 0 || never < 0 {
		t.Fatalf("all three groups must appear:\n%s", out)
	}
	// Regression: reflection findings share the Index's partial channel with
	// parse failures, so before the tag they printed as "failed to parse". On
	// Django that mislabelled 99 files, none of which had failed to parse, and
	// it buried the one claim only this tool makes.
	if !(runtime < parse && parse < never) {
		t.Fatalf("groups are ordered by what each says about the change:\n%s", out)
	}
	if strings.Contains(out, NotAnalysedPrefix) || strings.Contains(out, RuntimeDispatchPrefix) {
		t.Fatalf("the grouping tags are machinery and must not reach the reader:\n%s", out)
	}
	for _, path := range analysis.PartialPaths {
		if !strings.Contains(out, path) {
			t.Fatalf("every partial path stays disclosed, missing %s:\n%s", path, out)
		}
	}
}

// Why the grouping tags are stripped at the point of printing.
//
// Regression, found on pytest. The tags exist so writeAnalysis can bucket a
// partial-analysis path, but that same reason string is reused in a finding's
// summary and in its verify hint. Stripping it only in the listing left the
// other two leaking, and a reader was handed:
//
//	verify: rg -n '\bTestIssue14445\b' -- '*'   # runtime dispatch: this file imports pkgutil ...
//
// A shell command with our internal bucketing vocabulary pasted into its
// comment. Printing is the one place every reuse converges, so that is where
// the tags come off.
func TestGroupingTagsNeverReachTheReader(t *testing.T) {
	findings := []Finding{{
		Dimension: DimRisk,
		Subject:   Anchor{Name: "handler", Path: "app/views.py", Line: 12},
		Tier:      Unresolvable,
		Summary:   "dependents could not be resolved (" + RuntimeDispatchPrefix + " this file imports pkgutil)",
		Evidence:  []string{NotAnalysedPrefix + " ERB is inventory-only"},
		Verify:    "rg -n 'handler'   # " + RuntimeDispatchPrefix + " this file imports pkgutil",
	}}

	var buf bytes.Buffer
	writeFindings(&buf, findings)
	out := buf.String()

	for _, tag := range []string{RuntimeDispatchPrefix, NotAnalysedPrefix} {
		if strings.Contains(out, tag) {
			t.Fatalf("tag %q leaked into the report:\n%s", tag, out)
		}
	}
	// Stripping the tag must not strip the explanation with it.
	if !strings.Contains(out, "this file imports pkgutil") {
		t.Fatalf("the reason itself was lost:\n%s", out)
	}
	if !strings.Contains(out, "ERB is inventory-only") {
		t.Fatalf("evidence text was lost:\n%s", out)
	}
}

// A complete analysis prints no evidence section at all, so a fully resolved
// repository's report is exactly what it was before the curveball. This is the
// curveball's own requirement that existing behaviour keeps working, pinned.
func TestACompleteAnalysisAddsNothingToTheReport(t *testing.T) {
	var buf bytes.Buffer
	writeAnalysis(&buf, Analysis{ConfirmedEntities: 12})
	if buf.Len() != 0 {
		t.Fatalf("a fully resolved repository gained output:\n%s", buf.String())
	}
}
