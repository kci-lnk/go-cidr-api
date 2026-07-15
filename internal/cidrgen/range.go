package cidrgen

import (
	"encoding/binary"
	"math/bits"
	"net/netip"
)

type ipValue struct {
	hi      uint64
	lo      uint64
	bitSize int
}

func ipv4Value(input []byte) ipValue {
	return ipValue{lo: uint64(binary.LittleEndian.Uint32(input)), bitSize: 32}
}

func ipv6Value(input []byte) ipValue {
	return ipValue{
		hi:      binary.BigEndian.Uint64(input[:8]),
		lo:      binary.BigEndian.Uint64(input[8:]),
		bitSize: 128,
	}
}

func (v ipValue) compare(other ipValue) int {
	if v.hi < other.hi {
		return -1
	}
	if v.hi > other.hi {
		return 1
	}
	if v.lo < other.lo {
		return -1
	}
	if v.lo > other.lo {
		return 1
	}
	return 0
}

func (v ipValue) next() (ipValue, bool) {
	if v.bitSize == 32 && v.lo == uint64(^uint32(0)) {
		return ipValue{}, false
	}
	if v.bitSize == 128 && v.hi == ^uint64(0) && v.lo == ^uint64(0) {
		return ipValue{}, false
	}
	next := v
	next.lo++
	if next.lo == 0 {
		next.hi++
	}
	return next, true
}

func (v ipValue) trailingZeros() int {
	if v.bitSize == 32 {
		if v.lo == 0 {
			return 32
		}
		return min(bits.TrailingZeros64(v.lo), 32)
	}
	if v.lo != 0 {
		return bits.TrailingZeros64(v.lo)
	}
	if v.hi != 0 {
		return 64 + bits.TrailingZeros64(v.hi)
	}
	return 128
}

func (v ipValue) blockEnd(hostBits int) ipValue {
	end := v
	switch {
	case hostBits == 128:
		end.hi, end.lo = ^uint64(0), ^uint64(0)
	case hostBits > 64:
		end.hi |= (uint64(1) << (hostBits - 64)) - 1
		end.lo = ^uint64(0)
	case hostBits == 64:
		end.lo = ^uint64(0)
	case hostBits > 0:
		end.lo |= (uint64(1) << hostBits) - 1
	}
	return end
}

func (v ipValue) addr() netip.Addr {
	if v.bitSize == 32 {
		var raw [4]byte
		binary.BigEndian.PutUint32(raw[:], uint32(v.lo))
		return netip.AddrFrom4(raw)
	}
	var raw [16]byte
	binary.BigEndian.PutUint64(raw[:8], v.hi)
	binary.BigEndian.PutUint64(raw[8:], v.lo)
	return netip.AddrFrom16(raw)
}

func rangeToPrefixes(start, end ipValue) []string {
	result := make([]string, 0, 8)
	for start.compare(end) <= 0 {
		hostBits := start.trailingZeros()
		blockEnd := start.blockEnd(hostBits)
		for blockEnd.compare(end) > 0 {
			hostBits--
			blockEnd = start.blockEnd(hostBits)
		}
		result = append(result, netip.PrefixFrom(start.addr(), start.bitSize-hostBits).String())
		next, ok := blockEnd.next()
		if !ok {
			break
		}
		start = next
	}
	return result
}
