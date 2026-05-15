## iterate iteration 1 (2026-05-15)

Phase 16.49 (v0.104.0) implemented: profile-driven sync.Pool pooling for three traversal hot paths (walkUp 13%, hasViaIsA 9%, getViaIsA 8%). Total alloc count reduced 37% (120M→75M). Key benchmarks: IsAGet_Hit −73% latency/−100% allocs, GetUp_Depth1 −70%/−100%, OwnsVsHas/Has-via-IsA −79%/−100%. All sites audited; non-escaping sites pooled, escaping sites documented and skipped. New: pool.go, PROFILE.md, pool_test.go (concurrent/reset/regression/alloc-count benchmarks). Updated: BENCH.md, ROADMAP.md, CHANGELOG.md. All gates pass: go vet clean, golangci-lint clean, go test -race -count=3 clean, coverage 95.0%.

