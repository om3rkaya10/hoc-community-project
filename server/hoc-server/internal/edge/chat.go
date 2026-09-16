package edge

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"hoc-server/internal/accounts"
	"hoc-server/internal/config"
)

// Gameloft "Arion" group chat (ChatLibv2, RE 2026-09-15). The client locates
// service "groupchat" then talks HTTP (boost::asio, UA "ChatLibv2",
// x-www-form-urlencoded bodies). The chat access_token is the DEVICE
// account's (scope "chat"), so player identity is carried in the payloads:
//
//	POST /chat/rooms/<room>/subscribe    access_token
//	  → {"type":"room_info","cmd_url","listen_url","https_listen_url",
//	     "room_info":{num_members,motd,quota_period,reconnect_key,send_quota}}
//	GET  <listen_url>                    line stream (no Content-Length → ReadLine
//	  mode); each line a JSON doc: {"type":"room_info",...} heartbeat or
//	  {"type":"message","id","sent","msg","untranslated_msg","moderator",
//	   "sender":{"credential","nickname","avatar"}} — every field read with an
//	  unchecked GetString, so all of them must be strings.
//	POST <cmd_url>                       user={"nickname":..}&msg=..&access_token=..
//	POST /chat/rooms/<room>/invite       recipient=gllive:<user> → Kairos "invitation"
//
// The engine marks a channel started only after a room_info doc arrives on
// the listen stream (UpdateChannel → MarkStarted); sends are refused before.
// Friend PM rooms are named HOCP2pChatID#<NickA>#<NickB>; team/guild chat
// uses other room names — the server is room-name agnostic.

type chatMsg struct {
	Type      string     `json:"type"`
	ID        string     `json:"id"`
	Sent      string     `json:"sent"`
	Msg       string     `json:"msg"`
	Untrans   string     `json:"untranslated_msg"`
	Moderator bool       `json:"moderator"`
	Sender    chatSender `json:"sender"`
}

type chatSender struct {
	Credential string `json:"credential"`
	Nickname   string `json:"nickname"`
	Avatar     string `json:"avatar"`
}

type chatMember struct {
	user         string
	queue        chan chatMsg
	seen         time.Time
	reconnectKey string
}

type chatRoom struct {
	name    string
	members map[string]*chatMember // by Norm(username)
	seq     int64
}

var (
	chatMu    sync.Mutex
	chatRooms = map[string]*chatRoom{}
	// MessageId::operator< compares only the first four bytes. Seed the
	// process counter from time so a server restart does not replay 0001,
	// 0002, ... into a client that retained its per-room duplicate set.
	chatSeq = time.Now().UnixNano() % (36 * 36 * 36 * 36)
)

const (
	// chatv2::TIMEOUT is 10 s per ReadLine: something must arrive on an idle
	// listen stream at least that often.
	chatHeartbeat = 5 * time.Second
	// Keep the listen socket alive. The client reconnects on transport EOF;
	// forced rotation creates a race where the next POST lands before the
	// new room_info has started the channel, and it also replays queued
	// messages through the duplicate filter.
	chatStreamLife  = 24 * time.Hour
	chatFirstMsgGap = 300 * time.Millisecond
	chatQueueDepth  = 64
)

func roomInfo(room *chatRoom, key string) map[string]any {
	return map[string]any{
		"num_members":   room.memberCount(),
		"motd":          "",
		"quota_period":  10,
		"reconnect_key": key,
		"send_quota":    20,
	}
}

func chatRoomGet(name string, create bool) *chatRoom {
	chatMu.Lock()
	defer chatMu.Unlock()
	r := chatRooms[name]
	if r == nil && create {
		r = &chatRoom{name: name, members: map[string]*chatMember{}}
		chatRooms[name] = r
	}
	return r
}

func (r *chatRoom) join(user string) *chatMember {
	chatMu.Lock()
	defer chatMu.Unlock()
	m := r.members[user]
	if m == nil {
		m = &chatMember{user: user, queue: make(chan chatMsg, chatQueueDepth)}
		r.members[user] = m
	}
	m.seen = time.Now()
	return m
}

func (r *chatRoom) leave(user string) {
	chatMu.Lock()
	defer chatMu.Unlock()
	delete(r.members, user)
}

func (r *chatRoom) memberCount() int {
	chatMu.Lock()
	defer chatMu.Unlock()
	return len(r.members)
}

// post fans the message out to every member's queue (including the sender —
// the client renders its own line from the echo).
func (r *chatRoom) post(m chatMsg) int {
	chatMu.Lock()
	defer chatMu.Unlock()
	n := 0
	for _, mem := range r.members {
		select {
		case mem.queue <- m:
			n++
		default:
			// drop oldest to keep the stream moving
			select {
			case <-mem.queue:
			default:
			}
			select {
			case mem.queue <- m:
				n++
			default:
			}
		}
	}
	return n
}

// chatHost is the host the ChatLib client must reach. Its boost::asio
// resolver is NOT covered by the APK's DNS remap (only GL/XPlayer/curl are),
// so on a LAN/public edge hand out the real edge host/IP; in the Nox DNAT
// path the gameloft hostname resolves through the emulator's hosts file.
func chatHost() string {
	if config.IsDirectEdge() && config.EdgeHost != "" {
		return config.EdgeHost
	}
	return config.ClientHost()
}

func chatBase() string {
	return "http://" + chatHost() + ":8080"
}

func chatBaseTLS() string {
	return "https://" + chatHost() + ":8443"
}

func respondChatJSON(w http.ResponseWriter, status int, v any) {
	b, _ := json.Marshal(v)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(status)
	_, _ = w.Write(b)
	fmt.Printf("  -> chat %d %s\n", status, string(b))
}

// handleChat serves everything under /chat/.
func handleChat(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 16384))
	form, _ := url.ParseQuery(string(body))
	get := func(k string) string {
		if v := form.Get(k); v != "" {
			return v
		}
		return r.URL.Query().Get(k)
	}
	fmt.Printf(" [CHAT] %s %s\n", r.Method, r.URL.Path)

	// /chat/<rooms|channels>/<name>[/subscribe|/listen|/messages|/unsubscribe|/invite]
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		respondChatJSON(w, 404, map[string]any{"error": "bad_path"})
		return
	}
	kind, name := parts[1], parts[2]
	action := ""
	if len(parts) >= 4 {
		action = parts[3]
	}
	roomKey := kind + "/" + name

	acc := accounts.ResolveToken(get("access_token"))
	if acc == nil {
		acc, _ = accountFromRequest(r)
	}
	if acc == nil && get("k") != "" {
		acc = accounts.Get(get("k"))
	}
	if acc == nil {
		fmt.Printf(" [CHAT] no account (token=%q k=%q)\n", get("access_token"), get("k"))
		respondChatJSON(w, 401, map[string]any{"error": "invalid_token"})
		return
	}
	user := accounts.Norm(acc.Username)

	switch action {
	case "subscribe":
		room := chatRoomGet(roomKey, true)
		key := get("reconnect_key")
		if key == "" {
			key = fmt.Sprintf("rk_%s_%d", user, time.Now().UnixNano()/1e6)
		}
		mem := room.join(user)
		mem.reconnectKey = key
		base := chatBase() + "/chat/" + kind + "/" + url.PathEscape(name)
		info := roomInfo(room, key)
		// The client uses listen_url verbatim and sends no token on the
		// long-poll, so the member identity rides in the URL itself.
		// https_listen_url is deliberately the same plain-HTTP URL: over WAN
		// the TLS listen attempt against the raw edge IP stalled ~30 s before
		// the client fell back, and sends are dropped until the listen
		// stream is up (WAN 2026-09-15).
		listen := base + "/listen?k=" + url.QueryEscape(user)
		resp := map[string]any{
			"type":             "room_info",
			"cmd_url":          base,
			"listen_url":       listen,
			"https_listen_url": listen,
			"room_info":        info,
		}
		for k, v := range info {
			resp[k] = v
		}
		fmt.Printf(" [CHAT] %s subscribed %s members=%d\n", user, roomKey, room.memberCount())
		respondChatJSON(w, 200, resp)

	case "unsubscribe":
		if room := chatRoomGet(roomKey, false); room != nil {
			room.leave(user)
		}
		respondChatJSON(w, 200, map[string]any{"status": "ok"})

	case "listen":
		// Streamed body (no Content-Length → the client's HTTPClient reads
		// JSON lines until the server closes). The first line is always
		// room_info: the engine marks the channel started (UpdateChannel →
		// MarkStarted) only after a room_info arrives on the listen path, and
		// sends are refused until then. Further room_info lines are the idle
		// heartbeat (chatv2::TIMEOUT is 10 s per ReadLine). Streams are
		// closed after chatStreamLife; the client re-issues the GET and the
		// member queue carries anything posted in between.
		room := chatRoomGet(roomKey, true)
		mem := room.join(user)
		flusher, ok := w.(http.Flusher)
		if !ok {
			respondChatJSON(w, 500, map[string]any{"error": "no_stream"})
			return
		}
		h := w.Header()
		h.Set("Content-Type", "application/json")
		// ChatLib HTTPClient reads the response body as LF-delimited JSON;
		// its header parser has no Transfer-Encoding/chunk decoder. Force
		// close-delimited identity framing and flush raw JSON documents.
		h.Set("Transfer-Encoding", "identity")
		h.Set("Cache-Control", "no-cache")
		w.WriteHeader(200)
		flusher.Flush()
		fmt.Printf(" [CHAT] stream open %s in %s\n", user, roomKey)
		writeDoc := func(v any) bool {
			b, _ := json.Marshal(v)
			if _, err := w.Write(append(b, '\r', '\n')); err != nil {
				return false
			}
			flusher.Flush()
			return true
		}
		key := mem.reconnectKey
		if key == "" {
			key = "rk_" + user
		}
		info := roomInfo(room, key)
		info["type"] = "room_info"
		if !writeDoc(info) {
			return
		}
		opened := time.Now()
		life := time.NewTimer(chatStreamLife)
		defer life.Stop()
		beat := time.NewTicker(chatHeartbeat)
		defer beat.Stop()
		for {
			select {
			case m := <-mem.queue:
				if gap := chatFirstMsgGap - time.Since(opened); gap > 0 {
					time.Sleep(gap)
				}
				fmt.Printf(" [CHAT] deliver %s → %s in %s\n", m.Sender.Nickname, user, roomKey)
				if !writeDoc(m) {
					return
				}
			case <-beat.C:
				if !writeDoc(info) {
					return
				}
			case <-life.C:
				fmt.Printf(" [CHAT] stream rotate %s in %s\n", user, roomKey)
				return
			case <-r.Context().Done():
				fmt.Printf(" [CHAT] stream closed %s in %s\n", user, roomKey)
				return
			}
		}

	case "invite":
		// SendInviteRequest: recipient=gllive:<user>. The recipient's client
		// joins the room when its Kairos stream delivers an invitation alert
		// (GLonlineSession::KairosServiceCallback → ChatSession::JoinChatRoom).
		target := accounts.FindPlayer(get("recipient"))
		if target != nil {
			// The chat token belongs to the device account; the inviting
			// player is the other nick in the P2P room name
			// (HOCP2pChatID#<NickA>#<NickB>). The recipient's client resolves
			// this credential against its friend list to show the alert.
			inviter := credentialFor(acc)
			if strings.HasPrefix(name, "HOCP2pChatID#") {
				for _, nick := range strings.Split(strings.TrimPrefix(name, "HOCP2pChatID#"), "#") {
					if p := accounts.FindPlayer(nick); p != nil && accounts.Norm(p.Username) != accounts.Norm(target.Username) {
						inviter = credentialFor(p)
					}
				}
			}
			payload, _ := json.Marshal(map[string]any{
				"type":       "invitation",
				"room":       name,
				"credential": inviter,
			})
			n := KairosNotify(target.Username, string(payload))
			fmt.Printf(" [CHAT] %s invited %s to %s (kairos streams=%d)\n", user, target.Username, roomKey, n)
		} else {
			fmt.Printf(" [CHAT] invite: unknown recipient %q\n", get("recipient"))
		}
		respondChatJSON(w, 200, map[string]any{"status": "ok"})

	case "messages", "":
		// send
		text := get("message")
		if text == "" {
			text = get("msg")
		}
		// The chat token is the DEVICE account's; the player identity comes
		// with the message as user={"nickname":"..."} (ArionUser).
		nick := accountNickname(acc)
		cred := credentialFor(acc)
		var au struct {
			Nickname   string `json:"nickname"`
			Credential string `json:"credential"`
			Avatar     string `json:"avatar"`
		}
		if err := json.Unmarshal([]byte(get("user")), &au); err == nil && au.Nickname != "" {
			nick = au.Nickname
			if p := accounts.FindPlayer(au.Nickname); p != nil {
				cred = credentialFor(p)
			}
			if au.Credential != "" {
				cred = au.Credential
			}
		}
		room := chatRoomGet(roomKey, true)
		// ChatSession::MessageId::operator< compares only the first four
		// bytes (strncmp(..., 4) @0x012dc690). IDs that differ after byte 4
		// collapse to the same set key. Generate a fixed-width base-36
		// counter so every message has a distinct four-byte ordering key.
		chatMu.Lock()
		chatSeq++
		id := chatSeq % (36 * 36 * 36 * 36)
		chatMu.Unlock()
		idText := strconv.FormatInt(id, 36)
		if len(idText) < 4 {
			idText = strings.Repeat("0", 4-len(idText)) + idText
		}
		// MessageResponse::Parse reads every field with an unchecked
		// GetString: id/sent/msg/untranslated_msg/sender.* MUST be strings.
		m := chatMsg{
			Type:      "message",
			ID:        idText,
			Sent:      time.Now().UTC().Format("2006-01-02 15:04:05Z"),
			Msg:       text,
			Untrans:   text,
			Moderator: false,
			Sender: chatSender{
				Credential: cred,
				Nickname:   nick,
				Avatar:     au.Avatar,
			},
		}
		n := room.post(m)
		fmt.Printf(" [CHAT] %s → %s %q delivered_to=%d\n", user, roomKey, text, n)
		respondChatJSON(w, 200, m)

	default:
		respondChatJSON(w, 200, map[string]any{"status": "ok"})
	}
}
