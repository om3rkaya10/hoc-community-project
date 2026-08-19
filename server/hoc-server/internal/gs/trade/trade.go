package trade

import (
	"fmt"
	"net"
	"time"

	"hoc-server/internal/accounts"
	"hoc-server/internal/config"
	"hoc-server/internal/domain/kitabe"
	"hoc-server/internal/domain/talent"
	"hoc-server/internal/session"
	wiregs "hoc-server/internal/wire/gs"
	"hoc-server/internal/wire/msgpack"
)

// Ctx is the trade request context (no I/O ownership beyond Send).
type Ctx struct {
	Conn         net.Conn
	Sess         *session.Session
	Custom       bool
	Loading      bool
	TskCID       int
	Body         []byte
	Sub          uint16
	Send         func(sub uint16, body []byte) // trade S2C 0x0d/sub
	KitabeSeeded *bool
}

type Handler func(c *Ctx) (handled bool)

var registry = map[uint16]Handler{}

func init() {
	registry[1] = handleUserInfo
	registry[5] = handleBuyItem
	registry[0x31] = handleSync
	registry[0x83] = handleSync
	registry[0x71] = handleSync
	registry[0x73] = handleSync
	registry[0x27] = handleAchievement
	registry[0x12] = handleEpilogue
	registry[0x89] = handleEpilogue
	registry[0xa] = handleUserInGame
	registry[0x4a] = handleKitabeFamily
	registry[0x4b] = handleKitabeFamily
	registry[0x4c] = handleKitabeFamily
	registry[0x4d] = handleKitabeFamily
	registry[0x4e] = handleKitabeFamily
	registry[0x4f] = handleKitabeFamily
	registry[0x50] = handleKitabeFamily
	registry[0x51] = handleKitabeFamily
	registry[0x52] = handleKitabeFamily
	registry[0x53] = handleKitabeFamily
	registry[0x59] = handleKitabeFamily
	registry[0x7c] = handleKitabeFamily
	registry[0x82] = handleSelectTablet
	registry[0x81] = handleInputAge
	registry[0x42] = handleTalentUnlock
	registry[0x43] = handleTalentUnlock
	registry[0x1e] = handleTalentOp
	registry[0x1f] = handleTalentOp
	registry[0x3b] = handleTalentOp
	registry[0x3c] = handleTalentOp
	registry[0x5c] = handleSelectFlag
	registry[0x5d] = handleExpandTablet
	registry[0x70] = handleNickname
	registry[0x6e] = handleBuyItemCRM
}

func Dispatch(c *Ctx) {
	if c.Custom && config.CustomNoLoadMap && !c.Loading {
		if c.Sub == 1 || c.Sub == 5 {
			fmt.Printf(" [GS] trade sub=%#x IGNORED (custom seat)\n", c.Sub)
			return
		}
	}
	h, ok := registry[c.Sub]
	if !ok {
		fmt.Printf(" [GS] trade sub=%#x IGNORED (no typed ACK)\n", c.Sub)
		return
	}
	if h(c) {
		return
	}
}

func acc(c *Ctx) *accounts.Account {
	if c.Sess != nil {
		return c.Sess.Account
	}
	return nil
}

func handleUserInfo(c *Ctx) bool {
	body := wiregs.BuildUserInfo(acc(c))
	c.Send(1, body)
	return true
}

func handleBuyItem(c *Ctx) bool {
	body := wiregs.BuildBuyItemEmpty(acc(c))
	c.Send(5, body)
	return true
}

func handleSync(c *Ctx) bool {
	c.Send(c.Sub, wiregs.TradeResultAck(26, 0))
	// Kitabe after-menu seed on first 0x31 (not mid custom seat).
	if c.Sub == 0x31 && config.ServerKitabe && config.KitabeSeedAfterMenu &&
		c.KitabeSeeded != nil && !*c.KitabeSeeded {
		menuOK := !(c.Custom && config.CustomNoLoadMap && !c.Loading)
		if menuOK {
			*c.KitabeSeeded = true
			go func() {
				if config.KitabeSeedDelayMS > 0 {
					time.Sleep(time.Duration(config.KitabeSeedDelayMS) * time.Millisecond)
				}
				body := kitabe.UnlockResponse(acc(c))
				c.Send(0x4a, body)
				fmt.Printf(" [GS SENT] ★UnlockTablet 0x4a after-menu (%dB)\n", len(body))
			}()
		}
	}
	return true
}

func handleAchievement(c *Ctx) bool {
	c.Send(0x27, wiregs.TradeAchievementAck(wiregs.TradeReqName(c.Body)))
	return true
}

func handleEpilogue(c *Ctx) bool {
	c.Send(c.Sub, msgpack.EmptyArray())
	return true
}

func handleUserInGame(c *Ctx) bool {
	body := c.Body
	if len(body) == 0 {
		body = wiregs.TradeResultAck(26, 0)
	}
	c.Send(0xa, body)
	return true
}

func handleSelectTablet(c *Ctx) bool {
	if !config.ServerKitabe {
		return true
	}
	gid := 1
	if rq, err := msgpack.Decode(c.Body); err == nil {
		if arr, ok := rq.([]any); ok && len(arr) >= 2 {
			if v, ok := asInt(arr[len(arr)-1]); ok && v > 0 {
				gid = v
			} else if v, ok := asInt(arr[1]); ok && v > 0 {
				gid = v
			}
		}
	} else {
		for i := len(c.Body) - 1; i >= 0; i-- {
			if c.Body[i] < 0x80 && c.Body[i] > 0 {
				gid = int(c.Body[i])
				break
			}
		}
	}
	if a := acc(c); a != nil {
		a.SetSelectedPage(gid)
	}
	c.Send(0x82, kitabe.SelectGroupResponse(0, gid))
	return true
}

func handleInputAge(c *Ctx) bool {
	a := acc(c)
	age, gender, _ := accounts.DefaultAge, accounts.DefaultGender, 1
	if a != nil {
		age, gender, _ = a.AgeFields()
	}
	ints := scanMsgpackPosInts(c.Body)
	switch {
	case len(ints) >= 2 && ints[len(ints)-2] >= 13:
		age, gender = ints[len(ints)-2], ints[len(ints)-1]
	case len(ints) >= 3 && ints[len(ints)-3] >= 13:
		age, gender = ints[len(ints)-3], ints[len(ints)-2]
	case len(ints) >= 1 && ints[len(ints)-1] >= 13:
		age = ints[len(ints)-1]
	}
	if gender != 1 && gender != 2 {
		gender = accounts.DefaultGender
	}
	if a != nil {
		a.SetAge(age, gender)
	}
	fmt.Printf(" [ACCOUNT] ★InputAge age=%d gender=%d\n", age, gender)
	c.Send(0x81, wiregs.TradeResultAck(26, 0))
	return true
}

func scanMsgpackPosInts(b []byte) []int {
	var out []int
	i := 0
	for i < len(b) {
		t := b[i]
		switch {
		case t < 0x80:
			out = append(out, int(t))
			i++
		case t == 0xcc && i+1 < len(b):
			out = append(out, int(b[i+1]))
			i += 2
		case t == 0xcd && i+2 < len(b):
			out = append(out, int(b[i+1])<<8|int(b[i+2]))
			i += 3
		case t == 0xce && i+4 < len(b):
			v := int(b[i+1])<<24 | int(b[i+2])<<16 | int(b[i+3])<<8 | int(b[i+4])
			out = append(out, v)
			i += 5
		case t >= 0xa0 && t <= 0xbf:
			i += 1 + int(t&0x1f)
		default:
			i++
		}
	}
	return out
}

func handleTalentUnlock(c *Ctx) bool {
	if !config.ServerTalent {
		fmt.Printf(" [GS] trade talent %#x IGNORED\n", c.Sub)
		return true
	}
	a := acc(c)
	groupID, runeCost := 1, 0
	var layerID *int
	if rq, err := msgpack.Decode(c.Body); err == nil {
		if arr, ok := rq.([]any); ok {
			if len(arr) > 3 {
				if v, ok := asInt(arr[3]); ok {
					groupID = v
				}
			}
			if c.Sub == 0x43 {
				if len(arr) > 5 {
					if v, ok := asInt(arr[5]); ok {
						layerID = &v
					}
				}
				if len(arr) > 6 {
					runeCost, _ = asInt(arr[6])
				}
			} else if len(arr) > 4 {
				runeCost, _ = asInt(arr[4])
			}
		}
	} else {
		fmt.Printf(" [TRADE] talent unlock %#x parse=%v\n", c.Sub, err)
	}
	runeV, emblem := 9999, 99999
	groups := map[int]accounts.TalentGroupRec{}
	if a != nil {
		groups = a.UnlockTalent(groupID, layerID, runeCost)
		emblem, runeV, _ = a.Wallet()
	} else {
		groups = (&accounts.Account{TalentPoints: config.TalentPointsDefault}).EnsureTalentPages()
	}
	c.Send(c.Sub, talent.UnlockResponse(0, runeV, emblem, groups))
	fmt.Printf(" [GS SENT] â˜…UnlockTalent %#x group=%d cost=%d\n", c.Sub, groupID, runeCost)
	return true
}

func handleTalentOp(c *Ctx) bool {
	if !config.ServerTalent {
		fmt.Printf(" [GS] trade talent-op %#x IGNORED\n", c.Sub)
		return true
	}
	a := acc(c)
	name := []byte("player")
	groupID := 1
	var infos [][]int
	reset := c.Sub == 0x3c
	if a != nil {
		name = []byte(a.Username)
		if a.Nickname != "" {
			name = []byte(a.Nickname)
		}
	}
	if rq, err := msgpack.Decode(c.Body); err == nil {
		if arr, ok := rq.([]any); ok && len(arr) >= 3 {
			if s, ok := asString(arr[1]); ok {
				name = []byte(s)
				if v, ok := asInt(arr[2]); ok {
					groupID = v
				}
				infos = parseTalentInfos(arr, 3)
			} else if s, ok := asString(arr[2]); ok {
				name = []byte(s)
				if len(arr) > 3 {
					if v, ok := asInt(arr[3]); ok {
						groupID = v
					}
				}
				infos = parseTalentInfos(arr, 4)
			} else if v, ok := asInt(arr[2]); ok {
				groupID = v
				infos = parseTalentInfos(arr, 3)
			}
		}
	}
	var g accounts.TalentGroupRec
	pts := config.TalentPointsDefault
	if a != nil {
		_, g = a.ApplyTalentUpdate(groupID, infos, reset)
		pts = g.Echo
	} else {
		g = accounts.TalentGroupRec{Echo: pts, Unlocked: true, Limit: pts, F18: pts, Talents: infos}
	}
	c.Send(c.Sub, talent.UnlockedTalentResponse(0, name, groupID, g, pts))
	fmt.Printf(" [GS SENT] ★UnlockedTalent %#x group=%d reset=%v n=%d\n",
		c.Sub, groupID, reset, len(g.Talents))
	return true
}

func parseTalentInfos(arr []any, start int) [][]int {
	if start >= len(arr) {
		return nil
	}
	if list, ok := arr[start].([]any); ok {
		var out [][]int
		for _, row := range list {
			if r, ok := row.([]any); ok && len(r) >= 1 {
				tid, _ := asInt(r[0])
				trk := 0
				if len(r) > 1 {
					trk, _ = asInt(r[1])
				}
				c := 0
				if len(r) > 2 {
					c, _ = asInt(r[2])
				}
				out = append(out, []int{tid, trk, c})
			}
		}
		return out
	}
	// single-op: [26, name, group, talentId, rank, ...]
	if tid, ok := asInt(arr[start]); ok {
		trk := 1
		if start+1 < len(arr) {
			if v, ok := asInt(arr[start+1]); ok {
				trk = v
			}
		}
		return [][]int{{tid, trk, 0}}
	}
	return nil
}

func handleSelectFlag(c *Ctx) bool {
	pole, pattern, flagType := 0, 0, 0x11
	if rq, err := msgpack.Decode(c.Body); err == nil {
		if arr, ok := rq.([]any); ok {
			if len(arr) >= 4 {
				if tag, ok := asInt(arr[0]); ok && (tag == 0x1a || tag == 26) {
					pole, _ = asInt(arr[1])
					pattern, _ = asInt(arr[2])
					if v, ok := asInt(arr[3]); ok && v != 0 {
						flagType = v
					}
				} else if len(arr) >= 3 {
					pole, _ = asInt(arr[0])
					pattern, _ = asInt(arr[1])
					if v, ok := asInt(arr[2]); ok && v != 0 {
						flagType = v
					}
				}
			} else if len(arr) >= 3 {
				pole, _ = asInt(arr[0])
				pattern, _ = asInt(arr[1])
				if v, ok := asInt(arr[2]); ok && v != 0 {
					flagType = v
				}
			}
		}
	}
	body := wiregs.SelectFlagResponse(pole, pattern, flagType)
	c.Send(0x5c, body)
	fmt.Printf(" [GS SENT] ★SelectFlag 0x5c pole=%d pattern=%d type=%#x\n",
		pole, pattern, flagType)
	return true
}

func handleExpandTablet(c *Ctx) bool {
	if !config.ServerKitabe {
		fmt.Printf(" [GS] trade 0x5d Expand IGNORED (SERVER_KITABE=False)\n")
		return true
	}
	a := acc(c)
	emblem, runeV := 99999, 9999
	cur, next := 50, 75
	if a != nil {
		emblem, runeV, _ = a.Wallet()
		cur, next = a.ExpandTabletCapacity()
	}
	c.Send(0x5d, kitabe.ExpandResponse(0, cur, next, emblem, runeV, 5))
	fmt.Printf(" [GS SENT] ★ExpandTablet 0x5d size=%d next=%d\n", cur, next)
	return true
}

func handleNickname(c *Ctx) bool {
	a := acc(c)
	nick, user := "Player", "player"
	if a != nil {
		if a.Nickname != "" {
			nick = a.Nickname
		} else if a.Username != "" {
			nick = a.Username
		}
		if a.Username != "" {
			user = a.Username
		}
	}
	if rq, err := msgpack.Decode(c.Body); err == nil {
		if arr, ok := rq.([]any); ok {
			if len(arr) >= 2 {
				if s, ok := asString(arr[1]); ok && s != "" {
					user = s
				}
			}
			if len(arr) >= 4 {
				if s, ok := asString(arr[3]); ok && s != "" {
					nick = s
					if a != nil {
						a.SetNickname(nick)
					}
				}
			}
		}
	}
	c.Send(0x70, wiregs.NicknameResponse(nick, user, 0))
	fmt.Printf(" [GS SENT] ★Nickname 0x70 nick=%q\n", nick)
	return true
}

func handleBuyItemCRM(c *Ctx) bool {
	a := acc(c)
	itemID, qty, payType, price := 0, 1, 5, 0
	if rq, err := msgpack.Decode(c.Body); err == nil {
		if arr, ok := rq.([]any); ok && len(arr) >= 7 {
			itemID, _ = asInt(arr[2])
			if v, ok := asInt(arr[3]); ok && v > 0 {
				qty = v
			}
			payType, _ = asInt(arr[4])
			if v, ok := asInt(arr[5]); ok && v != 0 {
				price = v
			} else if v, ok := asInt(arr[6]); ok {
				price = v
			}
		}
	}
	if a != nil {
		a.PurchaseCRM(itemID, qty, payType, price)
	}
	body := wiregs.BuildBuyItem(a, wiregs.BuyItemOptions{
		Ownership: true,
		Kitabe:    config.ServerKitabe,
	})
	c.Send(0x6e, body)
	fmt.Printf(" [GS SENT] ★BuyItemCRM 0x6e item=%d qty=%d pay=%d price=%d (%dB)\n",
		itemID, qty, payType, price, len(body))
	return true
}

func handleKitabeFamily(c *Ctx) bool {
	if !config.ServerKitabe {
		fmt.Printf(" [GS] trade kitabe %#x IGNORED (SERVER_KITABE=False)\n", c.Sub)
		return true
	}
	if c.Custom && config.CustomNoLoadMap && !c.Loading && c.Sub == 0x4a {
		// Unsolicited/login mid-seat UnlockTablet was a crash class; C2S mid-seat
		// still ignored for bare 0x4a (Python SEAT_SEED off). Other family ACKs OK.
		fmt.Printf(" [GS] trade 0x4a IGNORED (custom seat)\n")
		return true
	}
	a := acc(c)
	applyKitabeMutation(c.Sub, c.Body, a)
	body := kitabe.UnlockResponse(a)
	c.Send(c.Sub, body)
	fmt.Printf(" [GS SENT] ★Kitabe ACK %#x (%dB)\n", c.Sub, len(body))
	return true
}

func applyKitabeMutation(sub uint16, body []byte, a *accounts.Account) {
	if a == nil || len(body) == 0 {
		return
	}
	rq, err := msgpack.Decode(body)
	if err != nil {
		return
	}
	arr, ok := rq.([]any)
	if !ok {
		return
	}
	page0 := a.SelectedPage0()
	ids := kitabe.TabletItemIDs()

	switch sub {
	case 0x4b:
		page, slot, idx := kitabeWirePageSlot(arr, page0, "slot_first")
		if slot == nil {
			if v, ok := asInt(arrElem(arr, 2)); ok {
				slot = &v
			}
		}
		if idx == nil {
			if v, ok := asInt(arrElem(arr, 3)); ok {
				idx = &v
			}
		}
		a.SetSelectedPage(page + 1)
		if slot != nil && idx != nil && *idx >= 0 && *idx < len(ids) {
			tid := ids[*idx]
			a.EquipTablet(page, *slot, tid)
			a.SetTabletAwake(tid, true)
			fmt.Printf(" [GS] ★0x4b WEAR id=%d page=%d slot=%d\n", tid, page, *slot)
		}
	case 0x4c:
		page, slot, _ := kitabeWirePageSlot(arr, page0, "slot_first")
		if slot == nil {
			if v, ok := asInt(arrElem(arr, 2)); ok {
				slot = &v
			}
		}
		a.SetSelectedPage(page + 1)
		if slot != nil {
			a.UnequipTablet(page, *slot)
			fmt.Printf(" [GS] ★0x4c UNEQUIP page=%d slot=%d\n", page, *slot)
		}
	case 0x4d, 0x4e:
		page, slot, _ := kitabeWirePageSlot(arr, page0, "sleep")
		a.SetSelectedPage(page + 1)
		tid := 0
		eq := a.EquippedTablets()
		if slot != nil {
			if e, ok := eq[[2]int{page, *slot}]; ok {
				tid = e.ID
			}
		}
		if tid == 0 {
			for candidate := 0; candidate < 3; candidate++ {
				if e, ok := eq[[2]int{page, candidate}]; ok && e.ID != 0 {
					tid = e.ID
					break
				}
			}
		}
		if tid != 0 {
			a.SetTabletAwake(tid, sub == 0x4d)
			fmt.Printf(" [GS] ★%#x WAKE/SLEEP tid=%d\n", sub, tid)
		}
	case 0x4a:
		if len(arr) >= 3 {
			if slot, ok := asInt(arr[2]); ok {
				a.UnlockSlot(page0, slot)
				fmt.Printf(" [GS] ★0x4a UNLOCK SLOT page=%d slot=%d\n", page0, slot)
			}
		}
	case 0x7c:
		page1 := page0 + 1
		if len(arr) > 2 {
			if v, ok := asInt(arr[2]); ok && v >= 1 && v <= 7 {
				page1 = v
			}
		}
		pay := -1
		if len(arr) >= 4 {
			pay, _ = asInt(arr[3])
		}
		switch pay {
		case 2:
			a.Debit(2, 50)
		case 5:
			a.Debit(5, 10)
		}
		a.UnlockPage(page1)
		fmt.Printf(" [GS] ★0x7c UNLOCK PAGE %d pay=%d\n", page1, pay)
	case 0x4f, 0x51:
		page, _, idx := kitabeWirePageSlot(arr, page0, "insc")
		a.SetSelectedPage(page + 1)
		tid, socks := parseTabletInfoSockets(arr)
		if tid == 0 && idx != nil && *idx >= 0 && *idx < len(ids) {
			tid = ids[*idx]
		}
		if tid != 0 {
			old := a.TabletSockets()[tid]
			keep := map[int]bool{}
			for _, p := range socks {
				keep[p[0]] = true
			}
			for _, pair := range old {
				oid := pair[0]
				if oid != 0 && !keep[oid] {
					qty := pair[1]
					if qty < 1 {
						qty = 1
					}
					a.AddInscription(oid, qty)
				}
			}
			a.SetBackpackSockets(tid, socks)
			fmt.Printf(" [GS] ★%#x SOCKET tablet=%d n=%d\n", sub, tid, len(socks))
		}
	case 0x52:
		if len(arr) >= 3 {
			iid, _ := asInt(arr[2])
			qty := 1
			if len(arr) >= 4 {
				if v, ok := asInt(arr[3]); ok && v > 0 {
					qty = v
				}
			}
			a.RemoveInscription(iid, qty)
			fmt.Printf(" [GS] ★0x52 DELETE_INSC id=%d qty=%d\n", iid, qty)
		}
	case 0x53:
		page, slot, idx := kitabeWirePageSlot(arr, page0, "delete")
		a.SetSelectedPage(page + 1)
		tid := 0
		eq := a.EquippedTablets()
		if slot != nil {
			if e, ok := eq[[2]int{page, *slot}]; ok {
				tid = e.ID
			}
		}
		if tid == 0 && idx != nil && *idx >= 0 && *idx < len(ids) {
			tid = ids[*idx]
		}
		if tid != 0 {
			a.DeleteTablet(tid)
			fmt.Printf(" [GS] ★0x53 DELETE_TABLET id=%d\n", tid)
		}
	}
}

func arrElem(arr []any, i int) any {
	if i < 0 || i >= len(arr) {
		return nil
	}
	return arr[i]
}

// kitabeWirePageSlot mirrors Python _kitabe_wire_page_slot (page 0-based).
func kitabeWirePageSlot(rq []any, defaultPage0 int, mode string) (page int, slot, idx *int) {
	page = defaultPage0
	if len(rq) < 3 {
		return page, nil, nil
	}
	var ints []int
	for _, x := range rq[2:] {
		if v, ok := asInt(x); ok {
			ints = append(ints, v)
		}
	}
	if len(ints) == 0 {
		return page, nil, nil
	}
	last := ints[len(ints)-1]
	var trail *int
	if last >= 1 && last <= 7 {
		p := last - 1
		trail = &p
	}
	a := ints[0]
	var b *int
	if len(ints) > 1 {
		bb := ints[1]
		b = &bb
	}
	if trail != nil {
		page = *trail
	}
	switch mode {
	case "sleep":
		if b != nil && *b >= 0 && *b <= 2 {
			slot = b
		} else if a >= 0 && a <= 2 {
			slot = &a
		}
		return page, slot, nil
	case "delete":
		if a >= 0 && a <= 2 {
			slot = &a
			idx = &a
			return page, slot, idx
		}
		idx = &a
		return page, nil, idx
	case "insc":
		if a >= 0 && a <= 2 {
			slot = &a
		}
		return page, slot, b
	default: // slot_first
		if a >= 0 && a <= 2 {
			slot = &a
		}
		return page, slot, b
	}
}

func parseTabletInfoSockets(arr []any) (tid int, socks map[int][2]int) {
	socks = map[int][2]int{}
	if len(arr) < 5 {
		return 0, socks
	}
	ti, ok := arr[4].([]any)
	if !ok || len(ti) == 0 {
		return 0, socks
	}
	tid, _ = asInt(ti[0])
	if len(ti) < 2 {
		return tid, socks
	}
	m, ok := ti[1].(map[any]any)
	if !ok {
		return tid, socks
	}
	for k, entry := range m {
		si, ok := asInt(k)
		if !ok {
			continue
		}
		ent, ok := entry.([]any)
		if !ok || len(ent) < 2 {
			continue
		}
		filled, _ := ent[0].(bool)
		if !filled {
			continue
		}
		ii, ok := ent[1].([]any)
		if !ok || len(ii) < 1 {
			continue
		}
		inscID, _ := asInt(ii[0])
		qty := 1
		if len(ii) > 10 {
			if v, ok := asInt(ii[10]); ok && v > 0 {
				qty = v
			}
		}
		if inscID > 0 {
			socks[si] = [2]int{inscID, qty}
		}
	}
	return tid, socks
}

func asInt(v any) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case int8:
		return int(t), true
	case int16:
		return int(t), true
	case int32:
		return int(t), true
	case int64:
		return int(t), true
	case uint8:
		return int(t), true
	case uint16:
		return int(t), true
	case uint32:
		return int(t), true
	case uint64:
		return int(t), true
	default:
		return 0, false
	}
}

func asString(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case []byte:
		return string(t), true
	default:
		return "", false
	}
}
