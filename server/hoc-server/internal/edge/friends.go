package edge

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"hoc-server/internal/accounts"
)

// Gaia Osiris social endpoints (RE 2026-09-15, GLonlineSession /
// gaia::Osiris in libAndroid.so). The client only ever asks for
// connection_type "friend" and request type "connection_approval":
//
//	GET  /accounts/me/connections/friend?access_token&limit&game
//	     → JSON array, each {"credential":"gllive:<user>"} (DealWithFriendList)
//	POST /accounts/me/connections/friend   body target_credential=gllive:<user>
//	     [&required_approval=False after an accept → reverse link]
//	POST /accounts/me/connections/friend/<cred>/delete
//	GET  /accounts/me/requests/connection_approval?access_token
//	     → JSON array {id, creation, type, connection_type, requester:{credential}}
//	       (ResultFriendCallBack; type must equal "connection_approval")
//	GET  /accounts/me/requests/sent
//	POST /accounts/me/requests/<id>/accept|reject|ignore|cancel
//	GET  /profiles?credentials=a,b&include_fields=...  (Seshat batch profiles)
//	     → JSON array of profile objects keyed by "credential"
//
// gaia::BaseServiceManager::ParseMessages turns a top-level array into one
// BaseJSONServiceResponse per element; a bare object is a single response.
// HTTP 200 maps to Gaia response code 0 (success); any other status is
// forwarded as-is to ResultFriendCallBack.

func credentialFor(acc *accounts.Account) string {
	return "gllive:" + accounts.Norm(acc.Username)
}

func respondJSONStatus(w http.ResponseWriter, status int, v any) {
	b, _ := json.Marshal(v)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.Header().Set("Connection", "close")
	w.WriteHeader(status)
	_, _ = w.Write(b)
	fmt.Printf("  -> %d %s\n", status, string(b))
}

func friendEntry(acc *accounts.Account) map[string]any {
	return map[string]any{
		"credential":      credentialFor(acc),
		"connection_type": "friend",
		"game":            "mygame",
	}
}

func requestEntry(req accounts.FriendRequestRec, target *accounts.Account) map[string]any {
	from := accounts.Get(req.From)
	fromCred := "gllive:" + req.From
	if from != nil {
		fromCred = credentialFor(from)
	}
	return map[string]any{
		"id":              req.ID,
		"creation":        req.Creation,
		"type":            "connection_approval",
		"connection_type": "friend",
		"status":          "pending",
		"requester":       map[string]any{"credential": fromCred},
		"target":          map[string]any{"credential": credentialFor(target)},
	}
}

// handleOsirisAccounts serves everything under /accounts/me/{connections,requests}.
func handleOsirisAccounts(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	me, how := accountFromRequest(r)
	if me == nil {
		fmt.Printf(" [FRIEND] no account for %s (%s)\n", r.URL.Path, how)
		respondJSONStatus(w, 401, map[string]any{"error": "invalid_token"})
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	// parts: accounts me connections|requests ...
	if len(parts) < 3 {
		respondJSON(w, r, map[string]any{"status": 1, "error": 0})
		return
	}
	rest := parts[3:]
	switch parts[2] {
	case "connections":
		handleFriendConnections(w, r, me, rest)
	case "requests":
		handleFriendRequests(w, r, me, rest)
	default:
		respondJSON(w, r, map[string]any{"status": 1, "error": 0, "requests": []any{}})
	}
}

func handleFriendConnections(w http.ResponseWriter, r *http.Request, me *accounts.Account, rest []string) {
	// rest: [friend] | [friend, <cred>] | [friend, <cred>, delete] | [friend, count]
	if len(rest) == 0 {
		respondJSON(w, r, []any{})
		return
	}
	if len(rest) == 1 {
		if r.Method == http.MethodPost {
			target := accounts.FindPlayer(first(r, "target_credential"))
			if target == nil {
				fmt.Printf(" [FRIEND] %s → %q NOT FOUND\n", me.Username, first(r, "target_credential"))
				respondJSONStatus(w, 404, map[string]any{"error": "not_found", "message": "user not found"})
				return
			}
			if accounts.Norm(target.Username) == accounts.Norm(me.Username) {
				respondJSONStatus(w, 400, map[string]any{"error": "self"})
				return
			}
			if len(me.FriendUsernames()) >= accounts.MaxFriends && !me.IsFriend(target.Username) {
				respondJSONStatus(w, 409, map[string]any{"error": "limit"})
				return
			}
			direct := strings.EqualFold(first(r, "required_approval"), "false")
			req, outcome := accounts.SendFriendRequest(me, target, direct)
			fmt.Printf(" [FRIEND] %s → %s outcome=%d direct=%v id=%s\n", me.Username, target.Username, outcome, direct, req.ID)
			switch outcome {
			case accounts.FriendRequestCreated, accounts.FriendRequestExists:
				// Wake the target's Kairos stream so its client re-polls
				// connection_approval (and resolves our nickname) right away.
				if n := KairosNotify(target.Username, `{"type":"connection_request"}`); n > 0 {
					fmt.Printf(" [FRIEND] kairos notified %s streams=%d\n", target.Username, n)
				}
				respondJSON(w, r, requestEntry(req, target))
			default:
				respondJSON(w, r, friendEntry(target))
			}
			return
		}
		list := []any{}
		for _, name := range me.FriendUsernames() {
			if f := accounts.Get(name); f != nil {
				list = append(list, friendEntry(f))
			}
		}
		fmt.Printf(" [FRIEND] list %s n=%d\n", me.Username, len(list))
		respondJSON(w, r, list)
		return
	}
	if rest[1] == "count" {
		respondJSON(w, r, map[string]any{"count": len(me.FriendUsernames())})
		return
	}
	other := accounts.Get(rest[1])
	if len(rest) >= 3 && rest[2] == "delete" {
		name := rest[1]
		if other != nil {
			name = other.Username
		}
		ok := accounts.RemoveFriend(me, name)
		fmt.Printf(" [FRIEND] %s deleted %s ok=%v\n", me.Username, name, ok)
		respondJSON(w, r, map[string]any{"status": 1, "error": 0})
		return
	}
	// ConnectionExists probe.
	if other != nil && me.IsFriend(other.Username) {
		respondJSON(w, r, friendEntry(other))
		return
	}
	respondJSONStatus(w, 404, map[string]any{"error": "not_found"})
}

func handleFriendRequests(w http.ResponseWriter, r *http.Request, me *accounts.Account, rest []string) {
	// rest: [connection_approval] | [sent] | [<id>, accept|reject|ignore|cancel]
	if len(rest) >= 2 {
		id, action := rest[0], rest[1]
		switch action {
		case "accept":
			from := accounts.AcceptFriendRequest(me, id)
			if from == nil {
				fmt.Printf(" [FRIEND] %s accept id=%s: no such request\n", me.Username, id)
				respondJSONStatus(w, 404, map[string]any{"error": "not_found"})
				return
			}
			fmt.Printf(" [FRIEND] %s ACCEPTED %s (id=%s)\n", me.Username, from.Username, id)
			respondJSON(w, r, friendEntry(from))
		case "reject", "ignore":
			ok := accounts.DropFriendRequest(me, id)
			fmt.Printf(" [FRIEND] %s %s id=%s ok=%v\n", me.Username, action, id, ok)
			respondJSON(w, r, map[string]any{"status": 1, "error": 0})
		case "cancel":
			ok := accounts.CancelSentFriendRequest(me, id)
			fmt.Printf(" [FRIEND] %s cancel id=%s ok=%v\n", me.Username, id, ok)
			respondJSON(w, r, map[string]any{"status": 1, "error": 0})
		default:
			respondJSON(w, r, map[string]any{"status": 1, "error": 0})
		}
		return
	}
	if len(rest) == 1 && rest[0] == "sent" {
		list := []any{}
		for _, s := range me.SentFriendRequests() {
			if to := accounts.Get(s.To); to != nil {
				list = append(list, requestEntry(s.Req, to))
			}
		}
		respondJSON(w, r, list)
		return
	}
	list := []any{}
	for _, req := range me.PendingFriendRequests() {
		list = append(list, requestEntry(req, me))
	}
	if len(list) > 0 {
		fmt.Printf(" [FRIEND] pending %s n=%d\n", me.Username, len(list))
	}
	respondJSON(w, r, list)
}

// handleBatchProfiles serves GET /profiles?credentials=a,b&include_fields=...
// (gaia::Seshat::GetBatchProfiles). DealWithFriendList reads, per entry:
// credential, _hoc_UserInfo_Nickname_Key_VER0020 ("<nick>|<credential>" — the
// client boost::splits it on '|' and DROPS the entry unless it has ≥2 parts),
// _hoc_UserInfo_Key_Gameplay_Server_Info (string → FriendGameRoomInfo),
// _hoc_vip_level (int), _hoc_icon_sign (Trade_UserData base64), _hoc_vip_frame (string → atoi).
func handleBatchProfiles(w http.ResponseWriter, r *http.Request) {
	list := []any{}
	for _, raw := range strings.Split(first(r, "credentials"), ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		acc := accounts.Get(raw)
		if acc == nil {
			continue
		}
		list = append(list, map[string]any{
			"credential":                             credentialFor(acc),
			"username":                               acc.Username,
			"nickname":                               accountNickname(acc),
			"_hoc_UserInfo_Nickname_Key_VER0020":     accountNickname(acc) + "|" + credentialFor(acc),
			"_hoc_UserInfo_Key_Gameplay_Server_Info": "",
			"_hoc_vip_level":                         0,
			"_hoc_icon_sign":                         acc.IconSign,
			"_hoc_vip_frame":                         "0",
			"level":                                  acc.Level,
		})
	}
	respondJSON(w, r, list)
}
