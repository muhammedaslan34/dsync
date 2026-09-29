// Command dsync sends text and files between your computers.
package main

import (
	"fmt"
	"os"
	"runtime"

	"dsync/internal/config"
	"dsync/internal/proto"
)

const version = "0.1.0"

const usage = `dsync - send text and files between your computers

Usage:
  dsync serve                         run the service that receives messages
  dsync devices [--wait 1.5s]         list devices on the local network
  dsync text [--to NAME] MESSAGE      send text to another device
  echo hi | dsync text [--to NAME]    send text read from stdin
  dsync send [--to NAME] FILE...      send files
  dsync version

Flags for text and send (put them before the message or files):
  --to NAME          device name or id prefix (optional if only one device is found)
  --addr HOST[:PORT] send directly, skipping discovery (e.g. over Tailscale)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cfg, err := config.Load()
	if err != nil {
		fatal(err)
	}

	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "serve":
		err = cmdServe(cfg, args)
	case "devices":
		err = cmdDevices(cfg, args)
	case "text":
		err = cmdText(cfg, args)
	case "send":
		err = cmdSend(cfg, args)
	case "version":
		fmt.Println("dsync", version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		fatal(err)
	}
}

func selfDevice(cfg *config.Config) proto.Device {
	return proto.Device{ID: cfg.ID, Name: cfg.Name, OS: runtime.GOOS, Port: cfg.Port}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "dsync:", err)
	os.Exit(1)
}
