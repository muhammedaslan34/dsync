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
go test -tags webkit2_41 ./...        # tests
DSYNC_REAL_CLIPBOARD=1 go test ./internal/clip   # also test the real clipboard (overwrites it)
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
in the sidebar; pick one and type, or drag files and folders onto the window (or use 📎 → Files… /
Folder…) to send them. A folder shows as one item with overall progress and is recreated
with its subfolders on the other side (symlinks and empty folders are skipped). Received
files go to `~/Downloads/dsync` by default; change it at the bottom of the sidebar.
Enter sends, Shift+Enter adds a new line, and every
message has a Copy button. Open **Settings** (the sliders icon, or click your own device card) to rename this computer, see its addresses, change the received-files folder or switch between light and dark themes.

If a device doesn't appear by itself, click **Connect a device**: dsync searches every address on your
network for it (this works even when the firewall blocks discovery broadcasts, as long as
TCP 47101 is open), or you can type its address. This computer's addresses are listed at
the bottom of that window; click one to copy it.

There is no size limit on files; they stream from disk, and the receiving computer checks
it has enough free space before a transfer starts. Text longer than 1 MB is sent as a
`.txt` file automatically.

Copy an image (a screenshot, or "Copy image" in a browser) or files, and press **Ctrl+V**
in a conversation: a preview asks before sending. Pictures show inline in the chat;
click one to open it.

**Clipboard sync:** turn on *Sync clipboard with paired devices* in Settings on both
computers, and text or images you copy on one can be pasted on the other a moment later.
It only goes to paired devices that are online and have it switched on, it isn't kept in
the chat history, and anything a password manager marks as secret (KeePassXC, KDE, and the
standard Windows flag) is never sent. On GNOME/Wayland it works through XWayland, which
is there by default.

Interrupted transfers resume where they stopped: if the connection drops, the sender keeps
retrying for a few minutes, and a failed file has a **Retry** button (or run the same
`dsync send` again); for a folder, files that already arrived are skipped. The receiver keeps partial files as hidden `.dsync-*.part` files in
the download folder until the transfer finishes; unused ones are deleted after a week.
The checksum always covers the whole file, so a resumed file is verified the same way.

**Tray icon:** closing the window keeps dsync running in the tray, so it keeps receiving and
syncing the clipboard. Click the icon to open it again; its menu also toggles clipboard sync
and quits. Messages and files that arrive while the window is closed show a desktop
notification, and pairing requests bring the window back. Under Settings → Background you
can turn this off, start dsync when you log in (it starts in the tray), and on Linux add it to
the app menu. On GNOME the tray needs the *AppIndicator and KStatusNotifierItem Support*
extension; without a tray, closing the window quits as before. Opening dsync again while
it runs brings the running one back.

Only one dsync (app or `dsync serve`) can run per computer, since both use the same ports.

Command-line tool:

```sh
dsync devices                     # list machines found on the network
dsync text "hello"                # send text (auto-picks the device if there is only one)
dsync text --to MyPC "hello"      # pick a device by name
cat notes.txt | dsync text        # send text from stdin
dsync send --to MyPC a.zip b.iso  # send files with a progress bar
dsync send --to MyPC ~/Photos     # send a folder (run again to resume)
dsync text --addr 100.64.0.2 "hi" # skip discovery
dsync pair --to MyPC              # pair (confirm the code on both screens)
```

Settings and message history live in `~/.config/dsync/` (Linux) or `%AppData%\dsync\`
(Windows).

## Pairing and security

Devices must be paired before they can send each other anything. Pick a device and click
**Pair**: both computers show the same 6-digit code, and you accept on the other one if
the codes match. (Headless machines running `dsync serve` answer by typing `y`; from the
command line, use `dsync pair --to NAME`.)

- Every connection is TLS 1.3. Each device creates its own key on first run
  (`identity.pem` in the settings folder) and pairing pins the other device's key, so
  nobody else on the network can read, send or impersonate.
- The code is derived from both keys, so a machine intercepting the pairing would make
  the two codes differ. Only accept when they match.
- If a paired device's key changes (reinstall, or something pretending to be it), dsync
  refuses to talk to it until you unpair and pair again.
- Unpairing (the broken-link icon in the header) removes trust on both sides.

## Roadmap

1. ✅ Discovery + text, desktop app
2. ✅ File transfer (streaming, progress, checksum, cancel)
3. ✅ Pairing + TLS 1.3
4. ✅ Resume, paste to send, folders
5. ✅ Clipboard sync
6. ✅ Tray icon, notifications, start at login
7. `dsync control` (launch Moonlight)
