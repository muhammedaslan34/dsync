// Package update checks GitHub for a newer dsync release, downloads the
// right file for how this copy was installed, verifies it against the
// release's SHA256SUMS, and installs it.
package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Repo is where releases are published.
const Repo = "muhammedaslan34/dsync"

// apiBase is a variable so tests can point it at a fake GitHub.
var apiBase = "https://api.github.com"

var client = &http.Client{Timeout: 30 * time.Second}

// Release is a published version.
type Release struct {
	Version string  `json:"version"` // e.g. "1.0.2"
	Notes   string  `json:"notes"`
	URL     string  `json:"url"` // the release page
	Assets  []Asset `json:"-"`
}

type Asset struct {
	Name string
	URL  string
	Size int64
}

func (r Release) asset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// Latest returns the newest published release.
func Latest(ctx context.Context) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/repos/"+Repo+"/releases/latest", nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "dsync-updater")
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("could not reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var gh struct {
		Tag    string `json:"tag_name"`
		Body   string `json:"body"`
		URL    string `json:"html_url"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&gh); err != nil {
		return Release{}, err
	}
	r := Release{Version: strings.TrimPrefix(gh.Tag, "v"), Notes: gh.Body, URL: gh.URL}
	for _, a := range gh.Assets {
		r.Assets = append(r.Assets, Asset{Name: a.Name, URL: a.URL, Size: a.Size})
	}
	return r, nil
}

// Newer reports whether version a is newer than b ("1.2.10" > "1.2.9").
// A development build ("dev") is older than any release.
func Newer(a, b string) bool {
	pa, oka := parse(a)
	pb, okb := parse(b)
	if !oka {
		return false
	}
	if !okb {
		return true
	}
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parse(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// Download fetches the named asset of r into dir, checking it against the
// release's SHA256SUMS. progress gets the bytes so far and the total.
func Download(ctx context.Context, r Release, name, dir string, progress func(done, total int64)) (string, error) {
	a, ok := r.asset(name)
	if !ok {
		return "", fmt.Errorf("release %s has no %s", r.Version, name)
	}
	sums, ok := r.asset("SHA256SUMS")
	if !ok {
		return "", errors.New("release has no SHA256SUMS to check the download against")
	}
	want, err := expectedSum(ctx, sums.URL, name)
	if err != nil {
		return "", err
	}

	resp, err := get(ctx, a.URL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	pw := &progressWriter{total: a.Size, fn: progress}
	_, err = io.Copy(io.MultiWriter(f, h, pw), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return "", err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		os.Remove(path)
		return "", errors.New("the download doesn't match the release's checksum; not installing it")
	}
	return path, nil
}

// get downloads without the overall client timeout, since installers are
// large; ctx still bounds it.
func get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "dsync-updater")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download failed: %s", resp.Status)
	}
	return resp, nil
}

func expectedSum(ctx context.Context, url, name string) (string, error) {
	resp, err := get(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	for sc.Scan() {
		// "<hex>  <name>" (sha256sum format; "*" marks binary mode)
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name && len(fields[0]) == 64 {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("SHA256SUMS has no entry for %s", name)
}

type progressWriter struct {
	done, total int64
	fn          func(done, total int64)
	last        time.Time
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if p.fn != nil && (time.Since(p.last) > 150*time.Millisecond || p.done == p.total) {
		p.last = time.Now()
		p.fn(p.done, p.total)
	}
	return len(b), nil
}
