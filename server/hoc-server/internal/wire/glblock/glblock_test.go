package glblock_test

import (
	"encoding/binary"
	"testing"

	"hoc-server/internal/wire/glblock"
)

func TestTeamPlayGameInfoPin(t *testing.T) {
	got := glblock.TeamPlayGameInfo([]byte("mock_nonce"), 1, "172.16.42.2", 9999)
	want := "" +
		"004d0000e02d00000000000f1014066d6f636b5f706172616d" +
		"0007100e0200000009100f03000000010010102b063137322e31362e34322e32" +
		"0007102c02270f000f0402066d6f636b5f6e6f6e6365"
	hx := hexOf(got)
	if hx != want {
		t.Fatalf("e02d pin mismatch\n got %s\nwant %s", hx, want)
	}
}

func TestSearchCustomRoomsWire(t *testing.T) {
	got := glblock.SearchCustomRooms([]glblock.CustomRoom{{
		ID: 7, Name: []byte("deneme"), Flag1012: 1, Capacity: 10,
		Members: 2, JSON1014: []byte(`{"mode":4}`),
	}})
	if glblock.Opcode(got) != 0xe03b {
		t.Fatalf("opcode=%#x", glblock.Opcode(got))
	}
	outer := glblock.IterChildren(got)
	if len(outer) != 1 || outer[0].TypeID != 0x103A || outer[0].Type != glblock.TypeTree {
		t.Fatalf("outer=%v", outer)
	}
	entries := parsePackedChildren(t, outer[0].Value)
	if len(entries) != 1 || entries[0].TypeID != 0x103B || entries[0].Type != glblock.TypeTree {
		t.Fatalf("entries=%v", entries)
	}
	fields := parsePackedChildren(t, entries[0].Value)
	if len(fields) != 12 {
		t.Fatalf("field count=%d fields=%v", len(fields), fields)
	}
	byID := map[uint16]glblock.Child{}
	for _, field := range fields {
		byID[field.TypeID] = field
	}
	if binary.BigEndian.Uint32(byID[0x100F].Value) != 7 || string(byID[0x102A].Value) != "deneme" {
		t.Fatalf("room identity fields=%v", fields)
	}
	if binary.BigEndian.Uint16(byID[0x100E].Value) != 10 || binary.BigEndian.Uint32(byID[0x1015].Value) != 2 {
		t.Fatalf("room counts fields=%v", fields)
	}
	if string(byID[0x1014].Value) != `{"mode":4}` || byID[0x103E].Type != glblock.TypeInt {
		t.Fatalf("room metadata fields=%v", fields)
	}
}

func TestSearchCustomEmptyPin(t *testing.T) {
	if got, want := hexOf(glblock.SearchCustomEmpty()), "000d0000e03b000000000005103a00"; got != want {
		t.Fatalf("empty search\n got %s\nwant %s", got, want)
	}
}

func TestRelayFailCleanCancelPin(t *testing.T) {
	got := glblock.RelayFail()
	want := "00110000210b000000000009ff000300007532"
	if hx := hexOf(got); hx != want {
		t.Fatalf("relay failure pin\n got %s\nwant %s", hx, want)
	}
	children := glblock.IterChildren(got)
	if len(children) != 1 || children[0].TypeID != 0xFF00 || children[0].Type != glblock.TypeInt {
		t.Fatalf("relay failure child=%v", children)
	}
	if len(children[0].Value) != 4 || binary.BigEndian.Uint32(children[0].Value) != 30002 {
		t.Fatalf("relay failure value=%x", children[0].Value)
	}
}

func parsePackedChildren(t *testing.T, body []byte) []glblock.Child {
	t.Helper()
	var out []glblock.Child
	for i := 0; i < len(body); {
		if i+5 > len(body) {
			t.Fatalf("short child at %d/%d", i, len(body))
		}
		n := int(binary.BigEndian.Uint16(body[i : i+2]))
		if n < 5 || i+n > len(body) {
			t.Fatalf("bad child len=%d at %d/%d", n, i, len(body))
		}
		out = append(out, glblock.Child{
			TypeID: binary.BigEndian.Uint16(body[i+2 : i+4]),
			Type:   body[i+4],
			Value:  append([]byte(nil), body[i+5:i+n]...),
		})
		i += n
	}
	return out
}

func hexOf(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexdigits[v>>4]
		out[i*2+1] = hexdigits[v&0xf]
	}
	return string(out)
}
