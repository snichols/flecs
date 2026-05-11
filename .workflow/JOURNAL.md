## iterate iteration 1 (2026-05-11)

Implemented slog-compatible lifecycle logger: SetLogger/Logger methods on World, nil-check fast path at each of 10 event sites (entity created/deleted, component registered, table created, system added/closed, observer registered/unsubscribed, snapshot serialized/loaded). 22 tests in log_test.go — all green. Coverage 95.6% (≥90%). BenchmarkSetExistingComponent unchanged at ~51 ns/op 0 allocs. Updated doc.go, CHANGELOG.md, README.md, BENCH.md.

