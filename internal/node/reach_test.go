package node

import (
	"context"
	"testing"
)

func peerState(n *Node, id string) Peer {
	for _, p := range n.Peers() {
		if p.ID == id {
			return p
		}
	}
	return Peer{}
}

// Broadcast discovery doesn't reach the test nodes, so finding the peer
// online proves the direct check at its last address works.
func TestScanFindsPairedPeerWithoutBroadcast(t *testing.T) {
	a, b := newTestNode(t, "Laptop"), newTestNode(t, "PC")
	pair(a, b, b.addr)
	a.peers[b.cfg.ID].Online = false
	a.peers[b.cfg.ID].LastSeen = 0

	a.Scan(context.Background())
	if p := peerState(a.Node, b.cfg.ID); !p.Online || p.OneWay {
		t.Fatalf("want online, got %+v", p)
	}
}

// The PC can reach the laptop but the laptop can't reach the PC (its
// firewall blocks incoming connections): the laptop must say so rather
// than just "offline".
func TestOneWayReachability(t *testing.T) {
	laptop, pc := newTestNode(t, "Laptop"), newTestNode(t, "PC")
	pair(laptop, pc, pc.addr)
	pair(pc, laptop, laptop.addr)

	pc.srv.Close() // the PC's firewall now blocks everything coming in
	if err := pc.SendText(context.Background(), laptop.cfg.ID, "here's the file"); err != nil {
		t.Fatalf("PC -> laptop should still work: %v", err)
	}
	laptop.peers[pc.cfg.ID].LastSeen = 0 // long since we last reached it
	laptop.Scan(context.Background())

	p := peerState(laptop.Node, pc.cfg.ID)
	if p.Online || !p.OneWay {
		t.Fatalf("want one-way (not online), got online=%v oneWay=%v", p.Online, p.OneWay)
	}

	// A device we haven't heard from recently is plain offline.
	laptop.peers[pc.cfg.ID].LastHeard = 0
	laptop.Scan(context.Background())
	if p := peerState(laptop.Node, pc.cfg.ID); p.Online || p.OneWay {
		t.Fatalf("want plain offline, got %+v", p)
	}
}
