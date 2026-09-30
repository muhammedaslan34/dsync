#!/bin/sh
# Removes a dsync installed with install.sh. Your settings, pairings and
# history in ~/.config/dsync are kept; delete that folder to remove them.
set -e
data="${XDG_DATA_HOME:-$HOME/.local/share}"
pkill -x dsync-gui 2>/dev/null || true
rm -f "$HOME/.local/bin/dsync-gui" "$HOME/.local/bin/dsync"
rm -rf "$HOME/.local/lib/dsync"
rm -f "$data/applications/dsync.desktop" "$data/icons/hicolor/256x256/apps/dsync.png"
rm -f "${XDG_CONFIG_HOME:-$HOME/.config}/autostart/dsync.desktop"
update-desktop-database "$data/applications" 2>/dev/null || true
echo "dsync removed. Your settings are still in ~/.config/dsync."
echo "If you opened the firewall for it: sudo ufw delete allow 47100/udp; sudo ufw delete allow 47101/tcp"
