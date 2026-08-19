package gs

import (
	"encoding/binary"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"hoc-server/internal/accounts"
	"hoc-server/internal/config"
	"hoc-server/internal/gs/trade"
	"hoc-server/internal/match"
	"hoc-server/internal/netx"
	"hoc-server/internal/session"
	wiregs "hoc-server/internal/wire/gs"
)

type connState struct {
	mu            sync.Mutex
	seq           int32
	conn          net.Conn
	sess          *session.Session
	custom        bool
	peerReady     bool
	roomID        int
	tskcid        int
	loading       bool
	playing       bool
	lastFrame     time.Time
	kitabeSeeded  bool
	skillBodies   [][]byte // cache for post-AllAck SkillAck replay
	resumeSyn     int
	resumePending bool
}

var (
	statesMu sync.Mutex
	states   = map[net.Conn]*connState{}
	clock    *match.Clock
)

func Init() {
	clock = match.NewClock(func(conn net.Conn, opcode, sub uint16, body []byte, inmatch bool) {
		st := getState(conn)
		if st == nil {
			return
		}
		st.mu.Lock()
		seq := st.seq
		st.seq++
		var pkt []byte
		if inmatch {
			pkt = wiregs.BuildInMatch(seq, opcode, sub, body, 0)
		} else {
			pkt = wiregs.BuildReply(seq, opcode, sub, body, 0x24)
		}
		// Bound the write: this runs under the room's wire lock, so a peer
		// that stopped reading would otherwise stall the whole room's frame
		// clock (AGENTS5 §2.6).
		netx.ApplyMatchWriteDeadline(conn)
		_, _ = conn.Write(pkt)
		netx.ClearMatchWriteDeadline(conn)
		st.mu.Unlock()
	})
}

func getState(c net.Conn) *connState {
	statesMu.Lock()
	defer statesMu.Unlock()
	return states[c]
}

func (st *connState) canPeerSeatSync() bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.custom && st.peerReady && !st.loading
}

func (st *connState) canPeerRoomWrite() bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.custom && st.peerReady
}

func setState(c net.Conn, st *connState) {
	statesMu.Lock()
	states[c] = st
	statesMu.Unlock()
}

func delState(c net.Conn) {
	statesMu.Lock()
	delete(states, c)
	statesMu.Unlock()
}

func Listen(addr string) error {
	Init()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	fmt.Printf(" TCP %s (GS) listening...\n", addr)
	for {
		c, err := ln.Accept()
		if err != nil {
			return err
		}
		go Handle(c)
	}
}

func Handle(conn net.Conn) {
	defer conn.Close()
	addr := conn.RemoteAddr().String()
	fmt.Printf("\n###### [GS] connection %s ######\n", addr)

	st := &connState{conn: conn, roomID: config.DefaultRoomID, tskcid: config.DefaultTskCID}
	setState(conn, st)
	defer func() {
		if st.sess != nil {
			if fails := st.sess.NoteSoftResumeEOF(); fails > 0 {
				fmt.Printf(" [MATCH] soft-resume EOF fail #%d user=%q\n", fails, st.sess.Username)
			}
			held, last, room := st.sess.DetachGSForDisconnect(
				conn, st.playing, config.MatchReconnectHold, config.MatchReconnectHoldTTL,
			)
			if held {
				fmt.Printf(" [MATCH] reconnect hold user=%q room=%d ttl=%s\n",
					st.sess.Username, st.roomID, config.MatchReconnectHoldTTL)
			} else if last && room != nil && clock != nil {
				clock.DisarmIfIdle(room)
			}
		}
		delState(conn)
	}()

	buf := make([]byte, 0, 16384)
	tmp := make([]byte, 8192)
	ackSent := false
	framePeriod := time.Second / time.Duration(config.FrameHZ)

	for {
		// With the dedicated room ticker the frame clock no longer rides on
		// read timeouts, so keep the socket on the idle deadline and let the
		// owner goroutine drive frames.
		deadline := 120 * time.Millisecond
		if st.playing && config.FrameClock && !config.FrameTicker {
			deadline = framePeriod
		}
		_ = conn.SetReadDeadline(time.Now().Add(deadline))
		n, err := conn.Read(tmp)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				if st.playing && st.sess != nil && st.sess.Room != nil && config.FrameClock && clock != nil {
					clock.PumpOnTimeoutFor(st.sess.Room, st.sess)
					st.lastFrame = time.Now()
				}
				continue
			}
			fmt.Printf(" [GS] EOF/err %s: %v\n", addr, err)
			return
		}
		buf = append(buf, tmp[:n]...)

		for {
			key, seq, payload, pktLen, ok := wiregs.TryDecryptPacket(buf)
			if !ok {
				if pktLen > 11 && len(buf) < pktLen {
					break
				}
				if len(buf) >= 11 {
					fmt.Printf(" [GS] bad decrypt; drop 1B (have=%d)\n", len(buf))
					buf = buf[1:]
					continue
				}
				break
			}
			buf = buf[pktLen:]
			fmt.Printf(" [GS PKT] key=%#02x seq=%d plen=%d\n", key, seq, len(payload))

			if !ackSent {
				if op, _, sub, body, ok := wiregs.ParseC2SHeaderWithSlot(payload); ok && op == 9 && sub == 4 {
					if !handleReLogin(conn, st, seq, body) {
						return
					}
					ackSent = true
					continue
				}
				handleLogin(conn, st, seq, payload)
				ackSent = true
				continue
			}
			handlePkt(conn, st, seq, payload)
		}
	}
}

func handleReLogin(conn net.Conn, st *connState, seq uint16, body []byte) bool {
	if !config.MatchReconnectHold {
		fmt.Printf(" [GS] ReLoginReq rejected: reconnect hold disabled\n")
		return false
	}
	req, ok := wiregs.ParseReLoginReq(body)
	if !ok {
		fmt.Printf(" [GS] ReLoginReq rejected: malformed body=%dB hex=%x\n", len(body), body)
		return false
	}
	sess, claim, why := session.ClaimMatchHold(req.RoomName, req.GUID, conn, time.Now())
	if sess == nil {
		fmt.Printf(" [GS] ReLoginReq rejected room=%q guid=%q reason=%s\n", req.RoomName, req.GUID, why)
		return false
	}

	ackSeq := int32(seq) + 1
	// LIVE: soft success Ack still yields Dev|3005 even when syn-aligned. After
	// N quick EOF cycles, answer success=0 so the client stops Relogin spam.
	failMax := config.MatchReloginFailMax
	if failMax > 0 && sess.SoftResumeFailCount() >= failMax {
		// success=0 does not stop mid-match soft path (Game+0x160≠1 still
		// calls OnSoftReconnectSucceed). Refuse without a soft-shaped Ack.
		fmt.Printf(" [GS] ReLoginReq refuse after %d soft-fail EOFs user=%q (no Ack)\n",
			sess.SoftResumeFailCount(), sess.Username)
		sess.MarkGSLeave()
		session.LeaveRoom(sess, "relogin_fail_max")
		return false
	}

	ackBody := wiregs.ReLoginAck(claim.RoomID, claim.Seat, claim.TskCID)
	st.mu.Lock()
	st.sess = sess
	st.custom = true
	st.peerReady = false
	st.roomID = claim.RoomID
	st.tskcid = claim.TskCID
	st.loading = false
	st.playing = false
	st.seq = ackSeq + 1
	st.resumeSyn = int(req.RequestedSyn)
	st.resumePending = true
	pkt := wiregs.BuildReply(ackSeq, 9, 5, ackBody, 0x24)
	_, err := conn.Write(pkt)
	st.mu.Unlock()
	if err != nil {
		fmt.Printf(" [GS] ReLoginAck write failed user=%q: %v\n", sess.Username, err)
		return false
	}
	if !sess.CompleteMatchResume(conn) {
		fmt.Printf(" [GS] ReLoginAck lost room claim user=%q room=%d\n", sess.Username, claim.RoomID)
		return false
	}
	st.mu.Lock()
	st.peerReady = true
	st.playing = true
	st.mu.Unlock()
	sess.NoteSoftResumeAck()

	// Align syn to req+1. Bump when behind; rewind only if every other peer is
	// frozen/held (LIVE Fix12 always-rewind + 50ms stall → duplicate syn=7
	// flap and op7 paused from StartPlay). AwaitResumePing still gates op11
	// until rejoiner op3.
	peersFrozen := sess.MatchPeersFrozen()
	claim.Room.Lock()
	oldSyn := claim.Room.MatchSynNext
	oldFrame := claim.Room.MatchSFrame
	forced := false
	if req.RequestedSyn > 0 {
		need := int(req.RequestedSyn) + 1
		if claim.Room.MatchSynNext < need {
			claim.Room.MatchSynNext = need
			if req.GSFrame > 0 && claim.Room.MatchSFrame < int(req.GSFrame) {
				claim.Room.MatchSFrame = int(req.GSFrame)
			}
		} else if claim.Room.MatchSynNext > need && peersFrozen {
			forced = true
			claim.Room.MatchSynNext = need
			if req.GSFrame > 0 {
				claim.Room.MatchSFrame = int(req.GSFrame)
			} else if claim.Room.MatchSFrame > int(req.RequestedSyn) {
				claim.Room.MatchSFrame = int(req.RequestedSyn)
			}
		}
	}
	sframe := claim.Room.MatchSFrame
	synNext := claim.Room.MatchSynNext
	claim.Room.Unlock()
	if synNext != oldSyn || sframe != oldFrame {
		fmt.Printf(" [MATCH] resume syn sync user=%q syn %d->%d sframe %d->%d (req=%d forced=%v)\n",
			sess.Username, oldSyn, synNext, oldFrame, sframe, req.RequestedSyn, forced)
	}
	fmt.Printf(" [GS SENT] ReLoginAck sub=5 seq=%d user=%q room=%s seat=%d hero=%d reqsyn=%d gsfrm=%d exefrm=%d sframe=%d synnext=%d ack=%x reqbody=%dB\n",
		ackSeq, sess.Username, req.RoomName, claim.Seat+1, claim.Hero, req.RequestedSyn,
		req.GSFrame, req.ExeFrame, sframe, synNext, ackBody, len(body))
	// Do not pump op11 here — wait for rejoiner op3 (NoteSoftResumeStable).
	return true
}

// replayMatchResume sends the exact missed op7/op11 syn stream to one
// rejoiner. The room clock and survivor are never rewound or paused. The
// rejoiner's MatchAwaitResumePing gate remains set while replay is sent, so
// the live clock continues for the survivor but does not inject a far-ahead
// packet into this connection before its cursor is caught up.
func replayMatchResume(conn net.Conn, st *connState) bool {
	if st == nil || st.sess == nil || st.sess.Room == nil {
		return false
	}
	st.mu.Lock()
	pending, fromSyn := st.resumePending, st.resumeSyn
	st.mu.Unlock()
	if !pending {
		return false
	}

	room := st.sess.Room
	room.Lock()
	packets, oldest, ok := room.MatchReplayFromLocked(fromSyn)
	if !ok {
		room.Unlock()
		fmt.Printf(" [MATCH] resume replay unavailable user=%q reqsyn=%d oldest=%d\n",
			st.sess.Username, fromSyn, oldest)
		st.mu.Lock()
		st.resumePending = false
		st.mu.Unlock()
		return false
	}
	// Claim wire before releasing room state. Any newly numbered live packet
	// waits behind the replay; the room clock itself is never rewound.
	room.WireLock()
	room.Unlock()
	for _, p := range packets {
		st.mu.Lock()
		seq := st.seq
		st.seq++
		netx.ApplyMatchWriteDeadline(conn)
		_, err := conn.Write(wiregs.BuildInMatch(seq, p.Opcode, p.Sub, p.Body, p.Slot))
		netx.ClearMatchWriteDeadline(conn)
		st.mu.Unlock()
		if err != nil {
			fmt.Printf(" [MATCH] resume replay write failed user=%q syn=%d: %v\n",
				st.sess.Username, p.Syn, err)
			break
		}
	}
	room.WireUnlock()
	st.mu.Lock()
	st.resumePending = false
	st.mu.Unlock()
	// Reopen this peer only after the exact requested syn range has been sent.
	st.sess.NoteSoftResumeStable()
	fmt.Printf(" [MATCH] resume replay user=%q from=%d packets=%d\n",
		st.sess.Username, fromSyn, len(packets))
	return true
}

func handleLogin(conn net.Conn, st *connState, seq uint16, payload []byte) {
	user, token := wiregs.ParseLoginIdentity(payload)
	sess, how := session.ResolveGS(token, user)
	custom := false
	if sess != nil {
		sess.AttachGS(conn)
		st.sess = sess
		st.roomID = sess.RoomID
		st.tskcid = sess.TskCID
		if st.roomID <= 0 {
			st.roomID = config.DefaultRoomID
		}
		if st.tskcid <= 0 {
			st.tskcid = config.DefaultTskCID
		}
		custom = sess.CustomMatchArmed
		// Rejoin / second GS: still in an open custom room ⇒ seat path even if
		// CustomMatchArmed was already consumed by a prior GS attempt.
		if !custom && config.CustomNoLoadMap && sess.Room != nil {
			stRoom := sess.Room.State
			if stRoom == "open" || stRoom == "starting" {
				custom = true
			}
		}
		if sess.CustomMatchArmed {
			sess.CustomMatchArmed = false
		}
		st.custom = custom
	}
	fmt.Printf(" [GS] LoginReq resolve=%s user=%q token=%q custom=%v\n", how, user, token, custom)

	ackSeq := int32(seq) + 1
	ack := wiregs.LoginAck(st.roomID, st.tskcid)
	pkt := wiregs.BuildReply(ackSeq, 9, 2, ack, 0x24)
	_, _ = conn.Write(pkt)
	st.seq = ackSeq + 1
	fmt.Printf(" [GS SENT] LoginAck seq=%d hoc_r%d\n", ackSeq, st.roomID)

	acc := (*accounts.Account)(nil)
	if sess != nil {
		acc = sess.Account
	}
	bodyUI := wiregs.BuildUserInfo(acc)
	_, _ = conn.Write(wiregs.BuildReply(st.seq, 0x0d, 1, bodyUI, 0x24))
	st.seq++
	customSeat := custom && config.CustomNoLoadMap
	if customSeat {
		// Python LIVE pin: custom MatchSetting receives GetUserInfo only. Even a
		// light BuyItem re-enters inventory handlers mid-seat and dirties the
		// client task. This local profile must not alter the main-menu delivery
		// latch either; a replacement trade GS is restored after room exit.
		heroesDelivered := false
		if sess != nil {
			heroesDelivered = sess.HeroesDelivered
		}
		fmt.Printf(" [GS SENT] GetUserInfo only (custom seat; BuyItem skipped, heroes latch=%v)\n",
			heroesDelivered)
	} else {
		bodyBI := wiregs.BuildBuyItem(acc, wiregs.BuyItemOptions{
			Ownership: true,
			Kitabe:    config.LoginBuyItemKitabe,
		})
		_, _ = conn.Write(wiregs.BuildReply(st.seq, 0x0d, 5, bodyBI, 0x24))
		st.seq++
		fmt.Printf(" [GS SENT] GetUserInfo+BuyItem level inject\n")
		if sess != nil {
			sess.HeroesDelivered = true
		}
	}

	if customSeat {
		cid := wiregs.CIDOnly(st.tskcid)
		if config.PushPlayerReady100A {
			_, _ = conn.Write(wiregs.BuildReply(st.seq, 9, 0x100A, cid, 0x24))
			st.seq++
		}
		if config.PushGotData100B {
			_, _ = conn.Write(wiregs.BuildReply(st.seq, 9, 0x100B, cid, 0x24))
			st.seq++
			fmt.Printf(" [GS SENT] ★SOC 0x100B seat lobby\n")
		}
	} else if !custom {
		cid := wiregs.CIDOnly(st.tskcid)
		if config.PushGotData100B {
			_, _ = conn.Write(wiregs.BuildReply(st.seq, 9, 0x100B, cid, 0x24))
			st.seq++
			fmt.Printf(" [GS SENT] ★SOC 0x100B (hybrid hold)\n")
		}
	}
	st.mu.Lock()
	st.peerReady = true
	st.mu.Unlock()
	if custom && config.CustomNoLoadMap && sess != nil {
		sendSeat1002(conn, st, "login-100B")
	}
}

func handlePkt(conn net.Conn, st *connState, seq uint16, payload []byte) {
	op, slot, sub, body, ok := wiregs.ParseC2SHeaderWithSlot(payload)
	if !ok {
		return
	}
	if st.sess != nil {
		st.sess.TouchGSActivity()
	}
	fmt.Printf(" [GS] op=%d sub=0x%04x body=%dB\n", op, sub, len(body))

	switch {
	case op == 9 && sub == 0x3001:
		st.mu.Lock()
		wasCustom := st.custom
		st.custom = false
		st.loading = false
		st.playing = false
		st.mu.Unlock()
		if wasCustom && st.sess != nil && st.sess.Room != nil {
			notifyRoomLeave3003(st.sess.Room, st.sess, "gs-3001")
		}
		if st.sess != nil {
			st.sess.MarkGSLeave()
		}
		fmt.Printf(" [GS] 0x3001 leave: local state idle\n")

	case op == 9 && sub == 0x1006:
		handleSeatHop(conn, st, body)

	case op == 9 && sub == 0x1008:
		hero, skin := wiregs.ParseHeroChange(body)
		if !wiregs.ValidSeatHero(hero) {
			fmt.Printf(" [GS] 0x1008 BAD hero=%d skin=%d body=%dB — ignore (no wipe)\n",
				hero, skin, len(body))
			return
		}
		if st.sess != nil {
			accepted, owner := session.ClaimSeatHero(st.sess, hero, skin)
			if !accepted {
				fmt.Printf(" [ROOM] HERO-LOCK reject user=%q hero=%d owner=%q (no-ack)\n",
					st.sess.Username, hero, owner)
				return
			}
		}
		ack := wiregs.HeroAck(st.tskcid, hero, skin)
		sendSyn(conn, st, 0x1009, ack)
		fmt.Printf(" [GS SENT] HeroAck hero=%d skin=%d\n", hero, skin)
		if st.custom && config.CustomNoLoadMap && !st.loading {
			sendSeat1002(conn, st, "after-HeroAck")
		}

	case op == 9 && sub == 0x100C:
		skill := body
		if len(skill) < 4 {
			skill = wiregs.CIDOnly(st.tskcid)
		}
		if s1, s2, ok := wiregs.ParseSummonerSpells(skill); ok && st.sess != nil {
			st.sess.SeatSpell1, st.sess.SeatSpell2 = s1, s2
		}
		if rdy, ok := wiregs.ParseReadyFromSkill(skill); ok && st.sess != nil {
			accepted := st.sess.ApplyReadyFromSkill(rdy == 1)
			fmt.Printf(" [GS] READY 0x100C user=%q value=%d accepted=%v\n",
				st.sess.Username, rdy, accepted)
		}
		st.skillBodies = append(st.skillBodies, append([]byte(nil), skill...))
		if len(st.skillBodies) > 8 {
			st.skillBodies = st.skillBodies[len(st.skillBodies)-8:]
		}
		sendSyn(conn, st, 0x100D, skill)
		if st.custom && st.sess != nil && st.sess.Room != nil {
			broadcastRoomSyn(st.sess.Room, st.sess, 0x100D, skill, "skill-ack-peer")
		}
		sp1, sp2 := 0, 0
		if st.sess != nil {
			sp1, sp2 = st.sess.SeatSpell1, st.sess.SeatSpell2
		}
		fmt.Printf(" [GS SENT] SkillAck echo %dB spells=%d/%d\n", len(skill), sp1, sp2)
		if st.custom && config.CustomNoLoadMap && !st.loading {
			sendSeat1002(conn, st, "after-SkillAck")
		}

	case op == 9 && sub == 0x1010:
		cid := wiregs.CIDOnly(st.tskcid)
		sendSyn(conn, st, 0x1011, cid)
		fmt.Printf(" [GS SENT] 0x1011 AllAck\n")
		// Re-ack real pick only (AllAck wipes seat UI).
		if st.sess != nil && wiregs.ValidSeatHero(st.sess.SeatHeroID) {
			ack := wiregs.HeroAck(st.tskcid, st.sess.SeatHeroID, st.sess.SeatSkinID)
			sendSyn(conn, st, 0x1009, ack)
			fmt.Printf(" [GS SENT] ★HeroAck re-ack after AllAck hero=%d\n", st.sess.SeatHeroID)
		}
		for i, sb := range st.skillBodies {
			sendSyn(conn, st, 0x100D, sb)
			fmt.Printf(" [GS SENT] ★SkillAck replay after AllAck #%d/%d %dB\n",
				i+1, len(st.skillBodies), len(sb))
		}
		if st.custom && config.CustomNoLoadMap && !st.loading {
			sendSeat1002(conn, st, "after-AllAck")
		}

	case op == 9 && sub == 0x2001:
		handleReady(conn, st)

	case op == 9 && sub == 0x2003:
		sendSyn(conn, st, 0x2004, wiregs.StartPlay(st.tskcid))
		st.playing = true
		st.loading = false
		if st.sess != nil {
			st.sess.SetMatchPlaying(true)
			st.sess.SetMatchLoading(false)
			st.sess.TouchGSActivity()
			if st.sess.Room != nil {
				clock.Arm(st.sess.Room)
			}
		}
		st.lastFrame = time.Now()
		fmt.Printf(" [GS SENT] ★StartPlay 0x2004 — frame clock armed (recv-timeout)\n")

	case op == 3:
		// Always in-match op4 framing — login-style op4 after soft SUCCESS|3002
		// confused the gameplay path. First post-ReloginAck op3 replays the
		// exact missed syn stream before the live clock can target this peer.
		if st.sess != nil {
			if !replayMatchResume(conn, st) {
				st.sess.NoteSoftResumeStable()
			}
		}
		var i1, i2 uint32
		if len(body) >= 8 {
			i1 = binary.LittleEndian.Uint32(body[0:4])
			i2 = binary.LittleEndian.Uint32(body[4:8])
		}
		pong := wiregs.Op4Pong(i1, i2, uint32(st.roomID))
		st.mu.Lock()
		seqN := st.seq
		st.seq++
		_, _ = conn.Write(wiregs.BuildInMatch(seqN, 4, 0, pong, 0))
		st.mu.Unlock()
		if st.playing {
			fmt.Printf(" [GS SENT] op4 pong room=%d\n", st.roomID)
		}

	case op == 7:
		handleUnitAction(conn, st, slot, sub, body)

	case op == 0xc || op == 12:
		trade.Dispatch(&trade.Ctx{
			Conn:         conn,
			Sess:         st.sess,
			Custom:       st.custom,
			Loading:      st.loading || (st.sess != nil && st.sess.MatchLoading),
			TskCID:       st.tskcid,
			Body:         body,
			Sub:          sub,
			KitabeSeeded: &st.kitabeSeeded,
			Send: func(s uint16, b []byte) {
				st.mu.Lock()
				seqN := st.seq
				st.seq++
				_, _ = conn.Write(wiregs.BuildReply(seqN, 0x0d, s, b, 0x24))
				st.mu.Unlock()
				fmt.Printf(" [GS SENT] trade ACK sub=%#x %dB\n", s, len(b))
			},
		})

	default:
	}
}

func handleSeatHop(conn net.Conn, st *connState, body []byte) {
	if st == nil || st.sess == nil {
		return
	}
	oldSeat := st.sess.Seat
	wireSite, guid, ok := wiregs.ParseChangeSite1006(body)
	if !ok {
		wireSite = oldSeat + 1
		fmt.Printf(" [GS] 0x1006 invalid body=%dB — stay seat=%d\n", len(body), oldSeat+1)
	}

	room := st.sess.Room
	newSeat := oldSeat
	changed := false
	why := "invalid"
	if ok {
		room, oldSeat, newSeat, changed, why = session.ChangeSeat(st.sess, wireSite-1, guid)
	}
	hero := st.sess.SeatHeroID
	ack := wiregs.SiteAck1007(st.tskcid, newSeat+1, hero)
	oldWire := oldSeat + 1
	if oldWire < 1 || oldWire > 10 {
		oldWire = 1
	}

	seq, err := sendRoomSyn(conn, st, 0x1007, ack, oldWire)
	if err != nil {
		fmt.Printf(" [ROOM] hop 0x1007 requester write fail user=%q: %v\n", st.sess.Username, err)
		return
	}
	peerSends := 0
	if changed && room != nil {
		peerSends = broadcastRoomHop1007(room, st.sess, conn, oldSeat, newSeat, hero, ack)
	}
	fmt.Printf(" [ROOM] hop 0x1007 user=%q old=%d requested=%d new=%d hero=%d seq=%d peers=%d changed=%v why=%s\n",
		st.sess.Username, oldWire, wireSite, newSeat+1, hero, seq, peerSends, changed, why)
}

func handleReady(conn net.Conn, st *connState) {
	accepted, why := st.sess.AcceptReadyStart()
	if !accepted {
		if why == "cancelled" {
			fmt.Printf(" [GS] ★READY-CANCEL 0x2001 ignored user=%q\n", st.sess.Username)
		} else {
			fmt.Printf(" [GS] ★HERO-GATE 0x2001 rejected\n")
		}
		return
	}
	if !config.CustomReadyLoadMap {
		return
	}
	room := st.sess.Room
	if st.custom && config.CustomNoLoadMap && !st.loading {
		sendSeat1002(conn, st, "ready-2001")
	}

	// Multi: guest ready alone does not LoadMap; host starts when guests ready.
	if room != nil && room.MemberCount() >= 2 {
		isHost := room.Host == st.sess
		if !isHost {
			fmt.Printf(" [GS] ★READY-GATE guest ready user=%s wait host\n", st.sess.Username)
			return
		}
		if !room.GuestsReady() {
			fmt.Printf(" [GS] ★READY-GATE host Start — guests not ready\n")
			return
		}
		broadcastSharedLoadMap(room, "host-start-all-ready")
		return
	}

	// Solo 1P
	st.loading = true
	acc := st.sess.Account
	nick, guid := "", st.sess.GUID
	if acc != nil {
		nick = acc.Nickname
	}
	seed := 0
	gameMode := config.DefaultCustomMode
	mapName := ""
	customOpts := config.DefaultCustomRoomOptions()
	if room != nil {
		seed = room.EnsureSeed()
		mapName = room.MapName
		customOpts = room.CustomOpts
		gameMode = config.GameModeForMapName(mapName)
	}
	lm := wiregs.LoadMapSolo(
		st.tskcid, st.sess.SeatHeroID, st.sess.SeatSkinID,
		st.sess.SeatSpell1, st.sess.SeatSpell2,
		gameMode, config.GSIModeParam, nick, guid,
	)
	if seed != 0 {
		// rebuild with seed via shared helper
		lm = wiregs.LoadMapShared(st.tskcid, 1, seed, gameMode, config.GSIModeParam, customOpts, []wiregs.LoadMapMember{{
			Seat0: 0, Hero: st.sess.SeatHeroID, Skin: st.sess.SeatSkinID,
			Spell1: st.sess.SeatSpell1, Spell2: st.sess.SeatSpell2,
			Nick: nick, GUID: guid, IsOwner: true,
		}})
	}
	sendSyn(conn, st, 0x2002, lm)
	fmt.Printf(" [GS SENT] LoadMap 0x2002 seat=1 hero=%d map=%q mode=%d opts=%d/%d/%d/%d\n",
		st.sess.SeatHeroID, mapName, gameMode, customOpts.InitialGold,
		customOpts.GetGold, customOpts.InitialLevel, customOpts.GetExp)
}

func broadcastSharedLoadMap(room *session.Room, reason string) {
	members := room.SnapshotMembers()
	missing := []string{}
	for _, m := range members {
		if m == nil || m.Account == nil {
			continue
		}
		if m.SeatHeroID <= 0 {
			missing = append(missing, m.Username)
		}
	}
	if len(missing) > 0 {
		fmt.Printf(" [ROOM] ★HERO-GATE LoadMap blocked — no pick: %v (%s)\n", missing, reason)
		return
	}
	seed := room.EnsureSeed()
	var roster []wiregs.LoadMapMember
	for _, m := range members {
		if m == nil || m.Account == nil || m.SeatHeroID <= 0 {
			continue
		}
		nick := m.Account.Nickname
		roster = append(roster, wiregs.LoadMapMember{
			Seat0: m.Seat, Hero: m.SeatHeroID, Skin: m.SeatSkinID,
			Spell1: m.SeatSpell1, Spell2: m.SeatSpell2,
			Nick: nick, GUID: m.GUID, IsOwner: room.Host == m,
		})
	}
	room.Lock()
	room.State = "match"
	room.MatchClockArmed = false
	room.MatchSFrame = 0
	room.MatchSynNext = 0
	room.MatchFramesSent = 0
	tsk := room.TskCID
	// Read the room's map and custom options under the same lock; both are
	// translated outside it.
	mapName := room.MapName
	customOpts := room.CustomOpts
	room.Unlock()
	gameMode := config.GameModeForMapName(mapName)
	if tsk <= 0 {
		tsk = config.DefaultTskCID
	}
	for _, dest := range members {
		if dest == nil || dest.Account == nil {
			continue
		}
		dest.SetMatchLoading(true)
	}
	for _, dest := range members {
		if dest == nil || dest.Account == nil {
			continue
		}
		localSeat := dest.Seat + 1
		if localSeat < 1 {
			localSeat = 1
		}
		payload := wiregs.LoadMapShared(tsk, localSeat, seed, gameMode, config.GSIModeParam, customOpts, roster)
		for _, gc := range dest.SnapshotGSConns() {
			st2 := getState(gc)
			if st2 == nil {
				continue
			}
			st2.loading = true
			sendSyn(gc, st2, 0x2002, payload)
			fmt.Printf(" [ROOM] ★LoadMap SHARED 0x2002 → user=%s seat1=%d seed=%d roster=%d map=%q mode=%d opts=%d/%d/%d/%d (%s)\n",
				dest.Username, localSeat, seed, len(roster), mapName, gameMode,
				customOpts.InitialGold, customOpts.GetGold, customOpts.InitialLevel,
				customOpts.GetExp, reason)
		}
	}
}

func handleUnitAction(conn net.Conn, st *connState, slot byte, sub uint16, act []byte) {
	room := (*session.Room)(nil)
	if st.sess != nil {
		room = st.sess.Room
	}
	if room == nil || !st.playing {
		fmt.Printf(" [GS] op7 ignored before armed room clock sub=%#x body=%dB\n", sub, len(act))
		return
	}

	// LIVE pin: op7 and op11 share one room syn counter. Stamp and consume
	// under the room lock, then hand over to the room wire lock for the actual
	// writes so neither packet can overtake the other on the wire (sequenceID
	// wrong UP/TP -> Dev|3004, AGENTS4 §11.7). The writes must NOT run under
	// the room lock: a congested WAN peer would block all room state
	// (AGENTS5 §2.3.5).
	room.Lock()
	if !room.MatchClockArmed {
		room.Unlock()
		fmt.Printf(" [GS] op7 ignored: room clock not armed sub=%#x body=%dB\n", sub, len(act))
		return
	}
	frameAt := room.MatchSFrame + config.FrameLead
	if frameAt <= room.MatchSFrame {
		frameAt = room.MatchSFrame + 1
	}
	synUsed := room.MatchSynNext
	stamped, oldFrame, oldSyn, ok := wiregs.StampUnitAction(act, int32(frameAt), int32(synUsed))
	if !ok {
		room.Unlock()
		fmt.Printf(" [GS] op7 ignored: short body sub=%#x body=%dB\n", sub, len(act))
		return
	}
	if config.SynConsume {
		room.MatchSynNext++
	}
	room.AppendMatchReplayLocked(session.MatchReplayPacket{
		Syn: synUsed, Opcode: 7, Sub: sub, Slot: slot, Body: stamped,
	})
	members := append([]*session.Session(nil), room.Members...)
	sframe := room.MatchSFrame
	// Take the wire lock before releasing the state lock so op7 keeps its slot
	// in wire order (hand-over-hand); never acquire mu while holding wireMu.
	room.WireLock()
	room.Unlock()
	defer room.WireUnlock()

	st.mu.Lock()
	seqN := st.seq
	st.seq++
	_, _ = conn.Write(wiregs.BuildInMatch(seqN, 7, sub, stamped, slot))
	st.mu.Unlock()

	peerSends := 0
	for _, m := range members {
		if m == nil || m == st.sess || !m.IsMatchPlaying() || m.IsMatchClockFrozen() {
			continue
		}
		for _, gc := range m.SnapshotGSConns() {
			st2 := getState(gc)
			if st2 == nil {
				continue
			}
			st2.mu.Lock()
			s2 := st2.seq
			st2.seq++
			netx.ApplyMatchWriteDeadline(gc)
			_, _ = gc.Write(wiregs.BuildInMatch(s2, 7, sub, stamped, slot))
			netx.ClearMatchWriteDeadline(gc)
			st2.mu.Unlock()
			peerSends++
		}
	}
	fmt.Printf(" [MATCH] op7 RELAY sub=%#x slot=%d frame[0]=%d(was %d,sframe=%d) syn[1]=%d(was %d) peers=%d\n",
		sub, slot, frameAt, oldFrame, sframe, synUsed, oldSyn, peerSends)
}

func sendSyn(conn net.Conn, st *connState, sub uint16, body []byte) {
	st.mu.Lock()
	seq := st.seq
	st.seq++
	_, _ = conn.Write(wiregs.BuildReply(seq, 9, sub, body, 0x24))
	st.mu.Unlock()
}

func sendSeat1002(conn net.Conn, st *connState, reason string) {
	if st.sess == nil {
		return
	}
	if st.sess.Room != nil {
		syncRoomSeats1002(st.sess.Room, reason)
		return
	}
	fmt.Printf(" [GS] seat-sync skipped room=nil user=%q (%s)\n", st.sess.Username, reason)
}

type roomSeatEntry struct {
	sess   *session.Session
	member wiregs.SeatRosterMember
}

func roomSeatEntries(room *session.Room) ([]roomSeatEntry, int) {
	if room == nil {
		return nil, config.DefaultTskCID
	}
	room.Lock()
	members := append([]*session.Session(nil), room.Members...)
	host := room.Host
	tskcid := room.TskCID
	room.Unlock()
	if tskcid <= 0 {
		tskcid = config.DefaultTskCID
	}

	entries := make([]roomSeatEntry, 0, len(members))
	for _, member := range members {
		if member == nil || member.Account == nil {
			continue
		}
		nick := member.Account.Nickname
		if nick == "" {
			nick = member.Account.Username
		}
		guid := member.GUID
		if guid == "" && member.Username != "" {
			guid = "gllive:" + member.Username
		}
		hero := member.SeatHeroID
		skin, spell1, spell2 := 0, 0, 0
		if hero > 0 {
			skin = member.SeatSkinID
			spell1 = member.SeatSpell1
			spell2 = member.SeatSpell2
		}
		entries = append(entries, roomSeatEntry{
			sess: member,
			member: wiregs.SeatRosterMember{
				Seat0: member.Seat, Hero: hero, Skin: skin,
				Spell1: spell1, Spell2: spell2,
				Nick: nick, GUID: guid, IsOwner: member == host,
				Ready: member.SeatReady && hero > 0,
			},
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].member.Seat0 < entries[j].member.Seat0
	})
	return entries, tskcid
}

func syncRoomSeats1002(room *session.Room, reason string) {
	entries, tskcid := roomSeatEntries(room)
	if len(entries) == 0 {
		return
	}
	roster := make([]wiregs.SeatRosterMember, 0, len(entries))
	for _, entry := range entries {
		roster = append(roster, entry.member)
	}
	for _, dest := range entries {
		localSeat := dest.member.Seat0
		payload := wiregs.SeatRoster1002(tskcid, localSeat, roster)
		for _, gsConn := range dest.sess.SnapshotGSConns() {
			st := getState(gsConn)
			if st == nil || !st.canPeerSeatSync() {
				continue
			}
			seq, err := sendRoomSyn(gsConn, st, 0x1002, payload, localSeat+1)
			if err != nil {
				fmt.Printf(" [ROOM] seat-sync write fail user=%q: %v\n", dest.sess.Username, err)
				continue
			}
			fmt.Printf(" [ROOM] seat-sync user=%q local=%d members=%d seq=%d (%s)\n",
				dest.sess.Username, localSeat+1, len(roster), seq, reason)
		}
	}
}

func broadcastRoomSyn(room *session.Room, source *session.Session, sub uint16, body []byte, reason string) {
	if room == nil || source == nil {
		return
	}
	sourceSite := source.Seat + 1
	for _, dest := range room.SnapshotMembers() {
		if dest == nil || dest == source {
			continue
		}
		for _, gsConn := range dest.SnapshotGSConns() {
			st := getState(gsConn)
			if st == nil || !st.canPeerSeatSync() {
				continue
			}
			seq, err := sendRoomSyn(gsConn, st, sub, body, sourceSite)
			if err == nil {
				fmt.Printf(" [ROOM] peer-sync sub=0x%04x from=%q to=%q seq=%d (%s)\n",
					sub, source.Username, dest.Username, seq, reason)
			}
		}
	}
}

// broadcastRoomHop1007 sends the atomic seat move to every other live GS in
// the room. The requester was already ACKed by handleSeatHop. Header nibble is
// the mover's old 1-based seat for every recipient; no 0x3003/0x1002 follows.
func broadcastRoomHop1007(room *session.Room, mover *session.Session, requester net.Conn, oldSeat, newSeat, hero int, body []byte) int {
	if room == nil || mover == nil {
		return 0
	}
	oldWire := oldSeat + 1
	if oldWire < 1 || oldWire > 10 {
		oldWire = 1
	}
	sent := 0
	for _, dest := range room.SnapshotMembers() {
		if dest == nil {
			continue
		}
		for _, gsConn := range dest.SnapshotGSConns() {
			if gsConn == requester {
				continue
			}
			peerState := getState(gsConn)
			if peerState == nil || !peerState.canPeerRoomWrite() {
				continue
			}
			seq, err := sendRoomSyn(gsConn, peerState, 0x1007, body, oldWire)
			if err != nil {
				fmt.Printf(" [ROOM] hop 0x1007 write fail from=%q to=%q: %v\n",
					mover.Username, dest.Username, err)
				continue
			}
			sent++
			fmt.Printf(" [ROOM] hop 0x1007 peer from=%q to=%q old=%d new=%d hero=%d seq=%d\n",
				mover.Username, dest.Username, oldWire, newSeat+1, hero, seq)
		}
	}
	return sent
}

func broadcastRoomLeave3003(room *session.Room, leaver *session.Session, reason string) {
	if room == nil || leaver == nil {
		return
	}
	leftSeat := leaver.Seat
	nick := leaver.Username
	if leaver.Account != nil {
		if leaver.Account.Nickname != "" {
			nick = leaver.Account.Nickname
		} else if leaver.Account.Username != "" {
			nick = leaver.Account.Username
		}
	}
	room.Lock()
	tskcid := room.TskCID
	room.Unlock()
	if tskcid <= 0 {
		tskcid = config.DefaultTskCID
	}
	body := wiregs.PlayerLeaveRoom3003(tskcid, leftSeat, nick)
	for _, dest := range room.SnapshotMembers() {
		if dest == nil || dest == leaver || dest.Seat == leftSeat {
			continue
		}
		for _, gsConn := range dest.SnapshotGSConns() {
			st := getState(gsConn)
			if st == nil || !st.canPeerRoomWrite() {
				continue
			}
			seq, err := sendRoomSyn(gsConn, st, 0x3003, body, leftSeat+1)
			if err == nil {
				fmt.Printf(" [ROOM] leave 0x3003 from=%q seat=%d to=%q seq=%d (%s)\n",
					leaver.Username, leftSeat+1, dest.Username, seq, reason)
			}
		}
	}
}

func notifyRoomLeave3003(room *session.Room, leaver *session.Session, reason string) {
	if leaver == nil || !leaver.ClaimSeatVacate() {
		return
	}
	broadcastRoomLeave3003(room, leaver, reason)
}

func sendRoomSyn(conn net.Conn, st *connState, sub uint16, body []byte, senderSite int) (int32, error) {
	if senderSite < 1 {
		senderSite = 1
	}
	if senderSite > 10 {
		senderSite = 10
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	seq := st.seq
	_, err := conn.Write(wiregs.BuildSynReply(seq, sub, body, senderSite, 0x24))
	if err != nil {
		return seq, err
	}
	st.seq++
	return seq, nil
}
