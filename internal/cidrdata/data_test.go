package cidrdata

import (
	"reflect"
	"testing"
)

func TestOperatorsForISP(t *testing.T) {
	tests := map[string][]string{
		"中国电信/CN2":   {OperatorTelecom},
		"鹏博士/联通":     {OperatorUnicom},
		"电信/中国移动/联通": Operators,
		"中移铁通":       {OperatorMobile},
		"天地通电信":      {},
	}
	for input, want := range tests {
		if got := OperatorsForISP(input); !reflect.DeepEqual(got, want) {
			t.Errorf("OperatorsForISP(%q) = %#v, want %#v", input, got, want)
		}
	}
}

func TestCanonicalNames(t *testing.T) {
	if got, ok := CanonicalProvince("广西壮族自治区"); !ok || got != "广西壮族自治区" {
		t.Fatalf("CanonicalProvince() = %q, %v", got, ok)
	}
	if got := CanonicalCity("北京市", "北京"); got != "北京市" {
		t.Fatalf("CanonicalCity() = %q", got)
	}
	if got := DisplayName("阿坝藏族羌族自治州"); got != "阿坝" {
		t.Fatalf("DisplayName() = %q", got)
	}
}
