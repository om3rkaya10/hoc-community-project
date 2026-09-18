package trade

import (
	"fmt"
	"math/rand"
	"net"
	"time"

	"hoc-server/internal/accounts"
	"hoc-server/internal/config"
	"hoc-server/internal/domain/items"
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
	registry[0x5b] = handleUseFlag
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

// handleSelectFlag — trade 0x5c (UserInfo::SendSelectFlagRequest @0xc21808):
// [26, name, pattern, pole, type(0x11 normal / 0x12 guild), uid].
func handleSelectFlag(c *Ctx) bool {
	a := acc(c)
	pole, pattern, flagType := a.FlagSelection()
	if rq, err := msgpack.Decode(c.Body); err == nil {
		if arr, ok := rq.([]any); ok && len(arr) >= 5 {
			if v, ok := asInt(arr[2]); ok {
				pattern = v
			}
			if v, ok := asInt(arr[3]); ok {
				pole = v
			}
			if v, ok := asInt(arr[4]); ok && v != 0 {
				flagType = v
			}
		}
	}
	if flagType == 0 {
		flagType = 0x11
	}
	if a != nil {
		a.SetFlagSelection(pole, pattern, flagType)
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

// handleUseFlag — trade 0x5b from Unit::CostOutGameBanner:
// [26, name, patternID (Object u32 0x56), 1, false, uid]. One banner charge
// is consumed; no S2C reply is expected.
func handleUseFlag(c *Ctx) bool {
	a := acc(c)
	if a == nil {
		return true
	}
	patternID := 0
	if rq, err := msgpack.Decode(c.Body); err == nil {
		if arr, ok := rq.([]any); ok && len(arr) >= 3 {
			if v, ok := asInt(arr[2]); ok && items.IsPattern(v) {
				patternID = v
			}
		}
	}
	if patternID == 0 {
		_, patternID, _ = a.FlagSelection()
	}
	a.UsePattern(patternID)
	fmt.Printf(" [GS] ★UseFlag 0x5b pattern=%d left=%d\n", patternID, a.PatternCounts()[patternID])
	return true
}

func handleBuyItemCRM(c *Ctx) bool {
	a := acc(c)
	// [2] item, [3] qty, [4] payType, [5] line total, [6] unit price. A 50-pack
	// of banners arrives as qty=50 with [5] = the pack price, so the debit is
	// the line total, never unit×qty.
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
	granted := false
	if a != nil {
		granted = a.Purchase(itemID, qty, payType, price)
	}
	body := wiregs.BuildBuyItem(a, wiregs.BuyItemOptions{
		Ownership: true,
		Kitabe:    config.ServerKitabe,
	})
	c.Send(0x6e, body)
	fmt.Printf(" [GS SENT] ★BuyItemCRM 0x6e item=%d(%s type=%d) qty=%d pay=%d price=%d granted=%v (%dB)\n",
		itemID, items.Name(itemID), items.TypeOf(itemID), qty, payType, price, granted, len(body))
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
	if c.Sub == 0x4e {
		result, payType, ownedIndex, tabletID := handleSleepTablet(c.Body, a)
		body := kitabe.UnlockResponseWithResultCallback(a, result, payType, ownedIndex)
		c.Send(c.Sub, body)
		fmt.Printf(" [GS SENT] ★Kitabe SLEEP 0x4e result=%d tablet=%d pay=%d index=%d (%dB)\n",
			result, tabletID, payType, ownedIndex, len(body))
		return true
	}
	if c.Sub == 0x53 || c.Sub == 0x4b || c.Sub == 0x4c || c.Sub == 0x4d {
		fmt.Printf(" [GS] kitabe %#x body=%x owned=%v\n", c.Sub, c.Body, a.OwnedTabletIDs())
	}
	reply := applyKitabeMutation(c.Sub, c.Body, a)
	body := kitabe.UnlockResponseFull(a, 0, reply.resultItem, reply.ascended, 0, 0)
	c.Send(c.Sub, body)
	fmt.Printf(" [GS SENT] ★Kitabe ACK %#x item=%d ascended=%d (%dB)\n", c.Sub, reply.resultItem, reply.ascended, len(body))
	return true
}

// handleSleepTablet — trade 0x4e (UserInfo::SendSleepTabletRequest):
// [26, name, 1, ownedIndex, payType, uid, page1]. ownedIndex is the tablet's
// position in the owned TabletInfo vector (TabletButton+0x400, set from
// TabletSlot::getIndexIDInPacket for equipped cards or the backpack loop
// index). The reply's [10] is echoed as that index (onSleepTabletResponse
// reads UserInfo+0xB4C[index]).
func handleSleepTablet(body []byte, a *accounts.Account) (result, payType, ownedIndex, tabletID int) {
	result = 1
	if a == nil || len(body) == 0 {
		return result, payType, ownedIndex, tabletID
	}
	rq, err := msgpack.Decode(body)
	if err != nil {
		return result, payType, ownedIndex, tabletID
	}
	arr, ok := rq.([]any)
	if !ok || len(arr) < 5 {
		return result, payType, ownedIndex, tabletID
	}
	index, ok := asInt(arr[3])
	if !ok {
		return result, payType, ownedIndex, tabletID
	}
	payType, _ = asInt(arr[4])
	cost := 0
	switch payType {
	case accounts.PayEmblem:
		cost = config.KitabeSleepEmblem
	case accounts.PayRune:
		cost = config.KitabeSleepRune
	default:
		fmt.Printf(" [GS] ★0x4e SLEEP rejected payType=%d\n", payType)
		return result, payType, ownedIndex, tabletID
	}
	inst, ok := a.TabletAt(index)
	if !ok {
		fmt.Printf(" [GS] ★0x4e SLEEP rejected index=%d owned=%d body=%x\n", index, len(a.OwnedTabletIDs()), body)
		return result, payType, ownedIndex, tabletID
	}
	tabletID = inst.ID
	ownedIndex = index
	if !a.SleepTablet(inst.UID, payType, cost) {
		fmt.Printf(" [GS] ★0x4e SLEEP rejected tablet=%d uid=%d pay=%d cost=%d insufficient\n", tabletID, inst.UID, payType, cost)
		return result, payType, ownedIndex, tabletID
	}
	return 0, payType, ownedIndex, tabletID
}

// Inscription tiers (item_prototype_hoc.tbl, 16 stats each). The EXCHANGE
// page trades 4 inscriptions of one tier for ONE RANDOM inscription of the
// next tier (the result slot shows a "?"); the third panel swaps one gold for
// a chosen gold against emblems/runes. The event golds 1044-1050 are never
// produced by an exchange.
const (
	inscriptionTierBronze = 1
	inscriptionTierSilver = 2
	inscriptionTierGold   = 3
)

var inscriptionTiers = map[int][]int{
	inscriptionTierBronze: {447, 448, 449, 450, 451, 452, 454, 455, 456, 457, 458, 459, 460, 461, 462, 463},
	inscriptionTierSilver: {480, 482, 484, 486, 488, 490, 513, 494, 496, 498, 500, 502, 504, 506, 508, 510},
	inscriptionTierGold:   {481, 483, 485, 487, 489, 491, 493, 495, 497, 499, 501, 503, 505, 507, 509, 511},
}

func inscriptionTierOf(id int) int {
	for tier, ids := range inscriptionTiers {
		for _, v := range ids {
			if v == id {
				return tier
			}
		}
	}
	return 0
}

// randomInscription picks the exchange result; swapped by tests.
var randomInscription = func(pool []int) int {
	return pool[rand.Intn(len(pool))]
}

// inscriptionRows reads a vector<InscriptionInfo> and returns the item ids.
func inscriptionRows(v any) ([]int, bool) {
	rows, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]int, 0, len(rows))
	for _, raw := range rows {
		row, ok := raw.([]any)
		if !ok || len(row) == 0 {
			return nil, false
		}
		id, ok := asInt(row[0])
		if !ok || id <= 0 {
			return nil, false
		}
		out = append(out, id)
	}
	return out, true
}

// mergeInscriptionSource parses 0x50 ([26, username, 0, vector<InscriptionInfo>,
// uid]): four inscriptions of one bronze/silver tier, any stats. Returns the
// consumed ids and a random inscription of the next tier.
func mergeInscriptionSource(arr []any) (sources []int, targetID int, ok bool) {
	if len(arr) < 4 {
		return nil, 0, false
	}
	sources, ok = inscriptionRows(arr[3])
	if !ok || len(sources) != 4 {
		return nil, 0, false
	}
	tier := inscriptionTierOf(sources[0])
	for _, id := range sources[1:] {
		if inscriptionTierOf(id) != tier {
			return nil, 0, false
		}
	}
	next, ok := inscriptionTiers[tier+1]
	if !ok || tier == 0 {
		return nil, 0, false
	}
	return sources, randomInscription(next), true
}

// exchangeGoldSource parses 0x59 (TradeMessageExchangeGoldInscriptions,
// live capture 2026-09-18: [26, name, targetItemID, payType,
// vector[sourceInscriptionInfo], uid]). The price is not on the wire — the
// client only checks its wallet against UserInfo+0x310/+0x314 — so the
// server charges its own config price for payType.
func exchangeGoldSource(arr []any) (sourceID, targetID, payType int, ok bool) {
	if len(arr) < 5 {
		return 0, 0, 0, false
	}
	targetID, _ = asInt(arr[2])
	payType, _ = asInt(arr[3])
	rows, ok := inscriptionRows(arr[4])
	if !ok || len(rows) != 1 {
		return 0, 0, 0, false
	}
	sourceID = rows[0]
	if sourceID == targetID ||
		inscriptionTierOf(sourceID) != inscriptionTierGold ||
		inscriptionTierOf(targetID) != inscriptionTierGold {
		return 0, 0, 0, false
	}
	return sourceID, targetID, payType, true
}

// kitabeReply carries the per-operation ints of the Unlock family reply
// (kitabe.UnlockResponseFull [6] and [7]).
type kitabeReply struct {
	resultItem int // [6]: inscription produced by 0x50 / 0x59
	ascended   int // [7]: 1 when a 0x4f request ascended its tablet
}

// applyKitabeMutation applies one Kitabe request to the account and returns
// the reply's per-operation fields.
func applyKitabeMutation(sub uint16, body []byte, a *accounts.Account) (reply kitabeReply) {
	if a == nil || len(body) == 0 {
		return reply
	}
	rq, err := msgpack.Decode(body)
	if err != nil {
		return reply
	}
	arr, ok := rq.([]any)
	if !ok {
		return reply
	}
	page0 := a.SelectedPage0()
	owned := a.OwnedTabletViews()
	// tabletAt resolves a client owned-vector index to the instance.
	tabletAt := func(idx *int) (accounts.TabletView, bool) {
		if idx == nil || *idx < 0 || *idx >= len(owned) {
			return accounts.TabletView{}, false
		}
		return owned[*idx], true
	}

	switch sub {
	case 0x50:
		sources, targetID, ok := mergeInscriptionSource(arr)
		if !ok {
			fmt.Printf(" [GS] ★0x50 MERGE rejected malformed/mixed tiers body=%x\n", body)
			return reply
		}
		if !a.ExchangeInscriptionSet(sources, targetID, 0, 0) {
			fmt.Printf(" [GS] ★0x50 MERGE rejected sources=%v insufficient\n", sources)
			return reply
		}
		reply.resultItem = targetID
		fmt.Printf(" [GS] ★0x50 MERGE sources=%v → %d (%s)\n", sources, targetID, items.Name(targetID))
	case 0x59:
		sourceID, targetID, payType, ok := exchangeGoldSource(arr)
		if !ok {
			fmt.Printf(" [GS] ★0x59 EXCHANGE_GOLD rejected malformed body=%x\n", body)
			return reply
		}
		price := 0
		switch payType {
		case accounts.PayEmblem:
			price = config.KitabeExchangeGoldEmblem
		case accounts.PayRune:
			price = config.KitabeExchangeGoldRune
		default:
			fmt.Printf(" [GS] ★0x59 EXCHANGE_GOLD rejected payType=%d\n", payType)
			return reply
		}
		if !a.ExchangeInscriptionSet([]int{sourceID}, targetID, payType, price) {
			fmt.Printf(" [GS] ★0x59 EXCHANGE_GOLD rejected source=%d target=%d pay=%d insufficient\n", sourceID, targetID, payType)
			return reply
		}
		reply.resultItem = targetID
		fmt.Printf(" [GS] ★0x59 EXCHANGE_GOLD %d → %d (%s) pay=%d price=%d\n", sourceID, targetID, items.Name(targetID), payType, price)
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
		if inst, ok := tabletAt(idx); ok && slot != nil {
			// Wearing never ascends. TabletInfo[8]==2 ("Ascended": locked
			// for editing, passive active in match) is granted only by the
			// 0x4f ascend flag below. Auto-waking here locked every equipped
			// tablet and made a paid 0x4e unlock moot as soon as the player
			// re-equipped the tablet on another page.
			a.EquipTablet(page, *slot, inst.UID)
			fmt.Printf(" [GS] ★0x4b WEAR id=%d uid=%d index=%d page=%d slot=%d ascended=%v\n", inst.ID, inst.UID, inst.Index, page, *slot, inst.Awake)
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
	case 0x4d:
		// SendWakeTabletRequest has no caller in the client (ascension goes
		// through the 0x4f ascend flag); kept for completeness:
		// [26, name, slotIdx, ownedIndex, page1, uid].
		var target *accounts.TabletView
		for _, pos := range []int{3, 2} {
			if len(arr) > pos {
				if idx, ok := asInt(arr[pos]); ok {
					if inst, ok := tabletAt(&idx); ok {
						target = &inst
						break
					}
				}
			}
		}
		if len(arr) > 4 {
			if v, ok := asInt(arr[4]); ok && v >= 1 && v <= 7 {
				a.SetSelectedPage(v)
			}
		}
		if target != nil {
			a.SetTabletAwake(target.UID, true)
			fmt.Printf(" [GS] ★0x4d WAKE id=%d uid=%d\n", target.ID, target.UID)
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
		// [3] is the owned-vector index of the copy being edited
		// (DlgTabletPage copies TabletButton+0x400 into
		// DlgInscriptionPage+0xb0c); with several copies of one tablet it
		// is the only thing that tells them apart. Fall back to the first
		// copy of the TabletInfo id when the index is missing/-1.
		var target *accounts.TabletView
		if inst, ok := tabletAt(idx); ok && (tid == 0 || inst.ID == tid) {
			target = &inst
		} else {
			for i := range owned {
				if owned[i].ID == tid {
					target = &owned[i]
					break
				}
			}
		}
		// SendFillInscriptionSlotRequest is define_array<int,string,int,int,
		// TabletInfo,int,int,int> = [26, name, 1, idx, TabletInfo, ascend,
		// uid, page1]. DlgInscriptionPage::OnClickedConfirmBox sends
		// ascend=1 only after the "Do you want to ascend this tablet?"
		// confirm (both energy bars full and TabletInfo+0x34 != 2); the
		// plain "save your modification" path sends 0. 0x51 has no such field.
		ascend := false
		if sub == 0x4f && len(arr) > 5 {
			if v, ok := asInt(arr[5]); ok && v != 0 {
				ascend = true
			}
		}
		if target != nil {
			tid = target.ID
			old := target.Sockets
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
			a.SetBackpackSockets(target.UID, socks)
			if ascend && len(socks) > 0 {
				a.SetTabletAwake(target.UID, true)
				reply.ascended = 1
			}
			fmt.Printf(" [GS] ★%#x SOCKET tablet=%d uid=%d index=%d n=%d ascend=%v\n", sub, tid, target.UID, target.Index, len(socks), ascend)
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
		// SendDeleteTabletRequest, live capture 2026-09-13:
		// [26, name, ownedIndex, 1, uid, page1] — unlike 0x4e the index is
		// packed before the constant 1.
		var target *accounts.TabletView
		if len(arr) > 2 {
			if idx, ok := asInt(arr[2]); ok {
				if inst, ok := tabletAt(&idx); ok {
					target = &inst
				}
			}
		}
		if len(arr) > 5 {
			if v, ok := asInt(arr[5]); ok && v >= 1 && v <= 7 {
				a.SetSelectedPage(v)
			}
		}
		if target != nil {
			a.DeleteTablet(target.UID)
			fmt.Printf(" [GS] ★0x53 DELETE_TABLET id=%d uid=%d index=%d\n", target.ID, target.UID, target.Index)
		} else {
			fmt.Printf(" [GS] ★0x53 DELETE_TABLET rejected body=%x\n", body)
		}
	}
	return reply
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
