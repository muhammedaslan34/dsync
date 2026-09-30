# dsync

Send text (and soon files) between your Windows PC and Linux laptop.

## Install

Download the file for your system from `dist/release` (see *Packaging* below):

- **Windows:** run `dsync-setup-VERSION-windows-amd64.exe`. It installs dsync (the app
  and the `dsync.exe` command-line tool) to Program Files, adds Start menu and desktop
  shortcuts, installs Microsoft WebView2 if missing, and lets dsync through Windows
  Firewall on home and work networks. Nothing needs to be run by hand (there is no `sudo`
  on Windows). If Windows has your network set to *Public*, the installer offers to make it
  *Private*, since Windows blocks dsync on public networks. If other computers still can't
  reach the PC, open dsync's *Settings → Windows Firewall* and click the fix buttons;
  Windows asks for permission. Uninstall it from *Settings → Apps*; your settings and
  pairings in `%AppData%\dsync` are kept.
- **Arch Linux / CachyOS:** `sudo pacman -U dsync-VERSION-1-x86_64.pkg.tar.zst`, then
  `sudo ufw allow dsync` (or `sudo firewall-cmd --permanent --add-service=dsync`).
- **Other Linux:** unpack `dsync-VERSION-linux-x86_64.tar.gz` and run `./install.sh`. It
  installs for your user only (no root), adds dsync to the app menu, and offers to open
  the firewall. `./uninstall.sh` removes it. Needs WebKitGTK 4.1.

## macOS

Download `dsync-VERSION-macos-universal.dmg` from the release (it runs on Apple Silicon and
Intel Macs), open it and drag **dsync** into Applications. dsync isn't signed with an Apple
Developer ID, so the first time macOS says it can't be opened: right-click dsync → **Open**
→ **Open**. Allow it on the local network when macOS asks. On macOS there's no menu bar icon
yet, so closing the window quits dsync. The disk image is built by
`.github/workflows/macos.yml` on GitHub's Macs whenever a release is published.

## Updates

dsync checks GitHub for a new release shortly after it starts and twice a day; an
**Update** badge appears when there is one. *Settings → Updates* installs it: the Windows
setup runs (Windows asks for permission), the Arch package is installed with a password
window, and a `install.sh` install is replaced in place. Downloads are checked against the
release's `SHA256SUMS` before anything is installed.

## Packaging

`./scripts/package.sh` runs the tests, builds everything, and writes the three installers
plus `SHA256SUMS` to `dist/release`. The version comes from `info.productVersion` in
`wails.json`. It needs the build tools below, `makepkg` for the Arch package, and Docker:
the Windows installer is made with NSIS, run from a small Debian image
(`packaging/nsis`) that is built the first time.

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
files go to a `dsync` folder in your Downloads folder by default (wherever your system keeps
it); change it in Settings. Files whose names start with a dot, like `.env`, are hidden by
Linux file managers: press Ctrl+H to see them.
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

To clear history, use the eraser button in a conversation's header, or *Settings → History →
Clear all*. It only removes the messages on this computer: received files stay in your
Downloads folder and the other computer keeps its copy.

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

**Remote control:** click the screen icon in a paired device's header (or run
`dsync control --to NAME`) to see and control its desktop. The streaming is done by
[Moonlight](https://moonlight-stream.org) on the computer you control *from* and
[Sunshine](https://app.lizardbyte.dev) on the one being controlled:

```sh
sudo pacman -S moonlight-qt              # Arch/CachyOS, to control other computers
sudo pacman -S sunshine                  # Arch/CachyOS, to be controlled
winget install MoonlightGameStreamingProject.Moonlight   # Windows
winget install LizardByte.Sunshine                        # Windows
```

You can also install them from dsync: *Settings → Remote control → Install*, and the
Windows installer offers both as options. Open Sunshine once after installing it to set its
username and password. dsync then does
the rest: it starts Sunshine on the other computer if it isn't running, pairs Moonlight with
it the first time (sending the PIN over dsync's encrypted connection), and opens the stream.
If you save the Sunshine login in dsync's Settings on the controlled computer, that first
pairing needs nobody there; otherwise dsync shows the PIN on that computer to type into
Sunshine. The login is stored in dsync's settings file, readable only by your user.

Before it starts, the control window lets you pick **which screen** to control (the other
computer's monitors, when it has several), the **size** (smaller sizes make everything look
bigger: a Windows PC switches its resolution to match while you control it, and back
afterwards), fullscreen or window, the frame rate, and the **quality** (bitrate: *High* by default, *Best* for the sharpest
text on a fast network) with an optional **Sharpest text** switch (full 4:4:4 color, for
recent graphics on both sides). *Full size* streams at your screen's real resolution.
The **mouse** works in *Desktop* mode by default (the pointer over there follows yours
exactly), or *Game* mode (captured, raw movement); on Linux (GNOME) and Windows you can
also make your pointer faster or slower while controlling, and it goes back afterwards.
Choosing the screen and the
bigger sizes on Windows use the Sunshine login saved in dsync on that PC. While
controlling, Ctrl+Alt+Shift+Q stops and Ctrl+Alt+Shift+X switches fullscreen; to zoom into
one spot, use the controlled computer's magnifier (Win and + on Windows).

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
dsync control --to MyPC           # control MyPC's desktop with Moonlight
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
7. ✅ Remote control (Moonlight + Sunshine)
