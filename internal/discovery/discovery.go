// Package discovery finds other dsync devices on the local network using UDP
// broadcast: a client broadcasts a "discover" packet and every running
// service answers with an "announce" packet sent straight back to it.
package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"time"

	"dsync/internal/proto"
)

// Peer is a device that answered a discovery request.
type Peer struct {
	proto.Device
	IP net.IP
}

// Addr is the host:port of the peer's HTTP service.
func (p Peer) Addr() string {
	return net.JoinHostPort(p.IP.String(), strconv.Itoa(p.Port))
}

// Respond answers discovery requests until ctx is cancelled. self is called
// for every reply so a renamed device is announced under its new name.
func Respond(ctx context.Context, self func() proto.Device) error {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: proto.DiscoveryPort})
	if err != nil {
		return fmt.Errorf("listen udp :%d: %w", proto.DiscoveryPort, err)
	}
	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	buf := make([]byte, 2048)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, net.ErrClosed) {
				return err
			}
			// Windows reports ICMP "port unreachable" as a read error; ignore it.
			continue
		}
		me := self()
		var p proto.Packet
		if json.Unmarshal(buf[:n], &p) != nil || p.Type != proto.TypeDiscover || p.ID == me.ID {
			continue
		}
		reply, _ := json.Marshal(proto.Packet{Type: proto.TypeAnnounce, Version: proto.Version, Device: me})
		conn.WriteToUDP(reply, from)
	}
}

// Discover broadcasts a request and collects answers for the given duration.
func Discover(ctx context.Context, self proto.Device, wait time.Duration) ([]Peer, error) {
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	req, _ := json.Marshal(proto.Packet{Type: proto.TypeDiscover, Version: proto.Version, Device: self})
	targets := broadcastAddrs()
	send := func() {
		for _, ip := range targets {
			conn.WriteToUDP(req, &net.UDPAddr{IP: ip, Port: proto.DiscoveryPort})
		}
	}

	deadline := time.Now().Add(wait)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetReadDeadline(deadline)

	// UDP can drop packets, so ask a few times.
	send()
	go func() {
		for range 2 {
			select {
			case <-time.After(wait / 4):
				send()
			case <-ctx.Done():
				return
			}
		}
	}()

	found := map[string]Peer{}
	buf := make([]byte, 2048)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				break
			}
			if errors.Is(err, net.ErrClosed) {
				return nil, err
			}
			continue
		}
		var p proto.Packet
		if json.Unmarshal(buf[:n], &p) != nil || p.Type != proto.TypeAnnounce || p.ID == self.ID {
			continue
		}
		found[p.ID] = Peer{Device: p.Device, IP: from.IP}
	}

	peers := make([]Peer, 0, len(found))
	for _, p := range found {
		peers = append(peers, p)
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].Name < peers[j].Name })
	return peers, nil
}

// broadcastAddrs returns the limited broadcast address plus the directed
// broadcast address of every IPv4 network we're on, so the request goes out
// on all interfaces (Windows only uses one for 255.255.255.255).
func broadcastAddrs() []net.IP {
	ips := []net.IP{net.IPv4bcast}
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagBroadcast == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipn.IP.To4()
			if ip4 == nil {
				continue
			}
			mask := ipn.Mask
			if len(mask) == net.IPv6len {
				mask = mask[12:]
			}
			b := make(net.IP, net.IPv4len)
			for i := range b {
				b[i] = ip4[i] | ^mask[i]
			}
			ips = append(ips, b)
		}
	}
	return ips
}
