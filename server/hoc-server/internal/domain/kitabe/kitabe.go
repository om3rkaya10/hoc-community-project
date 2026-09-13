package kitabe

import (
	"fmt"

	"hoc-server/internal/accounts"
	"hoc-server/internal/wire/msgpack"
)

const (
	SlotFilled    = 0
	SlotEmptyOpen = 1
	SlotLocked    = 2
)

func inscriptionInfo(itemID, qty int, owned bool) []byte {
	var out []byte
	out = append(out, msgpack.FixArray(13)...)
	out = append(out, msgpack.Int(int64(itemID))...)
	for i := 0; i < 4; i++ {
		out = append(out, msgpack.Int(0)...)
	}
	out = append(out, msgpack.Bool(owned)...)
	out = append(out, msgpack.Int(0)...)
	out = append(out, msgpack.Float32(0)...)
	out = append(out, msgpack.Int(0)...)
	out = append(out, msgpack.Int(0)...)
	out = append(out, msgpack.Int(int64(qty))...)
	out = append(out, msgpack.EmptyArray()...)
	out = append(out, msgpack.EmptyArray()...)
	return out
}

func inscriptionSlot(filled bool, itemID, qty int) []byte {
	var out []byte
	out = append(out, msgpack.FixArray(3)...)
	out = append(out, msgpack.Bool(filled)...)
	out = append(out, inscriptionInfo(itemID, qty, filled)...)
	out = append(out, msgpack.EmptyArray()...)
	return out
}

func TabletInfo(itemID int, sockets map[int][2]int, awake bool) []byte {
	if sockets == nil {
		sockets = map[int][2]int{}
	}
	keys := sortedKeys(sockets)
	var out []byte
	out = append(out, msgpack.FixArray(16)...)
	out = append(out, msgpack.Int(int64(itemID))...)
	out = append(out, msgpack.FixMap(len(keys))...)
	for _, idx := range keys {
		pair := sockets[idx]
		out = append(out, msgpack.Int(int64(idx))...)
		out = append(out, inscriptionSlot(true, pair[0], pair[1])...)
	}
	out = append(out, msgpack.Int(0)...)
	out = append(out, msgpack.Int(0)...)
	out = append(out, msgpack.Float32(0)...)
	out = append(out, msgpack.Int(0)...)
	out = append(out, msgpack.Int(0)...)
	out = append(out, msgpack.Float32(0)...)
	wake := int64(0)
	if awake {
		wake = 2
	}
	out = append(out, msgpack.Int(wake)...)
	for i := 0; i < 6; i++ {
		out = append(out, msgpack.Int(0)...)
	}
	out = append(out, msgpack.EmptyArray()...)
	return out
}

func sortedKeys(m map[int][2]int) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

func InscriptionMap(pairs [][4]int) []byte {
	byOuter := map[int][][3]int{}
	for _, p := range pairs {
		byOuter[p[0]] = append(byOuter[p[0]], [3]int{p[1], p[2], p[3]})
	}
	outers := make([]int, 0, len(byOuter))
	for o := range byOuter {
		outers = append(outers, o)
	}
	for i := 0; i < len(outers); i++ {
		for j := i + 1; j < len(outers); j++ {
			if outers[j] < outers[i] {
				outers[i], outers[j] = outers[j], outers[i]
			}
		}
	}
	var out []byte
	out = append(out, msgpack.FixMap(len(outers))...)
	for _, o := range outers {
		inners := byOuter[o]
		out = append(out, msgpack.Int(int64(o))...)
		out = append(out, msgpack.FixMap(len(inners))...)
		for _, in := range inners {
			out = append(out, msgpack.Int(int64(in[0]))...)
			out = append(out, inscriptionInfo(in[1], in[2], true)...)
		}
	}
	return out
}

// OwnedTabletsVector is the backpack (UserInfo+0xB4C vector<TabletInfo>).
// Its order is the client's tablet index space: TabletSlot[5]
// (indexIDInPacket) and the sleep/delete/wake requests all refer to a
// position in this vector, so callers must pass the account's OwnedTabletIDs
// unchanged.
func OwnedTabletsVector(ids []int, sockets map[int]map[int][2]int, awake map[int]bool) []byte {
	var out []byte
	out = append(out, msgpack.FixArray(len(ids))...)
	for _, tid := range ids {
		socks := sockets[tid]
		out = append(out, TabletInfo(tid, socks, awake[tid])...)
	}
	return out
}

// OwnedIndexMap maps tablet id → position in the owned vector.
func OwnedIndexMap(ids []int) map[int]int {
	out := make(map[int]int, len(ids))
	for i, id := range ids {
		if _, dup := out[id]; !dup {
			out[id] = i
		}
	}
	return out
}

// EquippedSlotsVector is UserInfo+0xB40. TabletSlot[5] carries the owned
// index of each equipped tablet (DlgTabletPage::RefreshTabletButtonGroup
// hides that backpack entry and stores the index on the card; the unlock,
// delete and wake requests send it back).
func EquippedSlotsVector(equipped map[[2]int]struct {
	ID      int
	Sockets map[int][2]int
}, awake map[int]bool, autoWakeAll bool, ownedIndex map[int]int) []byte {
	type slot struct {
		id   int
		sock map[int][2]int
	}
	var slots []slot
	keys := make([][2]int, 0, len(equipped))
	for k := range equipped {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j][0] < keys[i][0] || (keys[j][0] == keys[i][0] && keys[j][1] < keys[i][1]) {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	for _, k := range keys {
		eq := equipped[k]
		if eq.ID == 0 {
			continue
		}
		slots = append(slots, slot{id: eq.ID, sock: eq.Sockets})
	}
	var out []byte
	out = append(out, msgpack.FixArray(len(slots))...)
	for _, s := range slots {
		isAwake := autoWakeAll || awake[s.id]
		out = append(out, tabletSlotWithPacketIndex(SlotFilled, 0, 0, s.id, s.sock, isAwake, ownedIndexOf(ownedIndex, s.id))...)
	}
	return out
}

// ownedIndexOf falls back to -1 (the client's "no backpack entry" default)
// for a tablet that is equipped but missing from the owned vector.
func ownedIndexOf(ownedIndex map[int]int, tabletID int) int {
	if idx, ok := ownedIndex[tabletID]; ok {
		return idx
	}
	return -1
}

func tabletSlot(state, emblemCost, runeCost, itemID int, socks map[int][2]int, awake bool) []byte {
	return tabletSlotWithPacketIndex(state, emblemCost, runeCost, itemID, socks, awake, 0)
}

func tabletSlotWithPacketIndex(state, emblemCost, runeCost, itemID int, socks map[int][2]int, awake bool, packetIndex int) []byte {
	var out []byte
	out = append(out, msgpack.FixArray(6)...)
	out = append(out, TabletInfo(itemID, socks, awake)...)
	out = append(out, msgpack.Int(int64(state))...)
	out = append(out, msgpack.Int(int64(emblemCost))...)
	out = append(out, msgpack.Int(int64(runeCost))...)
	// [4] vector<int>: TabletSlot::getIndexIDInPacket() = getIntVal(0) reads
	// element 0 of this vector (+0x6c), NOT the trailing [5] int. An empty
	// vector reads back as 0, i.e. "backpack entry 0".
	out = append(out, msgpack.FixArray(1)...)
	out = append(out, msgpack.SignedInt32(int64(packetIndex))...)
	out = append(out, msgpack.Int(int64(packetIndex))...)
	return out
}

func priceField(a, b, c uint16) []byte {
	var out []byte
	out = append(out, msgpack.FixArray(5)...)
	out = append(out, msgpack.U16(a)...)
	out = append(out, msgpack.U16(b)...)
	out = append(out, msgpack.U16(c)...)
	out = append(out, msgpack.EmptyArray()...)
	out = append(out, msgpack.EmptyArray()...)
	return out
}

func slotGroup(unlocked bool, slots [][]byte, pageRune, pageEmblem int) []byte {
	var out []byte
	out = append(out, msgpack.FixArray(5)...)
	out = append(out, msgpack.Bool(unlocked)...)
	out = append(out, msgpack.FixArray(len(slots))...)
	for _, s := range slots {
		out = append(out, s...)
	}
	out = append(out, priceField(uint16(pageRune), uint16(pageEmblem), 0)...)
	out = append(out, msgpack.EmptyArray()...)
	out = append(out, msgpack.EmptyArray()...)
	return out
}

// FullGroups map keys 1..numPages — all_open empty slots use state=1.
func FullGroups(numPages, slotsPerPage int, allOpen bool, equipped map[[2]int]struct {
	ID      int
	Sockets map[int][2]int
}, awake map[int]bool, autoWakeAll bool, unlocked map[int]bool, states map[string]int, ownedIndex map[int]int) []byte {
	if len(unlocked) == 0 {
		unlocked = map[int]bool{}
		for page := 1; page <= numPages; page++ {
			unlocked[page] = true
		}
	}
	var out []byte
	out = append(out, msgpack.FixMap(numPages)...)
	for page := 1; page <= numPages; page++ {
		page0 := page - 1
		pageOpen := unlocked[page]
		slots := make([][]byte, 0, slotsPerPage)
		for si := 0; si < slotsPerPage; si++ {
			eq, ok := equipped[[2]int{page0, si}]
			if !pageOpen {
				slots = append(slots, tabletSlot(SlotLocked, 100*(si+1), 500*(si+1), 0, nil, false))
			} else if ok && eq.ID != 0 {
				aw := autoWakeAll || awake[eq.ID]
				slots = append(slots, tabletSlotWithPacketIndex(
					SlotFilled, 0, 0, eq.ID, eq.Sockets, aw, ownedIndexOf(ownedIndex, eq.ID),
				))
			} else if state, exists := states[fmt.Sprintf("%d:%d", page0, si)]; exists {
				emblemCost, runeCost := 0, 0
				if state == SlotLocked {
					emblemCost, runeCost = 100*(si+1), 500*(si+1)
				}
				slots = append(slots, tabletSlot(state, emblemCost, runeCost, 0, nil, false))
			} else if allOpen {
				slots = append(slots, tabletSlot(SlotEmptyOpen, 0, 0, 0, nil, false))
			} else {
				slots = append(slots, tabletSlot(SlotLocked, 100, 500, 0, nil, false))
			}
		}
		out = append(out, msgpack.Int(int64(page))...)
		pageRune, pageEmblem := 0, 0
		if !pageOpen {
			pageRune, pageEmblem = 50, 10
		}
		out = append(out, slotGroup(pageOpen, slots, pageRune, pageEmblem)...)
	}
	return out
}

func awakeSet(a *accounts.Account) map[int]bool {
	if a == nil {
		return map[int]bool{}
	}
	return a.AwakeTablets()
}

// GESubMember10 — BuyItem[17]: +0xB58 / +0xB40 / +0xB4C.
func GESubMember10(a *accounts.Account) []byte {
	pairs := a.InscriptionPairs()
	equipped := a.EquippedTablets()
	socks := a.TabletSockets()
	aw := awakeSet(a)
	owned := a.OwnedTabletIDs()
	idx := OwnedIndexMap(owned)
	var out []byte
	out = append(out, msgpack.FixArray(3)...)
	out = append(out, InscriptionMap(pairs)...)
	out = append(out, EquippedSlotsVector(equipped, aw, false, idx)...)
	out = append(out, OwnedTabletsVector(owned, socks, aw)...)
	return out
}

// UnlockResponse builds S2C 0x4a (11-elem).
func UnlockResponse(a *accounts.Account) []byte {
	return UnlockResponseWithResultCallback(a, 0, 0, 0)
}

// UnlockResponseWithCallback keeps a successful full authoritative Kitabe state
// while supplying the operation-specific callback target in trailing fields [9]/[10].
func UnlockResponseWithCallback(a *accounts.Account, callbackA, callbackB int) []byte {
	return UnlockResponseWithResultCallback(a, 0, callbackA, callbackB)
}

func UnlockResponseWithResultCallback(a *accounts.Account, result, callbackA, callbackB int) []byte {
	emblem, runeV := 99999, 9999
	if a != nil {
		emblem, runeV = a.Emblem, a.Rune
	}
	pairs := a.InscriptionPairs()
	equipped := a.EquippedTablets()
	socks := a.TabletSockets()
	aw := awakeSet(a)
	owned := a.OwnedTabletIDs()
	idx := OwnedIndexMap(owned)
	unlocked := map[int]bool{}
	states := map[string]int{}
	if a != nil {
		unlocked = a.UnlockedPageSet()
		states = a.SlotStateSnapshot()
	}
	var out []byte
	out = append(out, msgpack.FixArray(11)...)
	out = append(out, msgpack.Int(int64(result))...)
	out = append(out, msgpack.Int(int64(emblem))...)
	out = append(out, msgpack.Int(int64(runeV))...)
	out = append(out, InscriptionMap(pairs)...)
	out = append(out, EquippedSlotsVector(equipped, aw, false, idx)...)
	out = append(out, OwnedTabletsVector(owned, socks, aw)...)
	out = append(out, msgpack.Int(0)...)
	out = append(out, msgpack.Int(0)...)
	out = append(out, FullGroups(7, 3, true, equipped, aw, false, unlocked, states, idx)...)
	out = append(out, msgpack.Int(int64(callbackA))...)
	out = append(out, msgpack.Int(int64(callbackB))...)
	return out
}

func ExpandResponse(result, current, next, emblem, runeV, payType int) []byte {
	var out []byte
	out = append(out, msgpack.FixArray(9)...)
	out = append(out, msgpack.Int(int64(result))...)
	out = append(out, msgpack.Int(int64(current))...)
	out = append(out, msgpack.Int(int64(next))...)
	out = append(out, msgpack.Int(int64(emblem))...)
	out = append(out, msgpack.Int(int64(runeV))...)
	out = append(out, msgpack.Int(int64(payType))...)
	out = append(out, msgpack.RawStr(nil)...)
	out = append(out, msgpack.Int(0)...)
	out = append(out, msgpack.Int(0)...)
	return out
}

func SelectGroupResponse(result, groupID int) []byte {
	var out []byte
	out = append(out, msgpack.FixArray(2)...)
	out = append(out, msgpack.Int(int64(result))...)
	out = append(out, msgpack.Int(int64(groupID))...)
	return out
}
