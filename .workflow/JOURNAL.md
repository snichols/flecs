## iterate iteration 1 (2026-05-10)

Phase 9.2.2 implemented: ChildOf hierarchy serialization. Extended jsonEntity with Parent field (omitempty). MarshalJSON does topological sort via DFS (parent-before-child order, sibling allocation order, cycle detection). UnmarshalJSON restores ChildOf before components in a single sequential pass. 10 new tests covering single/multi-level hierarchies, wide trees, cascade delete, topological ordering, cycle detection, missing parent error, anonymous child, two-step round-trip, and parent-absent check. All 25 marshal tests and full suite pass with -race -count=2. Coverage 96.8%. doc.go and CHANGELOG updated.

