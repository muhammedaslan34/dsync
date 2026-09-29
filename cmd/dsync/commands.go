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
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"dsync/internal/client"
	"dsync/internal/config"
	"dsync/internal/discovery"
	"dsync/internal/node"
	"dsync/internal/proto"
)

func cmdServe(cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	fs.Parse(args)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	n := node.New(cfg, func(event string, data any) {
		if m, ok := data.(node.Message); ok && event == node.EventMessage && m.Incoming {
			fmt.Printf("\n[%s] text from %s:\n%s\n", time.Now().Format("15:04:05"), m.PeerName, m.Text)
		}
	})
	self := n.Self()
	log.Printf("serving as %q (id %s): http :%d, discovery udp :%d", self.Name, self.ID, self.Port, proto.DiscoveryPort)
	return n.Run(ctx)
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
	fmt.Fprintln(tw, "NAME\tOS\tADDRESS\tID")
	for _, p := range peers {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", p.Name, p.OS, p.Addr(), p.ID)
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

	target, name, err := resolveTarget(cfg, *to, *addr)
	if err != nil {
		return err
	}
	msg := proto.TextMessage{FromID: cfg.ID, FromName: cfg.Name, FromPort: cfg.Port, Text: text}
	if err := client.SendText(context.Background(), target, msg); err != nil {
		return fmt.Errorf("send to %s: %w", name, err)
	}
	fmt.Printf("sent to %s\n", name)
	return nil
}

// resolveTarget picks the HTTP address to send to, either from --addr or by
// discovering devices and matching --to against their name or id.
func resolveTarget(cfg *config.Config, to, addr string) (target, name string, err error) {
	if addr != "" {
		if _, _, err := net.SplitHostPort(addr); err != nil {
			addr = net.JoinHostPort(addr, strconv.Itoa(proto.DefaultHTTPPort))
		}
		return addr, addr, nil
	}

	peers, err := discovery.Discover(context.Background(), selfDevice(cfg), 1200*time.Millisecond)
	if err != nil {
		return "", "", err
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
			return "", "", fmt.Errorf("no device named %q found (try `dsync devices`)", to)
		}
		return "", "", errors.New("no devices found (is `dsync serve` running on the other machine?)")
	case 1:
		return matches[0].Addr(), matches[0].Name, nil
	default:
		names := make([]string, len(matches))
		for i, p := range matches {
			names[i] = p.Name
		}
		return "", "", fmt.Errorf("several devices found, pick one with --to: %s", strings.Join(names, ", "))
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

	target, name, err := resolveTarget(cfg, *to, *addr)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	for _, p := range fs.Args() {
		if err := sendOne(ctx, cfg, target, p); err != nil {
			return fmt.Errorf("send %s to %s: %w", filepath.Base(p), name, err)
		}
	}
	return nil
}

func sendOne(ctx context.Context, cfg *config.Config, target, path string) error {
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
	err = client.SendFile(ctx, target, h, f, progress)
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
