package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"1.0.1", "1.0.0", true},
		{"1.2.10", "1.2.9", true},
		{"2.0.0", "1.9.9", true},
		{"1.0.0", "1.0.0", false},
		{"1.0.0", "1.0.1", false},
		{"1.0.0", "dev", true}, // a development build is older than any release
		{"garbage", "1.0.0", false},
		{"v1.1.0", "1.0.0", true},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

type fakeReleaseOptions struct {
	tamperAsset      bool
	tamperSums       bool
	tamperSignature  bool
	wrongKey         bool
	missingSignature bool
}

func testSigningKey(t *testing.T, seedByte byte) ed25519.PrivateKey {
	t.Helper()
	seed := bytes.Repeat([]byte{seedByte}, ed25519.SeedSize)
	privateKey := ed25519.NewKeyFromSeed(seed)
	publicDER, err := x509.MarshalPKIXPublicKey(privateKey.Public())
	if err != nil {
		t.Fatal(err)
	}
	old := releasePublicKeyDERBase64
	releasePublicKeyDERBase64 = base64.StdEncoding.EncodeToString(publicDER)
	t.Cleanup(func() { releasePublicKeyDERBase64 = old })
	return privateKey
}

// fakeGitHub serves a release with one asset and signed SHA256SUMS. It returns
// the number of requests for the asset itself, so tests can prove metadata is
// authenticated before an executable is fetched.
func fakeGitHub(t *testing.T, asset []byte, opts fakeReleaseOptions) *int {
	t.Helper()
	privateKey := testSigningKey(t, 1)
	if opts.wrongKey {
		testSigningKey(t, 2)
	}
	sum := sha256.Sum256(asset)
	sums := []byte(fmt.Sprintf("%s  other-file\n%s  dsync-1.0.2-linux-x86_64.tar.gz\n", strings.Repeat("0", 64), hex.EncodeToString(sum[:])))
	signature := ed25519.Sign(privateKey, sums)
	if opts.tamperSums {
		sums = append(sums, []byte("# changed after signing\n")...)
	}
	if opts.tamperSignature {
		signature[0] ^= 0xff
	}
	assetRequests := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/" + Repo + "/releases/latest":
			signatureAsset := fmt.Sprintf(`,{"name":"SHA256SUMS.sig","browser_download_url":"%s/signature","size":64}`, srv.URL)
			if opts.missingSignature {
				signatureAsset = ""
			}
			fmt.Fprintf(w, `{"tag_name":"v1.0.2","body":"notes","html_url":"https://example/rel","assets":[
				{"name":"dsync-1.0.2-linux-x86_64.tar.gz","browser_download_url":"%[1]s/a","size":%[2]d},
				{"name":"SHA256SUMS","browser_download_url":"%[1]s/sums","size":%[3]d}%[4]s]}`, srv.URL, len(asset), len(sums), signatureAsset)
		case "/a":
			assetRequests++
			if opts.tamperAsset {
				w.Write(append([]byte("x"), asset...))
				return
			}
			w.Write(asset)
		case "/sums":
			w.Write(sums)
		case "/signature":
			if opts.missingSignature {
				http.NotFound(w, r)
				return
			}
			w.Write(signature)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = old })
	return &assetRequests
}

func TestLatestAndDownload(t *testing.T) {
	data := bytes.Repeat([]byte("dsync"), 50000)
	fakeGitHub(t, data, fakeReleaseOptions{})
	r, err := Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "1.0.2" || r.URL != "https://example/rel" || len(r.Assets) != 3 {
		t.Fatalf("release: %+v", r)
	}
	var last int64
	path, err := Download(context.Background(), r, "dsync-1.0.2-linux-x86_64.tar.gz", t.TempDir(), func(d, total int64) { last = d })
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, data) || last != int64(len(data)) {
		t.Errorf("downloaded %d bytes, progress ended at %d", len(got), last)
	}
	if _, err := Download(context.Background(), r, "missing.exe", t.TempDir(), nil); err == nil {
		t.Error("a missing asset should fail")
	}
}

func TestDownloadRejectsTamperedFile(t *testing.T) {
	fakeGitHub(t, []byte("the real installer"), fakeReleaseOptions{tamperAsset: true})
	r, _ := Latest(context.Background())
	dir := t.TempDir()
	_, err := Download(context.Background(), r, "dsync-1.0.2-linux-x86_64.tar.gz", dir, nil)
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("want a checksum error, got %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Error("the bad download was left on disk")
	}
}

func TestDownloadAuthenticatesChecksumsBeforeFetchingAsset(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts fakeReleaseOptions
	}{
		{name: "tampered sums", opts: fakeReleaseOptions{tamperSums: true}},
		{name: "tampered signature", opts: fakeReleaseOptions{tamperSignature: true}},
		{name: "wrong key", opts: fakeReleaseOptions{wrongKey: true}},
		{name: "missing signature", opts: fakeReleaseOptions{missingSignature: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := fakeGitHub(t, []byte("installer"), tc.opts)
			r, err := Latest(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			_, err = Download(context.Background(), r, "dsync-1.0.2-linux-x86_64.tar.gz", t.TempDir(), nil)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "sig") {
				t.Fatalf("want a signature error, got %v", err)
			}
			if *requests != 0 {
				t.Fatalf("fetched the executable %d time(s) before authenticating metadata", *requests)
			}
		})
	}
}

func TestDownloadFailsClosedWithoutEmbeddedKey(t *testing.T) {
	fakeGitHub(t, []byte("installer"), fakeReleaseOptions{})
	releasePublicKeyDERBase64 = ""
	r, err := Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = Download(context.Background(), r, "dsync-1.0.2-linux-x86_64.tar.gz", t.TempDir(), nil)
	if err == nil || !strings.Contains(err.Error(), "no release verification public key") {
		t.Fatalf("want a missing-key error, got %v", err)
	}
}

func makeTarball(t *testing.T, files map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	p := filepath.Join(t.TempDir(), "u.tar.gz")
	os.WriteFile(p, buf.Bytes(), 0o644)
	return p
}

func TestInstallLocal(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "dsync-gui"), []byte("old gui"), 0o755)
	os.WriteFile(filepath.Join(dir, "dsync"), []byte("old cli"), 0o755)
	tb := makeTarball(t, map[string]string{
		"dsync-1.0.2-linux-x86_64/dsync-gui":  "new gui",
		"dsync-1.0.2-linux-x86_64/dsync":      "new cli",
		"dsync-1.0.2-linux-x86_64/install.sh": "not copied",
		"../dsync":                            "evil",
	})
	if err := installLocal(tb, dir); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"dsync-gui": "new gui", "dsync": "new cli"} {
		got, _ := os.ReadFile(filepath.Join(dir, name))
		if string(got) != want {
			t.Errorf("%s = %q", name, got)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "install.sh")); err == nil {
		t.Error("copied a file it shouldn't")
	}

	// An archive without both programs changes nothing.
	incomplete := makeTarball(t, map[string]string{"dsync-1.0.3-linux-x86_64/dsync": "cli only"})
	os.WriteFile(filepath.Join(dir, "dsync"), []byte("kept"), 0o755)
	if err := installLocal(incomplete, dir); err == nil {
		t.Error("an incomplete update should fail")
	}
}

func TestAssetNames(t *testing.T) {
	for kind, want := range map[string]string{
		KindInstaller: "dsync-setup-1.2.3-windows-amd64.exe",
		KindPacman:    "dsync-1.2.3-1-x86_64.pkg.tar.zst",
		KindLocal:     "dsync-1.2.3-linux-x86_64.tar.gz",
		KindMacApp:    "dsync-1.2.3-macos-universal.dmg",
		KindManual:    "",
	} {
		if got := AssetName(kind, "1.2.3"); got != want {
			t.Errorf("%s: %q", kind, got)
		}
	}
}

// When the API refuses (rate limit), the release is read from GitHub's
// website: the latest-release redirect and the release's SHA256SUMS.
func TestLatestFallsBackToWebsite(t *testing.T) {
	privateKey := testSigningKey(t, 3)
	data := bytes.Repeat([]byte("dsync"), 40000)
	sum := sha256.Sum256(data)
	name := "dsync-1.0.7-linux-x86_64.tar.gz"
	sums := []byte(fmt.Sprintf("%s  %s\n%s  ../evil\n%s  invalid-checksum\n", hex.EncodeToString(sum[:]), name, strings.Repeat("1", 64), strings.Repeat("g", 64)))
	signature := ed25519.Sign(privateKey, sums)
	var apiCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/" + Repo + "/releases/latest":
			apiCalls++
			http.Error(w, "API rate limit exceeded", http.StatusForbidden)
		case "/" + Repo + "/releases/latest":
			http.Redirect(w, r, "/"+Repo+"/releases/tag/v1.0.7", http.StatusFound)
		case "/" + Repo + "/releases/download/v1.0.7/SHA256SUMS":
			w.Write(sums)
		case "/" + Repo + "/releases/download/v1.0.7/SHA256SUMS.sig":
			w.Write(signature)
		case "/" + Repo + "/releases/download/v1.0.7/" + name:
			w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	oldAPI, oldWeb := apiBase, webBase
	apiBase, webBase = srv.URL, srv.URL
	t.Cleanup(func() { apiBase, webBase = oldAPI, oldWeb })

	r, err := Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if apiCalls != 1 || r.Version != "1.0.7" || !strings.HasSuffix(r.URL, "/releases/tag/v1.0.7") {
		t.Fatalf("release %+v after %d API calls", r, apiCalls)
	}
	if _, ok := r.asset("../evil"); ok {
		t.Error("a path in SHA256SUMS became an asset")
	}
	if _, ok := r.asset("invalid-checksum"); ok {
		t.Error("an invalid checksum entry became an asset")
	}
	if _, ok := r.asset(signatureName); !ok {
		t.Error("the detached signature was not selected as release metadata")
	}
	var last, total int64
	path, err := Download(context.Background(), r, name, t.TempDir(), func(d, tot int64) { last, total = d, tot })
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, data) || last != int64(len(data)) || total != int64(len(data)) {
		t.Errorf("downloaded %d bytes, progress %d/%d", len(got), last, total)
	}
}

// If both the API and the website fail, the error says it's GitHub's limit.
func TestRateLimitError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)
	oldAPI, oldWeb := apiBase, webBase
	apiBase, webBase = srv.URL, srv.URL
	t.Cleanup(func() { apiBase, webBase = oldAPI, oldWeb })
	_, err := Latest(context.Background())
	if err == nil || !strings.Contains(err.Error(), "limiting requests") {
		t.Fatalf("want a rate-limit error, got %v", err)
	}
}
