package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
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

// fakeGitHub serves a release with one asset and SHA256SUMS. tamper makes
// the asset's content differ from its checksum.
func fakeGitHub(t *testing.T, asset []byte, tamper bool) {
	t.Helper()
	sum := sha256.Sum256(asset)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/" + Repo + "/releases/latest":
			fmt.Fprintf(w, `{"tag_name":"v1.0.2","body":"notes","html_url":"https://example/rel","assets":[
				{"name":"dsync-1.0.2-linux-x86_64.tar.gz","browser_download_url":"%[1]s/a","size":%[2]d},
				{"name":"SHA256SUMS","browser_download_url":"%[1]s/sums","size":10}]}`, srv.URL, len(asset))
		case "/a":
			if tamper {
				w.Write(append([]byte("x"), asset...))
				return
			}
			w.Write(asset)
		case "/sums":
			fmt.Fprintf(w, "%s  other-file\n%s  dsync-1.0.2-linux-x86_64.tar.gz\n", strings.Repeat("0", 64), hex.EncodeToString(sum[:]))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = old })
}

func TestLatestAndDownload(t *testing.T) {
	data := bytes.Repeat([]byte("dsync"), 50000)
	fakeGitHub(t, data, false)
	r, err := Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "1.0.2" || r.URL != "https://example/rel" || len(r.Assets) != 2 {
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
	fakeGitHub(t, []byte("the real installer"), true)
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
		KindManual:    "",
	} {
		if got := AssetName(kind, "1.2.3"); got != want {
			t.Errorf("%s: %q", kind, got)
		}
	}
}
