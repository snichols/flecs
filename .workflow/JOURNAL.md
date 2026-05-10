## iterate iteration 1 (2026-05-10)

Implemented internal/storage/entityindex package: Record and Index types, New/Alloc/Free/IsAlive/Get/Count/Each methods, paged record array (64 entries/page, lazy allocation), FIFO recycle queue for first-freed-first-reused ordering, generation overflow panic, full godoc on all exported symbols. 18 tests covering all specified scenarios including page growth, pointer stability, recycle ordering, stale handles, and generation overflow. 100% statement coverage, go test ./... -race passes, go vet clean, goimports clean.

