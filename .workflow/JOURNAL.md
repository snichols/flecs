## iterate iteration 1 (2026-05-14)

Implemented GET /type_info/{path} REST endpoint. Adds restTypeInfo handler to rest.go with depth-1 reflect.StructField walk for typed structs, opaque:true for dynamic components, unit annotation via UnitFor, and 404 for bare entity-tags or unknown paths. Added 9-case test file rest_type_info_test.go covering typed struct, dynamic, bare entity, nonexistent, unit, nested struct, opaque fields, concurrent access, and cache-control. Updated docs/FlecsRemoteApi.md with new section, docs/README.md lines 90 and 181, README.md feature list, CHANGELOG.md v0.87.0 entry, and ROADMAP.md heading bump. All tests pass under -race -count=3; go vet and golangci-lint clean; restTypeInfo 100% coverage; overall package 95.0%.

