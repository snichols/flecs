## iterate iteration 1 (2026-05-10)

Phase 8.3: three micro-optimizations shipped with before/after benchmarks.

A) Field[T] zero-alloc: replaced reflect.Value.Interface().([]T) with unsafe.Slice over the column base pointer. Added ColumnBasePtr to table.Table and BaseUnsafe to column.Column. Field[T]: 93.68→16.9 ns/op (-82%), 1 alloc→0. QueryIterField_10k: -25% ns, -2 allocs. QueryAcrossArchetypes_10k: -27% ns, -12 allocs.

B) Observer dispatch zero-alloc: removed per-fire snapshot slice from dispatchObservers. Unsubscribe now takes immediate effect for not-yet-visited observers. Updated Unsubscribe godoc and TestObserveUnsubscribeDuringDispatch. ObserverFires_5observers_10k: 2.98ms→1.01ms (-66%), 480kB→0 B (-10000 allocs/10k).

C) Lazy seen map: getViaIsA/hasViaIsA now accept nil seen and allocate lazily on first IsA pair found. Get[T]/Has[T] in world.go and HasID in id_ops.go pass nil. Eliminates map allocation on the common no-IsA local-miss path.

BENCH.md updated with Phase 8.3 before/after section. All tests pass with -race -count=2. go vet and golangci-lint clean. Coverage 97.0% (floor: 90%).

