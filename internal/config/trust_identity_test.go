package config

import "testing"

func TestTrustRequiresUnpairBeforeIdentityChange(t *testing.T) {
	c := &Config{}
	original := TrustedPeer{ID: "device", Name: "Laptop", Fingerprint: "key-a", CanControl: true}
	if err := c.Trust(original); err != nil {
		t.Fatal(err)
	}
	if err := c.Trust(TrustedPeer{ID: original.ID, Name: "Imposter", Fingerprint: "key-b"}); err == nil {
		t.Fatal("same device id replaced a trusted key")
	}
	if got, _ := c.TrustedByID(original.ID); got != original {
		t.Fatalf("trusted identity changed to %+v", got)
	}
	if err := c.Trust(TrustedPeer{ID: "other-id", Name: "Imposter", Fingerprint: original.Fingerprint}); err == nil {
		t.Fatal("trusted key moved to another device id")
	}
	c.Untrust(original.ID)
	if err := c.Trust(TrustedPeer{ID: original.ID, Name: "Replacement", Fingerprint: "key-b"}); err != nil {
		t.Fatalf("trust after explicit unpair: %v", err)
	}
}
