package flecs

// SlotOf returns the ID of the built-in SlotOf relationship entity (index 47).
//
// A prefab child that carries (SlotOf, prefab) defines a named slot on that
// prefab. When the prefab is instantiated, the runtime adds a (prefabChild,
// instanceChild) pair to the instance root, allowing the caller to resolve the
// copied child in O(1) without a name lookup:
//
//	tank := fw.NewEntity(); flecs.MarkPrefab(fw, tank)
//	turret := fw.NewEntity(); flecs.MarkPrefab(fw, turret)
//	flecs.AddID(fw, turret, flecs.MakePair(w.ChildOf(), tank))
//	flecs.AddID(fw, turret, flecs.MakePair(w.SlotOf(), tank))
//
//	inst := fw.NewEntity()
//	flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), tank))
//
//	instTurret, ok := flecs.GetPairTarget(r, inst, turret) // → the copied turret child
//
// SlotOf is bootstrapped as Exclusive (one slot per child), PairIsTag (no data
// payload on the pair), and Relationship, mirroring C EcsSlotOf at
// src/bootstrap.c:973,1274,1282,1324.
func (w *World) SlotOf() ID { return w.slotOfID }
