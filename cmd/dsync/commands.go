package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"dsync/internal/client"
	"dsync/internal/config"
	"dsync/internal/discovery"
	"dsync/internal/identity"
	"dsync/internal/node"
	"dsync/internal/proto"
)

func cmdServe(cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	fs.Parse(args)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// All interactive requests share one stdin queue. That keeps an answer
	// attached to the prompt currently on screen when several peers ask at
	// once, and lets timeout/close events remove stale queued prompts.
	prompts := newServePrompter(os.Stdout)
	var n *node.Node
	n, err := node.New(cfg, func(event string, data any) {
		switch event {
		case node.EventMessage:
			if m := data.(node.Message); m.Incoming && m.File == nil {
				prompts.notice(fmt.Sprintf("[%s] text from %s:\n%s", time.Now().Format("15:04:05"), m.PeerName, m.Text))
			}
		case node.EventMessageUpdate:
			if m := data.(node.Message); m.Incoming && m.File != nil && m.File.Status != node.StatusActive {
				prompts.notice(fmt.Sprintf("[%s] file from %s: %s (%s)", time.Now().Format("15:04:05"), m.PeerName, m.File.Name, m.File.Status))
			}
		case node.EventPairRequest:
			r := data.(node.PairRequest)
			warning := ""
			if r.KeyChanged {
				warning = "\nWARNING: this device id or key conflicts with an existing pairing; unpair the old device first."
			}
			prompts.ask(servePrompt{
				kind: promptPair, id: r.ID,
				text:   fmt.Sprintf("%s wants to pair. Code: %s\nFingerprint: %s%s\nAccept only if the other device shows the same code. Accept? [y/N] ", r.Name, r.Code, r.Fingerprint, warning),
				answer: func(accept bool) { n.AnswerPair(r.ID, accept) },
			})
		case node.EventPairClosed:
			prompts.close(promptPair, data.(string))
		case node.EventIncomingRequest:
			r := data.(node.IncomingRequest)
			kind, details := "file", fmt.Sprintf("%d bytes", r.Size)
			if r.Folder {
				kind, details = "folder", fmt.Sprintf("%d files, %d bytes", r.Files, r.Size)
			}
			prompts.ask(servePrompt{
				kind: promptIncoming, id: r.ID,
				text:   fmt.Sprintf("%s (%s) wants to send the %s %q (%s).\nAccept? [y/N] ", r.PeerName, r.Fingerprint, kind, r.Name, details),
				answer: func(accept bool) { n.AnswerIncoming(r.ID, accept) },
			})
		case node.EventIncomingClosed:
			prompts.close(promptIncoming, data.(string))
		case node.EventControlPIN:
			r := data.(node.ControlPIN)
			if !r.Automatic {
				prompts.notice(fmt.Sprintf("%s (%s) is requesting remote control. Enter PIN %s in Sunshine: %s", r.PeerName, r.Fingerprint, r.PIN, r.URL))
				break
			}
			prompts.ask(servePrompt{
				kind: promptControl, id: r.ID,
				text:   fmt.Sprintf("%s (%s) is requesting remote control of this computer.\nAllow this one-time PIN submission? [y/N] ", r.PeerName, r.Fingerprint),
				answer: func(accept bool) { n.AnswerControlPIN(r.ID, accept) },
			})
		case node.EventControlPINClosed:
			prompts.close(promptControl, data.(string))
		}
	})
	if err != nil {
		return err
	}
	prompts.start(ctx, os.Stdin)
	self := n.Self()
	log.Printf("serving as %q (key %s): tcp :%d, discovery udp :%d", self.Name, n.Fingerprint(), self.Port, proto.DiscoveryPort)
	return n.Run(ctx)
}

func cmdPair(cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
	to := fs.String("to", "", "device id prefix from `dsync devices`")
	addr := fs.String("addr", "", "pair directly with HOST[:PORT], skipping discovery")
	fs.Parse(args)

	cl, id, err := newClient(cfg)
	if err != nil {
		return err
	}
	t, err := resolveTarget(cfg, cl, *to, *addr)
	if err != nil {
		return err
	}
	if t.paired {
		fmt.Printf("already paired with %s\n", t.name)
		return nil
	}
	fmt.Printf("Pairing with %s. Code: %s\nAccept on %s if it shows the same code (waiting up to a minute)...\n",
		t.name, identity.PairCode(id.Fingerprint, t.fp), t.name)

	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	resp, err := cl.Pair(ctx, t.addr, t.fp, proto.PairRequest{FromID: cfg.ID, FromName: cfg.Name, FromPort: cfg.Port, OS: runtime.GOOS})
	if err != nil {
		return fmt.Errorf("pairing failed: %w", err)
	}
	if err := cfg.Trust(config.TrustedPeer{ID: t.id, Name: resp.Name, Fingerprint: t.fp}); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("paired with %s\n", resp.Name)
	return nil
}

func cmdDevices(cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("devices", flag.ExitOnError)
	wait := fs.Duration("wait", 1500*time.Millisecond, "how long to wait for answers")
	fs.Parse(args)

	peers, err := discovery.Discover(context.Background(), selfDevice(cfg), *wait)
	if err != nil {
		return err
	}
	if len(peers) == 0 {
		fmt.Println("no devices found (is `dsync serve` running on the other machine, and is its firewall open?)")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tOS\tADDRESS\tPAIRED\tID")
	for _, p := range peers {
		paired := "no"
		name := p.Name
		if trusted, ok := cfg.TrustedByID(p.ID); ok {
			paired = "yes"
			name = trusted.Name
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", name, p.OS, p.Addr(), paired, p.ID)
	}
	return tw.Flush()
}

func cmdText(cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("text", flag.ExitOnError)
	to := fs.String("to", "", "device name or id prefix")
	addr := fs.String("addr", "", "send directly to HOST[:PORT], skipping discovery")
	fs.Parse(args)

	text := strings.Join(fs.Args(), " ")
	if text == "" || text == "-" {
		if fi, _ := os.Stdin.Stat(); fi != nil && fi.Mode()&os.ModeCharDevice != 0 {
			return errors.New("no message given (pass it as an argument or pipe it in)")
		}
		b, err := io.ReadAll(io.LimitReader(os.Stdin, proto.MaxTextBytes+1))
		if err != nil {
			return err
		}
		text = strings.TrimRight(string(b), "\r\n")
	}
	if text == "" {
		return errors.New("message is empty")
	}
	if len(text) > proto.MaxTextBytes {
		return fmt.Errorf("message is larger than %d bytes; send it as a file instead", proto.MaxTextBytes)
	}

	cl, _, err := newClient(cfg)
	if err != nil {
		return err
	}
	t, err := resolvePaired(cfg, cl, *to, *addr)
	if err != nil {
		return err
	}
	msg := proto.TextMessage{FromID: cfg.ID, FromName: cfg.Name, FromPort: cfg.Port, Text: text}
	if err := cl.SendText(context.Background(), t.addr, t.fp, msg); err != nil {
		return fmt.Errorf("send to %s: %w", t.name, err)
	}
	fmt.Printf("sent to %s\n", t.name)
	return nil
}

func newClient(cfg *config.Config) (*client.Client, *identity.Identity, error) {
	id, err := identity.Load(cfg.Dir())
	if err != nil {
		return nil, nil, err
	}
	return client.New(id), id, nil
}

type target struct {
	addr, name, id, fp string
	paired             bool
}

// resolvePaired is resolveTarget for commands that need a paired device.
func resolvePaired(cfg *config.Config, cl *client.Client, to, addr string) (target, error) {
	t, err := resolveTarget(cfg, cl, to, addr)
	if err == nil && !t.paired {
		prefix := t.id
		if len(prefix) > 8 {
			prefix = prefix[:8]
		}
		err = fmt.Errorf("not paired with %s; run `dsync pair --to %s` or pair in the app", t.name, prefix)
	}
	return t, err
}

// resolveTarget finds the device to talk to, either from --addr or by
// discovering devices and matching --to against their name or id, then asks
// it for its id and key. A paired device must present its pinned key.
func resolveTarget(cfg *config.Config, cl *client.Client, to, addr string) (target, error) {
	if addr == "" {
		var err error
		if addr, err = discoverAddr(cfg, to); err != nil {
			return target{}, err
		}
	} else if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, strconv.Itoa(proto.DefaultHTTPPort))
	}

	d, fp, err := cl.Info(context.Background(), addr)
	if err != nil {
		return target{}, fmt.Errorf("could not reach %s: %w", addr, err)
	}
	t := target{addr: addr, name: d.Name, id: d.ID, fp: fp}
	if trusted, ok := cfg.TrustedByID(d.ID); ok {
		if trusted.Fingerprint != fp {
			return target{}, fmt.Errorf("%s: %w", d.Name, client.ErrIdentityChanged)
		}
		t.paired, t.name = true, trusted.Name
	}
	return t, nil
}

func discoverAddr(cfg *config.Config, to string) (string, error) {
	peers, err := discovery.Discover(context.Background(), selfDevice(cfg), 1200*time.Millisecond)
	if err != nil {
		return "", err
	}
	var matches []discovery.Peer
	for _, p := range peers {
		if trusted, ok := cfg.TrustedByID(p.ID); ok {
			p.Name = trusted.Name
		}
		if to == "" || strings.EqualFold(p.Name, to) || strings.HasPrefix(p.ID, to) {
			matches = append(matches, p)
		}
	}
	switch len(matches) {
	case 0:
		if to != "" {
			return "", fmt.Errorf("no device named %q found (try `dsync devices`)", to)
		}
		return "", errors.New("no devices found (is dsync running on the other machine?)")
	case 1:
		return matches[0].Addr(), nil
	default:
		names := make([]string, len(matches))
		for i, p := range matches {
			names[i] = p.Name
		}
		return "", fmt.Errorf("several devices found, pick one with --to: %s", strings.Join(names, ", "))
	}
}

func cmdSend(cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	to := fs.String("to", "", "device name or id prefix")
	addr := fs.String("addr", "", "send directly to HOST[:PORT], skipping discovery")
	fs.Parse(args)
	if fs.NArg() == 0 {
		return errors.New("no files given")
	}
	for _, p := range fs.Args() {
		if _, err := os.Stat(p); err != nil {
			return err
		}
	}

	cl, _, err := newClient(cfg)
	if err != nil {
		return err
	}
	t, err := resolvePaired(cfg, cl, *to, *addr)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	for _, p := range fs.Args() {
		send := sendOne
		if st, _ := os.Stat(p); st.IsDir() {
			send = sendFolder
		}
		if err := send(ctx, cfg, cl, t, p); err != nil {
			return fmt.Errorf("send %s to %s: %w", filepath.Base(p), t.name, err)
		}
	}
	return nil
}

// sendFolder sends a folder's files one by one. Running the same command
// again skips files the device already has and resumes the one that was
// interrupted.
func sendFolder(ctx context.Context, cfg *config.Config, cl *client.Client, t target, root string) error {
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	entries, total, err := node.ListFolder(abs)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return errors.New("the folder has no files")
	}
	fid, run := client.FolderID(cfg.ID, abs), node.NewRunID()
	fmt.Fprintf(os.Stderr, "%s: %d files, %s\n", filepath.Base(abs), len(entries), humanBytes(total))

	status, errText := node.StatusDone, ""
	defer func() {
		cl.FolderEnd(context.WithoutCancel(ctx), t.addr, t.fp, proto.FolderEnd{FolderID: fid, Status: status, Error: errText})
	}()
	skipped := 0
	for i, e := range entries {
		h := client.FileHeader{FolderID: fid, FolderRun: run, FolderSize: total, FolderFiles: len(entries), RelPath: e.Rel}
		label := fmt.Sprintf("[%d/%d] %s", i+1, len(entries), e.Rel)
		err := sendPath(ctx, cfg, cl, t, e.Path, label, h)
		if errors.Is(err, client.ErrAlreadyReceived) {
			skipped++
			continue
		}
		if err != nil {
			status, errText = node.StatusFailed, err.Error()
			if ctx.Err() != nil {
				status = node.StatusCanceled
			}
			return fmt.Errorf("%s: %w", e.Rel, err)
		}
	}
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "%d files were already there from an earlier attempt\n", skipped)
	}
	return nil
}

func sendOne(ctx context.Context, cfg *config.Config, cl *client.Client, t target, path string) error {
	return sendPath(ctx, cfg, cl, t, path, filepath.Base(path), client.FileHeader{})
}

// sendPath sends one file with a progress line labeled label. h may carry
// folder fields.
func sendPath(ctx context.Context, cfg *config.Config, cl *client.Client, t target, path, label string, h client.FileHeader) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}

	var start, last time.Time
	var startDone int64 = -1
	progress := func(done int64) {
		if startDone < 0 {
			startDone, start = done, time.Now()
		}
		if time.Since(last) < 200*time.Millisecond && done != st.Size() {
			return
		}
		last = time.Now()
		pct := 100.0
		if st.Size() > 0 {
			pct = float64(done) * 100 / float64(st.Size())
		}
		rate := float64(done-startDone) / max(time.Since(start).Seconds(), 0.001)
		fmt.Fprintf(os.Stderr, "\r%s  %5.1f%%  %s / %s  %s/s   ", label, pct, humanBytes(done), humanBytes(st.Size()), humanBytes(int64(rate)))
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	h.FromID, h.FromName, h.FromPort = cfg.ID, cfg.Name, cfg.Port
	h.Name, h.Size = filepath.Base(path), st.Size()
	// Same id each run, so running the command again resumes.
	h.TransferID = client.TransferID(cfg.ID, abs, st.Size(), st.ModTime())
	err = cl.SendFile(ctx, t.addr, t.fp, h, f, progress)
	if !errors.Is(err, client.ErrAlreadyReceived) {
		fmt.Fprintln(os.Stderr)
	}
	return err
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// cmdControl opens Moonlight controlling another computer's desktop,
// starting Sunshine there and pairing Moonlight with it first if needed.
func cmdControl(cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("control", flag.ExitOnError)
	to := fs.String("to", "", "device name or id prefix")
	addr := fs.String("addr", "", "the device's HOST[:PORT], skipping discovery")
	fs.Parse(args)

	cl, _, err := newClient(cfg)
	if err != nil {
		return err
	}
	t, err := resolvePaired(cfg, cl, *to, *addr)
	if err != nil {
		return err
	}
	d, _, err := cl.Info(context.Background(), t.addr)
	if err != nil {
		return err
	}
	n, err := node.New(cfg, func(event string, data any) {
		if s, ok := data.(node.ControlState); ok && event == node.EventControl {
			if s.PIN != "" {
				fmt.Fprintf(os.Stderr, "%s\n  PIN: %s\n", s.Message, s.PIN)
			} else if s.Message != "" {
				fmt.Fprintln(os.Stderr, s.Message)
			}
		}
	})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	err = n.RunControl(ctx, node.Peer{ID: t.id, Name: t.name, OS: d.OS, Addr: t.addr}, t.fp)
	var ce interface{ Hint() string }
	if errors.As(err, &ce) && ce.Hint() != "" {
		return fmt.Errorf("%w\n%s", err, ce.Hint())
	}
	return err
}
