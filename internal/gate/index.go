package gate

import (
	"fmt"
	"sort"
	"strings"
)

// Symbol and Relation are this package's own view of the graph records. They
// deliberately do not reuse sem.SymbolRecord and sem.RelationRecord: importing
// sem pulls in the tree-sitter bindings and with them CGO, and the point of
// this package is that its tests run without either. The collect layer projects
// the real records onto these.
type Symbol struct {
	ID   string
	Name string
	Path string
	Line int
	Kind string
	// QualifiedName is the container-scoped name, e.g. "Router.ServeHTTP".
	// The graph keys symbols by bare name while the semantic diff reports
	// methods and fields qualified, so an index that knows only one of the two
	// cannot resolve the other. Empty for symbols with no container.
	QualifiedName string
}

type Relation struct {
	FromID string
	ToID   string
	Type   string
	// Confidence and Resolution are the provider's own statement about how
	// firmly it resolved this edge (sem.RelationRecord carries both). Gate
	// originally dropped them at the projection boundary, which is precisely
	// why it could not tell a proven call from an inferred one. See
	// relationTier in evidence.go.
	Confidence float64
	Resolution string
}

// dependencyRelations are the edge types where "A -> B" means changing B can
// break A, so reversing them answers "who depends on this".
//
// Two families are deliberately absent.
//
// CONTAINS and DEFINES are structural, not behavioural: a file containing a
// symbol is not a caller of it, and counting them would make every symbol in a
// file a dependent of every other.
//
// DATA_FLOWS encodes the direction data travels, which is not the direction
// dependency travels. The provider emits `resolveRepo -> runStats` with the
// reason "callee return value assigned to local and returned by caller" — the
// callee pointing at its caller. Reversed, that reads as "resolveRepo depends
// on runStats", the exact inverse of the truth, and it put unrelated callees
// into the dependent list of anything that returned their values.
var dependencyRelations = map[string]bool{
	"CALLS":        true,
	"ASYNC_CALLS":  true,
	"CONSTRUCTS":   true,
	"USES_TYPE":    true,
	"PARAM_TYPE":   true,
	"RETURNS_TYPE": true,
	"READS_FIELD":  true,
	"WRITES_FIELD": true,
	"EXTENDS":      true,
	"IMPLEMENTS":   true,
	"OVERRIDES":    true,
	"INHERITS":     true,
}

// entryPointRelations mark a symbol as invoked from outside the repository
// altogether. A symbol that handles a route is called by HTTP requests, not by
// any call site a parser can find, so its incoming CALLS count is structurally
// meaningless: it will be zero no matter how heavily the endpoint is used.
//
// This is the case the Track 2 curveball is really about, and it is far more
// common than a parse failure. Found by running Gate on pallets/flask, where a
// signature change to a decorator-registered login view was reported as
// "0 dependents" at tier confirmed — Gate at its most certain about the most
// externally-visible code in the application. The graph had the evidence the
// whole time (HANDLES_ROUTE, 509 of them in that repository); Gate simply never
// looked at it.
var entryPointRelations = map[string]string{
	"HANDLES_ROUTE": "registered as a route handler",
	"HANDLES_TOOL":  "registered as a tool handler",
}

// dynamicDispatchImports name the facilities that let a program call a symbol
// without any call site a parser can see. A file that imports one of these can
// invoke its own package's symbols by name at runtime, so the absence of an
// incoming CALLS edge there proves nothing.
//
// This is a heuristic and it is deliberately file-scoped: reflective dispatch
// in one file can in principle reach anything, but marking the whole repository
// unresolvable would make the signal useless. In practice the dispatcher and
// its targets sit together — flask's cli.py discovering app objects, and the
// Go pattern below — so the file is the honest unit. It fails toward
// disclosure: a false "we could not see" costs a reader one check, while a
// false "0 dependents" is what this whole revision exists to prevent.
//
// Found by regression: the demo repository's reflectively dispatched
// Handlers.Refund was caught only by accident, because its qualified name did
// not resolve. Fixing that name lookup made it resolve cleanly and report a
// confident zero — the bug returning through the door the fix opened.
var dynamicDispatchImports = map[string]string{
	"reflect":                 "runtime reflection",
	"importlib":               "dynamic import by name",
	"inspect":                 "runtime introspection",
	"pkgutil":                 "runtime package walking",
	"java.lang.reflect":       "runtime reflection",
	"java.util.ServiceLoader": "runtime service loading",
	"System.Reflection":       "runtime reflection",
	"ReflectionClass":         "runtime reflection",
	"ReflectionMethod":        "runtime reflection",
}

// Two rules keep this map honest, both learned from a false positive on
// pytest-dev/pytest.
//
// First, the descriptions name what the facility does, never which language it
// belongs to. An earlier version mapped "plugin" to "Go plugin loading", and a
// Python file importing a module of that name was duly reported as using Go
// plugin loading — a confident, specific, wrong claim in a tool whose entire
// subject is unwarranted confidence.
//
// Second, and the reason that entry is gone: an import is matched by bare
// module name, with no language attached, so a name common across ecosystems
// will collide. "plugin" is an ordinary module name in many languages;
// "importlib" and "java.lang.reflect" are not. Only distinctive names belong
// here, and a new entry has to earn its place by being unlikely to name
// something ordinary in another language.

// KNOWN GAP, disclosed rather than papered over: this rule can only fire where
// dynamic dispatch arrives through an import, because IMPORTS is the edge the
// graph gives us. Languages whose dispatch is a builtin -- JavaScript eval and
// bracket access, Ruby send, PHP variable functions -- import nothing, so no
// edge exists to key on and Gate will report a confident zero there.
//
// That is the wrong direction to fail in, and it is not fixable from the
// relation stream alone; it needs a body-level pattern the provider does not
// currently emit. It is recorded in BUILDATHON.md section 8 as a blind spot
// rather than left for a reader to discover.

// Index answers "who depends on this symbol" by holding the dependency edges
// reversed. Building it once per run costs one pass; asking the question
// without it costs a full scan per changed entity.
// dependencyEdge is one reversed dependency arc together with how far it may
// be trusted. Storing the tier on the edge rather than recomputing it lets a
// multi-hop walk report the weakest step it crossed.
type dependencyEdge struct {
	From string
	Tier EvidenceTier
}

type Index struct {
	symbols  map[string]Symbol
	incoming map[string][]dependencyEdge
	// partial holds paths whose analysis the provider could not complete —
	// parse failures, inventory-only languages, dynamic dispatch. A symbol
	// defined in one of these is Unresolvable no matter what edges exist,
	// because the edges that would contradict it are the ones we cannot see.
	partial map[string]string
	// entryPoints maps a symbol to why it is externally invoked. Presence here
	// means a dependent count is a floor, never a total.
	entryPoints map[string]string
	// byName resolves a bare symbol name to its definitions, because the
	// semantic diff reports entity names while the graph is keyed by
	// compound-v1 IDs.
	byName map[string][]string
}

func NewIndex(symbols []Symbol, relations []Relation) *Index {
	ix := &Index{
		symbols:     make(map[string]Symbol, len(symbols)),
		incoming:    make(map[string][]dependencyEdge),
		partial:     make(map[string]string),
		entryPoints: make(map[string]string),
		byName:      make(map[string][]string),
	}
	for _, s := range symbols {
		ix.symbols[s.ID] = s
		ix.byName[s.Name] = append(ix.byName[s.Name], s.ID)
		// Register the qualified name too. Without it every method and field
		// looked absent from the graph: the diff says "Router.ServeHTTP", the
		// graph says "ServeHTTP", and Gate reported the mismatch as a region it
		// could not analyse. That is the Track 2 failure running backwards —
		// claiming blindness about code the graph sees perfectly well — and it
		// erodes the unresolvable marker just as badly as over-claiming does.
		if s.QualifiedName != "" && s.QualifiedName != s.Name {
			ix.byName[s.QualifiedName] = append(ix.byName[s.QualifiedName], s.ID)
		}
	}
	for _, r := range relations {
		if r.Type == "IMPORTS" {
			if facility, ok := dynamicDispatchImports[externalTargetName(r.ToID)]; ok {
				if path := fileIDPath(r.FromID); path != "" {
					// Tagged so the renderer can bucket this apart from a parse
					// failure. The distinction is the curveball stated exactly:
					// a parse failure means the graph could not read the code,
					// an inventory-only file means it never tried, but runtime
					// dispatch means it read everything, understood it, and the
					// answer is still incomplete.
					ix.MarkPartial(path, fmt.Sprintf(
						"%s this file imports %s (%s), so its symbols can be invoked by name at runtime and an absent call edge is not evidence of an absent caller",
						RuntimeDispatchPrefix, externalTargetName(r.ToID), facility))
				}
			}
		}
		if reason, ok := entryPointRelations[r.Type]; ok {
			// The handler is the source of the relation; the target names the
			// route or tool it answers to, which is worth showing.
			detail := reason
			if target := externalTargetName(r.ToID); target != "" {
				detail = reason + " (" + target + ")"
			}
			ix.entryPoints[r.FromID] = detail
		}
		if dependencyRelations[r.Type] {
			ix.incoming[r.ToID] = append(ix.incoming[r.ToID], dependencyEdge{From: r.FromID, Tier: relationTier(r)})
		}
	}
	// The snapshot builds relations in parallel, so the slice arrives in
	// whatever order the workers finished. Sorting the adjacency once here is
	// what makes every downstream walk reproducible: without it two runs of the
	// same commit emit different bytes, and a verdict nobody can reproduce is
	// not a gate.
	for id := range ix.incoming {
		edges := ix.incoming[id]
		sort.Slice(edges, func(i, j int) bool { return edges[i].From < edges[j].From })
	}
	for name := range ix.byName {
		sort.Strings(ix.byName[name])
	}
	return ix
}

// externalTargetName pulls the readable tail off an external endpoint id such
// as "external:route:/login". Returns empty for anything else.
func externalTargetName(id string) string {
	const prefix = "external:"
	if !strings.HasPrefix(id, prefix) {
		return ""
	}
	rest := id[len(prefix):]
	if i := strings.Index(rest, ":"); i >= 0 && i+1 < len(rest) {
		return rest[i+1:]
	}
	return rest
}

// fileIDPath pulls the repository-relative path out of a file record id such as
// "local/repo:file:pkg/refund.go". Returns empty for anything else.
func fileIDPath(id string) string {
	const marker = ":file:"
	if i := strings.Index(id, marker); i >= 0 {
		return id[i+len(marker):]
	}
	return ""
}

// EntryPointReason reports whether any of these symbols is invoked from outside
// the repository, and why. A dependent count for such a symbol is a floor: the
// callers exist, they are just not call sites.
func (ix *Index) EntryPointReason(ids []string) (string, bool) {
	for _, id := range ids {
		if reason, ok := ix.entryPoints[id]; ok {
			return reason, true
		}
	}
	return "", false
}

// Resolve maps an entity name from the semantic diff onto graph symbol IDs.
// A name defined in several files returns several IDs; path narrows it to the
// file the change was reported in, which is the common case and the only one
// where a dependent count is meaningful.
func (ix *Index) Resolve(name, path string) []string {
	candidates := ix.byName[name]
	if len(candidates) <= 1 || path == "" {
		return candidates
	}
	var inFile []string
	for _, id := range candidates {
		if ix.symbols[id].Path == path {
			inFile = append(inFile, id)
		}
	}
	if len(inFile) > 0 {
		return inFile
	}
	return candidates
}

// Dependents walks incoming dependency edges up to hops levels and returns
// every symbol found, in one total order (see sortSymbols) rather than by
// distance: the count is what the verdict uses, and a stable listing is what
// makes two runs comparable. The starting symbols are never in their own result.
//
// hops is capped by the caller (see risk.go). Without a cap, one utility
// function pulls in most of the repository and the report becomes noise.
func (ix *Index) Dependents(ids []string, hops int) []Symbol {
	found, _ := ix.DependentsWithTier(ids, hops)
	return found
}

// MarkPartial records that analysis of a path is incomplete, with a reason a
// reader can act on. Called by the collect layer once it knows which files the
// provider failed to parse, which languages are inventory-only, and which
// regions use dispatch a static parser cannot follow.
//
// This is separate from NewIndex on purpose: every existing caller keeps
// compiling and keeps its current behaviour, and a repository with nothing
// partial is byte-for-byte what it was before the curveball.
func (ix *Index) MarkPartial(path, reason string) {
	if path == "" {
		return
	}
	ix.partial[path] = reason
}

// PartialReason returns why analysis of a path is incomplete, and whether it is.
func (ix *Index) PartialReason(path string) (string, bool) {
	reason, ok := ix.partial[path]
	return reason, ok
}

// PartialPaths lists every path marked partial, sorted, with reasons aligned.
func (ix *Index) PartialPaths() (paths []string, reasons []string) {
	for p := range ix.partial {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		reasons = append(reasons, ix.partial[p])
	}
	return paths, reasons
}

// DependentsWithTier is Dependents plus the honest caveat: how far the returned
// count may be trusted.
//
// It reports a *composition*, not a single label, and that distinction was
// learned the hard way. Reporting only the weakest edge crossed made every
// large blast radius read "inferred": a walk over 232 dependents is nearly
// certain to cross one name-matched edge somewhere, so the marker fired on
// precisely the high-fan-out symbols that most need a trustworthy number, and a
// marker that always fires teaches the reader to ignore it.
//
// So each dependent carries the weakest tier on the path that reached it, and
// the caller learns "232 dependents, 180 of them proven". One proven dependent
// is enough to establish that a breaking change has dependents at all, which is
// what the verdict actually turns on.
//
// The subject's own region still overrides everything: if the graph could not
// analyse the file the symbol lives in, the edges that would contradict a low
// count are the ones we cannot see, and the whole count is a floor.
func (ix *Index) DependentsWithTier(ids []string, hops int) ([]Symbol, TierCounts) {
	counts := TierCounts{Region: Confirmed}
	if _, external := ix.EntryPointReason(ids); external {
		// Its real callers are HTTP requests, tool invocations, or whatever
		// registered it. None of them is a call site, so no walk can find them.
		counts.Region = Unresolvable
	}
	for _, id := range ids {
		if s, ok := ix.symbols[id]; ok {
			if _, partial := ix.partial[s.Path]; partial {
				counts.Region = Unresolvable
			}
		}
	}

	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		seen[id] = true
	}
	// tierTo[id] is the weakest tier on the path that first reached id. The
	// adjacency is sorted at build time, so "first reached" is deterministic.
	tierTo := make(map[string]EvidenceTier, len(ids))
	for _, id := range ids {
		tierTo[id] = Confirmed
	}

	var found []Symbol
	frontier := ids
	for hop := 0; hop < hops && len(frontier) > 0; hop++ {
		var next []string
		for _, id := range frontier {
			for _, edge := range ix.incoming[id] {
				if seen[edge.From] {
					continue
				}
				seen[edge.From] = true
				next = append(next, edge.From)

				tier := Weakest(tierTo[id], edge.Tier)
				// An unresolved endpoint has an ID but no definition record —
				// an external or unparsed target. It still counts as a
				// dependent, so synthesise enough of a Symbol to name it.
				symbol, ok := ix.symbols[edge.From]
				if !ok {
					// We know something points here, not what it is. That is
					// inference, not proof.
					tier = Weakest(tier, Heuristic)
					symbol = Symbol{ID: edge.From, Name: edge.From}
				} else if _, partial := ix.partial[symbol.Path]; partial {
					tier = Weakest(tier, Unresolvable)
				}
				tierTo[edge.From] = tier
				found = append(found, symbol)
			}
		}
		frontier = next
	}

	sortSymbols(found)
	found = dedupeByLocation(found)
	for _, s := range found {
		switch tierTo[s.ID] {
		case Confirmed:
			counts.ConfirmedCount++
		case Heuristic:
			counts.HeuristicCount++
		default:
			counts.UnresolvableCount++
		}
	}
	return found, counts
}

// dedupeByLocation collapses symbols that name the same place. A symbol can be
// reached through more than one compound-v1 ID — a name resolved in two files,
// or a re-declaration the parser records twice — and reporting it twice both
// inflates the dependent count and prints the same line under one finding.
func dedupeByLocation(symbols []Symbol) []Symbol {
	if len(symbols) < 2 {
		return symbols
	}
	out := symbols[:1]
	for _, s := range symbols[1:] {
		last := out[len(out)-1]
		if s.Name == last.Name && s.Path == last.Path && s.Line == last.Line {
			continue
		}
		out = append(out, s)
	}
	return out
}

// sortSymbols gives the result one total order — path, then name, then ID —
// so the same graph always yields the same list. ID is the final tiebreak
// because two symbols can share a name and a file (an overload, a generic
// instantiation) and only the compound-v1 ID separates them.
func sortSymbols(symbols []Symbol) {
	sort.Slice(symbols, func(i, j int) bool {
		a, b := symbols[i], symbols[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID < b.ID
	})
}
