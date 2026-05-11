## iterate iteration 1 (2026-05-11)

Implemented readonly concurrency window (issue #79): readonly.go with ReadonlyBegin/ReadonlyEnd/Readonly; readonly atomic.Bool field on World; all mutators (Delete, Set, Remove, AddID, RemoveID, SetPair, SetByID) extended with || w.readonly.Load() under deferMu; REST GET handlers wrapped in w.Readonly; concurrent_test.go with 6 tests (concurrent readers, enqueue writes, delete enqueued, nested defer, regression); doc.go/CHANGELOG/README updated. go test ./... -race -count=10 passes clean, coverage 95.6%.

## iterate iteration 2 (2026-05-11)

Removed unused readonlyVel type from concurrent_test.go to fix golangci-lint unused type error. All other requirements remain satisfied: readonly.go, World.readonly field, mutator guards, REST Readonly wrappers, 4 concurrent tests, docs/CHANGELOG/README updates, go test -race passes, lint clean.

