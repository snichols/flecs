package flecs

// cmdKind discriminates the operation stored in a cmd.
// Mirrors ecs_cmd_kind_t from src/commands.h in the C flecs upstream.
type cmdKind uint8

const (
	cmdSkip     cmdKind = iota // coalescer rewrites superseded cmds to skip
	cmdAddID                   // AddID(e, id) — no payload
	cmdRemoveID                // RemoveID(e, id) — no payload
	cmdSetByID                 // Set[T](e,v) or SetByID(e,id,v) — payload in arena
	cmdDelete                  // Delete(e) — no payload
	cmdSetPair                 // SetPair[T](e,rel,tgt,v) — id=pairID; payload in arena
	cmdModified                // synthetic: value already in column; fire OnSet+BumpChange
	cmdClear                   // Clear(e) — remove all components, entity stays alive
)

// cmd is a single deferred operation.
// 32 bytes (vs C's ecs_cmd_t at 56 bytes — Go omits the union-tag overhead
// and the stage pointer). Layout mirrors src/commands.h:49–62.
type cmd struct {
	kind          cmdKind // 1 byte
	firstAdd      bool    // 1 byte — set by Pass 2 for the first cmdModified of a component newly added in this batch (skip OnReplace)
	_             [2]byte // padding so nextForEntity sits at offset 4
	nextForEntity int32   // intrusive linked list; see cmdQueue.append for encoding
	id            ID      // component / tag / pair ID
	entity        ID      // target entity
	valueOff      uint32  // offset into cmdArena for payload; 0 if none
	valueSize     uint32  // payload size in bytes; 0 for tags / no-payload cmds
}

// cmdEntry tracks the head and tail of a per-entity cmd chain.
// Mirrors ecs_cmd_entry_t in the C upstream.
// Stored by value in the entries map to avoid a per-entry heap allocation.
type cmdEntry struct {
	first, last int32 // indices into cmdQueue.cmds
}
