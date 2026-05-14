## Goal

Reclassify the access-modifiers entry at `docs/README.md:115` from "gap" to "deliberately not applicable." Similar in spirit to issue #191 (which corrected `OnDelete`/`OnDeleteTarget` from "not-yet-ported observer events" to "already shipped as cleanup-policy traits"), this entry already explains *why* the feature doesn't apply to Go-flecs, but is still framed as a gap. It should be reframed as an intentional non-port and cross-linked to a new callout in `docs/Queries.md` that explains the model.

### Background (verified)

**Current entry — `docs/README.md:115`:**

> *"Access modifiers on query terms — `In` / `InOut` / `Out` / `None` per-term annotations used by the C scheduler for pipeline sync-point inference. Go flecs governs mutation via `Read`/`Write` world scopes; per-term annotations are not ported."*

The body of the entry already concedes the feature is structurally redundant with the Go port's mutation-scoping model:

- Upstream uses per-term `In`/`InOut`/`Out`/`None` annotations so the C scheduler can infer pipeline sync points (which systems read what, which write what, where barriers go).
- Go-flecs governs mutation at the `w.Read(fn)` / `w.Write(fn)` scope level — every Write block is implicitly read-write for everything inside it. Parallelism is by-stage (Phase 12.1) and by-explicit-worker (Phase 16.27 `RunSystemWorker`), not per-term.
- Per-term access annotations would therefore be redundant with the existing scoping invariants.

This is the same structural pattern as issue #191's `OnDelete`/`OnDeleteTarget` correction: a feature that looked like a port-gap on intake but, on re-read, is an intentional design divergence where Go-flecs's model already covers the upstream use case via a different mechanism.

**Existing callout — `docs/Queries.md:1483`:** already contains the same explanation; should be elevated to a dedicated, cross-linkable section that the README entry points at.

### Required changes

1. **`docs/README.md`** — rewrite the entry at line 115 from "gap" framing to:

   > **Access modifiers (`In`/`InOut`/`Out`/`None`)** — **N/A by design.** Upstream uses per-term access modifiers for pipeline sync-point inference. Go-flecs governs mutation at the `w.Write(fn)` scope level — every Write block is implicitly read-write for everything inside it, and parallelism is by-stage (Phase 12.1) and by-explicit-worker (Phase 16.27 `RunSystemWorker`). Per-term annotations would be redundant. Cross-link: `docs/Queries.md` access-modifier callout.

   Match the surrounding tone (e.g., the ✅ shipped vs. deferred entries); consider an N/A glyph or "N/A by design" prefix consistent with other deliberate divergences if such precedent exists, else introduce the convention here.

2. **`docs/Queries.md`** — promote the existing `docs/Queries.md:1483` callout into a properly-anchored section (e.g., `## Access modifiers — N/A by design`) so the README cross-link has a stable target. Content should explain:
   - What the upstream feature is (per-term `In`/`InOut`/`Out`/`None` for pipeline sync-point inference).
   - Why Go-flecs doesn't need it (Read/Write scope-level mutation governance; by-stage and by-explicit-worker parallelism).
   - That this is final, not deferred — per-term annotations would be redundant with the existing scoping invariants.

3. **Roadmap table** — verified: `docs/ROADMAP.md` does not list access modifiers; no change needed. (If a maintainer-only internal tracker lists it, reclassify there.)

4. No `CHANGELOG.md` entry needed (no behavior change). If convention requires, a one-line "docs: reclassify access modifiers from gap to N/A by design" note may ride along with the next feature CHANGELOG entry.

## Verification

Pre-edit grep (already run during issue prep) confirms exactly two callsites:

```
docs/README.md:115:- **Access modifiers on query terms** — `In` / `InOut` / `Out` / `None` ...
docs/Queries.md:1483:**Access modifiers** — `In` / `InOut` / `Out` / `None` ...
```

Post-edit, re-run `git ls-files docs/` and `grep -rn "access modifier\|Access modifier\|InOut\|In/Out" docs/` to confirm both have been updated consistently and that no stale "gap" framing remains.

## Mechanical acceptance

- `go vet ./...` clean (no code changes expected)
- `go test ./... -race -count=1` passes (no test changes expected)
- No new CHANGELOG entry required; optional brief mention in next feature CHANGELOG entry is acceptable.

## Non-goals

- Do **NOT** introduce per-term access modifiers as a Go-side feature. The N/A classification is final.
- Do **NOT** modify the existing `Read`/`Write` API.

## Constraints

- @docs/README.md — line 115 is the misleading entry; matches the issue-#191 pattern of "gap that's really an intentional divergence."
- @docs/Queries.md — line 1483 already explains the model; promote to a proper anchored section so the README can cross-link to it.
- @CLAUDE.md — repo conventions; verify file references and doc style match existing N/A-by-design entries where they exist.
- Reference: issue #191 — same docs-correction pattern (gap → "already covered by existing mechanism").
