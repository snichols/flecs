## iterate iteration 1 (2026-05-11)

Implemented exclusive access checking (debug build, -tags flecs_exclusive_access). Created: exclusive_access_on/off.go (paired build-tagged const), goid.go (goroutine ID via runtime.Stack), exclusive_access.go (ExclusiveAccessBegin/End + checkExclusiveAccessWrite/Read). Added exclusiveAccess atomic.Uint64 + exclusiveThread string to World. Instrumented all public mutators and readers across 13 files. Created 5 debug build tests + 1 release no-op test. Added CI job, doc.go debug section, CHANGELOG unreleased entry, README feature row. Both builds pass go test -race -count=10 and golangci-lint clean.

