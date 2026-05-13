## Goal

Clean up a `go tool cover` HTML report that accidentally landed in the repo during the Phase 15.17 / v0.49.0 merge, and broaden `.gitignore` so the iterate loop's per-package coverage artifacts can never recur.

### Symptom

Merge commit `a52cdd6` (Phase 15.17 — With trait, v0.49.0) committed `with_cover.html` to the repo root — a 9,142-line `go tool cover -html` report (~480 KB). The file lives at `/work/agents/claude/projects/flecs/with_cover.html`.

### Root cause

`.gitignore` covers the canonical names (`cover.html`, `coverage.html`, `cover.out`, `coverage.txt`, plus a top-level `*.out`) but NOT the per-package shape `*_cover.html` that the iterate loop generated, presumably via something like:

```
go test -coverprofile=with_cover.out ./... && go tool cover -html=with_cover.out -o with_cover.html
```

The `*.out` line catches `with_cover.out`, but the matching HTML slipped through. Future trait phases will repeat this whenever the iterate loop names coverage files after the package under test.

### Deliverables

1. Delete `with_cover.html` from the repo. Verify with `git status`.
2. Broaden `.gitignore` — add directly under the existing `# Test coverage output` block:
   ```
   *_cover.html
   *_cover.out
   *_coverage.html
   *_coverage.out
   ```
   These catch the `<package>_cover.*` / `<package>_coverage.*` shapes the iterate loop produces.
3. Verify no other coverage artifacts are tracked: `git ls-files | grep -iE '(cover|coverage)' | grep -E '\.(html|out)$'`. Delete anything beyond `with_cover.html`. Report findings in the iteration notes. (Pre-flight at issue-file time: only `with_cover.html` matches.)
4. No code or docs changes beyond `.gitignore`. No CHANGELOG entry. No version bump — this is workflow hygiene, not user-visible behavior.

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean (should be unaffected — no Go changes)
- `go test ./... -race -count=1` passes (no need for `-count=3`; non-functional change)

### Non-goals

- Do NOT modify the iterate loop's coverage generation step — fixing `.gitignore` is the right layer.
- Do NOT add a generic `*.html` ignore — the project may intentionally ship `.html` (e.g., generated docs) later.
- Do NOT rewrite git history to scrub `with_cover.html` from `a52cdd6`. Accept the v0.49.0 bloat as already shipped; just prevent recurrence.

## Constraints

- @.gitignore — current "Test coverage output" block at lines 1-6 covers `*.out`, `coverage.txt`, `coverage.html`, `cover.html`, `cover.out`. New patterns must slot in under this block, not replace it.
- @CLAUDE.md — follow existing project conventions for minimal-diff hygiene cleanups.
