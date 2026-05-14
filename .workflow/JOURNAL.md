# Iteration Journal

## Iteration 1 — 2026-05-14

### Summary

Implemented Phase 16.26: Multi-variable query support (v0.81.0).

### What was done

**query.go**:
- Bumped variable cap from 8 → 16 in `buildVarSlotsFromTerms`; updated panic message to drop "v1 cap" and "Phase 16.25.x" references
- Replaced `collectVarDomain` with generalized `collectVarDomainFor(varName, bindings)` that handles any variable name with outer bindings, including fixed-source + tgtVar terms
- Added `buildVarTopoOrder` function: builds variable dependency graph (B depends on A when term has srcVar=A, tgtVar=B); Kahn's algorithm topo-sort; cycle detection panics at construction with cycle path
- Added `traceVarCycle` helper for cycle path extraction
- Refactored `buildVarRows` to use recursive `buildVarRowsRec` implementing N-level nested join loop driven by topo-sorted `varOrder`
- Updated `varCheckTable` to skip terms with `srcVar != ""` or `Src != 0` (variable-to-variable and fixed-source constraints, consumed during domain collection)
- Updated `buildFixedSourcePtrs` to skip terms with `tgtVar != ""` (fixed-source + variable pair target handled by domain collection)
- Added `varOrder []string` field to `Query` struct
- Removed "v1 limitation" comment from `WithVar`

**cached_query.go**:
- Added `varOrder []string` field
- Populated from `buildVarTopoOrder` at construction
- Passed through in proxy struct for `buildVarRows`

**query_var_test.go**:
- Updated `TestVarCapExceededPanics` from 9→17 variables (new cap=16)
- Added `qvGalaxy`, `qvHealth` component types
- Added `qvBuildStarScene` scene builder (ships + planets + star)
- Added 10 new multi-variable test functions covering: two-variable chain, three-variable chain, empty join (two variants), many-to-many, health filter, multiple bindings via Iter.Var, cycle detection panic, 16-variable cap works, CachedQuery multi-variable, WithSourceTerm composition

**Doc updates**:
- `docs/Queries.md`: renamed heading to "Query variables", added multi-variable example, updated cap, added cycle-detection section, removed v1 single-variable limitation, replaced join-order limitation with Phase 16.27 forward reference
- `docs/README.md`: updated gap entry to reflect v0.81.0 multi-variable
- `README.md`: updated feature index and compatibility matrix
- `ROADMAP.md`: bumped heading to "through v0.81.0"; added Phase 16.26 entry; replaced "Multi-variable join optimization" future-work with "Join-order optimization (Phase 16.27 candidate)"
- `CHANGELOG.md`: added v0.81.0 entry at top

### Acceptance criteria met

- `go vet ./...` clean ✓
- `golangci-lint run` clean ✓
- `go test ./... -race -count=3` passes ✓
- Coverage 95.0% ✓
- All 27 existing Phase 16.25 tests continue to pass ✓
- 10 new multi-variable tests pass ✓
## iterate iteration 1 (2026-05-14)

Phase 16.26 multi-variable query support implemented: N-variable nested join loop with topo-sort dependency ordering, cycle detection, cap bumped to 16, fixed-source+TgtVar composition, 10 new tests, all docs updated. go vet/lint clean, race -count=3 passes, coverage 95.0%.

