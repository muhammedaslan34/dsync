// Package update checks GitHub for a newer dsync release, downloads the
// right file for how this copy was installed, verifies it against the
// release's SHA256SUMS, and installs it.
package update

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
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

const (
	checksumsName = "SHA256SUMS"
	signatureName = "SHA256SUMS.sig"
)

// releasePublicKeyDERBase64 is the base64-encoded PKIX DER Ed25519 public key
// used to authenticate release checksums. Official builds set it with:
//
//	-X dsync/internal/update.releasePublicKeyDERBase64=<base64 DER public key>
//
// It is deliberately empty in source: a build without a configured release
// key must fail closed instead of silently trusting unsigned updates.
var releasePublicKeyDERBase64 string

// apiBase and webBase are variables so tests can point them at a fake
// GitHub.
var (
	apiBase = "https://api.github.com"
	webBase = "https://github.com"
)

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

// Latest returns the newest published release. It asks GitHub's API, and
// when that refuses (it allows 60 requests an hour per network without a
// login, and answers 403 or 429 after that) it reads the release from
// GitHub's website instead, which has no such limit.
func Latest(ctx context.Context) (Release, error) {
	r, err := latestFromAPI(ctx)
	var limited *rateLimited
	if errors.As(err, &limited) {
		if r, werr := latestFromWeb(ctx); werr == nil {
			return r, nil
		}
	}
	return r, err
}

type rateLimited struct{ status string }

func (e *rateLimited) Error() string {
	return "GitHub is limiting requests from this network right now (" + e.status + "); try again in an hour"
}

func latestFromAPI(ctx context.Context) (Release, error) {
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
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return Release{}, &rateLimited{resp.Status}
	}
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

// latestFromWeb finds the newest release without the API: the release page
// "releases/latest" redirects to the newest tag, and its authenticated
// SHA256SUMS lists the files (every file is in it, so the download can be
// checked). The signature is verified before any filename in SHA256SUMS is
// trusted as a release asset.
func latestFromWeb(ctx context.Context) (Release, error) {
	noRedirect := &http.Client{
		Timeout:       client.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, webBase+"/"+Repo+"/releases/latest", nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("User-Agent", "dsync-updater")
	resp, err := noRedirect.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("could not reach GitHub: %w", err)
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	i := strings.LastIndex(loc, "/releases/tag/")
	if resp.StatusCode/100 != 3 || i < 0 {
		return Release{}, fmt.Errorf("GitHub answered %s without a release", resp.Status)
	}
	tag := loc[i+len("/releases/tag/"):]
	if _, ok := parse(tag); !ok {
		return Release{}, fmt.Errorf("unexpected release tag %q", tag)
	}

	r := Release{Version: strings.TrimPrefix(tag, "v"), URL: webBase + "/" + Repo + "/releases/tag/" + tag}
	download := webBase + "/" + Repo + "/releases/download/" + tag + "/"
	sums, err := getSmall(ctx, download+checksumsName, 1<<20)
	if err != nil {
		return Release{}, err
	}
	sig, err := getSmall(ctx, download+signatureName, ed25519.SignatureSize)
	if err != nil {
		return Release{}, fmt.Errorf("release has no usable %s: %w", signatureName, err)
	}
	if err := verifyChecksumSignature(sums, sig); err != nil {
		return Release{}, err
	}
	r.Assets = append(r.Assets,
		Asset{Name: checksumsName, URL: download + checksumsName},
		Asset{Name: signatureName, URL: download + signatureName},
	)
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 && len(f[0]) == 64 {
			if _, err := hex.DecodeString(f[0]); err != nil {
				continue
			}
			name := strings.TrimPrefix(f[1], "*")
			if name != "" && !strings.ContainsAny(name, "/\\") {
				r.Assets = append(r.Assets, Asset{Name: name, URL: download + name})
			}
		}
	}
	if err := sc.Err(); err != nil {
		return Release{}, fmt.Errorf("read %s: %w", checksumsName, err)
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

// Download fetches the named asset of r into dir. It first authenticates the
// release's SHA256SUMS with the public key pinned into this build, then checks
// the downloaded asset against that authenticated checksum. progress gets the
// bytes so far and the total.
func Download(ctx context.Context, r Release, name, dir string, progress func(done, total int64)) (string, error) {
	a, ok := r.asset(name)
	if !ok {
		return "", fmt.Errorf("release %s has no %s", r.Version, name)
	}
	sums, ok := r.asset(checksumsName)
	if !ok {
		return "", fmt.Errorf("release has no %s to check the download against", checksumsName)
	}
	signature, ok := r.asset(signatureName)
	if !ok {
		return "", fmt.Errorf("release has no %s; refusing an unauthenticated update", signatureName)
	}
	sumsData, err := getSmall(ctx, sums.URL, 1<<20)
	if err != nil {
		return "", err
	}
	signatureData, err := getSmall(ctx, signature.URL, ed25519.SignatureSize)
	if err != nil {
		return "", fmt.Errorf("could not download %s: %w", signatureName, err)
	}
	if err := verifyChecksumSignature(sumsData, signatureData); err != nil {
		return "", err
	}
	want, err := expectedSum(sumsData, name)
	if err != nil {
		return "", err
	}

	// Do not fetch or create the target file until its expected checksum has
	// been authenticated above.
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
	total := a.Size
	if total <= 0 {
		total = max(resp.ContentLength, 0) // sizes aren't known when read from the website
	}
	pw := &progressWriter{total: total, fn: progress}
	_, err = io.Copy(io.MultiWriter(f, h, pw), resp.Body)
	if err == nil && progress != nil && pw.done != total {
		progress(pw.done, pw.done) // the size wasn't known: report the end
	}
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

func getSmall(ctx context.Context, url string, maxBytes int64) ([]byte, error) {
	resp, err := get(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("downloaded metadata exceeds %d bytes", maxBytes)
	}
	return data, nil
}

func verifyChecksumSignature(sums, signature []byte) error {
	if releasePublicKeyDERBase64 == "" {
		return errors.New("this build has no release verification public key; refusing to install an update")
	}
	der, err := base64.StdEncoding.DecodeString(releasePublicKeyDERBase64)
	if err != nil {
		return errors.New("this build has an invalid release verification public key; refusing to install an update")
	}
	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return errors.New("this build has an invalid release verification public key; refusing to install an update")
	}
	publicKey, ok := parsed.(ed25519.PublicKey)
	if !ok || len(publicKey) != ed25519.PublicKeySize {
		return errors.New("this build's release verification key is not Ed25519; refusing to install an update")
	}
	if len(signature) != ed25519.SignatureSize || !ed25519.Verify(publicKey, sums, signature) {
		return errors.New("SHA256SUMS has an invalid release signature; refusing to install the update")
	}
	return nil
}

func expectedSum(sums []byte, name string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		// "<hex>  <name>" (sha256sum format; "*" marks binary mode)
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name && len(fields[0]) == 64 {
			if _, err := hex.DecodeString(fields[0]); err == nil {
				return strings.ToLower(fields[0]), nil
			}
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%s has no valid entry for %s", checksumsName, name)
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
