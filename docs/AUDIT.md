# Phase 16.58 Documentation Audit

Audit trail for the Phase 16.58 documentation & API consistency hardening pass
(`v0.113.0`). Each dimension below records what was checked and what was fixed.
"Checked, no gaps" means the dimension was verified clean with no changes needed.

---

## 1. godoc completeness

**Method:** AST walk via `go doc -all ./...`; confirmed all exported symbols
have a doc comment that begins with the symbol name (Go convention).

**Scope:** root package (`github.com/snichols/flecs`) and
`flectest` subpackage (`github.com/snichols/flecs/flectest`).

**Result:** Checked, no gaps. Godoc gap = 0 for both packages.

---

## 2. Cross-reference integrity (`docs/*.md`)

**Method:** New `TestDocLinks` test (`docs/docs_link_test.go`) — stdlib-only,
parses every `docs/*.md`, extracts internal Markdown links matching
`[text](file.md)` or `[text](file.md#anchor)`, and asserts:
- target file exists on disk
- target anchor resolves to a heading (GitHub-style or explicit `{#id}`)

**Broken links found and fixed:**

| Source file | Broken link / anchor | Fix applied |
|---|---|---|
| `Relationships.md` | `Queries.md#traversal` (section renamed to "Relationship Traversal") | Changed to `Queries.md#relationship-traversal` |
| `README.md` (docs/) | `README.md#feature-gap-list` — heading had trailing text making anchor longer | Added `{#feature-gap-list}` to heading |
| Multiple files | `ObserversManual.md#on-delete-and-on-delete-target`, `#on-table-create`, `#on-table-delete`, `#on-table-empty-and-on-table-fill`, `#propagation-along-isa`, `#disabling-an-observer` | These headings use `{#id}` explicit anchors; `extractAnchors` was updated to recognise this syntax — all anchors now resolve |
| `ObserversManual.md`, `README.md` | `Systems.md#merge-hooks` | Heading uses `{#merge-hooks}` explicit anchor; fixed in anchor extractor |
| `Relationships.md` | `ObserversManual.md#on-delete-and-on-delete-target` | Fixed in anchor extractor (same `{#id}` issue) |

**Result:** `TestDocLinks` passes; all `docs/*.md` internal links resolve.

---

## 3. Stale version references

**Method:** Grepped docs + comments for `v0.\d+`; compared claimed ship
versions against CHANGELOG entries and git tags.

**Result:** Checked, no material gaps. CHANGELOG runs cleanly in descending
order v0.90.0 → v0.112.0 → v0.113.0 (new entry added this phase). ROADMAP
"Shipped (through vX)" heading updated from v0.112.0 → v0.113.0. No
code-vs-doc version drift equivalent to the v0.112.0 codec-format note was
found.

---

## 4. CHANGELOG consolidation

**Method:** Scanned CHANGELOG.md for formatting consistency and entry coverage
v0.90.0 → v0.112.0.

**Result:** Checked, no gaps in existing entries. Added v0.113.0 entry for
this phase. No "Unreleased" placeholder section (project convention does not
use one).

---

## 5. BENCH.md refresh

**Method:** Compared every benchmark name in the index table against the
current `bench_test.go` benchmark list. Re-ran `go test -bench=. -benchmem
-benchtime=1s ./...`.

**Changes made:**

- Removed stale `DeferBegin/DeferEnd` description from `BenchmarkDeferNested`
  row; updated to current `w.Write` scope terminology.
- Added 10 missing headline benchmarks to the index (sections j–n):
  `BenchmarkEach1_vs_All1`, `BenchmarkCachedQueryChangedHit_10kTables`,
  `BenchmarkCachedQueryChangedAfterSet`, `BenchmarkInheritableEach1_*`,
  `BenchmarkMultiThreadedSystem`, `BenchmarkMultiThreadedDeferredSet`,
  `BenchmarkQueryUpTraversal`, `BenchmarkDeferSingleSet`,
  `BenchmarkDeferBatchedAdds`.
- Added v0.113.0 baseline section with fresh numbers and measurement
  environment (AMD Ryzen Threadripper PRO 5995WX 64-Cores, Go 1.26.1,
  Linux amd64, 2026-05-15).

**Result:** No stale rows (all referenced benchmarks exist in `bench_test.go`).
Numbers updated.

---

## 6. Example test coverage (headline features)

**Verified:** every headline feature has at least one runnable `Example*`
function in the test suite.

| Headline feature | Example function | File |
|---|---|---|
| Query iteration | `ExampleQuery`, `ExampleCachedQuery`, `ExampleEach2` | `example_query_test.go` |
| iter.Seq | `ExampleAll1`, `ExampleAll2`, `ExampleAll3`, `ExampleAll4`, `ExampleQueryAll` | `queries_iter_examples_test.go` |
| Observers | `ExampleObserve` | `example_observer_test.go` |
| Hierarchies | `ExampleWorld_childOf` | `example_childof_test.go` |
| Prefabs | `ExampleWorld_isA` | `example_isa_test.go` |
| Snapshots (basic) | `ExampleTakeSnapshot` *(added this phase)* | `example_snapshot_test.go` |
| Streaming snapshots | `ExampleWorld_TakeSnapshotTo` | `snapshot_stream_example_test.go` |
| Snapshot migration | `ExampleWorld_RegisterMigration` | `snapshot_migration_example_test.go` |
| Context cancellation | `ExampleWorld_ProgressContext` *(added this phase)* | `example_context_test.go` |
| flectest | `ExampleNewWorld`, `ExampleNewWorldWith`, `ExampleMustEntity`, `ExampleMustChild` | `flectest/example_test.go` |
| expvar | `ExamplePublishExpvar`, `ExampleExpvarMap` | `expvar_example_test.go` |
| REST | `ExampleNewRESTHandler` | `example_rest_test.go` |

**Result:** All 12 headline features covered. Two examples added.

---

## 7. README accuracy

**Issues found and fixed:**

- **Concurrency model section** (`## Concurrency model`): used the removed
  `w.Readonly(func() { ... })` / `ReadonlyEnd` API (removed in v0.40.0).
  Replaced with the current `w.Read(func(*Reader))` / `w.Write(func(*Writer))`
  API and updated descriptive text.
- **Feature table** rows `"Deferred commands"` and `"Readonly concurrency
  window"` referenced removed methods (`Defer`, `DeferBegin`, `DeferEnd`,
  `w.Readonly(fn)`, `ReadonlyBegin`, `ReadonlyEnd`). Updated to current API.

**Result:** README is accurate; quickstart code and all feature table entries
reflect the current exported API.

---

## 8. `docs/AUDIT.md`

This file. Created as required by Phase 16.58 deliverables.
