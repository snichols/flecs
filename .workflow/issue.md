## Goal

**Phase 16.58 — documentation & API consistency hardening pass. Target version v0.113.0.**

A quality-consolidation phase with **zero new API surface**. The library is functionally complete (full upstream parity plus 7 Go-idiomatic value-adds through v0.112.0 / Phase 16.57). This phase hardens documentation, godoc, and internal consistency to prepare for a *potential* v1.0 declaration. Tagging v1.0 itself remains an operator decision and is explicitly out of scope.

**Scope-creep guard (hard constraint):** this phase MUST NOT add, rename, or change any exported symbol. It is documentation, comments, examples, and test-only changes. If the audit uncovers a genuine API bug, file a *separate* issue rather than fixing it here, and reference that issue in the PR description. Emphasize this so the iterate agent does not "improve" any signatures.

This phase deliberately produces a smaller diff than recent feature phases. That is correct. If a dimension is already clean, a short honest "checked, no gaps" in `AUDIT.md` is a valid result — do not manufacture changes.

### Audit dimensions

**1. godoc completeness**
- Enumerate every exported symbol (`go doc -all ./...` or AST walk) across the root package and `flectest`
- Flag every exported symbol with NO doc comment
- Flag every doc comment that does not begin with the symbol name (Go convention)
- Add concise, accurate doc comments for the gaps. Do NOT pad — one good sentence beats three vague ones
- Verify package-level `doc.go` overview reflects the current feature set (context.Context, iter.Seq, streaming snapshots, flectest, expvar, snapshot migration — all post-port-completion additions should be mentioned)

**2. Cross-reference integrity (docs/*.md)**
- Every `docs/*.md` internal link (`[x](Y.md#anchor)`) must resolve — target file exists and the anchor exists
- Every `@`/code reference to a symbol must name a symbol that still exists
- `docs/README.md` gap list: every "shipped in vX" claim matches the actual CHANGELOG/tag; every remaining "not yet ported" genuinely still a gap (most should be ✅ given the major-gaps-closed milestone)
- The "Manuals" table status column accuracy

**3. Stale version references**
- Grep all docs + comments for version strings (`v0.\d+`) and verify none claim a feature shipped in the wrong version
- ROADMAP "Shipped (through vX)" heading matches the latest tag (currently `v0.112.0`; becomes `v0.113.0` after this phase)
- CHANGELOG ordering strictly descending; every shipped phase 16.35–16.57 has an entry
- Sweep for code-vs-doc version drift like the codec format-version note Phase 16.57 found (docs said "v1" while code was v2→v3)

**4. CHANGELOG consolidation**
- Verify every version v0.90.0 → v0.112.0 has a CHANGELOG entry with consistent formatting
- Do NOT rewrite history; only fix formatting inconsistency, missing entries, or factual errors
- Add an "Unreleased" placeholder section only if the project convention already uses one (verify first)

**5. BENCH.md refresh**
- Re-run `go test -bench=. -benchmem ./...` and update BENCH.md numbers if materially drifted
- Verify every benchmark referenced in BENCH.md still exists in `bench_test.go`; remove stale rows; add any missing headline benchmark
- Benchmark numbers are environment-dependent. The failure mode being fixed is "references a deleted benchmark" / "claims a number off by 10x", NOT "off by 15%". Document the measurement environment

**6. Example test coverage of headline features**
- Verify each headline feature has at least one runnable godoc-visible `Example`: query iteration, iter.Seq, observers, hierarchies, prefabs, snapshots, streaming snapshots, snapshot migration, context cancellation, flectest, expvar, REST
- For any headline feature with NO runnable example, add a minimal one
- Examples must pass (`go test`) and produce deterministic output where `// Output:` is used

**7. README accuracy**
- Feature table reflects current capability
- Install / import instructions correct (module path, Go version)
- Any "TODO" / "coming soon" language that is now done → update
- Quickstart code compiles and runs (extract into an example test if not already)

### Deliverables

- All exported symbols documented (godoc gap = 0 for root + `flectest`)
- All `docs/*.md` internal links resolve
- All version claims in docs match tags/CHANGELOG
- CHANGELOG complete and consistently formatted v0.90.0 → v0.112.0
- BENCH.md refreshed (no stale/missing benchmark rows; numbers re-measured; environment documented)
- Every headline feature has a runnable Example
- README accurate
- New `docs/AUDIT.md` summarizing what was checked and what was fixed (audit trail; one screen, not a novel; "checked, no gaps" is a valid line)
- If any genuine API bug found: a SEPARATE filed issue, referenced in the PR description, NOT fixed here

### Required tests / verification

- `go doc -all ./...` produces no "undocumented" warnings (or an equivalent AST-based check committed as a `tools/doccheck` test if desired — agent decides; minimal is fine)
- `TestDocLinks` (new, in a `docs_test` package or root) — parses every `docs/*.md`, extracts internal links, asserts each target file + anchor exists. Keep it small and dependency-free (stdlib only)
- `TestExamplesCompile` — implicit via `go test ./...` (examples are tests); ensure all new examples pass
- All existing tests pass unchanged (only adding examples + the link-check test; do not change test logic)
- `go vet ./...`, `golangci-lint run ./...`, `go test ./... -race -count=3` clean
- Coverage ≥ 95% maintained (examples count toward coverage; should hold or improve)

### Documentation update matrix

This phase IS the documentation update:
- `@/work/agents/claude/projects/flecs/doc.go` — package overview current
- `@/work/agents/claude/projects/flecs/docs/README.md` — gap list + manuals table accurate
- `@/work/agents/claude/projects/flecs/ROADMAP.md` — heading + shipped list accurate; add Phase 16.58 entry
- `@/work/agents/claude/projects/flecs/CHANGELOG.md` — complete + consistent; add v0.113.0 entry for this hardening pass
- `@/work/agents/claude/projects/flecs/BENCH.md` — refreshed
- `@/work/agents/claude/projects/flecs/README.md` — accurate
- New `@/work/agents/claude/projects/flecs/docs/AUDIT.md` — audit summary
- Plus godoc comments across source files (the bulk of the diff)

### Non-goals

- ANY new/renamed/changed exported symbol (hard constraint — separate issue if a bug is found)
- Rewriting documentation prose for style (only fix inaccuracy, staleness, broken links, missing godoc)
- Tagging v1.0 (operator decision; out of scope)
- Performance optimization (BENCH.md is refreshed, not improved)
- Restructuring the `docs/` layout or file organization
- Removing or deprecating any feature
- Changing test logic (only adding examples + the link-check test)

### Notes

- Target version: **v0.113.0** / Phase **16.58**
- Label `snichols/queued` (NOT a bug)
- The doc-link checker test is the one piece of genuinely new test code; keep it small and stdlib-only
- Repo state at filing: latest tag `v0.112.0`, Phase 16.57 (snapshot schema-version + migration registry) shipped; `doc.go` and `VISION.md` present; `docs/AUDIT.md` does not yet exist; CHANGELOG runs cleanly v0.90.0 → v0.112.0 in strict descending order

## Constraints

- @/work/agents/claude/projects/flecs/doc.go — package-level overview must reflect the current post-port feature set (context.Context, iter.Seq, streaming snapshots, flectest, expvar, snapshot migration); audit and update for accuracy without changing exported symbols
- @/work/agents/claude/projects/flecs/docs/README.md — authoritative gap list + Manuals status table; every "shipped in vX" claim must match the CHANGELOG/tag, every remaining gap must genuinely still be open
- @/work/agents/claude/projects/flecs/ROADMAP.md — "Shipped (through vX)" heading must match latest tag; add a Phase 16.58 entry; shipped list accuracy
- @/work/agents/claude/projects/flecs/CHANGELOG.md — strictly descending order; complete and consistently formatted v0.90.0 → v0.112.0; add the v0.113.0 entry; do not rewrite history (formatting/missing-entry/factual fixes only)
- @/work/agents/claude/projects/flecs/BENCH.md — re-measure via `go test -bench=. -benchmem ./...`; benchmark rows must correspond to benchmarks that still exist in @/work/agents/claude/projects/flecs/bench_test.go; document the measurement environment
- @/work/agents/claude/projects/flecs/README.md — feature table, install/import (module path, Go version), and quickstart must be accurate and compile
- @/work/agents/claude/projects/flecs/bench_test.go — source of truth for which benchmarks exist; reconcile BENCH.md against it
- @/work/agents/claude/projects/flecs/flectest — subpackage; audit its godoc completeness alongside the root package (godoc gap must be 0 here too)
- @/work/agents/claude/projects/flecs/docs — every `docs/*.md` internal link and symbol reference must resolve; the new `TestDocLinks` enforces this
- @/work/agents/claude/projects/flecs/VISION.md — keep changes within the stated product direction; this phase consolidates quality (Go-idiomatic, well-documented, parity-complete) rather than expanding scope
- Hard constraint (no file reference): zero exported-symbol change — no added, renamed, or modified exported symbol anywhere in the diff. A genuine API bug becomes a separate filed issue referenced in the PR description, never an in-phase fix. Test logic is unchanged; only runnable examples and the stdlib-only `TestDocLinks` are added
