## iterate iteration 1 (2026-05-12)

Phase 14.8 complete: ported docs/ComponentTraits.md from upstream C flecs. Full Go-idiomatic manual covering the two implemented traits (SetInheritable[T] and the OnInstantiate/Inherit/Override/DontInherit entity IDs) plus 20+ unimplemented traits each with C-API sketches, Go workarounds, and explicit "Not yet ported" callouts. Closes with a scannable Trait system roadmap table. Created docs/component_traits_examples_test.go with 8 TestComponentTraits_* functions (all passing under go test ./... -race -count=1 and go vet). Updated docs/README.md (ComponentTraits row → landed/14.8, 9 new gaps), ROADMAP.md (14.8 → shipped v0.27.0), CHANGELOG.md (Unreleased entry).

