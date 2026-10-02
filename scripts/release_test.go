package scripts

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Execute the actual publishing step with local release downloads and a
// recorded upload boundary. OpenSSL performs the real signing/verification.
func TestReleasePublishAuthenticatesExistingChecksums(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("release publishing uses Ubuntu shell tools")
	}
	for _, tool := range []string{"bash", "openssl"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is required to exercise release publishing", tool)
		}
	}
	workflow, err := os.ReadFile("../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	_, step, ok := strings.Cut(string(workflow), "      - name: Attach to the release\n")
	if !ok {
		t.Fatal("release publish step missing")
	}
	_, script, ok := strings.Cut(step, "        run: |\n")
	if !ok {
		t.Fatal("release publish script missing")
	}
	var commands []string
	for _, line := range strings.Split(strings.TrimSuffix(script, "\n"), "\n") {
		if !strings.HasPrefix(line, "          ") {
			break
		}
		commands = append(commands, strings.TrimPrefix(line, "          "))
	}
	script = strings.Join(commands, "\n")
	privateKey := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	publicDER, err := x509.MarshalPKIXPublicKey(privateKey.Public())
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})
	original := []byte(strings.Repeat("1", 64) + "  existing-linux.tar.gz\n")
	for _, mode := range []string{"valid", "tampered checksums", "tampered signature", "missing signature", "wrong signing key"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			fixtures := filepath.Join(dir, "fixtures")
			tools := filepath.Join(dir, "tools")
			for _, path := range []string{fixtures, tools, filepath.Join(dir, "out"), filepath.Join(dir, "temp")} {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			write := func(path string, data []byte, mode os.FileMode) {
				t.Helper()
				if err := os.WriteFile(path, data, mode); err != nil {
					t.Fatal(err)
				}
			}
			sums := append([]byte(nil), original...)
			signature := ed25519.Sign(privateKey, sums)
			switch mode {
			case "tampered checksums":
				sums[0] = '2'
			case "tampered signature":
				signature[0] ^= 0xff
			case "wrong signing key":
				seed := make([]byte, ed25519.SeedSize)
				seed[0] = 1
				signature = ed25519.Sign(ed25519.NewKeyFromSeed(seed), sums)
			}
			write(filepath.Join(fixtures, "SHA256SUMS"), sums, 0o600)
			if mode != "missing signature" {
				write(filepath.Join(fixtures, "SHA256SUMS.sig"), signature, 0o600)
			}
			asset := []byte("new macOS release asset")
			write(filepath.Join(dir, "out", "dsync-macos.dmg"), asset, 0o600)
			write(filepath.Join(tools, "gh"), []byte(`#!/bin/bash
set -euo pipefail
[ "$1" = release ]
case "$2" in
  download)
    shift 3
    patterns=()
    dest=.
    while [ "$#" -gt 0 ]; do
      case "$1" in
        --pattern) patterns+=("$2"); shift 2 ;;
        --dir) dest="$2"; shift 2 ;;
        --clobber) shift ;;
        *) exit 2 ;;
      esac
    done
    for pattern in "${patterns[@]}"; do
      cp "$RELEASE_FIXTURE/$pattern" "$dest/$pattern"
    done ;;
  upload) printf '%s\n' "$*" >> "$UPLOAD_LOG" ;;
  *) exit 2 ;;
esac
`), 0o700)
			cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", script)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(),
				"PATH="+tools+string(os.PathListSeparator)+os.Getenv("PATH"),
				"RUNNER_TEMP="+filepath.Join(dir, "temp"),
				"DSYNC_UPDATE_PUBLIC_KEY="+base64.StdEncoding.EncodeToString(publicDER),
				"DSYNC_UPDATE_PRIVATE_KEY_PEM="+string(privatePEM),
				"RELEASE_FIXTURE="+fixtures, "UPLOAD_LOG="+filepath.Join(dir, "uploads"), "TAG=v1.2.3")
			out, runErr := cmd.CombinedOutput()
			if mode != "valid" {
				if runErr == nil {
					t.Fatalf("published unauthenticated metadata:\n%s", out)
				}
				if uploads, err := os.ReadFile(filepath.Join(dir, "uploads")); !os.IsNotExist(err) {
					t.Fatalf("attempted upload after authentication failure: %s, %v", uploads, err)
				}
				return
			}
			if runErr != nil {
				t.Fatalf("valid release failed: %v\n%s", runErr, out)
			}
			updated, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
			if err != nil {
				t.Fatal(err)
			}
			want := string(original) + fmt.Sprintf("%x  dsync-macos.dmg\n", sha256.Sum256(asset))
			if string(updated) != want {
				t.Fatalf("updated checksums = %q, want %q", updated, want)
			}
			sig, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS.sig"))
			if err != nil || !ed25519.Verify(privateKey.Public().(ed25519.PublicKey), updated, sig) {
				t.Fatalf("updated metadata lacks a valid signature: %v", err)
			}
			if uploads, err := os.ReadFile(filepath.Join(dir, "uploads")); err != nil || strings.Count(string(uploads), "release upload") != 2 {
				t.Fatalf("valid release uploads: %s, %v", uploads, err)
			}
		})
	}
}
