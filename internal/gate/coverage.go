package gate

import (
	"fmt"
	"sort"
	"strings"
)

// coverageFindingLimit bounds the coverage section. Unchecked entities beyond
// it are still counted in the header and still rank in the review order.
const coverageFindingLimit = 10

// Coverage reports the entities nobody checked.
//
// The collect layer resolves each entity's CoverageState; this decides what is
// worth reporting. Unchecked entities with dependents lead, because "nothing
// tests this and other code depends on it" is a different claim from "nothing
// tests this dead corner".
//
// NoResolver is never reported as a finding. Not being able to look for tests
// is a limit of Gate, not a defect in the change, and charging the author for
// it would be the quiet dishonesty this tool exists to avoid. It surfaces in
// the header count and in Availability instead.
func Coverage(entities []ChangedEntity) []Finding {
	// Findings are paired with the blast radius that ranks them. Finding itself
	// deliberately carries no count: it is the render contract, shared by every
	// dimension, and a field only the coverage sort reads would be dead weight
	// in the other three.
	type ranked struct {
		finding    Finding
		dependents int
	}
	var withDependents, without []ranked
	for _, e := range entities {
		if e.Coverage != Unchecked {
			continue
		}
		finding := Finding{
			Dimension: DimCoverage,
			Subject:   e.Anchor,
			// The tier is stated rather than left at its zero value, and that
			// is the fix for a defect worth recording: an unset tier used to
			// render as "confirmed", so every coverage finding claimed proof it
			// had never been asked for. Unset silently meaning trusted is the
			// same failure the evidence tiers exist to prevent, one layer up.
			//
			// Confirmed is honest here only because ResolveCoverage now demotes
			// an entity in an unresolvable region to NoResolver, which never
			// reaches this loop. What is left is a genuine graph observation:
			// we looked at every incoming edge and none came from a test.
			Tier: coverageTier(e),
			Summary: fmt.Sprintf("%s %s is unchecked: no test covers it (%s)",
				e.Kind, e.ChangeType, e.DependentsCounts.Describe()),
			// An absence claim always ships with the way to settle it, even at
			// Confirmed. Every other finding says "here is what I found, go
			// look"; this one says "I found nothing", and a reader has no way
			// to distinguish a true absence from a test the graph cannot see
			// unless Gate hands them the command.
			Verify: coverageVerifyHint(e),
		}
		if e.Dependents > 0 {
			withDependents = append(withDependents, ranked{finding, e.Dependents})
		} else {
			without = append(without, ranked{finding, e.Dependents})
		}
	}

	// Rank inside each group by blast radius before the cap applies. Without
	// this the section is truncated in the entity order, which is alphabetical
	// by path — so a 39-dependent unchecked type in internal/sem could be cut
	// while a 1-dependent helper in internal/cli survived, purely because "c"
	// sorts before "s". The cap decides how much to print; it should not also
	// decide what matters.
	byBlastRadius := func(group []ranked) {
		sort.SliceStable(group, func(i, j int) bool {
			a, b := group[i], group[j]
			if a.dependents != b.dependents {
				return a.dependents > b.dependents
			}
			if a.finding.Subject.Path != b.finding.Subject.Path {
				return a.finding.Subject.Path < b.finding.Subject.Path
			}
			return a.finding.Subject.Name < b.finding.Subject.Name
		})
	}
	byBlastRadius(withDependents)
	byBlastRadius(without)

	all := append(withDependents, without...)
	if len(all) > coverageFindingLimit {
		all = all[:coverageFindingLimit]
	}
	findings := make([]Finding, 0, len(all))
	for _, r := range all {
		findings = append(findings, r.finding)
	}
	return findings
}

// coverageTier says how far the "nobody tested this" claim can be trusted.
//
// An entity whose own region the graph could not analyse never reaches a
// coverage finding — ResolveCoverage marks it NoResolver — so the remaining
// case is a clean region, where the absence of a test edge is a real
// observation. The unresolvable arm is kept anyway: it costs one branch, and
// the alternative is a function that is only correct as long as its caller
// stays correct.
func coverageTier(e ChangedEntity) EvidenceTier {
	if e.DependentsCounts.Region == Unresolvable {
		return Unresolvable
	}
	return Confirmed
}

// coverageVerifyHint is how a reader settles an absence claim for themselves.
func coverageVerifyHint(e ChangedEntity) string {
	if e.VerifyHint != "" {
		return e.VerifyHint
	}
	return fmt.Sprintf("rg -n '\\b%s\\b' -g '*test*'   # a test the graph cannot resolve would not appear above",
		lastSegment(e.Name))
}

// lastSegment reduces a qualified name to the part a text search will match:
// the diff reports methods as "Router.ServeHTTP", and no test file contains
// that string.
func lastSegment(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 && i+1 < len(name) {
		return name[i+1:]
	}
	return name
}

// testPathMarkers are the path shapes that mean "this file holds tests". The
// list is convention, not proof, and it is the reason CoverageState has a
// NoResolver arm: a language whose tests match none of these is reported as
// unresolvable rather than as untested.
var testPathMarkers = []string{"_test.", "test_", ".test.", ".spec.", "/tests/", "/test/"}

// IsTestPath reports whether a repository-relative path looks like a test file.
func IsTestPath(path string) bool {
	for _, marker := range testPathMarkers {
		if strings.Contains(path, marker) {
			return true
		}
	}
	return strings.HasPrefix(path, "test/") || strings.HasPrefix(path, "tests/")
}

// ResolveCoverage decides, for each entity, whether a test exercises it.
//
// Coverage is read off the graph rather than by running a search per entity:
// an incoming dependency edge from a symbol that lives in a test file is direct
// evidence that a test reaches this code, and the edges are already loaded. A
// per-entity search would be more thorough and would cost seconds each, which
// on an agent-sized change set is minutes.
//
// hasTests says whether the repository contains any test files at all. When it
// does not, every entity is NoResolver rather than Unchecked: "this repo has no
// test tree we can read" is not the same claim as "this change is untested",
// and Gate must not convert one into the other.
func ResolveCoverage(entities []ChangedEntity, ix *Index, hasTests bool) {
	for i := range entities {
		entity := &entities[i]
		if !hasTests {
			entity.Coverage = NoResolver
			continue
		}
		// CI/CD configuration has no unit test to find, so "unchecked" would be
		// a category error rather than a finding. See IsPipelineKind.
		if IsPipelineKind(entity.Kind) {
			entity.Coverage = NoResolver
			continue
		}
		// A region the graph could not analyse cannot be searched for tests
		// either. The edges that would prove a test reaches this code are the
		// same edges the parser failed to produce, so reporting Unchecked here
		// would charge the author for Gate's blind spot — the coverage-side
		// twin of the dependent count that was reported as a confident zero.
		if _, partial := ix.PartialReason(entity.Path); partial {
			entity.Coverage = NoResolver
			continue
		}
		ids := ix.Resolve(entity.Name, entity.Path)
		if len(ids) == 0 {
			// The diff named an entity the graph does not know — an
			// unsupported language, or a file that failed to parse. Absence of
			// a symbol is absence of evidence, not evidence of absence.
			entity.Coverage = NoResolver
			continue
		}
		entity.Coverage = Unchecked
		for _, test := range ix.Dependents(ids, 1) {
			if IsTestPath(test.Path) {
				entity.Coverage = Verified
				entity.CoveringTests = append(entity.CoveringTests, test.Name)
			}
		}
		sort.Strings(entity.CoveringTests)
	}
}
