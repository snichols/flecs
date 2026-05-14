## iterate iteration 1 (2026-05-14)

Phase 16.19 entity scoping implemented (v0.74.0): new entity_scope.go with WithinScope/PushScope/PopScope/GetScope; scopeStack []ID field on *Writer; Writer.NewEntity and RangeNew hook auto-injects (ChildOf, scope) via AddID; world.Write resets stack on top-level entry; MakeAlive explicitly bypasses scope; 13 tests in entity_scope_test.go at 95.0% package coverage; docs updated (README.md, HierarchiesManual.md new §Entity scoping, CHANGELOG.md v0.74.0 entry, ROADMAP.md heading bump + Phase 16.19 bullet). All tests pass with -race -count=3, go vet clean, golangci-lint clean.

