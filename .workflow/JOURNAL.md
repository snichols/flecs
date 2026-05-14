## iterate iteration 1 (2026-05-14)

Phase 16.13 dynamic component registration fully implemented and shipped as v0.68.0. New API: RegisterDynamicComponent, RegisterDynamicComponentWithMarshaler, GetIDPtr, SetIDPtr, EachByID, OnAddByID/OnSetByID/OnRemoveByID. Storage routes through archetype/sparse/DontFragment unchanged. JSON: base64 default with custom hook override via unmarshalDynamic. 28 tests in component_dynamic_test.go, coverage 95.0%, go vet/golangci-lint/go test -race -count=3 all clean. Docs: EntitiesComponents.md dynamic section added, docs/README.md gap flipped to shipped, README.md feature row added, CHANGELOG.md v0.68.0 entry, ROADMAP.md heading bumped to v0.68.0.

