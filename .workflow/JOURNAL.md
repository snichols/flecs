## iterate iteration 1 (2026-05-11)

Implemented exclusive access checking (debug build, -tags flecs_exclusive_access). Created: exclusive_access_on/off.go (paired build-tagged const), goid.go (goroutine ID via runtime.Stack), exclusive_access.go (ExclusiveAccessBegin/End + checkExclusiveAccessWrite/Read). Added exclusiveAccess atomic.Uint64 + exclusiveThread string to World. Instrumented all public mutators and readers across 13 files. Created 5 debug build tests + 1 release no-op test. Added CI job, doc.go debug section, CHANGELOG unreleased entry, README feature row. Both builds pass go test -race -count=10 and golangci-lint clean.

## iterate iteration 2 (2026-05-11)

Fixed coverage regression: split exclusive_access.go into build-tagged files (exclusive_access_release.go with empty no-op stubs, exclusive_access_debug.go with full implementation). The original file had unreachable dead-code after `if !flecsExclusiveAccess { return }` guards that Go's coverage tool counted as uncovered statements, dropping coverage from 95.6% to 94.4%. Empty stubs contribute zero statements to coverage arithmetic. Coverage is now 95.7% (≥95% requirement met). Also removed the now-unnecessary goid() stub from exclusive_access_off.go. Both builds pass go test -race -count=3 and go vet clean.

## iterate iteration 3 (2026-05-11)

Fixed golangci-lint unused errors on both builds: added //nolint:unused to const flecsExclusiveAccess in exclusive_access_on/off.go and to World fields exclusiveAccess/exclusiveThread in world.go. Both golangci-lint runs (release and debug tag) are now clean. All tests pass on both builds with -race -count=3.

