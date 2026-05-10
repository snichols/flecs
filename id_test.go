package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

func TestEntityRoundTrip(t *testing.T) {
	cases := []struct {
		index, gen uint32
	}{
		{0, 0},
		{1, 0},
		{0, 1},
		{42, 7},
		{0xFFFFFFFF, 0xFFFFFFFF},
	}
	for _, c := range cases {
		id := flecs.MakeEntity(c.index, c.gen)
		if got := id.Index(); got != c.index {
			t.Errorf("MakeEntity(%d,%d).Index() = %d, want %d", c.index, c.gen, got, c.index)
		}
		if got := id.Generation(); got != c.gen {
			t.Errorf("MakeEntity(%d,%d).Generation() = %d, want %d", c.index, c.gen, got, c.gen)
		}
	}
}

func TestWithGenerationPreservesIndex(t *testing.T) {
	id := flecs.MakeEntity(99, 1)
	id2 := id.WithGeneration(42)
	if id2.Index() != 99 {
		t.Errorf("WithGeneration changed index: got %d, want 99", id2.Index())
	}
	if id2.Generation() != 42 {
		t.Errorf("WithGeneration: generation = %d, want 42", id2.Generation())
	}
}

func TestPairEncodeDecode(t *testing.T) {
	cases := []struct {
		first, second flecs.ID
	}{
		{flecs.MakeEntity(1, 0), flecs.MakeEntity(2, 0)},
		{flecs.MakeEntity(0x0FFFFFFF, 0), flecs.MakeEntity(0xFFFFFFFF, 0)},
		{flecs.MakeEntity(100, 0), flecs.MakeEntity(200, 0)},
	}
	for _, c := range cases {
		pair := flecs.MakePair(c.first, c.second)
		if !pair.IsPair() {
			t.Errorf("MakePair(%v,%v).IsPair() = false, want true", c.first, c.second)
		}
		wantFirst := c.first & 0x0FFFFFFF
		if got := pair.First(); got != wantFirst {
			t.Errorf("pair.First() = %d, want %d", got, wantFirst)
		}
		wantSecond := flecs.ID(uint32(c.second))
		if got := pair.Second(); got != wantSecond {
			t.Errorf("pair.Second() = %d, want %d", got, wantSecond)
		}
	}
}

func TestHasFlag(t *testing.T) {
	id := flecs.MakeEntity(5, 0)
	if id.HasFlag(flecs.FlagPair) {
		t.Error("entity should not have FlagPair")
	}
	pair := flecs.MakePair(flecs.MakeEntity(1, 0), flecs.MakeEntity(2, 0))
	if !pair.HasFlag(flecs.FlagPair) {
		t.Error("pair should have FlagPair")
	}
	if pair.HasFlag(flecs.FlagToggle) {
		t.Error("pair should not have FlagToggle")
	}
}

func TestZeroValue(t *testing.T) {
	var id flecs.ID
	if id.IsPair() {
		t.Error("zero ID.IsPair() should be false")
	}
	if id.Index() != 0 {
		t.Errorf("zero ID.Index() = %d, want 0", id.Index())
	}
	if id.Generation() != 0 {
		t.Errorf("zero ID.Generation() = %d, want 0", id.Generation())
	}
	if id.First() != 0 {
		t.Errorf("zero ID.First() = %d, want 0", id.First())
	}
	if id.Second() != 0 {
		t.Errorf("zero ID.Second() = %d, want 0", id.Second())
	}
}

func TestString(t *testing.T) {
	entity := flecs.MakeEntity(3, 7)
	if got, want := entity.String(), "e:3#7"; got != want {
		t.Errorf("entity.String() = %q, want %q", got, want)
	}

	first := flecs.MakeEntity(10, 0)
	second := flecs.MakeEntity(20, 0)
	pair := flecs.MakePair(first, second)
	// first is masked to 28 bits: 10 & 0x0FFFFFFF = 10; second is uint32(20) = 20
	if got, want := pair.String(), "(10,20)"; got != want {
		t.Errorf("pair.String() = %q, want %q", got, want)
	}
}
