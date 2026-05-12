## iterate iteration 1 (2026-05-12)

Phase 15.8 fully implemented: unexported scope interface (scopeWorld() *World) added to scope.go; both *Reader and *Writer satisfy it. All 16 read free-functions migrated from *Reader to scope (Get, GetRef, Has, Owns, GetPair, GetPairRef, HasID, OwnsID, Each1-4, GetUp, HasUp, TargetUp, PrefabOf, plus IsEnabledID and IsEnabled in cantoggle.go). AsReader() deleted. All test files, doc.go, docs/*.md, docs/observers_examples_test.go, and README.md updated. New scope_promotion_test.go with 6 TestScopePromotion_* tests. CHANGELOG v0.40.0, ROADMAP.md (Phase 15.8 shipped, Acyclic bumped to 15.9). go vet/golangci-lint clean; go test ./... -race -count=3 passes; coverage 95.0%.

