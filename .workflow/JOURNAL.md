## iterate iteration 1 (2026-05-10)

Phase 1.5 complete: World public ECS facade implemented. Created internal/ids leaf package to break import cycles between root flecs and internal subpackages. Updated id.go to use type alias (flecs.ID = ids.ID). Updated table, entityindex to import ids. Added Component field + EntityCallback type + OnAdd/OnSet/OnRemove hooks to component package. Added AssociateID and LookupByID to Registry. Implemented World with NewEntity, Delete, IsAlive, Count, RegisterComponent[T], Set[T], Get[T], Has[T], Remove[T], and migrate() archetype migration helper. Full test suite: 97.6% root-package coverage, go test ./... -race passes, go vet ./... passes, gofmt/goimports clean.

