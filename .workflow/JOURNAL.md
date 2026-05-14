## iterate iteration 1 (2026-05-14)

Implemented fixed per-term source (v0.73.0): WithSourceTerm builder, (Term).Source chained builder, Term.Src field, resolveFixedSourcePtr/buildFixedSourcePtrs helpers, dead-iter on missing required source, snapshot-at-iter-start contract, optional divergence (absent optional source yields FieldMaybe nil/false, entities still match), full CachedQuery support, 19 new tests at 95.0% package coverage, docs/Queries.md new section, CHANGELOG.md v0.73.0 entry, ROADMAP.md bumped.

