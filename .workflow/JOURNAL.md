## iterate iteration 1 (2026-05-10)

Implemented Phase 9.2 value plane: World.GetByID and World.SetByID for runtime-dynamic component access. Created value_ops.go with GetByID (reflect.NewAt materialization, IsA inheritance, 1 alloc/call), SetByID (type-safety check, OnAdd+OnSet hooks, Defer queue support, bounce buffer for addressability), and extracted shared setImmediateByPtr from setImmediate[T]. Added 17 test cases in value_ops_test.go covering all spec requirements. Updated doc.go with GetByID/SetByID usage guidance. All tests pass (race -count=2), go vet clean, golangci-lint clean, coverage 97.1%.

