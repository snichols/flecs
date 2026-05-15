package flecs

import "sync"

// idSeenPool pools map[ID]struct{} objects used as cycle-detection seen-sets
// in traversal functions (walkUp, hasViaIsA, getViaIsA, getViaIsAByID).
//
// Safety: the map never escapes the callsite that acquires it — it is only
// used during the traversal call tree and returned to the pool before the
// function returns. This invariant is verified by manual escape-analysis audit.
//
// Reset: clear(m) is called before every Put, so the next Get always receives
// an empty map. The backing array is retained, amortising future insertions.
var idSeenPool = sync.Pool{New: func() any { return make(map[ID]struct{}, 8) }}
