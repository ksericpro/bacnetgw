package bacnet

import (
	"fmt"
	"net"
	"strings"
)

// SubnetInfo describes a detected local IPv4 interface and its directed broadcast address.
type SubnetInfo struct {
	InterfaceName string `json:"interface_name"`
	LocalIP       net.IP `json:"local_ip"`
	CIDR          string `json:"cidr"`         // Interface IP with prefix length, e.g. "192.168.0.154/24"
	NetworkCIDR   string `json:"network_cidr"` // Masked network CIDR, e.g. "192.168.0.0/24"
	BroadcastIP   net.IP `json:"broadcast_ip"`
}

// DetectSubnets enumerates active non-loopback IPv4 interfaces and calculates each subnet's directed broadcast IP.
// If subnetSelector or bindIP is non-empty (and not "0.0.0.0"), only matching interfaces/subnets are returned.
// subnetSelector accepts an interface name ("Ethernet 2"), network CIDR ("192.168.0.0/24"),
// interface CIDR ("192.168.0.154/24"), IP address ("192.168.0.154" or "192.168.0.160"), or prefix ("192.168.0").
func DetectSubnets(subnetSelector string, bindIP string) ([]SubnetInfo, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("enumerate network interfaces: %w", err)
	}

	var targetIP net.IP
	if bindIP != "" && bindIP != "0.0.0.0" {
		targetIP = net.ParseIP(bindIP)
	}

	var all []SubnetInfo
	for _, iface := range ifaces {
		if (iface.Flags & net.FlagUp) == 0 {
			continue
		}
		if (iface.Flags & net.FlagLoopback) != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet == nil {
				continue
			}
			ip4 := ipNet.IP.To4()
			if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
				continue
			}
			if targetIP != nil && !ip4.Equal(targetIP) {
				continue
			}

			mask := ipNet.Mask
			if len(mask) == 16 {
				mask = mask[12:16]
			}
			if len(mask) != 4 {
				continue
			}

			netIP := make(net.IP, 4)
			bcast := make(net.IP, 4)
			for i := 0; i < 4; i++ {
				netIP[i] = ip4[i] & mask[i]
				bcast[i] = ip4[i] | ^mask[i]
			}
			maskedNet := &net.IPNet{IP: netIP, Mask: mask}

			all = append(all, SubnetInfo{
				InterfaceName: iface.Name,
				LocalIP:       ip4,
				CIDR:          ipNet.String(),
				NetworkCIDR:   maskedNet.String(),
				BroadcastIP:   bcast,
			})
		}
	}

	return FilterSubnets(all, subnetSelector), nil
}

// FilterSubnets filters a slice of SubnetInfo using a flexible user selector string.
// Empty selector, "*", or "all" returns all subnets unchanged.
func FilterSubnets(subnets []SubnetInfo, selector string) []SubnetInfo {
	sel := strings.TrimSpace(selector)
	if sel == "" || sel == "*" || strings.EqualFold(sel, "all") {
		out := make([]SubnetInfo, len(subnets))
		copy(out, subnets)
		return out
	}

	var matched []SubnetInfo
	for _, sn := range subnets {
		if MatchesSubnet(sn, sel) {
			matched = append(matched, sn)
		}
	}
	return matched
}

// MatchesSubnet reports whether sn matches selector (interface name, CIDR, IP in subnet, or octet prefix).
func MatchesSubnet(sn SubnetInfo, selector string) bool {
	sel := strings.TrimSpace(selector)
	if sel == "" || sel == "*" || strings.EqualFold(sel, "all") {
		return true
	}

	// 1. Interface name match (case-insensitive)
	if strings.EqualFold(sn.InterfaceName, sel) {
		return true
	}

	// 2. Exact CIDR strings ("192.168.0.0/24" or "192.168.0.154/24")
	if strings.EqualFold(sn.NetworkCIDR, sel) || strings.EqualFold(sn.CIDR, sel) {
		return true
	}

	// Parse the subnet's canonical IPNet
	_, snNet, snErr := net.ParseCIDR(sn.NetworkCIDR)
	if snErr != nil {
		if ip, parsedNet, err := net.ParseCIDR(sn.CIDR); err == nil && ip != nil {
			snNet = parsedNet
		}
	}

	// 3. Selector given in CIDR notation (e.g. "192.168.0.0/24" or "192.168.0.10/24")
	if selIP, selNet, err := net.ParseCIDR(sel); err == nil {
		if snNet != nil {
			snOnes, snBits := snNet.Mask.Size()
			selOnes, selBits := selNet.Mask.Size()
			if snOnes == selOnes && snBits == selBits && snNet.IP.Equal(selNet.IP.To4()) {
				return true
			}
			if snNet.Contains(selIP) || selNet.Contains(sn.LocalIP) {
				return true
			}
		}
		return false
	}

	// 4. Selector given as an IPv4 address (local IP, broadcast IP, network IP, or target controller IP)
	if ip := net.ParseIP(sel); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			if sn.LocalIP.Equal(ip4) || sn.BroadcastIP.Equal(ip4) {
				return true
			}
			if snNet != nil && snNet.Contains(ip4) {
				return true
			}
			return false
		}
	}

	// 5. Octet prefix match (e.g. "192.168.0" or "192.168.0.")
	prefix := strings.TrimSuffix(sel, ".") + "."
	if strings.HasPrefix(sn.NetworkCIDR, prefix) ||
		strings.HasPrefix(sn.CIDR, prefix) ||
		strings.HasPrefix(sn.LocalIP.String(), prefix) {
		return true
	}

	return false
}

// SubnetContainsIP reports whether ip belongs to at least one of the provided subnets.
func SubnetContainsIP(subnets []SubnetInfo, ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	for _, sn := range subnets {
		if sn.LocalIP.Equal(ip4) || sn.BroadcastIP.Equal(ip4) {
			return true
		}
		cidrStr := sn.NetworkCIDR
		if cidrStr == "" {
			cidrStr = sn.CIDR
		}
		if _, snNet, err := net.ParseCIDR(cidrStr); err == nil && snNet.Contains(ip4) {
			return true
		}
	}
	return false
}
