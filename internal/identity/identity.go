// Package identity manages this device's TLS key pair. Devices trust each
// other by the SHA-256 fingerprint of their public key, which is pinned at
// pairing time, so the certificates are self-signed and never expire in
// practice.
package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const fileName = "identity.pem"

// Identity is this device's certificate and its fingerprint.
type Identity struct {
	Cert        tls.Certificate
	Fingerprint string // hex SHA-256 of the public key
}

// Load reads the identity from dir, creating one on first use.
func Load(dir string) (*Identity, error) {
	path := filepath.Join(dir, fileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		data, err = generate()
		if err == nil {
			if err = os.MkdirAll(dir, 0o700); err == nil {
				err = os.WriteFile(path, data, 0o600)
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("identity: %w", err)
	}
	cert, err := tls.X509KeyPair(data, data)
	if err != nil {
		return nil, fmt.Errorf("identity: parse %s: %w", path, err)
	}
	fp, err := Fingerprint(cert.Certificate[0])
	if err != nil {
		return nil, err
	}
	return &Identity{Cert: cert, Fingerprint: fp}, nil
}

// generate makes a new ECDSA P-256 key and self-signed certificate, returned
// as PEM with the certificate followed by the key.
func generate() ([]byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "dsync device"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(50, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	out := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return append(out, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})...), nil
}

// Fingerprint returns the hex SHA-256 of a DER certificate's public key.
func Fingerprint(certDER []byte) (string, error) {
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return hex.EncodeToString(sum[:]), nil
}

// PairCode is the 6-digit code both devices show while pairing. It depends
// on both fingerprints, so a machine in the middle, which must present its
// own key to each side, makes the two screens show different codes.
func PairCode(fpA, fpB string) string {
	if fpA > fpB {
		fpA, fpB = fpB, fpA
	}
	sum := sha256.Sum256([]byte("dsync-pair-v1\x00" + fpA + "\x00" + fpB))
	n := binary.BigEndian.Uint64(sum[:8]) % 1_000_000
	s := fmt.Sprintf("%06d", n)
	return s[:3] + " " + s[3:]
}

// Short formats a fingerprint for display, e.g. "3F9A 12C4 7B0E 55D1".
func Short(fp string) string {
	fp = strings.ToUpper(fp)
	if len(fp) > 16 {
		fp = fp[:16]
	}
	var parts []string
	for i := 0; i < len(fp); i += 4 {
		parts = append(parts, fp[i:min(i+4, len(fp))])
	}
	return strings.Join(parts, " ")
}
