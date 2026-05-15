package flecs

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"unsafe"

	"github.com/snichols/flecs/internal/component"
	"github.com/snichols/flecs/internal/storage/table"
)

// Sentinel errors for migration failures.
var (
	ErrMissingMigration       = errors.New("flecs: missing migration")
	ErrSnapshotNewerThanWorld = errors.New("flecs: snapshot schema version is newer than world")
)

// MigrationFunc transforms decoded snapshot data before it is materialized
// into the live world. The context mc exposes component-name-keyed records;
// mutations (rename, drop, add, byte-rewrite) on mc are applied before restore.
type MigrationFunc func(m *MigrationContext) error

// migrationEntry stores one registered migration step.
type migrationEntry struct {
	from, to uint32
	fn       MigrationFunc
}

// MigrationContext is the neutral IR over decoded snapshot tables.
// It holds per-entity component data keyed by component name string,
// allowing mutations before materialization into the live world.
type MigrationContext struct {
	comps      map[string][]*ComponentRecord // component name → ordered entity records
	entities   []uint64                      // all entities in encounter order (deduplicated)
	entityTags map[uint64][]string           // entity ID → current component names
}

// ComponentRecord holds one entity's raw bytes for a single component.
type ComponentRecord struct {
	Entity uint64
	Raw    []byte // nil for tag components (zero size)
}

// SetRaw replaces the raw bytes for this component record.
func (r *ComponentRecord) SetRaw(b []byte) {
	r.Raw = append([]byte(nil), b...)
}

// EachComponent calls fn for every entity record of the named component.
// Records are presented in snapshot encounter order. Returning a non-nil
// error from fn stops iteration and is returned to the caller.
func (m *MigrationContext) EachComponent(name string, fn func(rec *ComponentRecord) error) error {
	for _, rec := range m.comps[name] {
		if err := fn(rec); err != nil {
			return err
		}
	}
	return nil
}

// RenameComponent renames all records for oldName to newName. If oldName is
// not present the call is a no-op. Existing newName records (if any) are
// merged after the renamed records.
func (m *MigrationContext) RenameComponent(oldName, newName string) error {
	recs, ok := m.comps[oldName]
	if !ok {
		return nil
	}
	// Update entityTags
	for _, rec := range recs {
		tags := m.entityTags[rec.Entity]
		for i, t := range tags {
			if t == oldName {
				tags[i] = newName
				break
			}
		}
	}
	m.comps[newName] = append(recs, m.comps[newName]...)
	delete(m.comps, oldName)
	return nil
}

// DropComponent removes all records for name. No-op if the component is not
// present. After dropping, entities that had the component will no longer
// include it in their tag set.
func (m *MigrationContext) DropComponent(name string) error {
	recs, ok := m.comps[name]
	if !ok {
		return nil
	}
	delete(m.comps, name)
	for _, rec := range recs {
		tags := m.entityTags[rec.Entity]
		newTags := tags[:0:len(tags)]
		for _, t := range tags {
			if t != name {
				newTags = append(newTags, t)
			}
		}
		m.entityTags[rec.Entity] = newTags
	}
	return nil
}

// AddComponent adds name with the given value bytes to every entity for which
// where(entityTags) returns true. Entities that already have the component are
// skipped. value is copied for each added entity.
func (m *MigrationContext) AddComponent(name string, value []byte, where func(entityTags []string) bool) error {
	for _, e := range m.entities {
		tags := m.entityTags[e]
		if !where(tags) {
			continue
		}
		// Skip if already has component
		has := false
		for _, t := range tags {
			if t == name {
				has = true
				break
			}
		}
		if has {
			continue
		}
		raw := append([]byte(nil), value...)
		m.comps[name] = append(m.comps[name], &ComponentRecord{Entity: e, Raw: raw})
		m.entityTags[e] = append(m.entityTags[e], name)
	}
	return nil
}

// ─── World API ───────────────────────────────────────────────────────────────

// SetSchemaVersion sets the user schema version persisted into every snapshot.
// The default is 0. Increment the version whenever component layouts change
// and register a corresponding migration via RegisterMigration.
func (w *World) SetSchemaVersion(v uint32) {
	w.schemaVersion = v
}

// SchemaVersion returns the world's current user schema version.
func (w *World) SchemaVersion() uint32 {
	return w.schemaVersion
}

// RegisterMigration registers a migration function from fromVersion to
// toVersion. Migrations form a consecutive chain: to load a v1 snapshot
// into a v3 world you must register v1→v2 and v2→v3. The fromVersion and
// toVersion must satisfy toVersion == fromVersion+1 for the chain to resolve
// without a gap.
func (w *World) RegisterMigration(fromVersion, toVersion uint32, migrate MigrationFunc) {
	w.migrations = append(w.migrations, migrationEntry{from: fromVersion, to: toVersion, fn: migrate})
}

// ─── Internal helpers ────────────────────────────────────────────────────────

// buildMigrationChain resolves a consecutive chain of MigrationFuncs from
// [from, to). It returns ErrMissingMigration wrapping the first gap found.
func buildMigrationChain(migrations []migrationEntry, from, to uint32) ([]MigrationFunc, error) {
	// Build a lookup: fromVer → toVer → fn
	lookup := make(map[uint32]map[uint32]MigrationFunc)
	for _, m := range migrations {
		if lookup[m.from] == nil {
			lookup[m.from] = make(map[uint32]MigrationFunc)
		}
		lookup[m.from][m.to] = m.fn
	}
	chain := make([]MigrationFunc, 0, int(to-from))
	for cur := from; cur < to; cur++ {
		next := cur + 1
		fns, ok := lookup[cur]
		if !ok {
			return nil, fmt.Errorf("%w: no migration from schema version %d to %d", ErrMissingMigration, cur, next)
		}
		fn, ok := fns[next]
		if !ok {
			return nil, fmt.Errorf("%w: no migration from schema version %d to %d", ErrMissingMigration, cur, next)
		}
		chain = append(chain, fn)
	}
	return chain, nil
}

// parsedEntityIndex holds entity index data parsed from a snapshot blob,
// ready to be applied to a world after successful migration.
type parsedEntityIndex struct {
	alive, recycled []ID
	maxID           uint32
}

// parseComponentsForMigration reads the component-registry section and
// returns a map from snapshot component ID to component name. It does NOT
// validate that names are present in the target registry.
func parseComponentsForMigration(br *binReader) (map[ID]string, error) {
	count, err := br.u32()
	if err != nil {
		return nil, err
	}
	idToName := make(map[ID]string, count)
	for i := uint32(0); i < count; i++ {
		cid, err := br.id()
		if err != nil {
			return nil, err
		}
		nameLen, err := br.u32()
		if err != nil {
			return nil, err
		}
		nameBytes, err := br.raw(int(nameLen))
		if err != nil {
			return nil, err
		}
		idToName[cid] = string(nameBytes)
	}
	return idToName, nil
}

// parseEntityIndexData reads the entity-index section into an in-memory struct
// without modifying the world.
func parseEntityIndexData(br *binReader) (*parsedEntityIndex, error) {
	aliveCount, err := br.u32()
	if err != nil {
		return nil, err
	}
	alive := make([]ID, aliveCount)
	for i := range alive {
		if alive[i], err = br.id(); err != nil {
			return nil, err
		}
	}
	recycleCount, err := br.u32()
	if err != nil {
		return nil, err
	}
	recycled := make([]ID, recycleCount)
	for i := range recycled {
		if recycled[i], err = br.id(); err != nil {
			return nil, err
		}
	}
	maxID, err := br.u32()
	if err != nil {
		return nil, err
	}
	return &parsedEntityIndex{alive: alive, recycled: recycled, maxID: maxID}, nil
}

// applyEntityIndexData writes the pre-parsed entity index into the world.
func applyEntityIndexData(pei *parsedEntityIndex, w *World) {
	w.index.RestoreUserState(firstSnapUserIndex, pei.alive, pei.recycled, pei.maxID)
}

// parseMigrationContext reads the tables section (section 3) and builds a
// MigrationContext IR. Column bytes are copied; bitsets and parent columns
// are consumed from the binary reader but not preserved (they are lost
// across migrations in this v1 implementation).
func parseMigrationContext(br *binReader, idToName map[ID]string) (*MigrationContext, error) {
	mc := &MigrationContext{
		comps:      make(map[string][]*ComponentRecord),
		entityTags: make(map[uint64][]string),
	}
	seenEntities := make(map[uint64]bool)

	numTables, err := br.u32()
	if err != nil {
		return nil, err
	}
	for i := uint32(0); i < numTables; i++ {
		sigLen, err := br.u32()
		if err != nil {
			return nil, err
		}
		sig := make([]ID, sigLen)
		for j := range sig {
			if sig[j], err = br.id(); err != nil {
				return nil, err
			}
		}

		rowCount, err := br.u32()
		if err != nil {
			return nil, err
		}
		entityIDs := make([]uint64, rowCount)
		for j := range entityIDs {
			id, err := br.id()
			if err != nil {
				return nil, err
			}
			entityIDs[j] = uint64(id)
		}

		// Map component IDs to names
		compNames := make([]string, sigLen)
		for j, cid := range sig {
			compNames[j] = idToName[cid]
		}

		// Column data per component
		colData := make([][]byte, sigLen)
		elemSizes := make([]uint32, sigLen)
		for ci := range sig {
			elemSize, err := br.u32()
			if err != nil {
				return nil, err
			}
			elemSizes[ci] = elemSize
			if elemSize == 0 || rowCount == 0 {
				continue
			}
			totalBytes := int(elemSize) * int(rowCount)
			data, err := br.raw(totalBytes)
			if err != nil {
				return nil, err
			}
			colData[ci] = append([]byte(nil), data...) // copy
		}

		// Build ComponentRecord per entity per component
		for j, e := range entityIDs {
			if !seenEntities[e] {
				seenEntities[e] = true
				mc.entities = append(mc.entities, e)
			}
			for ci, name := range compNames {
				if name == "" {
					continue // unknown component (no name in registry at take time)
				}
				var raw []byte
				if colData[ci] != nil {
					elemSize := int(elemSizes[ci])
					raw = append([]byte(nil), colData[ci][j*elemSize:(j+1)*elemSize]...)
				}
				mc.comps[name] = append(mc.comps[name], &ComponentRecord{Entity: e, Raw: raw})
				mc.entityTags[e] = append(mc.entityTags[e], name)
			}
		}

		// Consume (skip) bitsets — not preserved across migration
		bitsetCount, err := br.u32()
		if err != nil {
			return nil, err
		}
		for k := uint32(0); k < bitsetCount; k++ {
			if _, err := br.id(); err != nil {
				return nil, err
			}
			wordCount, err := br.u32()
			if err != nil {
				return nil, err
			}
			for l := uint32(0); l < wordCount; l++ {
				if _, err := br.u64(); err != nil {
					return nil, err
				}
			}
		}

		// Consume (skip) parent columns
		parentColCount, err := br.u32()
		if err != nil {
			return nil, err
		}
		for k := uint32(0); k < parentColCount; k++ {
			if _, err := br.id(); err != nil {
				return nil, err
			}
			colLen, err := br.u32()
			if err != nil {
				return nil, err
			}
			for l := uint32(0); l < colLen; l++ {
				if _, err := br.id(); err != nil {
					return nil, err
				}
			}
		}
	}
	return mc, nil
}

// validateMigrationContext checks that every component name remaining in mc
// after migration is registered in the target world.
func validateMigrationContext(mc *MigrationContext, w *World) error {
	allIDs := w.registry.IDs()
	nameToInfo := make(map[string]*component.TypeInfo, len(allIDs))
	for _, cid := range allIDs {
		info, ok := w.registry.LookupByID(cid)
		if !ok || info.Name == "" || info.Name == "tag" {
			continue
		}
		nameToInfo[info.Name] = info
	}
	for name := range mc.comps {
		if _, ok := nameToInfo[name]; !ok {
			return fmt.Errorf("flecs: migrated component %q is not registered in the target world", name)
		}
	}
	return nil
}

// materializeMigrationContext creates archetype tables in w and fills them
// with the migrated component data from mc. Entity index entries must already
// have been applied before calling this function.
func materializeMigrationContext(mc *MigrationContext, w *World) error {
	// Build name → ID and name → TypeInfo from the current registry.
	allIDs := w.registry.IDs()
	nameToID := make(map[string]ID, len(allIDs))
	nameToInfo := make(map[string]*component.TypeInfo, len(allIDs))
	for _, cid := range allIDs {
		info, ok := w.registry.LookupByID(cid)
		if !ok || info.Name == "" || info.Name == "tag" {
			continue
		}
		nameToID[info.Name] = cid
		nameToInfo[info.Name] = info
	}

	// Build entity → comp name → raw lookup from comps.
	entityRaw := make(map[uint64]map[string][]byte)
	for name, recs := range mc.comps {
		for _, rec := range recs {
			if entityRaw[rec.Entity] == nil {
				entityRaw[rec.Entity] = make(map[string][]byte)
			}
			entityRaw[rec.Entity][name] = rec.Raw
		}
	}

	// Group entities by their migrated signature.
	type compPair struct {
		id   ID
		name string
	}
	type group struct {
		sig      []ID
		names    []string
		entities []uint64
	}
	groupMap := make(map[string]*group)
	var groupOrder []string

	for _, e := range mc.entities {
		tags := mc.entityTags[e]
		pairs := make([]compPair, 0, len(tags))
		for _, tag := range tags {
			id, ok := nameToID[tag]
			if !ok {
				continue
			}
			pairs = append(pairs, compPair{id, tag})
		}
		sort.Slice(pairs, func(i, j int) bool { return pairs[i].id < pairs[j].id })

		sig := make([]ID, len(pairs))
		names := make([]string, len(pairs))
		for i, p := range pairs {
			sig[i] = p.id
			names[i] = p.name
		}

		key := sigKey(sig)
		g, exists := groupMap[key]
		if !exists {
			g = &group{sig: sig, names: names}
			groupMap[key] = g
			groupOrder = append(groupOrder, key)
		}
		g.entities = append(g.entities, e)
	}

	// Materialize each group into the world.
	for _, key := range groupOrder {
		g := groupMap[key]

		// Build TypeInfo slice.
		types := make([]*component.TypeInfo, len(g.sig))
		for i, name := range g.names {
			info, ok := nameToInfo[name]
			if !ok {
				return fmt.Errorf("flecs: component %q not found in registry during materialization", name)
			}
			types[i] = info
		}

		// Create or find the table.
		t, exists := w.tables[key]
		if !exists {
			t = table.New(g.sig, types)
			w.tables[key] = t
			for _, id := range g.sig {
				w.compIndex.Register(id, t)
			}
		}

		// Append entities (zero-fills columns).
		for _, e := range g.entities {
			t.Append(ID(e))
		}

		// Fill column data from IR.
		for ci, compID := range g.sig {
			info := types[ci]
			if info.Size == 0 {
				continue // tag
			}
			name := g.names[ci]
			base, _, n := t.ColumnBasePtr(compID)
			if base == nil || n == 0 {
				continue
			}
			elemSize := uintptr(info.Size)
			dst := unsafe.Slice((*byte)(base), uintptr(n)*elemSize)
			for row, e := range g.entities {
				raw := entityRaw[e][name]
				if len(raw) == 0 {
					continue // no data (tag in snapshot, or AddComponent with nil value)
				}
				if uintptr(len(raw)) != elemSize {
					return fmt.Errorf("flecs: component %q for entity %d: raw size %d != registry size %d",
						name, e, len(raw), elemSize)
				}
				copy(dst[uintptr(row)*elemSize:], raw)
			}
			runtime.KeepAlive(t)
		}

		// Set Record.Table and Record.Row for each entity.
		for row, e := range g.entities {
			if rec := w.index.Get(ID(e)); rec != nil {
				rec.Table = t
				rec.Row = uint32(row)
			}
		}
	}
	return nil
}

// snapshotDeserializeMigration implements the migration restore path.
// It decodes the snapshot blob into an intermediate representation, applies
// the registered migration chain, and only then materializes into the world
// (ensuring atomicity: world is untouched on any error before clearUserState).
func snapshotDeserializeMigration(ctx context.Context, w *World, blob []byte, snapshotSchema uint32) error {
	chain, err := buildMigrationChain(w.migrations, snapshotSchema, w.schemaVersion)
	if err != nil {
		return err
	}

	br := &binReader{data: blob}

	// Parse component registry → snapshot ID → name map (no registry validation yet).
	idToName, err := parseComponentsForMigration(br)
	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Parse entity index → in-memory struct (world untouched).
	pei, err := parseEntityIndexData(br)
	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Parse tables → MigrationContext IR (world untouched).
	mc, err := parseMigrationContext(br, idToName)
	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Apply migration chain (world untouched on error).
	for _, fn := range chain {
		if err := fn(mc); err != nil {
			return err
		}
	}

	// Validate migrated IR against current world registry.
	if err := validateMigrationContext(mc, w); err != nil {
		return err
	}

	// All decode + migrate succeeded; now commit to world state.
	clearUserState(w)

	applyEntityIndexData(pei, w)

	if err := materializeMigrationContext(mc, w); err != nil {
		return err
	}

	// Restore sections 4–10 from the binReader (cursor is positioned just after section 3).
	if err := deserializeEmptyTableUserEnts(br, w); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := deserializeSparseData(br, w); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := deserializeUnionState(br, w); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := deserializeEntityRange(br, w); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := deserializePolicies(br, w); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := deserializeOrderedChildren(br, w); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := deserializeUnitDefs(br, w); err != nil {
		return err
	}

	w.inheritorCache = nil
	w.pipelineDirty = true
	w.cachedQueries = nil
	for _, t := range w.tables {
		t.ResetEmptyTicks()
	}
	return nil
}
