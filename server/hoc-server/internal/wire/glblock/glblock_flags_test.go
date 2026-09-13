package glblock

import (
	"encoding/binary"
	"testing"
)

func TestGuildLoginCompleteCarriesEveryRequiredChild(t *testing.T) {
	pkt := GuildLoginComplete()
	if Opcode(pkt) != 0x6000 {
		t.Fatalf("opcode=%#x", Opcode(pkt))
	}
	body := pkt[2+8:]
	want := map[uint16]uint8{0xff00: TypeInt, 0x150b: TypeString, 0x150d: TypeInt, 0x1505: TypeString, 0x150a: TypeString}
	seen := map[uint16]uint8{}
	for len(body) >= 5 {
		bsz := int(binary.BigEndian.Uint16(body[0:2]))
		seen[binary.BigEndian.Uint16(body[2:4])] = body[4]
		body = body[bsz:]
	}
	for id, typ := range want {
		if seen[id] != typ {
			t.Fatalf("child %#x type=%d want %d (seen=%v)", id, seen[id], typ, seen)
		}
	}
}
