# Changelog

All notable changes to dsync. Downloads for every version are on the
[releases page](https://github.com/muhammedaslan34/dsync/releases).

## Unreleased

- Phone app: Android no longer asks for permissions dsync never uses (microphone,
  drawing over other apps, fingerprint), which makes Play Protect less likely to block
  the APK.

## 1.0.11 — 2026-09-30

- **Languages:** the desktop and phone apps come in Arabic, Turkish and French as well as
  English (Settings → Language; "System" follows your device). Arabic is laid out right
  to left. On the desktop, the tray menu and notifications follow the language.
- **Updates:** fixed "GitHub answered 403". When GitHub's API limit is reached, dsync
  reads the release from GitHub's website instead.
- **Phone app:** opens with the dsync logo instead of Expo's placeholder splash screen.

## 1.0.10 — 2026-09-30

- **Phone app:** the Android and iPhone apps have the dsync icon.
- **Building:** the Windows files can be code-signed when a certificate is configured
  (`packaging/sign/README.md`).

## 1.0.9 — 2026-09-30

- **Phone app** for Android (APK) and iPhone (unsigned .ipa). Pair it by scanning a QR
  code (Settings → Connect a phone), then send text, the clipboard, photos and files both
  ways, encrypted.
- Computers listen for phones on TCP 47102; the installers open it on private networks.

## 1.0.8 — 2026-09-30

- **macOS:** a universal disk image for Apple Silicon and Intel Macs, built automatically
  for each release.
- **Remote control:** Desktop mouse mode (the pointer follows yours exactly) or Game mode.
- **Pointer speed while controlling** on GNOME and Windows, restored afterwards.

## 1.0.7 — 2026-09-30

- **Sharper remote control:** streams at your screen's real resolution, with *Standard*,
  *High* (new default) and *Best* quality, and optional full color detail (YUV 4:4:4) for
  crisp small text.

## 1.0.6 — 2026-09-30

- **Clear history:** one conversation (the eraser in its header) or everything
  (Settings → History → Clear all). Received files and running transfers are kept.

## 1.0.5 — 2026-09-30

- **Remote control options:** choose the screen, the size (on Windows the screen switches
  to it, so things look bigger), fullscreen or a window, and 30/60/120 fps. Choices are
  remembered per device.

## 1.0.4 — 2026-09-30

- **Controlling Linux from Windows:** pairing works (dsync checks that Sunshine is reachable
  before pairing).
- **Linux firewall:** installing Sunshine from dsync opens its ports, with an *Allow through
  firewall* button in Settings when they're blocked.

## 1.0.3 — 2026-09-30

- **Remote control on Linux:** Sunshine is started through its own service instead of a
  second copy that fought it for ports.

## 1.0.2 — 2026-09-30

- **Update button:** dsync checks for new versions and installs them (checked against
  `SHA256SUMS` first).
- **Install Moonlight and Sunshine from dsync,** and from the Windows installer.
- **"Can't reach it":** clearer status for a device that can send to you but that you
  can't reach.
- Fixed: received files go to your real Downloads folder (also when OneDrive or your Linux
  desktop moved it); devices on networks that block broadcasts are found directly.

## 1.0.1 — 2026-09-30

- **Windows firewall done for you:** the installer lets dsync through on home and work
  networks, offers to make a Public network Private, and Settings has fix buttons.

## 1.0.0 — 2026-09-30

- First release: send text, files of any size, whole folders and the clipboard between
  Windows and Linux computers, with pairing and end-to-end encryption, resumable
  transfers, a tray icon, and remote control through Moonlight and Sunshine.
