## iterate iteration 1 (2026-05-10)

Implemented Phase 4.4: built-in Name component closing Phase 4. Added Name struct in name.go, registered as built-in component entity at index 3 in World.New() (user entities shift to index 4). Implemented SetName/GetName/RemoveName (thin wrappers over Set[Name]/Get[Name]/Remove[Name]), LookupChild (EachChild scan for non-zero parent; eachAlive scan with Owns[Name]+ParentOf filter for root scope), Lookup (dot-split path walk with malformed-path guards), and PathOf (ChildOf chain walk upward, stops at unnamed ancestors with cycle guard). Updated isa_test.go baseline (2→3 built-ins). Full name_test.go with 25 tests covering all required cases. go test ./... -race -count=2 passes at 97.4% coverage; go vet and golangci-lint clean.

