## iterate iteration 1 (2026-05-14)

Phase 16.25: Query variables ($Var, single-variable v1) — v0.80.0. Added WithVar/WithPairTgtVar constructors, SrcVar/TgtVar Term fields, variable slot compilation, buildVarRows pre-materialisation, nextVar/Var iterator, CachedQuery variable support, 24 tests, 95.1% coverage; go vet + golangci-lint clean; race -count=3 passes. Docs: Queries.md §Query variables, docs/README.md gap closed, README.md feature index, CHANGELOG.md v0.80.0, ROADMAP.md heading bump + Phase 16.25.x future work.

## iterate iteration 2 (2026-05-14)

Added missing (Term).SrcVar(name string) Term and (Term).TgtVar(name string) Term chained setter methods. Renamed Term struct fields from exported SrcVar/TgtVar to unexported srcVar/tgtVar (required since Go disallows a field and method with the same name on the same type), updated all field access sites, and added 3 new test functions covering happy-path and panic cases. Coverage restored to 95.1%; go vet + golangci-lint clean; race -count=3 passes.

