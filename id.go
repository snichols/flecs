// Package flecs is a Go port of the flecs Entity Component System library.
//
// The World is the central ECS object: it owns entities, component metadata,
// and archetype tables. Components are first-class entities. Archetype-based
// storage groups entities that share the same component set into contiguous
// structure-of-arrays tables, enabling efficient per-component iteration.
//
// World is NOT goroutine-safe; external synchronization is required.
//
// See https://github.com/SanderMertens/flecs for the upstream C implementation.
package flecs

import "github.com/snichols/flecs/internal/ids"

// ID is a 64-bit entity or pair identifier.
//
// Entity IDs consist of a 32-bit unique index in the lower bits and a 32-bit
// generation counter in the upper bits. Pair IDs encode a relationship (28
// bits, at bits 32-59) and a target (32 bits, at bits 0-31), with FlagPair
// set in the top nibble.
//
// The underlying type is defined in internal/ids to allow internal subpackages
// to use ID without creating import cycles with the root flecs package.
type ID = ids.ID

// ID flag constants mirror the upstream ECS_PAIR, ECS_AUTO_OVERRIDE,
// ECS_TOGGLE, and ECS_VALUE_PAIR bit values exactly.
const (
	FlagPair         = ids.FlagPair
	FlagAutoOverride = ids.FlagAutoOverride
	FlagToggle       = ids.FlagToggle
	FlagValuePair    = ids.FlagValuePair
)

// MakeEntity constructs an entity ID from an index and a generation counter.
func MakeEntity(index, generation uint32) ID { return ids.MakeEntity(index, generation) }

// MakePair constructs a pair ID from a relationship and a target entity.
// first is masked to 28 bits; second is masked to 32 bits; FlagPair is set.
func MakePair(first, second ID) ID { return ids.MakePair(first, second) }
