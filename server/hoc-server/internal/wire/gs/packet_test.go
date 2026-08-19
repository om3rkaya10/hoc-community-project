package gs_test

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"

	wiregs "hoc-server/internal/wire/gs"
)

func TestLoginAckHocR(t *testing.T) {
	p := wiregs.LoginAck(1, 2)
	if binary.LittleEndian.Uint32(p[0:4]) != 2 {
		t.Fatal("tskcid")
	}
	// UTF after session_id
	off := 4 + 1 + 2 + 4 + 4
	ln := binary.LittleEndian.Uint16(p[off : off+2])
	s := string(p[off+2 : off+2+int(ln)])
	if s != "hoc_r1" {
		t.Fatalf("utf=%q", s)
	}
}

func TestParseLoginIdentityAcceptsTemporaryUsername(t *testing.T) {
	payload := append([]byte{0x00, 0x01, 0x02}, []byte("gllive:BlueFox_7 mock_s0042")...)
	user, token := wiregs.ParseLoginIdentity(payload)
	if user != "BlueFox_7" || token != "mock_s0042" {
		t.Fatalf("identity user=%q token=%q", user, token)
	}
}

func TestReLoginAckGoldenAndRequestParse(t *testing.T) {
	// Syn prolog i32 tskcid=2 + seat1 + UTF hoc_r3 + success=1
	wantAck := []byte{0x02, 0x00, 0x00, 0x00, 0x01, 0x06, 0x00, 'h', 'o', 'c', '_', 'r', '3', 0x01}
	if got := wiregs.ReLoginAck(3, 0, 2); !bytes.Equal(got, wantAck) {
		t.Fatalf("ReLoginAck=%x want=%x", got, wantAck)
	}

	body := []byte{1}
	body = append(body, wiregs.UTF("hoc_r3")...)
	body = append(body, wiregs.UTF("gllive:enterpries1")...)
	tmp := make([]byte, 4)
	binary.LittleEndian.PutUint32(tmp, 2431)
	body = append(body, tmp...)
	body = append(body, 4)
	req, ok := wiregs.ParseReLoginReq(body)
	if !ok || req.UseUDP != 1 || req.RoomName != "hoc_r3" || req.GUID != "gllive:enterpries1" ||
		req.RequestedSyn != 2431 || req.Mode != 4 {
		t.Fatalf("ReLoginReq ok=%v req=%+v", ok, req)
	}
	// Trailing bytes after the classic 1-int+mode form are accepted (live bodies
	// carry gsfrm/exefrm and optional pad).
	if _, ok := wiregs.ParseReLoginReq(append(append([]byte(nil), body...), 0)); !ok {
		t.Fatal("rejected trailing ReLoginReq bytes")
	}
}

func TestParseReLoginReqLiveLayout(t *testing.T) {
	// Sanitized form of the observed room=1 reconnect layout.
	live, err := hex.DecodeString("020000001200676c6c6976653a656e746572707269657332000600686f635f72311200676c6c6976653a656e7465727072696573325201000000")
	if err != nil || len(live) != 58 {
		t.Fatalf("live len=%d err=%v", len(live), err)
	}
	req, ok := wiregs.ParseReLoginReq(live)
	if !ok || req.RoomName != "hoc_r1" || req.GUID != "gllive:enterpries2" ||
		req.UseUDP != 0 || req.RequestedSyn != 338 || req.Mode != 0 {
		t.Fatalf("live44 ok=%v req=%+v", ok, req)
	}

	put3 := func(a, b, c uint32) []byte {
		out := make([]byte, 12)
		binary.LittleEndian.PutUint32(out[0:4], a)
		binary.LittleEndian.PutUint32(out[4:8], b)
		binary.LittleEndian.PutUint32(out[8:12], c)
		return out
	}
	udp3 := []byte{1}
	udp3 = append(udp3, wiregs.UTF("hoc_r4")...)
	udp3 = append(udp3, wiregs.UTF("gllive:enterpries2")...)
	udp3 = append(udp3, put3(10, 20, 30)...)
	req, ok = wiregs.ParseReLoginReq(udp3)
	if !ok || req.UseUDP != 1 || req.RoomName != "hoc_r4" || req.GUID != "gllive:enterpries2" ||
		req.RequestedSyn != 10 || req.GSFrame != 20 || req.ExeFrame != 30 {
		t.Fatalf("udp3 fallback ok=%v req=%+v", ok, req)
	}
}

func TestGameplayFrameLayout(t *testing.T) {
	b := wiregs.GameplayFrame(10, 3, 1)
	if len(b) != 24 {
		t.Fatalf("len=%d", len(b))
	}
	if binary.LittleEndian.Uint32(b[0:4]) != 10 {
		t.Fatal("frame")
	}
	if binary.LittleEndian.Uint32(b[4:8]) != 1 {
		t.Fatal("frm_inc")
	}
	if binary.LittleEndian.Uint32(b[8:12]) != 3 {
		t.Fatal("syn")
	}
}

func TestStampUnitAction(t *testing.T) {
	orig := make([]byte, 24)
	binary.LittleEndian.PutUint32(orig[0:4], 29)
	binary.LittleEndian.PutUint32(orig[4:8], 0)
	binary.LittleEndian.PutUint32(orig[8:12], 1234)

	got, oldFrame, oldSyn, ok := wiregs.StampUnitAction(orig, 31, 30)
	if !ok {
		t.Fatal("stamp failed")
	}
	if oldFrame != 29 || oldSyn != 0 {
		t.Fatalf("old=%d/%d", oldFrame, oldSyn)
	}
	if binary.LittleEndian.Uint32(got[0:4]) != 31 || binary.LittleEndian.Uint32(got[4:8]) != 30 {
		t.Fatalf("stamp=%d/%d", binary.LittleEndian.Uint32(got[0:4]), binary.LittleEndian.Uint32(got[4:8]))
	}
	if binary.LittleEndian.Uint32(got[8:12]) != 1234 {
		t.Fatal("tail changed")
	}
	if binary.LittleEndian.Uint32(orig[0:4]) != 29 {
		t.Fatal("input mutated")
	}
}

func TestParseC2SHeaderWithSlot(t *testing.T) {
	p := make([]byte, 14+8)
	binary.LittleEndian.PutUint16(p[9:11], uint16((7<<4)|3))
	binary.LittleEndian.PutUint16(p[11:13], 0x10)
	op, slot, sub, body, ok := wiregs.ParseC2SHeaderWithSlot(p)
	if !ok || op != 7 || slot != 3 || sub != 0x10 || len(body) != 8 {
		t.Fatalf("got ok=%v op=%d slot=%d sub=%#x body=%d", ok, op, slot, sub, len(body))
	}
}

func TestParseChangeSite1006AndSiteAck1007(t *testing.T) {
	guid := "gllive:enterpries1"
	body := make([]byte, 4+2+len(guid)+1+4)
	binary.LittleEndian.PutUint32(body[0:4], 2)
	binary.LittleEndian.PutUint16(body[4:6], uint16(len(guid)))
	copy(body[6:], guid)
	body[6+len(guid)] = 6

	wireSite, gotGUID, ok := wiregs.ParseChangeSite1006(body)
	if !ok || wireSite != 6 || gotGUID != guid {
		t.Fatalf("parse ok=%v site=%d guid=%q", ok, wireSite, gotGUID)
	}
	bad := append([]byte(nil), body...)
	bad[6+len(guid)] = 0
	if _, _, ok := wiregs.ParseChangeSite1006(bad); ok {
		t.Fatal("accepted zero wire site")
	}

	ack := wiregs.SiteAck1007(2, 6, 333)
	if len(ack) != 9 || binary.LittleEndian.Uint32(ack[0:4]) != 2 || ack[4] != 6 || binary.LittleEndian.Uint32(ack[5:9]) != 333 {
		t.Fatalf("ack=%x", ack)
	}
}
