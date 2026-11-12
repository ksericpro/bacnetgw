package bacnet

import (
	"net"
	"testing"
)

func TestFilterSubnetsAndContainsIP(t *testing.T) {
	sample := []SubnetInfo{
		{
			InterfaceName: "vEthernet (WSL)",
			LocalIP:       net.ParseIP("172.22.96.1").To4(),
			CIDR:          "172.22.96.1/20",
			NetworkCIDR:   "172.22.96.0/20",
			BroadcastIP:   net.ParseIP("172.22.111.255").To4(),
		},
		{
			InterfaceName: "Ethernet 2",
			LocalIP:       net.ParseIP("192.168.0.154").To4(),
			CIDR:          "192.168.0.154/24",
			NetworkCIDR:   "192.168.0.0/24",
			BroadcastIP:   net.ParseIP("192.168.0.255").To4(),
		},
		{
			InterfaceName: "Wi-Fi",
			LocalIP:       net.ParseIP("10.65.3.100").To4(),
			CIDR:          "10.65.3.100/24",
			NetworkCIDR:   "10.65.3.0/24",
			BroadcastIP:   net.ParseIP("10.65.3.255").To4(),
		},
	}

	cases := []struct {
		selector      string
		wantCount     int
		wantInterface string
	}{
		{selector: "", wantCount: 3},
		{selector: "all", wantCount: 3},
		{selector: "192.168.0.0/24", wantCount: 1, wantInterface: "Ethernet 2"},
		{selector: "192.168.0.154/24", wantCount: 1, wantInterface: "Ethernet 2"},
		{selector: "192.168.0.154", wantCount: 1, wantInterface: "Ethernet 2"},
		{selector: "192.168.0.160", wantCount: 1, wantInterface: "Ethernet 2"}, // Controller IP on same subnet
		{selector: "192.168.0.255", wantCount: 1, wantInterface: "Ethernet 2"},
		{selector: "192.168.0", wantCount: 1, wantInterface: "Ethernet 2"},
		{selector: "ethernet 2", wantCount: 1, wantInterface: "Ethernet 2"},
		{selector: "Wi-Fi", wantCount: 1, wantInterface: "Wi-Fi"},
		{selector: "10.65.3", wantCount: 1, wantInterface: "Wi-Fi"},
		{selector: "172.22.96.0/20", wantCount: 1, wantInterface: "vEthernet (WSL)"},
		{selector: "10.99.99.0/24", wantCount: 0},
	}

	for _, tc := range cases {
		got := FilterSubnets(sample, tc.selector)
		if len(got) != tc.wantCount {
			t.Fatalf("selector %q: got %d matches, want %d", tc.selector, len(got), tc.wantCount)
		}
		if tc.wantCount == 1 && got[0].InterfaceName != tc.wantInterface {
			t.Fatalf("selector %q: got interface %q, want %q", tc.selector, got[0].InterfaceName, tc.wantInterface)
		}
	}

	ethOnly := FilterSubnets(sample, "192.168.0.0/24")
	if !SubnetContainsIP(ethOnly, net.ParseIP("192.168.0.160")) {
		t.Fatalf("expected 192.168.0.160 to belong to 192.168.0.0/24")
	}
	if SubnetContainsIP(ethOnly, net.ParseIP("10.65.3.50")) {
		t.Fatalf("did not expect 10.65.3.50 to belong to 192.168.0.0/24")
	}
}

