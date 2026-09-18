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

// SocketCount is the number of inscription sockets every tablet card draws
// (TabletButton::SetTabletInfo loops 0..3, TabletInfo::fillSlot inserts the
// same four keys client-side).
const SocketCount = 4

func TabletInfo(itemID int, sockets map[int][2]int, awake bool) []byte {
	var out []byte
	out = append(out, msgpack.FixArray(16)...)
	out = append(out, msgpack.Int(int64(itemID))...)
	// [1] map<int,InscriptionSlot> always carries all four sockets. The
	// card only hides a socket's energy-type marker when it finds the key
	// with filled=false; a missing key leaves the marker in its initial
	// state (visible, movie clip still playing), which is the Order/Chaos
	// flicker seen on empty tablets in the loft.
	out = append(out, msgpack.FixMap(SocketCount)...)
	for idx := 0; idx < SocketCount; idx++ {
		out = append(out, msgpack.Int(int64(idx))...)
		if pair, ok := sockets[idx]; ok && pair[0] > 0 {
			out = append(out, inscriptionSlot(true, pair[0], pair[1])...)
		} else {
			out = append(out, inscriptionSlot(false, 0, 0)...)
		}
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
// Its order is the client's tablet index space: TabletSlot[4][0]
// (indexIDInPacket) and the sleep/delete/fill requests all refer to a
// position in this vector, so it is emitted exactly in account order.
func OwnedTabletsVector(owned []accounts.TabletView) []byte {
	var out []byte
	out = append(out, msgpack.FixArray(len(owned))...)
	for _, v := range owned {
		out = append(out, TabletInfo(v.ID, v.Sockets, v.Awake)...)
	}
	return out
}

// EquippedSlotsVector is UserInfo+0xB40. TabletSlot[4][0] carries the owned
// index of each equipped copy (DlgTabletPage::RefreshTabletButtonGroup hides
// that backpack entry and stores the index on the card; the unlock, delete
// and fill requests send it back).
func EquippedSlotsVector(equipped map[[2]int]accounts.EquippedTablet) []byte {
	keys := sortedSlotKeys(equipped)
	var out []byte
	out = append(out, msgpack.FixArray(len(keys))...)
	for _, k := range keys {
		eq := equipped[k]
		out = append(out, tabletSlotWithPacketIndex(SlotFilled, 0, 0, eq.ID, eq.Sockets, eq.Awake, packetIndexOf(eq))...)
	}
	return out
}

func sortedSlotKeys(equipped map[[2]int]accounts.EquippedTablet) [][2]int {
	keys := make([][2]int, 0, len(equipped))
	for k, eq := range equipped {
		if eq.ID != 0 {
			keys = append(keys, k)
		}
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j][0] < keys[i][0] || (keys[j][0] == keys[i][0] && keys[j][1] < keys[i][1]) {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

// packetIndexOf falls back to -1 (the client's "no backpack entry" default)
// for a slot whose instance is missing from the owned vector.
func packetIndexOf(eq accounts.EquippedTablet) int {
	if eq.Index < 0 {
		return -1
	}
	return eq.Index
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
func FullGroups(numPages, slotsPerPage int, allOpen bool, equipped map[[2]int]accounts.EquippedTablet, unlocked map[int]bool, states map[string]int) []byte {
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
				slots = append(slots, tabletSlotWithPacketIndex(
					SlotFilled, 0, 0, eq.ID, eq.Sockets, eq.Awake, packetIndexOf(eq),
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

// GESubMember10 — BuyItem[17]: +0xB58 / +0xB40 / +0xB4C.
func GESubMember10(a *accounts.Account) []byte {
	var out []byte
	out = append(out, msgpack.FixArray(3)...)
	out = append(out, InscriptionMap(a.InscriptionPairs())...)
	out = append(out, EquippedSlotsVector(a.EquippedTablets())...)
	out = append(out, OwnedTabletsVector(a.OwnedTabletViews())...)
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
	return UnlockResponseFull(a, result, 0, 0, callbackA, callbackB)
}

// UnlockResponseFull additionally sets the two per-operation ints:
//   - [6] resultItem: DispatchTradeMsg passes it as the third argument of
//     onMergeInscriptionResponse / onMergeGoldInscriptionResponse, which
//     run Item::GetPrototype/GetDisplayInfo on it to show the inscription
//     the exchange produced (0 → nothing is shown).
//   - [7] ascended: third argument of onFillInscriptionSlotResponse;
//     non-zero keeps the page open and plays the ascension animation, zero
//     returns to the tablet page.
func UnlockResponseFull(a *accounts.Account, result, resultItem, ascended, callbackA, callbackB int) []byte {
	emblem, runeV := 99999, 9999
	if a != nil {
		emblem, runeV = a.Emblem, a.Rune
	}
	pairs := a.InscriptionPairs()
	equipped := a.EquippedTablets()
	owned := a.OwnedTabletViews()
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
	out = append(out, EquippedSlotsVector(equipped)...)
	out = append(out, OwnedTabletsVector(owned)...)
	out = append(out, msgpack.Int(int64(resultItem))...)
	out = append(out, msgpack.Int(int64(ascended))...)
	out = append(out, FullGroups(7, 3, true, equipped, unlocked, states)...)
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
