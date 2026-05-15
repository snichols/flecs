# Query DSL — Flecs Query Language v1 Reference

Go flecs v0.95.0 (Phase 16.40) ships a Flecs Query Language v1 subset parser used by the
`GET /query?expr=` REST endpoint. This document is the authoritative reference for the
supported syntax, resolution semantics, and error format.

---

## Table of Contents

1. [Overview](#overview)
2. [Supported tokens](#supported-tokens)
3. [Grammar](#grammar)
4. [Term kinds](#term-kinds)
5. [Identifier resolution](#identifier-resolution)
6. [Pair terms](#pair-terms)
7. [Wildcards](#wildcards)
8. [Error semantics](#error-semantics)
9. [Examples](#examples)
10. [Deferred features (v2)](#deferred-features-v2)

---

## Overview

The DSL parser (`parseQueryExpr`) converts a string expression into a `[]Term` slice that
can be passed directly to `NewQueryFromTerms`. It is designed to be called inside a
`World.Read(fn)` scope so identifier lookups see a stable name index across all terms.

The v1 parser is deliberately narrow — it covers the minimum surface required by the
FlecsExplorer query editor. Richer constructs (OR, scope groups, variables, source binding)
are explicitly deferred to v2; see [Deferred features](#deferred-features-v2).

---

## Supported tokens

| Token | Meaning |
|-------|---------|
| `Identifier` | Bare name (e.g. `Position`) or dot-separated path (e.g. `game.Position`). Resolved via `World.Lookup`. |
| `,` | AND — separates terms. Whitespace around `,` is ignored. |
| `!` | NOT prefix — negates the following term (`!Disabled`). |
| `(R, T)` | Pair term — `R` is the relationship, `T` is the target. |
| `*` | Wildcard — resolves to `World.Wildcard()`. Valid in either slot of a pair. |
| `_` | Any — resolves to `World.Any()`. Valid only as the target of a pair. |

Identifier characters: `[A-Za-z_][A-Za-z0-9_.]*`. Digits and uppercase are allowed
after the first character. The `.` character is allowed within an identifier and is
treated as the path separator (see [Identifier resolution](#identifier-resolution)).

---

## Grammar

```
expr     = term { "," term }
term     = [ "!" ] prim
prim     = "(" rel "," target ")" | ident
rel      = ident | "*"
target   = ident | "*" | "_"
ident    = letter { letter | digit | "." }
letter   = "A".."Z" | "a".."z" | "_"
digit    = "0".."9"
```

Whitespace (space, tab, newline, carriage return) is skipped freely between all tokens.

---

## Term kinds

| Expression | Term kind | `Term.Kind` |
|------------|-----------|-------------|
| `Position` | AND — entity must have the component | `TermAnd` |
| `Position, Velocity` | AND × 2 — must have both | `TermAnd` |
| `!Disabled` | NOT — entity must NOT have the component | `TermNot` |
| `(ChildOf, parent)` | AND pair — entity must have this exact pair | `TermAnd` |
| `!Position` | NOT — excluded from results | `TermNot` |

---

## Identifier resolution

Every identifier token is resolved via `World.Lookup(name)`, which supports:

- **Simple names** — `"Position"` → looks up a root-scope entity named `"Position"`.
- **Dotted paths** — `"game.Position"` → equivalent to `World.LookupChild(game, "Position")`.
- **Built-in names** — `"ChildOf"`, `"IsA"`, `"Disabled"`, `"Prefab"`, `"Wildcard"`, `"Any"` are resolved from an in-memory index (`builtinByName`) without requiring a `Name` component on the built-in entities.

An unknown identifier produces a `400 Bad Request` response whose body includes the
offending name (see [Error semantics](#error-semantics)).

---

## Pair terms

`(R, T)` constructs a pair ID via `MakePair(relID, tgtID)` and wraps it in a `With` term.
Negation works the same as for component terms: `!(R, T)` produces a `Without` term.

Valid pair combinations:

| `R` slot | `T` slot | Meaning |
|----------|----------|---------|
| named entity | named entity | Exact pair match |
| `*` (Wildcard) | named entity | Any relationship to this target |
| named entity | `*` (Wildcard) | This relationship to any target |
| `*` | `*` | Any pair |
| named entity | `_` (Any) | This relationship to any ONE target (at most one match per entity) |

Wildcard pairs (`*` in either slot) are excluded from the `fields` map in the response
because the concrete target entity is unknown statically.

---

## Wildcards

- **`*`** — `World.Wildcard()` — matches ALL entities or pairs carrying the matched ID.
- **`_`** — `World.Any()` — like Wildcard but returns AT MOST ONE match per entity per
  archetype table. Use `_` when you want to know whether a relationship exists at all,
  not to enumerate all targets.

Both are valid in the relationship slot as well as the target slot of a pair.

---

## Error semantics

Parse and resolution errors are returned as `*ParseQueryError`:

```go
type ParseQueryError struct {
    Pos    int    // 0-based rune offset where the error was detected
    Nearby string // up to 5 runes of context starting at Pos
    Msg    string // human-readable description
}
```

The REST endpoint surfaces this as a `400 Bad Request` with body:

```
query parse error at position 9 (near "XYZ"): unknown identifier "XYZ"
```

The `Pos` field lets the FlecsExplorer UI pin an error marker at the exact character
offset in the query input.

---

## Examples

### Bare component

```
Position
```

Returns all entities with a `Position` component.

### AND

```
Position, Velocity
```

Returns entities with both `Position` and `Velocity`.

### NOT

```
Position, !Disabled
```

Returns entities with `Position` that do NOT have the `Disabled` tag.

### Pair (exact)

```
(ChildOf, parent)
```

Returns entities whose `ChildOf` relationship points exactly to `parent`.

### Pair (wildcard target)

```
(ChildOf, *)
```

Returns all entities with any `ChildOf` relationship (all children).

### Pair (Any target)

```
(ChildOf, _)
```

Returns entities that have at least one `ChildOf` relationship — at most one match per
entity per archetype table.

### Dotted-path component

```
game.Position
```

Resolves `game.Position` as a two-segment path: looks up `game` in the root scope, then
finds `Position` as a child of `game`.

### Mixed expression

```
game.Position, !Disabled, (ChildOf, scene)
```

Returns entities with `game.Position`, no `Disabled` tag, and a `ChildOf` → `scene` relationship.

---

## Deferred features (v2)

The following Flecs Query Language constructs are intentionally NOT supported in v1.
They are documented here so future contributors do not redesign from scratch.

| Feature | Upstream syntax | Notes |
|---------|----------------|-------|
| OR | `Position \|\| Velocity` | Complex term combinator; requires alternative iterator paths. |
| Negated scope groups | `!(Position, *)` | Group-level NOT; needs compound term nodes. |
| Transitive walk | `(IsA, *, Up)` / `(ChildOf, root, Cascade)` | Requires per-term traversal modifier. |
| Equality predicates | `$this == "name"` | Entity identity check via `World.Lookup`; use `IsEntity` directly in Go. |
| Source binding | `Position(e)` | Reads a component from a specific fixed entity, not `$this`. |
| Query variables | `$X` | Multi-hop relational join; `WithVar` / `WithPairTgtVar` in Go. |
| Term-level access modifiers | `[in] Position` / `[inout]` / `[out]` / `[none]` | Read/write intent for change detection. |
| Optional terms | `?Position` | `FieldMaybe` semantics; entity may or may not have the component. |
| `AndFrom` / `OrFrom` / `NotFrom` | `Position AND Prefab` | Set-from-entity combinators. |

Until v2 is implemented, use the Go API directly for these constructs:

```go
q := flecs.NewQueryFromTerms(w,
    flecs.With(posID),
    flecs.Without(disabledID),
    flecs.With(flecs.MakePair(w.ChildOf(), parentID)),
)
```
