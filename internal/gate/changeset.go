package gate

// This file holds the two questions the collect layer has to answer about a
// file or an entity before any signal runs: is this thing code at all, and if
// the graph could not analyse it, could that silence be hiding a caller?
//
// Both live here rather than in the collect layer because both are judgements
// about evidence, and every other judgement about evidence in this package is
// pure and unit-tested. Putting them next to the CGO boundary would make them
// the only rules in Gate that could not be tested in milliseconds.

// proseKinds are entity kinds that describe a document's structure rather than
// code: a Markdown heading, a fenced block, a bullet.
//
// They have to be excluded, and the reason is specific rather than tidiness.
// Gate resolves every changed entity against the graph and reports the ones it
// cannot find as regions the graph could not analyse. A Markdown heading is
// never in the symbol graph, so a change set that touches a design document
// manufactures dozens of "unresolvable" entities — on this repository, 66 of
// 583, nearly all of them headings in Gate's own PLAN.md.
//
// That is not a cosmetic problem. The curveball's whole point is that the
// unresolvable marker means "the graph could not see here, so absence of a
// finding is not evidence of safety". A marker that fires on prose is a marker
// a reader learns to skip, and then it is not there on the day it fires on a
// Ruby template that really is hiding a caller. Precision in what we admit we
// cannot see is part of the claim.
//
// Prose entities are excluded from the change set, never silently: the collect
// layer reports how many were dropped and why.
// "document" is the whole-file entity the provider emits for a prose file, and
// it was found missing from this list by running Gate on pytest and scrapy:
// three and six of their changed entities respectively were .rst documents. One
// of them, doc/en/reference/plugin_list.rst, matched a HANDLES_TOOL edge and was
// reported as "registered as a tool handler: its callers are external
// invocations" — a reStructuredText page described as a live entry point. Prose
// left in the change set does not merely add noise; it collects claims.
var proseKinds = map[string]bool{
	"document":    true,
	"section":     true,
	"heading":     true,
	"code_fence":  true,
	"paragraph":   true,
	"list_item":   true,
	"blockquote":  true,
	"table":       true,
	"table_row":   true,
	"link":        true,
	"frontmatter": true,
}

// IsProseKind reports whether an entity kind describes prose structure rather
// than code, and so can carry no dependency relations in either direction.
//
// Configuration kinds such as "setting" are deliberately absent: a changed
// setting can genuinely break something, and dropping it would be exactly the
// silent narrowing this package exists to prevent. It stays in the change set
// and is simply reported as having no resolvable dependents, which is true.
func IsProseKind(kind string) bool { return proseKinds[kind] }

// PartitionProse splits a change set into the entities worth analysing and a
// count of the prose entities removed. The count is returned rather than
// discarded so the caller can disclose the narrowing instead of performing it
// quietly.
func PartitionProse(entities []ChangedEntity) (analysable []ChangedEntity, prose int) {
	analysable = make([]ChangedEntity, 0, len(entities))
	for _, e := range entities {
		if IsProseKind(e.Kind) {
			prose++
			continue
		}
		analysable = append(analysable, e)
	}
	return analysable, prose
}

// pipelineKinds are CI/CD entities — a workflow, one of its jobs, a step.
//
// They are not prose and must stay in the change set: editing a release job can
// break a build just as surely as editing a function, and dropping them would
// be the silent narrowing PartitionProse is careful to avoid. What they cannot
// have is a unit test.
//
// Found by running Gate on gin-gonic/gin, where nine of ten changed entities
// were GitHub Actions workflows and jobs and every one was reported
// "unchecked: no test covers it" at tier confirmed. That is Gate stating, with
// full confidence, that a CI job has no unit test. True, and a category error:
// "unchecked" is supposed to mean somebody should have written a test and did
// not, and no one was ever going to unit-test jobs.trivy-scan.
//
// So coverage reports NoResolver for them — the state that already means "this
// dimension has no jurisdiction here" and is deliberately never rendered as a
// finding against the author. Risk still applies: a pipeline entity with
// dependents is still ranked and still reported.
var pipelineKinds = map[string]bool{
	"workflow": true,
	"job":      true,
	"step":     true,
	"stage":    true,
	"pipeline": true,
}

// IsPipelineKind reports whether an entity is CI/CD configuration, for which
// "no test covers this" is a category error rather than a finding.
//
// "setting" is deliberately absent. A configuration value can be exercised by a
// test — repositories do test their settings — so the coverage question is
// well-formed there even when the answer is usually no.
func IsPipelineKind(kind string) bool { return pipelineKinds[kind] }

// inertLanguages are languages whose files cannot invoke code at all: prose,
// data, stylesheet and diff formats.
//
// This exists because of what the provider's capability report cannot say. The
// obvious rule — "mark a file as a blind spot when its language supports none
// of the relations the blast-radius walk follows" — was measured against this
// repository and is wrong. Markdown, CSS, Patch, Git Ignore, HTML, Vue and
// Svelte all advertise exactly [CONTAINS, DEFINES], so the capability report
// cannot separate a Vue single-file component, which embeds JavaScript and can
// absolutely hide a caller, from a CHANGELOG that cannot hide anything. The
// missing relation support is not the signal; for the template languages it is
// the symptom.
//
// So the classification is Gate's, and it is written as a denylist on purpose.
// An unrecognised language falls through as capable of hiding a caller, which
// costs one extra line in a report. An allowlist would fail the other way: a
// template language nobody thought of would be silently certified as clean,
// which is the precise failure the Track 2 curveball named. When a rule has to
// guess, it should guess toward disclosure.
var inertLanguages = map[string]bool{
	"Markdown": true, "MDX": true, "reStructuredText": true, "AsciiDoc": true,
	"Org": true, "Plain Text": true, "Text": true, "License": true,

	"JSON": true, "JSON5": true, "JSONC": true, "JSON Lines": true,
	"YAML": true, "TOML": true, "INI": true, "CSV": true, "TSV": true,
	"Properties": true, "Lock": true, "EditorConfig": true,
	"Pip Requirements": true, "Requirements": true,

	"CSS": true, "SCSS": true, "Sass": true, "Less": true, "SVG": true,

	"Patch": true, "Diff": true,
	"Git Ignore": true, "Git Attributes": true, "Git Config": true,
}

// CanHideACaller reports whether silence about a file in this language might be
// concealing a dependency, and so is worth reporting as a blind spot.
//
// The empty language is treated as capable: an unclassified file is exactly the
// case where a confident silence is least defensible.
func CanHideACaller(language string) bool { return !inertLanguages[language] }
