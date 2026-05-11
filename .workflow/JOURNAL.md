## iterate iteration 1 (2026-05-11)

Phase 9.5 change detection fully implemented. CachedQuery.Changed() returns true on first call and when any matching table is mutated (column write, structural change, new table). Table.changeCount uint64 is bumped by Append, RemoveSwap, and BumpChange(); World calls BumpChange() after in-place writes in setImmediateByPtr and setPairImmediate. 12 change detection tests, 4 new table unit tests, 2 benchmarks. Doc.go "Change detection" section added. CHANGELOG Unreleased entry and README feature list updated. flecs coverage 96.0%, table coverage 96.3%, go test -race -count=2 clean, go vet clean, golangci-lint clean.

