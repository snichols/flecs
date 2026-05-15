package flecs

import "fmt"

// ParseErrorCode identifies the category of a parse error for programmatic discrimination.
// The zero value ErrCodeUnknown is used for legacy / unclassified errors.
type ParseErrorCode int

const (
	ErrCodeUnknown        ParseErrorCode = iota // zero value; no specific code
	ErrCodeExpectedIdent                        // expected an identifier or '('
	ErrCodeUnclosedParen                        // missing closing ')' or '"'
	ErrCodeUnboundVar                           // variable referenced but never bound
	ErrCodeCycle                                // variable dependency cycle
	ErrCodeUnknownIdent                         // identifier not found in world
	ErrCodeBadModifier                          // invalid postfix modifier (e.g. chained .Up.Cascade)
	ErrCodeBadCombination                       // invalid prefix/operator combination (e.g. ?!)
)

// ParseQueryError is returned by parseQueryExpr when the expression cannot
// be parsed or an identifier cannot be resolved.
//
// Pos is the rune offset (0-based) where the error was detected.
// Nearby is up to 5 runes of context starting at Pos.
// Code identifies the error category (ErrCodeUnknown for unclassified errors).
// Msg describes what went wrong.
type ParseQueryError struct {
	Pos    int
	Nearby string
	Code   ParseErrorCode
	Msg    string
}

func (e *ParseQueryError) Error() string {
	return fmt.Sprintf("query parse error at position %d (near %q): %s", e.Pos, e.Nearby, e.Msg)
}

// parseQueryExpr parses a Flecs Query Language v2 expression and resolves
// identifiers against w. Must be called from within a w.Read(fn) or w.Write(fn)
// scope so the name index is stable across all terms.
//
// v2 grammar additions over v1 (precedence high→low):
//
//	`?`                  optional prefix; `?!X` is an error
//	`!`                  negation; `!(A,B)` is a negated scope group
//	ident / pair         identifier or pair, with optional source-binding suffix
//	`.Up` `.SelfUp`      traversal postfixes on pair terms (default rel = pair's rel)
//	`.Cascade`           cascading traversal postfix on pair terms
//	`==` `!=` `~`        equality predicates with `$this` LHS
//	`||`                 OR (tighter than `,`)
//	`,`                  AND (term separator)
//
// Supported identifier forms:
//
//	Position              — component on $this
//	Position(hero)        — component on named entity (WithSourceTerm)
//	Position($var)        — component on query variable (WithVar)
//	(Rel, target)         — pair on $this; optional .Up/.SelfUp/.Cascade postfix
//	(Rel, $var)           — pair with variable target (WithPairTgtVar)
//	!(A, B)               — negated scope group (WithoutScope)
//	$this == hero         — entity equality predicate (IsEntity)
//	$this != hero         — entity inequality predicate (NotEntity)
//	$this == "name"       — name pattern match (NameMatches)
//	$this ~ "pattern"     — name substring match (NameMatches)
//	AndFrom(e)            — expand e's components as AND terms
//	OrFrom(e)             — expand e's components as OR terms
//	NotFrom(e)            — expand e's components as NOT terms
//
// Variable names start with `$`; the cap is 16 per query (mirrors engine cap).
// Returns an error with Pos, Nearby, and Code on any parse or resolution failure.
func parseQueryExpr(expr string, w *World) ([]Term, error) {
	if expr == "" {
		return nil, &ParseQueryError{Pos: 0, Nearby: "", Msg: "empty expression"}
	}
	p := &queryParser{
		runes: []rune(expr),
		w:     w,
		vars:  make(map[string]int),
	}
	return p.parseExpr()
}

// queryParser holds the parser state.
type queryParser struct {
	runes []rune
	pos   int
	w     *World
	vars  map[string]int // variable name → slot index (first-appearance order)
}

func (p *queryParser) nearby() string {
	start := p.pos
	if start > len(p.runes) {
		start = len(p.runes)
	}
	end := start + 5
	if end > len(p.runes) {
		end = len(p.runes)
	}
	return string(p.runes[start:end])
}

func (p *queryParser) errorf(msg string, args ...any) error {
	return &ParseQueryError{
		Pos:    p.pos,
		Nearby: p.nearby(),
		Msg:    fmt.Sprintf(msg, args...),
	}
}

func (p *queryParser) errorCode(code ParseErrorCode, msg string, args ...any) error {
	return &ParseQueryError{
		Pos:    p.pos,
		Nearby: p.nearby(),
		Code:   code,
		Msg:    fmt.Sprintf(msg, args...),
	}
}

func (p *queryParser) skipWhitespace() {
	for p.pos < len(p.runes) {
		r := p.runes[p.pos]
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			p.pos++
		} else {
			break
		}
	}
}

// parseIdent parses a dotted identifier: letter { letter | digit | "." }
func (p *queryParser) parseIdent() (string, bool) {
	if p.pos >= len(p.runes) {
		return "", false
	}
	r := p.runes[p.pos]
	if !isIdentStart(r) {
		return "", false
	}
	start := p.pos
	p.pos++
	for p.pos < len(p.runes) && isIdentContinue(p.runes[p.pos]) {
		p.pos++
	}
	return string(p.runes[start:p.pos]), true
}

// parseVarIdent parses a variable-name identifier: letter { letter | digit | "_" }
// (no dots — variable names may not contain path separators).
func (p *queryParser) parseVarIdent() (string, bool) {
	if p.pos >= len(p.runes) {
		return "", false
	}
	r := p.runes[p.pos]
	if !isIdentStart(r) {
		return "", false
	}
	start := p.pos
	p.pos++
	for p.pos < len(p.runes) {
		c := p.runes[p.pos]
		if c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			p.pos++
		} else {
			break
		}
	}
	return string(p.runes[start:p.pos]), true
}

func isIdentStart(r rune) bool {
	return r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
}

func isIdentContinue(r rune) bool {
	return r == '_' || r == '.' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

func (p *queryParser) resolveIdent(name string) (ID, error) {
	id, ok := p.w.Lookup(name)
	if !ok {
		return 0, &ParseQueryError{
			Pos:    p.pos,
			Nearby: p.nearby(),
			Code:   ErrCodeUnknownIdent,
			Msg:    fmt.Sprintf("unknown identifier %q", name),
		}
	}
	return id, nil
}

// registerVar registers a variable name and assigns it a slot index.
// Returns an error if the variable cap (16) is exceeded.
func (p *queryParser) registerVar(name string) error {
	if _, ok := p.vars[name]; ok {
		return nil
	}
	if len(p.vars) >= 16 {
		return p.errorCode(ErrCodeBadCombination, "query variable count exceeds the cap of 16")
	}
	p.vars[name] = len(p.vars)
	return nil
}

// parseStringLiteral parses a double-quoted string literal. pos must point at '"'.
func (p *queryParser) parseStringLiteral() (string, error) {
	if p.pos >= len(p.runes) || p.runes[p.pos] != '"' {
		return "", p.errorCode(ErrCodeExpectedIdent, `expected string literal starting with '"'`)
	}
	p.pos++ // consume opening '"'
	var buf []rune
	for {
		if p.pos >= len(p.runes) {
			return "", p.errorCode(ErrCodeUnclosedParen, "unclosed string literal")
		}
		r := p.runes[p.pos]
		if r == '"' {
			p.pos++ // consume closing '"'
			return string(buf), nil
		}
		if r == '\\' {
			p.pos++
			if p.pos >= len(p.runes) {
				return "", p.errorCode(ErrCodeUnclosedParen, "unclosed string literal after backslash")
			}
			switch p.runes[p.pos] {
			case '"':
				buf = append(buf, '"')
			case '\\':
				buf = append(buf, '\\')
			case 'n':
				buf = append(buf, '\n')
			case 't':
				buf = append(buf, '\t')
			default:
				buf = append(buf, '\\', p.runes[p.pos])
			}
			p.pos++
			continue
		}
		buf = append(buf, r)
		p.pos++
	}
}

// parseRelOrWild parses the relationship slot of a pair: ident or "*".
func (p *queryParser) parseRelOrWild() (ID, error) {
	p.skipWhitespace()
	if p.pos < len(p.runes) && p.runes[p.pos] == '*' {
		p.pos++
		return p.w.Wildcard(), nil
	}
	name, ok := p.parseIdent()
	if !ok {
		return 0, p.errorf("expected identifier or '*' for pair relationship")
	}
	return p.resolveIdent(name)
}

// parseTargetOrWild parses the target slot of a pair: ident, "*", or "_".
func (p *queryParser) parseTargetOrWild() (ID, error) {
	p.skipWhitespace()
	if p.pos >= len(p.runes) {
		return 0, p.errorf("expected pair target but reached end of expression")
	}
	switch p.runes[p.pos] {
	case '*':
		p.pos++
		return p.w.Wildcard(), nil
	case '_':
		p.pos++
		return p.w.Any(), nil
	}
	name, ok := p.parseIdent()
	if !ok {
		return 0, p.errorf("expected identifier, '*', or '_' for pair target")
	}
	return p.resolveIdent(name)
}

// parsePair parses "(rel, tgt)" where tgt may be a $var, ident, *, or _.
// On entry pos points at '('. On return pos is just past ')'.
// After ')' checks for .Up / .SelfUp / .Cascade traversal postfix.
func (p *queryParser) parsePair() (Term, error) {
	p.pos++ // consume '('
	rel, err := p.parseRelOrWild()
	if err != nil {
		return Term{}, err
	}
	p.skipWhitespace()
	if p.pos >= len(p.runes) || p.runes[p.pos] != ',' {
		return Term{}, p.errorCode(ErrCodeExpectedIdent, "expected ',' separating relationship and target in pair")
	}
	p.pos++ // consume ','
	p.skipWhitespace()

	var t Term
	if p.pos < len(p.runes) && p.runes[p.pos] == '$' {
		// Variable target: (Rel, $var) → WithPairTgtVar
		p.pos++ // consume '$'
		varName, ok := p.parseVarIdent()
		if !ok {
			return Term{}, p.errorCode(ErrCodeExpectedIdent, "expected variable name after '$' in pair target")
		}
		if varName == "this" {
			return Term{}, p.errorCode(ErrCodeBadCombination, "$this as pair target is not supported")
		}
		if err := p.registerVar(varName); err != nil {
			return Term{}, err
		}
		t = WithPairTgtVar(rel, varName)
	} else {
		tgt, err := p.parseTargetOrWild()
		if err != nil {
			return Term{}, err
		}
		t = With(MakePair(rel, tgt))
	}

	p.skipWhitespace()
	if p.pos >= len(p.runes) || p.runes[p.pos] != ')' {
		return Term{}, p.errorCode(ErrCodeUnclosedParen, "unclosed pair: expected ')'")
	}
	p.pos++ // consume ')'

	return p.parseTraversalPostfix(t, rel)
}

// parseTraversalPostfix checks for a .Up / .SelfUp / .Cascade postfix on a pair term.
// rel is the pair's relationship ID, used as the default traversal relation.
// Chains like .Up.Cascade are rejected with ErrCodeBadModifier.
func (p *queryParser) parseTraversalPostfix(t Term, rel ID) (Term, error) {
	p.skipWhitespace()
	if p.pos >= len(p.runes) || p.runes[p.pos] != '.' {
		return t, nil
	}

	dotPos := p.pos
	p.pos++ // consume '.'
	// Use parseVarIdent (no dots) so "Up.Cascade" is not consumed as one token.
	name, ok := p.parseVarIdent()
	if !ok || (name != "Up" && name != "SelfUp" && name != "Cascade") {
		p.pos = dotPos // backtrack: not a traversal modifier
		return t, nil
	}

	switch name {
	case "Up":
		t = t.Up(rel)
	case "SelfUp":
		t = t.SelfUp(rel)
	case "Cascade":
		t = t.Cascade(rel)
	}

	// Reject chained traversal modifiers
	p.skipWhitespace()
	if p.pos < len(p.runes) && p.runes[p.pos] == '.' {
		chainDotPos := p.pos
		p.pos++
		name2, ok2 := p.parseVarIdent()
		if ok2 && (name2 == "Up" || name2 == "SelfUp" || name2 == "Cascade") {
			return Term{}, p.errorCode(ErrCodeBadModifier,
				"chained traversal .%s.%s is invalid; use a single traversal modifier", name, name2)
		}
		p.pos = chainDotPos // backtrack
	}

	return t, nil
}

// parseScopeGroupBody parses !(and_list) — the '!' has already been consumed and
// pos points at '(' on entry. On return pos is just past ')'.
func (p *queryParser) parseScopeGroupBody() (Term, error) {
	p.pos++ // consume '('
	innerTerms, err := p.parseAndList(true)
	if err != nil {
		return Term{}, err
	}
	p.skipWhitespace()
	if p.pos >= len(p.runes) || p.runes[p.pos] != ')' {
		return Term{}, p.errorCode(ErrCodeUnclosedParen, "unclosed scope group: expected ')'")
	}
	p.pos++ // consume ')'
	if len(innerTerms) == 0 {
		return Term{}, p.errorf("empty scope group")
	}
	return WithoutScope(func(b *ScopeBuilder) {
		b.terms = append(b.terms, innerTerms...)
	}), nil
}

// parsePredicate parses the operator and RHS of a $this predicate.
// On entry pos is just past "$this".
//
//	$this == hero        → IsEntity(Lookup("hero"))
//	$this != hero        → NotEntity(Lookup("hero"))
//	$this == "name"      → NameMatches("name")
//	$this ~ "pattern"    → NameMatches("pattern")
func (p *queryParser) parsePredicate() (Term, error) {
	p.skipWhitespace()
	if p.pos >= len(p.runes) {
		return Term{}, p.errorCode(ErrCodeExpectedIdent, "expected ==, !=, or ~ after $this")
	}

	var op string
	switch {
	case p.pos+1 < len(p.runes) && p.runes[p.pos] == '=' && p.runes[p.pos+1] == '=':
		op = "=="
		p.pos += 2
	case p.pos+1 < len(p.runes) && p.runes[p.pos] == '!' && p.runes[p.pos+1] == '=':
		op = "!="
		p.pos += 2
	case p.runes[p.pos] == '~':
		op = "~"
		p.pos++
	default:
		return Term{}, p.errorCode(ErrCodeExpectedIdent, "expected ==, !=, or ~ after $this")
	}

	p.skipWhitespace()

	if op == "~" {
		// ~ requires a string literal RHS
		if p.pos >= len(p.runes) || p.runes[p.pos] != '"' {
			return Term{}, p.errorCode(ErrCodeExpectedIdent, "~ operator requires a string literal on the right-hand side")
		}
		pattern, err := p.parseStringLiteral()
		if err != nil {
			return Term{}, err
		}
		return NameMatches(pattern), nil
	}

	// == or != with string literal → name match
	if p.pos < len(p.runes) && p.runes[p.pos] == '"' {
		name, err := p.parseStringLiteral()
		if err != nil {
			return Term{}, err
		}
		if op == "!=" {
			return Term{}, p.errorCode(ErrCodeBadCombination,
				`$this != "string" is not supported; use $this ~ for name pattern matching`)
		}
		return NameMatches(name), nil
	}

	// == or != with identifier → entity lookup
	name, ok := p.parseIdent()
	if !ok {
		return Term{}, p.errorCode(ErrCodeExpectedIdent,
			"expected entity name or string literal after %s", op)
	}
	id, err := p.resolveIdent(name)
	if err != nil {
		return Term{}, err
	}
	if op == "==" {
		return IsEntity(id), nil
	}
	return NotEntity(id), nil
}

// parseFromCall parses "AndFrom(ent)", "OrFrom(ent)", or "NotFrom(ent)".
// funcName is the already-parsed keyword; pos is past it.
func (p *queryParser) parseFromCall(funcName string) (Term, error) {
	p.skipWhitespace()
	if p.pos >= len(p.runes) || p.runes[p.pos] != '(' {
		return Term{}, p.errorCode(ErrCodeExpectedIdent, "expected '(' after %s", funcName)
	}
	p.pos++ // consume '('
	p.skipWhitespace()

	name, ok := p.parseIdent()
	if !ok {
		return Term{}, p.errorCode(ErrCodeExpectedIdent, "expected entity name in %s()", funcName)
	}
	id, err := p.resolveIdent(name)
	if err != nil {
		return Term{}, err
	}

	p.skipWhitespace()
	if p.pos >= len(p.runes) || p.runes[p.pos] != ')' {
		return Term{}, p.errorCode(ErrCodeUnclosedParen, "unclosed %s(): expected ')'", funcName)
	}
	p.pos++ // consume ')'

	switch funcName {
	case "AndFrom":
		return AndFrom(id), nil
	case "OrFrom":
		return OrFrom(id), nil
	default:
		return NotFrom(id), nil
	}
}

// parseSourceBindingTerm parses "comp(src)" where compID is already resolved
// and pos points at '('. Handles entity names and $var sources.
//
//	Position(hero)   → WithSourceTerm(posID, heroID)
//	Position($var)   → WithVar(posID, "var")
//	Position($this)  → With(posID)          (explicit $this = default source)
func (p *queryParser) parseSourceBindingTerm(compID ID) (Term, error) {
	p.pos++ // consume '('
	p.skipWhitespace()

	if p.pos >= len(p.runes) {
		return Term{}, p.errorCode(ErrCodeUnclosedParen, "unclosed source binding: expected entity name or ')'")
	}

	if p.runes[p.pos] == '*' {
		return Term{}, p.errorCode(ErrCodeBadCombination, "wildcard source binding is not supported")
	}

	var t Term
	if p.runes[p.pos] == '$' {
		p.pos++ // consume '$'
		varName, ok := p.parseVarIdent()
		if !ok {
			return Term{}, p.errorCode(ErrCodeExpectedIdent, "expected variable name after '$' in source binding")
		}
		if varName == "this" {
			t = With(compID) // $this is the default source; no change
		} else {
			if err := p.registerVar(varName); err != nil {
				return Term{}, err
			}
			t = WithVar(compID, varName)
		}
	} else {
		name, ok := p.parseIdent()
		if !ok {
			return Term{}, p.errorCode(ErrCodeExpectedIdent, "expected entity name in source binding")
		}
		id, err := p.resolveIdent(name)
		if err != nil {
			return Term{}, err
		}
		t = WithSourceTerm(compID, id)
	}

	p.skipWhitespace()
	if p.pos >= len(p.runes) || p.runes[p.pos] != ')' {
		return Term{}, p.errorCode(ErrCodeUnclosedParen, "unclosed source binding: expected ')'")
	}
	p.pos++ // consume ')'
	return t, nil
}

// parsePrimaryTerm parses a primary query term (no prefix modifiers):
// predicate, pair, from-call, or identifier with optional source binding.
func (p *queryParser) parsePrimaryTerm() (Term, error) {
	p.skipWhitespace()
	if p.pos >= len(p.runes) {
		return Term{}, p.errorf("unexpected end of expression; expected term")
	}

	// $ → predicate ($this op rhs) or error
	if p.runes[p.pos] == '$' {
		savedPos := p.pos
		p.pos++ // consume '$'
		varName, ok := p.parseVarIdent()
		if !ok {
			p.pos = savedPos
			return Term{}, p.errorCode(ErrCodeExpectedIdent, "expected variable name after '$'")
		}
		if varName != "this" {
			return Term{}, p.errorCode(ErrCodeBadCombination,
				"$%s as a standalone term is not supported; use $var in a pair target or source binding", varName)
		}
		return p.parsePredicate()
	}

	// '(' → pair term
	if p.runes[p.pos] == '(' {
		return p.parsePair()
	}

	// Identifier: component, from-call, or component with source binding
	name, ok := p.parseIdent()
	if !ok {
		return Term{}, p.errorCode(ErrCodeExpectedIdent, "expected identifier or '(' to begin term")
	}

	switch name {
	case "AndFrom", "OrFrom", "NotFrom":
		return p.parseFromCall(name)
	}

	id, err := p.resolveIdent(name)
	if err != nil {
		return Term{}, err
	}

	p.skipWhitespace()
	if p.pos < len(p.runes) && p.runes[p.pos] == '(' {
		return p.parseSourceBindingTerm(id)
	}
	return With(id), nil
}

// parseTerm parses one query term with optional ? and ! prefixes.
//
//	?    → optional (TermOptional); must not be followed by !
//	!    → negation (TermNot); !(A,B) produces a scope group
//
// The modifier is applied after the primary is parsed; predicates and from-calls
// cannot be negated or made optional.
func (p *queryParser) parseTerm() (Term, error) {
	p.skipWhitespace()
	if p.pos >= len(p.runes) {
		return Term{}, p.errorf("unexpected end of expression; expected term")
	}

	// Optional prefix ?
	isOptional := false
	if p.runes[p.pos] == '?' {
		isOptional = true
		p.pos++
		p.skipWhitespace()
		if p.pos < len(p.runes) && p.runes[p.pos] == '!' {
			return Term{}, p.errorCode(ErrCodeBadCombination, "?! combination is invalid; ? must not be followed by !")
		}
	}

	// Negation prefix !
	isNegated := false
	if p.pos < len(p.runes) && p.runes[p.pos] == '!' {
		isNegated = true
		p.pos++
		p.skipWhitespace()
		// !(terms) → negated scope group
		if p.pos < len(p.runes) && p.runes[p.pos] == '(' {
			if isOptional {
				return Term{}, p.errorCode(ErrCodeBadCombination, "? cannot modify a scope group")
			}
			return p.parseScopeGroupBody()
		}
		if p.pos >= len(p.runes) {
			return Term{}, p.errorf("expected term after '!'")
		}
	}

	t, err := p.parsePrimaryTerm()
	if err != nil {
		return Term{}, err
	}

	if isNegated {
		if t.Kind != TermAnd {
			return Term{}, p.errorCode(ErrCodeBadCombination, "! can only negate a component or pair term")
		}
		t.Kind = TermNot
	}
	if isOptional {
		if t.Kind != TermAnd {
			return Term{}, p.errorCode(ErrCodeBadCombination, "? can only modify a component or pair term")
		}
		t.Kind = TermOptional
	}
	return t, nil
}

// parseOrGroup parses a `||`-separated group of terms.
// If multiple terms are present all TermAnd members are promoted to TermOr;
// other kinds (TermOptional, TermNot, etc.) are left unchanged — see docs for
// ?A || B semantics.
func (p *queryParser) parseOrGroup() ([]Term, error) {
	t, err := p.parseTerm()
	if err != nil {
		return nil, err
	}
	terms := []Term{t}

	for {
		p.skipWhitespace()
		if p.pos+1 >= len(p.runes) || p.runes[p.pos] != '|' || p.runes[p.pos+1] != '|' {
			break
		}
		p.pos += 2 // consume "||"
		t, err = p.parseTerm()
		if err != nil {
			return nil, err
		}
		terms = append(terms, t)
	}

	if len(terms) > 1 {
		for i := range terms {
			if terms[i].Kind == TermAnd {
				terms[i].Kind = TermOr
			}
		}
	}
	return terms, nil
}

// parseAndList parses a comma-separated list of or-groups.
// If insideScope is true the list also stops at ')' (scope group close),
// otherwise any non-',' non-EOF token is an error.
func (p *queryParser) parseAndList(insideScope bool) ([]Term, error) {
	orTerms, err := p.parseOrGroup()
	if err != nil {
		return nil, err
	}
	result := orTerms

	for {
		p.skipWhitespace()
		if p.pos >= len(p.runes) {
			break
		}
		if insideScope && p.runes[p.pos] == ')' {
			break
		}
		if p.runes[p.pos] != ',' {
			if insideScope {
				return nil, p.errorf("expected ',' or ')' in scope group")
			}
			return nil, p.errorf("expected ',' or end of expression")
		}
		p.pos++ // consume ','
		more, err := p.parseOrGroup()
		if err != nil {
			return nil, err
		}
		result = append(result, more...)
	}
	return result, nil
}

// checkVarCycles detects cycles in the variable dependency graph using DFS.
// deps maps each variable name to the list of variable names it depends on
// (must-be-bound-before). Returns ParseQueryError with ErrCodeCycle if a cycle
// is found; nil otherwise.
func checkVarCycles(deps map[string][]string) error {
	visited := make(map[string]bool, len(deps))
	inStack := make(map[string]bool, len(deps))

	var hasCycle func(v string) bool
	hasCycle = func(v string) bool {
		if inStack[v] {
			return true
		}
		if visited[v] {
			return false
		}
		visited[v] = true
		inStack[v] = true
		for _, dep := range deps[v] {
			if hasCycle(dep) {
				return true
			}
		}
		inStack[v] = false
		return false
	}

	for v := range deps {
		if hasCycle(v) {
			return &ParseQueryError{
				Code: ErrCodeCycle,
				Msg:  "variable dependency cycle detected",
			}
		}
	}
	return nil
}

// parseExpr parses the full expression from the current position to EOF
// and returns the term list.
func (p *queryParser) parseExpr() ([]Term, error) {
	return p.parseAndList(false)
}
