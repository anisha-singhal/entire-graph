# Gate — Buildathon 2026 submission

**Track 2: Build with Graph Intelligence** · Bengaluru Tech Week
Fork: `anisha-singhal/entire-graph` · Entire mirror region: `aws-ap-south-1` (India)

---

> ## Judges — start here
>
> ```sh
> git clone https://github.com/anisha-singhal/entire-graph.git
> cd entire-graph
> go build -o entire-graph ./cmd/entire-graph      # needs CGO (tree-sitter)
> ./entire-graph gate --repo . --base HEAD~3 --head HEAD
> ```
>
> The submission is on the default branch, **`main`**. The `gate` branch is an
> identical mirror of the same commit and is kept only as a fallback; either
> clone works.
>
> This is a fork of `entireio/entire-graph`. Everything from `3a2a715` down is
> upstream; every commit above it is this submission.

---

## 1. One sentence

`entire graph gate` turns an agent-written pull request into a ranked reading order and a
keep / continue / revert verdict, using only evidence the agent did not produce.

## 2. The problem

A human PR is three files because a human got tired. An agent PR is forty files because the
agent did not. Today the reviewer has two options: read all forty — which destroys the reason
the agent was used — or skim and approve, which is how regressions ship.

Git tells you *which lines* changed. It does not tell you what those lines can break, which of
them anyone verified, or which forty-first file everybody forgot.

**Gate is the third option.** It reads the entity-level semantic diff, computes what
structurally depends on the change, resolves which tests actually cover it, and emits a verdict,
an exit code, and a ranked list of which four of the forty entities deserve your eyes.

> **Pitch:** Your agent wrote a 47-file PR. Gate tells you which 4 to read, and what nobody checked.

## 3. The moat — evidence, never assertion

| Signal | Evidence source | Can the agent fake it? |
|---|---|---|
| Blast radius | code graph — `CALLS`, `USES_TYPE`, `PARAM_TYPE`, `RETURNS_TYPE`, `READS_FIELD`, `EXTENDS` … | No. The callers predate the change and were written by other people. |
| Coverage / unchecked | the repository's own test tree | Only by writing a test that asserts nothing — which is why `UNASSERTED` is on the roadmap. |
| Companion gap *(roadmap)* | git history — `FILE_CHANGES_WITH` | No. History predates the session entirely. |
| Clone drift *(roadmap)* | `SIMILAR_TO` — MinHash near-duplicate bodies | No. |

**Gate never asks the agent what it did.** An agent cannot make Gate say `keep` by describing its
work well, because Gate never reads the description. That is also why the tool can block a push:
a reviewer that changes its mind is not a gate.

The second half of the idea is **absence**. Every other review tool reports what it found. Gate
reports what nobody looked at — where "nobody" means the agent, the developer, and the test
suite. `unchecked` is reported as its own count and is never folded into "verified". A gate with
no test is not passing.

## 4. Run it

```sh
go build -o entire-graph ./cmd/entire-graph        # needs CGO (tree-sitter)

./entire-graph gate --repo . --base HEAD~3 --head HEAD     # verdict + review order
./entire-graph gate --repo . --checkpoint <id>             # gate an Entire checkpoint
./entire-graph gate --repo . --base HEAD~3 --head HEAD --json
./entire-graph gate --help
```

Exit codes: `0` keep · `1` continue · `2` revert · `5` unusable (insufficient evidence).

Tests — no CGO, no git, no toolchain needed:

```sh
go test ./internal/gate/ -cover                    # 56 tests, 84.2% of statements
GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test -timeout 30m ./...
```

## 5. Architecture

One impure layer, five pure ones.

| Layer | File | Pure? |
|---|---|---|
| collect | `internal/cli/gate.go` | **No** — the only layer that reads git, the snapshot, or the filesystem |
| index | `internal/gate/index.go` | yes |
| signals | `internal/gate/risk.go`, `internal/gate/coverage.go` | yes |
| verdict | `internal/gate/verdict.go` | yes |
| render | `internal/gate/render.go` | yes |
| contract | `internal/gate/types.go` | yes |

`internal/gate` deliberately does **not** import `internal/sem` — that would pull in tree-sitter
and CGO. It defines its own `Symbol` and `Relation`; `collect` projects the real records onto
them. This is why the whole signal/verdict/render surface is testable in sub-second synthetic
fixtures, and why a new evidence source is an additive file rather than a rewrite.

A missing dimension can never *raise* a verdict — degradation is a pinned behaviour, not a
convention.

## 6. Built · not built

**Built** — `--base/--head` and `--checkpoint` entry points; reverse-dependency walk over 12 edge
types at depth ≤ 2 (`--hops 1|2`); coverage resolution into verified / unchecked / no-resolver;
review order (dependents descending, ties broken unchecked-first); text and `--json` renderers;
`--all`; exit codes; monotone degradation; `gate --help`.

Added by the Track 2 revision (§7b): three evidence tiers — confirmed / heuristic `~` /
unresolvable `?` — carried on every relation, every dependent count and every finding, with only
confirmed evidence permitted to raise a verdict; per-entity tier composition (`232 dependents, 180
proven`) rather than a single label; **entry-point detection** over `HANDLES_ROUTE` and
`HANDLES_TOOL`, so a decorator-registered handler reports a floor instead of a confident zero;
a `verify:` fallback command on every claim Gate cannot stand behind; and scope filtering, so
documentation entities and inert data files are excluded from analysis and the exclusion is
reported rather than performed silently.

**Not built** — companion gap (`SIMILAR_TO`) and clone drift (`FILE_CHANGES_WITH`), both cut for
time although the edges are already loaded in `collect`; the `UNASSERTED` third coverage state;
the pre-push hook; `--explain <n>` to print the dependency chain.

## 7. What running Gate on ourselves found

None of this is synthetic. Every item below was surfaced by pointing Gate at this repository.

- **A correctness bug in Gate's own edge selection.** `DATA_FLOWS` encodes *data* direction, not
  *dependency* direction — reversed for a "who depends on me" walk it reads as the exact inverse
  of the truth, and inflated every dependent count. Caught by reading Gate's output and not
  believing it: `newStatsCollector` was reported as having `unexpectedArgumentsError` as a
  dependent, and it does not call it. Removing `DATA_FLOWS` took `runStats` from 156 dependents
  to 130. The lesson generalises: **an edge type's direction is not automatically its dependency
  direction** — check the `reason` field before adding a relation to `dependencyRelations`.
- **Two determinism bugs in Gate itself** — parallel-snapshot edge order (now sorted at index
  build) and a Go map iteration in `verifyCommand` (now an ordered slice).
- **One nondeterminism in the upstream provider** — see §8.
- **A real finding in this repository's own history:** the `stats` refactor changed
  `statsCollector`, `sessionAcc` and `runStats` — functions and types with substantial fan-out —
  and left them `unchecked`. Verdict: `revert`.
- **Every method and field in the change set was being reported as invisible.** The semantic diff
  names methods qualified (`ExitCodeError.Error`), the graph indexed them bare (`Error`), so
  `Resolve` matched neither — and Gate reported the mismatch as a region the graph could not
  analyse. 41 methods and 135 fields on this repository, and after the fix `no-resolver` fell
  **221 → 35** and unresolvable entities **259 → 37**. This is the curveball's failure running
  backwards: claiming blindness about code the graph holds in full. It costs exactly as much
  trust as over-claiming, because a marker that fires wrongly is a marker readers learn to skip.
- **Gate was computing the blast radius of its own documentation.** 66 of 583 entities were
  Markdown headings and fenced code blocks from `PLAN.md`, each one duly reported as a region the
  graph could not resolve. Prose entities now leave the change set before any signal runs, and
  the count is printed.
- **89 of 94 reported blind spots could not have hidden anything** — `.md`, `.json`, `.toml`,
  `.gitignore`, plus four of Gate's own saved findings flagged `E_MINIFIED`. The five that
  mattered, real parse failures in vendored tree-sitter grammars, were buried under
  `... and 86 more`. The listing is now 5 + 7, grouped as *failed to parse* above *never analysed
  for relations*, and Vue, Svelte and HTML are deliberately kept in the second group because they
  embed code and genuinely can hide a caller.
- **The evidence tier committed the Track 2 failure inside the type built to prevent it — three
  times.** `Coverage()` never set a tier, and the zero value was treated as trustworthy in three
  separate places:

  | Where | What the unset tier did |
  |---|---|
  | `EvidenceTier.Trusted()` | answered `true` for the zero value, so unstated evidence could accuse |
  | `tierLabel()` | had no arm for `""`; it fell through the default and printed `[confirmed]` |
  | `EvidenceTier.Marker()` | returned no sigil, so the line read as confirmed at a glance |

  Every one of them let a caller who forgot to state a tier inherit full confidence. That is
  precisely the defect the tiers exist to prevent — silence rendered as assurance — reproduced
  inside the abstraction introduced to fix it, and it survived the curveball implementation
  because we only ever tested tiers that were set. All three now fail closed: `Trusted()` is
  deleted, `tierLabel` returns `untiered — treat as unverified`, and `Marker()` returns `!`.
  Pinned by `TestUnsetTierIsMarkedNotSilent` and
  `TestAnUnsetTierNeverRendersAsConfirmed`.

  The general lesson is worth more than the three fixes: **a default arm is a confidence
  assignment.** In a tool whose claim is calibrated confidence, every `switch` that falls through
  to the trusting answer is a silent over-claim waiting for a caller who forgets.

Saved evidence lives in [`docs/graph-findings/`](docs/graph-findings/). Note that
`pre-noon-gate-on-self.txt` was captured **before** the `DATA_FLOWS` fix and therefore still
contains the inflated counts — it is kept deliberately, as the artifact the bug was found in.

## 7b. The Noon Curveball — "Graph is evidence, not an oracle"

**The assumption it invalidated, in one sentence:** Gate assumed a graph edge is
a fact and the absence of an edge is the absence of a dependency.

The second half is the dangerous one. Code reached only through dynamic
dispatch, reflection or generated sources resolves to **zero dependents** — and
zero dependents is exactly the condition under which Gate stayed quiet and
returned `keep`. A tool whose entire pitch is *"we report what nobody checked"*
was silently converting *"we could not see"* into *"there is nothing there"*. It
was most confident where the graph was blindest.

§8 already disclosed "dependent counts are heuristic" in prose. That is the
point the curveball makes: a limitation admitted in a README but absent from the
data model is not a disclosure — it is a footnote under a number the tool still
prints with full authority.

**Where the assumption physically lived** — found with `entire graph search` and
`entire graph impact`, *before* editing (saved in `docs/graph-findings/`). The
provider was never the problem: `sem.RelationRecord` already carries
`Confidence`, `Resolution`, `Reason` and `Evidence[]`. Gate threw all of it away
on one line, because the type it projected onto had nowhere to put it:

```go
internal/cli/gate.go:263
relations = append(relations, gate.Relation{FromID: r.FromID, ToID: r.ToID, Type: r.Type})

internal/gate/index.go:18
type Relation struct { FromID, ToID, Type string }   // Confidence, Resolution: dropped
```

Gate was *structurally incapable* of telling confirmed from heuristic evidence.

**Three claims, now distinguishable everywhere** — text, `--json`, and the
verdict logic:

| | means | may reach `revert`? |
|---|---|---|
| `confirmed` | the provider resolved it exactly | yes |
| `~ heuristic` | inferred — a name, shape or package match | no, caps at `continue` |
| `? unresolvable` | the graph could not look here at all | no, and always reported |

Counts are reported as a composition rather than a label, because a single label
throws away what a reviewer would act on:

```
 1. Run          @ internal/cli/root.go:43
    body_changed · 232 dependents (2 proven, 230 inferred) · verified
 ~sessionAcc @ internal/cli/stats.go:610   [heuristic — inferred, verify before acting]
    type signature_changed has 8 dependent(s) within 2 hop(s) — 6 proven, 2 inferred
      verify: entire graph neighbors --repo . --symbol sessionAcc --file internal/cli/stats.go --relation CALLS --direction in
```

`8 dependents (6 proven, 2 inferred)` is trustworthy. `232 dependents (2 proven,
230 inferred)` is a warning. The old output printed both as a bare number.

**Two calibration mistakes, both caught by running Gate on this repository:**

1. The first tier mapping keyed on `confidence >= 0.9`. Measured against the real
   snapshot, `exact` edges carry confidences of 1, 0.92, 0.85 **and 0.7** — the
   threshold demoted 6,737 genuinely exact edges and made nearly every finding
   read "inferred". The resolution *method* now decides the tier, confidence is
   only a floor. A tool that marks everything uncertain has not become more
   honest; it has moved the noise.
2. Reporting only the weakest edge crossed made every large blast radius
   "inferred", firing the marker on exactly the symbols that most need a
   trustworthy number. Hence the composition above.

**What stayed intact.** All 27 pre-curveball tests still pass; one fixture gained
an explicit `DependentsTier: Confirmed` and its assertion is unchanged. A fully
resolved repository renders exactly as before — bare counts, no markers, no
`EVIDENCE` section — pinned by `TestFullyResolvedRepositoryIsUnaffectedByTiering`,
`TestEvidenceSectionIsAbsentWhenAnalysisIsComplete` and
`TestACompleteAnalysisAddsNothingToTheReport`. Coverage went
**78.9% → 84.2%**; the gate suite went **27 → 56 tests**.

**Why it can be trusted.** The revision extends an invariant Gate already had.
`Decide` already refused to let a *missing dimension* raise a verdict; it now
refuses to let *unproven evidence* raise one. Same rule: Gate may only accuse on
the strength of what it can actually show. The originating failure is closed by
a test — an entity with invisible callers used to render `0 dependents` and reach
`keep`; it now renders `dependents unresolvable (graph could not see this
region)`, emits a finding with a `verify:` command, and caps at `continue`.

The incomplete-analysis fixture is `internal/gate/partial_fixture_test.go`:
dynamic dispatch (a name-matched edge), generated code (a file the parser
failed on), reflection (a symbol with *no* edges at all) and an inventory-only
template language.

## 8. Known limitations — disclosed, not hidden

- **Dependent counts and `CALLS` resolution are heuristic.** Dynamic dispatch, reflection and
  generated code are invisible to a static graph. Since the Track 2 revision (§7b) this is
  represented in the data model and the verdict rather than only admitted here: such regions are
  reported as `unresolvable`, never as a confident zero.
- **The tier mapping is calibrated against this repository's resolution distribution**, measured in
  `docs/graph-findings/curveball-resolution-distribution.txt`. A provider that renames or adds a
  resolution method degrades to `heuristic`, which is the safe direction, but the mapping would
  want re-measuring.
- **Output is not byte-identical across runs — text as well as `--json`.**
  `internal/sem/analyze.go:1250` iterates a Go map when reconciling renames, so `change_type`
  flips between `added` and `renamed` on a handful of entities, and a flip to a breaking type
  opens or closes a risk finding. Measured over five consecutive runs of
  `gate --base HEAD~3 --head HEAD`: the verdict (`revert`), the exit code, the entity count and
  every header count were **identical every time**; the risk section differed by one finding
  (17 diff lines of 173). So the decision reproduces and the report does not quite. The fix is
  three lines — sort the deleted keys before the rename loop — but lands in `internal/sem`, which
  this project's contributor rules place out of scope; the intended next step is to offer it
  upstream.
- **Removals and renames cannot reach `revert`.** Gate builds one snapshot, of `HEAD`, and a
  removed symbol is by definition absent from it — so `Resolve` returns nothing and the entity is
  tiered `unresolvable`. Measured on this repository: **19 of 19 renames and 27 of 28 removals**.
  Removal is the most breaking change type there is, and it is the one Gate is structurally
  blindest to. This is disclosed rather than fixed because `sem.ProviderSnapshotOptions` has no
  ref parameter, so resolving against the base tree is not a small change. The behaviour is at
  least honest: such entities are reported as unresolvable and force `continue`, never `keep`.
- **Interface-satisfaction entry points are still invisible.** Route and tool registrations are
  now recognised (§7b), but a Go type reached through an interface has no `HANDLES_ROUTE` edge.
  Measured on `gin-gonic/gin`: `Engine.ServeHTTP` — the function every single request in the
  framework passes through — resolves to **exactly one caller, a benchmark, at resolution
  `type_inferred`**. Every production caller reaches it through `http.Handler` and leaves no
  edge. Registration-shaped entry points are covered; dispatch-shaped ones are not.
- **Languages whose dynamic dispatch is a builtin are not detected.** Reflection detection keys
  on an `IMPORTS` edge — `importlib`/`inspect`/`pkgutil` in Python, `reflect`/`plugin` in Go, and
  the JVM, .NET and PHP equivalents. But JavaScript `eval` and bracket access, Ruby `send`, and
  PHP variable functions import nothing, so there is no edge to key on and Gate reports a
  confident zero. Closing it needs a body-level pattern the provider does not currently emit.
- **The scope filters are Gate's judgement, not the provider's.** Documentation kinds and inert
  languages are two hand-maintained lists in `internal/gate/changeset.go`. The capability report
  cannot substitute for them: Markdown, CSS, Vue and Svelte all advertise exactly
  `[CONTAINS, DEFINES]`, so it cannot separate a Vue component that embeds JavaScript from a
  CHANGELOG. Both lists fail toward disclosure — an unrecognised language is treated as able to
  hide a caller — and every exclusion is counted and printed in the report's `SCOPE` line.
- **Coverage is graph-derived, not search-derived.** The design called for a per-entity
  `SearchRepository` probe; at ~400 entities that costs minutes. It instead reads incoming
  dependency edges whose source lives in a test file — cheaper and still agent-independent, but
  it misses a test that exercises code without a resolved edge.
- **`TESTS` edges are unusable here** — 9 of 11,397 symbols on this repo, measured.
- **A test that asserts nothing still counts as covered.** `UNASSERTED` is designed, not built.
- **`verifyCommand` returns a whole-suite command**, not a narrow per-entity one.
- **`--profile full` costs ~23 s wall / 57 s CPU** on this repository, rebuilt on every run
  because Gate reads the working tree. Larger repos need the durable cache — warm it once with
  `entire graph index --repo . --head --profile full` before a batch of runs.
- **Verdicts are advisory.** Exit codes make them enforceable, but the evidence is heuristic and
  the tool says so in its own output.
- `dedupeByLocation` and `writeWarnings` are exercised end to end but have no direct unit tests,
  which is part of why package coverage sits at 84.2% rather than higher.

## 9. How the graph was used

Gate is a *consumer* of the `entire graph` provider, and the graph was also the primary
navigation tool while building it:

- `entire graph snapshot` — symbols, relations and file records feed `collect`.
- `entire graph diff` / `checkpoint` — the entity-level semantic diff Gate ranks.
- `entire graph capabilities` — feature-detection before trusting semantic relations
  (`docs/graph-findings/capabilities.json`).
- `entire graph search` / `impact` / `neighbors` — used to locate code during development instead
  of broad grep exploration; the orientation queries are saved under `docs/graph-findings/`.

## 10. AI agent usage disclosure

This project was built with substantial assistance from an AI coding agent (Claude Code, Anthropic
Claude Opus). The agent was used for implementation, test authoring, debugging and documentation
throughout; every design decision, the verdict semantics, and all commits were reviewed and
authorised by the human author, and the work is recorded as Entire checkpoints on this branch
(`entire checkpoint list`, `entire checkpoint explain <id>`).

Fittingly, Gate exists because agent-authored changes need evidence a human can check without
reading all of them — and the bug in §7 was found by refusing to trust the agent's own output.
