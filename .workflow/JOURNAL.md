## iterate iteration 1 (2026-05-10)

Implemented Each1/Each2/Each3/Each4 typed iteration helpers in each.go. All four functions auto-register component types, use fixed-size local [N]ID arrays for term IDs, delegate to NewQuery/Iter/Field, and invoke the closure with live column pointers. Tests in each_test.go cover: Each1–4 basic, mixed archetypes, no-match, auto-registration, tag components, GC pointer survival, and explicit mutation-is-live contract. All requirements met: go test -race -count=2 passes, go vet clean, golangci-lint clean, flecs coverage 97.9%.

