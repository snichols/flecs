## Symptom

Phase 16.13 (v0.68.0, commit `47cac29`) accidentally committed two coverage-report HTML files to the repo:

- `cov.html` (14,775 lines)
- `coverage_dyn.html` (14,766 lines)

Combined: ~29,500 LOC of `go tool cover` output. They account for ~31,000 of the 31,259 insertions in the v0.68.0 diff.

## Root cause

The `.gitignore` patterns broadened in #165 cover the per-package convention `<pkg>_cover.html` / `<pkg>_coverage.html`, but NOT:

- Files like `cov.html` (no underscore prefix; just `cov`).
- Files like `coverage_dyn.html` (where `coverage_` is the prefix and `dyn` is the disambiguator — fails to match `*_coverage.html`).

The iterate loop's coverage step is producing files with new naming conventions; each new convention escapes the existing patterns. This is a recurrence of the pattern fixed in #165 — the prior broadening wasn't exhaustive enough.

## Audit (snapshot at time of filing)

`git ls-files | grep -iE '\.(html|out)$'` returns:

```
cov.html
coverage_dyn.html
```

`git ls-files | grep -E '(cov|coverage)'` returns:

```
cov.html
coverage_boost_test.go
coverage_dyn.html
```

Note `coverage_boost_test.go` is a legitimate tracked Go test file — flagging this for deliverable (2) because the originally-proposed pattern `*coverage*` would match it as a footgun for future contributors. See deliverable (2) note.

## Deliverables

### 1. Delete tracked coverage files

`git rm cov.html coverage_dyn.html` + commit.

### 2. Broaden `.gitignore` to be exhaustive

Replace the existing per-package patterns with patterns that catch the entire family. The originally-proposed patterns were:

```
# Coverage reports — exhaustive patterns
*cov*.html
*cov*.out
*coverage*
```

**Important refinement:** `*coverage*` (no extension constraint) would match `coverage_boost_test.go`, which is a legitimately tracked test file. While `.gitignore` does not un-track files that are already tracked, this would silently hide the file from future contributors (e.g., a fresh clone where someone deletes and re-adds it, or new test files following the same `coverage_*_test.go` convention).

Suggested safer patterns:

```
# Coverage reports — exhaustive patterns
*cov*.html
*cov*.out
*coverage*.html
*coverage*.out
*coverage*.txt
```

Rationale:
- `*cov*.html` catches `cov.html`, `cover.html`, `coverage.html`, `pkg_cover.html`, `cov_dyn.html`, etc.
- `*cov*.out` catches `cov.out`, `cover.out`, `pkg_cover.out`.
- `*coverage*.{html,out,txt}` catches `coverage.html`, `coverage_dyn.html`, `coverage.txt`, etc. — extension-constrained to avoid colliding with Go source files like `coverage_boost_test.go`.

Implementing agent: confirm the final set with the audit in deliverable (3). If you can argue `*coverage*` is fine because `.gitignore` won't un-track and the convention is unlikely to recur, that's defensible — but document the choice.

### 3. Verify no other coverage artifacts are tracked

After the gitignore change, run:

```
git ls-files | grep -iE '\.(html|out)$'
git ls-files | grep -E '(cov|coverage)'
```

Confirm only legitimately-tracked files remain (expected: zero HTML/out matches; `coverage_boost_test.go` remains tracked).

### 4. No code or docs changes

Beyond the gitignore + file deletion. No CHANGELOG entry needed.

## Mechanical acceptance

- `git ls-files | grep -iE '\.(html|out)$'` returns empty.
- `go vet ./...` clean.
- `golangci-lint run` clean.
- `go test ./... -race -count=1` passes (non-functional change; `-count=1` is sufficient).

## Non-goals

- Do NOT modify the iterate loop's coverage-generation step.
- Do NOT rewrite git history.
- Do NOT add a generic `*.html` rule — the project may one day have intentional HTML files.

## References

- Issue #165 — previous housekeeping. This issue is the recurrence of a pattern that prior gitignore broadening didn't fully prevent.
- Commit `47cac29` (Phase 16.13, v0.68.0) — where the offending files landed.
