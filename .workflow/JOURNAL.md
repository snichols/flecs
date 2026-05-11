## iterate iteration 1 (2026-05-10)

Implemented Phase 7.3: OnFixedUpdate built-in pipeline phase with frame timing. Added onFixedUpdateID at index 7, time/frameCount/fixedTimestep/fixedAccumulator fields on World. Added Time(), FrameCount(), FixedTimestep(), SetFixedTimestep() methods. Refactored Progress() to validate dt>=0, accumulate time/frameCount, run OnFixedUpdate via per-iteration Defer accumulator loop between PreUpdate and OnUpdate. Extended NewSystemInPhase whitelist and updated panic message to list all 4 phases. Wrote 15 tests in timer_test.go covering all required scenarios. Updated TestIsAWorldCountBaseline 6→7. go test ./... -race -count=2 passes; golangci-lint clean; coverage 97.2%.

