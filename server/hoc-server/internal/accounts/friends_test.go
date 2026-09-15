package accounts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"hoc-server/internal/accounts"
)

var fixturePath string

func loadFriendFixture(t *testing.T) (a, b *accounts.Account) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.json")
	fixturePath = path
	raw := map[string]any{"accounts": map[string]any{
		"alpha": map[string]any{"username": "alpha", "password": "x", "user_id": 1000101, "level": 40},
		"beta":  map[string]any{"username": "beta", "password": "x", "user_id": 1000102, "level": 40},
	}}
	bs, _ := json.Marshal(raw)
	if err := os.WriteFile(path, bs, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := accounts.Load(path); err != nil {
		t.Fatal(err)
	}
	a, b = accounts.Get("alpha"), accounts.Get("beta")
	if a == nil || b == nil {
		t.Fatal("fixture accounts missing")
	}
	return a, b
}

func TestFriendRequestAcceptFlow(t *testing.T) {
	a, b := loadFriendFixture(t)

	req, out := accounts.SendFriendRequest(a, b, false)
	if out != accounts.FriendRequestCreated || req.ID == "" || req.From != "alpha" {
		t.Fatalf("send: out=%d req=%+v", out, req)
	}
	// Idempotent resend.
	if _, out := accounts.SendFriendRequest(a, b, false); out != accounts.FriendRequestExists {
		t.Fatalf("resend out=%d", out)
	}
	pend := b.PendingFriendRequests()
	if len(pend) != 1 || pend[0].ID != req.ID {
		t.Fatalf("pending=%+v", pend)
	}
	if len(a.PendingFriendRequests()) != 0 || len(a.FriendUsernames()) != 0 {
		t.Fatal("requester must not see the request as incoming or as a friend yet")
	}
	sent := a.SentFriendRequests()
	if len(sent) != 1 || sent[0].To != "beta" {
		t.Fatalf("sent=%+v", sent)
	}

	from := accounts.AcceptFriendRequest(b, req.ID)
	if from == nil || from.Username != "alpha" {
		t.Fatalf("accept from=%v", from)
	}
	if !a.IsFriend("gllive:beta") || !b.IsFriend("alpha") {
		t.Fatal("friendship must be bilateral")
	}
	if len(b.PendingFriendRequests()) != 0 {
		t.Fatal("request must be consumed")
	}
	// Reverse link the client sends after accepting (required_approval=False).
	if _, out := accounts.SendFriendRequest(b, a, true); out != accounts.FriendRequestFriends {
		t.Fatalf("reverse link out=%d", out)
	}
	if !accounts.RemoveFriend(a, "beta") || a.IsFriend("beta") || b.IsFriend("alpha") {
		t.Fatal("remove must drop both sides")
	}
}

func TestFriendRequestCrossingBecomesMutual(t *testing.T) {
	a, b := loadFriendFixture(t)
	if _, out := accounts.SendFriendRequest(a, b, false); out != accounts.FriendRequestCreated {
		t.Fatalf("a→b out=%d", out)
	}
	if _, out := accounts.SendFriendRequest(b, a, false); out != accounts.FriendRequestAccepted {
		t.Fatalf("b→a out=%d", out)
	}
	if !a.IsFriend("beta") || !b.IsFriend("alpha") || len(b.PendingFriendRequests()) != 0 {
		t.Fatal("crossing requests must resolve into a friendship")
	}
}

func TestFriendRequestRejectAndPersist(t *testing.T) {
	a, b := loadFriendFixture(t)
	req, _ := accounts.SendFriendRequest(a, b, false)
	if !accounts.DropFriendRequest(b, req.ID) || accounts.DropFriendRequest(b, req.ID) {
		t.Fatal("drop must succeed once")
	}
	req, _ = accounts.SendFriendRequest(a, b, false)
	if !accounts.CancelSentFriendRequest(a, req.ID) || len(b.PendingFriendRequests()) != 0 {
		t.Fatal("cancel must remove the request from the target")
	}
	// Persisted shape survives a reload.
	req, _ = accounts.SendFriendRequest(a, b, false)
	accounts.AcceptFriendRequest(b, req.ID)
	a.SetIconSign("kpCT")
	var f struct {
		Accounts map[string]*accounts.Account `json:"accounts"`
	}
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if got := f.Accounts["alpha"]; got == nil || len(got.Friends) != 1 || got.Friends[0] != "beta" || got.IconSign != "kpCT" {
		t.Fatalf("persisted alpha=%+v", got)
	}
}

func TestFindPlayerPrefersNickname(t *testing.T) {
	a, b := loadFriendFixture(t)
	a.SetNickname("Enterpries")
	b.SetNickname("alpha") // nick collides with a's login name
	if got := accounts.FindPlayer("gllive:Enterpries"); got != a {
		t.Fatalf("nickname lookup got %v", got)
	}
	if got := accounts.FindPlayer("ALPHA"); got != b {
		t.Fatalf("nickname must win over username, got %v", got)
	}
	if got := accounts.FindPlayer("beta"); got != b {
		t.Fatalf("username fallback got %v", got)
	}
	if got := accounts.FindPlayer("nobody"); got != nil {
		t.Fatalf("unknown must be nil, got %v", got)
	}
}
