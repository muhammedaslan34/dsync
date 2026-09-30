package clip

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestSystemClipboard uses the real clipboard, so it only runs when asked:
//
//	DSYNC_REAL_CLIPBOARD=1 go test ./internal/clip
//
// It overwrites whatever is on the clipboard. On Linux it needs wl-copy
// and wl-paste (Wayland) to act as another app.
func TestSystemClipboard(t *testing.T) {
	if os.Getenv("DSYNC_REAL_CLIPBOARD") == "" {
		t.Skip("set DSYNC_REAL_CLIPBOARD=1 to test with the real clipboard")
	}
	if _, err := exec.LookPath("wl-copy"); err != nil {
		t.Skip("needs wl-copy")
	}
	b, err := System()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	changes := b.Watch(ctx)
	time.Sleep(500 * time.Millisecond)

	next := func(kind string) Content {
		t.Helper()
		for {
			select {
			case c := <-changes:
				if c.Kind == kind {
					return c
				}
			case <-time.After(4 * time.Second):
				t.Fatalf("no %s change seen", kind)
			}
		}
	}

	// Another app copies text.
	exec.Command("wl-copy", "dsync clipboard test 1").Run()
	if c := next(Text); string(c.Data) != "dsync clipboard test 1" {
		t.Errorf("text: got %q", c.Data)
	}

	// Another app copies an image (found by polling).
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 64, 48)))
	cmd := exec.Command("wl-copy", "--type", "image/png")
	cmd.Stdin = bytes.NewReader(buf.Bytes())
	cmd.Run()
	if c := next(Image); len(c.Data) == 0 {
		t.Error("image: empty")
	} else if cfg, err := png.DecodeConfig(bytes.NewReader(c.Data)); err != nil || cfg.Width != 64 {
		t.Errorf("image: not the copied PNG (%v)", err)
	}

	// We write; another app pastes it.
	if err := b.Write(ctx, Content{Kind: Text, Data: []byte("written by dsync 2")}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	out, _ := exec.Command("wl-paste", "--no-newline").Output()
	if strings.TrimSpace(string(out)) != "written by dsync 2" {
		t.Errorf("paste after write: %q", out)
	}
}
