package msgpack

import "testing"

func TestDecodeNestedTradeValue(t *testing.T) {
	b := append(FixArray(4), Int(26)...)
	b = append(b, RawStr([]byte("enterpries1"))...)
	b = append(b, FixMap(1)...)
	b = append(b, Int(2)...)
	b = append(b, Bool(true)...)
	b = append(b, FixArray(2)...)
	b = append(b, SignedInt32(-3)...)
	b = append(b, Float32(1.5)...)

	v, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	a, ok := v.([]any)
	if !ok || len(a) != 4 {
		t.Fatalf("decoded %#v", v)
	}
	if a[0] != int64(26) || a[1] != "enterpries1" {
		t.Fatalf("scalars %#v", a[:2])
	}
	m := a[2].(map[any]any)
	if m[int64(2)] != true {
		t.Fatalf("map %#v", m)
	}
}

func TestDecodeRejectsTrailingBytes(t *testing.T) {
	if _, err := Decode([]byte{0x90, 0x00}); err == nil {
		t.Fatal("expected trailing-byte error")
	}
}
