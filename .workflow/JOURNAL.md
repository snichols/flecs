## iterate iteration 1 (2026-05-11)

Implemented Phase 10.1 parallel system dispatch: SetParallel/Parallel/SetWriteSet on System, SetWorkerCount/WorkerCount on World with persistent goroutine pool (buffered chan func(), 2*n size), phase dispatch refactored to batch parallel systems with pairwise-disjoint write sets via sync.WaitGroup, sync.Mutex added to all deferred queue paths (Set/Remove/Delete/AddID/RemoveID/SetPair/SetByID/DeferBegin/DeferEnd), 12 tests in parallel_test.go, 2 benchmarks in bench_test.go, doc.go/CHANGELOG/README/BENCH updated. go test ./... -race -count=2 passes, coverage 95.5%, go vet and golangci-lint clean.

