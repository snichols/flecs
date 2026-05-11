## iterate iteration 1 (2026-05-10)

Implemented Phase 5.3 deferred command queue. New defer.go adds DeferBegin/DeferEnd/Defer/IsDeferred with full godoc. World gains deferDepth and deferred fields. Set[T], Remove[T], Delete, AddID, RemoveID, SetPair[T] each split into public defer-aware wrapper + *Immediate helper; SetName/RemoveName inherit deferral transitively. Flush runs at depth 0 so hooks/observers fire in queue order. 20 new tests in defer_test.go covering all specified scenarios. go test -race -count=2 passes; coverage 97.8% (matches 97.9% baseline); go vet + golangci-lint clean.

