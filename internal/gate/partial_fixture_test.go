package gate

// The partial-analysis fixture: a repository the graph cannot fully resolve.
//
// The pre-curveball fixture (fixture_test.go) is a fully resolved call chain,
// and it stays exactly as it was — the curveball requires that fully resolved
// code keeps behaving identically. This is its counterpart, modelling the three
// ways static analysis goes blind in a real repository:
//
//	Dispatch  -> Handler        CALLS, resolution "heuristic"  (dynamic dispatch:
//	                             resolved by name, not proven)
//	Generated                    lives in a file the parser failed on
//	Reflected                    called only through reflection: NO edges at all,
//	                             which the graph reports as zero dependents
//	Template                     an inventory-only language: listed, never parsed
//
// Reflected is the important one. It has no incoming edges, so before the
// curveball Gate printed "0 dependents", found nothing to say, and let the
// verdict reach keep. The graph was not telling us the symbol is unused; it was
// telling us it could not look. Those must not render as the same claim.

const (
	idHandler   = "repo:Go:pkg/handler.go:function:Handler"
	idDispatch  = "repo:Go:pkg/dispatch.go:function:Dispatch"
	idGenerated = "repo:Go:pkg/api.gen.go:function:Generated"
	idReflected = "repo:Go:pkg/reflect.go:function:Reflected"
	idTemplate  = "repo:ERB:app/views/show.html.erb:symbol:Template"
	idCaller    = "repo:Go:pkg/caller.go:function:Caller"
	// idAdapter is reached only through the inferred edge, so a walk to it
	// crosses one proven and one inferred hop and nothing else.
	idAdapter = "repo:Go:pkg/adapter.go:function:Adapter"
)

// pathGenerated failed to parse; pathTemplate is an inventory-only language.
// Both are marked partial by the collect layer in a real run
// (see markPartialAnalysis in internal/cli/gate.go).
const (
	pathGenerated = "pkg/api.gen.go"
	pathTemplate  = "app/views/show.html.erb"
	pathReflected = "pkg/reflect.go"
)

func partialSymbols() []Symbol {
	return []Symbol{
		{ID: idHandler, Name: "Handler", Path: "pkg/handler.go", Line: 20, Kind: "function"},
		{ID: idDispatch, Name: "Dispatch", Path: "pkg/dispatch.go", Line: 9, Kind: "function"},
		{ID: idGenerated, Name: "Generated", Path: pathGenerated, Line: 4, Kind: "function"},
		{ID: idReflected, Name: "Reflected", Path: pathReflected, Line: 15, Kind: "function"},
		{ID: idTemplate, Name: "Template", Path: pathTemplate, Line: 1, Kind: "symbol"},
		{ID: idCaller, Name: "Caller", Path: "pkg/caller.go", Line: 30, Kind: "function"},
		{ID: idAdapter, Name: "Adapter", Path: "pkg/adapter.go", Line: 12, Kind: "function"},
	}
}

func partialRelations() []Relation {
	return []Relation{
		// Proven: an exact structural match, the provider's own top tier.
		{FromID: idCaller, ToID: idDispatch, Type: "CALLS",
			Confidence: 0.95, Resolution: "exact"},
		// Inferred: dynamic dispatch resolved by name. Probably right. Not proven.
		{FromID: idDispatch, ToID: idHandler, Type: "CALLS",
			Confidence: 0.8, Resolution: "name_only"},
		// Generated code: the file failed to parse, so this edge is all we have.
		{FromID: idGenerated, ToID: idHandler, Type: "CALLS",
			Confidence: 0.75, Resolution: "pattern"},
		// Adapter is reached only through dynamic dispatch: one inferred hop
		// on top of an otherwise exact chain.
		{FromID: idDispatch, ToID: idAdapter, Type: "CALLS",
			Confidence: 0.8, Resolution: "name_only"},
		// Reflected has NO incoming edges on purpose. That absence is the bug.
	}
}

// partialIndex builds the index and marks the regions the provider admitted it
// could not analyse, exactly as markPartialAnalysis does for a real snapshot.
func partialIndex() *Index {
	ix := NewIndex(partialSymbols(), partialRelations())
	ix.MarkPartial(pathGenerated, "E_PARSE_ERROR: relations from this file are absent")
	ix.MarkPartial(pathTemplate, "ERB is inventory-only: no relations are extracted from it")
	ix.MarkPartial(pathReflected, "reflective dispatch: call sites are not statically visible")
	return ix
}
