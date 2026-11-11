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
	CIDR          string `json:"cidr"`
	BroadcastIP   net.IP `json:"broadcast_ip"`
}

// DetectSubnets enumerates active non-loopback IPv4 interfaces and calculates each subnet's directed broadcast IP.
// If interfaceFilter or bindIP is non-empty (and not "0.0.0.0"), only matching interfaces are returned.
func DetectSubnets(interfaceFilter string, bindIP string) ([]SubnetInfo, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("enumerate network interfaces: %w", err)
	}

	filterIface := strings.TrimSpace(strings.ToLower(interfaceFilter))
	var targetIP net.IP
	if bindIP != "" && bindIP != "0.0.0.0" {
		targetIP = net.ParseIP(bindIP)
	}

	var results []SubnetInfo
	for _, iface := range ifaces {
		if (iface.Flags & net.FlagUp) == 0 {
			continue
		}
		if (iface.Flags & net.FlagLoopback) != 0 {
			continue
		}
		if filterIface != "" && strings.ToLower(iface.Name) != filterIface {
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

			bcast := make(net.IP, 4)
			for i := 0; i < 4; i++ {
				bcast[i] = ip4[i] | ^mask[i]
			}

			results = append(results, SubnetInfo{
				InterfaceName: iface.Name,
				LocalIP:       ip4,
				CIDR:          ipNet.String(),
				BroadcastIP:   bcast,
			})
		}
	}

	return results, nil
}

