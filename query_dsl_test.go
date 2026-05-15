package flecs

import (
	"errors"
	"strings"
	"testing"
)

// dslPos, dslVel, dslTag are test-local component types for DSL parser tests.
type dslPos struct{ X, Y float32 }
type dslVel struct{ DX, DY float32 }
type dslTag struct{}

func dslSetupWorld(t *testing.T) *World {
	t.Helper()
	w := New()
	posID := RegisterComponent[dslPos](w)
	w.SetName(posID, "Position")
	velID := RegisterComponent[dslVel](w)
	w.SetName(velID, "Velocity")
	tagID := RegisterComponent[dslTag](w)
	w.SetName(tagID, "Disabled")
	w.Write(func(fw *Writer) {
		parent := fw.NewEntity()
		w.SetName(parent, "parent")
	})
	return w
}

func dslParse(t *testing.T, w *World, expr string) ([]Term, error) {
	t.Helper()
	var terms []Term
	var parseErr error
	w.Read(func(_ *Reader) {
		terms, parseErr = parseQueryExpr(expr, w)
	})
	return terms, parseErr
}

func TestParse_SingleComponent(t *testing.T) {
	w := dslSetupWorld(t)
	terms, err := dslParse(t, w, "Position")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 1 {
		t.Fatalf("want 1 term, got %d", len(terms))
	}
	if terms[0].Kind != TermAnd {
		t.Errorf("want TermAnd, got %v", terms[0].Kind)
	}
}

func TestParse_TwoComponents_AND(t *testing.T) {
	w := dslSetupWorld(t)
	terms, err := dslParse(t, w, "Position, Velocity")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 2 {
		t.Fatalf("want 2 terms, got %d", len(terms))
	}
	for i, tm := range terms {
		if tm.Kind != TermAnd {
			t.Errorf("term[%d]: want TermAnd, got %v", i, tm.Kind)
		}
	}
}

func TestParse_NotPrefix(t *testing.T) {
	w := dslSetupWorld(t)
	terms, err := dslParse(t, w, "Position, !Disabled")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 2 {
		t.Fatalf("want 2 terms, got %d", len(terms))
	}
	if terms[0].Kind != TermAnd {
		t.Errorf("term[0]: want TermAnd, got %v", terms[0].Kind)
	}
	if terms[1].Kind != TermNot {
		t.Errorf("term[1]: want TermNot, got %v", terms[1].Kind)
	}
}

func TestParse_Pair(t *testing.T) {
	w := dslSetupWorld(t)
	terms, err := dslParse(t, w, "(ChildOf, parent)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 1 {
		t.Fatalf("want 1 term, got %d", len(terms))
	}
	if terms[0].Kind != TermAnd {
		t.Errorf("want TermAnd, got %v", terms[0].Kind)
	}
	if !terms[0].ID.IsPair() {
		t.Error("expected pair ID")
	}
}

func TestParse_PairWildcardTarget(t *testing.T) {
	w := dslSetupWorld(t)
	terms, err := dslParse(t, w, "(ChildOf, *)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 1 {
		t.Fatalf("want 1 term, got %d", len(terms))
	}
	if !terms[0].ID.IsPair() {
		t.Error("expected pair ID")
	}
	if terms[0].ID.Second() != ID(w.Wildcard().Index()) {
		t.Errorf("expected Wildcard as target, got %v", terms[0].ID.Second())
	}
}

func TestParse_PairAnyTarget(t *testing.T) {
	w := dslSetupWorld(t)
	terms, err := dslParse(t, w, "(ChildOf, _)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 1 {
		t.Fatalf("want 1 term, got %d", len(terms))
	}
	if !terms[0].ID.IsPair() {
		t.Error("expected pair ID")
	}
	if terms[0].ID.Second() != ID(w.Any().Index()) {
		t.Errorf("expected Any as target, got %v", terms[0].ID.Second())
	}
}

func TestParse_NestedSpaces(t *testing.T) {
	w := dslSetupWorld(t)
	terms, err := dslParse(t, w, " Position , ! Disabled ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 2 {
		t.Fatalf("want 2 terms, got %d", len(terms))
	}
	if terms[0].Kind != TermAnd {
		t.Errorf("term[0]: want TermAnd, got %v", terms[0].Kind)
	}
	if terms[1].Kind != TermNot {
		t.Errorf("term[1]: want TermNot, got %v", terms[1].Kind)
	}
}

func TestParse_EmptyExpr(t *testing.T) {
	w := dslSetupWorld(t)
	_, err := dslParse(t, w, "")
	if err == nil {
		t.Fatal("expected error for empty expression, got nil")
	}
	var pe *ParseQueryError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *ParseQueryError, got %T: %v", err, err)
	}
	if pe.Pos != 0 {
		t.Errorf("expected error at position 0, got %d", pe.Pos)
	}
}

func TestParse_TrailingComma(t *testing.T) {
	w := dslSetupWorld(t)
	_, err := dslParse(t, w, "Position,")
	if err == nil {
		t.Fatal("expected error for trailing comma, got nil")
	}
}

func TestParse_UnclosedParen(t *testing.T) {
	w := dslSetupWorld(t)
	_, err := dslParse(t, w, "(ChildOf, parent")
	if err == nil {
		t.Fatal("expected error for unclosed paren, got nil")
	}
}

func TestParse_UnknownIdentifier(t *testing.T) {
	w := dslSetupWorld(t)
	_, err := dslParse(t, w, "NonExistentXYZ")
	if err == nil {
		t.Fatal("expected error for unknown identifier, got nil")
	}
	if !strings.Contains(err.Error(), "NonExistentXYZ") {
		t.Errorf("error message should contain offending identifier, got: %v", err)
	}
}

func TestParse_DottedPath(t *testing.T) {
	w := New()
	w.Write(func(fw *Writer) {
		game := fw.NewEntity()
		w.SetName(game, "game")
		pos := fw.NewEntity()
		w.SetName(pos, "Position")
		AddID(fw, pos, MakePair(w.ChildOf(), game))
	})
	terms, err := dslParse(t, w, "game.Position")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 1 {
		t.Fatalf("want 1 term, got %d", len(terms))
	}
	if terms[0].Kind != TermAnd {
		t.Errorf("want TermAnd, got %v", terms[0].Kind)
	}
}

func TestParse_PairWithWildcardRel(t *testing.T) {
	w := dslSetupWorld(t)
	// (*, parent) — wildcard as relationship is valid
	terms, err := dslParse(t, w, "(*, parent)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 1 {
		t.Fatalf("want 1 term, got %d", len(terms))
	}
	if !terms[0].ID.IsPair() {
		t.Error("expected pair ID")
	}
	if terms[0].ID.First() != ID(w.Wildcard().Index()) {
		t.Errorf("expected Wildcard as relationship, got %v", terms[0].ID.First())
	}
}

func TestParse_MultipleNots(t *testing.T) {
	w := dslSetupWorld(t)
	terms, err := dslParse(t, w, "Position, !Disabled, !Velocity")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 3 {
		t.Fatalf("want 3 terms, got %d", len(terms))
	}
	if terms[0].Kind != TermAnd {
		t.Errorf("term[0]: want TermAnd, got %v", terms[0].Kind)
	}
	if terms[1].Kind != TermNot {
		t.Errorf("term[1]: want TermNot, got %v", terms[1].Kind)
	}
	if terms[2].Kind != TermNot {
		t.Errorf("term[2]: want TermNot, got %v", terms[2].Kind)
	}
}

// Additional error-path tests to improve coverage.

func TestParse_PairMissingComma(t *testing.T) {
	w := dslSetupWorld(t)
	// "(ChildOf parent)" — missing comma separator in pair
	_, err := dslParse(t, w, "(ChildOf parent)")
	if err == nil {
		t.Fatal("expected error for pair without comma, got nil")
	}
}

func TestParse_PairEmptyAfterOpen(t *testing.T) {
	w := dslSetupWorld(t)
	// "(" followed by non-ident
	_, err := dslParse(t, w, "(123)")
	if err == nil {
		t.Fatal("expected error for pair with invalid relationship, got nil")
	}
}

func TestParse_PairEmptyTarget(t *testing.T) {
	w := dslSetupWorld(t)
	// "(ChildOf,)" — missing target
	_, err := dslParse(t, w, "(ChildOf,)")
	if err == nil {
		t.Fatal("expected error for pair with empty target, got nil")
	}
}

func TestParse_BangAtEndOfInput(t *testing.T) {
	w := dslSetupWorld(t)
	_, err := dslParse(t, w, "Position,!")
	if err == nil {
		t.Fatal("expected error for trailing !, got nil")
	}
}

func TestParse_InvalidTokenAfterTerm(t *testing.T) {
	w := dslSetupWorld(t)
	// "Position Position" — two terms without comma
	_, err := dslParse(t, w, "Position Position")
	if err == nil {
		t.Fatal("expected error for missing comma between terms, got nil")
	}
}

func TestParse_PairUnknownRel(t *testing.T) {
	w := dslSetupWorld(t)
	_, err := dslParse(t, w, "(UnknownRel, parent)")
	if err == nil {
		t.Fatal("expected error for unknown relationship, got nil")
	}
}

func TestParse_PairUnknownTarget(t *testing.T) {
	w := dslSetupWorld(t)
	_, err := dslParse(t, w, "(ChildOf, UnknownTarget)")
	if err == nil {
		t.Fatal("expected error for unknown target, got nil")
	}
}

func TestParse_PairBareOpen(t *testing.T) {
	w := dslSetupWorld(t)
	// "(" alone — parseRelOrWild calls parseIdent with pos at EOF
	_, err := dslParse(t, w, "(")
	if err == nil {
		t.Fatal("expected error for bare open paren, got nil")
	}
}

func TestParse_PairTargetEOF(t *testing.T) {
	w := dslSetupWorld(t)
	// "(ChildOf," with nothing after comma — parseTargetOrWild hits EOF
	_, err := dslParse(t, w, "(ChildOf,")
	if err == nil {
		t.Fatal("expected error for pair with no target, got nil")
	}
}

func TestParse_NumericTerm(t *testing.T) {
	w := dslSetupWorld(t)
	// "42" — parseTerm: not '(' and parseIdent returns !ok (digit is not ident start)
	_, err := dslParse(t, w, "42")
	if err == nil {
		t.Fatal("expected error for numeric term, got nil")
	}
}
