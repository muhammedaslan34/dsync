package node

import (
	"testing"

	"dsync/internal/discovery"
)

func TestInfoHidesNameUntilCallerIsTrusted(t *testing.T) {
	a, b := newTestNode(t, "Laptop"), newTestNode(t, "Private workstation name")
	d, _, err := a.cl.Info(t.Context(), b.addr)
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != discovery.PublicName {
		t.Fatalf("unpaired info exposed name %q", d.Name)
	}
	if d.ID != b.cfg.ID || d.Port != b.cfg.Port {
		t.Fatalf("unpaired info cannot be used for pairing: %+v", d)
	}

	pair(a, b, b.addr)
	d, _, err = a.cl.Info(t.Context(), b.addr)
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != b.cfg.Name {
		t.Fatalf("trusted info name = %q, want %q", d.Name, b.cfg.Name)
	}
}
