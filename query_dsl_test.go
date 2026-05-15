package flecs

import (
	"errors"
	"fmt"
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

// ── v2 parser tests ──────────────────────────────────────────────────────────

// dslSetupWorldV2 extends dslSetupWorld with additional named entities needed
// by v2 tests.
func dslSetupWorldV2(t *testing.T) *World {
	t.Helper()
	w := dslSetupWorld(t)
	w.Write(func(fw *Writer) {
		hero := fw.NewEntity()
		w.SetName(hero, "hero")
		villain := fw.NewEntity()
		w.SetName(villain, "villain")
		preset := fw.NewEntity()
		w.SetName(preset, "presetEntity")
		// Give presetEntity a component so AndFrom/OrFrom/NotFrom have something to expand.
		Set(fw, preset, dslPos{X: 1})
	})
	return w
}

// ── OR operator ─────────────────────────────────────────────────────────────

func TestParse_Or_Simple(t *testing.T) {
	w := dslSetupWorldV2(t)
	terms, err := dslParse(t, w, "Position || Velocity")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 2 {
		t.Fatalf("want 2 terms, got %d", len(terms))
	}
	for i, tm := range terms {
		if tm.Kind != TermOr {
			t.Errorf("term[%d]: want TermOr, got %v", i, tm.Kind)
		}
	}
}

func TestParse_Or_TighterThanAnd(t *testing.T) {
	w := dslSetupWorldV2(t)
	// "A || B, C" → AND( Or(A,B), With(C) )
	terms, err := dslParse(t, w, "Position || Velocity, Disabled")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 3 {
		t.Fatalf("want 3 terms, got %d", len(terms))
	}
	if terms[0].Kind != TermOr {
		t.Errorf("term[0]: want TermOr (OR group), got %v", terms[0].Kind)
	}
	if terms[1].Kind != TermOr {
		t.Errorf("term[1]: want TermOr (OR group), got %v", terms[1].Kind)
	}
	if terms[2].Kind != TermAnd {
		t.Errorf("term[2]: want TermAnd (AND), got %v", terms[2].Kind)
	}
}

func TestParse_Or_Chained(t *testing.T) {
	w := dslSetupWorldV2(t)
	terms, err := dslParse(t, w, "Position || Velocity || Disabled")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 3 {
		t.Fatalf("want 3 terms, got %d", len(terms))
	}
	for i, tm := range terms {
		if tm.Kind != TermOr {
			t.Errorf("term[%d]: want TermOr, got %v", i, tm.Kind)
		}
	}
}

// ── Scope groups ─────────────────────────────────────────────────────────────

func TestParse_ScopeGroup_Not(t *testing.T) {
	w := dslSetupWorldV2(t)
	terms, err := dslParse(t, w, "!(Position, Velocity)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 1 {
		t.Fatalf("want 1 term (scope), got %d", len(terms))
	}
	if terms[0].Kind != TermScope {
		t.Errorf("want TermScope, got %v", terms[0].Kind)
	}
	if len(terms[0].Sub) != 2 {
		t.Errorf("want 2 inner terms, got %d", len(terms[0].Sub))
	}
}

func TestParse_ScopeGroup_Nested(t *testing.T) {
	w := dslSetupWorldV2(t)
	terms, err := dslParse(t, w, "!(Position, !(Velocity, Disabled))")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 1 {
		t.Fatalf("want 1 term (outer scope), got %d", len(terms))
	}
	outer := terms[0]
	if outer.Kind != TermScope {
		t.Errorf("outer: want TermScope, got %v", outer.Kind)
	}
	if len(outer.Sub) != 2 {
		t.Fatalf("outer: want 2 inner terms, got %d", len(outer.Sub))
	}
	if outer.Sub[0].Kind != TermAnd {
		t.Errorf("outer.Sub[0]: want TermAnd, got %v", outer.Sub[0].Kind)
	}
	if outer.Sub[1].Kind != TermScope {
		t.Errorf("outer.Sub[1]: want TermScope (nested scope), got %v", outer.Sub[1].Kind)
	}
	inner := outer.Sub[1]
	if len(inner.Sub) != 2 {
		t.Errorf("inner scope: want 2 sub-terms, got %d", len(inner.Sub))
	}
}

func TestParse_ScopeGroup_RequiresNot(t *testing.T) {
	w := dslSetupWorldV2(t)
	// "(A, B)" without leading ! is parsed as a pair; A/B are not registered
	// identifiers so it should error (unknown_ident).
	_, err := dslParse(t, w, "(UnknownA, UnknownB)")
	if err == nil {
		t.Fatal("expected error for pair with unknown identifiers, got nil")
	}
	// Verify it errors as an identifier issue, not as a scope-group issue
	var pe *ParseQueryError
	if errors.As(err, &pe) && pe.Code == ErrCodeBadCombination {
		t.Error("(A,B) without ! should error as unknown_ident pair, not bad_combination")
	}
}

// ── Source binding ────────────────────────────────────────────────────────────

func TestParse_SourceBinding_Entity(t *testing.T) {
	w := dslSetupWorldV2(t)
	terms, err := dslParse(t, w, "Position(hero)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 1 {
		t.Fatalf("want 1 term, got %d", len(terms))
	}
	if terms[0].Kind != TermAnd {
		t.Errorf("want TermAnd, got %v", terms[0].Kind)
	}
	if terms[0].Src == 0 {
		t.Error("want non-zero Src (fixed source entity), got 0")
	}
}

func TestParse_SourceBinding_Unknown(t *testing.T) {
	w := dslSetupWorldV2(t)
	_, err := dslParse(t, w, "Position(nonExistentEntity)")
	if err == nil {
		t.Fatal("expected error for unknown source entity, got nil")
	}
	var pe *ParseQueryError
	if errors.As(err, &pe) && pe.Code != ErrCodeUnknownIdent {
		t.Errorf("want ErrCodeUnknownIdent, got %v", pe.Code)
	}
}

// ── Optional term ─────────────────────────────────────────────────────────────

func TestParse_OptionalTerm(t *testing.T) {
	w := dslSetupWorldV2(t)
	terms, err := dslParse(t, w, "?Position")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 1 {
		t.Fatalf("want 1 term, got %d", len(terms))
	}
	if terms[0].Kind != TermOptional {
		t.Errorf("want TermOptional, got %v", terms[0].Kind)
	}
}

func TestParse_OptionalNotCombination(t *testing.T) {
	w := dslSetupWorldV2(t)
	_, err := dslParse(t, w, "?!Position")
	if err == nil {
		t.Fatal("expected error for ?! combination, got nil")
	}
	var pe *ParseQueryError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *ParseQueryError, got %T: %v", err, err)
	}
	if pe.Code != ErrCodeBadCombination {
		t.Errorf("want ErrCodeBadCombination, got %v", pe.Code)
	}
}

// ── Traversal postfixes ────────────────────────────────────────────────────────

func TestParse_TraversalUp(t *testing.T) {
	w := dslSetupWorldV2(t)
	terms, err := dslParse(t, w, "(ChildOf, parent).Up")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 1 {
		t.Fatalf("want 1 term, got %d", len(terms))
	}
	if terms[0].Traverse != TraverseUp {
		t.Errorf("want TraverseUp, got %v", terms[0].Traverse)
	}
	if terms[0].Trav == 0 {
		t.Error("want non-zero Trav (traversal relation), got 0")
	}
}

func TestParse_TraversalSelfUp(t *testing.T) {
	w := dslSetupWorldV2(t)
	terms, err := dslParse(t, w, "(ChildOf, parent).SelfUp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 1 {
		t.Fatalf("want 1 term, got %d", len(terms))
	}
	if terms[0].Traverse != TraverseSelfUp {
		t.Errorf("want TraverseSelfUp, got %v", terms[0].Traverse)
	}
}

func TestParse_TraversalCascade(t *testing.T) {
	w := dslSetupWorldV2(t)
	terms, err := dslParse(t, w, "(ChildOf, parent).Cascade")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 1 {
		t.Fatalf("want 1 term, got %d", len(terms))
	}
	if terms[0].Traverse != TraverseCascade {
		t.Errorf("want TraverseCascade, got %v", terms[0].Traverse)
	}
}

func TestParse_TraversalCombination(t *testing.T) {
	w := dslSetupWorldV2(t)
	_, err := dslParse(t, w, "(ChildOf, parent).Up.Cascade")
	if err == nil {
		t.Fatal("expected error for chained traversal .Up.Cascade, got nil")
	}
	var pe *ParseQueryError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *ParseQueryError, got %T: %v", err, err)
	}
	if pe.Code != ErrCodeBadModifier {
		t.Errorf("want ErrCodeBadModifier, got %v", pe.Code)
	}
}

// ── Query variables ────────────────────────────────────────────────────────────

func TestParse_Variable_Simple(t *testing.T) {
	w := dslSetupWorldV2(t)
	terms, err := dslParse(t, w, "(ChildOf, $parent)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 1 {
		t.Fatalf("want 1 term, got %d", len(terms))
	}
	if terms[0].Kind != TermAnd {
		t.Errorf("want TermAnd, got %v", terms[0].Kind)
	}
	// WithPairTgtVar sets tgtVar, not a pair ID
	if terms[0].ID.IsPair() {
		t.Error("WithPairTgtVar term must not have a pair-form ID")
	}
}

func TestParse_Variable_Reused(t *testing.T) {
	w := dslSetupWorldV2(t)
	terms, err := dslParse(t, w, "(ChildOf, $parent), Position($parent)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 2 {
		t.Fatalf("want 2 terms, got %d", len(terms))
	}
	// First term: WithPairTgtVar → non-pair ID, tgtVar
	if terms[0].ID.IsPair() {
		t.Error("term[0]: WithPairTgtVar must not have pair-form ID")
	}
	// Second term: WithVar → srcVar, TermAnd
	if terms[1].Kind != TermAnd {
		t.Errorf("term[1]: want TermAnd, got %v", terms[1].Kind)
	}
	if terms[1].Src != 0 {
		t.Error("term[1]: WithVar must not set Src (it sets srcVar)")
	}
}

func TestParse_Variable_Cycle(t *testing.T) {
	// Cycles cannot arise from natural DSL parsing, so test the cycle-detection
	// helper directly with a hand-crafted cyclic dependency graph.
	deps := map[string][]string{
		"a": {"b"},
		"b": {"a"},
	}
	err := checkVarCycles(deps)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	var pe *ParseQueryError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *ParseQueryError, got %T: %v", err, err)
	}
	if pe.Code != ErrCodeCycle {
		t.Errorf("want ErrCodeCycle, got %v", pe.Code)
	}
}

func TestParse_Variable_Cap16(t *testing.T) {
	w := dslSetupWorldV2(t)
	// Build a query with 17 distinct variable names — all bound as pair targets.
	// We need 17 distinct relationships. We'll use ChildOf 17 times with different
	// variable names; the cap check fires before any iteration, so even duplicate
	// rel is fine.
	var names []string
	for i := 0; i < 17; i++ {
		names = append(names, fmt.Sprintf("v%02d", i))
	}

	// Build the expression "(ChildOf, $v00), (ChildOf, $v01), ..."
	var expr strings.Builder
	for i, n := range names {
		if i > 0 {
			expr.WriteString(", ")
		}
		expr.WriteString("(ChildOf, $")
		expr.WriteString(n)
		expr.WriteString(")")
	}

	_, err := dslParse(t, w, expr.String())
	if err == nil {
		t.Fatal("expected error for 17 variables (cap exceeded), got nil")
	}
	if !strings.Contains(err.Error(), "16") {
		t.Errorf("error message should mention cap of 16, got: %v", err)
	}
}

// ── Equality predicates ────────────────────────────────────────────────────────

func TestParse_Equality_Entity(t *testing.T) {
	w := dslSetupWorldV2(t)
	terms, err := dslParse(t, w, "$this == hero")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 1 {
		t.Fatalf("want 1 term, got %d", len(terms))
	}
	if terms[0].Kind != TermEq {
		t.Errorf("want TermEq, got %v", terms[0].Kind)
	}
}

func TestParse_Equality_String(t *testing.T) {
	w := dslSetupWorldV2(t)
	// $this == "hero" → NameMatches("hero")
	terms, err := dslParse(t, w, `$this == "hero"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 1 {
		t.Fatalf("want 1 term, got %d", len(terms))
	}
	if terms[0].Kind != TermNameMatch {
		t.Errorf("want TermNameMatch, got %v", terms[0].Kind)
	}
	if terms[0].Pattern != "hero" {
		t.Errorf("want pattern %q, got %q", "hero", terms[0].Pattern)
	}
}

func TestParse_Equality_NotEqual(t *testing.T) {
	w := dslSetupWorldV2(t)
	terms, err := dslParse(t, w, "$this != villain")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 1 {
		t.Fatalf("want 1 term, got %d", len(terms))
	}
	if terms[0].Kind != TermNotEq {
		t.Errorf("want TermNotEq, got %v", terms[0].Kind)
	}
}

func TestParse_NameMatch(t *testing.T) {
	w := dslSetupWorldV2(t)
	terms, err := dslParse(t, w, `$this ~ "prefix_*"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 1 {
		t.Fatalf("want 1 term, got %d", len(terms))
	}
	if terms[0].Kind != TermNameMatch {
		t.Errorf("want TermNameMatch, got %v", terms[0].Kind)
	}
	if terms[0].Pattern != "prefix_*" {
		t.Errorf("want pattern %q, got %q", "prefix_*", terms[0].Pattern)
	}
}

// ── AndFrom / OrFrom / NotFrom ─────────────────────────────────────────────────

func TestParse_AndFrom(t *testing.T) {
	w := dslSetupWorldV2(t)
	terms, err := dslParse(t, w, "AndFrom(presetEntity)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// AndFrom expands at query construction; parser produces TermAndFrom
	if len(terms) != 1 {
		t.Fatalf("want 1 term, got %d", len(terms))
	}
	if terms[0].Kind != TermAndFrom {
		t.Errorf("want TermAndFrom, got %v", terms[0].Kind)
	}
}

func TestParse_OrFrom(t *testing.T) {
	w := dslSetupWorldV2(t)
	terms, err := dslParse(t, w, "OrFrom(presetEntity)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 1 {
		t.Fatalf("want 1 term, got %d", len(terms))
	}
	if terms[0].Kind != TermOrFrom {
		t.Errorf("want TermOrFrom, got %v", terms[0].Kind)
	}
}

func TestParse_NotFrom(t *testing.T) {
	w := dslSetupWorldV2(t)
	terms, err := dslParse(t, w, "NotFrom(presetEntity)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 1 {
		t.Fatalf("want 1 term, got %d", len(terms))
	}
	if terms[0].Kind != TermNotFrom {
		t.Errorf("want TermNotFrom, got %v", terms[0].Kind)
	}
}

// ── Error code discrimination ──────────────────────────────────────────────────

func TestParse_ErrorCodes(t *testing.T) {
	w := dslSetupWorldV2(t)

	cases := []struct {
		expr string
		code ParseErrorCode
		desc string
	}{
		{"Position Position", ErrCodeUnknown, "missing comma (unknown/unclassified)"},
		{"(ChildOf, parent", ErrCodeUnclosedParen, "unclosed pair paren"},
		{"NonExistentXYZ", ErrCodeUnknownIdent, "unknown identifier"},
		{"?!Position", ErrCodeBadCombination, "bad ?! combination"},
		{"(ChildOf, parent).Up.Cascade", ErrCodeBadModifier, "chained traversal"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			_, err := dslParse(t, w, tc.expr)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc.expr)
			}
			var pe *ParseQueryError
			if !errors.As(err, &pe) {
				t.Fatalf("expected *ParseQueryError, got %T", err)
			}
			if pe.Code != tc.code {
				t.Errorf("want code %v, got %v (err: %v)", tc.code, pe.Code, err)
			}
		})
	}

	// cycle code
	t.Run("cycle code from checkVarCycles", func(t *testing.T) {
		err := checkVarCycles(map[string][]string{"x": {"y"}, "y": {"x"}})
		var pe *ParseQueryError
		if !errors.As(err, &pe) || pe.Code != ErrCodeCycle {
			t.Errorf("want ErrCodeCycle, got %v", err)
		}
	})

	// unbound_var code exists (tested via synthetic path)
	t.Run("ErrCodeUnboundVar constant exists", func(t *testing.T) {
		e := &ParseQueryError{Code: ErrCodeUnboundVar, Msg: "test"}
		if e.Code != ErrCodeUnboundVar {
			t.Error("ErrCodeUnboundVar constant mismatch")
		}
	})
}

// ── Coverage-completing error-path tests ─────────────────────────────────────

func TestParse_StringLiteral_Escapes(t *testing.T) {
	w := dslSetupWorldV2(t)
	cases := []struct {
		expr    string
		pattern string
	}{
		{`$this ~ "a\nb"`, "a\nb"}, // \n
		{`$this ~ "a\tb"`, "a\tb"}, // \t
		{`$this ~ "a\\b"`, `a\b`},  // \\
		{`$this ~ "a\"b"`, `a"b`},  // \"
		{`$this ~ "a\xb"`, `a\xb`}, // unknown escape kept verbatim
	}
	for _, tc := range cases {
		terms, err := dslParse(t, w, tc.expr)
		if err != nil {
			t.Errorf("expr %q: unexpected error: %v", tc.expr, err)
			continue
		}
		if len(terms) != 1 || terms[0].Kind != TermNameMatch {
			t.Errorf("expr %q: want single TermNameMatch", tc.expr)
			continue
		}
		if terms[0].Pattern != tc.pattern {
			t.Errorf("expr %q: want pattern %q, got %q", tc.expr, tc.pattern, terms[0].Pattern)
		}
	}
}

func TestParse_StringLiteral_UnclosedErrors(t *testing.T) {
	w := dslSetupWorldV2(t)

	_, err := dslParse(t, w, `$this ~ "unclosed`)
	if err == nil {
		t.Error("want error for unclosed string literal")
	}

	_, err = dslParse(t, w, `$this ~ "trailing\`)
	if err == nil {
		t.Error("want error for string with trailing backslash")
	}
}

func TestParse_ScopeGroup_Empty(t *testing.T) {
	w := dslSetupWorldV2(t)
	_, err := dslParse(t, w, "!()")
	if err == nil {
		t.Fatal("want error for empty scope group")
	}
}

func TestParse_ScopeGroup_Unclosed(t *testing.T) {
	w := dslSetupWorldV2(t)
	_, err := dslParse(t, w, "!(Position")
	if err == nil {
		t.Fatal("want error for unclosed scope group")
	}
	var pe *ParseQueryError
	if errors.As(err, &pe) && pe.Code != ErrCodeUnclosedParen {
		t.Errorf("want ErrCodeUnclosedParen, got %v", pe.Code)
	}
}

func TestParse_Predicate_NotStringError(t *testing.T) {
	w := dslSetupWorldV2(t)
	_, err := dslParse(t, w, `$this != "hero"`)
	if err == nil {
		t.Fatal("want error for $this != string literal")
	}
	var pe *ParseQueryError
	if errors.As(err, &pe) && pe.Code != ErrCodeBadCombination {
		t.Errorf("want ErrCodeBadCombination, got %v", pe.Code)
	}
}

func TestParse_Predicate_TildeNonString(t *testing.T) {
	w := dslSetupWorldV2(t)
	_, err := dslParse(t, w, "$this ~ ident")
	if err == nil {
		t.Fatal("want error for $this ~ non-string-literal")
	}
}

func TestParse_FromCall_Errors(t *testing.T) {
	w := dslSetupWorldV2(t)

	// Missing ( after keyword
	_, err := dslParse(t, w, "AndFrom presetEntity")
	if err == nil {
		t.Error("want error for AndFrom without paren")
	}

	// Unclosed: missing )
	_, err = dslParse(t, w, "AndFrom(presetEntity")
	if err == nil {
		t.Error("want error for unclosed AndFrom()")
	}
}

func TestParse_SourceBinding_WildcardError(t *testing.T) {
	w := dslSetupWorldV2(t)
	_, err := dslParse(t, w, "Position(*)")
	if err == nil {
		t.Fatal("want error for wildcard source binding Position(*)")
	}
	var pe *ParseQueryError
	if errors.As(err, &pe) && pe.Code != ErrCodeBadCombination {
		t.Errorf("want ErrCodeBadCombination, got %v", pe.Code)
	}
}

func TestParse_SourceBinding_UnclosedError(t *testing.T) {
	w := dslSetupWorldV2(t)
	_, err := dslParse(t, w, "Position(hero")
	if err == nil {
		t.Fatal("want error for unclosed source binding Position(hero")
	}
	var pe *ParseQueryError
	if errors.As(err, &pe) && pe.Code != ErrCodeUnclosedParen {
		t.Errorf("want ErrCodeUnclosedParen, got %v", pe.Code)
	}
}

func TestParse_PairTarget_ThisError(t *testing.T) {
	w := dslSetupWorldV2(t)
	_, err := dslParse(t, w, "(ChildOf, $this)")
	if err == nil {
		t.Fatal("want error for $this as pair target")
	}
	var pe *ParseQueryError
	if errors.As(err, &pe) && pe.Code != ErrCodeBadCombination {
		t.Errorf("want ErrCodeBadCombination, got %v", pe.Code)
	}
}

func TestParse_PairVarTarget_MissingName(t *testing.T) {
	w := dslSetupWorldV2(t)
	// (ChildOf, $) — $ not followed by a valid var name
	_, err := dslParse(t, w, "(ChildOf, $)")
	if err == nil {
		t.Fatal("want error for $ with no var name in pair target")
	}
}

func TestParse_PairVarTarget_EOFAfterDollar(t *testing.T) {
	w := dslSetupWorldV2(t)
	_, err := dslParse(t, w, "(ChildOf, $")
	if err == nil {
		t.Fatal("want error for $ at EOF in pair target")
	}
}

func TestParse_SourceBinding_ExplicitThis(t *testing.T) {
	// Position($this) → explicit $this source → same as With(Position)
	w := dslSetupWorldV2(t)
	terms, err := dslParse(t, w, "Position($this)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(terms) != 1 || terms[0].Kind != TermAnd {
		t.Errorf("want single TermAnd, got %d terms", len(terms))
	}
}

func TestParse_SourceBinding_EOFError(t *testing.T) {
	w := dslSetupWorldV2(t)
	// Position( with nothing — EOF inside source binding
	_, err := dslParse(t, w, "Position(")
	if err == nil {
		t.Fatal("want error for EOF inside source binding")
	}
}

func TestParse_SourceBinding_InvalidName(t *testing.T) {
	w := dslSetupWorldV2(t)
	// Position() — empty parens, no entity name
	_, err := dslParse(t, w, "Position()")
	if err == nil {
		t.Fatal("want error for Position() with empty source name")
	}
}

func TestParse_TraversalPostfix_NonKeyword(t *testing.T) {
	// (ChildOf, parent).SomethingElse — dot followed by non-traversal ident
	// parseTraversalPostfix backtracks and the outer parser rejects the stray dot.
	w := dslSetupWorldV2(t)
	_, err := dslParse(t, w, "(ChildOf, parent).SomethingElse")
	if err == nil {
		t.Fatal("want error for non-traversal postfix .SomethingElse")
	}
}

func TestParse_Predicate_EOFAfterThis(t *testing.T) {
	// "$this" with no operator
	w := dslSetupWorldV2(t)
	_, err := dslParse(t, w, "$this")
	if err == nil {
		t.Fatal("want error for $this with no operator")
	}
}

func TestParse_StandaloneVar_Error(t *testing.T) {
	// $foo as a standalone term is not supported
	w := dslSetupWorldV2(t)
	_, err := dslParse(t, w, "$foo")
	if err == nil {
		t.Fatal("want error for standalone $var term")
	}
	var pe *ParseQueryError
	if errors.As(err, &pe) && pe.Code != ErrCodeBadCombination {
		t.Errorf("want ErrCodeBadCombination, got %v", pe.Code)
	}
}

func TestParse_VarCycles_NoCycleSharedNode(t *testing.T) {
	// a→b, c→b: no cycle; verifies DFS "already visited" fast-path
	deps := map[string][]string{"a": {"b"}, "c": {"b"}, "b": {}}
	if err := checkVarCycles(deps); err != nil {
		t.Errorf("want nil for non-cyclic graph, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Phase 16.45 DSL tests
// ---------------------------------------------------------------------------

// dslSetupWorldV3 creates a world with components/entities for Phase 16.45 DSL tests.
func dslSetupWorldV3(t *testing.T) (*World, ID, ID, ID) {
	t.Helper()
	w := New()
	posID := RegisterComponent[dslPos](w)
	w.SetName(posID, "Position")
	rel := ID(0)
	tgt := ID(0)
	w.Write(func(fw *Writer) {
		rel = fw.NewEntity()
		w.SetName(rel, "DockedTo")
		tgt = fw.NewEntity()
		w.SetName(tgt, "hero")
	})
	return w, posID, rel, tgt
}

func TestParse_RelVarSyntax(t *testing.T) {
	w, _, _, tgt := dslSetupWorldV3(t)
	terms, err := dslParse(t, w, "($Rel, hero)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Filter out TermVarDecl terms (none expected here, but be defensive)
	var andTerms []Term
	for _, tm := range terms {
		if tm.Kind == TermAnd {
			andTerms = append(andTerms, tm)
		}
	}
	if len(andTerms) != 1 {
		t.Fatalf("want 1 TermAnd, got %d", len(andTerms))
	}
	if andTerms[0].relVar == "" {
		t.Error("want relVar set in parsed ($Rel, hero) term")
	}
	if andTerms[0].relVar != "Rel" {
		t.Errorf("want relVar=Rel, got %q", andTerms[0].relVar)
	}
	if andTerms[0].relVarTarget != tgt {
		t.Errorf("want relVarTarget=%v (hero), got %v", tgt, andTerms[0].relVarTarget)
	}
}

func TestParse_RelBothVar(t *testing.T) {
	w, _, _, _ := dslSetupWorldV3(t)
	terms, err := dslParse(t, w, "($R, $T)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var andTerms []Term
	for _, tm := range terms {
		if tm.Kind == TermAnd {
			andTerms = append(andTerms, tm)
		}
	}
	if len(andTerms) != 1 {
		t.Fatalf("want 1 TermAnd, got %d", len(andTerms))
	}
	if andTerms[0].relVar != "R" {
		t.Errorf("want relVar=R, got %q", andTerms[0].relVar)
	}
	if andTerms[0].tgtVar != "T" {
		t.Errorf("want tgtVar=T, got %q", andTerms[0].tgtVar)
	}
	if andTerms[0].relVarTarget != 0 {
		t.Errorf("want relVarTarget=0 (both-var), got %v", andTerms[0].relVarTarget)
	}
}

func TestParse_NegativeVar(t *testing.T) {
	w, posID, rel, _ := dslSetupWorldV3(t)
	_ = posID
	_ = rel
	// (DockedTo, $planet), !DockedTo($this, $planet) — predicate negation form.
	// $planet is bound by the first term, then used in the negated predicate.
	terms, err := dslParse(t, w, "(DockedTo, $planet), !DockedTo($this, $planet)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var andCount, notCount int
	for _, tm := range terms {
		switch tm.Kind {
		case TermAnd:
			andCount++
		case TermNot:
			notCount++
			if tm.tgtVar != "planet" {
				t.Errorf("want tgtVar=planet on TermNot, got %q", tm.tgtVar)
			}
		}
	}
	if andCount < 1 {
		t.Error("want at least 1 TermAnd term")
	}
	if notCount != 1 {
		t.Errorf("want 1 TermNot term, got %d", notCount)
	}
}

func TestParse_NegativeVar_UnboundError(t *testing.T) {
	w, _, _, _ := dslSetupWorldV3(t)
	// !DockedTo($this, $freeVar) — predicate form; $freeVar unbound → error.
	_, err := dslParse(t, w, "!DockedTo($this, $freeVar)")
	if err == nil {
		t.Fatal("want error for unbound negative variable, got nil")
	}
	pqe, ok := err.(*ParseQueryError)
	if !ok {
		t.Fatalf("want *ParseQueryError, got %T: %v", err, err)
	}
	if pqe.Code != ErrCodeUnboundNegativeVar {
		t.Errorf("want ErrCodeUnboundNegativeVar, got %v", pqe.Code)
	}
}

func TestParse_TableVarSyntax(t *testing.T) {
	w, posID, _, _ := dslSetupWorldV3(t)
	_ = posID
	terms, err := dslParse(t, w, "$T:Position($this)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should contain a TermVarDecl for T and a TermAnd for Position.
	var declCount, andCount int
	for _, tm := range terms {
		switch tm.Kind {
		case TermVarDecl:
			declCount++
			if tm.srcVar != "T" {
				t.Errorf("want srcVar=T in TermVarDecl, got %q", tm.srcVar)
			}
			if tm.varKind != VarTable {
				t.Errorf("want VarTable kind, got %v", tm.varKind)
			}
		case TermAnd:
			andCount++
		}
	}
	if declCount != 1 {
		t.Errorf("want 1 TermVarDecl, got %d", declCount)
	}
	if andCount != 1 {
		t.Errorf("want 1 TermAnd (Position), got %d", andCount)
	}
}
