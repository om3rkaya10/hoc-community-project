package gs

import (
	"encoding/binary"

	"hoc-server/internal/config"
)

const (
	PINumInts = 146
	PISize    = PINumInts*4 + 4*2 // 592
)

func BuildReply(seq int32, opcode, sub uint16, payload []byte, magic byte) []byte {
	if magic == 0 {
		magic = 0x24
	}
	L := uint32(len(payload) + 5)
	op := opcode & 0xF
	opsub := op | (op << 4)
	out := make([]byte, 14+len(payload))
	out[0] = magic
	binary.LittleEndian.PutUint32(out[1:5], uint32(seq))
	binary.LittleEndian.PutUint32(out[5:9], L)
	binary.LittleEndian.PutUint16(out[9:11], opsub)
	binary.LittleEndian.PutUint16(out[11:13], sub)
	out[13] = 0
	copy(out[14:], payload)
	return out
}

// BuildSynReply — HSGS S2C with opsub=(9<<4)|sender_site_1based (required for 0x1002/0x1007).
func BuildSynReply(seq int32, sub uint16, payload []byte, senderSite1Based int, magic byte) []byte {
	if magic == 0 {
		magic = 0x24
	}
	L := uint32(len(payload) + 5)
	opsub := uint16((9 << 4) | (senderSite1Based & 0xF))
	out := make([]byte, 14+len(payload))
	out[0] = magic
	binary.LittleEndian.PutUint32(out[1:5], uint32(seq))
	binary.LittleEndian.PutUint32(out[5:9], L)
	binary.LittleEndian.PutUint16(out[9:11], opsub)
	binary.LittleEndian.PutUint16(out[11:13], sub)
	out[13] = 0
	copy(out[14:], payload)
	return out
}

func BuildInMatch(seq int32, opcode, sub uint16, payload []byte, slot byte) []byte {
	L := uint32(len(payload) + 5)
	opsub := ((opcode & 0xF) << 4) | uint16(slot&0xF)
	out := make([]byte, 14+len(payload))
	out[0] = 0x40
	binary.LittleEndian.PutUint32(out[1:5], uint32(seq))
	binary.LittleEndian.PutUint32(out[5:9], L)
	binary.LittleEndian.PutUint16(out[9:11], opsub)
	binary.LittleEndian.PutUint16(out[11:13], sub)
	out[13] = 0
	copy(out[14:], payload)
	return out
}

func GameplayFrame(frame, syn, frmInc int32) []byte {
	out := make([]byte, 4*4+2*2+4)
	binary.LittleEndian.PutUint32(out[0:4], uint32(frame))
	binary.LittleEndian.PutUint32(out[4:8], uint32(frmInc))
	binary.LittleEndian.PutUint32(out[8:12], uint32(syn))
	binary.LittleEndian.PutUint32(out[12:16], 0)
	// two shorts + int already zero
	return out
}

// StampUnitAction rewrites the two server-owned lockstep fields in a C2S op7
// body. The client deliberately sends sqid (int[1]) as zero; echoing it raw
// causes an immediate sequenceID wrong UP disconnect. The original input is
// left untouched so callers can safely retain it for diagnostics.
func StampUnitAction(body []byte, frame, syn int32) (out []byte, oldFrame, oldSyn int32, ok bool) {
	if len(body) < 8 {
		return nil, 0, 0, false
	}
	out = append([]byte(nil), body...)
	oldFrame = int32(binary.LittleEndian.Uint32(out[0:4]))
	oldSyn = int32(binary.LittleEndian.Uint32(out[4:8]))
	binary.LittleEndian.PutUint32(out[0:4], uint32(frame))
	binary.LittleEndian.PutUint32(out[4:8], uint32(syn))
	return out, oldFrame, oldSyn, true
}

func LoginAck(roomID, tskcid int) []byte {
	roomUTF := []byte("hoc_r" + itoa(roomID))
	out := make([]byte, 0, 64)
	buf4 := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf4, uint32(tskcid))
	out = append(out, buf4...)
	out = append(out, 0x01)
	buf2 := make([]byte, 2)
	binary.LittleEndian.PutUint16(buf2, 2)
	out = append(out, buf2...)
	binary.LittleEndian.PutUint32(buf4, 0x00000101)
	out = append(out, buf4...)
	binary.LittleEndian.PutUint32(buf4, 1)
	out = append(out, buf4...)
	binary.LittleEndian.PutUint16(buf2, uint16(len(roomUTF)))
	out = append(out, buf2...)
	out = append(out, roomUTF...)
	binary.LittleEndian.PutUint16(buf2, 0)
	out = append(out, buf2...)
	binary.LittleEndian.PutUint32(buf4, 0)
	out = append(out, buf4...)
	out = append(out, make([]byte, 16)...)
	return out
}

func CIDOnly(tskcid int) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(tskcid))
	return b
}

func StartPlay(tskcid int) []byte { return CIDOnly(tskcid) }

func UTF(s string) []byte {
	b := []byte(s)
	if len(b) > 64 {
		b = b[:64]
	}
	out := make([]byte, 2+len(b))
	binary.LittleEndian.PutUint16(out[0:2], uint16(len(b)))
	copy(out[2:], b)
	return out
}

type ReLoginRequest struct {
	UseUDP       byte
	RoomName     string
	GUID         string
	RequestedSyn int32
	GSFrame      int32
	ExeFrame     int32
	Mode         byte
}

// ParseReLoginReq decodes C2S command-4 ReLoginReq.
//
// LIVE 44B (2026-08-09 enterpries2 hex):
//
//	i32 header (tskcid) + UTF guid + u8 use_udp + UTF room + UTF guid + i32 req + u8 mode
//
// Example: tskcid=2, guid=gllive:enterpries2, udp=0, room=hoc_r1, guid again, req=338, mode=0.
func ParseReLoginReq(body []byte) (ReLoginRequest, bool) {
	if req, ok := parseReLoginReqLive(body); ok {
		return req, true
	}
	room, guid, idEnd, useUDP, ok := findReLoginIdentity(body)
	if !ok {
		return ReLoginRequest{}, false
	}
	req := ReLoginRequest{UseUDP: useUDP, RoomName: room, GUID: guid}
	bookmarkAt := skipReLoginTokenUTFs(body, idEnd)
	if off, n := findReLoginBookmarks(body, bookmarkAt); off >= 0 {
		req.RequestedSyn = int32(binary.LittleEndian.Uint32(body[off : off+4]))
		if n >= 3 {
			req.GSFrame = int32(binary.LittleEndian.Uint32(body[off+4 : off+8]))
			req.ExeFrame = int32(binary.LittleEndian.Uint32(body[off+8 : off+12]))
			if off+12 < len(body) {
				req.Mode = body[off+12]
			}
		} else if off+4 < len(body) {
			req.Mode = body[off+4]
		}
	}
	return req, true
}

func parseReLoginReqLive(body []byte) (ReLoginRequest, bool) {
	if len(body) < 4+2+1+2+2+4 {
		return ReLoginRequest{}, false
	}
	off := 4 // i32 header (tskcid)
	guid, off, ok := parseUTFAt(body, off, 256)
	if !ok || !isReLoginGUID(guid) {
		return ReLoginRequest{}, false
	}
	if off >= len(body) {
		return ReLoginRequest{}, false
	}
	useUDP := body[off]
	off++
	if useUDP > 1 {
		return ReLoginRequest{}, false
	}
	room, off, ok := parseUTFAt(body, off, 64)
	if !ok || !isReLoginRoom(room) {
		return ReLoginRequest{}, false
	}
	guid2, off, ok := parseUTFAt(body, off, 256)
	if !ok || !isReLoginGUID(guid2) {
		return ReLoginRequest{}, false
	}
	if off+4 > len(body) {
		return ReLoginRequest{}, false
	}
	req := ReLoginRequest{
		UseUDP:       useUDP,
		RoomName:     room,
		GUID:         guid,
		RequestedSyn: int32(binary.LittleEndian.Uint32(body[off : off+4])),
	}
	off += 4
	if off < len(body) {
		req.Mode = body[off]
	}
	return req, true
}

func findReLoginIdentity(body []byte) (room, guid string, idEnd int, useUDP byte, ok bool) {
	starts := []int{0}
	if len(body) > 0 && body[0] <= 1 {
		// Prefer explicit use_udp prefix when present.
		starts = []int{1, 0}
	}
	for _, start := range starts {
		type field struct {
			s   string
			end int
		}
		var fields []field
		for off := start; off+2 <= len(body); {
			s, next, ok := parseUTFAt(body, off, 256)
			if !ok {
				off++
				continue
			}
			fields = append(fields, field{s: s, end: next})
			off = next
		}
		var room, guid string
		var idEnd int
		for _, f := range fields {
			if room == "" && isReLoginRoom(f.s) {
				room = f.s
				if f.end > idEnd {
					idEnd = f.end
				}
			}
			if guid == "" && isReLoginGUID(f.s) {
				guid = f.s
				if f.end > idEnd {
					idEnd = f.end
				}
			}
		}
		if room != "" && guid != "" && idEnd > 0 {
			udp := byte(0)
			if start == 1 {
				udp = body[0]
			}
			return room, guid, idEnd, udp, true
		}
	}
	return "", "", 0, 0, false
}

func skipReLoginTokenUTFs(body []byte, off int) int {
	for {
		s, next, ok := parseUTFAt(body, off, 256)
		if !ok || next+4 > len(body) {
			return off
		}
		if isReLoginRoom(s) || isReLoginGUID(s) || !isPrintableASCII(s) {
			return off
		}
		// Only skip when a plausible bookmark region remains after the token.
		if _, n := findReLoginBookmarks(body, next); n == 0 {
			return off
		}
		off = next
	}
}

func isPrintableASCII(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

func findReLoginBookmarks(body []byte, afterID int) (off int, n int) {
	if afterID < 0 {
		afterID = 0
	}
	for o := afterID; o+12 <= len(body); o++ {
		a := int32(binary.LittleEndian.Uint32(body[o : o+4]))
		b := int32(binary.LittleEndian.Uint32(body[o+4 : o+8]))
		c := int32(binary.LittleEndian.Uint32(body[o+8 : o+12]))
		if !plausibleReLoginBookmark(a) || !plausibleReLoginBookmark(b) || !plausibleReLoginBookmark(c) {
			continue
		}
		if abs32(a-b) > 100000 || abs32(b-c) > 100000 {
			continue
		}
		// First non-trivial triple after identity (skip trailing 0,0,0 pad).
		if a+b+c == 0 {
			continue
		}
		return o, 3
	}
	for o := afterID; o+4 <= len(body); o++ {
		a := int32(binary.LittleEndian.Uint32(body[o : o+4]))
		if plausibleReLoginBookmark(a) && a != 0 {
			return o, 1
		}
	}
	return -1, 0
}

func plausibleReLoginBookmark(v int32) bool {
	return v >= 0 && v < 5_000_000
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func isReLoginRoom(s string) bool {
	return len(s) >= 5 && s[:5] == "hoc_r"
}

func isReLoginGUID(s string) bool {
	return len(s) >= 7 && (s[:7] == "gllive:" || s[:7] == "GLLive:")
}

func parseUTFAt(body []byte, off, max int) (string, int, bool) {
	if off < 0 || off+2 > len(body) {
		return "", off, false
	}
	n := int(binary.LittleEndian.Uint16(body[off : off+2]))
	off += 2
	if n > max || off+n > len(body) {
		return "", off, false
	}
	return string(body[off : off+n]), off + n, true
}

// ReLoginAck is the active-match command-5 success body.
// Syn S2C always leads with i32le tskcid (HandleSynGameState ReadInt) before
// the cmd-5 trailer: u8 seat1 + UTF hoc_rN + u8 success=1.
func ReLoginAck(roomID, seat0, tskcid int) []byte {
	if seat0 < 0 {
		seat0 = 0
	}
	if seat0 > 9 {
		seat0 = 9
	}
	if tskcid <= 0 {
		tskcid = 2
	}
	out := CIDOnly(tskcid)
	out = append(out, byte(seat0+1))
	out = append(out, UTF("hoc_r"+itoa(roomID))...)
	out = append(out, 1)
	return out
}

// ReLoginAckFail is command-5 with success=0 + i32le error after the same
// tskcid + seat + room prolog. Note: on mid-match soft branch (Game+0x160≠1)
// the client may still invoke OnSoftReconnectSucceed — use sparingly.
func ReLoginAckFail(roomID, seat0, tskcid int, errCode int32) []byte {
	if seat0 < 0 {
		seat0 = 0
	}
	if seat0 > 9 {
		seat0 = 9
	}
	if tskcid <= 0 {
		tskcid = 2
	}
	out := CIDOnly(tskcid)
	out = append(out, byte(seat0+1))
	out = append(out, UTF("hoc_r"+itoa(roomID))...)
	out = append(out, 0)
	tmp := make([]byte, 4)
	binary.LittleEndian.PutUint32(tmp, uint32(errCode))
	out = append(out, tmp...)
	return out
}

func EncodePI(heroID, skin, spell1, spell2, seat0Based int, occupied, inRoom, isOwner bool, nick, guid string, allowUTF bool) []byte {
	ints := make([]int32, PINumInts)
	if occupied {
		site0 := seat0Based
		if site0 < 0 {
			site0 = 0
		}
		if site0 > 9 {
			site0 = 9
		}
		ints[0] = int32(site0 + 1)
		ints[1] = int32(heroID)
		if heroID != 0 {
			ints[4] = int32(skin)
		}
		if inRoom {
			ints[2] = 1
		}
		if isOwner {
			ints[3] = 1
		}
		if heroID != 0 {
			ints[10] = int32(spell1)
			ints[11] = int32(spell2)
		}
	}
	out := make([]byte, 0, PISize)
	tmp := make([]byte, 4)
	for i := 0; i < 10; i++ {
		binary.LittleEndian.PutUint32(tmp, uint32(ints[i]))
		out = append(out, tmp...)
	}
	if allowUTF && (nick != "" || guid != "") {
		out = append(out, UTF(nick)...)
		out = append(out, UTF(guid)...)
		out = append(out, UTF("")...)
		out = append(out, UTF("")...)
	} else {
		out = append(out, 0, 0, 0, 0, 0, 0, 0, 0)
	}
	for i := 10; i < PINumInts; i++ {
		binary.LittleEndian.PutUint32(tmp, uint32(ints[i]))
		out = append(out, tmp...)
	}
	return out
}

// LoadMapSolo builds a single-seat LoadMap. It keeps every custom-room
// option at its default (-1 / false), so map defaults are preserved.
func LoadMapSolo(tskcid, hero, skin, spell1, spell2, gsiMode, gsiParam int, nick, guid string) []byte {
	return LoadMapShared(tskcid, 1, 0, gsiMode, gsiParam, config.DefaultCustomRoomOptions(), []LoadMapMember{{
		Seat0: 0, Hero: hero, Skin: skin, Spell1: spell1, Spell2: spell2,
		Nick: nick, GUID: guid, IsOwner: true,
	}})
}

// LoadMapMember describes one human seat for shared LoadMap.
type LoadMapMember struct {
	Seat0          int
	Hero, Skin     int
	Spell1, Spell2 int
	Nick, GUID     string
	IsOwner        bool
}

// LoadMapShared — same seed+roster for every dest; localSeat is 1-based for recipient.
//
// opts carries the custom-room "advanced options". They occupy the trailing
// block that is only present when gsiParam == 4 (GAME_MODE_PARAM_CUSTOMIZE);
// pass config.DefaultCustomRoomOptions() to keep every map default.
func LoadMapShared(tskcid, localSeat, seed, gsiMode, gsiParam int, opts config.CustomRoomOptions, members []LoadMapMember) []byte {
	if localSeat < 1 {
		localSeat = 1
	}
	if localSeat > 10 {
		localSeat = 10
	}
	bySeat := map[int]LoadMapMember{}
	for _, m := range members {
		s := m.Seat0
		if s < 0 {
			s = 0
		}
		if s > 9 {
			s = 9
		}
		bySeat[s] = m
	}
	body := []byte{0x01}
	tmp := make([]byte, 4)
	binary.LittleEndian.PutUint32(tmp, uint32(localSeat))
	body = append(body, tmp...)
	binary.LittleEndian.PutUint32(tmp, uint32(seed))
	body = append(body, tmp...)
	binary.LittleEndian.PutUint32(tmp, uint32(gsiMode))
	body = append(body, tmp...)
	binary.LittleEndian.PutUint32(tmp, uint32(gsiParam))
	body = append(body, tmp...)
	binary.LittleEndian.PutUint32(tmp, 0)
	body = append(body, tmp...)
	body = append(body, 0x00)
	if gsiParam == 4 {
		// Custom-room "advanced options" block (21 bytes). Field order is
		// the client's own LoadMap debug template:
		//   m_initial_gold, m_get_gold, m_initial_level, m_get_exp   (int32)
		//   m_talent, m_inscription, m_lasthithint, m_revive,
		//   m_is_kill_streak_reward_reduce                            (bool)
		// Lua reads these back via GetCustomInfo(); -1 means "keep the map
		// default", which is why this must not be left zeroed.
		for _, v := range []int32{
			opts.InitialGold, opts.GetGold, opts.InitialLevel, opts.GetExp,
		} {
			binary.LittleEndian.PutUint32(tmp, uint32(v))
			body = append(body, tmp...)
		}
		for _, b := range []bool{
			opts.Talent, opts.Inscription, opts.LastHitHint, opts.Revive,
			opts.KillStreakReduce,
		} {
			if b {
				body = append(body, 0x01)
			} else {
				body = append(body, 0x00)
			}
		}
	}
	empty := EncodePI(0, 0, 0, 0, 0, false, false, false, "", "", false)
	for seat := 0; seat < 10; seat++ {
		m, ok := bySeat[seat]
		pi := empty
		if ok && m.Hero > 0 {
			pi = EncodePI(m.Hero, m.Skin, m.Spell1, m.Spell2, seat, true, true, m.IsOwner, m.Nick, m.GUID, true)
		}
		if seat == 0 {
			body = append(body, pi...)
		} else {
			body = append(body, 0x00) // human occupy=0
			body = append(body, pi...)
		}
	}
	body = append(body, 0x00)
	head := make([]byte, 4)
	binary.LittleEndian.PutUint32(head, uint32(tskcid))
	return append(head, body...)
}

func Op4Pong(int1, int2, roomID uint32) []byte {
	out := make([]byte, 12)
	binary.LittleEndian.PutUint32(out[0:4], int1)
	binary.LittleEndian.PutUint32(out[4:8], int2)
	binary.LittleEndian.PutUint32(out[8:12], roomID)
	return out
}

// Decrypt inbound C2S: key = wire[0]^0x48, magic plaintext 0x48, header 11B LE.
func TryDecryptPacket(buf []byte) (key byte, seq uint16, payload []byte, pktLen int, ok bool) {
	if len(buf) < 11 {
		return 0, 0, nil, 0, false
	}
	key = buf[0] ^ 0x48
	hdr := make([]byte, 11)
	for i := 0; i < 11; i++ {
		hdr[i] = buf[i] ^ key
	}
	if hdr[0] != 0x48 {
		return key, 0, nil, 0, false
	}
	length := int(binary.LittleEndian.Uint16(hdr[5:7]))
	pktLen = 11 + length
	if length < 0 || pktLen > 100000 {
		return key, 0, nil, 0, false
	}
	if len(buf) < pktLen {
		return key, 0, nil, pktLen, false // need more data
	}
	dec := make([]byte, pktLen)
	for i := 0; i < pktLen; i++ {
		dec[i] = buf[i] ^ key
	}
	seq = binary.LittleEndian.Uint16(dec[1:3])
	payload = dec[11:]
	return key, seq, payload, pktLen, true
}

// EncryptC2S builds a XOR-wrapped client GS frame (magic 0x48 plaintext).
func EncryptC2S(seq uint16, payload []byte, key byte) []byte {
	plain := make([]byte, 11+len(payload))
	plain[0] = 0x48
	binary.LittleEndian.PutUint16(plain[1:3], seq)
	binary.LittleEndian.PutUint16(plain[5:7], uint16(len(payload)))
	copy(plain[11:], payload)
	for i := range plain {
		plain[i] ^= key
	}
	return plain
}

func ParseLoginIdentity(payload []byte) (user, token string) {
	// Best-effort ASCII scan for username / mock_s#### token.
	asc := make([]byte, 0, len(payload))
	for _, c := range payload {
		if c >= 32 && c < 127 {
			asc = append(asc, c)
		} else {
			asc = append(asc, '.')
		}
	}
	s := string(asc)
	// token mock_sNNNN
	for i := 0; i+9 <= len(s); i++ {
		if s[i:i+6] == "mock_s" {
			j := i + 6
			for j < len(s) && j < i+12 && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			token = s[i:j]
			break
		}
	}
	for _, prefix := range []string{"gllive:", "glive:", "hoc:"} {
		if candidate := asciiAfterPrefix(payload, prefix); candidate != "" {
			user = candidate
			break
		}
	}
	for _, u := range []string{"enterpries1", "enterpries2", "phone"} {
		if user != "" {
			break
		}
		if containsASCII(payload, u) {
			user = u
			break
		}
	}
	return
}

func asciiAfterPrefix(payload []byte, prefix string) string {
	if len(prefix) == 0 {
		return ""
	}
	for i := 0; i+len(prefix) < len(payload); i++ {
		matched := true
		for j := range prefix {
			a, b := payload[i+j], prefix[j]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if a != b {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		start := i + len(prefix)
		end := start
		for end < len(payload) && isLoginUserByte(payload[end]) {
			end++
		}
		if end > start {
			return string(payload[start:end])
		}
	}
	return ""
}

func isLoginUserByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.' || c == '@'
}

func containsASCII(b []byte, sub string) bool {
	sb := []byte(sub)
	for i := 0; i+len(sb) <= len(b); i++ {
		ok := true
		for j := 0; j < len(sb); j++ {
			if b[i+j] != sb[j] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var dig [20]byte
	i := len(dig)
	for n > 0 {
		i--
		dig[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		dig[i] = '-'
	}
	return string(dig[i:])
}

// C2S syn header helpers: after decrypt, client header is 11B; sub often in payload[0:5].
func ClientOpcodeGuess(payload []byte) (op, sub uint16) {
	if len(payload) < 5 {
		return 0, 0
	}
	// Many paths: first byte family hints; sub at LE u16 offset varies.
	// Empirical: trade uses field; HSGS uses payload leading.
	return 0, binary.LittleEndian.Uint16(payload[1:3])
}
