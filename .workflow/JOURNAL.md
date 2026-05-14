## iterate iteration 1 (2026-05-14)

Phase 16.23 implemented: OnPreMerge/OnPostMerge/RemovePreMergeHook/RemovePostMergeHook with ErrMergeReentry guard. Pre-hook mutations batch with current merge; post-hook mutations queue for next. Hooks fire once per merge boundary (not per worker stage). 13 tests in merge_hooks_test.go; 95.0% coverage; go vet and -race -count=3 clean. All six doc targets updated (Systems.md, ObserversManual.md, docs/README.md, README.md, CHANGELOG.md, ROADMAP.md). v0.78.0 shipped.

