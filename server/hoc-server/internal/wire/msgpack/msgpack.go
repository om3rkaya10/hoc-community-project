package msgpack

import (
	"encoding/binary"
	"math"
)

func FixArray(n int) []byte {
	if n < 16 {
		return []byte{0x90 | byte(n)}
	}
	return []byte{0xdc, byte(n >> 8), byte(n)}
}

func FixMap(n int) []byte {
	if n < 16 {
		return []byte{0x80 | byte(n)}
	}
	return []byte{0xde, byte(n >> 8), byte(n)}
}

func Bool(v bool) []byte {
	if v {
		return []byte{0xc3}
	}
	return []byte{0xc2}
}

func Int(n int64) []byte {
	if n >= 0 && n <= 0x7f {
		return []byte{byte(n)}
	}
	if n >= -32 && n < 0 {
		return []byte{byte(int8(n))}
	}
	if n >= 0 && n <= 0xff {
		return []byte{0xcc, byte(n)}
	}
	if n >= 0 && n <= 0xffff {
		return []byte{0xcd, byte(n >> 8), byte(n)}
	}
	b := make([]byte, 5)
	b[0] = 0xce
	binary.BigEndian.PutUint32(b[1:], uint32(n))
	return b
}

// SignedInt32 matches Python _mp_int for values outside positive fixint.
func SignedInt32(n int64) []byte {
	if n >= 0 && n < 0x80 {
		return []byte{byte(n)}
	}
	b := make([]byte, 5)
	b[0] = 0xd2
	binary.BigEndian.PutUint32(b[1:], uint32(int32(n)))
	return b
}

func U16(n uint16) []byte {
	if n < 0x80 {
		return []byte{byte(n)}
	}
	if n < 0x100 {
		return []byte{0xcc, byte(n)}
	}
	return []byte{0xcd, byte(n >> 8), byte(n)}
}

func Float32(f float32) []byte {
	b := make([]byte, 5)
	b[0] = 0xca
	binary.BigEndian.PutUint32(b[1:], math.Float32bits(f))
	return b
}

func RawStr(s []byte) []byte {
	n := len(s)
	if n < 32 {
		out := make([]byte, 1+n)
		out[0] = 0xa0 | byte(n)
		copy(out[1:], s)
		return out
	}
	out := make([]byte, 3+n)
	out[0] = 0xda
	binary.BigEndian.PutUint16(out[1:3], uint16(n))
	copy(out[3:], s)
	return out
}

func EmptyArray() []byte { return []byte{0x90} }
func EmptyMap() []byte   { return []byte{0x80} }
