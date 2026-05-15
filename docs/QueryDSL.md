# Query DSL — Flecs Query Language v2 Reference

Go flecs v0.96.0 (Phase 16.41) ships a Flecs Query Language v2 parser used by the
`GET /query?expr=` REST endpoint. This document is the authoritative reference for the
supported syntax, resolution semantics, operator precedence, and error format.

---

## Table of Contents

1. [Overview](#overview)
2. [Supported tokens](#supported-tokens)
3. [Grammar](#grammar)
4. [Operator precedence](#operator-precedence)
5. [Term kinds](#term-kinds)
6. [Identifier resolution](#identifier-resolution)
7. [Pair terms](#pair-terms)
8. [Wildcards](#wildcards)
9. [OR operator](#or-operator)
10. [Scope groups](#scope-groups)
11. [Optional terms](#optional-terms)
12. [Traversal postfixes](#traversal-postfixes)
13. [Source binding](#source-binding)
14. [Query variables](#query-variables)
15. [Equality predicates](#equality-predicates)
16. [Type-list operators](#type-list-operators)
17. [Error semantics](#error-semantics)
18. [Parse error codes](#parse-error-codes)
19. [Examples](#examples)

---

## Overview

The DSL parser (`parseQueryExpr`) converts a string expression into a `[]Term` slice that
can be passed directly to `NewQueryFromTerms`. It is called inside a `World.Read(fn)` scope
so identifier lookups see a stable name index across all terms.

The v2 parser extends v1 with the full query surface: OR (`||`), negated scope groups
(`!(A,B)`), optional terms (`?Position`), traversal postfixes (`.Up`, `.SelfUp`, `.Cascade`),
source binding (`Position(hero)`), query variables (`$parent`), equality predicates
(`$this == hero`, `$this ~ "pattern"`), and type-list operators (`AndFrom`, `OrFrom`,
`NotFrom`).

---

## Supported tokens

| Token | Meaning |
|-------|---------|
| `Identifier` | Bare name (`Position`) or dotted path (`game.Position`). Resolved via `World.Lookup`. |
| `,` | AND — separates terms. |
| `\|\|` | OR — alternative: entity must match at least one. |
| `!` | NOT prefix — negates the following simple term or opens a scope group with `!(`. |
| `?` | Optional prefix — `FieldMaybe` semantics; entity may or may not have the component. |
| `(R, T)` | Pair term — relationship `R` with target `T`. |
| `*` | Wildcard — resolves to `World.Wildcard()`. |
| `_` | Any — resolves to `World.Any()`. Valid only as the target of a pair. |
| `$name` | Query variable — `$this` is the implicit entity variable; `$X` is a named variable. |
| `"..."` | String literal — used with the `~` name-match predicate. |
| `.Up` / `.SelfUp` / `.Cascade` | Traversal postfix on a pair or component term. |
| `AndFrom(e)` | Type-list expansion — require all components that `e` carries. |
| `OrFrom(e)` | Type-list expansion — require at least one component that `e` carries. |
| `NotFrom(e)` | Type-list expansion — exclude all components that `e` carries. |

Identifier characters: `[A-Za-z_][A-Za-z0-9_.]*`. The `.` within an identifier is the
path separator (see [Identifier resolution](#identifier-resolution)).

Whitespace (space, tab, newline, carriage return) is skipped freely between all tokens.

---

## Grammar

```
expr         = and-list
and-list     = or-list { "," or-list }
or-list      = term { "||" term }
term         = [ "?" ] simple-term
simple-term  = not-term | from-call | predicate | scope-group | prim-term
not-term     = "!" (scope-group | prim-term)
scope-group  = "!" "(" and-list ")"
from-call    = ("AndFrom"|"OrFrom"|"NotFrom") "(" ident ")"
predicate    = "$this" "==" ident
             | "$this" "!=" ident
             | "$this" "~"  string-literal
prim-term    = pair-term | tblvar-term | bound-term | ident-term
pair-term    = "(" rel-slot "," target ")" [ traversal ]
tblvar-term  = "$" var-ident ":" ident [ "(" "$this" ")" ]   ; table-kind variable
bound-term   = ident "(" source [ "," "$" var-ident ] ")"    ; optional var target
ident-term   = ident [ traversal ]
rel-slot     = "$" var-ident | ident | "*"                   ; variable or fixed rel
target       = var | ident | "*" | "_"
source       = "$this" | ident
var          = "$" var-ident
traversal    = "." ("Up"|"SelfUp"|"Cascade") [ "(" ident ")" ]
ident        = letter { letter | digit | "." }
var-ident    = letter { letter | digit }       ; no dots, no path separators
string-lit   = '"' { char | escape } '"'
escape       = "\" ( "n" | "t" | "\" | '"' )
letter       = "A".."Z" | "a".."z" | "_"
digit        = "0".."9"
```

---

## Operator precedence

Operators bind tightest to loosest:

| Level | Operator | Example |
|-------|----------|---------|
| 1 (tightest) | Term prefix: `?`, `!` | `?Position`, `!Disabled` |
| 1 | Traversal postfix: `.Up`, `.SelfUp`, `.Cascade` | `(ChildOf,root).Up` |
| 2 | OR: `\|\|` | `Position \|\| Velocity` |
| 3 (loosest) | AND: `,` | `Position, !Disabled` |

`!(A, B)` is a scope group (group-level NOT), not a pair-then-negation.

---

## Term kinds

| Expression | `Term.Kind` | Meaning |
|------------|-------------|---------|
| `Position` | `TermAnd` | Entity must have the component |
| `!Disabled` | `TermNot` | Entity must NOT have the component |
| `?Position` | `TermOptional` | Component may or may not be present |
| `Position \|\| Velocity` | `TermOr` × 2 | Must have at least one |
| `!(Position, *)` | `TermScope` (NOT) | Scope-level NOT; group must all be absent |
| `(ChildOf, parent)` | `TermAnd` | Exact pair match |
| `Position.Up` | `TermAnd` (.Up) | Match entity or any ancestor via ChildOf |
| `Position(hero)` | `TermAnd` (source) | Read Position from entity `hero`, not `$this` |
| `$this == hero` | `TermEq` | Entity identity equality predicate |
| `$this != hero` | `TermNotEq` | Entity identity inequality predicate |
| `$this ~ "pattern"` | `TermNameMatch` | Case-insensitive name substring match |
| `AndFrom(preset)` | `TermAndFrom` | Require all components carried by `preset` |
| `OrFrom(preset)` | `TermOrFrom` | Require at least one component carried by `preset` |
| `NotFrom(preset)` | `TermNotFrom` | Exclude all components carried by `preset` |

---

## Identifier resolution

Every identifier token is resolved via `World.Lookup(name)`, which supports:

- **Simple names** — `"Position"` → root-scope entity named `"Position"`.
- **Dotted paths** — `"game.Position"` → `World.LookupChild(game, "Position")`.
- **Built-in names** — `"ChildOf"`, `"IsA"`, `"Disabled"`, `"Prefab"`, `"Wildcard"`,
  `"Any"` are resolved from a built-in index without requiring a `Name` component.

An unknown identifier produces a `400 Bad Request` with error code `ErrCodeUnknownIdent`.

---

## Pair terms

`(R, T)` constructs a pair ID via `MakePair(relID, tgtID)`.
Negation works the same as for component terms: `!(R, T)` produces a `TermNot`.

Valid pair combinations:

| `R` slot | `T` slot | Meaning |
|----------|----------|---------|
| named entity | named entity | Exact pair match |
| `*` (Wildcard) | named entity | Any relationship to this target |
| named entity | `*` (Wildcard) | This relationship to any target |
| `*` | `*` | Any pair |
| named entity | `_` (Any) | This relationship to any ONE target |
| named entity | `$varName` | Variable-captured target |

`$this` is not allowed as a pair target (produces `ErrCodeBadCombination`).
Wildcard pairs are excluded from the `fields` map because the concrete target is unknown statically.

---

## Wildcards

- **`*`** — `World.Wildcard()` — matches ALL pairs carrying the matched ID.
- **`_`** — `World.Any()` — at most ONE match per entity per archetype table.

Both are valid in the relationship slot and the target slot of a pair.

---

## OR operator

`||` separates alternatives within a comma-separated AND list. An entity matches when it
satisfies at least one OR branch.

```
Position || Velocity
```

Returns entities that have `Position`, `Velocity`, or both.

```
Position || Velocity, !Disabled
```

Returns entities with `(Position OR Velocity) AND NOT Disabled`.

The REST handler executes OR-only queries by splitting into one sub-query per OR member
(each member promoted to `TermAnd`) and unioning entity IDs — see `restBuildExecSets`.

---

## Scope groups

`!(...)` negates a group of terms as a unit — the entity must match NONE of the enclosed
terms:

```
!(Position, Velocity)
```

Returns entities that have neither `Position` nor `Velocity`. (Different from
`!Position, !Velocity`, which is semantically equivalent here but is parsed as two
separate `TermNot` terms; scope groups can contain OR sub-expressions.)

Nested scopes are supported:

```
!(Position || Velocity)
```

Excludes entities that have `Position` or `Velocity` (or both).

An empty scope group (`!()`) is a parse error (`ErrCodeExpectedIdent`).

---

## Optional terms

A `?` prefix makes a term optional (`TermOptional` / `FieldMaybe` semantics):

```
Position, ?Velocity
```

Returns all entities with `Position`. If an entity also has `Velocity`, the `Velocity`
field appears in `fields`; otherwise the key is absent. Use `?fields=false` to suppress
field materialisation entirely.

Optional terms compose with traversal:

```
?Position.SelfUp
```

---

## Traversal postfixes

A `.Modifier` suffix after a component name or pair anchors the term to a relationship
traversal:

| Postfix | Go term constructor | Meaning |
|---------|--------------------|---------| 
| `.Up` | `With(id).Up(rel)` | Entity or any ancestor via `rel` (inherited, not locally required) |
| `.SelfUp` | `With(id).SelfUp(rel)` | Entity or any ancestor via `rel` (locally required OR inherited) |
| `.Cascade` | `With(id).Cascade(rel)` | Like SelfUp but tables visited root-first |

Default traversal relationship is `ChildOf`. To specify a different relationship, append
it in parentheses:

```
Position.Up(IsA)
(ChildOf, root).Up(IsA)
```

`.Cascade` requires a `CachedQuery`; using it in `GET /query?expr=` is a parse-accepted
but runtime-unsupported combination that returns 503 (the REST handler uses live queries).

Traversal postfixes are not valid on `AndFrom`/`OrFrom`/`NotFrom` calls, `$this`
predicates, or scope groups (`ErrCodeBadModifier`).

---

## Source binding

`ident(source)` reads a component from a fixed entity rather than `$this`:

```
Position(hero)
```

Reads `Position` from the entity named `hero`. The entity variable `$this` is the default
source — `Position($this)` is equivalent to `Position`.

The `source` slot accepts identifiers and `$this`; wildcards (`*`) are not allowed
(`ErrCodeBadCombination`).

Source binding composes with traversal:

```
Position(hero).Up
```

---

## Query variables

A `$varName` token captures a matched entity (or relationship/table) as a named variable,
enabling relational joins. Variable bindings are surfaced in the REST response under the
`"vars"` key of each result entry.

**Pair-target variable** (original form):
```
(ChildOf, $parent)
```
Captures the `ChildOf` target in `$parent`. The variable is accessible via `it.Var("parent")`.

**Relationship-slot variable** (Phase 16.45):
```
($Rel, hero)       -- relationship variable, fixed target
($Rel, $tgt)       -- both relationship and target are variables
```
Iterates all distinct relationships paired with `hero`. Go API: `WithPairRelVar("Rel", heroID)`, `WithPairBothVar("Rel", "tgt")`.

**Negative-variable constraint** (Phase 16.45):
```
(Velocity, $x), !Brake($this, $x)
```
The `!Brake($this, $x)` term filters out entities that have `(Brake, $x)` — `$x` must be bound by an earlier term. A free (unbound) variable in a negated term is a parse error (`ErrCodeUnboundNegativeVar`). Go API: `Without(brakeID).TgtVar("x")`.

**Table-kind variable** (Phase 16.45):
```
$T:Position($this)
```
`$T` binds to each archetype table containing `Position`; subsequent entity iteration runs within that table. Accessible via `it.VarTable("T")`. Go API: `WithTableVar("T")` + `With(posID)`.

`$this` is the implicit entity variable and is only valid as the subject of an equality predicate or as the first argument of a predicate form — it is not allowed as a pair target.

Variable names follow `var-ident` syntax (letters and digits only, no dots). Maximum 16 user variables per expression.

---

## Equality predicates

Three predicate forms filter `$this` by identity or name:

| Expression | Go term | Meaning |
|------------|---------|---------|
| `$this == hero` | `IsEntity(heroID)` | Entity must be `hero` exactly |
| `$this != hero` | `NotEntity(heroID)` | Entity must NOT be `hero` |
| `$this ~ "pattern"` | `NameMatches("pattern")` | Name contains `pattern` (case-insensitive) |

The `~` operator only accepts a string literal in double quotes. Using an unquoted
identifier (`$this ~ hero`) is a parse error (`ErrCodeBadCombination`).

`!=` with a string literal is also rejected (`ErrCodeBadCombination`).

Pure-predicate queries (no `TermAnd` anchor) are executed by prepending a synthetic
`With(nameID)` anchor so the engine can iterate named entities; the predicate terms then
filter the result.

---

## Type-list operators

`AndFrom`, `OrFrom`, and `NotFrom` expand a named entity's component list into a set of
term requirements at construction time. The source entity's `DontInherit` components
(`Prefab`, `Disabled`) are filtered out.

| Expression | Go term | Meaning |
|------------|---------|---------|
| `AndFrom(preset)` | `AndFrom(presetID)` | Require ALL components of `preset` |
| `OrFrom(preset)` | `OrFrom(presetID)` | Require at LEAST ONE component of `preset` |
| `NotFrom(preset)` | `NotFrom(presetID)` | Exclude ALL components of `preset` |

Syntax:

```
AndFrom(preset)
OrFrom(myGroup), !Disabled
```

`!` and `?` prefixes are not supported on type-list terms (`ErrCodeBadCombination`).
Traversal postfixes are not supported (`ErrCodeBadModifier`).

---

## Error semantics

Parse and resolution errors are returned as `*ParseQueryError`:

```go
type ParseQueryError struct {
    Code   ParseErrorCode // machine-readable category
    Pos    int            // 0-based rune offset where the error was detected
    Nearby string         // up to 5 runes of context starting at Pos
    Msg    string         // human-readable description
}
```

The REST endpoint surfaces this as a `400 Bad Request` with body:

```
query parse error at position 9 (near "XYZ"): unknown identifier "XYZ"
```

The `Pos` field lets a UI pin an error marker at the exact character offset in the input.

---

## Parse error codes

| Code constant | Numeric | When emitted |
|---------------|---------|--------------|
| `ErrCodeUnknown` | `0` | Fallback; unexpected parser internal state. |
| `ErrCodeExpectedIdent` | `1` | Identifier expected but not found (empty scope group, missing arg). |
| `ErrCodeUnclosedParen` | `2` | EOF before matching `)` or `"`. |
| `ErrCodeUnboundVar` | `3` | Named variable used as pair target with no defining source term. |
| `ErrCodeCycle` | `4` | Variable dependency cycle detected. |
| `ErrCodeUnknownIdent` | `5` | Identifier not found in world (unknown entity name). |
| `ErrCodeBadModifier` | `6` | Traversal postfix on a term kind that does not support it. |
| `ErrCodeBadCombination` | `7` | Illegal operator/term combination (e.g., `$this` as pair target, `*` as source, `~` with non-string, `!` on from-call). |
| `ErrCodeUnboundNegativeVar` | `8` | Variable in a negated term is not bound by any earlier positive term. |

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

### OR

```
Position || Velocity
```

Returns entities with `Position`, `Velocity`, or both.

### OR with AND

```
(Dangerous || Hostile), Position
```

Returns entities that are `Dangerous` or `Hostile` AND have `Position`.

### Scope group (NOT)

```
!(Position, Velocity)
```

Returns entities with neither `Position` nor `Velocity`.

### Optional term

```
Position, ?Velocity
```

Returns all entities with `Position`. Velocity data appears in `fields` only when present.

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

Returns entities that have at least one `ChildOf` relationship.

### Traversal Up

```
Position.Up
```

Returns entities (or their `ChildOf` ancestors) that carry `Position`.

### Traversal SelfUp

```
Position.SelfUp(IsA)
```

Returns entities that carry `Position` directly OR inherit it via IsA.

### Source binding

```
Position(hero)
```

Filters the result to entities where `hero` has `Position` (i.e., reads Position from
the fixed entity `hero` rather than from `$this`).

### Equality predicate — exact entity

```
$this == hero
```

Returns only the entity named `hero`.

### Equality predicate — name match

```
$this ~ "unit"
```

Returns entities whose name contains `"unit"` (case-insensitive).

### Variable capture

```
(ChildOf, $parent)
```

Returns all entities that have any `ChildOf` relationship, capturing the parent.

### Type-list AndFrom

```
AndFrom(prefab)
```

Returns entities that have every component carried by `prefab`.

### Dotted-path component

```
game.Position
```

Resolves as a two-segment path: `game` in root scope, `Position` as its child.

### Complex compound

```
game.Position, (Dangerous || Hostile), !Disabled
```

Returns entities with `game.Position`, either `Dangerous` or `Hostile`, and no `Disabled` tag.
