// Package flecs is a Go port of the flecs Entity Component System library.
// See https://github.com/SanderMertens/flecs for the upstream C implementation.
package flecs

import "fmt"

// ID is a 64-bit entity or pair identifier.
//
// Entity IDs consist of a 32-bit unique index in the lower bits and a 32-bit
// generation counter in the upper bits. Pair IDs encode a relationship (28
// bits, at bits 32-59) and a target (32 bits, at bits 0-31), with FlagPair
// set in the top nibble.
type ID uint64

// ID flag constants mirror the upstream ECS_PAIR, ECS_AUTO_OVERRIDE,
// ECS_TOGGLE, and ECS_VALUE_PAIR bit values exactly.
const (
	FlagPair         ID = 1 << 63
	FlagAutoOverride ID = 1 << 62
	FlagToggle       ID = 1 << 61
	FlagValuePair    ID = (1 << 60) | (1 << 63)
)

// componentMask strips the top-4 flag bits, leaving bits 0-59.
const componentMask ID = 0x0FFFFFFFFFFFFFFF

// Index returns the lower 32 bits of the ID (unique entity index).
func (id ID) Index() uint32 {
	return uint32(id)
}

// Generation returns the upper 32 bits of the ID (entity generation counter).
// Meaningful only for non-pair IDs.
func (id ID) Generation() uint32 {
	return uint32(id >> 32)
}

// WithGeneration returns a copy of the ID with the generation field replaced
// by gen, leaving the lower 32-bit index unchanged.
func (id ID) WithGeneration(gen uint32) ID {
	return ID(uint32(id)) | ID(gen)<<32
}

// IsPair reports whether FlagPair is set on this ID.
func (id ID) IsPair() bool {
	return id&FlagPair != 0
}

// First returns the relationship component of a pair ID (bits 32-59, 28 bits).
// Returns 0 if IsPair is false.
func (id ID) First() ID {
	if !id.IsPair() {
		return 0
	}
	return ID((id & componentMask) >> 32)
}

// Second returns the target component of a pair ID (bits 0-31, 32 bits).
// Returns 0 if IsPair is false.
func (id ID) Second() ID {
	if !id.IsPair() {
		return 0
	}
	return ID(uint32(id))
}

// HasFlag reports whether the given flag bit is set in this ID.
func (id ID) HasFlag(flag ID) bool {
	return id&flag != 0
}

// String returns a human-readable debug representation. Entities are formatted
// as e:<index>#<generation>; pairs as (<first>,<second>).
func (id ID) String() string {
	if id.IsPair() {
		return fmt.Sprintf("(%d,%d)", id.First(), id.Second())
	}
	return fmt.Sprintf("e:%d#%d", id.Index(), id.Generation())
}

// MakeEntity constructs an entity ID from an index and a generation counter.
func MakeEntity(index, generation uint32) ID {
	return ID(index) | ID(generation)<<32
}

// MakePair constructs a pair ID from a relationship and a target entity.
// first is masked to 28 bits; second is masked to 32 bits; FlagPair is set.
func MakePair(first, second ID) ID {
	return FlagPair | (first&0x0FFFFFFF)<<32 | ID(uint32(second))
}
