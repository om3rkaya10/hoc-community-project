package gs

import (
	"encoding/base64"
	"encoding/binary"
	"sort"

	"hoc-server/internal/accounts"
	"hoc-server/internal/config"
	"hoc-server/internal/domain/kitabe"
	"hoc-server/internal/domain/talent"
	"hoc-server/internal/wire/msgpack"
)

func itemInfo(itemID, quantity int) []byte {
	var out []byte
	out = append(out, msgpack.FixArray(7)...)
	out = append(out, msgpack.Int(int64(itemID))...)
	out = append(out, msgpack.RawStr(nil)...)
	out = append(out, msgpack.Int(0)...)
	// setUserInfo copies ItemInfo source +0x0c (wire [3]) into the
	// getItemCount() entry's quantity field. Wire [2] is not the count.
	out = append(out, msgpack.Int(int64(quantity))...)
	out = append(out, msgpack.Int(0)...)
	out = append(out, msgpack.EmptyArray()...)
	out = append(out, msgpack.EmptyArray()...)
	return out
}

// inventoryVector is the consumable ItemInfo inventory (GetUserInfo[7] /
// BuyItem[13]); UserInfo::getItemCount reads it for shop own-counts and the
// bag's item tab.
func inventoryVector(a *accounts.Account) []byte {
	counts := a.ItemCounts()
	ids := make([]int, 0, len(counts))
	for id := range counts {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	var out []byte
	out = append(out, msgpack.FixArray(len(ids))...)
	for _, id := range ids {
		out = append(out, itemInfo(id, counts[id])...)
	}
	return out
}

// hocPole encodes HocPole / HocFlag (7-elem, define_array<i,Ss,i,Ss,i,vec<int>,vec<Ss>>
// @0xc7b4cc): [2] is the count UserInfo::HasPole / GetPatternCount read.
func hocPole(itemID, count int) []byte {
	var out []byte
	out = append(out, msgpack.FixArray(7)...)
	out = append(out, msgpack.Int(int64(itemID))...)
	out = append(out, msgpack.RawStr(nil)...)
	out = append(out, msgpack.Int(int64(count))...)
	out = append(out, msgpack.RawStr(nil)...)
	out = append(out, msgpack.Int(0)...)
	out = append(out, msgpack.EmptyArray()...)
	out = append(out, msgpack.EmptyArray()...)
	return out
}

func hocPoleMap(counts map[int]int) []byte {
	ids := make([]int, 0, len(counts))
	for id, n := range counts {
		if id > 0 && n > 0 {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	var out []byte
	out = append(out, msgpack.FixMap(len(ids))...)
	for _, id := range ids {
		out = append(out, msgpack.Int(int64(id))...)
		out = append(out, hocPole(id, counts[id])...)
	}
	return out
}

// GESub5Member18 is the flag ownership blob (unpack @0xc79f28):
// [0] achievements map<str,map<str,str>>, [1] map<int,HocPole> → UserInfo+0xB70,
// [2] map<int,HocFlag> → +0xB88, [3] current pole → +0xBA0, [4] current
// pattern → +0xBA8, [5] flag type → +0xBB0. The BuyItem handler
// (DispatchTradeMsg @0x1281b58) assigns all of these unconditionally, so every
// BuyItem reply must carry it at [19] or the client forgets its flags.
func GESub5Member18(a *accounts.Account) []byte {
	pole, pattern, flagType := a.FlagSelection()
	var out []byte
	out = append(out, msgpack.FixArray(6)...)
	out = append(out, msgpack.EmptyMap()...)
	out = append(out, hocPoleMap(a.PoleCounts())...)
	out = append(out, hocPoleMap(a.PatternCounts())...)
	out = append(out, msgpack.Int(int64(pole))...)
	out = append(out, msgpack.Int(int64(pattern))...)
	out = append(out, msgpack.Int(int64(flagType))...)
	return out
}

// GESub5Member18B64 is the GetUserInfo strvec[0x12] form
// (UserInfo::clientParseGEMember18 @0xc1cc98: DecodeBase64 → msgpack).
func GESub5Member18B64(a *accounts.Account) []byte {
	return []byte(base64.StdEncoding.EncodeToString(GESub5Member18(a)))
}

// BuildUserInfo — trade sub1. Talent map at [14] when SERVER_TALENT (wipe-safe).
func BuildUserInfo(a *accounts.Account) []byte {
	return buildUserInfo(a, 0)
}

// BuildUserInfoLoginInject is the GetUserInfo that immediately precedes the
// login BuyItem inject. It under-reports rune by 1 so that the following
// BuyItem ([4]=true rune) produces a non-zero rune delta in the client's
// BuyItem handler (DispatchTradeMsg @0x1290994: delta!=0 -> CCGiftInfo +
// tracking path). Verified live 2026-09-13: with a zero delta at login the
// client never starts its post-login Gaia chain (/alerts/me, /devices,
// /locate/asset, game_object -> CRM) and the shop/hero-select item requests
// time out ("Oyun sunucusundan yanıt bekleniyor"); with any non-zero delta
// (old all-99999 layout gave +90000, experiment gave +1) the chain runs.
// The BuyItem that follows sets the HUD to the true value, so nothing
// user-visible changes. Do not use for the client's own 0x1 requests.
func BuildUserInfoLoginInject(a *accounts.Account) []byte {
	return buildUserInfo(a, -1)
}

func buildUserInfo(a *accounts.Account, runeAdj int) []byte {
	level, runeV, emblem, gems := 40, 9999, 99999, 99999
	nick, user := "Player", "player"
	talentPts := config.TalentPointsDefault
	tabletPkt := 50
	selGroup := 1
	if a != nil {
		if a.Level > 0 {
			level = a.Level
		}
		runeV, emblem, gems = a.Rune, a.Emblem, a.Gems
		if a.Nickname != "" {
			nick = a.Nickname
		}
		if a.Username != "" {
			user = a.Username
		}
		if a.TalentPoints > 0 {
			talentPts = a.TalentPoints
		}
		if a.SelectedTabletGroup > 0 {
			selGroup = a.SelectedTabletGroup
		}
		tabletPkt = a.TabletCapacity()
	}
	if runeV+runeAdj >= 0 {
		runeV += runeAdj
	}
	n := 0x12f
	iv := make([]int64, n)
	iv[0] = int64(level)
	iv[2] = int64(runeV)
	iv[3] = int64(emblem)
	iv[0x28] = int64(talentPts)
	iv[0x88] = int64(tabletPkt)
	iv[0x4f] = int64(config.KitabeSleepEmblem)        // getUserSleepTabletEmblem
	iv[0x50] = int64(config.KitabeSleepRune)          // getUserSleepTabletRune
	iv[0x6b] = int64(config.KitabeExchangeGoldEmblem) // getUserExchangeGoldEmblems
	iv[0x6c] = int64(config.KitabeExchangeGoldRune)   // getUserExchangeGoldRunes
	iv[0x10b] = int64(selGroup)
	iv[244] = int64(gems)
	age, gender, saved := accounts.DefaultAge, accounts.DefaultGender, 1
	if a != nil {
		age, gender, saved = a.AgeFields()
	}
	iv[0x10d] = int64(age)    // getUserAges
	iv[0x12a] = int64(gender) // getUserGender
	iv[0x12e] = int64(saved)  // getUserIsSavedAge — 0 ⇒ DlgAge every login
	iv[0x27] = 1
	iv[0x2a] = 1
	for _, idx := range []int{0x122, 0x123, 0x124, 0x125, 0x126, 0x127, 0x128} {
		if idx < len(iv) {
			iv[idx] = 0xFFFF
		}
	}
	var intvec []byte
	intvec = append(intvec, msgpack.FixArray(len(iv))...)
	for _, x := range iv {
		intvec = append(intvec, msgpack.Int(x)...)
	}
	strSlots := make([][]byte, 0x13)
	strSlots[7] = []byte(user)
	strSlots[9] = []byte(nick)
	strSlots[0x12] = GESub5Member18B64(a) // getGESubMember18 = getStrVal(0x12)
	var strvec []byte
	strvec = append(strvec, msgpack.FixArray(len(strSlots))...)
	for _, s := range strSlots {
		strvec = append(strvec, msgpack.RawStr(s)...)
	}
	tmap := talent.MapFromAccount(a)
	var out []byte
	out = append(out, msgpack.FixArray(17)...)
	out = append(out, msgpack.Int(0)...)
	out = append(out, msgpack.Bool(false)...)
	out = append(out, msgpack.EmptyArray()...)
	out = append(out, intvec...)
	for i := 4; i <= 12; i++ {
		if i == 7 {
			out = append(out, inventoryVector(a)...)
		} else {
			out = append(out, msgpack.EmptyArray()...)
		}
	}
	out = append(out, msgpack.EmptyMap()...)
	out = append(out, tmap...)
	out = append(out, msgpack.EmptyArray()...)
	out = append(out, strvec...)
	return out
}

type BuyItemOptions struct {
	Ownership bool
	Kitabe    bool
}

func skinInfo(skinID int) []byte {
	var out []byte
	out = append(out, msgpack.FixArray(5)...)
	out = append(out, msgpack.Int(int64(skinID))...)
	out = append(out, msgpack.RawStr(nil)...)
	out = append(out, msgpack.Int(4)...)
	out = append(out, msgpack.EmptyArray()...)
	out = append(out, msgpack.EmptyArray()...)
	return out
}

func heroInfo(a *accounts.Account, heroID int) []byte {
	var out []byte
	out = append(out, msgpack.FixArray(8)...)
	out = append(out, msgpack.Int(int64(heroID))...)
	out = append(out, msgpack.Int(0)...)
	out = append(out, msgpack.Int(0)...)
	out = append(out, msgpack.RawStr(nil)...)
	out = append(out, msgpack.Int(4)...)
	skins := a.OwnedSkinIDs(heroID)
	out = append(out, msgpack.FixArray(len(skins))...)
	for _, skinID := range skins {
		out = append(out, skinInfo(skinID)...)
	}
	out = append(out, msgpack.EmptyArray()...)
	out = append(out, msgpack.EmptyArray()...)
	return out
}

func heroVector(a *accounts.Account, enabled bool) []byte {
	var heroes []int
	if enabled && a != nil {
		heroes = a.OwnedHeroIDs()
	}
	var out []byte
	out = append(out, msgpack.FixArray(len(heroes))...)
	for _, heroID := range heroes {
		out = append(out, heroInfo(a, heroID)...)
	}
	return out
}

// BuildBuyItem is the authoritative TradeMessageBuyItemResponse. Wallet fields
// are absolute, and [11]/[12] always carry the same typed ownership truth.
//
// Client truth (libAndroid 3.6.5a, TradeMessageBuyItemResponse::msgpack_unpack
// @0x012B96E4 + DispatchTradeMsg BuyItem case @0x012813F8, verified live
// 2026-09-13 with Rune=9999/Gems=99999 distinct): the handler SETs
//
//	UserInfo rune   (+0x128) <- [4]   (struct +0x14)
//	UserInfo emblem (+0x130) <- [5]   (struct +0x10)
//	UserInfo gems   (+0x2d4) <- [27]  (struct +0x18; unreachable without
//	                                   encoding [19] GESub5Member18, left out)
//
// [2]/[3]/[6]/[7]/[10] are not read by the wallet path. The old "[2]/[3]/[4]"
// rule (AGENTS2) came from the all-99999 Python oracle and was wrong: it put
// gems at [4], so every BuyItem/BuyItemCRM reply flipped the rune HUD to 99999.
func BuildBuyItem(a *accounts.Account, opts BuyItemOptions) []byte {
	emblem, runeV, gems := 99999, 9999, 99999
	if a != nil {
		emblem, runeV, gems = a.Wallet()
	}
	withKitabe := config.ServerKitabe && opts.Kitabe
	n := 13
	if withKitabe {
		n = 20
	}
	var out []byte
	out = append(out, msgpack.FixArray(n)...)
	out = append(out, msgpack.Int(0)...)
	out = append(out, msgpack.RawStr(nil)...)
	out = append(out, msgpack.Int(int64(emblem))...) // [2] (unused by wallet path)
	out = append(out, msgpack.Int(int64(runeV))...)  // [3] (unused by wallet path)
	out = append(out, msgpack.Int(int64(runeV))...)  // [4] -> UserInfo rune
	out = append(out, msgpack.Int(int64(emblem))...) // [5] -> UserInfo emblem
	out = append(out, msgpack.Int(int64(runeV))...)  // [6]
	out = append(out, msgpack.Int(int64(gems))...)   // [7]
	out = append(out, msgpack.EmptyMap()...)
	out = append(out, msgpack.RawStr(nil)...)
	out = append(out, msgpack.Int(int64(gems))...)
	out = append(out, heroVector(a, opts.Ownership)...) // [11] owned
	out = append(out, heroVector(a, opts.Ownership)...) // [12] valid/status
	if withKitabe {
		// [13] ItemInfo inventory must replay the same authoritative state as
		// GetUserInfo[7]; the client assigns rather than merges this vector.
		out = append(out, inventoryVector(a)...)
		out = append(out, msgpack.EmptyArray()...)
		out = append(out, msgpack.EmptyArray()...)
		out = append(out, msgpack.EmptyArray()...)
		out = append(out, kitabe.GESubMember10(a)...)
		out = append(out, msgpack.Int(0)...)    // [18]
		out = append(out, GESub5Member18(a)...) // [19] flags (assigned unconditionally)
	}
	return out
}

func BuildBuyItemEmpty(a *accounts.Account) []byte {
	return BuildBuyItem(a, BuyItemOptions{
		Ownership: true,
		Kitabe:    config.LoginBuyItemKitabe,
	})
}

func TradeResultAck(tag, result int) []byte {
	var out []byte
	out = append(out, msgpack.FixArray(2)...)
	out = append(out, msgpack.Int(int64(tag))...)
	out = append(out, msgpack.Int(int64(result))...)
	return out
}

// InputAgeAck — trade 0x81 S2C [age, gender] (not [26,0]).
func InputAgeAck(age, gender int) []byte {
	var out []byte
	out = append(out, msgpack.FixArray(2)...)
	out = append(out, msgpack.Int(int64(age))...)
	out = append(out, msgpack.Int(int64(gender))...)
	return out
}

func TradeAchievementAck(name []byte) []byte {
	var out []byte
	out = append(out, msgpack.FixArray(3)...)
	out = append(out, msgpack.Int(26)...)
	out = append(out, msgpack.RawStr(name)...)
	out = append(out, msgpack.EmptyArray()...)
	return out
}

// SelectFlagResponse — trade 0x5c. TradeMessageSelectFlagResponse is
// define_array<int,string,int,int,int> (unpack @0x12c5e40): [0] result (0 =
// ok, else "process failed"), [1] string, [2] pattern → UserInfo+0xBA8,
// [3] pole → +0xBA0, [4] flag type → +0xBB0 (handler @0x1287444).
func SelectFlagResponse(pole, pattern, flagType int) []byte {
	if flagType != 0x11 && flagType != 0x12 {
		flagType = 0x11
	}
	var out []byte
	out = append(out, msgpack.FixArray(5)...)
	out = append(out, msgpack.Int(0)...)
	out = append(out, msgpack.RawStr(nil)...)
	out = append(out, msgpack.Int(int64(pattern))...)
	out = append(out, msgpack.Int(int64(pole))...)
	out = append(out, msgpack.Int(int64(flagType))...)
	return out
}

// NicknameResponse — trade 0x70 (6-elem; display nick in [3..5]).
func NicknameResponse(nick, username string, result int) []byte {
	if nick == "" {
		nick = "Player"
	}
	if username == "" {
		username = nick
	}
	if len(nick) > 31 {
		nick = nick[:31]
	}
	if len(username) > 31 {
		username = username[:31]
	}
	var out []byte
	out = append(out, msgpack.FixArray(6)...)
	out = append(out, msgpack.Int(int64(result))...)
	out = append(out, msgpack.RawStr([]byte(username))...)
	out = append(out, msgpack.Int(0)...)
	nb := []byte(nick)
	out = append(out, msgpack.RawStr(nb)...)
	out = append(out, msgpack.RawStr(nb)...)
	out = append(out, msgpack.RawStr(nb)...)
	return out
}

func TradeReqName(body []byte) []byte {
	if len(body) < 3 {
		return nil
	}
	i := 0
	if body[0] == 0x92 || body[0] == 0x93 || (body[0]&0xf0) == 0x90 {
		i = 1
	}
	if i < len(body) && body[i] < 0x80 {
		i++
	} else if i+1 < len(body) && body[i] == 0xcc {
		i += 2
	} else if i+5 <= len(body) && (body[i] == 0xce || body[i] == 0xd2) {
		i += 5
	}
	if i < len(body) && body[i] >= 0xa0 && body[i] <= 0xbf {
		n := int(body[i] & 0x1f)
		if i+1+n <= len(body) {
			return body[i+1 : i+1+n]
		}
	}
	return nil
}

func PackLE32(v uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return b
}

func HeroAck(tskcid, hero, skin int) []byte {
	out := make([]byte, 16)
	binary.LittleEndian.PutUint32(out[0:4], uint32(tskcid))
	binary.LittleEndian.PutUint32(out[4:8], uint32(hero))
	binary.LittleEndian.PutUint32(out[8:12], uint32(skin))
	return out
}

func ParseC2SHeader(payload []byte) (op, sub uint16, body []byte, ok bool) {
	op, _, sub, body, ok = ParseC2SHeaderWithSlot(payload)
	return
}

// ParseC2SHeaderWithSlot also preserves the low-nibble sender seat required
// when relaying op7 back to the local client and room peers.
func ParseC2SHeaderWithSlot(payload []byte) (op uint16, slot byte, sub uint16, body []byte, ok bool) {
	if len(payload) < 14 {
		return 0, 0, 0, nil, false
	}
	opsub := binary.LittleEndian.Uint16(payload[9:11])
	op = opsub >> 4
	slot = byte(opsub & 0xF)
	sub = binary.LittleEndian.Uint16(payload[11:13])
	body = payload[14:]
	return op, slot, sub, body, true
}

// ParseHeroChange — LobbyChangeHero: hero|skin are the last two LE int32s (Python pin).
func ParseHeroChange(body []byte) (hero, skin int) {
	if len(body) < 8 {
		return 0, 0
	}
	hero = int(binary.LittleEndian.Uint32(body[len(body)-8 : len(body)-4]))
	skin = int(binary.LittleEndian.Uint32(body[len(body)-4:]))
	return
}

// ParseChangeSite1006 parses cid_u32 + utf16len/guid + site_u8. The site is
// 1-based on the wire; callers keep room/session seats 0-based.
func ParseChangeSite1006(body []byte) (wireSite int, guid string, ok bool) {
	if len(body) < 7 {
		return 0, "", false
	}
	off := 4 // cid
	guidLen := int(binary.LittleEndian.Uint16(body[off : off+2]))
	off += 2
	if guidLen > 64 || off+guidLen >= len(body) {
		return 0, "", false
	}
	guid = string(body[off : off+guidLen])
	off += guidLen
	wireSite = int(body[off])
	if wireSite < 1 || wireSite > 10 {
		return 0, guid, false
	}
	return wireSite, guid, true
}

// SiteAck1007 is cid_u32 + new_site_u8 + hero_i32. The old 1-based site is
// carried separately in the HSGS header low nibble by BuildSynReply.
func SiteAck1007(tskcid, wireSite, hero int) []byte {
	out := make([]byte, 9)
	binary.LittleEndian.PutUint32(out[0:4], uint32(tskcid))
	out[4] = byte(wireSite)
	binary.LittleEndian.PutUint32(out[5:9], uint32(hero))
	return out
}

// ValidSeatHero — UI/creature pick range (reject ASCII garbage from misaligned parse).
func ValidSeatHero(hero int) bool {
	return hero > 0 && hero < 0x100000
}
