## iterate iteration 1 (2026-05-15)

Phase 16.50 implemented: per-stage queues for parallel-batch dispatch. Core: wave-based dispatcher assigns each system in a parallel batch to an exclusive stage slot (wavePos → stages[wavePos+1]), sequential waves via wg.Wait() prevent cross-wave concurrent access, mergeWorkerStages() helper shared by MT and parallel-batch paths. Fixed latent data race on cmdQueue.append under -race. 11 new TestParallelSystems_* tests, 2 new benchmarks, ROADMAP/CHANGELOG/README/BENCH/docs updated. go vet clean, golangci-lint clean, go test ./... -race -count=10 clean, coverage 95.0%.

