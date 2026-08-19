package gs_test

import (
	"encoding/binary"
	"testing"

	wiregs "hoc-server/internal/wire/gs"
)

func TestSeatRoster1002TwoPlayers(t *testing.T) {
	got := wiregs.SeatRoster1002(2, 1, []wiregs.SeatRosterMember{
		{Seat0: 0, Hero: 346, IsOwner: true},
		{Seat0: 1, Hero: 378, Ready: true},
	})
	if binary.LittleEndian.Uint32(got[:4]) != 2 || got[4] != 2 {
		t.Fatalf("header cid=%d count=%d", binary.LittleEndian.Uint32(got[:4]), got[4])
	}
	first := 5
	second := first + wiregs.PISize
	if len(got) != 4+1+2*wiregs.PISize+1 {
		t.Fatalf("len=%d", len(got))
	}
	if binary.LittleEndian.Uint32(got[first:first+4]) != 1 || binary.LittleEndian.Uint32(got[first+4:first+8]) != 346 {
		t.Fatalf("first PI seat/hero=%d/%d", binary.LittleEndian.Uint32(got[first:first+4]), binary.LittleEndian.Uint32(got[first+4:first+8]))
	}
	if binary.LittleEndian.Uint32(got[first+12:first+16]) != 1 {
		t.Fatal("host owner flag missing")
	}
	if binary.LittleEndian.Uint32(got[second:second+4]) != 2 || binary.LittleEndian.Uint32(got[second+4:second+8]) != 378 {
		t.Fatalf("second PI seat/hero=%d/%d", binary.LittleEndian.Uint32(got[second:second+4]), binary.LittleEndian.Uint32(got[second+4:second+8]))
	}
	if binary.LittleEndian.Uint32(got[second+8:second+12]) != 1 {
		t.Fatal("ready flag missing")
	}
	if got[len(got)-1] != 2 {
		t.Fatalf("local seat=%d", got[len(got)-1])
	}
}

func TestPlayerLeaveRoom3003(t *testing.T) {
	got := wiregs.PlayerLeaveRoom3003(2, 1, "Enterpries2")
	want := []byte{2, 0, 0, 0, 2, 11, 0, 'E', 'n', 't', 'e', 'r', 'p', 'r', 'i', 'e', 's', '2'}
	if string(got) != string(want) {
		t.Fatalf("got=%x want=%x", got, want)
	}
}
