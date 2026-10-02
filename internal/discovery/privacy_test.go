package discovery

import (
	"testing"

	"dsync/internal/proto"
)

func TestPublicDeviceRedactsChosenName(t *testing.T) {
	original := proto.Device{ID: "stable-id", Name: "Muhammed's laptop", OS: "linux", Port: 47101}
	public := PublicDevice(original)
	if public.Name != PublicName {
		t.Fatalf("public name = %q", public.Name)
	}
	if public.ID != original.ID || public.OS != original.OS || public.Port != original.Port {
		t.Fatalf("pairing fields changed: %+v", public)
	}
	if original.Name != "Muhammed's laptop" {
		t.Fatal("PublicDevice mutated its input")
	}
}
