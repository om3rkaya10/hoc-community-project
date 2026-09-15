package accounts

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// FriendRequestRec is a pending Osiris connection_approval request stored on
// the TARGET account (the one who must accept). ID is what the client echoes
// back in POST /accounts/me/requests/<id>/accept|reject|ignore.
type FriendRequestRec struct {
	ID       string `json:"id"`
	From     string `json:"from"`     // requester username (Norm'd)
	Creation string `json:"creation"` // Gaia wire "YYYY-MM-DD HH:MM:SS"
}

// FriendRequestOutcome tells the edge what SendFriendRequest did.
type FriendRequestOutcome int

const (
	FriendRequestCreated  FriendRequestOutcome = iota // pending on target
	FriendRequestExists                               // already pending, nothing changed
	FriendRequestAccepted                             // target had already asked us → mutual friendship
	FriendRequestFriends                              // already friends
)

const MaxFriends = 50 // DlgLgmMainMenuFriends header "ARKADAŞLAR (n/50)"

func gaiaNow() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05")
}

func containsNorm(list []string, name string) bool {
	for _, v := range list {
		if v == name {
			return true
		}
	}
	return false
}

func removeNorm(list []string, name string) []string {
	out := list[:0]
	for _, v := range list {
		if v != name {
			out = append(out, v)
		}
	}
	return out
}

func (a *Account) friendsLocked() []string {
	out := make([]string, len(a.Friends))
	copy(out, a.Friends)
	sort.Strings(out)
	return out
}

// FriendUsernames returns the bilateral friend set, sorted.
func (a *Account) FriendUsernames() []string {
	if a == nil {
		return nil
	}
	mu.RLock()
	defer mu.RUnlock()
	return a.friendsLocked()
}

func (a *Account) IsFriend(other string) bool {
	if a == nil {
		return false
	}
	mu.RLock()
	defer mu.RUnlock()
	return containsNorm(a.Friends, Norm(other))
}

// PendingFriendRequests are the incoming requests a must answer.
func (a *Account) PendingFriendRequests() []FriendRequestRec {
	if a == nil {
		return nil
	}
	mu.RLock()
	defer mu.RUnlock()
	out := make([]FriendRequestRec, len(a.FriendRequests))
	copy(out, a.FriendRequests)
	return out
}

// SentFriendRequests scans every account for requests a has sent.
func (a *Account) SentFriendRequests() []struct {
	Req FriendRequestRec
	To  string
} {
	if a == nil {
		return nil
	}
	mu.RLock()
	defer mu.RUnlock()
	me := Norm(a.Username)
	var out []struct {
		Req FriendRequestRec
		To  string
	}
	for _, other := range byUN {
		for _, req := range other.FriendRequests {
			if req.From == me {
				out = append(out, struct {
					Req FriendRequestRec
					To  string
				}{req, other.Username})
			}
		}
	}
	return out
}

func requestIndexLocked(a *Account, id string) int {
	for i, req := range a.FriendRequests {
		if req.ID == id {
			return i
		}
	}
	return -1
}

func pendingFromLocked(target *Account, from string) int {
	for i, req := range target.FriendRequests {
		if req.From == from {
			return i
		}
	}
	return -1
}

func linkFriendsLocked(a, b *Account) {
	an, bn := Norm(a.Username), Norm(b.Username)
	if !containsNorm(a.Friends, bn) {
		a.Friends = append(a.Friends, bn)
	}
	if !containsNorm(b.Friends, an) {
		b.Friends = append(b.Friends, an)
	}
	// A fresh friendship supersedes any pending request in either direction.
	if i := pendingFromLocked(a, bn); i >= 0 {
		a.FriendRequests = append(a.FriendRequests[:i], a.FriendRequests[i+1:]...)
	}
	if i := pendingFromLocked(b, an); i >= 0 {
		b.FriendRequests = append(b.FriendRequests[:i], b.FriendRequests[i+1:]...)
	}
}

var requestSeq int64

func newRequestIDLocked() string {
	now := time.Now().UnixNano() / int64(time.Millisecond)
	if now <= requestSeq {
		now = requestSeq + 1
	}
	requestSeq = now
	return strconv.FormatInt(now, 10)
}

// SendFriendRequest is POST /accounts/me/connections/friend from `from`
// targeting `to` (required_approval default). When `direct` is set the
// client asked for no approval (it does this right after accepting, to add
// the reverse link) — we link immediately.
func SendFriendRequest(from, to *Account, direct bool) (FriendRequestRec, FriendRequestOutcome) {
	mu.Lock()
	defer mu.Unlock()
	fn, tn := Norm(from.Username), Norm(to.Username)
	if containsNorm(from.Friends, tn) && containsNorm(to.Friends, fn) {
		return FriendRequestRec{}, FriendRequestFriends
	}
	// Target already asked us (or client requested a direct link): mutual.
	if direct || pendingFromLocked(from, tn) >= 0 {
		linkFriendsLocked(from, to)
		_ = saveLocked()
		return FriendRequestRec{}, FriendRequestAccepted
	}
	if i := pendingFromLocked(to, fn); i >= 0 {
		return to.FriendRequests[i], FriendRequestExists
	}
	req := FriendRequestRec{ID: newRequestIDLocked(), From: fn, Creation: gaiaNow()}
	to.FriendRequests = append(to.FriendRequests, req)
	_ = saveLocked()
	return req, FriendRequestCreated
}

// AcceptFriendRequest resolves request id pending on `me`; returns the requester.
func AcceptFriendRequest(me *Account, id string) *Account {
	mu.Lock()
	defer mu.Unlock()
	i := requestIndexLocked(me, id)
	if i < 0 {
		return nil
	}
	req := me.FriendRequests[i]
	me.FriendRequests = append(me.FriendRequests[:i], me.FriendRequests[i+1:]...)
	from := byUN[req.From]
	if from != nil {
		linkFriendsLocked(me, from)
	}
	_ = saveLocked()
	return from
}

// DropFriendRequest removes a pending request on `me` (reject / ignore).
func DropFriendRequest(me *Account, id string) bool {
	mu.Lock()
	defer mu.Unlock()
	i := requestIndexLocked(me, id)
	if i < 0 {
		return false
	}
	me.FriendRequests = append(me.FriendRequests[:i], me.FriendRequests[i+1:]...)
	_ = saveLocked()
	return true
}

// CancelSentFriendRequest removes request id that `me` sent, wherever it sits.
func CancelSentFriendRequest(me *Account, id string) bool {
	mu.Lock()
	defer mu.Unlock()
	mine := Norm(me.Username)
	for _, other := range byUN {
		if i := requestIndexLocked(other, id); i >= 0 && other.FriendRequests[i].From == mine {
			other.FriendRequests = append(other.FriendRequests[:i], other.FriendRequests[i+1:]...)
			_ = saveLocked()
			return true
		}
	}
	return false
}

// RemoveFriend drops the bilateral link (and any pending request) between the two.
func RemoveFriend(a *Account, other string) bool {
	mu.Lock()
	defer mu.Unlock()
	on := Norm(other)
	an := Norm(a.Username)
	changed := containsNorm(a.Friends, on)
	a.Friends = removeNorm(a.Friends, on)
	if b := byUN[on]; b != nil {
		if containsNorm(b.Friends, an) {
			changed = true
		}
		b.Friends = removeNorm(b.Friends, an)
	}
	if changed {
		_ = saveLocked()
	}
	return changed
}

// FindPlayer resolves a name a player typed into the add-friend box. The
// in-game identity is the nickname (on the live server nick == login for
// every account but one), so a unique case-insensitive nickname match wins;
// the exact username is the fallback.
func FindPlayer(raw string) *Account {
	want := strings.ToLower(strings.TrimSpace(raw))
	for _, pfx := range []string{"gllive:", "glive:", "hoc:"} {
		want = strings.TrimPrefix(want, pfx)
	}
	if want == "" {
		return nil
	}
	mu.RLock()
	defer mu.RUnlock()
	var found *Account
	ambiguous := false
	for _, acc := range byUN {
		if acc.Device || strings.ToLower(strings.TrimSpace(acc.Nickname)) != want {
			continue
		}
		if found != nil {
			ambiguous = true
			break
		}
		found = acc
	}
	if found != nil && !ambiguous {
		return found
	}
	return byUN[want]
}

func (a *Account) SetIconSign(raw string) {
	if a == nil {
		return
	}
	raw = strings.TrimSpace(raw)
	persistMutation(a, func() {
		a.IconSign = raw
	})
}
