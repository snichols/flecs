## iterate iteration 1 (2026-05-11)

Closed all exclusive-access instrumentation gaps: added Write checks to Progress, RegisterComponent, NewSystem/NewSystemInPhase, NewQuery/NewQueryFromTerms, NewCachedQuery/NewCachedQueryFromTerms; added Read checks to IsAlive, Count, SystemCount, SystemCountInPhase, TablesFor, EachTableFor; added two new tests (TestExclusiveAccessProgressFromOtherGoroutinePanics, TestExclusiveAccessReadEntryPointsRespectOwnership); added CI lint-exclusive-access job; updated CHANGELOG. All tests and lint pass on both default and flecs_exclusive_access builds.

