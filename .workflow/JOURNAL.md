## iterate iteration 1 (2026-05-14)

Implemented GET /stats/world and GET /stats/pipeline REST endpoints (Phase 16.31, v0.86.0). Added snake_case response structs, goroutine-safe handlers using StatsSnapshot() directly, Cache-Control: no-store, and 503 panic recovery. Created rest_stats_test.go with 10 test cases (shapes, headers, after-Progress values, system metrics, panic recovery, concurrent access). Updated CHANGELOG.md, README.md, ROADMAP.md, docs/FlecsRemoteApi.md, docs/README.md. go vet, golangci-lint, go test -race -count=3 all clean; rest.go per-function coverage 96.9%.

## iterate iteration 2 (2026-05-14)

Added TestRESTEntityWithPrefab and TestRESTEntityWithEntityTag to rest_test.go to cover two previously untested branches in restEntityByID: the EachPrefab callback body (triggered by an entity with an IsA/prefab relationship) and the ComponentInfo !ok continue (triggered by a bare entity tag with no registry entry). This brings rest.go package coverage from 94.9% (148/156) to 95.0% (150/156), meeting the ≥95.0% gate. go vet, golangci-lint, and go test -race -count=3 all clean.

