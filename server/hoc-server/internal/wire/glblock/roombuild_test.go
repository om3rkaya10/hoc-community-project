package glblock

import "testing"

func TestRoomBuildAttr(t *testing.T) {
	// Nested attribute block as the client sends it in e03a child 0x1019:
	// ... 0x101d string "custom_3.5.2a".
	attr := append([]byte{0x00, 0x12, 0x10, 0x1d, 0x06}, []byte("custom_3.5.2a")...)
	pkt := append(make([]byte, 10), PackChildren([]Child{
		{TypeID: 0x1037, Type: 1, Value: []byte{0x1e}},
		{TypeID: 0x1019, Type: 0, Value: attr},
	})...)
	if got := RoomBuildAttr(pkt); got != "3.5.2a" {
		t.Fatalf("build = %q, want 3.5.2a", got)
	}
	if got := RoomBuildAttr(append(make([]byte, 10), PackChildren([]Child{{TypeID: 0x1037, Type: 1, Value: []byte{1}}})...)); got != "" {
		t.Fatalf("no attr = %q, want empty", got)
	}
}
