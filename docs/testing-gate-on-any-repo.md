# Testing Gate on any repository

A repo-agnostic procedure. Nothing here is specific to a language, framework or
project — Gate carries no framework knowledge, so the same five steps work
everywhere. Validated on Go, Python, TypeScript and Ruby repositories ranging
from 130 to 7,087 files.

---

## Step 0 — check the language is parsed semantically

Inventory-only languages get file and symbol records but **no relations**, so
the blast-radius dimension has nothing to walk and every entity comes back
unresolvable. Two seconds now saves a confusing report later.

```sh
entire-graph capabilities --json | python3 -c \
  "import json,sys; c=json.load(sys.stdin); print('Ruby' in c['semantic_languages'])"
```

Substitute the language you care about. 36 languages are semantic; the rest are
inventory-only.

## Step 1 — clone with history

```sh
git clone --depth 25 https://github.com/<owner>/<repo>.git
cd <repo>
git rev-parse --short HEAD        # write this down — pin it
```

**`--depth 1` will not work.** Gate diffs a commit range, and a shallow clone of
one commit has no `HEAD~3`. Depth 25 is enough for any range you will demo.

Record the SHA. Upstream `main` moves, and an unpinned demo silently changes
under you between rehearsal and presentation.

## Step 2 — run it

```sh
entire-graph gate --repo . --base HEAD~3 --head HEAD
```

Exit codes: `0` keep · `1` continue · `2` revert · `5` unusable.

Nothing to configure. No language flag, no framework hint, no ignore file.

## Step 3 — read the four things that matter

```
VERDICT  continue
         44 entities changed · 6 verified · 37 unchecked · 1 no-resolver
         PARTIAL ANALYSIS — 2 in regions the graph could not resolve, ...
```

1. **The header counts.** `unchecked` is the product: entities nobody tested.
   `no-resolver` means Gate could not determine coverage, which is a different
   claim and never folded into the first.
2. **REVIEW ORDER** — the ranked list. This is what a reviewer actually uses.
   On any healthy run these should be real source symbols, not documentation or
   CI config.
3. **The evidence markers.** A bare number is proven. `~` is inferred. `?` means
   the graph could not look, and the number is a floor.
4. **`where the graph could not see`** — three buckets, most important first:
   - *parsed fine, but callers may be resolved at runtime* — reflection,
     dynamic import, plugin loading. The graph read the code and the answer is
     still incomplete.
   - *failed to parse* — the graph could not read it.
   - *never analysed for relations* — not code.

## Step 4 — verify a claim instead of believing it

Every finding Gate is not certain of ships with the command to settle it:

```
verify: entire-graph neighbors --repo . --symbol <name> --file <path> \
          --relation CALLS --direction in
```

Run one. That is the whole point: the output is evidence, not an oracle.

---

## Picking a repository that actually exercises the hard part

Gate's central claim is about **code invoked by name at runtime**, where an
absent call edge is not an absent caller. To test that, pick a repo whose
architecture is dynamic. Mechanisms worth covering, each defeating static
analysis differently:

| Mechanism | Looks like | Detected via |
|---|---|---|
| Runtime reflection | `reflect`, `System.Reflection`, `java.lang.reflect` | import edge |
| Dynamic import by name | `importlib`, dotted-string config paths | import edge |
| Introspection / package walking | `inspect`, `pkgutil` | import edge |
| Service loading | `java.util.ServiceLoader` | import edge |
| Route / tool registration | decorator-registered handlers | `HANDLES_ROUTE`, `HANDLES_TOOL` |
| Interface dispatch | Go `http.Handler` | **not detected — see below** |
| Language builtins | JS `eval`, Ruby `send`, PHP variable functions | **not detected** |

The first five are found from the dependency graph with no framework knowledge.
The last two are disclosed limitations, not silent gaps: they import nothing, so
there is no edge to key on.

The ratio is the honest part. nestjs/nest imports `reflect-metadata` in 8 files
and reaches the same facility through the `Reflect` global — a JS builtin needing
no import — in 95 more. Gate sees the 8. In the framework whose entire DI model
is runtime metadata, that is about an eighth of the sites.

## Reference results

One binary, `--base HEAD~3 --head HEAD`, no per-repo configuration.

| Repo | Language | Files | Time | Runtime-dispatch files |
|---|---|---|---|---|
| sinatra | Ruby | 292 | 1.2s | 0 |
| gin | Go | 130 | 2.1s | 13 |
| scrapy | Python | 680 | 5.5s | 27 |
| pytest | Python | 690 | 9.6s | 27 |
| nest | TypeScript | 2,307 | 12.0s | 8 (of ~103 sites — see limitations) |
| django | Python | 7,087 | 30.6s | 96 |

## Pitfalls

- **Cold cache.** The first run pays the full snapshot cost. Warm it before
  demoing: `entire-graph index --repo . --head --profile full`.
- **Small change sets.** `HEAD~3..HEAD` on a quiet week is a handful of
  entities. To exercise ranking, diff two release tags instead:
  `--base <older-tag> --head <newer-tag>`.
- **CI-only ranges.** Some ranges are all workflow YAML. Gate handles these
  correctly — CI entities are outside the coverage dimension's jurisdiction —
  but they make a weak demo. Check the review order before committing to a range.
- **A repo with no test tree** reports every entity `no-resolver` and caps the
  verdict at `continue`. That is correct behaviour, not a failure.
