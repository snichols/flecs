## iterate iteration 1 (2026-05-10)

Implemented CachedQuery (Phase 6.1): new cached_query.go with CachedQuery type, NewCachedQuery constructor, Iter/Each/Count/EntityCount/Terms/Close/IsClosed/tryMatchTable methods. Added notifyTableCreated hook to world.go (fires in migrate() on new table creation and defensively in New()). Added cached bool field to QueryIter with updated Next() that skips per-candidate filtering. Added CachedQuerySliceLen test helper to export_test.go. 17 tests in cached_query_test.go covering all specified scenarios. go test ./... -race -count=2 passes; coverage 97.5%; go vet + golangci-lint clean.

