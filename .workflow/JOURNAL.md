## iterate iteration 1 (2026-05-15)

Phase 16.47 Parent Hierarchy Storage complete: 17 files, 31 tests pass -race -count=3, docs updated, pushed to snichols/issue-277

## iterate iteration 2 (2026-05-15)

Created docs/ParentStorage.md; renamed all parent_storage_test.go functions to exact required names; added 13 missing tests + BenchmarkParentStorage_Reparent_FullArchetype; fixed cleanup.go (policyOnDeleteTargetRemove bit) and world.go deleteImmediate so parent-storage PanicAction and RemoveAction work correctly. All tests pass -race -count=3.

## iterate iteration 3 (2026-05-15)

Added ParentStorage trait section to docs/Relationships.md — new subsection under "Relationship traits" (between PairIsTag and Relationship performance) describing SetParentStorage/IsParentStorage as a relationship-level option for non-fragmenting hierarchy storage. Includes ToC entry, API example, query/observer/cleanup/snapshot behavior notes, and a when-to-use comparison table. This was the sole gap flagged by the verify gate.

## iterate iteration 4 (2026-05-15)

Fixed stale reference on docs/README.md line 109: updated "Join-order optimization deferred to Phase 16.27" to "shipped in v0.99.0 (Phase 16.44)".

