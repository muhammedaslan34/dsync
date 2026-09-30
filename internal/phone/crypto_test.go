package phone

import "testing"

// The vectors in docs/phone-protocol.md; the phone app checks the same ones.
func TestVectors(t *testing.T) {
	var key [KeySize]byte
	var nonce [nonceSize]byte
	for i := range key {
		key[i] = byte(i)
	}
	for i := range nonce {
		nonce[i] = byte(100 + i)
	}
	for plain, want := range map[string]string{
		`{"t":1790752000000,"b":{"text":"héllo dsync"}}`: "ZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXp7Ju5EbL6NXn4buApjwG6t8nmb7esAh/nQgMoWpge0kygOmhSFhoRxYIR4J+x+qfOqwKxddCNYu4CGb3kBIm6g",
		"": "ZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXp79JVy1hlCgePIf7tOIQaTLA==",
	} {
		if got := sealWithNonce(&key, &nonce, []byte(plain)); got != want {
			t.Errorf("seal %q = %s", plain, got)
		}
		got, _, err := Open(&key, want)
		if err != nil || string(got) != plain {
			t.Errorf("open: %q %v", got, err)
		}
	}
	wrong := key
	wrong[0] ^= 1
	if _, _, err := Open(&wrong, "ZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXp79JVy1hlCgePIf7tOIQaTLA=="); err == nil {
		t.Error("a wrong key must not open it")
	}
}
