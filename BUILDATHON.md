# Gate

**Buildathon 2026 · Bengaluru Tech Week · Track 2: Build with Graph Intelligence**
Fork: `anisha-singhal/entire-graph` · Submission branch: **`main`** · Entire mirror region: `aws-ap-south-1` (India)

> Judges: jump to [Setup, run and test instructions](#setup-run-and-test-instructions) for the
> three-command quickstart. This is a fork of `entireio/entire-graph`; everything from `3a2a715`
> down is upstream, every commit above it is this submission.

---

## One-sentence summary

`entire graph gate` turns an agent-written pull request into a ranked reading order and a
**keep / continue / revert** verdict, using only evidence the agent did not produce.

> Your agent wrote a 47-file PR. Gate tells you which 4 to read, and what nobody checked.

---

## Problem, intended user and why it matters

**Intended user:** a developer who has to decide whether an AI agent's checkpoint is safe to merge.

A human PR is three files because a human got tired. An agent PR is forty files because the agent
did not. That single asymmetry is the problem: code review was designed around a bottleneck —
human patience — that agents removed. The reviewer of an agent PR has exactly two options today:

1. **Read all forty files** — which destroys the reason the agent was used at all.
2. **Skim and approve** — which is how regressions ship.

**The gap.** Git tells you *which lines* changed. It does not tell you what those lines can break,
which of them anyone verified, or which forty-first file everybody forgot. Every existing tool in
this space either reads the diff (linters, static analysis, change-risk scores) or reads the
agent's own account of the diff (LLM reviewers). Nobody offers a third option.

**Gate is the third option.** It reads the entity-level semantic diff, computes what structurally
depends on the change, resolves which tests actually cover it, and emits a verdict, an exit code,
and — the part a reviewer actually uses — a ranked list of which four of the forty entities
deserve their eyes.

### Why it matters: the moat is evidence, never assertion

Every finding Gate emits comes from an artifact that existed before the agent ran.

| Signal | Evidence source | Can the agent fake it? |
|---|---|---|
| Blast radius | code graph — `CALLS`, `USES_TYPE`, `PARAM_TYPE`, `RETURNS_TYPE`, `READS_FIELD`, `EXTENDS` … | No. The callers predate the change and were written by other people. |
| Coverage / unchecked | the repository's own test tree | Only by writing a test that asserts nothing — which is why `UNASSERTED` is on the roadmap. |
| Entry points | `HANDLES_ROUTE`, `HANDLES_TOOL` registrations | No. |
| Companion gap *(roadmap)* | git history — `FILE_CHANGES_WITH` | No. History predates the session entirely. |
| Clone drift *(roadmap)* | `SIMILAR_TO` — MinHash near-duplicate bodies | No. |

**Gate never asks the agent what it did.** It never reads the commit message or the checkpoint
prose. An agent cannot make Gate say `keep` by describing its work well — which is also why the
tool can legitimately block a push: a reviewer that can be talked around is not a gate.

### The second half of the idea: absence

Every other review tool reports what it *found*. Gate reports **what nobody looked at** — where
"nobody" means the agent, the developer, and the test suite. `unchecked` is reported as its own
count and is never folded into "verified". **A gate with no test is not passing.**

### Why intent-checking was rejected

An earlier design led with *"did the agent do what it said?"*, comparing stated intent against the
entity diff. It was dropped deliberately, and the reasoning is recorded because it is the obvious
question to ask:

1. **It checks a claim against a claim.** The diff is ground truth, but the agent's sentence sets
   the scope of what counts as *explained* — so a confused agent widens its own permission.
2. **The mechanism is thin under inspection.** *"So if I write 'fix bug', everything is
   unexplained?"* Yes, it would be.
3. **It does nothing for human commits**, and nothing when the agent said nothing.
4. **There is no structured intent to read.** `sem.Result` carries only the checkpoint ID;
   `entire checkpoint explain` returns prose. A deterministic said-vs-did check has no input.

Provenance answers what intent could not, including *"what happens when agents get better?"* —
Gate still works, because it never depended on the agent in the first place.

---

## Selected Entire track and why Entire is essential

**Track 2: Build with Graph Intelligence.**

**The checkpoint is the input. The graph is the evidence engine. Remove either and there is no
product.**

- `entire graph gate --checkpoint <id>` resolves a checkpoint to a commit range through
  `gitutil.FindCommitWithCheckpoint` and `sem.AnalyzeCheckpoint`. The checkpoint is a **first-class
  input type**, not a way of naming a SHA.
- Every shipped signal is a graph traversal over relation records. Delete the graph and Gate has
  nothing to report.
- The graph's `Resolution` / `Confidence` / `Reason` metadata is what the entire evidence-tier
  system (see the curveball section) is built on. No other input in this system distinguishes
  *"resolved exactly"* from *"inferred from a name match"*.

Which graph APIs Gate consumes, and why:

| API | Used for |
|---|---|
| `entire graph snapshot` | symbols, relations and file records feed the `collect` layer |
| `entire graph diff` / `checkpoint` | the entity-level semantic diff Gate ranks |
| `entire graph capabilities` | feature detection before trusting semantic relations (`docs/graph-findings/capabilities.json`) |
| `entire graph search` / `impact` / `neighbors` | how we navigated this codebase while building, instead of broad grep — orientation queries saved under `docs/graph-findings/` |

Gate calls these **in process** rather than by shelling out, so one snapshot serves every signal.

This is not Entire bolted onto a product for the sake of the rules. It is the substrate.

---

## Architecture and main workflow

One impure layer, five pure ones. Data flows in one direction only.

| Layer | File | Responsibility | Pure? |
|---|---|---|---|
| collect | `internal/cli/gate.go` | the **only** layer that reads git, the snapshot or the filesystem; projects real records onto Gate's own types | **No** |
| index | `internal/gate/index.go` | reverse-dependency index; edges sorted at build for determinism | yes |
| signals | `internal/gate/risk.go`, `coverage.go` | blast radius over 12 edge types at depth ≤ 2; coverage into verified / unchecked / no-resolver | yes |
| verdict | `internal/gate/verdict.go` | one pure `Decide` taking per-dimension availability flags | yes |
| render | `internal/gate/render.go` | byte-budgeted text and `--json` | yes |
| contract | `internal/gate/types.go`, `changeset.go`, `evidence.go` | types, scope filters, evidence tiers | yes |

`internal/gate` deliberately does **not** import `internal/sem` — that would pull in tree-sitter
and CGO. It defines its own `Symbol` and `Relation`; `collect` projects the real records onto them.
This is why the whole signal/verdict/render surface is testable in sub-second synthetic fixtures
with no CGO, no git and no toolchain, and why a new evidence source is an additive file rather
than a rewrite. It is also why the noon curveball took two hours instead of a redesign.

### Main workflow

```
commit range or checkpoint id
        │
        ▼
  collect ──► one snapshot (--profile full) + entity-level semantic diff + git range
        │
        ▼
  changeset ──► scope filter: prose and inert data files leave here, and the count is printed
        │
        ▼
  index ──► reverse-dependency index over 12 relation types, edges sorted
        │
        ├──► risk      : dependents within ≤ 2 hops, each tiered confirmed / heuristic / unresolvable
        └──► coverage  : incoming dependency edges whose source lives in a test file
        │
        ▼
  verdict (Decide) ──► keep / continue / revert / unusable + per-dimension availability
        │
        ▼
  render ──► VERDICT · REVIEW ORDER · RISK · COVERAGE · EVIDENCE · SCOPE · RULE   (text or --json)
        │
        ▼
  exit code 0 / 1 / 2 / 5
```

### Verdict model — four states, and the rule is printed in every run

| Exit | Verdict | Meaning |
|---:|---|---|
| 0 | `keep` | Verified. Ship it. |
| 1 | `continue` | Mostly fine — check these specific things. |
| 2 | `revert` | Something is wrong. Roll back. |
| 5 | `unusable` | Gate ran, here is the full report, nothing downstream can build on it. |

`unusable` exists because when the graph cannot parse a file or the language has no test resolver,
the honest answer is *"we could not check"* — not `revert`, which is a false accusation.

```
RULE  revert   = a removed or signature-changed entity with ≥1 dependent AND no covering test
      continue = risky change with tests, OR new entities with no tests
      keep     = none of the above
      unusable = the graph could not produce evidence for the changed files

DEGRADATION  a dimension that did not run cannot produce a finding against you.
             coverage unavailable  -> no finding may reach revert; cap at continue
             risk unavailable      -> cap at continue
             both unavailable      -> unusable (exit 5)
```

**Monotone degradation is a pinned behaviour, not a convention.** `revert` requires *≥1 dependent
AND no covering test*. If the coverage dimension did not run, then *nothing* has a covering test
and every change with a dependent reads as `revert` — which is not strictness but a false
accusation produced by a missing input. So `Decide` takes an availability flag per dimension, and
**a missing dimension can never raise a verdict.**

### Review order — what a reviewer actually uses

The verdict decides *whether*. The review order decides *what to read*: sorted by **dependents
descending, ties broken unchecked-first**. Deliberately not a product — uncheckedness is binary, so
`dependents × unchecked` would zero every verified entity and drop a 9-dependent change below a
6-dependent one.

```
VERDICT  revert
         583 entities changed · 56 verified · 306 unchecked · 221 no-resolver
         PARTIAL ANALYSIS — 259 in regions the graph could not resolve, 157 resting on inferred
         edges. Counts below are floors, not totals; this report is not authoritative.
         HEAD~3..HEAD

REVIEW ORDER — 583 entities changed, read these 5 first

 1. Run       @ internal/cli/root.go:43    body_changed · 232 dependents (2 proven, 230 inferred) · verified
 2. runGate   @ internal/cli/gate.go:103   added · ~130 dependents (none proven) · unchecked
 3. runStats  @ internal/cli/stats.go:189  body_changed · ~130 dependents (none proven) · unchecked

 The remaining 578 entities: 303 still unchecked, none with dependents
 Full list: --all
```

That last line is what earns trust: **Gate is not hiding 578 things — it is saying why they do not
need your eyes.** Output is byte-budgeted per section with counts for what was elided, matching
the repository's existing `--max-context-bytes` discipline, because large PRs are the point.

### What is built

`--base/--head` and `--checkpoint` entry points; reverse-dependency walk over 12 edge types at
depth ≤ 2 (`--hops 1|2`); coverage resolution into verified / unchecked / no-resolver; review order;
text and `--json` renderers; `--all`; exit codes; monotone degradation; `gate --help`.

Added by the Track 2 revision: three evidence tiers carried on every relation, dependent count and
finding, with only confirmed evidence permitted to raise a verdict; per-entity tier composition
(`232 dependents, 180 proven`) rather than a single label; entry-point detection over
`HANDLES_ROUTE`/`HANDLES_TOOL`; a `verify:` fallback command on every claim Gate cannot stand
behind; and scope filtering with the exclusion reported rather than performed silently.

---

## Entire Graph findings and verification

None of this is synthetic. Every item below was surfaced by pointing Gate at this repository.
Saved evidence lives in [`docs/graph-findings/`](docs/graph-findings/).

### 1. A correctness bug in Gate's own edge selection

`DATA_FLOWS` encodes **data** direction, not **dependency** direction. Reversed for a "who depends
on me" walk it reads as the exact inverse of the truth, and it inflated every dependent count in
the tool. Caught by reading Gate's output and not believing it: `newStatsCollector` was reported as
having `unexpectedArgumentsError` as a dependent, and it does not call it. Removing `DATA_FLOWS`
took `runStats` from **156 dependents to 130**.

> The lesson generalises: **an edge type's direction is not automatically its dependency
> direction** — check the `reason` field before adding a relation to `dependencyRelations`.

### 2. Claiming blindness about code the graph held in full

The semantic diff names methods qualified (`ExitCodeError.Error`); the graph indexed them bare
(`Error`). So `Resolve` matched neither, and **every method and field in the change set was
reported as invisible** — 41 methods and 135 fields on this repository. After the fix,
`no-resolver` fell **221 → 35** and unresolvable entities **259 → 37**.

This is the curveball's failure running backwards, and it costs exactly as much trust as
over-claiming knowledge, **because a marker that fires wrongly is a marker readers learn to skip.**

### 3. Gate was computing the blast radius of its own documentation

66 of 583 entities were Markdown headings and fenced code blocks from `PLAN.md`, each duly reported
as a region the graph could not resolve. Prose entities now leave the change set before any signal
runs, and the count is printed.

### 4. 89 of 94 reported blind spots could not have hidden anything

`.md`, `.json`, `.toml`, `.gitignore`, plus four of Gate's own saved findings flagged `E_MINIFIED`.
The five that mattered — real parse failures in vendored tree-sitter grammars — were buried under
`... and 86 more`. The listing is now 5 + 7, grouped as *failed to parse* above *never analysed for
relations*, and Vue, Svelte and HTML are deliberately kept in the second group because they embed
code and genuinely can hide a caller.

### 5. Two determinism bugs in Gate, and one upstream

Parallel-snapshot edge order (now sorted at index build) and a Go map iteration in `verifyCommand`
(now an ordered slice). The third is upstream and disclosed rather than patched — see
[Known limitations](#known-limitations-and-next-steps).

### 6. A real finding in this repository's own history

The `stats` refactor changed `statsCollector`, `sessionAcc` and `runStats` — functions and types
with substantial fan-out — and left them `unchecked`. Verdict: **`revert`**. This is the demo, and
it is on our own history rather than a fixture.

### 7. The evidence tier committed the Track 2 failure inside the type built to prevent it

`Coverage()` never set a tier, and the zero value was treated as trustworthy in three places:

| Where | What the unset tier did |
|---|---|
| `EvidenceTier.Trusted()` | answered `true` for the zero value, so unstated evidence could accuse |
| `tierLabel()` | had no arm for `""`; fell through the default and printed `[confirmed]` |
| `EvidenceTier.Marker()` | returned no sigil, so the line read as confirmed at a glance |

Silence rendered as assurance — reproduced *inside the abstraction introduced to fix it* — and it
survived the curveball implementation because we only ever tested tiers that were set. All three
now fail closed: `Trusted()` is deleted, `tierLabel` returns `untiered — treat as unverified`, and
`Marker()` returns `!`. Pinned by `TestUnsetTierIsMarkedNotSilent` and
`TestAnUnsetTierNeverRendersAsConfirmed`.

> **A default arm is a confidence assignment.** In a tool whose claim is calibrated confidence,
> every `switch` that falls through to the trusting answer is a silent over-claim waiting for a
> caller who forgets.

### Verification

| What | Command | Result |
|---|---|---|
| Gate package suite | `go test ./internal/gate/ -count=1 -cover` | **62 tests, 87.7% of statements, 0 failures** |
| Full repository suite | `GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test -timeout 30m ./...` | **9 packages with tests, 0 failures, exit 0** |
| Pre-curveball tests preserved | included in the run above | **27/27 still passing**, one fixture gained an explicit `DependentsTier: Confirmed`, its assertion unchanged |
| No regression for resolved repos | `TestFullyResolvedRepositoryIsUnaffectedByTiering`, `TestEvidenceSectionIsAbsentWhenAnalysisIsComplete`, `TestACompleteAnalysisAddsNothingToTheReport` | a fully resolved repository renders exactly as before |
| Originating curveball failure closed | `internal/gate/partial_fixture_test.go` | dynamic dispatch, generated code, reflection and an inventory-only template language each render as unresolvable, not as zero |
| Tier mapping generality | `docs/graph-findings/curveball-cross-repo-generality.txt` | same seven resolution strings in entire-graph (Go), gorilla/mux (Go) and pallets/flask (Python) |

Coverage went **78.9% → 87.7%** and the gate suite **27 → 62 tests** across the curveball
revision. No test was deleted or weakened to make the build pass.

`docs/graph-findings/pre-noon-gate-on-self.txt` was captured **before** the `DATA_FLOWS` fix and
still contains the inflated counts. It is kept deliberately, as the artifact the bug was found in.

---

## Noon Curveball: what changed and how we adapted

### The constraint, verbatim

> **"Graph is evidence, not an oracle."**

### The assumption it invalidated, in one sentence

Gate assumed **a graph edge is a fact, and the absence of an edge is the absence of a dependency.**

The second half is the dangerous one. Code reached only through dynamic dispatch, reflection or
generated sources resolves to **zero dependents** — and zero dependents was exactly the condition
under which Gate stayed quiet and returned `keep`. A tool whose entire pitch is *"we report what
nobody checked"* was silently converting *"we could not see"* into *"there is nothing there."*
**It was most confident where the graph was blindest.**

Our own README already said *dependent counts are heuristic*. That is precisely the point the
curveball makes: **a limitation admitted in prose but absent from the data model is not a
disclosure — it is a footnote under a number the tool still prints with full authority.**

### Where the assumption physically lived

Located with `entire graph search` and `entire graph impact` **before editing** — queries saved in
`docs/graph-findings/curveball-search.txt` and `curveball-impact.txt`. The provider was never the
problem: `sem.RelationRecord` already carried `Confidence`, `Resolution`, `Reason` and
`Evidence[]`. Gate threw all of it away on one line, because the type it projected onto had
nowhere to put it:

```go
internal/cli/gate.go:263
relations = append(relations, gate.Relation{FromID: r.FromID, ToID: r.ToID, Type: r.Type})

internal/gate/index.go:18
type Relation struct { FromID, ToID, Type string }   // Confidence, Resolution: dropped
```

Gate was **structurally incapable** of telling confirmed evidence from heuristic evidence. Not a
flaw in the logic — a missing field.

### What changed: three claims, now distinguishable everywhere

In text, in `--json`, and in the verdict logic:

| | means | may reach `revert`? |
|---|---|---|
| `confirmed` | the provider resolved it exactly | yes |
| `~ heuristic` | inferred — a name, shape or package match | no, caps at `continue` |
| `? unresolvable` | the graph could not look here at all | no, and always reported |

Counts are reported as a **composition** rather than a label, because a single label throws away
what a reviewer would act on:

```
 1. Run          @ internal/cli/root.go:43
    body_changed · 232 dependents (2 proven, 230 inferred) · verified
 ~sessionAcc @ internal/cli/stats.go:610   [heuristic — inferred, verify before acting]
    type signature_changed has 8 dependent(s) within 2 hop(s) — 6 proven, 2 inferred
      verify: entire graph neighbors --repo . --symbol sessionAcc --file internal/cli/stats.go --relation CALLS --direction in
```

`8 dependents (6 proven, 2 inferred)` is trustworthy. `232 dependents (2 proven, 230 inferred)` is
a warning. **The old output printed both as a bare number.**

Alongside the tiers: **entry-point detection** over `HANDLES_ROUTE` and `HANDLES_TOOL`, so a
decorator-registered handler reports a floor instead of a confident zero; a `verify:` fallback
command on every claim Gate cannot stand behind; and **scope filtering**, so documentation entities
and inert data files are excluded from analysis and the exclusion is reported rather than performed
silently.

### Two calibration mistakes, both caught by running Gate on this repository

1. **Thresholding on confidence made everything look uncertain.** The first tier mapping keyed on
   `confidence >= 0.9`. Measured against the real snapshot, `exact` edges carry confidences of 1,
   0.92, 0.85 **and 0.7** — so the threshold demoted **6,737 genuinely exact edges** and made
   nearly every finding read "inferred". The resolution *method* now decides the tier; confidence
   is only a floor. *A tool that marks everything uncertain has not become more honest; it has
   moved the noise.*
2. **Reporting only the weakest edge crossed** made every large blast radius "inferred", firing the
   caution marker on exactly the symbols that most need a trustworthy number. Hence the
   composition format above.

### What stayed intact

- All **27 pre-curveball tests still pass.** One fixture gained an explicit
  `DependentsTier: Confirmed` and its assertion is unchanged.
- A fully resolved repository renders **exactly as before** — bare counts, no markers, no
  `EVIDENCE` section — pinned by `TestFullyResolvedRepositoryIsUnaffectedByTiering`,
  `TestEvidenceSectionIsAbsentWhenAnalysisIsComplete` and
  `TestACompleteAnalysisAddsNothingToTheReport`.
- Coverage **78.9% → 87.7%**; the gate suite **27 → 62 tests**.
- No test was removed or weakened to make the build green.

### Why the revised behaviour can be trusted

The revision **extends an invariant Gate already had.** `Decide` already refused to let a *missing
dimension* raise a verdict; it now refuses to let *unproven evidence* raise one. Same rule: **Gate
may only accuse on the strength of what it can actually show.**

The originating failure is closed by a test. An entity with invisible callers used to render
`0 dependents` and reach `keep`; it now renders `dependents unresolvable (graph could not see this
region)`, emits a finding with a `verify:` command, and caps at `continue`. The
incomplete-analysis fixture is `internal/gate/partial_fixture_test.go`: dynamic dispatch (a
name-matched edge), generated code (a file the parser failed on), reflection (a symbol with *no*
edges at all) and an inventory-only template language.

The response landed in the layers the architecture predicted it would — `contract`, `signals`,
`verdict`, `render` — and touched the impure `collect` layer only to stop discarding metadata it
was already receiving.

---

## Checkpoint links and what each checkpoint proves

Three checkpoints on `main`. Inspect any of them with `entire checkpoint explain <id>`, and see the
entity-level change with `entire graph checkpoint <id> --json`.

| # | Checkpoint ID | Commit | Time | What it proves |
|---|---|---|---|---|
| 1 | `5151049c3e83` | `7389982` — *stable: pre-noon Gate MVP (risk + coverage, verdict, review order, 27 tests)* | 11:57 IST | The pre-curveball product existed and ran: two signals, the four-state verdict, the review order, exit codes and 27 passing tests. This is the baseline any curveball claim is measured against, and the reason "what stayed intact" is checkable rather than asserted. |
| 2 | `02125c15cb88` | `a156102` — *curveball: graph is evidence, not an oracle* | 14:20 IST | The complete Track 2 response: three evidence tiers carried end-to-end, entry-point detection, `verify:` commands, scope filtering, and the tests that pin both the new behaviour and the unchanged behaviour. Diffing checkpoint 1 against checkpoint 2 is the entire "what changed" claim, at entity level. |
| 3 | `147cc8a1-70c` *(temporary)* | `ae835a4` — *carry forward: uncommitted session files* | 14:20 IST | Session continuity: the working files carried across the fresh post-noon session, evidencing the reconstruct-from-repository protocol rather than a pasted transcript. |

Two things worth noting for judging:

- **The curveball session was a fresh session.** The morning session was closed completely at
  noon and the post-noon work reconstructed intent from the repository, `PLAN.md`'s HANDOFF section
  and `entire checkpoint explain` — not from a pasted transcript. Checkpoint 3 is the artifact of
  that handoff.
- **Checkpoint 1 was verified to carry a valid trailer before any curveball work began**, because
  a commit without one is invisible to judging. Both `7389982` and `a156102` carry
  `Entire-Checkpoint:` trailers, confirmable with
  `git log --format='%h %(trailers:key=Entire-Checkpoint,valueonly=true)' 3a2a715..HEAD`.

---

## Setup, run and test instructions

### Quickstart

```sh
git clone https://github.com/anisha-singhal/entire-graph.git
cd entire-graph
go build -o entire-graph ./cmd/entire-graph      # needs CGO (tree-sitter)
./entire-graph gate --repo . --base HEAD~3 --head HEAD
```

The submission is on the default branch, **`main`**. The `gate` branch is an identical mirror of
the same commit, kept only as a fallback; either clone works.

### Running Gate

```sh
./entire-graph gate --repo . --base HEAD~3 --head HEAD     # verdict + review order
./entire-graph gate --repo . --checkpoint <id>             # gate an Entire checkpoint
./entire-graph gate --repo . --base HEAD~3 --head HEAD --json
./entire-graph gate --repo . --base HEAD~3 --head HEAD --all --hops 2
./entire-graph gate --help
```

Exit codes: `0` keep · `1` continue · `2` revert · `5` unusable (insufficient evidence).

Because Gate reads the working tree, `--profile full` is rebuilt on every run (~23 s wall on this
repository). Warm the durable cache once before a batch of runs:

```sh
entire graph index --repo . --head --profile full
```

### The evidence-tier demo

A throwaway two-commit repository where one function is called normally and one method is reached
**only** through `reflect` — the reflection case must render `? unresolvable`, never `0 dependents`:

```sh
scripts/gate-demo-repo.sh
```

### Tests

The gate package needs no CGO, no git and no toolchain:

```sh
go test ./internal/gate/ -count=1 -cover
  ->  ok  github.com/entireio/entire-graph/internal/gate  coverage: 87.7% of statements  (62 tests)

GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test -timeout 30m ./...
  ->  9 packages with tests, 0 failures, exit 0
```

---

## Databricks use, data sources and limitations

**We did not opt in to Databricks.** The decision is deliberate and recorded so it is legible:

1. **It contradicts the thesis.** Gate's entire claim is deterministic, local, no-egress, no model,
   no keys. A verdict you cannot reproduce offline cannot gate a push.
2. **The provider's contract forbids it.** `entire-graph` is a no-egress provider — no remote
   fetches, hosted API calls, telemetry or runtime downloads — and Gate ships inside it.
3. **Nothing in the product needs it.** Every signal is a traversal over records already on disk.

### Data sources

All local, all read-only, none of them produced by the agent under review:

| Source | Read via | Written? |
|---|---|---|
| The code graph (symbols, relations, file records) | `sem` provider snapshot, in process | no |
| The entity-level semantic diff | `sem.AnalyzeGitRange` / `AnalyzeCheckpoint` | no |
| Git history and commit ranges | `gitutil` subprocess | no |
| The repository's own test tree | graph relations whose source symbol lives in a test file | no |
| Entire checkpoints | `gitutil.FindCommitWithCheckpoint` | no |

Gate writes nothing except its own report to stdout. No network, no keys, no telemetry.

---

## Known limitations and next steps

Disclosed, not hidden. Several of these are measured on this repository, and the numbers are given.

### Limitations

- **Dependent counts and `CALLS` resolution are heuristic.** Dynamic dispatch, reflection and
  generated code are invisible to a static graph. Since the Track 2 revision this is represented in
  the data model and the verdict rather than only admitted here: such regions are reported as
  `unresolvable`, never as a confident zero.
- **The tier mapping is calibrated against this repository's resolution distribution**, measured in
  `docs/graph-findings/curveball-resolution-distribution.txt` and cross-checked on two other
  repositories in `curveball-cross-repo-generality.txt`. A provider that renames or adds a
  resolution method degrades to `heuristic` — the safe direction — but the mapping would want
  re-measuring.
- **Output is not byte-identical across runs — text as well as `--json`.**
  `internal/sem/analyze.go:1250` iterates a Go map when reconciling renames, so `change_type` flips
  between `added` and `renamed` on a handful of entities, and a flip to a breaking type opens or
  closes a risk finding. Measured over five consecutive runs of `gate --base HEAD~3 --head HEAD`:
  the verdict (`revert`), the exit code, the entity count and every header count were **identical
  every time**; the risk section differed by one finding (17 diff lines of 173). **So the decision
  reproduces and the report does not quite.** The fix is three lines — sort the deleted keys before
  the rename loop — but lands in `internal/sem`, which this project's contributor rules place out of
  scope; the intended next step is to offer it upstream.
- **Removals and renames cannot reach `revert`.** Gate builds one snapshot, of `HEAD`, and a removed
  symbol is by definition absent from it — so `Resolve` returns nothing and the entity is tiered
  `unresolvable`. Measured on this repository: **19 of 19 renames and 27 of 28 removals.** Removal
  is the most breaking change type there is, and it is the one Gate is structurally blindest to.
  Disclosed rather than fixed because `sem.ProviderSnapshotOptions` has no ref parameter, so
  resolving against the base tree is not a small change. The behaviour is at least honest: such
  entities force `continue`, never `keep`.
- **Interface-satisfaction entry points are still invisible.** Route and tool registrations are now
  recognised, but a Go type reached through an interface — `net/http` invoking `ServeHTTP` via
  `http.Handler` — has no `HANDLES_ROUTE` edge, so it still reports a confident zero.
  Registration-shaped entry points are covered; dispatch-shaped ones are not.
- **The scope filters are Gate's judgement, not the provider's.** Documentation kinds and inert
  languages are two hand-maintained lists in `internal/gate/changeset.go`. The capability report
  cannot substitute for them: Markdown, CSS, Vue and Svelte all advertise exactly
  `[CONTAINS, DEFINES]`, so it cannot separate a Vue component that embeds JavaScript from a
  CHANGELOG. Both lists **fail toward disclosure** — an unrecognised language is treated as able to
  hide a caller — and every exclusion is counted and printed in the report's `SCOPE` line.
- **Coverage is graph-derived, not search-derived.** The design called for a per-entity
  `SearchRepository` probe; at ~400 entities that costs minutes. It instead reads incoming
  dependency edges whose source lives in a test file — cheaper and still agent-independent, but it
  misses a test that exercises code without a resolved edge.
- **`TESTS` edges are unusable here** — 9 of 11,397 symbols on this repository, measured.
- **A test that asserts nothing still counts as covered.** `UNASSERTED` is designed, not built.
- **`verifyCommand` returns a whole-suite command**, not a narrow per-entity one.
- **`--profile full` costs ~23 s wall / 57 s CPU** on this repository, rebuilt on every run because
  Gate reads the working tree. Larger repos need the durable cache.
- **Verdicts are advisory.** Exit codes make them enforceable, but the evidence is heuristic and the
  tool says so in its own output.
- `dedupeByLocation` and `writeWarnings` are exercised end to end but have no direct unit tests.

### Not built

- **Companion gap** (`FILE_CHANGES_WITH`) and **clone drift** (`SIMILAR_TO`) — the two signals cut
  from the morning for time. The edges are already loaded in `collect` and the signals are
  independent by construction, so this is additive work rather than a redesign.
- **The `UNASSERTED` third coverage state** — designed, not built.
- **The pre-push hook** — ~5 lines of shell wrapping the exit code. The exit codes it needs exist
  and are tested.
- **`--explain <n>`** to print the dependency chain behind a finding.

### Next steps, in priority order

1. **Base-tree resolution for removals and renames** — the biggest correctness gap, and the one the
   limitations section is most blunt about.
2. **`UNASSERTED`** — "a test that asserts nothing counts as covered" is the one way an agent could
   actually game the coverage signal.
3. **Companion gap, then clone drift** — the two cut signals.
4. **Offer the three-line `internal/sem` determinism fix upstream**, so the report reproduces
   byte-for-byte and not only decision-for-decision.
5. **The pre-push hook and `gate audit`** — repo-wide "symbols with dependents and zero tests", no
   diff needed.
6. **`--explain <n>`** — the shortest path from "trust the number" to "check the number yourself".

---

## AI agent usage disclosure

This project was built with substantial assistance from an AI coding agent (Claude Code, Anthropic
Claude Opus). The agent was used for implementation, test authoring, debugging and documentation
throughout; every design decision, the verdict semantics, and all commits were reviewed and
authorised by the human author, and the work is recorded as Entire checkpoints on this branch
(`entire checkpoint list`, `entire checkpoint explain <id>`).

Fittingly, Gate exists because agent-authored changes need evidence a human can check without
reading all of them — and the `DATA_FLOWS` bug above was found by refusing to trust the agent's own
output.
