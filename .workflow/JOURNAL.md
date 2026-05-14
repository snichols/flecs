## iterate iteration 1 (2026-05-14)

Deleted cov.html and coverage_dyn.html (accidentally committed in v0.68.0). Replaced narrow per-name .gitignore patterns with exhaustive globs (*cov*.html, *cov*.out, *coverage*.html, *coverage*.out, *coverage*.txt). All acceptance checks pass: git ls-files returns no .html/.out files, go vet/golangci-lint/go test -race -count=1 all clean.

