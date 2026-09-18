package accounts

import (
	"fmt"
	"sort"
	"strconv"

	"hoc-server/internal/config"
	"hoc-server/internal/domain/items"
)

// Tablet instances (inventory v2, 2026-09-18).
//
// A player may own several copies of the same tablet (the client counts
// copies per id in UserInfo::getTabletCount and addresses every tablet by its
// position in the owned TabletInfo vector), so sockets, ascension and the
// equipped slot belong to an INSTANCE, not to the tablet id. TabletInstances
// is the owned vector in wire order; each entry has a stable UID that the
// equipped-slot records reference.
//
// The legacy fields (OwnedTablets / BackpackSockets / AwakeTabletIDs and
// TabletRec.ID+Sockets) are still written as a mirror of the instances so an
// older server binary can read the file, and are read once to migrate a
// record that has no tablet_instances yet.

type TabletInstance struct {
	UID     int              `json:"uid"`
	ID      int              `json:"id"`
	Sockets map[string][]int `json:"sockets,omitempty"`
	Awake   bool             `json:"awake,omitempty"`
}

// TabletView is a read-only snapshot of one owned tablet.
type TabletView struct {
	UID     int
	ID      int
	Index   int // position in the owned vector (client index space)
	Sockets map[int][2]int
	Awake   bool
}

// EquippedTablet is one filled page slot.
type EquippedTablet struct {
	UID     int
	ID      int
	Index   int // owned-vector index, -1 when the instance is missing
	Sockets map[int][2]int
	Awake   bool
}

const inventoryVersionTabletInstances = 2

func socketsToJSON(sockets map[int][2]int) map[string][]int {
	out := map[string][]int{}
	for idx, pair := range sockets {
		if idx < 0 || pair[0] <= 0 {
			continue
		}
		qty := pair[1]
		if qty < 1 {
			qty = 1
		}
		out[strconv.Itoa(idx)] = []int{pair[0], qty}
	}
	return out
}

func socketsFromJSON(raw map[string][]int) map[int][2]int {
	out := map[int][2]int{}
	for si, pair := range raw {
		idx, err := strconv.Atoi(si)
		if err != nil || len(pair) < 1 || pair[0] <= 0 {
			continue
		}
		q := 1
		if len(pair) > 1 && pair[1] > 0 {
			q = pair[1]
		}
		out[idx] = [2]int{pair[0], q}
	}
	return out
}

func copySockets(raw map[string][]int) map[string][]int {
	out := map[string][]int{}
	for k, v := range raw {
		out[k] = append([]int(nil), v...)
	}
	return out
}

func parseSlotKey(key string) (page, slot int, ok bool) {
	if _, err := fmt.Sscanf(key, "%d:%d", &page, &slot); err != nil {
		return 0, 0, false
	}
	return page, slot, true
}

// ensureTabletInstancesLocked migrates a legacy record (or an Account built
// in code without instances) to the instance model. Idempotent.
func ensureTabletInstancesLocked(a *Account) bool {
	if a == nil || a.TabletInstances != nil {
		return false
	}
	owned := a.OwnedTablets
	if owned == nil && config.KitabeGrantAllTablets {
		owned = items.GrantableTabletIDs()
	}
	awake := map[int]bool{}
	for _, id := range a.AwakeTabletIDs {
		awake[id] = true
	}
	a.TabletInstances = []TabletInstance{}
	for _, id := range owned {
		if id <= 0 {
			continue
		}
		inst := TabletInstance{UID: a.nextTabletUIDLocked(), ID: id, Awake: awake[id]}
		if socks, ok := a.BackpackSockets[strconv.Itoa(id)]; ok {
			inst.Sockets = copySockets(socks)
		}
		a.TabletInstances = append(a.TabletInstances, inst)
	}
	// Bind the equipped slots to instances (lowest page/slot wins a copy;
	// an equipped id with no owned copy gets one appended so nothing
	// disappears from a page).
	keys := make([]string, 0, len(a.Tablets))
	for key := range a.Tablets {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		pi, si, _ := parseSlotKey(keys[i])
		pj, sj, _ := parseSlotKey(keys[j])
		return pi < pj || (pi == pj && si < sj)
	})
	// taken[page][uid]: a copy is unique within a page; the same copy on
	// another page is a legitimate second loadout, and the legacy record's
	// second page binds to the SAME instance (prefer the copy already bound
	// to that id on another page, then any unbound copy).
	taken := map[int]map[int]bool{}
	boundByID := map[int]int{}
	for _, key := range keys {
		rec := a.Tablets[key]
		page, _, _ := parseSlotKey(key)
		if rec.ID <= 0 {
			delete(a.Tablets, key)
			continue
		}
		if taken[page] == nil {
			taken[page] = map[int]bool{}
		}
		bound := -1
		if rec.UID > 0 {
			if i := a.tabletIndexByUIDLocked(rec.UID); i >= 0 && !taken[page][rec.UID] && a.TabletInstances[i].ID == rec.ID {
				bound = i
			}
		}
		if bound < 0 {
			if uid, ok := boundByID[rec.ID]; ok && !taken[page][uid] {
				bound = a.tabletIndexByUIDLocked(uid)
			}
		}
		if bound < 0 {
			ownedCopy := false
			for i, inst := range a.TabletInstances {
				if inst.ID != rec.ID {
					continue
				}
				ownedCopy = true
				if !taken[page][inst.UID] {
					bound = i
					break
				}
			}
			if bound < 0 && ownedCopy {
				// Same id twice on ONE page (pre-v0.1.3 EquipTablet
				// duplicated): keep the lowest slot only.
				delete(a.Tablets, key)
				continue
			}
		}
		if bound < 0 {
			a.TabletInstances = append(a.TabletInstances, TabletInstance{
				UID: a.nextTabletUIDLocked(), ID: rec.ID, Awake: awake[rec.ID],
			})
			bound = len(a.TabletInstances) - 1
		}
		inst := &a.TabletInstances[bound]
		if len(rec.Sockets) > 0 {
			inst.Sockets = copySockets(rec.Sockets)
		}
		taken[page][inst.UID] = true
		boundByID[inst.ID] = inst.UID
		a.Tablets[key] = TabletRec{UID: inst.UID, ID: inst.ID, Sockets: copySockets(inst.Sockets)}
	}
	syncTabletMirrorLocked(a)
	return true
}

func (a *Account) nextTabletUIDLocked() int {
	if a.NextTabletUID <= 0 {
		a.NextTabletUID = 1
	}
	uid := a.NextTabletUID
	a.NextTabletUID++
	return uid
}

// syncTabletMirrorLocked rewrites the legacy fields from the instances.
func syncTabletMirrorLocked(a *Account) {
	if a == nil {
		return
	}
	a.OwnedTablets = make([]int, 0, len(a.TabletInstances))
	a.BackpackSockets = map[string]map[string][]int{}
	a.AwakeTabletIDs = []int{}
	seenAwake := map[int]bool{}
	for _, inst := range a.TabletInstances {
		a.OwnedTablets = append(a.OwnedTablets, inst.ID)
		key := strconv.Itoa(inst.ID)
		if len(inst.Sockets) > 0 {
			if _, dup := a.BackpackSockets[key]; !dup {
				a.BackpackSockets[key] = copySockets(inst.Sockets)
			}
		}
		if inst.Awake && !seenAwake[inst.ID] {
			seenAwake[inst.ID] = true
			a.AwakeTabletIDs = append(a.AwakeTabletIDs, inst.ID)
		}
	}
	sort.Ints(a.AwakeTabletIDs)
	if a.Tablets == nil {
		a.Tablets = map[string]TabletRec{}
	}
	for key, rec := range a.Tablets {
		if i := a.tabletIndexByUIDLocked(rec.UID); i >= 0 {
			inst := a.TabletInstances[i]
			a.Tablets[key] = TabletRec{UID: inst.UID, ID: inst.ID, Sockets: copySockets(inst.Sockets)}
		}
	}
}

func (a *Account) tabletIndexByUIDLocked(uid int) int {
	if uid <= 0 {
		return -1
	}
	for i, inst := range a.TabletInstances {
		if inst.UID == uid {
			return i
		}
	}
	return -1
}

func tabletViewLocked(a *Account, i int) TabletView {
	inst := a.TabletInstances[i]
	return TabletView{UID: inst.UID, ID: inst.ID, Index: i, Sockets: socketsFromJSON(inst.Sockets), Awake: inst.Awake}
}

// normalizeDuplicateTabletsLocked drops a slot that references an instance
// already placed in a lower page/slot (one copy sits in one slot).
func (a *Account) normalizeDuplicateTabletsLocked() bool {
	if a == nil {
		return false
	}
	changed := ensureTabletInstancesLocked(a)
	if len(a.Tablets) < 2 {
		return changed
	}
	keys := make([]string, 0, len(a.Tablets))
	for key := range a.Tablets {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		pi, si, _ := parseSlotKey(keys[i])
		pj, sj, _ := parseSlotKey(keys[j])
		return pi < pj || (pi == pj && si < sj)
	})
	seen := map[[2]int]bool{} // {page, uid}: unique within a page only
	for _, key := range keys {
		rec := a.Tablets[key]
		if rec.UID <= 0 || a.tabletIndexByUIDLocked(rec.UID) < 0 {
			delete(a.Tablets, key)
			changed = true
			continue
		}
		page, _, _ := parseSlotKey(key)
		if seen[[2]int{page, rec.UID}] {
			delete(a.Tablets, key)
			changed = true
			continue
		}
		seen[[2]int{page, rec.UID}] = true
	}
	return changed
}

// ---- read API -------------------------------------------------------------

// OwnedTabletViews returns the owned vector in wire order.
func (a *Account) OwnedTabletViews() []TabletView {
	if a == nil {
		return nil
	}
	mu.Lock()
	defer mu.Unlock()
	ensureTabletInstancesLocked(a)
	out := make([]TabletView, 0, len(a.TabletInstances))
	for i := range a.TabletInstances {
		out = append(out, tabletViewLocked(a, i))
	}
	return out
}

// OwnedTabletIDs is the owned vector as tablet ids (duplicates possible).
func (a *Account) OwnedTabletIDs() []int {
	if a == nil {
		return nil
	}
	mu.Lock()
	defer mu.Unlock()
	ensureTabletInstancesLocked(a)
	out := make([]int, 0, len(a.TabletInstances))
	for _, inst := range a.TabletInstances {
		out = append(out, inst.ID)
	}
	return out
}

// TabletAt resolves a client-side index (TabletSlot[4][0] / backpack loop
// index carried by 0x4b/0x4e/0x4f/0x53) to the instance.
func (a *Account) TabletAt(index int) (TabletView, bool) {
	if a == nil || index < 0 {
		return TabletView{}, false
	}
	mu.Lock()
	defer mu.Unlock()
	ensureTabletInstancesLocked(a)
	if index >= len(a.TabletInstances) {
		return TabletView{}, false
	}
	return tabletViewLocked(a, index), true
}

// OwnedTabletAt is TabletAt reduced to the tablet id.
func (a *Account) OwnedTabletAt(index int) (int, bool) {
	v, ok := a.TabletAt(index)
	return v.ID, ok
}

// TabletByUID looks an instance up by its stable id.
func (a *Account) TabletByUID(uid int) (TabletView, bool) {
	if a == nil {
		return TabletView{}, false
	}
	mu.Lock()
	defer mu.Unlock()
	ensureTabletInstancesLocked(a)
	i := a.tabletIndexByUIDLocked(uid)
	if i < 0 {
		return TabletView{}, false
	}
	return tabletViewLocked(a, i), true
}

// OwnedTabletIndex is the owned-vector index of the first copy of tabletID,
// -1 when none is owned.
func (a *Account) OwnedTabletIndex(tabletID int) int {
	if a == nil {
		return -1
	}
	mu.Lock()
	defer mu.Unlock()
	ensureTabletInstancesLocked(a)
	for i, inst := range a.TabletInstances {
		if inst.ID == tabletID {
			return i
		}
	}
	return -1
}

// OwnedTabletCount is how many copies of tabletID the account owns
// (UserInfo::getTabletCount).
func (a *Account) OwnedTabletCount(tabletID int) int {
	n := 0
	for _, v := range a.OwnedTabletViews() {
		if v.ID == tabletID {
			n++
		}
	}
	return n
}

func equippedTabletsLocked(a *Account) map[[2]int]EquippedTablet {
	out := map[[2]int]EquippedTablet{}
	if a == nil {
		return out
	}
	ensureTabletInstancesLocked(a)
	for key, rec := range a.Tablets {
		page, slot, ok := parseSlotKey(key)
		if !ok {
			continue
		}
		i := a.tabletIndexByUIDLocked(rec.UID)
		if i < 0 {
			continue
		}
		v := tabletViewLocked(a, i)
		out[[2]int{page, slot}] = EquippedTablet{UID: v.UID, ID: v.ID, Index: v.Index, Sockets: v.Sockets, Awake: v.Awake}
	}
	return out
}

// EquippedTablets: (page,slot) → equipped instance.
func (a *Account) EquippedTablets() map[[2]int]EquippedTablet {
	mu.Lock()
	defer mu.Unlock()
	return equippedTabletsLocked(a)
}

// TabletSockets maps tablet id → sockets of the FIRST socketed copy. Kept
// for logs and tests; wire code uses OwnedTablets/EquippedTablets.
func (a *Account) TabletSockets() map[int]map[int][2]int {
	out := map[int]map[int][2]int{}
	for _, v := range a.OwnedTabletViews() {
		if len(v.Sockets) > 0 {
			if _, dup := out[v.ID]; !dup {
				out[v.ID] = v.Sockets
			}
		}
	}
	return out
}

// AwakeTablets maps tablet id → true when ANY owned copy is ascended. Kept
// for logs and tests; wire code reads TabletView.Awake per instance.
func (a *Account) AwakeTablets() map[int]bool {
	out := map[int]bool{}
	for _, v := range a.OwnedTabletViews() {
		if v.Awake {
			out[v.ID] = true
		}
	}
	return out
}

func (a *Account) TabletCapacity() int {
	if a == nil {
		return config.KitabeTabletCapacityDefault
	}
	mu.RLock()
	defer mu.RUnlock()
	if a.TabletPacketSize < 25 {
		return config.KitabeTabletCapacityDefault
	}
	return a.TabletPacketSize
}

// ---- mutations ------------------------------------------------------------

// tabletMutation runs mutate on a migrated record and re-syncs the mirror.
func tabletMutation(a *Account, mutate func()) {
	if a == nil {
		return
	}
	persistMutation(a, func() {
		ensureTabletInstancesLocked(a)
		mutate()
		syncTabletMirrorLocked(a)
	})
}

// SetBackpackSockets replaces the sockets of one instance.
func (a *Account) SetBackpackSockets(uid int, sockets map[int][2]int) {
	tabletMutation(a, func() {
		i := a.tabletIndexByUIDLocked(uid)
		if i < 0 {
			return
		}
		clean := socketsToJSON(sockets)
		if len(clean) == 0 {
			clean = nil
		}
		a.TabletInstances[i].Sockets = clean
	})
}

// EquipTablet places one instance in page/slot; the instance leaves any
// other slot it occupied (one copy sits in one slot).
func (a *Account) EquipTablet(page, slot, uid int) {
	if a == nil || uid <= 0 || page < 0 || page > 6 || slot < 0 || slot > 2 {
		return
	}
	tabletMutation(a, func() {
		i := a.tabletIndexByUIDLocked(uid)
		if i < 0 {
			return
		}
		if a.Tablets == nil {
			a.Tablets = map[string]TabletRec{}
		}
		targetKey := fmt.Sprintf("%d:%d", page, slot)
		// Pages are alternative loadouts (one is active in a match), so the
		// same copy may sit on several pages; it is unique only within a
		// page. v0.1.3–v0.1.9 evicted it from every other slot, so equipping
		// a tablet on page 2 silently emptied its slot on page 3 (player
		// video, 2026-09-18).
		for key, rec := range a.Tablets {
			if key == targetKey || rec.UID != uid {
				continue
			}
			if p, _, ok := parseSlotKey(key); ok && p == page {
				delete(a.Tablets, key)
			}
		}
		inst := a.TabletInstances[i]
		a.Tablets[targetKey] = TabletRec{UID: inst.UID, ID: inst.ID, Sockets: copySockets(inst.Sockets)}
	})
}

// UnequipTablet clears page/slot and returns the removed instance's UID (0
// when empty). Ascension survives unequipping (see SetTabletAwake).
func (a *Account) UnequipTablet(page, slot int) int {
	removed := 0
	tabletMutation(a, func() {
		key := fmt.Sprintf("%d:%d", page, slot)
		if rec, ok := a.Tablets[key]; ok {
			removed = rec.UID
			delete(a.Tablets, key)
		}
	})
	return removed
}

// SetTabletAwake sets the ascended flag of one instance. Ascension is a
// property of the copy itself (the loft shows unequipped ascended tablets as
// locked); only a paid unlock (SleepTablet) or deletion clears it.
func (a *Account) SetTabletAwake(uid int, awake bool) {
	tabletMutation(a, func() {
		if i := a.tabletIndexByUIDLocked(uid); i >= 0 {
			a.TabletInstances[i].Awake = awake
		}
	})
}

// DeleteTablet handles trade 0x53: unequip, return socketed inscriptions and
// drop the copy from the owned vector (later indexes shift down; the client
// re-reads the whole vector from the reply).
func (a *Account) DeleteTablet(uid int) {
	tabletMutation(a, func() {
		i := a.tabletIndexByUIDLocked(uid)
		if i < 0 {
			return
		}
		inst := a.TabletInstances[i]
		if a.Inscriptions == nil {
			a.Inscriptions = map[string]int{}
		}
		for _, pair := range socketsFromJSON(inst.Sockets) {
			a.Inscriptions[strconv.Itoa(pair[0])] += pair[1]
		}
		for key, rec := range a.Tablets {
			if rec.UID == uid {
				delete(a.Tablets, key)
			}
		}
		a.TabletInstances = append(a.TabletInstances[:i], a.TabletInstances[i+1:]...)
	})
}

// SleepTabletWithEmblem is SleepTablet with PayEmblem.
func (a *Account) SleepTabletWithEmblem(uid, cost int) bool {
	return a.SleepTablet(uid, PayEmblem, cost)
}

// SleepTablet atomically charges the unlock cost and clears the ascended
// flag of one instance. Repeating an already-completed unlock is idempotent.
func (a *Account) SleepTablet(uid, payType, cost int) bool {
	if a == nil || uid <= 0 || cost < 0 {
		return false
	}
	mu.Lock()
	defer mu.Unlock()
	ensureTabletInstancesLocked(a)
	i := a.tabletIndexByUIDLocked(uid)
	if i < 0 {
		return false
	}
	if !a.TabletInstances[i].Awake {
		return true
	}
	switch payType {
	case PayEmblem:
		if a.Emblem < cost {
			return false
		}
		a.Emblem -= cost
	case PayRune:
		if a.Rune < cost {
			return false
		}
		a.Rune -= cost
	default:
		return false
	}
	a.TabletInstances[i].Awake = false
	syncTabletMirrorLocked(a)
	_ = saveLocked()
	return true
}

// canGrantTabletLocked: another copy is allowed while the loft has room.
func (a *Account) canGrantTabletLocked() bool {
	ensureTabletInstancesLocked(a)
	capacity := config.KitabeTabletCapacityDefault
	if a.TabletPacketSize >= 25 {
		capacity = a.TabletPacketSize
	}
	return len(a.TabletInstances) < capacity
}

func (a *Account) grantTabletLocked(tabletID, qty int) {
	ensureTabletInstancesLocked(a)
	for n := 0; n < qty; n++ {
		a.TabletInstances = append(a.TabletInstances, TabletInstance{UID: a.nextTabletUIDLocked(), ID: tabletID})
	}
	syncTabletMirrorLocked(a)
}

func (a *Account) ExpandTabletCapacity() (current, next int) {
	persistMutation(a, func() {
		current = a.TabletPacketSize
		if current < 25 {
			current = config.KitabeTabletCapacityDefault
		}
		current = minInt(200, maxInt(current+25, config.KitabeTabletCapacityDefault))
		a.TabletPacketSize = current
		next = minInt(200, current+25)
	})
	if a == nil {
		return config.KitabeTabletCapacityDefault, config.KitabeTabletCapacityDefault + 25
	}
	return current, next
}
