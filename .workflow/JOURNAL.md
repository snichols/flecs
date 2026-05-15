## iterate iteration 1 (2026-05-15)

Phase 16.47 Parent Hierarchy Storage complete: 17 files, 31 tests pass -race -count=3, docs updated, pushed to snichols/issue-277

## iterate iteration 2 (2026-05-15)

Created docs/ParentStorage.md; renamed all parent_storage_test.go functions to exact required names; added 13 missing tests + BenchmarkParentStorage_Reparent_FullArchetype; fixed cleanup.go (policyOnDeleteTargetRemove bit) and world.go deleteImmediate so parent-storage PanicAction and RemoveAction work correctly. All tests pass -race -count=3.

