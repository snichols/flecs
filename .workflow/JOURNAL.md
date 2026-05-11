## iterate iteration 1 (2026-05-10)

Implemented Phase 5.2 multi-subscriber Observer system. New observer.go: EventKind enum (OnAdd/Set/Remove) with String(), observerKey/observerNode internals with deferred-removal semantics, Observer handle with idempotent Unsubscribe, Observe[T] (auto-registers component), ObserveID (raw id, no auto-register), Observe2[T] (multi-event single handle). Refactored fireOnAdd/Set/Remove to take explicit id ID so observers fire even when info==nil (tags/pairs). Updated all call sites in world.go and id_ops.go. 16 new tests in observer_test.go covering all required scenarios. go test -race -count=2 passes; coverage 97.9%; go vet + golangci-lint clean.

