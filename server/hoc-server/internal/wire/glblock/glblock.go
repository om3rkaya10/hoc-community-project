package glblock

import (
	"encoding/binary"
)

const (
	TypeChar   = 1
	TypeShort  = 2
	TypeInt    = 3
	TypeString = 6
	TypeTree   = 0
)

type Child struct {
	TypeID uint16
	Type   uint8
	Value  []byte
}

func PackChildren(kids []Child) []byte {
	var body []byte
	for _, c := range kids {
		bsz := len(c.Value) + 5
		hdr := make([]byte, 5)
		binary.BigEndian.PutUint16(hdr[0:2], uint16(bsz))
		binary.BigEndian.PutUint16(hdr[2:4], c.TypeID)
		hdr[4] = c.Type
		body = append(body, hdr...)
		body = append(body, c.Value...)
	}
	return body
}

func PackPacket(opcode uint16, kids []Child) []byte {
	inner := make([]byte, 8)
	binary.BigEndian.PutUint16(inner[0:2], 0) // dummy
	binary.BigEndian.PutUint16(inner[2:4], opcode)
	binary.BigEndian.PutUint32(inner[4:8], 0) // flags
	inner = append(inner, PackChildren(kids)...)
	out := make([]byte, 2+len(inner))
	binary.BigEndian.PutUint16(out[0:2], uint16(len(inner)))
	copy(out[2:], inner)
	return out
}

func IntBE(v int32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(v))
	return b
}

func ShortBE(v uint16) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, v)
	return b
}

func Char(v byte) []byte { return []byte{v} }

func IterChildren(pkt []byte) []Child {
	if len(pkt) < 10 {
		return nil
	}
	body := pkt[10:]
	var out []Child
	i := 0
	for i+5 <= len(body) {
		bsz := int(binary.BigEndian.Uint16(body[i : i+2]))
		tid := binary.BigEndian.Uint16(body[i+2 : i+4])
		typ := body[i+4]
		if bsz < 5 || i+bsz > len(body) {
			break
		}
		val := append([]byte(nil), body[i+5:i+bsz]...)
		out = append(out, Child{TypeID: tid, Type: typ, Value: val})
		i += bsz
	}
	return out
}

func Opcode(pkt []byte) uint16 {
	if len(pkt) < 6 {
		return 0
	}
	return binary.BigEndian.Uint16(pkt[4:6])
}

func HandshakeReply(accountID string) []byte {
	aid := []byte(accountID)
	if len(aid) > 32 {
		aid = aid[:32]
	}
	return PackPacket(0x6101, []Child{{0x0402, TypeString, aid}})
}

func LoginReply(userID int32) []byte {
	return PackPacket(0x2103, []Child{
		{0x030f, TypeInt, IntBE(0)},
		{0x0310, TypeInt, IntBE(userID)},
		{0x0311, TypeChar, Char(0)},
		{0x2011, TypeInt, IntBE(0)},
	})
}

func CreateCustomReply(roomID int32, roomName []byte) []byte {
	kids := []Child{{0x100F, TypeInt, IntBE(roomID)}}
	if len(roomName) > 0 {
		n := roomName
		if len(n) > 32 {
			n = n[:32]
		}
		kids = append(kids, Child{0x102A, TypeString, n})
	}
	return PackPacket(0xe039, kids)
}

func TeamPlayGameInfo(token []byte, roomID int32, gsIP string, gsPort uint16) []byte {
	if len(token) > 32 {
		token = token[:32]
	}
	return PackPacket(0xe02d, []Child{
		{0x1014, TypeString, []byte("mock_param")},
		{0x100E, TypeShort, ShortBE(0)},
		{0x100F, TypeInt, IntBE(roomID)},
		{0x102B, TypeString, []byte(gsIP)},
		{0x102C, TypeShort, ShortBE(gsPort)},
		{0x0402, TypeString, append([]byte(nil), token...)},
	})
}

func Empty(opcode uint16) []byte {
	return PackPacket(opcode, nil)
}

func JoinCustomHost(nick, userID string, seat byte) []byte {
	return JoinCustomRoster([]LobbyUser{{Nick: nick, UserID: userID, Seat: seat}})
}

type LobbyUser struct {
	Nick   string
	UserID string
	Seat   byte
}

// CustomRoom is the wire-facing snapshot used by e03b/0x103A room listings.
type CustomRoom struct {
	ID        int32
	Name      []byte
	Flag1012  byte
	Flag1013  byte
	Capacity  uint16
	Members   int32
	Param103E int32
	Flag1011  byte
	JSON1014  []byte
	Str1040   []byte
	Int1041   int32
	Str104B   []byte
}

func JoinCustomRoster(users []LobbyUser) []byte {
	var listKids []Child
	for _, u := range users {
		inner := PackChildren([]Child{
			{0x1009, TypeString, []byte(u.Nick)},
			{0x1045, TypeString, []byte(u.UserID)},
			{0x100A, TypeChar, Char(u.Seat)},
			{0x1007, TypeString, nil},
		})
		listKids = append(listKids, Child{0x100D, TypeTree, inner})
	}
	list := PackChildren(listKids)
	return PackPacket(0xe03d, []Child{
		{0x100C, TypeTree, list},
		{0x1044, TypeTree, nil},
	})
}

func NewUserJoined(nick, userID string, seat byte) []byte {
	inner := PackChildren([]Child{
		{0x1009, TypeString, []byte(nick)},
		{0x1045, TypeString, []byte(userID)},
		{0x100A, TypeChar, Char(seat)},
		{0x1007, TypeString, nil},
	})
	return PackPacket(0xe05d, []Child{{0x100D, TypeTree, inner}})
}

func SearchCustomRooms(rooms []CustomRoom) []byte {
	var entries []Child
	for _, room := range rooms {
		name := append([]byte(nil), room.Name...)
		if len(name) > 32 {
			name = name[:32]
		}
		json1014 := append([]byte(nil), room.JSON1014...)
		if len(json1014) == 0 {
			json1014 = []byte("{}")
		}
		if len(json1014) > 512 {
			json1014 = json1014[:512]
		}
		str1040 := append([]byte(nil), room.Str1040...)
		if len(str1040) > 64 {
			str1040 = str1040[:64]
		}
		str104b := append([]byte(nil), room.Str104B...)
		if len(str104b) > 64 {
			str104b = str104b[:64]
		}
		entry := PackChildren([]Child{
			{0x100F, TypeInt, IntBE(room.ID)},
			{0x102A, TypeString, name},
			{0x1012, TypeChar, Char(room.Flag1012)},
			{0x1013, TypeChar, Char(room.Flag1013)},
			{0x100E, TypeShort, ShortBE(room.Capacity)},
			{0x1015, TypeInt, IntBE(room.Members)},
			{0x103E, TypeInt, IntBE(room.Param103E)},
			{0x1011, TypeChar, Char(room.Flag1011)},
			{0x1014, TypeString, json1014},
			{0x1040, TypeString, str1040},
			{0x1041, TypeInt, IntBE(room.Int1041)},
			{0x104B, TypeString, str104b},
		})
		entries = append(entries, Child{0x103B, TypeTree, entry})
	}
	return PackPacket(0xe03b, []Child{{0x103A, TypeTree, PackChildren(entries)}})
}

func SearchCustomEmpty() []byte {
	return SearchCustomRooms(nil)
}

func RelayFail() []byte {
	// 30002 (0x7532) → HandleError clean-cancel (not hang)
	return PackPacket(0x210b, []Child{{0xFF00, TypeInt, IntBE(30002)}})
}

func RelaySuccess(gsIP string) []byte {
	return PackPacket(0x210b, []Child{
		{0x0202, TypeInt, IntBE(1)},
		{0x0210, TypeString, []byte(gsIP + ":20001")},
	})
}

func JoinRoomReply(gsIP string, gsPort uint16, token []byte) []byte {
	if len(token) > 32 {
		token = token[:32]
	}
	return PackPacket(0x2106, []Child{
		{0x0003, TypeString, []byte(gsIP)},
		{0x0101, TypeShort, ShortBE(gsPort)},
		{0x0402, TypeString, append([]byte(nil), token...)},
	})
}

func KeepaliveReply() []byte {
	return PackPacket(0x2105, nil)
}

func FlagsGLSReady() []byte {
	return PackPacket(0x6000, []Child{{0x0203, TypeString, []byte("flags")}})
}
