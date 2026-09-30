#!/bin/sh
# Installs dsync for the current user (no root needed): the programs go to
# ~/.local/lib/dsync with links in ~/.local/bin, plus an app menu entry.
set -e
here=$(cd "$(dirname "$0")" && pwd)
lib="$HOME/.local/lib/dsync"
bin="$HOME/.local/bin"
data="${XDG_DATA_HOME:-$HOME/.local/share}"

if ! ldconfig -p 2>/dev/null | grep -q 'libwebkit2gtk-4.1'; then
	echo "Note: dsync needs WebKitGTK 4.1, which doesn't seem to be installed."
	echo "  Arch:          sudo pacman -S webkit2gtk-4.1"
	echo "  Debian/Ubuntu: sudo apt install libwebkit2gtk-4.1-0"
	echo "  Fedora:        sudo dnf install webkit2gtk4.1"
	echo
fi

pkill -x dsync-gui 2>/dev/null && sleep 0.5 || true
mkdir -p "$lib" "$bin" "$data/applications" "$data/icons/hicolor/256x256/apps"
install -m 755 "$here/dsync-gui" "$here/dsync" "$lib/"
ln -sf "$lib/dsync-gui" "$bin/dsync-gui"
ln -sf "$lib/dsync" "$bin/dsync"
install -m 644 "$here/dsync.png" "$data/icons/hicolor/256x256/apps/dsync.png"
# Point the menu entry at the installed program explicitly, in case
# ~/.local/bin isn't on the PATH.
sed "s|^Exec=dsync-gui|Exec=\"$lib/dsync-gui\"|" "$here/dsync.desktop" > "$data/applications/dsync.desktop"
update-desktop-database "$data/applications" 2>/dev/null || true
gtk-update-icon-cache -q "$data/icons/hicolor" 2>/dev/null || true
echo "Installed dsync to $lib."

# Other computers reach dsync on UDP 47100 and TCP 47101.
open_firewall() {
	if command -v ufw >/dev/null && systemctl is-active -q ufw 2>/dev/null; then
		printf "Allow dsync through the ufw firewall? [Y/n] "
		read -r a
		case "$a" in [nN]*) return ;; esac
		sudo ufw allow 47100/udp comment dsync && sudo ufw allow 47101/tcp comment dsync
		return
	fi
	if command -v firewall-cmd >/dev/null && systemctl is-active -q firewalld 2>/dev/null; then
		printf "Allow dsync through firewalld? [Y/n] "
		read -r a
		case "$a" in [nN]*) return ;; esac
		sudo firewall-cmd --permanent --add-port=47100/udp --add-port=47101/tcp && sudo firewall-cmd --reload
	fi
}
if [ -t 0 ]; then open_firewall; fi

case ":$PATH:" in *":$bin:"*) ;; *) echo "Tip: add $bin to your PATH to use the dsync command." ;; esac
echo "Open dsync from your app menu, or run: dsync-gui"
