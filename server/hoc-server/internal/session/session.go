package session

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"hoc-server/internal/accounts"
	"hoc-server/internal/config"
)

var tokenSeq uint64

type Session struct {
	mu sync.Mutex

	Peer     int
	Conn     net.Conn
	Addr     string
	Username string
	Account  *accounts.Account
	Token    string
	GUID     string

	RoomID           int
	TskCID           int
	Seat             int // 0-based lobby seat
	SeatReady        bool
	ReadyCancelled   bool
	SeatVacateSent   bool
	CustomMatchArmed bool
	CustomRoomIntent bool
	AwaitingGS       bool
	HeroesDelivered  bool
	ProfileBootstrap bool

	SeatHeroID      int
	SeatSkinID      int
	SeatSpell1      int
	SeatSpell2      int
	MatchPlaying    bool
	MatchLoading    bool
	MatchHold       bool
	MatchHoldUntil  time.Time
	MatchHoldRoomID int
	MatchHoldSeat   int
	MatchHoldHero   int
	// MatchResumeGraceUntil freezes the shared clock after ReLoginAck until
	// this deadline (timer only — not cleared by op3 heartbeats).
	MatchResumeGraceUntil time.Time
	// MatchAwaitResumePing freezes the shared clock after soft ReLoginAck
	// until the rejoiner's first op3 — avoids pumping op11 into a half-ready
	// soft path (LIVE: Ack→immediate EOF / syn ratchet flap).
	MatchAwaitResumePing bool
	// LastGSActivity is the last inbound GS packet time for stall-freeze.
	LastGSActivity time.Time
	// SoftResumeFails counts Ack→quick-EOF cycles; used to stop Relogin flap.
	SoftResumeFails int
	softResumeAt    time.Time

	GSConns []net.Conn
	Room    *Room

	matchHoldGeneration   uint64
	matchResumeGeneration uint64
	matchResumePending    net.Conn

	// A custom-seat GS normally closes before the client returns to the main
	// menu. Kitabe/talent trade needs a replacement non-custom GS; keep this
	// one-shot request armed across the small GS-EOF/lobby-e02e race.
	tradeGSRebootstrapPending bool

	// The custom-room client can emit room_id=0 after a leave/recreate cycle
	// even though it just received a valid one-row e03b result. Keep the exact
	// per-session advertisement so lobby can correlate that one request without
	// guessing from the global room registry.
	customSearchRoomIDs []int
}

type Room struct {
	mu sync.Mutex

	ID        int
	Name      string
	Host      *Session
	Members   []*Session
	Capacity  int
	State     string // open | starting | match | closed
	CreatedAt time.Time

	Flag1012  byte
	Flag1013  byte
	Param103E int32
	Flag1011  byte
	JSON1014  []byte
	Str1040   string
	Int1041   int32
	Str104B   string

	// MapName is the raw "map" value from the create-time JSON1014
	// ("3V3" / "5V5" / "5V5_UR"). It is stored as the client sent it and
	// translated to GAME_MODE (GSI+4) at LoadMap time via
	// config.GameModeForMapName; an empty value means "not a custom room"
	// and falls back to the default. Storing the string rather than the
	// numeric mode avoids an ambiguous zero, since GAME_MODE 0 (5V5) is
	// itself a valid map.
	MapName string

	// CustomOpts holds the advanced room options ("başlangıç altını",
	// "başlangıç seviyesi", passive gold/xp, and the toggles) parsed from
	// the same create-time JSON1014. They are replayed into every LoadMap
	// so the match actually starts with what the host picked. Previously the
	// corresponding 21-byte LoadMap block was all zero, forcing zero gold/rates
	// instead of carrying the client's values and -1 "map default" sentinels.
	CustomOpts config.CustomRoomOptions

	MatchClockArmed bool
	MatchSFrame     int
	MatchSynNext    int
	MatchFramesSent int
	// Bounded exact op7/op11 history for a mid-match rejoiner. The room
	// clock never rewinds; a reconnecting peer replays its missed syn range.
	MatchReplay []MatchReplayPacket
	MatchSeed   int
	TskCID      int
	// MatchStallIgnoreUntil disables per-peer stall-freeze after a successful
	// soft resume so a survivor that went quiet during the pause cannot keep
	// the whole room locked forever.
	MatchStallIgnoreUntil time.Time

	// wireMu serializes the actual op7/op11 socket writes for this room.
	//
	// op7 and op11 share one syn counter and must not overtake each other on
	// the wire (sequenceID wrong UP/TP -> Dev|3004, AGENTS4 §11.7). That was
	// originally guaranteed by doing the writes under mu, but a blocking write
	// to a congested WAN peer then froze all room state for everyone
	// (AGENTS5 §2.3.5). Ordering now lives on this dedicated lock, so writes
	// stay serialized while mu is released immediately.
	wireMu sync.Mutex
}

type MatchReplayPacket struct {
	Syn         int
	Opcode, Sub uint16
	Slot        byte
	Body        []byte
}

const MatchReplayLimit = 8192

// AppendMatchReplayLocked and MatchReplayFromLocked require room.mu.
func (r *Room) AppendMatchReplayLocked(p MatchReplayPacket) {
	p.Body = append([]byte(nil), p.Body...)
	r.MatchReplay = append(r.MatchReplay, p)
	if drop := len(r.MatchReplay) - MatchReplayLimit; drop > 0 {
		copy(r.MatchReplay, r.MatchReplay[drop:])
		r.MatchReplay = r.MatchReplay[:MatchReplayLimit]
	}
}

func (r *Room) MatchReplayFromLocked(syn int) ([]MatchReplayPacket, int, bool) {
	if len(r.MatchReplay) == 0 {
		return nil, 0, false
	}
	oldest := r.MatchReplay[0].Syn
	if syn < oldest {
		return nil, oldest, false
	}
	out := make([]MatchReplayPacket, 0, len(r.MatchReplay))
	for _, p := range r.MatchReplay {
		if p.Syn >= syn {
			p.Body = append([]byte(nil), p.Body...)
			out = append(out, p)
		}
	}
	return out, oldest, true
}

// WireLock/WireUnlock serialize outbound in-match packet writes for the room.
// Never take mu while holding wireMu — always mu first, then wireMu.
func (r *Room) WireLock()   { r.wireMu.Lock() }
func (r *Room) WireUnlock() { r.wireMu.Unlock() }

type RoomOptions struct {
	Name      string
	Capacity  int
	Flag1012  byte
	Flag1013  byte
	Param103E int32
	JSON1014  []byte
}

// RoomLeaveEvent is emitted while the old room/seat links are still intact.
// Wire packages use it to paint a real peer vacate and, on pre-match host
// teardown, return survivors from MatchSetting before domain state is reset.
type RoomLeaveEvent struct {
	Room       *Room
	Leaver     *Session
	Survivors  []*Session
	LeaverSeat int
	LeaverNick string
	TskCID     int
	WasHost    bool
	Destroying bool
	InMatch    bool
	Reason     string
}

type RoomLeaveObserver func(RoomLeaveEvent)

type RoomSnapshot struct {
	ID        int
	Name      string
	Capacity  int
	Members   int
	Flag1012  byte
	Flag1013  byte
	Param103E int32
	Flag1011  byte
	JSON1014  []byte
	Str1040   string
	Int1041   int32
	Str104B   string
}

type MatchHoldClaim struct {
	Room   *Room
	RoomID int
	Seat   int
	Hero   int
	TskCID int
	Until  time.Time
}

var (
	sessMu  sync.RWMutex
	byConn  = map[net.Conn]*Session{}
	byToken = map[string]*Session{}
	peerSeq int64

	roomMu   sync.Mutex
	rooms    = map[int]*Room{}
	nextRoom = config.DefaultRoomID

	roomLeaveObserversMu sync.RWMutex
	roomLeaveObservers   []RoomLeaveObserver
)

func RegisterRoomLeaveObserver(observer RoomLeaveObserver) {
	if observer == nil {
		return
	}
	roomLeaveObserversMu.Lock()
	roomLeaveObservers = append(roomLeaveObservers, observer)
	roomLeaveObserversMu.Unlock()
}

func publishRoomLeave(event RoomLeaveEvent) {
	roomLeaveObserversMu.RLock()
	observers := append([]RoomLeaveObserver(nil), roomLeaveObservers...)
	roomLeaveObserversMu.RUnlock()
	for _, observer := range observers {
		observer(event)
	}
}

func Create(conn net.Conn, addr string) *Session {
	p := int(atomic.AddInt64(&peerSeq, 1))
	s := &Session{
		Peer:   p,
		Conn:   conn,
		Addr:   addr,
		RoomID: config.DefaultRoomID,
		TskCID: config.DefaultTskCID,
	}
	sessMu.Lock()
	byConn[conn] = s
	sessMu.Unlock()
	return s
}

func Get(conn net.Conn) *Session {
	sessMu.RLock()
	defer sessMu.RUnlock()
	return byConn[conn]
}

func Destroy(conn net.Conn) {
	sessMu.Lock()
	s := byConn[conn]
	delete(byConn, conn)
	sessMu.Unlock()
	if s == nil {
		return
	}
	// Mid-match / reconnect-hold: lobby TCP can die while the client is still
	// soft-rejoining GS. LeaveRoom here used to push 0x3003 to the survivor and
	// crash them (LIVE 2026-08-09). Keep seat + token; only explicit e02e /
	// hold-timeout / GS leave should vacate.
	if s.ShouldPreserveMatchOnLobbyDrop() {
		s.mu.Lock()
		if s.Conn == conn {
			s.Conn = nil
		}
		user := s.Username
		s.mu.Unlock()
		fmt.Printf(" [MATCH] lobby EOF preserved (in-match/hold) user=%q\n", user)
		s.DetachGS(conn) // no-op unless this conn was wrongly tracked as GS
		return
	}
	sessMu.Lock()
	if s.Token != "" {
		delete(byToken, s.Token)
	}
	sessMu.Unlock()
	LeaveRoom(s, "lobby_disconnect")
	s.DetachGS(conn)
}

// ShouldPreserveMatchOnLobbyDrop is true while the session still belongs to a
// live/held match seat. Lobby transport loss must not vacate that seat.
func (s *Session) ShouldPreserveMatchOnLobbyDrop() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.MatchHold || s.MatchPlaying || s.MatchLoading {
		return true
	}
	if !s.MatchResumeGraceUntil.IsZero() && time.Now().Before(s.MatchResumeGraceUntil) {
		return true
	}
	if s.Room != nil && s.Room.State == "match" {
		return true
	}
	return false
}

func (s *Session) EnsureToken() string {
	return s.token(false)
}

func (s *Session) AllocToken() string {
	return s.token(true)
}

func (s *Session) token(fresh bool) string {
	s.mu.Lock()
	if !fresh && s.Token != "" {
		tok := s.Token
		s.mu.Unlock()
		return tok
	}
	old := s.Token
	n := atomic.AddUint64(&tokenSeq, 1)
	tok := fmt.Sprintf("mock_s%04d", n%10000)
	s.Token = tok
	s.mu.Unlock()

	sessMu.Lock()
	if old != "" {
		delete(byToken, old)
	}
	byToken[tok] = s
	sessMu.Unlock()
	return tok
}

func (s *Session) BindAccount(a *accounts.Account) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Account = a
	if a != nil {
		s.Username = a.Username
		if s.GUID == "" {
			s.GUID = fmt.Sprintf("gllive:%s", a.Username)
		}
	}
}

func (s *Session) Gateway() string {
	return accounts.GatewayFor(s.Account)
}

func (s *Session) AttachGS(c net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attachGSLocked(c)
}

func (s *Session) attachGSLocked(c net.Conn) {
	for _, x := range s.GSConns {
		if x == c {
			return
		}
	}
	s.GSConns = append(s.GSConns, c)
	s.AwaitingGS = false
	s.tradeGSRebootstrapPending = false
}

func (s *Session) DetachGS(c net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.detachGSLocked(c)
}

func (s *Session) detachGSLocked(c net.Conn) {
	out := s.GSConns[:0]
	for _, x := range s.GSConns {
		if x != c {
			out = append(out, x)
		}
	}
	s.GSConns = out
}

// DetachGSForDisconnect removes one GS transport and, only when it was the
// final transport for a live match, converts the session into a reserved-seat
// reconnect hold. Explicit GS/lobby leave paths clear the playing latch before
// reaching this method and therefore never enter a hold.
func (s *Session) DetachGSForDisconnect(c net.Conn, transientMatch, enabled bool, ttl time.Duration) (held, last bool, room *Room) {
	if s == nil {
		return false, true, nil
	}
	if ttl <= 0 {
		enabled = false
	}
	now := time.Now()
	s.mu.Lock()
	resumePending := s.matchResumePending == c
	s.detachGSLocked(c)
	room = s.Room
	last = len(s.GSConns) == 0
	if !last {
		s.mu.Unlock()
		return false, false, room
	}
	if resumePending {
		s.matchResumePending = nil
	}
	eligible := enabled && room != nil && (transientMatch || resumePending) &&
		(s.MatchPlaying || resumePending)
	if eligible {
		s.matchHoldGeneration++
		generation := s.matchHoldGeneration
		s.MatchHold = true
		s.MatchHoldUntil = now.Add(ttl)
		s.MatchHoldRoomID = room.ID
		s.MatchHoldSeat = s.Seat
		s.MatchHoldHero = s.SeatHeroID
		s.MatchResumeGraceUntil = time.Time{}
		s.MatchAwaitResumePing = false
		s.MatchPlaying = false
		s.MatchLoading = false
		s.AwaitingGS = true
		s.mu.Unlock()
		time.AfterFunc(ttl, func() { expireMatchHold(s, generation) })
		return true, true, room
	}
	clearMatchHoldLocked(s)
	s.MatchPlaying = false
	s.MatchLoading = false
	s.mu.Unlock()
	return false, true, room
}

func clearMatchHoldLocked(s *Session) {
	s.matchHoldGeneration++
	s.MatchHold = false
	s.MatchHoldUntil = time.Time{}
	s.MatchHoldRoomID = 0
	s.MatchHoldSeat = 0
	s.MatchHoldHero = 0
	s.matchResumePending = nil
	// Grace is armed separately by CompleteMatchResume after hold clear.
}

func expireMatchHold(s *Session, generation uint64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if !s.MatchHold || s.matchHoldGeneration != generation {
		s.mu.Unlock()
		return
	}
	if delay := time.Until(s.MatchHoldUntil); delay > 0 {
		s.mu.Unlock()
		time.AfterFunc(delay, func() { expireMatchHold(s, generation) })
		return
	}
	roomID := s.MatchHoldRoomID
	clearMatchHoldLocked(s)
	s.AwaitingGS = false
	s.MatchPlaying = false
	s.MatchLoading = false
	s.mu.Unlock()
	fmt.Printf(" [MATCH] reconnect hold expired user=%q room=%d\n", s.Username, roomID)
	LeaveRoom(s, "reconnect_hold_timeout")
}

func (s *Session) SnapshotGSConns() []net.Conn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]net.Conn(nil), s.GSConns...)
}

// RequestTradeGSRebootstrap arms the one-shot main-menu GS restore and claims
// it immediately when the custom GS has already disconnected.
func (s *Session) RequestTradeGSRebootstrap() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tradeGSRebootstrapPending = true
	return s.claimTradeGSRebootstrapLocked()
}

// ClaimTradeGSRebootstrap is the keepalive-side fallback for the case where
// lobby e02e arrives just before the custom GS socket detaches.
func (s *Session) ClaimTradeGSRebootstrap() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.claimTradeGSRebootstrapLocked()
}

func (s *Session) CancelTradeGSRebootstrap() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.tradeGSRebootstrapPending = false
	s.mu.Unlock()
}

func (s *Session) claimTradeGSRebootstrapLocked() bool {
	if !s.tradeGSRebootstrapPending || len(s.GSConns) != 0 || s.AwaitingGS ||
		s.CustomMatchArmed || s.Room != nil {
		return false
	}
	s.tradeGSRebootstrapPending = false
	return true
}

func (s *Session) SetMatchPlaying(v bool) {
	s.mu.Lock()
	s.MatchPlaying = v
	s.mu.Unlock()
}

func (s *Session) SetMatchLoading(v bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.MatchLoading = v
	s.mu.Unlock()
}

func (s *Session) IsMatchLoading() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.MatchLoading
}

// MarkGSLeave returns server-side match state to idle after the client's
// one-way kGS_LeaveRoom (op9/sub 0x3001) notification.
func (s *Session) MarkGSLeave() {
	s.mu.Lock()
	clearMatchHoldLocked(s)
	s.AwaitingGS = false
	s.CustomMatchArmed = false
	s.MatchLoading = false
	s.MatchPlaying = false
	s.mu.Unlock()
}

func (s *Session) IsMatchPlaying() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.MatchPlaying
}

func (s *Session) IsMatchHeld() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.MatchHold
}

func (s *Session) CurrentRoom() *Room {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Room
}

func (s *Session) RememberCustomRoomSearch(roomIDs []int) {
	s.mu.Lock()
	s.customSearchRoomIDs = append(s.customSearchRoomIDs[:0], roomIDs...)
	s.mu.Unlock()
}

// CorrelateCustomRoomJoin resolves the client's recreate-only room_id=0 bug
// against the exact last e03b response sent to this session. It is deliberately
// one-shot and refuses to choose when the response contained zero or many rooms.
func (s *Session) CorrelateCustomRoomJoin(roomID int) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if roomID != 0 || s.Room != nil || !s.CustomRoomIntent || len(s.customSearchRoomIDs) != 1 {
		return roomID, false
	}
	resolved := s.customSearchRoomIDs[0]
	s.customSearchRoomIDs = nil
	return resolved, true
}

func (s *Session) ClearCustomRoomSearch() {
	s.mu.Lock()
	s.customSearchRoomIDs = nil
	s.mu.Unlock()
}

func ResolveGS(token, username string) (*Session, string) {
	sessMu.RLock()
	defer sessMu.RUnlock()
	if token != "" {
		if s := byToken[token]; s != nil {
			return s, "token"
		}
	}
	if username != "" {
		un := accounts.Norm(username)
		for _, s := range byConn {
			if s != nil && accounts.Norm(s.Username) == un {
				return s, "username"
			}
		}
	}
	return nil, "none"
}

// ClaimMatchHold resolves the reconnect identity carried by ReLoginReq and
// exclusively claims the exact reserved room/seat. Lookup is by room membership
// (not lobby byConn) so a lobby EOF during hold still finds the seat.
// The connection remains non-playing until CompleteMatchResume after ReLoginAck.
func ClaimMatchHold(roomName, guid string, conn net.Conn, now time.Time) (*Session, MatchHoldClaim, string) {
	if roomName == "" || guid == "" || conn == nil {
		return nil, MatchHoldClaim{}, "invalid"
	}
	var roomID int
	if _, err := fmt.Sscanf(roomName, "hoc_r%d", &roomID); err != nil || roomID <= 0 {
		return nil, MatchHoldClaim{}, "bad-room"
	}
	room := GetRoom(roomID)
	if room == nil {
		return nil, MatchHoldClaim{}, "no-room"
	}

	room.mu.Lock()
	var expired *Session
	var expiredGeneration uint64
	for _, s := range room.Members {
		if s == nil {
			continue
		}
		s.mu.Lock()
		generation := s.matchHoldGeneration
		valid := s.MatchHold && s.GUID == guid && s.Room == room &&
			s.MatchHoldRoomID == room.ID && s.RoomID == s.MatchHoldRoomID &&
			s.Seat == s.MatchHoldSeat && s.SeatHeroID == s.MatchHoldHero &&
			s.MatchHoldHero > 0 && room.State == "match" && room.MatchClockArmed
		if valid && !now.Before(s.MatchHoldUntil) {
			expired = s
			expiredGeneration = generation
			valid = false
		}
		if !valid {
			s.mu.Unlock()
			continue
		}
		claim := MatchHoldClaim{
			Room: room, RoomID: s.MatchHoldRoomID, Seat: s.MatchHoldSeat,
			Hero: s.MatchHoldHero, TskCID: room.TskCID, Until: s.MatchHoldUntil,
		}
		// Keep MatchHold set until CompleteMatchResume so the shared clock stays
		// frozen across ReLoginAck write (clearing here caused TP 2002 flap).
		s.matchResumePending = conn
		s.MatchResumeGraceUntil = time.Time{}
		s.attachGSLocked(conn)
		s.LastGSActivity = now
		s.MatchPlaying = false
		s.MatchLoading = false
		s.AwaitingGS = false
		s.mu.Unlock()
		room.mu.Unlock()
		if claim.TskCID <= 0 {
			claim.TskCID = config.DefaultTskCID
		}
		return s, claim, "ok"
	}
	room.mu.Unlock()
	if expired != nil {
		expireMatchHold(expired, expiredGeneration)
		return nil, MatchHoldClaim{}, "expired"
	}
	return nil, MatchHoldClaim{}, "no-hold"
}

func (s *Session) CompleteMatchResume(conn net.Conn) bool {
	if s == nil || conn == nil {
		return false
	}
	s.mu.Lock()
	room := s.Room
	s.mu.Unlock()
	if room == nil {
		return false
	}
	room.mu.Lock()
	s.mu.Lock()
	defer room.mu.Unlock()
	defer s.mu.Unlock()
	if s.matchResumePending != conn || s.Room != room || room.State != "match" || !room.MatchClockArmed {
		return false
	}
	attached := false
	for _, c := range s.GSConns {
		if c == conn {
			attached = true
			break
		}
	}
	if !attached {
		return false
	}
	member := false
	for _, candidate := range room.Members {
		if candidate == s {
			member = true
			break
		}
	}
	if !member {
		return false
	}
	s.matchResumePending = nil
	clearMatchHoldLocked(s)
	now := time.Now()
	s.matchResumeGeneration++
	gen := s.matchResumeGeneration
	s.LastGSActivity = now
	s.MatchPlaying = true
	s.MatchLoading = false
	s.AwaitingGS = false
	// Do not clear SoftResumeFails here — only after first post-ack op3
	// (NoteSoftResumeStable). Resetting on Ack made fail-max unreachable.
	s.MatchAwaitResumePing = true
	s.MatchResumeGraceUntil = time.Time{}
	grace := config.MatchResumeGrace
	if grace > 0 {
		s.MatchResumeGraceUntil = now.Add(grace)
		time.AfterFunc(grace, func() {
			s.mu.Lock()
			ok := s.matchResumeGeneration == gen
			s.mu.Unlock()
			if ok {
				s.ClearMatchResumeGrace()
			}
		})
	}
	// Keep stall ignore tiny so a half-open peer still freezes quickly; just
	// refresh survivor activity so we don't inherit the pre-hold stall latch.
	for _, m := range room.Members {
		if m == nil || m == s {
			continue
		}
		m.mu.Lock()
		m.LastGSActivity = now
		m.mu.Unlock()
	}
	return true
}

// ClearMatchResumeGrace ends the post-ReLoginAck clock freeze (timer / tests).
func (s *Session) ClearMatchResumeGrace() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.MatchResumeGraceUntil.IsZero() {
		s.mu.Unlock()
		return
	}
	s.MatchResumeGraceUntil = time.Time{}
	s.SoftResumeFails = 0
	s.softResumeAt = time.Time{}
	room := s.Room
	user := s.Username
	s.mu.Unlock()
	if room == nil {
		return
	}
	now := time.Now()
	ignore := config.MatchStallFreeze
	if ignore < 3*time.Second {
		ignore = 3 * time.Second
	}
	room.mu.Lock()
	room.MatchStallIgnoreUntil = now.Add(ignore)
	room.mu.Unlock()
	UnstallRoom(room)
	fmt.Printf(" [MATCH] resume grace cleared user=%q — clock may advance\n", user)
}

// NoteSoftResumeAck marks the time of a soft ReLoginAck for fail counting.
func (s *Session) NoteSoftResumeAck() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.softResumeAt = time.Now()
	s.mu.Unlock()
}

// NoteSoftResumeStable clears the post-ack op3 gate after the rejoiner pings.
func (s *Session) NoteSoftResumeStable() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if !s.MatchAwaitResumePing {
		s.mu.Unlock()
		return
	}
	s.MatchAwaitResumePing = false
	s.SoftResumeFails = 0
	s.softResumeAt = time.Time{}
	user := s.Username
	room := s.Room
	s.mu.Unlock()
	if room != nil {
		UnstallRoom(room)
	}
	fmt.Printf(" [MATCH] resume stable (op3) user=%q — clock may advance\n", user)
}

// NoteSoftResumeEOF returns the new fail count when GS EOFs shortly after Ack.
func (s *Session) NoteSoftResumeEOF() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Count EOFs through the full grace window (soft can look healthy for ~2s).
	window := config.MatchResumeGrace + 2*time.Second
	if window < 5*time.Second {
		window = 5 * time.Second
	}
	if s.softResumeAt.IsZero() || time.Since(s.softResumeAt) > window {
		return s.SoftResumeFails
	}
	s.SoftResumeFails++
	s.softResumeAt = time.Time{}
	return s.SoftResumeFails
}

func (s *Session) SoftResumeFailCount() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.SoftResumeFails
}

func (s *Session) ResetSoftResumeFails() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.SoftResumeFails = 0
	s.softResumeAt = time.Time{}
	s.mu.Unlock()
}

// TouchGSActivity marks inbound GS traffic for stall-freeze detection.
func (s *Session) TouchGSActivity() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.LastGSActivity = time.Now()
	s.mu.Unlock()
}

// IsMatchClockFrozen is true while a member is held, awaiting post-ack op3, or
// stalling (playing but silent — half-open TCP before EOF).
func (s *Session) IsMatchClockFrozen() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	hold := s.MatchHold
	awaitPing := s.MatchAwaitResumePing
	playing := s.MatchPlaying
	last := s.LastGSActivity
	room := s.Room
	s.mu.Unlock()
	if hold || awaitPing {
		return true
	}
	now := time.Now()
	stall := config.MatchStallFreeze
	if stall <= 0 || !playing || last.IsZero() || now.Sub(last) <= stall {
		return false
	}
	// Unsynchronized read: callers (op7/op11) often already hold room.mu.
	// A torn deadline only affects one freeze tick; locking here deadlocks.
	if room != nil {
		ignoreUntil := room.MatchStallIgnoreUntil
		if !ignoreUntil.IsZero() && now.Before(ignoreUntil) {
			return false
		}
	}
	return true
}

// MatchPeersFrozen reports whether every other member is held, in grace, or
// not playing — safe to rewind the shared syn to a rejoiner bookmark.
func (s *Session) MatchPeersFrozen() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	room := s.Room
	s.mu.Unlock()
	if room == nil {
		return true
	}
	room.mu.Lock()
	members := append([]*Session(nil), room.Members...)
	room.mu.Unlock()
	for _, m := range members {
		if m == nil || m == s {
			continue
		}
		if m.IsMatchPlaying() && !m.IsMatchClockFrozen() {
			return false
		}
	}
	return true
}

// UnstallRoom refreshes every member's GS activity after soft resume so a
// paused survivor cannot keep IsMatchClockFrozen stuck on stall.
func UnstallRoom(room *Room) {
	if room == nil {
		return
	}
	room.mu.Lock()
	members := append([]*Session(nil), room.Members...)
	room.mu.Unlock()
	now := time.Now()
	for _, m := range members {
		if m == nil {
			continue
		}
		m.mu.Lock()
		m.LastGSActivity = now
		m.mu.Unlock()
	}
}

// MatchResumeGrace reports whether post-ack grace is still active (tests).
func (s *Session) MatchResumeGrace() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.MatchResumeGraceUntil.IsZero() && time.Now().Before(s.MatchResumeGraceUntil)
}

func CreateRoom(host *Session, opts RoomOptions) *Room {
	if host == nil {
		return nil
	}
	LeaveRoom(host, "recreate")
	if opts.Name == "" {
		opts.Name = "room"
	}
	if opts.Capacity <= 0 {
		opts.Capacity = 10
	}
	json1014 := append([]byte(nil), opts.JSON1014...)
	if len(json1014) == 0 {
		json1014 = []byte("{}")
	}
	// The client encodes its map choice as {"map":"3V3"|"5V5"|"5V5_UR"} in
	// 0x1014. Capture it at create time so every later LoadMap uses the
	// room's real map instead of a hardcoded GAME_MODE.
	mapName := ""
	var roomCfg struct {
		Map string `json:"map"`
	}
	if err := json.Unmarshal(json1014, &roomCfg); err == nil {
		mapName = roomCfg.Map
	}
	customOpts := config.CustomRoomOptionsFromJSON(json1014)
	roomMu.Lock()
	defer roomMu.Unlock()
	id := nextRoom
	nextRoom++
	r := &Room{
		ID:         id,
		Name:       opts.Name,
		Host:       host,
		Members:    []*Session{host},
		Capacity:   opts.Capacity,
		State:      "open",
		CreatedAt:  time.Now(),
		Flag1012:   opts.Flag1012,
		Flag1013:   opts.Flag1013,
		Param103E:  opts.Param103E,
		JSON1014:   json1014,
		MapName:    mapName,
		CustomOpts: customOpts,
		TskCID:     config.DefaultTskCID,
	}
	rooms[id] = r
	host.mu.Lock()
	host.Room = r
	host.RoomID = id
	host.Seat = 0
	host.SeatReady = false
	host.ReadyCancelled = false
	host.SeatVacateSent = false
	resetSeatSelectionLocked(host)
	host.mu.Unlock()
	return r
}

func (r *Room) Lock()   { r.mu.Lock() }
func (r *Room) Unlock() { r.mu.Unlock() }

func GetRoom(id int) *Room {
	roomMu.Lock()
	defer roomMu.Unlock()
	return rooms[id]
}

func ListOpenRooms() []RoomSnapshot {
	roomMu.Lock()
	list := make([]*Room, 0, len(rooms))
	for _, room := range rooms {
		list = append(list, room)
	}
	roomMu.Unlock()

	out := make([]RoomSnapshot, 0, len(list))
	for _, room := range list {
		room.mu.Lock()
		if room.State == "open" {
			out = append(out, RoomSnapshot{
				ID: room.ID, Name: room.Name, Capacity: room.Capacity, Members: len(room.Members),
				Flag1012: room.Flag1012, Flag1013: room.Flag1013, Param103E: room.Param103E,
				Flag1011: room.Flag1011, JSON1014: append([]byte(nil), room.JSON1014...),
				Str1040: room.Str1040, Int1041: room.Int1041, Str104B: room.Str104B,
			})
		}
		room.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func DestroyRoom(room *Room, reason string) {
	if room == nil {
		return
	}
	roomMu.Lock()
	if rooms[room.ID] == room {
		delete(rooms, room.ID)
	}
	roomMu.Unlock()

	room.mu.Lock()
	members := append([]*Session(nil), room.Members...)
	room.Members = nil
	room.State = "closed"
	room.mu.Unlock()
	for _, member := range members {
		if member == nil {
			continue
		}
		member.mu.Lock()
		if member.Room == room {
			resetRoomStateLocked(member)
		}
		member.mu.Unlock()
	}
	fmt.Printf(" [ROOM] - destroy id=%d reason=%s\n", room.ID, reason)
}

func LeaveRoom(s *Session, reason string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	room := s.Room
	clearMatchHoldLocked(s)
	s.mu.Unlock()
	if room == nil {
		return
	}

	room.mu.Lock()
	isHost := room.Host == s
	clockArmed := room.MatchClockArmed
	tskcid := room.TskCID
	out := room.Members[:0]
	for _, member := range room.Members {
		if member != s {
			out = append(out, member)
		}
	}
	room.Members = out
	survivors := append([]*Session(nil), room.Members...)
	empty := len(survivors) == 0
	room.mu.Unlock()

	inMatch := clockArmed
	if !inMatch {
		for _, member := range survivors {
			if member != nil && member.IsMatchPlaying() {
				inMatch = true
				break
			}
		}
	}
	// Only tear the room down when nobody remains. Host leave with survivors
	// (seat lobby or in-match) promotes — do NOT e02f-kick the other player.
	destroying := empty
	publishRoomLeave(RoomLeaveEvent{
		Room: room, Leaver: s, Survivors: survivors,
		LeaverSeat: s.Seat, LeaverNick: sessionNickname(s), TskCID: tskcid,
		WasHost: isHost, Destroying: destroying, InMatch: inMatch, Reason: reason,
	})

	s.mu.Lock()
	if s.Room == room {
		resetRoomStateLocked(s)
	}
	s.mu.Unlock()

	if empty {
		prefix := "empty_"
		if isHost {
			prefix = "host_"
		}
		DestroyRoom(room, prefix+reason)
		return
	}
	if isHost {
		room.mu.Lock()
		room.Host = survivors[0]
		room.mu.Unlock()
		phase := "seat-lobby"
		if inMatch {
			phase = "in-match"
		}
		fmt.Printf(" [ROOM] host leave %s id=%d promote=%s reason=%s\n",
			phase, room.ID, survivors[0].Username, reason)
		return
	}
	fmt.Printf(" [ROOM] member leave id=%d user=%s reason=%s\n", room.ID, s.Username, reason)
}

func resetRoomStateLocked(s *Session) {
	clearMatchHoldLocked(s)
	s.Room = nil
	s.RoomID = config.DefaultRoomID
	s.Seat = 0
	s.SeatReady = false
	s.ReadyCancelled = false
	s.SeatVacateSent = false
	s.CustomMatchArmed = false
	s.CustomRoomIntent = false
	s.AwaitingGS = false
	resetSeatSelectionLocked(s)
	s.MatchLoading = false
	s.MatchPlaying = false
	s.customSearchRoomIDs = nil
}

func resetSeatSelectionLocked(s *Session) {
	s.SeatHeroID = 0
	s.SeatSkinID = 0
	s.SeatSpell1 = 0
	s.SeatSpell2 = 0
}

// JoinRoom assigns a free seat (or requested) and adds member.
func JoinRoom(s *Session, roomID int, reqSeat *int) (*Room, string) {
	r := GetRoom(roomID)
	if r == nil {
		return nil, "no-room"
	}
	r.mu.Lock()
	state := r.State
	r.mu.Unlock()
	if state == "closed" {
		return nil, "closed"
	}
	if state == "match" {
		return nil, "match"
	}
	s.mu.Lock()
	current := s.Room
	s.mu.Unlock()
	if current != nil && current != r {
		LeaveRoom(s, "switch_room")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State == "closed" {
		return nil, "closed"
	}
	if r.State == "match" {
		return nil, "match"
	}
	used := map[int]*Session{}
	for _, m := range r.Members {
		if m != nil {
			used[m.Seat] = m
		}
	}
	seat := 0
	if reqSeat != nil {
		seat = *reqSeat & 0xFF
	}
	if owner := used[seat]; owner != nil && owner != s {
		seat = -1
		for i := 0; i < 10; i++ {
			if used[i] == nil {
				seat = i
				break
			}
		}
		if seat < 0 {
			return nil, "full"
		}
	}
	already := false
	for _, m := range r.Members {
		if m == s {
			already = true
			break
		}
	}
	if !already {
		r.Members = append(r.Members, s)
	}
	s.mu.Lock()
	s.Room = r
	s.RoomID = r.ID
	s.Seat = seat
	s.SeatReady = false
	s.ReadyCancelled = false
	s.SeatVacateSent = false
	if current != r {
		resetSeatSelectionLocked(s)
	}
	s.customSearchRoomIDs = nil
	s.mu.Unlock()
	return r, "ok"
}

// ApplyReadyFromSkill records the explicit READY bit carried by 0x100C.
// READY=0 is a cancellation latch: a trailing 0x2001 must not re-arm it.
func (s *Session) ApplyReadyFromSkill(ready bool) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !ready {
		s.SeatReady = false
		s.ReadyCancelled = true
		return true
	}
	if s.SeatHeroID <= 0 {
		s.SeatReady = false
		return false
	}
	s.SeatReady = true
	s.ReadyCancelled = false
	return true
}

// AcceptReadyStart applies the normal 0x2001 latch unless an explicit
// 0x100C READY=0 cancellation is still active.
func (s *Session) AcceptReadyStart() (bool, string) {
	if s == nil {
		return false, "no-session"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.SeatHeroID <= 0 {
		s.SeatReady = false
		return false, "no-hero"
	}
	if s.ReadyCancelled {
		s.SeatReady = false
		return false, "cancelled"
	}
	s.SeatReady = true
	return true, "ok"
}

// ClaimSeatVacate marks the one real 0x3003 notification for this room stay.
// GS 0x3001 and the later lobby teardown can otherwise describe the same exit.
func (s *Session) ClaimSeatVacate() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.SeatVacateSent {
		return false
	}
	s.SeatVacateSent = true
	return true
}

func sessionNickname(s *Session) string {
	if s == nil {
		return ""
	}
	nick := s.Username
	if s.Account != nil {
		if s.Account.Nickname != "" {
			nick = s.Account.Nickname
		} else if s.Account.Username != "" {
			nick = s.Account.Username
		}
	}
	return nick
}

func (r *Room) MemberCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.Members)
}

func (r *Room) SnapshotMembers() []*Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*Session(nil), r.Members...)
}

// ChangeSeat applies a custom-room seat hop atomically. requestedSeat is
// 0-based; collisions are remapped to the first free seat in the requested
// team half. The caller must emit only the atomic 0x1007 hop primitive.
func ChangeSeat(s *Session, requestedSeat int, guid string) (room *Room, oldSeat, newSeat int, changed bool, why string) {
	if s == nil {
		return nil, 0, 0, false, "no-session"
	}

	s.mu.Lock()
	room = s.Room
	oldSeat = s.Seat
	if guid != "" {
		s.GUID = guid
	}
	if requestedSeat < 0 || requestedSeat >= 10 {
		s.mu.Unlock()
		return room, oldSeat, oldSeat, false, "bad-seat"
	}
	if room == nil {
		s.Seat = requestedSeat
		newSeat = requestedSeat
		s.mu.Unlock()
		return nil, oldSeat, newSeat, oldSeat != newSeat, "local"
	}
	s.mu.Unlock()

	room.mu.Lock()
	defer room.mu.Unlock()

	s.mu.Lock()
	if s.Room != room {
		oldSeat = s.Seat
		s.mu.Unlock()
		return nil, oldSeat, oldSeat, false, "room-changed"
	}
	oldSeat = s.Seat
	if guid != "" {
		s.GUID = guid
	}
	s.mu.Unlock()

	found := false
	used := make(map[int]*Session, len(room.Members))
	for _, member := range room.Members {
		if member == nil {
			continue
		}
		if member == s {
			found = true
			continue
		}
		used[member.Seat] = member
	}
	if !found {
		return nil, oldSeat, oldSeat, false, "not-member"
	}

	newSeat = requestedSeat
	if used[newSeat] != nil {
		base := 0
		if newSeat >= 5 {
			base = 5
		}
		newSeat = -1
		for seat := base; seat < base+5; seat++ {
			if used[seat] == nil {
				newSeat = seat
				break
			}
		}
		if newSeat < 0 {
			return room, oldSeat, oldSeat, false, "team-full"
		}
	}

	s.mu.Lock()
	s.Seat = newSeat
	s.mu.Unlock()
	return room, oldSeat, newSeat, oldSeat != newSeat, "ok"
}

// sameTeamSeat reports whether two 0-based seats share a custom-room team half
// (0..4 vs 5..9), matching ChangeSeat remapping.
func sameTeamSeat(a, b int) bool {
	return (a < 5) == (b < 5)
}

// ClaimSeatHero makes hero selection exclusive within one team half only.
// Enemy-team mirrors of the same hero are allowed. A same-team peer already
// owning the hero wins; the rejected request must receive no ACK.
func ClaimSeatHero(s *Session, hero, skin int) (accepted bool, owner string) {
	if s == nil {
		return false, ""
	}
	s.mu.Lock()
	room := s.Room
	if room == nil {
		s.SeatHeroID = hero
		s.SeatSkinID = skin
		s.mu.Unlock()
		return true, ""
	}
	s.mu.Unlock()

	room.mu.Lock()
	defer room.mu.Unlock()

	s.mu.Lock()
	if s.Room != room {
		s.mu.Unlock()
		return false, ""
	}
	mySeat := s.Seat
	s.mu.Unlock()

	found := false
	for _, member := range room.Members {
		if member == nil {
			continue
		}
		if member == s {
			found = true
			continue
		}
		member.mu.Lock()
		claimed := member.SeatHeroID == hero
		seat := member.Seat
		name := member.Username
		member.mu.Unlock()
		if claimed && sameTeamSeat(mySeat, seat) {
			return false, name
		}
	}
	if !found {
		return false, ""
	}

	s.mu.Lock()
	if s.Room != room {
		s.mu.Unlock()
		return false, ""
	}
	s.SeatHeroID = hero
	s.SeatSkinID = skin
	s.mu.Unlock()
	return true, ""
}

func (r *Room) GuestsReady() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	host := r.Host
	guests := 0
	ready := 0
	for _, m := range r.Members {
		if m == nil || m == host {
			continue
		}
		guests++
		m.mu.Lock()
		ok := m.SeatReady
		m.mu.Unlock()
		if ok {
			ready++
		}
	}
	if guests == 0 {
		return true
	}
	return ready == guests
}

func (r *Room) EnsureSeed() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.MatchSeed == 0 {
		r.MatchSeed = int(time.Now().Unix()^int64(r.ID<<16)) & 0x7fffffff
		if r.MatchSeed == 0 {
			r.MatchSeed = 1
		}
	}
	return r.MatchSeed
}

func (r *Room) LobbyWriteAll(pkt []byte, except *Session) {
	for _, m := range r.SnapshotMembers() {
		if m == nil || m == except || m.Conn == nil {
			continue
		}
		_, _ = m.Conn.Write(pkt)
	}
}
