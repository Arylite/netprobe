package probe

import (
	"net/netip"
	"testing"
)

func TestDefaultPolicy(t *testing.T) {
	p, err := ParsePolicy(DefaultDeny)
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]bool{
		"127.0.0.1":          true,
		"127.255.255.255":    true,
		"::1":                true,
		"169.254.169.254":    true,
		"fe80::1":            true,
		"fe80::1%eth0":       true,
		"0.0.0.0":            true,
		"::":                 true,
		"224.0.0.1":          true,
		"ff02::1":            true,
		"::ffff:127.0.0.1":   true,
		"::ffff:10.0.0.1":    false,
		"10.0.0.1":           false,
		"192.168.1.1":        false,
		"203.0.113.7":        false,
		"2001:db8::1":        false,
		"::ffff:203.0.113.7": false,

		// A gateway turns these into the IPv4 address they carry.
		"64:ff9b::a9fe:a9fe":   true,  // NAT64 to 169.254.169.254
		"64:ff9b::7f00:1":      true,  // NAT64 to 127.0.0.1
		"64:ff9b:1::a9fe:a9fe": true,  // local-use NAT64 to 169.254.169.254
		"2002:a9fe:a9fe::1":    true,  // 6to4 of 169.254.169.254
		"2002:7f00:1::1":       true,  // 6to4 of 127.0.0.1
		"64:ff9b::cb00:7107":   false, // NAT64 to 203.0.113.7
		"2002:cb00:7107::1":    false, // 6to4 of 203.0.113.7

		// Metadata services that are not link-local.
		"100.100.100.200": true,
		"192.0.0.192":     true,
		"168.63.129.16":   true,
		"fd00:ec2::254":   true,
		"100.100.100.201": false,
		"168.63.129.17":   false,
		"fd00:ec2::255":   false,
	}
	for addr, want := range tests {
		if got := p.Denies(netip.MustParseAddr(addr)); got != want {
			t.Errorf("Denies(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestZeroPolicyDeniesNothing(t *testing.T) {
	if (Policy{}).Denies(netip.MustParseAddr("127.0.0.1")) {
		t.Fatal("the zero policy denied an address")
	}
}

func TestParsePolicyRejectsBadRanges(t *testing.T) {
	for _, bad := range []string{"", "10.0.0.1", "10.0.0.0/33", "nope"} {
		if _, err := ParsePolicy([]string{bad}); err == nil {
			t.Errorf("ParsePolicy(%q) succeeded", bad)
		}
	}
}

func TestControl(t *testing.T) {
	p, _ := ParsePolicy([]string{"10.0.0.0/8"})
	if err := p.Control("tcp", "10.1.2.3:80", nil); err == nil {
		t.Fatal("a denied address was allowed")
	}
	if err := p.Control("tcp", "192.0.2.1:80", nil); err != nil {
		t.Fatalf("an allowed address was denied: %v", err)
	}
	if err := p.Control("tcp", "[fe80::1%eth0]:80", nil); err != nil {
		t.Fatalf("zone address: %v", err)
	}
	if err := p.Control("tcp", "not-an-address", nil); err == nil {
		t.Fatal("a malformed address was allowed")
	}
}
