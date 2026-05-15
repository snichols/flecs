## iterate iteration 1 (2026-05-15)

Phase 16.42 Compound units shipped as v0.97.0. All requirements complete: RegisterCompoundUnit/UnitProduct/UnitQuotient/UnitPower API; 10 built-in compound units (indices 63–72); siCanonical dimensional analysis; JSON and binary snapshot persistence; REST /type_info compound symbol; 37 new tests; 95.0% coverage; go vet, golangci-lint, -race -count=3 clean; docs updated.

## iterate iteration 2 (2026-05-15)

Fixed V2 /type_info endpoint: changed fr.GetName(unitID) to w.UnitSymbol(unitID) in rest.go line 560 so compound symbols (e.g. "N") flow through instead of entity names (e.g. "NewtonCompound"). Added TestRest_TypeInfo_CompoundUnit_V2 to rest_type_info_test.go exercising the V2 endpoint with a NewtonCompound-annotated component and asserting unit field == "N". Updated two existing tests (TestRESTTypeInfoWithUnit, TestRest_TypeInfo_UnitAnnotation_Nested) to expect the symbol "m" rather than entity name "Meter". All tests pass: go test ./... -race -count=3 clean, go vet clean, golangci-lint clean.

