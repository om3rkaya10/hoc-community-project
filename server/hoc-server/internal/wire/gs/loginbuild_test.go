package gs

import (
	"encoding/hex"
	"strings"
	"testing"
)

// Captured 2026-09-19 (local, user testc): token "mock_s0001", short 0x18,
// UTF "3.5.2a", UTF "hoc_r1".
const loginReqHead = "4000000000c7020000910001000001000000" +
	"0c00676c6c6976653a7465737463" + "000a006d6f636b5f7330303031" +
	"1800" + "0600332e352e3261" + "0600686f635f7231" + "00000000"

func TestParseLoginBuild(t *testing.T) {
	body, err := hex.DecodeString(loginReqHead)
	if err != nil {
		t.Fatal(err)
	}
	if got := ParseLoginBuild(body); got != "3.5.2a" {
		t.Fatalf("stock build = %q, want 3.5.2a", got)
	}
	// 60 Hz APK: GetSimpleGameBuildVersion returns "3.5.2b".
	sixty := []byte(strings.Replace(string(body), "3.5.2a", "3.5.2b", 1))
	if got := ParseLoginBuild(sixty); got != "3.5.2b" {
		t.Fatalf("60hz build = %q, want 3.5.2b", got)
	}
	if got := ParseLoginBuild([]byte("no room field here")); got != "" {
		t.Fatalf("garbage = %q, want empty", got)
	}
}
