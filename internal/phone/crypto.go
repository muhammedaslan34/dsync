// Package phone lets the dsync phone app (Expo Go) talk to a computer.
// Expo Go can't pin TLS certificates or use UDP, so phones use plain HTTP
// on their own port, with every body encrypted and authenticated with a
// key shared once through a QR code (NaCl secretbox: XSalsa20-Poly1305).
// See docs/phone-protocol.md.
package phone

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"golang.org/x/crypto/nacl/secretbox"
)

const (
	KeySize   = 32
	nonceSize = 24
	// maxAge is how old a sealed message may be; older ones are refused
	// so recorded requests can't be replayed later.
	maxAge = 2 * time.Minute
)

var errBadBox = errors.New("message could not be opened")

// Seal encrypts data with key as base64(nonce || secretbox).
func Seal(key *[KeySize]byte, data []byte) string {
	var nonce [nonceSize]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		panic(err)
	}
	return sealWithNonce(key, &nonce, data)
}

func sealWithNonce(key *[KeySize]byte, nonce *[nonceSize]byte, data []byte) string {
	out := secretbox.Seal(nonce[:], data, nonce, key)
	return base64.StdEncoding.EncodeToString(out)
}

// Open decrypts what Seal produced, returning the nonce too (for replay
// checks).
func Open(key *[KeySize]byte, sealed string) ([]byte, [nonceSize]byte, error) {
	var nonce [nonceSize]byte
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil || len(raw) < nonceSize+secretbox.Overhead {
		return nil, nonce, errBadBox
	}
	copy(nonce[:], raw[:nonceSize])
	data, ok := secretbox.Open(nil, raw[nonceSize:], &nonce, key)
	if !ok {
		return nil, nonce, errBadBox
	}
	return data, nonce, nil
}

// Envelope is the JSON inside every sealed request and response.
type Envelope struct {
	Time int64           `json:"t"` // unix ms when sealed
	Body json.RawMessage `json:"b"`
}

// SealJSON seals v with the current time.
func SealJSON(key *[KeySize]byte, v any) (string, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	env, _ := json.Marshal(Envelope{Time: time.Now().UnixMilli(), Body: body})
	return Seal(key, env), nil
}

// OpenJSON opens a sealed envelope into v, refusing ones that are too old
// or from the future.
func OpenJSON(key *[KeySize]byte, sealed string, v any) ([nonceSize]byte, error) {
	data, nonce, err := Open(key, sealed)
	if err != nil {
		return nonce, err
	}
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nonce, errBadBox
	}
	age := time.Since(time.UnixMilli(env.Time))
	if age > maxAge || age < -maxAge {
		return nonce, errors.New("message too old or clock too far off")
	}
	return nonce, json.Unmarshal(env.Body, v)
}

// NewKey makes a random key for a new phone.
func NewKey() *[KeySize]byte {
	var k [KeySize]byte
	if _, err := rand.Read(k[:]); err != nil {
		panic(err)
	}
	return &k
}
