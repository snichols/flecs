## iterate iteration 1 (2026-05-12)

Phase 12.1 complete: per-stage command queues for multi-threaded systems. Each worker goroutine owns its own stage.queue (no mutex on hot path); stages merged in ascending id order after wg.Wait(). Coverage 95.4%, all tests pass -race -count=3, BenchmarkMultiThreadedDeferredSet shows 2.95x speedup at workers=4 vs 1.

## iterate iteration 2 (2026-05-12)

Fixed TestStageMergeOrdering to properly verify requirement (c): hook firing in correct submission order across stages. The previous test used a single entity (only one worker stage fired the hook) and never inspected the order slice. Fix: use 4 entities so both workers (stages 1 and 2) each own 2 rows via clippedCopy; record entity IDs in hookOrder via OnAdd[stageVel]; assert that all stage-1 entity hooks appear before all stage-2 entity hooks after merge. All tests pass -race -count=3, go vet clean, golangci-lint clean, coverage 95.4%.

