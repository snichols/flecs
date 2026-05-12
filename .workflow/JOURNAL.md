## iterate iteration 1 (2026-05-12)

Implemented query-term traversal modifiers (Up/SelfUp/Cascade/Self) for issue #99. Added TraverseFlags type and Term builder methods; findUpSource helper; updated matchesTable/Iter/Next/Field with per-table upSources maps; IsFieldSelf and FieldShared[T] accessors; tableRelDepth and sortByCascadeDepth for cascade ordering in CachedQuery; notifyTableCreated re-sort on new cascade table. 16 new tests, coverage 95.1%, go vet + golangci-lint clean, -race -count=3 clean.

