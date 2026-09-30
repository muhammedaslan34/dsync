package main

import (
	"bufio"
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
	"sync/atomic"
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

	// Pairing requests are answered by typing y or n; the last one shown is
	// the one answered.
	var lastPair atomic.Value
	n, err := node.New(cfg, func(event string, data any) {
		switch event {
		case node.EventMessage:
			if m := data.(node.Message); m.Incoming && m.File == nil {
				fmt.Printf("\n[%s] text from %s:\n%s\n", time.Now().Format("15:04:05"), m.PeerName, m.Text)
			}
		case node.EventMessageUpdate:
			if m := data.(node.Message); m.Incoming && m.File != nil && m.File.Status != node.StatusActive {
				fmt.Printf("[%s] file from %s: %s (%s)\n", time.Now().Format("15:04:05"), m.PeerName, m.File.Name, m.File.Status)
			}
		case node.EventPairRequest:
			r := data.(node.PairRequest)
			lastPair.Store(r.ID)
			fmt.Printf("\n%s wants to pair. Code: %s\nAccept only if %s shows the same code. Accept? [y/N] ", r.Name, r.Code, r.Name)
		}
	})
	if err != nil {
		return err
	}
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			if id, _ := lastPair.Load().(string); id != "" {
				n.AnswerPair(id, strings.EqualFold(strings.TrimSpace(sc.Text()), "y"))
				lastPair.Store("")
			}
		}
	}()
	self := n.Self()
	log.Printf("serving as %q (key %s): tcp :%d, discovery udp :%d", self.Name, n.Fingerprint(), self.Port, proto.DiscoveryPort)
	return n.Run(ctx)
}

func cmdPair(cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
	to := fs.String("to", "", "device name or id prefix")
	addr := fs.String("addr", "", "pair directly with HOST[:PORT], skipping discovery")
	fs.Parse(args)

	cl, id, err := newClient()
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
	cfg.Trust(config.TrustedPeer{ID: t.id, Name: resp.Name, Fingerprint: t.fp})
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
		if _, ok := cfg.TrustedByID(p.ID); ok {
			paired = "yes"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", p.Name, p.OS, p.Addr(), paired, p.ID)
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

	cl, _, err := newClient()
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

func newClient() (*client.Client, *identity.Identity, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, nil, err
	}
	id, err := identity.Load(dir)
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
		err = fmt.Errorf("not paired with %s; run `dsync pair --to %q` or pair in the app", t.name, t.name)
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
		t.paired = true
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
		if st, err := os.Stat(p); err != nil {
			return err
		} else if st.IsDir() {
			return fmt.Errorf("%s is a folder; sending folders isn't supported yet", p)
		}
	}

	cl, _, err := newClient()
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
		if err := sendOne(ctx, cfg, cl, t, p); err != nil {
			return fmt.Errorf("send %s to %s: %w", filepath.Base(p), t.name, err)
		}
	}
	return nil
}

func sendOne(ctx context.Context, cfg *config.Config, cl *client.Client, t target, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}

	base := filepath.Base(path)
	start := time.Now()
	var last time.Time
	progress := func(done int64) {
		if time.Since(last) < 200*time.Millisecond && done != st.Size() {
			return
		}
		last = time.Now()
		pct := 100.0
		if st.Size() > 0 {
			pct = float64(done) * 100 / float64(st.Size())
		}
		rate := float64(done) / max(time.Since(start).Seconds(), 0.001)
		fmt.Fprintf(os.Stderr, "\r%s  %5.1f%%  %s / %s  %s/s   ", base, pct, humanBytes(done), humanBytes(st.Size()), humanBytes(int64(rate)))
	}
	h := client.FileHeader{FromID: cfg.ID, FromName: cfg.Name, FromPort: cfg.Port, Name: base, Size: st.Size()}
	err = cl.SendFile(ctx, t.addr, t.fp, h, f, progress)
	fmt.Fprintln(os.Stderr)
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
