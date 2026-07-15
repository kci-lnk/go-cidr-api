package cidrgen

import (
	"encoding/binary"
	"reflect"
	"testing"
)

func TestRangeToPrefixesIPv4(t *testing.T) {
	start := ipv4FromUint32(0x0e1f6c01)
	end := ipv4FromUint32(0x0e1f6cff)
	want := []string{
		"14.31.108.1/32", "14.31.108.2/31", "14.31.108.4/30", "14.31.108.8/29",
		"14.31.108.16/28", "14.31.108.32/27", "14.31.108.64/26", "14.31.108.128/25",
	}
	if got := rangeToPrefixes(start, end); !reflect.DeepEqual(got, want) {
		t.Fatalf("rangeToPrefixes() = %#v, want %#v", got, want)
	}
}

func TestRangeToPrefixesIPv6(t *testing.T) {
	start := ipValue{hi: 0x20010db800000000, lo: 1, bitSize: 128}
	end := ipValue{hi: 0x20010db800000000, lo: 3, bitSize: 128}
	want := []string{"2001:db8::1/128", "2001:db8::2/127"}
	if got := rangeToPrefixes(start, end); !reflect.DeepEqual(got, want) {
		t.Fatalf("rangeToPrefixes() = %#v, want %#v", got, want)
	}
}

func ipv4FromUint32(value uint32) ipValue {
	var raw [4]byte
	binary.LittleEndian.PutUint32(raw[:], value)
	return ipv4Value(raw[:])
}
