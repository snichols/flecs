# Contributing

## Building and testing

```sh
go test ./... -race && golangci-lint run
```

All tests must pass with `-race`. Coverage on the root `flecs` package must stay
at or above 90% (currently ~97%). The CI workflow (`.github/workflows/ci.yml`)
enforces the same checks on every PR.

## Benchmarks

```sh
# Run all benchmarks once
go test -bench=. -benchmem ./...

# Capture a baseline for statistical comparison
go test -bench=. -benchmem -count=10 ./... > baseline.txt

# Compare two baselines (install benchstat once)
go install golang.org/x/perf/cmd/benchstat@latest
benchstat before.txt after.txt
```

The CI `bench` job runs `go test -bench=. -benchmem -benchtime=100ms ./...` as
a smoke test — it verifies benchmarks compile and complete without panics. CI
numbers are NOT authoritative for performance work because the runner hardware
varies. Capture baselines on a dedicated, quiet machine instead. See `BENCH.md`
for the full benchmark index and recorded baseline numbers.

## Architecture

```
github.com/snichols/flecs          — root package; public API (World, queries, systems, …)
github.com/snichols/flecs/internal/
    ids/                           — leaf package: 64-bit ID type + pair encoding
    component/                     — TypeInfo, Registry, hook slots
    storage/
        entityindex/               — dense/sparse entity index + generation counters
        componentindex/            — component-ID → []Table reverse map
        table/                     — archetype Table: SoA column storage, edge cache
```

The root `flecs` package is the only public surface. Internal packages are
implementation details and their APIs may change without notice.

## Style

- Code must be `gofmt`-clean and pass `golangci-lint run` with the project's
  `.golangci.yml` (govet, staticcheck, errcheck, ineffassign, unused, gofmt,
  goimports).
- No new third-party dependencies.
- Exported symbols require godoc beginning with the symbol's name.
- Aim for coverage ≥ 90% on any file you touch.

## Automated PRs

This repository uses the `snichols/queued` workflow for autonomous pull requests.
Human PRs are welcome; open an issue first to discuss scope before writing code.
