package gs

import (
	"encoding/binary"
	"fmt"
	"net"

	"hoc-server/internal/config"
	"hoc-server/internal/netx"
	"hoc-server/internal/session"
	wiregs "hoc-server/internal/wire/gs"
)

// Chat routing hints the client stamps into every UnitAide / lobby-chat body
// (UNIT_AIDE_SUBOPCODE_AIM). 0x800 = own team, 0x4000 = everybody.
const (
	chatAimTeam = 0x800
	chatAimAll  = 0x4000
)

// handleLobbyChat relays SynGameState 0x1003 (custom-room lobby chat,
// DlgMatchSettingNew::callbackInputBox) to the sender's team half — the
// client stamps aim 0x800 on it — or to the whole room for aim 0x4000.
//
// Body (identical C2S and S2C — NetPacketSynGameState's ctor already writes
// the cid and the sender GUID before the caller's fields):
//
//	cid_u32 + UTF sender_guid + slot_u8 + aim_i32 + type_i32(7) + UTF text
//
// HandleSynGameState@0x127edd4 parses exactly that, so the body is relayed
// verbatim. Inserting anything shifts type off 7 and the client drops it.
//
// The sender is included: the client does not echo its own line locally, so
// nobody — the author included — sees a message the server drops.
func handleLobbyChat(conn net.Conn, st *connState, slot byte, body []byte) {
	if st == nil || st.sess == nil || st.sess.Room == nil {
		fmt.Printf(" [CHAT] 0x1003 ignored: no room body=%dB\n", len(body))
		return
	}
	if len(body) < 4+2+1+4+4+2 {
		fmt.Printf(" [CHAT] 0x1003 ignored: short body=%dB\n", len(body))
		return
	}
	guidLen := int(binary.LittleEndian.Uint16(body[4:6]))
	aimOff := 4 + 2 + guidLen + 1
	if guidLen > 64 || aimOff+8 > len(body) {
		fmt.Printf(" [CHAT] 0x1003 ignored: bad guid len=%d body=%dB\n", guidLen, len(body))
		return
	}
	aim := binary.LittleEndian.Uint32(body[aimOff : aimOff+4])
	teamOnly := aim == chatAimTeam
	room := st.sess.Room
	out := body

	site := st.sess.Seat + 1
	sent := 0
	for _, m := range room.SnapshotMembers() {
		if m == nil {
			continue
		}
		if teamOnly && !session.SameTeamSeat(st.sess.Seat, m.Seat) {
			continue
		}
		for _, gc := range m.SnapshotGSConns() {
			st2 := getState(gc)
			if st2 == nil || !st2.canPeerRoomWrite() {
				continue
			}
			if _, err := sendRoomSyn(gc, st2, 0x1003, out, site); err == nil {
				sent++
			}
		}
	}
	scope := "all"
	if teamOnly {
		scope = "team"
	}
	fmt.Printf(" [CHAT] lobby 0x1003 from=%q seat=%d aim=%#x scope=%s body=%dB recipients=%d\n",
		st.sess.Username, slot, aim, scope, len(body), sent)
}

// handleUnitAide relays op8 (NetPacketUnitAideAction) inside a match:
// sub=1 lobby-style chat, sub=2 in-game chat (DlgChatControl::SendMsg),
// sub=3 minimap ping (DlgMiniMap::_SetPlayerMark). The body is opaque to us
// (the receiver only needs the sender slot in the header nibble, which
// NGDataPtl::HandleUnitAideAction resolves via GetPlayerFromInRoomID) except
// for two ints:
//
//   - int[0] is the execution frame. The client drains op8 through the same
//     frame-synced queue as op7 (NGDataPtl::UpdateAISyn@0x129fa2c): a packet
//     whose frame is already behind the local counter triggers
//     Game::SetLogout — the "match aborts when someone chats" symptom. So it
//     is stamped with the room clock exactly like op7, minus the syn (op8
//     has none and must not consume the shared counter).
//   - int[1] is the aim: 0x800 own team, 0x4000 everybody.
//
// The client only accepts op8 while GameSubState==4 (OnRead@0x127da78), so
// peers that are not playing are skipped rather than fed a packet they drop.
func handleUnitAide(conn net.Conn, st *connState, slot byte, sub uint16, body []byte) {
	if st == nil || st.sess == nil || st.sess.Room == nil || !st.playing {
		fmt.Printf(" [CHAT] op8 sub=%d ignored before match body=%dB\n", sub, len(body))
		return
	}
	if len(body) < 8 {
		fmt.Printf(" [CHAT] op8 sub=%d ignored: short body=%dB\n", sub, len(body))
		return
	}
	aim := binary.LittleEndian.Uint32(body[4:8])
	teamOnly := aim == chatAimTeam && sub != 1
	room := st.sess.Room
	src := st.sess

	// Same hand-over-hand as handleUnitAction: stamp under the room lock, then
	// hold the wire lock while writing so no op11 frame overtakes the stamped
	// frame on any peer's socket.
	room.Lock()
	if !room.MatchClockArmed {
		room.Unlock()
		fmt.Printf(" [CHAT] op8 sub=%d ignored: room clock not armed body=%dB\n", sub, len(body))
		return
	}
	frameAt := room.MatchSFrame + config.FrameLead
	if frameAt <= room.MatchSFrame {
		frameAt = room.MatchSFrame + 1
	}
	stamped := append([]byte(nil), body...)
	oldFrame := binary.LittleEndian.Uint32(stamped[0:4])
	binary.LittleEndian.PutUint32(stamped[0:4], uint32(frameAt))
	members := append([]*session.Session(nil), room.Members...)
	room.WireLock()
	room.Unlock()
	defer room.WireUnlock()

	sent := 0
	for _, m := range members {
		if m == nil || !m.IsMatchPlaying() || m.IsMatchClockFrozen() {
			continue
		}
		if teamOnly && !session.SameTeamSeat(src.Seat, m.Seat) {
			continue
		}
		for _, gc := range m.SnapshotGSConns() {
			st2 := getState(gc)
			if st2 == nil {
				continue
			}
			st2.mu.Lock()
			seqN := st2.seq
			st2.seq++
			netx.ApplyMatchWriteDeadline(gc)
			_, err := gc.Write(wiregs.BuildInMatch(seqN, 8, sub, stamped, slot))
			netx.ClearMatchWriteDeadline(gc)
			st2.mu.Unlock()
			if err == nil {
				sent++
			}
		}
	}
	scope := "all"
	if teamOnly {
		scope = "team"
	}
	fmt.Printf(" [CHAT] op8 sub=%d from=%q slot=%d aim=%#x scope=%s frame=%d(was %d) body=%dB recipients=%d\n",
		sub, src.Username, slot, aim, scope, frameAt, oldFrame, len(body), sent)
}
