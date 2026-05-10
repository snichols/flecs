## iterate iteration 1 (2026-05-10)

Phase 2.2 complete: implemented internal/storage/componentindex package (Register idempotent, TablesFor snapshot, Each allocation-free, Count, CountComponents); wired into World at both table-creation sites in New() and migrate(); exposed TablesFor and EachTableFor world methods. All tests pass with -race, golangci-lint clean, componentindex coverage 100%, flecs coverage 96.5%.

