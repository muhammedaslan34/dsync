// Package clip reads and writes the system clipboard for clipboard sync.
// It hides the platform library behind a small interface so the sync logic
// can be tested with a fake.
package clip

import (
	"bytes"
	"context"
	"crypto/sha256"
	"time"

	"golang.design/x/clipboard"
)

// Kinds of clipboard content that are synced.
const (
	Text  = "text"
	Image = "image" // PNG
)

// Content is what is on the clipboard.
type Content struct {
	Kind string
	Data []byte
	// Sensitive means the app that copied it (a password manager) asked
	// for it not to be kept or shared.
	Sensitive bool
}

// Hash identifies content, to tell whether it changed.
func (c Content) Hash() [32]byte {
	return sha256.Sum256(append([]byte(c.Kind+"\x00"), c.Data...))
}

// Board is a clipboard.
type Board interface {
	// Watch reports clipboard changes made after it starts, until ctx ends.
	Watch(ctx context.Context) <-chan Content
	Write(ctx context.Context, c Content) error
}

// imagePoll is how often the clipboard is checked for a new image. Some
// systems (GNOME on Wayland, through XWayland) don't report image changes,
// so images are polled; reading even a large one takes a few milliseconds.
const imagePoll = time.Second

type system struct{}

// System returns the computer's clipboard, or an error saying why there
// isn't one (e.g. no X server or Wayland compositor to talk to).
func System() (Board, error) {
	if err := clipboard.Init(); err != nil {
		return nil, err
	}
	return system{}, nil
}

func (system) Write(ctx context.Context, c Content) error {
	f := clipboard.FmtText
	if c.Kind == Image {
		f = clipboard.FmtImage
	}
	_, err := clipboard.Write(ctx, f, c.Data)
	return err
}

func (system) Watch(ctx context.Context) <-chan Content {
	out := make(chan Content, 4)
	text := clipboard.Watch(ctx, clipboard.FmtText)
	lastText, _ := clipboard.Read(ctx, clipboard.FmtText)
	lastImage, _ := clipboard.Read(ctx, clipboard.FmtImage)

	go func() {
		defer close(out)
		tick := time.NewTicker(imagePoll)
		defer tick.Stop()
		send := func(c Content) {
			select {
			case out <- c:
			case <-ctx.Done():
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case d, ok := <-text:
				if !ok {
					return
				}
				if bytes.Equal(d.Bytes, lastText) {
					continue // e.g. the report of what was there at the start
				}
				lastText = d.Bytes
				send(Content{Kind: Text, Data: d.Bytes, Sensitive: d.Sensitive})
			case <-tick.C:
				img, err := clipboard.Read(ctx, clipboard.FmtImage)
				if err != nil || len(img) == 0 {
					lastImage = nil // no image now; the next one is new
					continue
				}
				if bytes.Equal(img, lastImage) {
					continue
				}
				lastImage = img
				secret, _ := clipboard.Sensitive(ctx)
				send(Content{Kind: Image, Data: img, Sensitive: secret})
			}
		}
	}()
	return out
}
