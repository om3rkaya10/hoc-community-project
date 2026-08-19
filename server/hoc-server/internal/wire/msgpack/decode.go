package msgpack

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Decode parses one MessagePack value and rejects trailing bytes. The HOC trade
// channel only needs the core scalar/array/map/string types implemented here.
func Decode(b []byte) (any, error) {
	d := decoder{b: b}
	v, err := d.value(0)
	if err != nil {
		return nil, err
	}
	if d.off != len(b) {
		return nil, fmt.Errorf("msgpack: %d trailing bytes", len(b)-d.off)
	}
	return v, nil
}

type decoder struct {
	b   []byte
	off int
}

func (d *decoder) value(depth int) (any, error) {
	if depth > 64 {
		return nil, fmt.Errorf("msgpack: nesting too deep")
	}
	if d.off >= len(d.b) {
		return nil, fmt.Errorf("msgpack: unexpected EOF")
	}
	t := d.b[d.off]
	d.off++

	switch {
	case t <= 0x7f:
		return int64(t), nil
	case t >= 0xe0:
		return int64(int8(t)), nil
	case t >= 0xa0 && t <= 0xbf:
		return d.stringN(int(t & 0x1f))
	case t >= 0x90 && t <= 0x9f:
		return d.arrayN(int(t&0x0f), depth+1)
	case t >= 0x80 && t <= 0x8f:
		return d.mapN(int(t&0x0f), depth+1)
	}

	switch t {
	case 0xc0:
		return nil, nil
	case 0xc2:
		return false, nil
	case 0xc3:
		return true, nil
	case 0xc4:
		n, err := d.uintN(1)
		if err != nil {
			return nil, err
		}
		return d.bytesN(int(n))
	case 0xc5:
		n, err := d.uintN(2)
		if err != nil {
			return nil, err
		}
		return d.bytesN(int(n))
	case 0xc6:
		n, err := d.uintN(4)
		if err != nil {
			return nil, err
		}
		return d.bytesN(int(n))
	case 0xca:
		u, err := d.uintN(4)
		if err != nil {
			return nil, err
		}
		return float64(math.Float32frombits(uint32(u))), nil
	case 0xcb:
		u, err := d.uintN(8)
		if err != nil {
			return nil, err
		}
		return math.Float64frombits(u), nil
	case 0xcc:
		u, err := d.uintN(1)
		return int64(u), err
	case 0xcd:
		u, err := d.uintN(2)
		return int64(u), err
	case 0xce:
		u, err := d.uintN(4)
		return int64(u), err
	case 0xcf:
		u, err := d.uintN(8)
		if err != nil {
			return nil, err
		}
		if u > math.MaxInt64 {
			return nil, fmt.Errorf("msgpack: uint64 %d overflows int64", u)
		}
		return int64(u), nil
	case 0xd0:
		u, err := d.uintN(1)
		return int64(int8(u)), err
	case 0xd1:
		u, err := d.uintN(2)
		return int64(int16(u)), err
	case 0xd2:
		u, err := d.uintN(4)
		return int64(int32(u)), err
	case 0xd3:
		u, err := d.uintN(8)
		return int64(u), err
	case 0xd9:
		n, err := d.uintN(1)
		if err != nil {
			return nil, err
		}
		return d.stringN(int(n))
	case 0xda:
		n, err := d.uintN(2)
		if err != nil {
			return nil, err
		}
		return d.stringN(int(n))
	case 0xdb:
		n, err := d.uintN(4)
		if err != nil {
			return nil, err
		}
		return d.stringN(int(n))
	case 0xdc:
		n, err := d.uintN(2)
		if err != nil {
			return nil, err
		}
		return d.arrayN(int(n), depth+1)
	case 0xdd:
		n, err := d.uintN(4)
		if err != nil {
			return nil, err
		}
		return d.arrayN(int(n), depth+1)
	case 0xde:
		n, err := d.uintN(2)
		if err != nil {
			return nil, err
		}
		return d.mapN(int(n), depth+1)
	case 0xdf:
		n, err := d.uintN(4)
		if err != nil {
			return nil, err
		}
		return d.mapN(int(n), depth+1)
	default:
		return nil, fmt.Errorf("msgpack: unsupported type %#x", t)
	}
}

func (d *decoder) uintN(n int) (uint64, error) {
	if n < 0 || d.off+n > len(d.b) {
		return 0, fmt.Errorf("msgpack: unexpected EOF")
	}
	p := d.b[d.off : d.off+n]
	d.off += n
	switch n {
	case 1:
		return uint64(p[0]), nil
	case 2:
		return uint64(binary.BigEndian.Uint16(p)), nil
	case 4:
		return uint64(binary.BigEndian.Uint32(p)), nil
	case 8:
		return binary.BigEndian.Uint64(p), nil
	default:
		return 0, fmt.Errorf("msgpack: invalid integer width %d", n)
	}
}

func (d *decoder) bytesN(n int) ([]byte, error) {
	if n < 0 || d.off+n > len(d.b) {
		return nil, fmt.Errorf("msgpack: unexpected EOF")
	}
	out := append([]byte(nil), d.b[d.off:d.off+n]...)
	d.off += n
	return out, nil
}

func (d *decoder) stringN(n int) (string, error) {
	b, err := d.bytesN(n)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (d *decoder) arrayN(n, depth int) ([]any, error) {
	if n < 0 || n > 1<<20 {
		return nil, fmt.Errorf("msgpack: invalid array size %d", n)
	}
	out := make([]any, n)
	for i := range out {
		v, err := d.value(depth)
		if err != nil {
			return nil, fmt.Errorf("msgpack: array[%d]: %w", i, err)
		}
		out[i] = v
	}
	return out, nil
}

func (d *decoder) mapN(n, depth int) (map[any]any, error) {
	if n < 0 || n > 1<<20 {
		return nil, fmt.Errorf("msgpack: invalid map size %d", n)
	}
	out := make(map[any]any, n)
	for i := 0; i < n; i++ {
		k, err := d.value(depth)
		if err != nil {
			return nil, fmt.Errorf("msgpack: map key %d: %w", i, err)
		}
		switch k.(type) {
		case nil, bool, int64, float64, string:
		default:
			return nil, fmt.Errorf("msgpack: unsupported map key %T", k)
		}
		v, err := d.value(depth)
		if err != nil {
			return nil, fmt.Errorf("msgpack: map value %d: %w", i, err)
		}
		out[k] = v
	}
	return out, nil
}
