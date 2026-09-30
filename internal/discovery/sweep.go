package discovery

import (
	"context"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"dsync/internal/proto"
)

// LocalAddr is one of this machine's IPv4 addresses.
type LocalAddr struct {
	IP        string `json:"ip"`
	Kind      string `json:"kind"` // "Wi-Fi", "Ethernet", "Tailscale", or the interface name
	Interface string `json:"interface"`
	prefix    netip.Prefix
}

var tailscaleRange = netip.MustParsePrefix("100.64.0.0/10")

// LocalAddrs lists the addresses other devices can reach this one on,
// skipping loopback, link-local and virtual (Docker, VM, WSL) interfaces.
func LocalAddrs() []LocalAddr {
	var out []LocalAddr
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || isVirtual(ifc.Name) {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip, ok := netip.AddrFromSlice(ipn.IP.To4())
			if !ok || ip.IsLinkLocalUnicast() {
				continue
			}
			bits, _ := ipn.Mask.Size()
			if len(ipn.Mask) == net.IPv6len {
				bits -= 96
			}
			out = append(out, LocalAddr{
				IP:        ip.String(),
				Kind:      interfaceKind(ifc.Name, ip),
				Interface: ifc.Name,
				prefix:    netip.PrefixFrom(ip, bits).Masked(),
			})
		}
	}
	// Real LANs first, VPNs after.
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i].Kind) < rank(out[j].Kind) })
	return out
}

func isVirtual(name string) bool {
	n := strings.ToLower(name)
	for _, p := range []string{"docker", "br-", "veth", "virbr", "vboxnet", "vmnet", "vethernet", "lxc", "podman", "cni", "flannel"} {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return strings.Contains(n, "wsl") || strings.Contains(n, "hyper-v")
}

func interfaceKind(name string, ip netip.Addr) string {
	n := strings.ToLower(name)
	switch {
	case tailscaleRange.Contains(ip) || strings.HasPrefix(n, "tailscale"):
		return "Tailscale"
	case strings.HasPrefix(n, "zt"):
		return "ZeroTier"
	case strings.HasPrefix(n, "wl") || strings.Contains(n, "wi-fi") || strings.Contains(n, "wireless") || strings.Contains(n, "wlan"):
		return "Wi-Fi"
	case strings.HasPrefix(n, "en") || strings.HasPrefix(n, "eth") || strings.Contains(n, "ethernet"):
		return "Ethernet"
	}
	return name
}

func rank(kind string) int {
	switch kind {
	case "Ethernet", "Wi-Fi":
		return 0
	case "Tailscale", "ZeroTier":
		return 2
	}
	return 1
}

// Sweep finds dsync devices by asking every address on the local networks
// directly over HTTP. It is slower than broadcast discovery but works when a
// firewall or router drops broadcasts. Networks larger than /24 are limited
// to the /24 around this machine's address.
func Sweep(ctx context.Context, selfID string, port int, info func(ctx context.Context, addr string) (proto.Device, error)) []Peer {
	var targets []netip.Addr
	own := map[netip.Addr]bool{}
	for _, la := range LocalAddrs() {
		ip := netip.MustParseAddr(la.IP)
		own[ip] = true
		p := la.prefix
		if p.Bits() >= 31 { // point-to-point, e.g. Tailscale's /32
			continue
		}
		if p.Bits() < 24 {
			p = netip.PrefixFrom(ip, 24).Masked()
		}
		for a := p.Addr().Next(); p.Contains(a); a = a.Next() {
			targets = append(targets, a)
		}
	}

	var (
		mu    sync.Mutex
		found = map[string]Peer{}
		wg    sync.WaitGroup
		sem   = make(chan struct{}, 128)
	)
	for _, ip := range targets {
		if own[ip] || ip.As4()[3] == 255 {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			addr := net.JoinHostPort(ip.String(), strconv.Itoa(port))
			reqCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
			defer cancel()
			d, err := info(reqCtx, addr)
			if err != nil || d.ID == "" || d.ID == selfID {
				return
			}
			mu.Lock()
			found[d.ID] = Peer{Device: d, IP: net.IP(ip.AsSlice())}
			mu.Unlock()
		}()
	}
	wg.Wait()

	peers := make([]Peer, 0, len(found))
	for _, p := range found {
		peers = append(peers, p)
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].Name < peers[j].Name })
	return peers
}
