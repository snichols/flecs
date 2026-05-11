## iterate iteration 1 (2026-05-11)

Implemented all three corrections: (1) removed flecs_exclusive_access build tag entirely — checks always compiled in; (2) replaced runtime.Stack goroutine-ID parsing with github.com/petermattis/goid (~ns vs ~µs per call); (3) purged all "debug build"/"release build" terminology from CHANGELOG, README, doc.go. Merged exclusive_access_debug.go into a single exclusive_access.go, deleted the on/off/release stub files and release-build no-op test, dropped build tag from exclusive_access_test.go, added TestGoidGetIsNonZero and TestExclusiveAccessZeroOverheadCommonPath. Collapsed CI to one test job + one lint job. All tests pass with -race -count=3; coverage 95.8%; go vet clean.

