## iterate iteration 1 (2026-05-10)

Phase 9.2.4 implemented: custom pair component serialization. Added SetPairByID, RegisterPairDataByType, jsonPair struct, pairs array in marshal/unmarshal. 14 new marshal tests + 5 component tests. All gates pass: go test -race -count=2, go vet, golangci-lint. Coverage 96.4% (flecs), 100% (component).

## iterate iteration 2 (2026-05-10)

Added missing Section 6 test TestAddIDAfterSetPairIsNoOp: verifies that calling AddID(w, e, pairID) after SetPair[T] on the same pairID is idempotent and does not corrupt the stored pair data. All gates pass: go test -race -count=2 green, go vet clean, golangci-lint clean.

## iterate iteration 3 (2026-05-10)

Fixed the only remaining verify-gate gap: updated README.md feature table JSON serialization row from "no pairs/hierarchies yet" to accurately reflect that full pair support shipped in Phase 9.2.4 (ChildOf/IsA hierarchies + custom tag/data pairs).

