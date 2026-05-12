## iterate iteration 1 (2026-05-12)

Phase 15.1 implemented: OnInstantiate Override/DontInherit/Inherit policies fully wired. New instantiate_policies.go with flag type, Set/Get/apply API. Override triggers eager copy on IsA-add (transitive, cycle-safe). DontInherit suppresses IsA chain walk and query auto-promotion. 13 test cases in instantiate_policies_test.go. go test ./... -race -count=3 clean. Main package coverage 95.0%. Docs updated: PrefabsManual, ComponentTraits roadmap table, docs/README feature-gap list, CHANGELOG v0.33.0, ROADMAP Shipped list.

