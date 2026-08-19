package gs

import (
	"encoding/binary"
)

type SeatRosterMember struct {
	Seat0   int
	Hero    int
	Skin    int
	Spell1  int
	Spell2  int
	Nick    string
	GUID    string
	IsOwner bool
	Ready   bool
}

// SeatRoster1002 builds the room-wide MatchSetting roster. The final seat byte
// is local to the recipient; every PlayerInfo carries its own 1-based seat.
func SeatRoster1002(tskcid, localSeat0 int, members []SeatRosterMember) []byte {
	if len(members) > 10 {
		members = members[:10]
	}
	if localSeat0 < 0 {
		localSeat0 = 0
	}
	if localSeat0 > 9 {
		localSeat0 = 9
	}

	body := make([]byte, 0, 4+1+len(members)*PISize+1)
	tmp := make([]byte, 4)
	binary.LittleEndian.PutUint32(tmp, uint32(tskcid))
	body = append(body, tmp...)
	body = append(body, byte(len(members)))
	for _, member := range members {
		body = append(body, EncodePI(
			member.Hero, member.Skin, member.Spell1, member.Spell2,
			member.Seat0, true, member.Ready && member.Hero > 0,
			member.IsOwner, member.Nick, member.GUID,
			member.Nick != "" || member.GUID != "",
		)...)
	}
	body = append(body, byte(localSeat0+1))
	return body
}

// SeatPlayerInfo1002 — MatchSetting seat paint (nick UTF + GUID + spells).
// Wire: tskcid + count(1) + PlayerInfo + seatByte(1-based).
func SeatPlayerInfo1002(tskcid, seat0, hero, skin, spell1, spell2 int, nick, guid string, isOwner, ready bool) []byte {
	return SeatRoster1002(tskcid, seat0, []SeatRosterMember{{
		Seat0: seat0, Hero: hero, Skin: skin, Spell1: spell1, Spell2: spell2,
		Nick: nick, GUID: guid, IsOwner: isOwner, Ready: ready,
	}})
}

// PlayerLeaveRoom3003 is the real peer-vacate notification. Seat is 1-based
// on wire; unlike 0x1002, this path actually resets the vacated player slot.
func PlayerLeaveRoom3003(tskcid, seat0 int, nick string) []byte {
	if seat0 < 0 {
		seat0 = 0
	}
	if seat0 > 9 {
		seat0 = 9
	}
	body := CIDOnly(tskcid)
	body = append(body, byte(seat0+1))
	body = append(body, UTF(nick)...)
	return body
}

// ParseSummonerSpells — READY+14 spell pair from SkillAck body (echo-only, no patch).
func ParseSummonerSpells(body []byte) (s1, s2 int, ok bool) {
	if len(body) < 4+4+20 {
		return 0, 0, false
	}
	readyOff := skillReadyOffset(body)
	if readyOff >= 0 && readyOff+14+8 <= len(body) {
		a := int(binary.LittleEndian.Uint32(body[readyOff+14 : readyOff+18]))
		b := int(binary.LittleEndian.Uint32(body[readyOff+18 : readyOff+22]))
		if spellPairOK(a, b) {
			return a, b, true
		}
	}
	start := readyOff + 14
	if readyOff < 0 {
		o, _, _ := skillBodyAfterUTFs(body)
		if o < 0 {
			return 0, 0, false
		}
		start = o + 14
	}
	end := len(body) - 7
	if end > start+64 {
		end = start + 64
	}
	for off := start; off < end; off++ {
		a := int(binary.LittleEndian.Uint32(body[off : off+4]))
		b := int(binary.LittleEndian.Uint32(body[off+4 : off+8]))
		if spellPairOK(a, b) {
			return a, b, true
		}
	}
	return 0, 0, false
}

func ParseReadyFromSkill(body []byte) (ready int, ok bool) {
	o := skillReadyOffset(body)
	if o < 0 {
		o, _, _ = skillBodyAfterUTFs(body)
	}
	if o < 0 || o+4 > len(body) {
		return 0, false
	}
	v := int(binary.LittleEndian.Uint32(body[o : o+4]))
	if v == 0 || v == 1 {
		return v, true
	}
	return 0, false
}

func spellPairOK(a, b int) bool {
	return a >= 100 && a <= 0x10000 && b >= 100 && b <= 0x10000
}

func skillBodyAfterUTFs(body []byte) (off int, guid, nick string) {
	if len(body) < 8 {
		return -1, "", ""
	}
	o := 4
	var utfs []string
	for i := 0; i < 2; i++ {
		if o+2 > len(body) {
			return -1, "", ""
		}
		ln := int(binary.LittleEndian.Uint16(body[o : o+2]))
		if ln > 256 || o+2+ln > len(body) {
			return -1, "", ""
		}
		o += 2
		utfs = append(utfs, string(body[o:o+ln]))
		o += ln
	}
	g, n := "", ""
	if len(utfs) > 0 {
		g = utfs[0]
	}
	if len(utfs) > 1 {
		n = utfs[1]
	}
	return o, g, n
}

func skillReadyOffset(body []byte) int {
	if len(body) < 12 {
		return -1
	}
	o := 4
	for i := 0; i < 5; i++ {
		if o+4 > len(body) {
			return -1
		}
		ln := int(binary.LittleEndian.Uint16(body[o : o+2]))
		if ln > 0 && ln <= 64 && o+2+ln <= len(body) {
			payload := body[o+2 : o+2+ln]
			if ln >= 2 || (ln == 1 && len(payload) > 0 && isAlnum(payload[0])) {
				o += 2 + ln
				continue
			}
		}
		peek := int(int32(binary.LittleEndian.Uint32(body[o : o+4])))
		if peek == 0 || peek == 1 {
			return o
		}
		if ln == 0 && o+2 <= len(body) {
			o += 2
			continue
		}
		return -1
	}
	return -1
}

func isAlnum(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}
