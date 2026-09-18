package kitabe

import (
	"testing"

	"hoc-server/internal/wire/msgpack"
)

// TabletButton::SetTabletInfo only hides a socket's energy marker when the
// socket key is present with filled=false; a missing key leaves the marker
// playing (Order/Chaos flicker). Every TabletInfo must therefore carry all
// four sockets.
func TestTabletInfoAlwaysCarriesFourSockets(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sockets map[int][2]int
		filled  []bool
	}{
		{"empty tablet", nil, []bool{false, false, false, false}},
		{"one socket", map[int][2]int{2: {494, 1}}, []bool{false, false, true, false}},
		{"full", map[int][2]int{0: {494, 1}, 1: {494, 1}, 2: {494, 1}, 3: {513, 1}}, []bool{true, true, true, true}},
	} {
		v, err := msgpack.Decode(TabletInfo(453, tc.sockets, false))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		m, ok := v.([]any)[1].(map[any]any)
		if !ok || len(m) != SocketCount {
			t.Fatalf("%s: socket map = %#v, want %d entries", tc.name, v.([]any)[1], SocketCount)
		}
		for idx, want := range tc.filled {
			slot, ok := m[int64(idx)].([]any)
			if !ok || len(slot) != 3 {
				t.Fatalf("%s: socket %d missing or malformed: %#v", tc.name, idx, m[int64(idx)])
			}
			if got := slot[0].(bool); got != want {
				t.Fatalf("%s: socket %d filled=%v, want %v", tc.name, idx, got, want)
			}
			if want {
				if id := slot[1].([]any)[0]; id != int64(tc.sockets[idx][0]) {
					t.Fatalf("%s: socket %d id=%v", tc.name, idx, id)
				}
			}
		}
	}
}
