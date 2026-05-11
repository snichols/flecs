## iterate iteration 1 (2026-05-10)

Phase 9.2.4 implemented: custom pair component serialization. Added SetPairByID, RegisterPairDataByType, jsonPair struct, pairs array in marshal/unmarshal. 14 new marshal tests + 5 component tests. All gates pass: go test -race -count=2, go vet, golangci-lint. Coverage 96.4% (flecs), 100% (component).

