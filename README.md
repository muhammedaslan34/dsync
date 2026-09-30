# dsync

Send text (and soon files) between your Windows PC and Linux laptop.

## Build

Needs Go 1.22+, Node.js and the Wails CLI:

```sh
sudo pacman -S go nodejs npm webkit2gtk-4.1
go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0   # installs to ~/go/bin
```

```sh
wails build                           # Linux app  -> build/bin/dsync-gui
wails build -platform windows/amd64   # Windows app -> build/bin/dsync-gui.exe (built from Linux)
wails dev                             # run with live reload while editing the UI
```

The optional command-line tool (for scripts) builds with plain Go:

```sh
go build -o dist/dsync ./cmd/dsync
GOOS=windows GOARCH=amd64 go build -o dist/dsync.exe ./cmd/dsync
```

## Firewall

dsync uses UDP 47100 (discovery) and TCP 47101 (data). Open them on both machines:

```sh
# Linux (ufw)
sudo ufw allow 47100/udp
sudo ufw allow 47101/tcp
```

```powershell
# Windows (admin PowerShell)
New-NetFirewallRule -DisplayName dsync-udp -Direction Inbound -Protocol UDP -LocalPort 47100 -Action Allow -Profile Private
New-NetFirewallRule -DisplayName dsync-tcp -Direction Inbound -Protocol TCP -LocalPort 47101 -Action Allow -Profile Private
```

Your Wi-Fi must be set to a *Private* network on Windows.

## Usage

Open **dsync-gui** on both computers. They find each other automatically and show up
in the sidebar; pick one and type, or drag files onto the window (or use 📎) to send them. Received
files go to `~/Downloads/dsync` by default; change it at the bottom of the sidebar.
Enter sends, Shift+Enter adds a new line, and every
message has a Copy button. Open **Settings** (the sliders icon, or click your own device card) to rename this computer, see its addresses, change the received-files folder or switch between light and dark themes.

If a device doesn't appear by itself, click **Connect a device**: dsync searches every address on your
network for it (this works even when the firewall blocks discovery broadcasts, as long as
TCP 47101 is open), or you can type its address. This computer's addresses are listed at
the bottom of that window; click one to copy it.

Only one dsync (app or `dsync serve`) can run per computer, since both use the same ports.

Command-line tool:

```sh
dsync devices                     # list machines found on the network
dsync text "hello"                # send text (auto-picks the device if there is only one)
dsync text --to MyPC "hello"      # pick a device by name
cat notes.txt | dsync text        # send text from stdin
dsync send --to MyPC a.zip b.iso  # send files with a progress bar
dsync text --addr 100.64.0.2 "hi" # skip discovery
```

Settings and message history live in `~/.config/dsync/` (Linux) or `%AppData%\dsync\`
(Windows).

> Traffic is plain HTTP for now. Pairing and encryption come in step 3, so only use
> it on your home network until then.

## Roadmap

1. ✅ Discovery + text, desktop app
2. ✅ File transfer (streaming, progress, checksum, cancel)
3. Pairing + TLS
4. Resume, clipboard sync, folders
5. `dsync control` (launch Moonlight), tray GUI, Tailscale
