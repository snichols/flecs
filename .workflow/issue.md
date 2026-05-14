## Goal

Fix the misleading gap entry at \`docs/README.md:152\` that describes \`OnDelete\` / \`OnDeleteTarget\` as not-yet-ported \"observer events.\" Research into the upstream C flecs source shows this is a misclassification: those identifiers exist upstream only as **cleanup-policy relationship traits**, not as observer events. The Go port already ships the equivalent functionality, and the relevant observer-side hook for reacting to deletion is the existing \`EventOnRemove\`.

The entry should be marked as ✅ shipped (mis-classified gap), with cross-links to where the functionality actually lives. A small ObserversManual.md callout should set expectations for future readers who come looking for an \"OnDelete observer event.\"

No code changes. Docs-only.

### Background (verified)

**Upstream:** In \`/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h\` lines 1932–1955, the kernel cleanly separates two categories:

- Lines 1932–1948 declare observer events: \`EcsOnAdd\`, \`EcsOnRemove\`, \`EcsOnSet\`, \`EcsMonitor\`, \`EcsOnTableCreate\`, \`EcsOnTableDelete\`.
- Lines 1950–1955 declare cleanup-policy relationship traits: \`EcsOnDelete\` and \`EcsOnDeleteTarget\`. The doc-comment text on those lines reads *\"Relationship used for specifying cleanup behavior\"* and *\"Relationship used to define what should happen when a target entity is deleted.\"* These are used as the **first element of a pair** with \`EcsRemove\` / \`EcsDelete\` / \`EcsPanic\` as the target.

No code path in upstream emits observer callbacks for \`EcsOnDelete\` / \`EcsOnDeleteTarget\` as event kinds. \`src/on_delete.c\` (the cleanup executor) contains zero \`flecs_emit\` / \`ecs_emit\` calls. Upstream's observer-side story for deletion is documented in \`docs/ObserversManual.md:2273\`: *\"When a parent and its children are deleted, \`OnRemove\` observers will be invoked for children first... This order is maintained for any relationship that has the \`(OnDeleteTarget, Delete)\` trait...\"* That is, the existing \`OnRemove\` observer event covers deletion-reactive observers; the cleanup-policy traits merely govern ordering and what gets removed.

**Go port:** Already aligned with upstream:

- \`observer.go:9–18\` defines exactly three event kinds — \`EventOnAdd\`, \`EventOnSet\`, \`EventOnRemove\`. The comment on line 17 is explicit: *\"EventOnRemove fires before a component is removed from an entity, including on entity deletion.\"*
- \`cleanup.go:5–106\` implements \`OnDelete\` / \`OnDeleteTarget\` strictly as cleanup-policy traits via \`policyOnDeleteDelete\`, \`policyOnDeleteTargetPanic\`, etc. flags matching upstream \`EcsIdOnDeleteDelete\` / \`EcsIdOnDeleteTargetPanic\` storage flags.
- \`world.go:63–64\` exposes \`OnDelete()\` / \`OnDeleteTarget()\` as built-in cleanup-policy trait entities (indices 12–13).

The Phase 15.0 cleanup-policy bundle (v0.32.0) already shipped the upstream-equivalent feature in full.

### Required changes

1. **\`docs/README.md\`** — rewrite the gap entry at line 152 from the misleading \"observer events\" framing to:
   - Mark ✅ shipped in v0.32.0 (Phase 15.0).
   - Note that upstream's \`EcsOnDelete\` / \`EcsOnDeleteTarget\` are cleanup-policy relationship traits, NOT observer events.
   - Cross-link to \`world.go:63–64\` (\`OnDelete()\` / \`OnDeleteTarget()\` built-in entities) and \`cleanup.go\` (policy machinery).
   - Note that the observer-side equivalent (\"react when an entity is deleted\") is the existing \`EventOnRemove\`, which fires before each component is removed during deletion (per upstream \`ObserversManual.md:2273\`).
   - Cross-link \`docs/ComponentTraits.md\` § OnDelete/OnDeleteTarget for the cleanup-policy API surface.

2. **\`docs/ObserversManual.md\`** — add a short callout (e.g., under the OnRemove section) clarifying that there are no dedicated \`OnDelete\` / \`OnDeleteTarget\` observer events in either upstream or Go-flecs; deletion is observable through \`EventOnRemove\`, and cleanup policies live separately under ComponentTraits.

3. No \`CHANGELOG.md\` entry needed (no behavior change). Optional: a one-line \"docs: corrected gap classification\" note under an unreleased section if convention requires it — defer to maintainer style.

## Constraints

- @CLAUDE.md — repo conventions; verify file references and doc-style.
- @docs/README.md — the file being corrected; line 152 is the misleading entry, line 153 is the genuine OnTableCreate/OnTableDelete gap that remains.
- @docs/ObserversManual.md — observer documentation; the new callout lands here.
- @docs/ComponentTraits.md — already contains the OnDelete/OnDeleteTarget cleanup-policy documentation that the corrected gap entry should cross-link to.
- @observer.go — line 17 comment is the authoritative source for the \"EventOnRemove covers deletion\" claim.
- @cleanup.go — the actual implementation of the cleanup-policy traits.
- @world.go — lines 63–64 expose \`OnDelete()\` / \`OnDeleteTarget()\` as built-in trait entities.
- Upstream reference: \`/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h\` lines 1932–1967 — the explicit two-category split.
- Upstream reference: \`/work/agents/claude/projects/SanderMertens/flecs/docs/ObserversManual.md:2273\` — the canonical statement that OnRemove (not OnDelete) is the deletion-reactive observer event.
- No code changes; docs-only. No new tests required. \`go vet\` / \`golangci-lint\` / \`go test ./... -race -count=3\` should already pass; just verify they still do post-edit.
