## iterate iteration 1 (2026-05-11)

Implemented minimal REST API addon: rest.go with NewRESTHandler returning a configured http.ServeMux, 7 routes (GET /stats, /components, /components/{id}, /entities, /entities/{id}, /snapshot; PUT /snapshot), helper types and writeJSON/writeError. Added 18 tests in rest_test.go covering all endpoints, method-not-allowed, unknown routes, concurrent reads, custom pairs, invalid IDs. Added ExampleNewRESTHandler in example_rest_test.go. Updated doc.go REST API section, CHANGELOG Unreleased entry, README feature table. Fixed latent nil-table panic in getViaIsA, hasViaIsA, PrefabOf, EachPrefab, ParentOf. go test ./... -race -count=2 clean, go vet clean, golangci-lint clean, coverage 95.8% (>= 90%).

