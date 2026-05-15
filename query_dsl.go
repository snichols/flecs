package flecs

import "fmt"

// ParseQueryError is returned by parseQueryExpr when the expression cannot
// be parsed or an identifier cannot be resolved.
//
// Pos is the rune offset (0-based) where the error was detected.
// Nearby is up to 5 runes of context starting at Pos.
// Msg describes what went wrong.
type ParseQueryError struct {
	Pos    int
	Nearby string
	Msg    string
}

func (e *ParseQueryError) Error() string {
	return fmt.Sprintf("query parse error at position %d (near %q): %s", e.Pos, e.Nearby, e.Msg)
}

// parseQueryExpr parses a Flecs Query Language v1 expression and resolves
// identifiers against w. Must be called from within a w.Read(fn) or w.Write(fn)
// scope so the name index is stable across all terms.
//
// Supported syntax:
//
//	expr     = term { "," term }
//	term     = ["!"] prim
//	prim     = "(" rel "," target ")" | ident
//	rel      = ident | "*"
//	target   = ident | "*" | "_"
//	ident    = letter { letter | digit | "." }
//	letter   = "A".."Z" | "a".."z" | "_"
//	digit    = "0".."9"
//
// "*" resolves to w.Wildcard(); "_" resolves to w.Any().
// "!" before a term produces a Without (TermNot) term.
// Dotted identifiers are resolved via w.Lookup (mirrors name.go Lookup semantics).
//
// Returns an error with Pos and Nearby set on any parse or resolution failure.
func parseQueryExpr(expr string, w *World) ([]Term, error) {
	if expr == "" {
		return nil, &ParseQueryError{Pos: 0, Nearby: "", Msg: "empty expression"}
	}
	p := &queryParser{runes: []rune(expr), w: w}
	return p.parseExpr()
}

// queryParser holds the parser state.
type queryParser struct {
	runes []rune
	pos   int
	w     *World
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
			Msg:    fmt.Sprintf("unknown identifier %q", name),
		}
	}
	return id, nil
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

// parsePair parses "(rel, target)" starting just after the opening "(".
// On entry p.pos points at the '(' character; on return p.pos is just past ')'.
func (p *queryParser) parsePair() (Term, error) {
	p.pos++ // consume '('
	rel, err := p.parseRelOrWild()
	if err != nil {
		return Term{}, err
	}
	p.skipWhitespace()
	if p.pos >= len(p.runes) || p.runes[p.pos] != ',' {
		return Term{}, p.errorf("expected ',' separating relationship and target in pair")
	}
	p.pos++ // consume ','
	tgt, err := p.parseTargetOrWild()
	if err != nil {
		return Term{}, err
	}
	p.skipWhitespace()
	if p.pos >= len(p.runes) || p.runes[p.pos] != ')' {
		return Term{}, p.errorf("unclosed pair: expected ')'")
	}
	p.pos++ // consume ')'
	return With(MakePair(rel, tgt)), nil
}

// parseTerm parses one query term: ["!"] ("(" pair ")" | ident).
func (p *queryParser) parseTerm() (Term, error) {
	p.skipWhitespace()
	if p.pos >= len(p.runes) {
		return Term{}, p.errorf("unexpected end of expression; expected term")
	}

	negate := false
	if p.runes[p.pos] == '!' {
		negate = true
		p.pos++
		p.skipWhitespace()
		if p.pos >= len(p.runes) {
			return Term{}, p.errorf("expected term after '!'")
		}
	}

	var t Term
	var err error
	if p.runes[p.pos] == '(' {
		t, err = p.parsePair()
	} else {
		name, ok := p.parseIdent()
		if !ok {
			return Term{}, p.errorf("expected identifier or '(' to begin term")
		}
		var id ID
		id, err = p.resolveIdent(name)
		if err != nil {
			return Term{}, err
		}
		t = With(id)
	}
	if err != nil {
		return Term{}, err
	}

	if negate {
		t = Without(t.ID)
	}
	return t, nil
}

// parseExpr parses a comma-separated list of terms.
func (p *queryParser) parseExpr() ([]Term, error) {
	t, err := p.parseTerm()
	if err != nil {
		return nil, err
	}
	terms := []Term{t}

	for {
		p.skipWhitespace()
		if p.pos >= len(p.runes) {
			break
		}
		if p.runes[p.pos] != ',' {
			return nil, p.errorf("expected ',' or end of expression")
		}
		p.pos++ // consume ','
		t, err = p.parseTerm()
		if err != nil {
			return nil, err
		}
		terms = append(terms, t)
	}
	return terms, nil
}
