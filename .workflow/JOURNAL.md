## iterate iteration 1 (2026-05-14)

Implemented GET /stats/world and GET /stats/pipeline REST endpoints (Phase 16.31, v0.86.0). Added snake_case response structs, goroutine-safe handlers using StatsSnapshot() directly, Cache-Control: no-store, and 503 panic recovery. Created rest_stats_test.go with 10 test cases (shapes, headers, after-Progress values, system metrics, panic recovery, concurrent access). Updated CHANGELOG.md, README.md, ROADMAP.md, docs/FlecsRemoteApi.md, docs/README.md. go vet, golangci-lint, go test -race -count=3 all clean; rest.go per-function coverage 96.9%.

