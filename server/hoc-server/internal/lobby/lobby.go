package lobby

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"time"

	"hoc-server/internal/accounts"
	"hoc-server/internal/config"
	"hoc-server/internal/session"
	"hoc-server/internal/wire/glblock"
)

func Handle(conn net.Conn, addr string) {
	defer conn.Close()
	sess := session.Create(conn, addr)
	defer session.Destroy(conn)
	peer := sess.Peer
	fmt.Printf("\n###### [LOBBY#%d TCP] %s ######\n", peer, addr)

	_ = conn.SetDeadline(time.Time{})
	buf := make([]byte, 0, 8192)
	tmp := make([]byte, 4096)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		n, err := conn.Read(tmp)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				fmt.Printf(" [LOBBY#%d IDLE] keeping open\n", peer)
				continue
			}
			fmt.Printf(" [LOBBY#%d EOF/err] %v\n", peer, err)
			return
		}
		buf = append(buf, tmp[:n]...)
		if len(buf) >= 2 && buf[0] == 0x16 && buf[1] == 0x03 {
			fmt.Printf(" [LOBBY#%d] TLS misrouted — close\n", peer)
			return
		}
		for {
			if len(buf) < 6 {
				break
			}
			totalLen := int(binary.BigEndian.Uint16(buf[0:2]))
			dummy := binary.BigEndian.Uint16(buf[2:4])
			pktLen := 2 + totalLen
			isGL := dummy == 0 && totalLen >= 8 && totalLen <= 0x8000
			if !isGL {
				if len(buf) < 8 {
					break
				}
				fmt.Printf(" [LOBBY#%d] drop 8B affine %x\n", peer, buf[:8])
				buf = buf[8:]
				continue
			}
			if len(buf) < pktLen {
				break
			}
			pkt := append([]byte(nil), buf[:pktLen]...)
			buf = buf[pktLen:]
			op := glblock.Opcode(pkt)
			fmt.Printf(" [LOBBY#%d PKT] op=0x%04x (%dB)\n", peer, op, pktLen)
			handleOpcode(conn, sess, peer, op, pkt)
		}
	}
}

func handleOpcode(conn net.Conn, sess *session.Session, peer int, op uint16, pkt []byte) {
	switch op {
	case 0x1601:
		acc := sess.Account
		if acc == nil {
			acc = accounts.Get("enterpries1")
		}
		aid := "1000001"
		if acc != nil {
			if acc.AccountID != "" {
				aid = acc.AccountID
			} else {
				aid = fmt.Sprintf("%d", acc.UserID)
			}
		}
		rep := glblock.HandshakeReply(aid)
		_, _ = conn.Write(rep)
		fmt.Printf(" [LOBBY#%d SENT] 0x6101 (%dB)\n", peer, len(rep))

	case 0x1203:
		uname := parseLoginUser(pkt)
		acc := accounts.Get(uname)
		created := false
		if acc == nil {
			acc, created = accounts.EnsureTemporary(uname)
		}
		if acc == nil {
			fmt.Printf(" [LOBBY#%d] 0x1203 identity missing for %q - seed enterpries1\n", peer, uname)
			acc = accounts.Get("enterpries1")
		}
		if gateway := connGateway(conn); gateway != "" && acc != nil {
			acc.SetTemporaryGateway(gateway)
		}
		sess.BindAccount(acc)
		sess.CustomMatchArmed = false
		sess.CustomRoomIntent = false
		sess.HeroesDelivered = false
		sess.ProfileBootstrap = false
		sess.ClearCustomRoomSearch()
		sess.CancelTradeGSRebootstrap()
		uid := int32(1000001)
		if acc != nil {
			uid = int32(acc.UserID)
		}
		rep := glblock.LoginReply(uid)
		_, _ = conn.Write(rep)
		fmt.Printf(" [LOBBY#%d SENT] 0x2103 user=%s nick=%q id=%d gateway=%s registered=%v\n",
			peer, sess.Username, accountNick(acc), uid, sess.Gateway(), created)
		_, _ = conn.Write(glblock.FlagsGLSReady())

	case 0x120b: // SearchRelayRoom — hybrid: SUCCESS until profile delivered
		gsip := sess.Gateway()
		switch {
		case sess.CustomRoomIntent:
			_, _ = conn.Write(glblock.RelayFail())
			fmt.Printf(" [LOBBY#%d SENT] 0x210b CANCEL 30002 (custom intent)\n", peer)
		case sess.HeroesDelivered:
			_, _ = conn.Write(glblock.RelayFail())
			fmt.Printf(" [LOBBY#%d SENT] 0x210b CANCEL 30002 (heroes delivered)\n", peer)
		default:
			// AUTO_MATCHMAKING=True pin — drive join→GS for level/wallet
			_, _ = conn.Write(glblock.RelaySuccess(gsip))
			fmt.Printf(" [LOBBY#%d SENT] 0x210b SUCCESS relay %s:20001 (profile inject)\n", peer, gsip)
		}

	case 0x1206: // join room → GS IP:PORT + token
		tok := sess.AllocToken()
		gsip := sess.Gateway()
		sess.AwaitingGS = true
		rep := glblock.JoinRoomReply(gsip, uint16(config.GSPort), []byte(tok))
		_, _ = conn.Write(rep)
		fmt.Printf(" [LOBBY#%d SENT] 0x2106 join GS=%s:%d token=%s\n", peer, gsip, config.GSPort, tok)

	case 0x1205: // keepalive
		_, _ = conn.Write(glblock.KeepaliveReply())
		fmt.Printf(" [LOBBY#%d SENT] 0x2105 keepalive\n", peer)
		// Profile bootstrap if hybrid never delivered (no 0x120b SUCCESS path).
		// Soak bots skip: leftover e02d poisons their join-token read.
		if !sess.HeroesDelivered && !sess.ProfileBootstrap {
			sess.ProfileBootstrap = true
			sendE02D(conn, sess, peer, "PROFILE-BOOTSTRAP", false)
		} else if (config.ServerKitabe || config.ServerTalent) && sess.ClaimTradeGSRebootstrap() {
			sendE02D(conn, sess, peer, "TRADE-GS after quit-room fallback", false)
		}

	case 0x1208:
		_, _ = conn.Write(glblock.Empty(0x2108))
		fmt.Printf(" [LOBBY#%d SENT] 0x2108 leave\n", peer)

	case 0xe03a:
		sess.CustomRoomIntent = true
		rooms := searchCustomRooms()
		roomIDs := make([]int, 0, len(rooms))
		for _, room := range rooms {
			roomIDs = append(roomIDs, int(room.ID))
		}
		sess.RememberCustomRoomSearch(roomIDs)
		_, _ = conn.Write(glblock.SearchCustomRooms(rooms))
		fmt.Printf(" [LOBBY#%d SENT] 0xe03b search open_rooms=%d\n", peer, len(rooms))
		if !sess.HeroesDelivered && !sess.ProfileBootstrap {
			sess.ProfileBootstrap = true
			sendE02D(conn, sess, peer, "PROFILE-BOOTSTRAP on e03a", false)
		}

	case 0xe038:
		sess.CustomRoomIntent = true
		sess.ClearCustomRoomSearch()
		opts := session.RoomOptions{
			Name: "room", Capacity: 10, Flag1012: 1, Flag1013: 0, JSON1014: []byte("{}"),
		}
		for _, c := range glblock.IterChildren(pkt) {
			switch {
			case c.TypeID == 0x102A && c.Type == glblock.TypeString && len(c.Value) > 0:
				opts.Name = string(c.Value)
			case c.TypeID == 0x1012 && c.Type == glblock.TypeChar && len(c.Value) > 0:
				opts.Flag1012 = c.Value[0]
			case c.TypeID == 0x1013 && c.Type == glblock.TypeChar && len(c.Value) > 0:
				opts.Flag1013 = c.Value[0]
			case c.TypeID == 0x100E && c.Type == glblock.TypeShort && len(c.Value) >= 2:
				opts.Capacity = int(binary.BigEndian.Uint16(c.Value[:2]))
			case c.TypeID == 0x1014 && c.Type == glblock.TypeString && len(c.Value) > 0:
				opts.JSON1014 = append([]byte(nil), c.Value...)
			}
		}
		room := session.CreateRoom(sess, opts)
		if room == nil {
			fmt.Printf(" [LOBBY#%d] e038 create failed\n", peer)
			return
		}
		roomName := []byte(room.Name)
		rep := glblock.CreateCustomReply(int32(room.ID), roomName)
		_, _ = conn.Write(rep)
		fmt.Printf(" [LOBBY#%d SENT] 0xe039 room_id=%d\n", peer, room.ID)
		if config.PushHostUserList && sess.Account != nil {
			nick := accountNick(sess.Account)
			uid := accountWireID(sess.Account)
			_, _ = conn.Write(glblock.JoinCustomHost(nick, uid, 0))
			fmt.Printf(" [LOBBY#%d SENT] 0xe03d host list\n", peer)
		}

	case 0xe03c: // join custom room
		if sess.Account == nil {
			fmt.Printf(" [LOBBY#%d] e03c SKIPPED — no account\n", peer)
			return
		}
		var joinRoomID *int
		var reqSeat *int
		for _, c := range glblock.IterChildren(pkt) {
			if c.TypeID == 0x100F && c.Type == glblock.TypeInt && len(c.Value) >= 4 {
				v := int(binary.BigEndian.Uint32(c.Value[:4]))
				joinRoomID = &v
			}
			if c.TypeID == 0x100A && c.Type == glblock.TypeChar && len(c.Value) > 0 {
				v := int(c.Value[0])
				reqSeat = &v
			}
		}
		if joinRoomID == nil {
			fmt.Printf(" [LOBBY#%d] e03c missing room_id\n", peer)
			return
		}
		requestedRoomID := *joinRoomID
		resolvedRoomID, correlated := sess.CorrelateCustomRoomJoin(requestedRoomID)
		if correlated {
			fmt.Printf(" [LOBBY#%d] e03c room_id=0 correlated advertised_room=%d\n", peer, resolvedRoomID)
		}
		room, why := session.JoinRoom(sess, resolvedRoomID, reqSeat)
		if room == nil {
			fmt.Printf(" [LOBBY#%d] e03c join FAIL room=%d resolved=%d why=%s\n",
				peer, requestedRoomID, resolvedRoomID, why)
			return
		}
		sess.ClearCustomRoomSearch()
		users := lobbyUsers(room)
		joinRep := glblock.JoinCustomRoster(users)
		_, _ = conn.Write(joinRep)
		fmt.Printf(" [LOBBY#%d SENT] 0xe03d join room=%d seat=%d members=%d\n",
			peer, room.ID, sess.Seat, len(users))
		nick := accountNick(sess.Account)
		uid := accountWireID(sess.Account)
		e05d := glblock.NewUserJoined(nick, uid, byte(sess.Seat))
		room.LobbyWriteAll(e05d, sess)
		room.LobbyWriteAll(joinRep, sess)
		sess.CustomRoomIntent = true
		sess.CustomMatchArmed = true
		_, _ = conn.Write(glblock.Empty(0xe068))
		_, _ = conn.Write(glblock.Empty(0xe069))
		sendE02D(conn, sess, peer, fmt.Sprintf("JOIN room=%d", room.ID), true)

	case 0xe057:
		_, _ = conn.Write(glblock.Empty(0xe058))

	case 0xe067:
		if !config.PushGSOnStartGame {
			return
		}
		_, _ = conn.Write(glblock.Empty(0xe068))
		_, _ = conn.Write(glblock.Empty(0xe069))
		sendE02D(conn, sess, peer, "start-game", true)

	case 0xe02e:
		_, _ = conn.Write(glblock.Empty(0xe02f))
		session.LeaveRoom(sess, "quit_e02e")
		sess.CustomMatchArmed = false
		sess.CustomRoomIntent = false
		sess.ClearCustomRoomSearch()
		fmt.Printf(" [LOBBY#%d SENT] 0xe02f quit-room\n", peer)
		if (config.ServerKitabe || config.ServerTalent) && sess.RequestTradeGSRebootstrap() {
			sendE02D(conn, sess, peer, "TRADE-GS after quit-room", false)
		}

	case 0xe076:
		_, _ = conn.Write(glblock.Empty(0xe077))
		fmt.Printf(" [LOBBY#%d SENT] 0xe077 prestart\n", peer)

	default:
		fmt.Printf(" [LOBBY#%d] unhandled op=0x%04x\n", peer, op)
	}
}

func lobbyUsers(room *session.Room) []glblock.LobbyUser {
	var out []glblock.LobbyUser
	for _, m := range room.SnapshotMembers() {
		if m == nil || m.Account == nil {
			continue
		}
		out = append(out, glblock.LobbyUser{
			Nick:   accountNick(m.Account),
			UserID: accountWireID(m.Account),
			Seat:   byte(m.Seat),
		})
	}
	return out
}

func searchCustomRooms() []glblock.CustomRoom {
	snapshots := session.ListOpenRooms()
	rooms := make([]glblock.CustomRoom, 0, len(snapshots))
	for _, room := range snapshots {
		rooms = append(rooms, glblock.CustomRoom{
			ID: int32(room.ID), Name: []byte(room.Name),
			Flag1012: room.Flag1012, Flag1013: room.Flag1013,
			Capacity: uint16(room.Capacity), Members: int32(room.Members),
			Param103E: room.Param103E, Flag1011: room.Flag1011,
			JSON1014: room.JSON1014, Str1040: []byte(room.Str1040),
			Int1041: room.Int1041, Str104B: []byte(room.Str104B),
		})
	}
	return rooms
}

func connGateway(conn net.Conn) string {
	if conn == nil || conn.LocalAddr() == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(conn.LocalAddr().String())
	if err != nil {
		host = conn.LocalAddr().String()
	}
	host = strings.Trim(host, "[]")
	if config.IsPlayerGateway(host) {
		return host
	}
	return ""
}

func accountNick(acc *accounts.Account) string {
	if acc == nil {
		return ""
	}
	if nick := strings.TrimSpace(acc.Nickname); nick != "" {
		return nick
	}
	return strings.TrimSpace(acc.Username)
}

func accountWireID(acc *accounts.Account) string {
	if acc == nil {
		return ""
	}
	if acc.AccountID != "" {
		return acc.AccountID
	}
	return fmt.Sprintf("%d", acc.UserID)
}

func sendE02D(conn net.Conn, sess *session.Session, peer int, tag string, custom bool) {
	tok := sess.EnsureToken()
	rid := sess.RoomID
	if rid <= 0 {
		rid = config.DefaultRoomID
	}
	gsip := sess.Gateway()
	if custom {
		sess.CancelTradeGSRebootstrap()
		sess.CustomMatchArmed = true
	}
	sess.AwaitingGS = true
	pkt := glblock.TeamPlayGameInfo([]byte(tok), int32(rid), gsip, uint16(config.GSPort))
	_, _ = conn.Write(pkt)
	fmt.Printf(" [LOBBY#%d SENT] *e02d %s token=%s room=%d gs=%s custom=%v (%dB)\n",
		peer, tag, tok, rid, gsip, custom, len(pkt))
}

func parseLoginUser(pkt []byte) string {
	for _, c := range glblock.IterChildren(pkt) {
		if c.TypeID == 0x0300 && len(c.Value) > 0 {
			return string(c.Value)
		}
	}
	for _, c := range glblock.IterChildren(pkt) {
		if c.Type == glblock.TypeString && len(c.Value) > 0 {
			s := strings.TrimSpace(string(c.Value))
			if accounts.Get(s) != nil {
				return s
			}
			lower := strings.ToLower(s)
			if strings.HasPrefix(lower, "gllive:") || strings.HasPrefix(lower, "glive:") || strings.HasPrefix(lower, "hoc:") {
				return s
			}
		}
	}
	return ""
}
