package edge

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"hoc-server/internal/accounts"
)

// Gaia Kairos alert stream (RE 2026-09-15). At login the client opens
//
//	GET /alerts/me?access_token&content_type=event-stream&push_method=streaming
//	    &alert_types=connection,invitation,connection_request
//
// and keeps it open (glwebtools ServerSideEventListener_Curl). Each SSE
// `data:` line is JSON handed to GLonlineSession::KairosServiceCallback:
//
//	{"type":"connection_request"}                 → SendFriendListRequests (re-poll pending friend requests)
//	{"type":"invitation","room":..,"credential":..} → join team chat room
//	{"type":"connection"}                         → KickOut (logged in elsewhere) — never emitted here
//
// Constraints from the client: Content-Type must be text/event-stream and the
// response MUST NOT be chunked ("Server Side Event cannot provide a chunked
// response"), hence Transfer-Encoding: identity + close-on-end. When the
// stream ends (state 3) the client simply calls StartKairos again.

type kairosSub struct {
	ch chan string
}

var (
	kairosMu   sync.Mutex
	kairosSubs = map[string]map[*kairosSub]struct{}{} // Norm(username) → subscribers
)

const kairosKeepalive = 25 * time.Second

func kairosSubscribe(user string) *kairosSub {
	s := &kairosSub{ch: make(chan string, 8)}
	kairosMu.Lock()
	defer kairosMu.Unlock()
	set := kairosSubs[user]
	if set == nil {
		set = map[*kairosSub]struct{}{}
		kairosSubs[user] = set
	}
	set[s] = struct{}{}
	return s
}

func kairosUnsubscribe(user string, s *kairosSub) {
	kairosMu.Lock()
	defer kairosMu.Unlock()
	if set := kairosSubs[user]; set != nil {
		delete(set, s)
		if len(set) == 0 {
			delete(kairosSubs, user)
		}
	}
}

// KairosNotify pushes one alert payload (JSON) to every open stream of user.
func KairosNotify(user, payload string) int {
	user = accounts.Norm(user)
	kairosMu.Lock()
	defer kairosMu.Unlock()
	n := 0
	for s := range kairosSubs[user] {
		select {
		case s.ch <- payload:
			n++
		default: // slow consumer; drop rather than block the request
		}
	}
	return n
}

// KairosOnline reports whether user currently holds an alert stream.
func KairosOnline(user string) bool {
	kairosMu.Lock()
	defer kairosMu.Unlock()
	return len(kairosSubs[accounts.Norm(user)]) > 0
}

func handleKairosAlerts(w http.ResponseWriter, r *http.Request) {
	acc, how := accountFromRequest(r)
	if acc == nil {
		fmt.Printf(" [KAIROS] no account (%s)\n", how)
		respondJSON(w, r, map[string]any{"status": 1, "error": 0, "alerts": []any{}})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok || r.URL.Query().Get("content_type") != "event-stream" {
		respondJSON(w, r, map[string]any{"status": 1, "error": 0, "alerts": []any{}})
		return
	}
	user := accounts.Norm(acc.Username)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Transfer-Encoding", "identity") // net/http: no chunking, close after reply
	h.Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(200)
	flusher.Flush()

	sub := kairosSubscribe(user)
	defer kairosUnsubscribe(user, sub)
	fmt.Printf(" [KAIROS] stream open user=%s\n", user)
	defer fmt.Printf(" [KAIROS] stream closed user=%s\n", user)

	// Pending friend requests that arrived while the player was offline are
	// listed at login already; nothing to replay here.
	ticker := time.NewTicker(kairosKeepalive)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case payload := <-sub.ch:
			if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
				return
			}
			flusher.Flush()
			fmt.Printf(" [KAIROS] → %s %s\n", user, payload)
		case <-ticker.C:
			// Keepalive as a real event: KairosServiceCallback ignores unknown
			// types, whereas a bare SSE comment line is not proven safe with
			// the glwebtools parser (WAN 2026-09-15: pushes after a comment
			// line were not acted on).
			if _, err := fmt.Fprint(w, "data: {\"type\":\"keepalive\"}\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
