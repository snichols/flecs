## iterate iteration 1 (2026-05-14)

Implemented OnReplace[T] / OnReplaceID lifecycle hook (v0.55.0). Added ReplaceCallback type and OnReplace field to component.Hooks; wired fireOnReplace into all fire sites (archetype, sparse-only, DontFragment, pair, and deferred cmdModified paths); added cmd.firstAdd flag to skip OnReplace on first write to just-migrated slots; added 15 test cases covering all required scenarios; updated ObserversManual.md, EntitiesComponents.md, docs/README.md, README.md, CHANGELOG.md, and ROADMAP.md. go vet clean, golangci-lint clean, tests pass with -race -count=3, coverage 95.0%.

