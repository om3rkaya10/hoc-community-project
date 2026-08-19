package lobby

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"hoc-server/internal/accounts"
	"hoc-server/internal/session"
	"hoc-server/internal/wire/glblock"
)

func TestM2SearchPrestartAndQuitHandlers(t *testing.T) {
	hostConn := &recordConn{}
	searchConn := &recordConn{}
	host := session.Create(hostConn, "host")
	searcher := session.Create(searchConn, "searcher")
	t.Cleanup(func() {
		session.Destroy(searchConn)
		session.Destroy(hostConn)
	})
	host.BindAccount(testAccount("enterpries1", "Enterpries", "1000001", 1000001))
	searcher.BindAccount(testAccount("enterpries2", "Enterpries2", "1000002", 1000002))
	room := session.CreateRoom(host, session.RoomOptions{
		Name: "M2 Visible", Capacity: 10, Flag1012: 1, JSON1014: []byte("{}"),
	})
	searcher.HeroesDelivered = true // keep this handler test to one e03b write

	handleOpcode(searchConn, searcher, searcher.Peer, 0xe03a, glblock.Empty(0xe03a))
	writes := searchConn.takeWrites()
	if len(writes) != 1 || glblock.Opcode(writes[0]) != 0xe03b {
		t.Fatalf("search writes=%d op=%#x", len(writes), firstOpcode(writes))
	}
	outer := glblock.IterChildren(writes[0])
	if len(outer) != 1 || outer[0].TypeID != 0x103A || len(outer[0].Value) == 0 {
		t.Fatalf("search room list missing: %v", outer)
	}

	handleOpcode(searchConn, searcher, searcher.Peer, 0xe076, glblock.Empty(0xe076))
	writes = searchConn.takeWrites()
	if len(writes) != 1 || glblock.Opcode(writes[0]) != 0xe077 {
		t.Fatalf("prestart writes=%d op=%#x", len(writes), firstOpcode(writes))
	}

	handleOpcode(hostConn, host, host.Peer, 0xe02e, glblock.Empty(0xe02e))
	writes = hostConn.takeWrites()
	if len(writes) != 2 || !hasOpcode(writes, 0xe02f) || !hasOpcode(writes, 0xe02d) {
		t.Fatalf("quit writes=%d e02f=%v trade-e02d=%v",
			len(writes), hasOpcode(writes, 0xe02f), hasOpcode(writes, 0xe02d))
	}
	if host.Room != nil || session.GetRoom(room.ID) != nil {
		t.Fatalf("quit did not remove room hostRoom=%v registry=%v", host.Room, session.GetRoom(room.ID))
	}
}

func TestQuitRoomDefersTradeGSUntilCustomSocketCloses(t *testing.T) {
	lobbyConn := &recordConn{}
	gsConn := &recordConn{}
	sess := session.Create(lobbyConn, "custom-race")
	t.Cleanup(func() { session.Destroy(lobbyConn) })
	sess.BindAccount(testAccount("enterpries2", "Enterpries2", "1000002", 1000002))
	session.CreateRoom(sess, session.RoomOptions{Name: "race", Capacity: 10})
	sess.HeroesDelivered = true
	sess.AttachGS(gsConn)

	handleOpcode(lobbyConn, sess, sess.Peer, 0xe02e, glblock.Empty(0xe02e))
	writes := lobbyConn.takeWrites()
	if len(writes) != 1 || !hasOpcode(writes, 0xe02f) || hasOpcode(writes, 0xe02d) {
		t.Fatalf("live custom GS should defer rebootstrap writes=%d e02f=%v e02d=%v",
			len(writes), hasOpcode(writes, 0xe02f), hasOpcode(writes, 0xe02d))
	}

	sess.DetachGS(gsConn)
	handleOpcode(lobbyConn, sess, sess.Peer, 0x1205, glblock.Empty(0x1205))
	writes = lobbyConn.takeWrites()
	if len(writes) != 2 || !hasOpcode(writes, 0x2105) || !hasOpcode(writes, 0xe02d) {
		t.Fatalf("keepalive did not restore trade GS writes=%d 2105=%v e02d=%v",
			len(writes), hasOpcode(writes, 0x2105), hasOpcode(writes, 0xe02d))
	}
	if !sess.AwaitingGS {
		t.Fatal("trade GS e02d did not arm AwaitingGS")
	}
}

func TestM2JoinReplyRotatesToken(t *testing.T) {
	conn := &recordConn{}
	sess := session.Create(conn, "client")
	t.Cleanup(func() { session.Destroy(conn) })
	sess.BindAccount(testAccount("enterpries1", "Enterpries", "1000001", 1000001))

	handleOpcode(conn, sess, sess.Peer, 0x1206, glblock.Empty(0x1206))
	first := sess.Token
	firstWrites := conn.takeWrites()
	handleOpcode(conn, sess, sess.Peer, 0x1206, glblock.Empty(0x1206))
	second := sess.Token
	secondWrites := conn.takeWrites()
	if first == second || len(firstWrites) != 1 || len(secondWrites) != 1 {
		t.Fatalf("token rotation first=%q second=%q writes=%d/%d", first, second, len(firstWrites), len(secondWrites))
	}
	if glblock.Opcode(firstWrites[0]) != 0x2106 || glblock.Opcode(secondWrites[0]) != 0x2106 {
		t.Fatalf("join reply opcode=%#x/%#x", glblock.Opcode(firstWrites[0]), glblock.Opcode(secondWrites[0]))
	}
	if old, _ := session.ResolveGS(first, ""); old != nil {
		t.Fatalf("old token still resolves")
	}
	if current, how := session.ResolveGS(second, ""); current != sess || how != "token" {
		t.Fatalf("new token resolve=%v how=%q", current, how)
	}
}

func TestM2LeaveRecreateClearsBusyGate(t *testing.T) {
	conn := &recordConn{}
	sess := session.Create(conn, "host")
	t.Cleanup(func() { session.Destroy(conn) })
	sess.BindAccount(testAccount("enterpries1", "Enterpries", "1000001", 1000001))

	handleOpcode(conn, sess, sess.Peer, 0xe038, glblock.Empty(0xe038))
	firstRoom := sess.Room
	if firstRoom == nil || !hasOpcode(conn.takeWrites(), 0xe039) {
		t.Fatal("first create did not return e039")
	}

	sess.HeroesDelivered = true
	sess.CustomMatchArmed = true
	handleOpcode(conn, sess, sess.Peer, 0xe02e, glblock.Empty(0xe02e))
	if !hasOpcode(conn.takeWrites(), 0xe02f) {
		t.Fatal("quit did not return e02f")
	}
	if sess.Room != nil || session.GetRoom(firstRoom.ID) != nil {
		t.Fatal("quit left a ghost room")
	}
	if sess.CustomRoomIntent || sess.CustomMatchArmed {
		t.Fatalf("quit left custom latch intent=%v armed=%v", sess.CustomRoomIntent, sess.CustomMatchArmed)
	}

	handleOpcode(conn, sess, sess.Peer, 0x1208, glblock.Empty(0x1208))
	if !hasOpcode(conn.takeWrites(), 0x2108) {
		t.Fatal("leave did not return 2108")
	}

	handleOpcode(conn, sess, sess.Peer, 0x120b, glblock.Empty(0x120b))
	writes := conn.takeWrites()
	if len(writes) != 1 || glblock.Opcode(writes[0]) != 0x210b {
		t.Fatalf("relay cancel writes=%d op=%#x", len(writes), firstOpcode(writes))
	}
	children := glblock.IterChildren(writes[0])
	if len(children) != 1 || children[0].TypeID != 0xFF00 || len(children[0].Value) != 4 {
		t.Fatalf("relay cancel child=%v", children)
	}
	if got := binary.BigEndian.Uint32(children[0].Value); got != 30002 {
		t.Fatalf("relay cancel code=%d", got)
	}

	handleOpcode(conn, sess, sess.Peer, 0xe03a, glblock.Empty(0xe03a))
	writes = conn.takeWrites()
	if len(writes) != 1 || glblock.Opcode(writes[0]) != 0xe03b {
		t.Fatalf("empty search writes=%d op=%#x", len(writes), firstOpcode(writes))
	}
	outer := glblock.IterChildren(writes[0])
	if len(outer) != 1 || outer[0].TypeID != 0x103A || len(outer[0].Value) != 0 {
		t.Fatalf("search still contains room: %v", outer)
	}

	handleOpcode(conn, sess, sess.Peer, 0xe038, glblock.Empty(0xe038))
	if sess.Room == nil || sess.Room.ID == firstRoom.ID || !hasOpcode(conn.takeWrites(), 0xe039) {
		t.Fatal("second create did not return a fresh e039")
	}
}

func TestHostQuitPromotesSurvivorKeepsRoom(t *testing.T) {
	hostConn := &recordConn{}
	guestConn := &recordConn{}
	guestGSConn := &recordConn{}
	host := session.Create(hostConn, "host")
	guest := session.Create(guestConn, "guest")
	t.Cleanup(func() {
		session.Destroy(guestConn)
		session.Destroy(hostConn)
	})
	host.BindAccount(testAccount("enterpries1", "Enterpries", "1000001", 1000001))
	guest.BindAccount(testAccount("enterpries2", "Enterpries2", "1000002", 1000002))
	room := session.CreateRoom(host, session.RoomOptions{Name: "teardown", Capacity: 10})
	if joined, why := session.JoinRoom(guest, room.ID, nil); joined != room || why != "ok" {
		t.Fatalf("join room=%v why=%q", joined, why)
	}
	guest.CustomRoomIntent = true
	guest.CustomMatchArmed = true
	guest.HeroesDelivered = true
	guest.AttachGS(guestGSConn)

	handleOpcode(hostConn, host, host.Peer, 0xe02e, glblock.Empty(0xe02e))
	if writes := guestConn.takeWrites(); len(writes) != 0 {
		t.Fatalf("host quit must not e02f-kick survivor writes=%d op=%#x", len(writes), firstOpcode(writes))
	}
	if session.GetRoom(room.ID) != room || room.Host != guest || guest.Room != room {
		t.Fatalf("survivor not promoted room=%v host=%v guestRoom=%v",
			session.GetRoom(room.ID), room.Host, guest.Room)
	}
	if !guest.CustomRoomIntent || !guest.CustomMatchArmed {
		t.Fatalf("survivor latches wiped intent=%v armed=%v",
			guest.CustomRoomIntent, guest.CustomMatchArmed)
	}
	if host.Room != nil {
		t.Fatalf("host still linked room=%v", host.Room)
	}
}

func TestRecreateJoinCorrelatesZeroToLastSingleSearchResult(t *testing.T) {
	hostConn := &recordConn{}
	guestConn := &recordConn{}
	host := session.Create(hostConn, "host")
	guest := session.Create(guestConn, "guest")
	t.Cleanup(func() {
		session.Destroy(guestConn)
		session.Destroy(hostConn)
	})
	host.BindAccount(testAccount("enterpries1", "Enterpries", "1000001", 1000001))
	guest.BindAccount(testAccount("enterpries2", "Enterpries2", "1000002", 1000002))
	guest.HeroesDelivered = true

	room := session.CreateRoom(host, session.RoomOptions{Name: "recreated", Capacity: 10, Flag1012: 1})
	handleOpcode(guestConn, guest, guest.Peer, 0xe03a, glblock.Empty(0xe03a))
	if !hasOpcode(guestConn.takeWrites(), 0xe03b) {
		t.Fatal("search did not return e03b")
	}

	zeroJoin := glblock.PackPacket(0xe03c, []glblock.Child{
		{TypeID: 0x100F, Type: glblock.TypeInt, Value: glblock.IntBE(0)},
		{TypeID: 0x100A, Type: glblock.TypeChar, Value: glblock.Char(0)},
	})
	handleOpcode(guestConn, guest, guest.Peer, 0xe03c, zeroJoin)
	writes := guestConn.takeWrites()
	if guest.Room != room || guest.RoomID != room.ID || guest.Seat != 1 {
		t.Fatalf("zero-id join did not use advertised room room=%v id=%d seat=%d", guest.Room, guest.RoomID, guest.Seat)
	}
	if !hasOpcode(writes, 0xe03d) || !hasOpcode(writes, 0xe02d) {
		t.Fatalf("correlated join writes=%d e03d=%v e02d=%v", len(writes), hasOpcode(writes, 0xe03d), hasOpcode(writes, 0xe02d))
	}
}

func TestUnknownLobbyUsersAutoCreateDistinctRoster(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.json")
	if err := os.WriteFile(path, []byte(`{"accounts":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := accounts.Load(path); err != nil {
		t.Fatal(err)
	}

	hostConn := &recordConn{}
	guestConn := &recordConn{}
	host := session.Create(hostConn, "host")
	guest := session.Create(guestConn, "guest")
	t.Cleanup(func() {
		session.Destroy(guestConn)
		session.Destroy(hostConn)
	})

	login := func(conn *recordConn, sess *session.Session, name string) {
		t.Helper()
		pkt := glblock.PackPacket(0x1203, []glblock.Child{{
			TypeID: 0x0300, Type: glblock.TypeString, Value: []byte("gllive:" + name),
		}})
		handleOpcode(conn, sess, sess.Peer, 0x1203, pkt)
		writes := conn.takeWrites()
		if len(writes) != 2 || glblock.Opcode(writes[0]) != 0x2103 {
			t.Fatalf("%s login writes=%d first=%#x", name, len(writes), firstOpcode(writes))
		}
	}
	login(hostConn, host, "BlueFox")
	login(guestConn, guest, "RedWolf")

	if host.Account == nil || guest.Account == nil || host.Account == guest.Account {
		t.Fatalf("accounts collapsed host=%v guest=%v", host.Account, guest.Account)
	}
	if host.Username != "bluefox" || host.Account.Nickname != "BlueFox" ||
		guest.Username != "redwolf" || guest.Account.Nickname != "RedWolf" {
		t.Fatalf("wrong identities host=%+v guest=%+v", host.Account, guest.Account)
	}
	if host.Account.UserID == guest.Account.UserID {
		t.Fatalf("duplicate user id=%d", host.Account.UserID)
	}

	room := session.CreateRoom(host, session.RoomOptions{Name: "nick-room", Capacity: 10})
	if joined, why := session.JoinRoom(guest, room.ID, nil); joined != room || why != "ok" {
		t.Fatalf("join room=%v why=%q", joined, why)
	}
	roster := glblock.JoinCustomRoster(lobbyUsers(room))
	if !bytes.Contains(roster, []byte("BlueFox")) || !bytes.Contains(roster, []byte("RedWolf")) {
		t.Fatalf("custom roster missing nicknames: %x", roster)
	}
	if bytes.Contains(roster, []byte("enterpries1")) {
		t.Fatalf("custom roster leaked seed identity: %x", roster)
	}
}

func testAccount(username, nick, accountID string, userID int64) *accounts.Account {
	return &accounts.Account{
		Username: username, Nickname: nick, AccountID: accountID, UserID: userID,
		Gateway: "172.16.42.2",
	}
}

func firstOpcode(writes [][]byte) uint16 {
	if len(writes) == 0 {
		return 0
	}
	return glblock.Opcode(writes[0])
}

func hasOpcode(writes [][]byte, want uint16) bool {
	for _, write := range writes {
		if glblock.Opcode(write) == want {
			return true
		}
	}
	return false
}

type recordConn struct {
	mu     sync.Mutex
	writes [][]byte
}

func (c *recordConn) Read([]byte) (int, error) { return 0, io.EOF }
func (c *recordConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.writes = append(c.writes, append([]byte(nil), p...))
	c.mu.Unlock()
	return len(p), nil
}
func (c *recordConn) Close() error                     { return nil }
func (c *recordConn) LocalAddr() net.Addr              { return testAddr("local") }
func (c *recordConn) RemoteAddr() net.Addr             { return testAddr("remote") }
func (c *recordConn) SetDeadline(time.Time) error      { return nil }
func (c *recordConn) SetReadDeadline(time.Time) error  { return nil }
func (c *recordConn) SetWriteDeadline(time.Time) error { return nil }
func (c *recordConn) takeWrites() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := append([][]byte(nil), c.writes...)
	c.writes = nil
	return out
}

type testAddr string

func (a testAddr) Network() string { return "test" }
func (a testAddr) String() string  { return string(a) }
