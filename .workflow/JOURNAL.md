## iterate iteration 1 (2026-05-10)

Phase 1.4 complete: TypeInfo.Type field added; entityindex.Record.Table field added with Free clearing it; new internal/storage/table package with Column (reflect-backed SoA, GC-safe) and Table (archetype storage with Append/RemoveSwap/Set/Get/ColumnIndex); 96.6% statement coverage; go test ./... -race passes across all 4 packages.

