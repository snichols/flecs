## iterate iteration 1 (2026-05-11)

Implemented SetMultiThreaded within-system row-range parallelism (issue #88). Added multiThreaded field to System with SetMultiThreaded/MultiThreaded accessors. Added iter clipping to QueryIter (workerIdx/workerTotal/wFirst/wCount fields, clippedCopy method, updated Next/Count/Entities/Field/FieldMaybe). Updated runPhase dispatcher to fan out N worker goroutines for multi-threaded systems with WaitGroup synchronization before sibling systems. Added 5 tests (ProcessesEachEntityOnce with workers 1/2/4/8, CannotBatchWithSiblings, UnevenSplit, EmptyWorkers, DeferredMutations), BenchmarkMultiThreadedSystem, and updated doc.go/CHANGELOG/README/ROADMAP. All tests pass under -race -count=10, coverage 95.8%, vet+lint clean.

