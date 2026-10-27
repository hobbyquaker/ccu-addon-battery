package ifaces

import "testing"

func TestSupported(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"BidCos-RF", true},
		{"HmIP-RF", true},
		{"BidCos-Wired", false},
		{"VirtualDevices", false},
		{"CUxD", false},
		{"hmip-rf", false},
		{"", false},
	} {
		if got := IsSupported(tc.name); got != tc.want {
			t.Errorf("IsSupported(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
	if got := SQL("interface"); got != "interface IN ('BidCos-RF', 'HmIP-RF')" {
		t.Errorf("SQL: %s", got)
	}
}
